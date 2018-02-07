package io

import (
	"gitlab.com/privategrity/crypto/cyclic"
	"gitlab.com/privategrity/server/cryptops/realtime"
	"gitlab.com/privategrity/server/globals"
	"gitlab.com/privategrity/server/services"
	"testing"
)

func TestRealtimeEncrypt(t *testing.T) {
	// Create a new Round
	roundId := "test"
	round := globals.NewRound(1)
	// Add round to the GlobalRoundMap
	globals.GlobalRoundMap.AddRound(roundId, round)

	// Create the test channels
	chIn := make(chan *services.Slot, round.BatchSize)
	chOut := make(chan *services.Slot, round.BatchSize)

	// Add the InChannel from the controller to round
	round.AddChannel(globals.REAL_ENCRYPT, chIn)
	// Kick off RealtimeEncrypt Transmission Handler
	services.BatchTransmissionDispatch(roundId, round.BatchSize,
		chOut, RealtimeEncryptHandler{})

	// Create a slot to pass into the TransmissionHandler
	var slot services.Slot = &realtime.SlotEncryptOut{
		Slot:             uint64(0),
		RecipientID:      uint64(42),
		EncryptedMessage: cyclic.NewInt(7),
	}

	// Pass slot as input to Encrypt's TransmissionHandler
	chOut <- &slot

	// Which should be populated into chIn once received
	received := <-chIn

	// Convert type for comparison
	expected := slot.(*realtime.SlotEncryptOut)
	actual := (*received).(*realtime.SlotEncryptIn)

	// Compare actual/expected
	if expected.Slot != actual.Slot {
		t.Errorf("Slot does not match!")
	}
	if expected.RecipientID != actual.RecipientID {
		t.Errorf("RecipientID does not match!"+
			" Got %v, expected %v.",
			actual.RecipientID,
			expected.RecipientID)
	}
	if expected.EncryptedMessage.Text(10) !=
		actual.EncryptedMessage.Text(10) {
		t.Errorf("EncryptedMessage does not match!"+
			" Got %v, expected %v.",
			actual.EncryptedMessage.Text(10),
			expected.EncryptedMessage.Text(10))
	}
}
