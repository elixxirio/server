///////////////////////////////////////////////////////////////////////////////
// Copyright © 2020 xx network SEZC                                          //
//                                                                           //
// Use of this source code is governed by a license that can be found in the //
// LICENSE file                                                              //
///////////////////////////////////////////////////////////////////////////////

package realtime

import (
	"gitlab.com/elixxir/comms/mixmessages"
	"gitlab.com/elixxir/crypto/cryptops"
	"gitlab.com/elixxir/crypto/cyclic"
	"gitlab.com/elixxir/gpumathsgo"
	"gitlab.com/elixxir/server/graphs"
	"gitlab.com/elixxir/server/internal/round"
	"gitlab.com/elixxir/server/services"
	"gitlab.com/elixxir/server/storage"
	"gitlab.com/xx_network/primitives/id"
)

const (
	RoundBuff   = 0
	Registry    = 1
	ErrReporter = 4
)

// Stream holding data containing keys and inputs used by decrypt
type KeygenDecryptStream struct {
	Grp        *cyclic.Group
	StreamPool *gpumaths.StreamPool

	// Link to round object
	R *cyclic.IntBuffer
	U *cyclic.IntBuffer

	// Unique to stream
	EcrPayloadA *cyclic.IntBuffer
	EcrPayloadB *cyclic.IntBuffer

	// Components for key generation
	Users        []*id.ID
	Salts        [][]byte
	KeysPayloadA *cyclic.IntBuffer
	KeysPayloadB *cyclic.IntBuffer
	KMACS        [][][]byte

	graphs.KeygenSubStream
}

func (s *KeygenDecryptStream) GetName() string {
	return "RealtimeDecryptStream"
}

//Link creates the stream's internal buffers and
func (s *KeygenDecryptStream) Link(grp *cyclic.Group, batchSize uint32, source ...interface{}) {
	roundBuf := source[RoundBuff].(*round.Buffer)
	userRegistry := source[Registry].(*storage.Storage)
	users := make([]*id.ID, batchSize)
	var clientReporter *round.ClientReport
	var roundID id.Round
	// Find the client error reporter and the roundID (if it exists)
	var ok bool
	for _, face := range source {
		_, ok = face.(*round.ClientReport)
		if ok {
			clientReporter = face.(*round.ClientReport)
		}

		_, ok = face.(id.Round)
		if ok {
			roundID = face.(id.Round)
		}
	}

	for i := uint32(0); i < batchSize; i++ {
		users[i] = &id.ID{}
	}

	var streamPool *gpumaths.StreamPool
	if len(source) >= 4 {
		// All arguments are being passed from the Link call, which should include the stream pool
		streamPool = source[3].(*gpumaths.StreamPool)
	}

	s.LinkRealtimeDecryptStream(grp, batchSize, roundBuf, userRegistry, streamPool, grp.NewIntBuffer(batchSize, grp.NewInt(1)),
		grp.NewIntBuffer(batchSize, grp.NewInt(1)), grp.NewIntBuffer(batchSize, grp.NewInt(1)),
		grp.NewIntBuffer(batchSize, grp.NewInt(1)), users,
		make([][]byte, batchSize), make([][][]byte, batchSize),
		clientReporter, roundID)
}

//Connects the internal buffers in the stream to the passed
func (s *KeygenDecryptStream) LinkRealtimeDecryptStream(grp *cyclic.Group, batchSize uint32,
	round *round.Buffer, storage *storage.Storage, pool *gpumaths.StreamPool, ecrPayloadA, ecrPayloadB,
	keysPayloadA, keysPayloadB *cyclic.IntBuffer, users []*id.ID, salts [][]byte, kmacs [][][]byte,
	clientReporter *round.ClientReport, roundId id.Round) {

	s.Grp = grp
	s.StreamPool = pool

	s.R = round.R.GetSubBuffer(0, batchSize)
	s.U = round.U.GetSubBuffer(0, batchSize)

	s.EcrPayloadA = ecrPayloadA
	s.EcrPayloadB = ecrPayloadB
	s.KeysPayloadA = keysPayloadA
	s.KeysPayloadB = keysPayloadB
	s.Users = users
	s.Salts = salts
	s.KMACS = kmacs

	s.KeygenSubStream.LinkStream(s.Grp, storage, s.Salts, s.KMACS, s.Users, s.KeysPayloadA,
		s.KeysPayloadB, clientReporter, roundId, batchSize)
}

// PermuteStream conforms to this interface.
type RealtimeDecryptSubStreamInterface interface {
	GetRealtimeDecryptSubStream() *KeygenDecryptStream
}

// GetRealtimeDecryptSubStream returns the sub-stream, used to return an embedded struct
// off an interface.
func (s *KeygenDecryptStream) GetRealtimeDecryptSubStream() *KeygenDecryptStream {
	return s
}

func (s *KeygenDecryptStream) Input(index uint32, slot *mixmessages.Slot) error {

	if index >= uint32(s.EcrPayloadA.Len()) {
		return services.ErrOutsideOfBatch
	}

	if !s.Grp.BytesInside(slot.PayloadA, slot.PayloadB) {
		return services.ErrOutsideOfGroup
	}

	// Check that the user id is formatted correctly
	if len(slot.SenderID) != id.ArrIDLen {
		return services.ErrUserIDTooShort
	}

	// Check that the salt is formatted correctly
	if len(slot.Salt) != 32 {
		return services.ErrSaltIncorrectLength
	}

	//copy the user id
	copy((*s.Users[index])[:], slot.SenderID[:])

	//link to the salt
	s.Salts[index] = slot.Salt

	//link to the KMACS
	s.KMACS[index] = slot.KMACs

	s.Grp.SetBytes(s.EcrPayloadA.Get(index), slot.PayloadA)
	s.Grp.SetBytes(s.EcrPayloadB.Get(index), slot.PayloadB)
	return nil
}

func (s *KeygenDecryptStream) Output(index uint32) *mixmessages.Slot {
	return &mixmessages.Slot{
		Index:    index,
		SenderID: (*s.Users[index])[:],
		Salt:     s.Salts[index],
		PayloadA: s.EcrPayloadA.Get(index).Bytes(),
		PayloadB: s.EcrPayloadB.Get(index).Bytes(),
		KMACs:    s.KMACS[index],
	}
}

//module in realtime Decrypt implementing mul3
var DecryptMul3 = services.Module{
	// Multiplies in own Encrypted Keys and Partial Cypher Texts
	Adapt: func(streamInput services.Stream, cryptop cryptops.Cryptop, chunk services.Chunk) error {
		dssi, ok := streamInput.(RealtimeDecryptSubStreamInterface)
		mul3, ok2 := cryptop.(cryptops.Mul3Prototype)

		if !ok || !ok2 {
			return services.InvalidTypeAssert
		}

		ds := dssi.GetRealtimeDecryptSubStream()

		for i := chunk.Begin(); i < chunk.End(); i++ {
			//Do mul3 ecrPayloadA=payloadAKey*R*ecrPayloadA%p
			mul3(ds.Grp, ds.KeysPayloadA.Get(i), ds.R.Get(i), ds.EcrPayloadA.Get(i))
			//Do mul3 ecrPayloadB=payloadBKey*U*ecrPayloadB%p
			mul3(ds.Grp, ds.KeysPayloadB.Get(i), ds.U.Get(i), ds.EcrPayloadB.Get(i))
		}
		return nil
	},
	Cryptop:    cryptops.Mul3,
	NumThreads: services.AutoNumThreads,
	InputSize:  services.AutoInputSize,
	Name:       "DecryptMul3",
}

//module in realtime Decrypt implementing mul3
var DecryptMul3Chunk = services.Module{
	// Multiplies in own Encrypted Keys and Partial Cypher Texts
	Adapt: func(streamInput services.Stream, cryptop cryptops.Cryptop, chunk services.Chunk) error {
		dssi, ok := streamInput.(RealtimeDecryptSubStreamInterface)
		mul3Chunk, ok2 := cryptop.(gpumaths.Mul3ChunkPrototype)

		if !ok || !ok2 {
			return services.InvalidTypeAssert
		}

		ds := dssi.GetRealtimeDecryptSubStream()

		kpa := ds.KeysPayloadA.GetSubBuffer(chunk.Begin(), chunk.End())
		kpb := ds.KeysPayloadB.GetSubBuffer(chunk.Begin(), chunk.End())
		epa := ds.EcrPayloadA.GetSubBuffer(chunk.Begin(), chunk.End())
		epb := ds.EcrPayloadB.GetSubBuffer(chunk.Begin(), chunk.End())
		R := ds.R.GetSubBuffer(chunk.Begin(), chunk.End())
		U := ds.U.GetSubBuffer(chunk.Begin(), chunk.End())
		pool := ds.StreamPool
		grp := ds.Grp

		//Do mul3 ecrPayloadA=payloadAKey*R*ecrPayloadA%p
		err := mul3Chunk(pool, grp, kpa, R, epa, epa)
		if err != nil {
			return err
		}

		//Do mul3 ecrPayloadB=payloadBKey*U*ecrPayloadB%p
		err = mul3Chunk(pool, grp, kpb, U, epb, epb)
		if err != nil {
			return err
		}

		return nil
	},
	Cryptop:    gpumaths.Mul3Chunk,
	NumThreads: 2,
	InputSize:  32,
	Name:       "DecryptMul3Chunk",
}

// InitDecryptGraph called to initialize the graph. Conforms to graphs.Initialize function type
func InitDecryptGraph(gc services.GraphGenerator) *services.Graph {
	/*if viper.GetBool("useGPU") {
		jww.FATAL.Panicf("Using realtime decrypt graph running on CPU instead of equivalent GPU graph")
	}*/
	g := gc.NewGraph("RealtimeDecrypt", &KeygenDecryptStream{})

	decryptKeygen := graphs.Keygen.DeepCopy()
	decryptMul3 := DecryptMul3.DeepCopy()

	g.First(decryptKeygen)
	g.Connect(decryptKeygen, decryptMul3)
	g.Last(decryptMul3)

	return g
}

// InitDecryptGPUGraph called to initialize the graph. Conforms to graphs.Initialize function type
func InitDecryptGPUGraph(gc services.GraphGenerator) *services.Graph {
	/*if !viper.GetBool("useGPU") {
		jww.WARN.Printf("Using realtime decrypt graph running on GPU instead of equivalent CPU graph")
	}*/
	g := gc.NewGraph("RealtimeDecryptGPU", &KeygenDecryptStream{})

	decryptKeygen := graphs.Keygen.DeepCopy()
	decryptMul3Chunk := DecryptMul3Chunk.DeepCopy()

	g.First(decryptKeygen)
	g.Connect(decryptKeygen, decryptMul3Chunk)
	g.Last(decryptMul3Chunk)

	return g
}
