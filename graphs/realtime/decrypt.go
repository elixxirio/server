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
	"gitlab.com/elixxir/server/globals"
	"gitlab.com/elixxir/server/graphs"
	"gitlab.com/elixxir/server/internal/round"
	"gitlab.com/elixxir/server/services"
	"gitlab.com/xx_network/primitives/id"
)

const (
	RoundBuff   = 0
	Registry    = 1
	ErrReporter = 4
)

// Stream holding data containing keys and inputs used by decrypt
type KeygenDecryptStream struct {
	Grp *cyclic.Group

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
func (ds *KeygenDecryptStream) Link(grp *cyclic.Group, batchSize uint32, source ...interface{}) {
	roundBuf := source[RoundBuff].(*round.Buffer)
	userRegistry := source[Registry].(globals.UserRegistry)
	users := make([]*id.ID, batchSize)
	var clientReporter *round.ClientReport
	var roundID id.Round
	// Find the client error reporter and the roundID (if it exists)
	for _, face := range source {
		clientReporter, _ = face.(*round.ClientReport)
		roundID, _ = face.(id.Round)
	}

	for i := uint32(0); i < batchSize; i++ {
		users[i] = &id.ID{}
	}

	ds.LinkRealtimeDecryptStream(grp, batchSize, roundBuf, userRegistry, grp.NewIntBuffer(batchSize, grp.NewInt(1)),
		grp.NewIntBuffer(batchSize, grp.NewInt(1)), grp.NewIntBuffer(batchSize, grp.NewInt(1)),
		grp.NewIntBuffer(batchSize, grp.NewInt(1)), users,
		make([][]byte, batchSize), make([][][]byte, batchSize),
		clientReporter, roundID)
}

//Connects the internal buffers in the stream to the passed
func (ds *KeygenDecryptStream) LinkRealtimeDecryptStream(grp *cyclic.Group, batchSize uint32,
	round *round.Buffer, userRegistry globals.UserRegistry, ecrPayloadA, ecrPayloadB,
	keysPayloadA, keysPayloadB *cyclic.IntBuffer, users []*id.ID, salts [][]byte, kmacs [][][]byte,
	clientReporter *round.ClientReport, roundId id.Round) {

	ds.Grp = grp

	ds.R = round.R.GetSubBuffer(0, batchSize)
	ds.U = round.U.GetSubBuffer(0, batchSize)

	ds.EcrPayloadA = ecrPayloadA
	ds.EcrPayloadB = ecrPayloadB
	ds.KeysPayloadA = keysPayloadA
	ds.KeysPayloadB = keysPayloadB
	ds.Users = users
	ds.Salts = salts
	ds.KMACS = kmacs

	ds.KeygenSubStream.LinkStream(ds.Grp, userRegistry, ds.Salts, ds.KMACS, ds.Users, ds.KeysPayloadA, ds.KeysPayloadB, clientReporter, roundId)
}

// PermuteStream conforms to this interface.
type RealtimeDecryptSubStreamInterface interface {
	GetRealtimeDecryptSubStream() *KeygenDecryptStream
}

// getPermuteSubStream returns the sub-stream, used to return an embedded struct
// off an interface.
func (ds *KeygenDecryptStream) GetRealtimeDecryptSubStream() *KeygenDecryptStream {
	return ds
}

func (ds *KeygenDecryptStream) Input(index uint32, slot *mixmessages.Slot) error {

	if index >= uint32(ds.EcrPayloadA.Len()) {
		return services.ErrOutsideOfBatch
	}

	if !ds.Grp.BytesInside(slot.PayloadA, slot.PayloadB) {
		return services.ErrOutsideOfGroup
	}

	// Check that the user id is formatted correctly
	if len(slot.SenderID) != id.ArrIDLen {
		return globals.ErrUserIDTooShort
	}

	// Check that the salt is formatted correctly
	if len(slot.Salt) != 32 {
		return globals.ErrSaltIncorrectLength
	}

	//copy the user id
	copy((*ds.Users[index])[:], slot.SenderID[:])

	//link to the salt
	ds.Salts[index] = slot.Salt

	//link to the KMACS
	ds.KMACS[index] = slot.KMACs

	ds.Grp.SetBytes(ds.EcrPayloadA.Get(index), slot.PayloadA)
	ds.Grp.SetBytes(ds.EcrPayloadB.Get(index), slot.PayloadB)
	return nil
}

func (ds *KeygenDecryptStream) Output(index uint32) *mixmessages.Slot {
	return &mixmessages.Slot{
		Index:    index,
		SenderID: (*ds.Users[index])[:],
		Salt:     ds.Salts[index],
		PayloadA: ds.EcrPayloadA.Get(index).Bytes(),
		PayloadB: ds.EcrPayloadB.Get(index).Bytes(),
		KMACs:    ds.KMACS[index],
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

// InitDecryptGraph called to initialize the graph. Conforms to graphs.Initialize function type
func InitDecryptGraph(gc services.GraphGenerator) *services.Graph {
	g := gc.NewGraph("RealtimeDecrypt", &KeygenDecryptStream{})

	decryptKeygen := graphs.Keygen.DeepCopy()
	decryptMul3 := DecryptMul3.DeepCopy()

	g.First(decryptKeygen)
	g.Connect(decryptKeygen, decryptMul3)
	//g.First(decryptMul3)
	g.Last(decryptMul3)

	return g
}
