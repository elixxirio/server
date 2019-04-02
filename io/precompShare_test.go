////////////////////////////////////////////////////////////////////////////////
// Copyright © 2018 Privategrity Corporation                                   /
//                                                                             /
// All rights reserved.                                                        /
////////////////////////////////////////////////////////////////////////////////

package io

import (
	pb "gitlab.com/elixxir/comms/mixmessages"
	"gitlab.com/elixxir/comms/node"
	"gitlab.com/elixxir/crypto/cyclic"
	"gitlab.com/elixxir/crypto/large"
	"gitlab.com/elixxir/primitives/id"
	"gitlab.com/elixxir/server/cryptops/precomputation"
	"gitlab.com/elixxir/server/globals"
	"gitlab.com/elixxir/server/services"
	"testing"
)

type DummyPrecompShareHandler struct{}

func (h DummyPrecompShareHandler) Handler(
	roundId string, batchSize uint64, slots []*services.Slot) {
	// Create the PrecompShareMessage
	msg := &pb.PrecompShareMessage{
		RoundID: roundId,
		LastOp:  int32(globals.PRECOMP_SHARE),
		Slots:   make([]*pb.PrecompShareSlot, batchSize),
	}

	// Iterate over the output channel
	for i := uint64(0); i < batchSize; i++ {
		// Type assert Slot to SlotShare
		out := (*slots[i]).(*precomputation.SlotShare)
		// Convert to PrecompShareSlot
		msgSlot := &pb.PrecompShareSlot{
			Slot: out.Slot,
			PartialRoundPublicCypherKey: out.
				PartialRoundPublicCypherKey.Bytes(),
		}

		// Put it into the slice
		msg.Slots[i] = msgSlot
	}

	// Advance internal state to PRECOMP_DECRYPT (the next phase)
	globals.GlobalRoundMap.SetPhase(roundId, globals.PRECOMP_SHARE)

	// Send the completed PrecompShareMessage
	node.SendPrecompShare(NextServer, msg)
}

func TestPrecompShare(t *testing.T) {
	// Set up Grp
	grp := cyclic.NewGroup(large.NewInt(107), large.NewInt(5), large.NewInt(4))
	globals.Clear(t)
	globals.SetGroup(&grp)

	// Create a new Round
	roundId := "test"
	round := globals.NewRound(1, globals.GetGroup())
	id.IsLastNode = false
	// Add round to the GlobalRoundMap
	globals.GlobalRoundMap.AddRound(roundId, round)

	// Create the test channels
	chIn := make(chan *services.Slot, round.BatchSize)
	chOut := make(chan *services.Slot, round.BatchSize)

	// Add the InChannel from the controller to round
	round.AddChannel(globals.PRECOMP_SHARE, chIn)
	// Kick off PrecompShare Transmission Handler
	services.BatchTransmissionDispatch(roundId, round.BatchSize,
		chOut, DummyPrecompShareHandler{})

	// Create a slot to pass into the TransmissionHandler
	var slot services.Slot = &precomputation.SlotShare{
		Slot:                        uint64(0),
		PartialRoundPublicCypherKey: globals.GetGroup().NewInt(3),
	}

	// Pass slot as input to Share's TransmissionHandler
	chOut <- &slot

	// Which should be populated into chIn once received
	received := <-chIn

	// Convert type for comparison
	expected := slot.(*precomputation.SlotShare)
	actual := (*received).(*precomputation.SlotShare)

	// Compare actual/expected
	if expected.Slot != actual.Slot {
		t.Errorf("Slot does not match!")
	}
	if expected.PartialRoundPublicCypherKey.Text(10) !=
		actual.PartialRoundPublicCypherKey.Text(10) {
		t.Errorf("PartialRoundPublicCypherKey does not match!"+
			" Got %v, expected %v.",
			actual.PartialRoundPublicCypherKey.Text(10),
			expected.PartialRoundPublicCypherKey.Text(10))
	}
}
