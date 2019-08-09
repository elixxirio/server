////////////////////////////////////////////////////////////////////////////////
// Copyright © 2019 Privategrity Corporation                                   /
//                                                                             /
// All rights reserved.                                                        /
////////////////////////////////////////////////////////////////////////////////

package precomputation

import (
	"gitlab.com/elixxir/comms/mixmessages"
	"gitlab.com/elixxir/crypto/cryptops"
	"gitlab.com/elixxir/crypto/cyclic"
	"gitlab.com/elixxir/server/server/round"
	"gitlab.com/elixxir/server/services"
)

// This file implements the Graph for the Precomputation Reveal phase.
// The reveal phase removes cypher keys from the message and
// associated data cypher text, revealing the private keys for the round.

// RevealStream holds data containing private key from encrypt and inputs used by strip
type RevealStream struct {
	Grp *cyclic.Group

	//Link to round object
	Z *cyclic.Int

	// Unique to stream
	CypherPayloadA *cyclic.IntBuffer
	CypherPayloadB *cyclic.IntBuffer
}

// GetName returns stream name
func (s *RevealStream) GetName() string {
	return "PrecompRevealStream"
}

// Link binds stream to state objects in round
func (s *RevealStream) Link(grp *cyclic.Group, batchSize uint32, source ...interface{}) {
	roundBuffer := source[0].(*round.Buffer)

	s.LinkStream(grp, batchSize, roundBuffer,
		grp.NewIntBuffer(batchSize, grp.NewInt(1)),
		grp.NewIntBuffer(batchSize, grp.NewInt(1)))
}

// LinkStream is called by Link other stream objects from round
func (s *RevealStream) LinkStream(grp *cyclic.Group, batchSize uint32, roundBuffer *round.Buffer, CypherPayloadA, CypherPayloadB *cyclic.IntBuffer) {
	s.Grp = grp

	s.Z = roundBuffer.Z

	s.CypherPayloadA = CypherPayloadA
	s.CypherPayloadB = CypherPayloadB
}

// Input initializes stream inputs from slot
func (s *RevealStream) Input(index uint32, slot *mixmessages.Slot) error {

	if index >= uint32(s.CypherPayloadA.Len()) {
		return services.ErrOutsideOfBatch
	}

	if !s.Grp.BytesInside(slot.PartialPayloadACypherText, slot.PartialPayloadBCypherText) {
		return services.ErrOutsideOfGroup
	}

	s.Grp.SetBytes(s.CypherPayloadA.Get(index), slot.PartialPayloadACypherText)
	s.Grp.SetBytes(s.CypherPayloadB.Get(index), slot.PartialPayloadBCypherText)
	return nil
}

// Output returns a cmix slot message
func (s *RevealStream) Output(index uint32) *mixmessages.Slot {

	return &mixmessages.Slot{
		Index:                           index,
		PartialPayloadACypherText:        s.CypherPayloadA.Get(index).Bytes(),
		PartialPayloadBCypherText: s.CypherPayloadB.Get(index).Bytes(),
	}
}

type revealSubstreamInterface interface {
	getSubStream() *RevealStream
}

// getSubStream implements reveal interface to return stream object
func (s *RevealStream) getSubStream() *RevealStream {
	return s
}

// RevealRootCoprime is a module in precomputation reveeal implementing cryptops.RootCoprimePrototype
var RevealRootCoprime = services.Module{
	// Runs root coprime for cypher message and cypher associated data
	Adapt: func(streamInput services.Stream, cryptop cryptops.Cryptop, chunk services.Chunk) error {
		s, ok := streamInput.(revealSubstreamInterface)
		rootCoprime, ok2 := cryptop.(cryptops.RootCoprimePrototype)

		if !ok || !ok2 {
			return services.InvalidTypeAssert
		}

		rs := s.getSubStream()
		tmp := rs.Grp.NewMaxInt()

		for i := chunk.Begin(); i < chunk.End(); i++ {
			// Execute rootCoprime on the keys for the Message
			// Eq 15.11 Root by cypher key to remove one layer of homomorphic
			// encryption from partially encrypted message cypher text.

			rootCoprime(rs.Grp, rs.CypherPayloadA.Get(i), rs.Z, tmp)
			rs.Grp.Set(rs.CypherPayloadA.Get(i), tmp)

			// Execute rootCoprime on the keys for the associated data
			// Eq 15.13 Root by cypher key to remove one layer of homomorphic
			// encryption from partially encrypted associated data cypher text.
			rootCoprime(rs.Grp, rs.CypherPayloadB.Get(i), rs.Z, tmp)
			rs.Grp.Set(rs.CypherPayloadB.Get(i), tmp)
		}
		return nil
	},
	Cryptop:    cryptops.RootCoprime,
	NumThreads: services.AutoNumThreads,
	InputSize:  services.AutoInputSize,
	Name:       "RevealRootCoprime",
}

// InitRevealGraph called to initialize the graph. Conforms to graphs.Initialize function type
func InitRevealGraph(gc services.GraphGenerator) *services.Graph {
	graph := gc.NewGraph("PrecompReveal", &RevealStream{})

	revealRootCoprime := RevealRootCoprime.DeepCopy()

	graph.First(revealRootCoprime)
	graph.Last(revealRootCoprime)

	return graph
}
