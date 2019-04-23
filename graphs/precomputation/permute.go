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
	"gitlab.com/elixxir/server/graphs"
	"gitlab.com/elixxir/server/node"
	"gitlab.com/elixxir/server/services"
)

// This file implements the Graph for the Precomputation Permute phase
// Permute phase permutes the message keys, the associated data keys, and their cypher
// text, while multiplying in its own keys.

// Stream holding data containing keys and inputs used by Permute
type PermuteStream struct {
	Grp             *cyclic.Group
	PublicCypherKey *cyclic.Int

	// Link to round object
	S   *cyclic.IntBuffer // Encrypted Inverse Permuted Internode Message Key
	V   *cyclic.IntBuffer // Encrypted Inverse Permuted Internode AssociatedData Key
	Y_S *cyclic.IntBuffer // Permuted Internode Message Partial Cypher Text
	Y_V *cyclic.IntBuffer // Permuted Internode AssociatedData Partial Cypher Text

	// Unique to stream
	KeysMsg           *cyclic.IntBuffer
	KeysMsgPermuted   []*cyclic.Int
	CypherMsg         *cyclic.IntBuffer
	CypherMsgPermuted []*cyclic.Int
	KeysAD            *cyclic.IntBuffer
	KeysADPermuted    []*cyclic.Int
	CypherAD          *cyclic.IntBuffer
	CypherADPermuted  []*cyclic.Int

	graphs.PermuteSubStream
}

func (s *PermuteStream) GetName() string {
	return "PrecompPermuteStream"
}

func (s *PermuteStream) Link(batchSize uint32, source interface{}) {
	round := source.(*node.RoundBuffer)

	s.Grp = round.Grp
	s.PublicCypherKey = round.CypherPublicKey

	s.S = round.S.GetSubBuffer(0, batchSize)
	s.V = round.V.GetSubBuffer(0, batchSize)
	s.Y_S = round.Y_S.GetSubBuffer(0, batchSize)
	s.Y_V = round.Y_V.GetSubBuffer(0, batchSize)

	s.KeysMsg = s.Grp.NewIntBuffer(batchSize, s.Grp.NewInt(1))
	s.CypherMsg = s.Grp.NewIntBuffer(batchSize, s.Grp.NewInt(1))
	s.KeysAD = s.Grp.NewIntBuffer(batchSize, s.Grp.NewInt(1))
	s.CypherAD = s.Grp.NewIntBuffer(batchSize, s.Grp.NewInt(1))

	s.CypherADPermuted = make([]*cyclic.Int, batchSize)
	s.CypherMsgPermuted = make([]*cyclic.Int, batchSize)
	s.KeysADPermuted = make([]*cyclic.Int, batchSize)
	s.KeysMsgPermuted = make([]*cyclic.Int, batchSize)

	s.PermuteSubStream.LinkStreams(batchSize, round.Permutations,
		graphs.PermuteIO{
			Input:  s.CypherMsg,
			Output: s.CypherMsgPermuted,
		}, graphs.PermuteIO{
			Input:  s.CypherAD,
			Output: s.CypherADPermuted,
		}, graphs.PermuteIO{
			Input:  s.KeysAD,
			Output: s.KeysADPermuted,
		}, graphs.PermuteIO{
			Input:  s.KeysMsg,
			Output: s.KeysMsgPermuted,
		},
	)
}

func (s *PermuteStream) Input(index uint32, slot *mixmessages.CmixSlot) error {

	if index >= uint32(s.KeysMsg.Len()) {
		return node.ErrOutsideOfBatch
	}

	if !s.Grp.BytesInside(slot.EncryptedMessageKeys, slot.PartialMessageCypherText,
		slot.EncryptedAssociatedDataKeys, slot.PartialAssociatedDataCypherText) {
		return node.ErrOutsideOfGroup
	}

	s.Grp.SetBytes(s.KeysMsg.Get(index), slot.EncryptedMessageKeys)
	s.Grp.SetBytes(s.KeysAD.Get(index), slot.EncryptedAssociatedDataKeys)
	s.Grp.SetBytes(s.CypherMsg.Get(index), slot.PartialMessageCypherText)
	s.Grp.SetBytes(s.CypherAD.Get(index), slot.PartialAssociatedDataCypherText)
	return nil
}

func (s *PermuteStream) Output(index uint32) *mixmessages.CmixSlot {

	return &mixmessages.CmixSlot{
		EncryptedMessageKeys:            s.KeysMsgPermuted[index].Bytes(),
		EncryptedAssociatedDataKeys:     s.KeysADPermuted[index].Bytes(),
		PartialMessageCypherText:        s.CypherMsgPermuted[index].Bytes(),
		PartialAssociatedDataCypherText: s.CypherADPermuted[index].Bytes(),
	}
}

// Module in Precomputation Permute implementing cryptops.Elgamal
var PermuteElgamal = services.Module{
	// Multiplies in own Encrypted Keys and Partial Cypher Texts
	Adapt: func(streamInput services.Stream, cryptop cryptops.Cryptop, chunk services.Chunk) error {
		s, ok := streamInput.(*PermuteStream)
		elgamal, ok2 := cryptop.(cryptops.ElGamalPrototype)

		if !ok || !ok2 {
			return services.InvalidTypeAssert
		}

		for i := chunk.Begin(); i < chunk.End(); i++ {
			// Execute elgamal on the keys for the Message

			// Eq 11.1: Encrypt the Permuted Internode Message Key under Homomorphic Encryption.
			// Eq 13.17: Then multiply the Permuted Internode Message Key under Homomorphic
			// Encryption into the Partial Encrypted Message Precomputation
			// Eq 11.2: Makes the Partial Cypher Text for the Permuted Internode Message Key
			// Eq 13.21: Multiplies the Partial Cypher Text for the Permuted Internode
			// Message Key into the Round Message Partial Cypher Text

			elgamal(s.Grp, s.S.Get(i), s.Y_S.Get(i), s.PublicCypherKey, s.KeysMsg.Get(i), s.CypherMsg.Get(i))

			// Execute elgamal on the keys for the Associated Data
			// Eq 11.1: Encrypt the Permuted Internode AssociatedData Key under Homomorphic Encryption
			// Eq 13.19: Multiplies the Permuted Internode AssociatedData Key under
			// Homomorphic Encryption into the Partial Encrypted AssociatedData Precomputation
			// Eq 11.2: Makes the Partial Cypher Text for the Permuted Internode AssociatedData Key
			// Eq 13.23 Multiplies the Partial Cypher Text for the Permuted Internode
			// AssociatedData Key into the Round AssociatedData Partial Cypher Text

			elgamal(s.Grp, s.V.Get(i), s.Y_V.Get(i), s.PublicCypherKey, s.KeysAD.Get(i), s.CypherAD.Get(i))
		}
		return nil
	},
	Cryptop:    cryptops.ElGamal,
	NumThreads: 5,
	InputSize:  services.AUTO_INPUTSIZE,
	Name:       "PermuteElgamal",
}

// Called to initialize the graph. Conforms to graphs.Initialize function type
func InitPermuteGraph(gc services.GraphGenerator) *services.Graph {
	g := gc.NewGraph("PrecompPermute", &PermuteStream{})

	PermuteElgamal := PermuteElgamal.DeepCopy()
	Permute := graphs.Permute.DeepCopy()

	g.First(PermuteElgamal)
	g.Connect(PermuteElgamal, Permute)
	g.Last(Permute)

	return g
}
