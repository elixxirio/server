////////////////////////////////////////////////////////////////////////////////
// Copyright © 2018 Privategrity Corporation                                   /
//                                                                             /
// All rights reserved.                                                        /
////////////////////////////////////////////////////////////////////////////////

package io

import (
	jww "github.com/spf13/jwalterweatherman"
	pb "gitlab.com/elixxir/comms/mixmessages"
	"gitlab.com/elixxir/comms/node"
	"gitlab.com/elixxir/crypto/hash"
	"gitlab.com/elixxir/primitives/format"
	"gitlab.com/elixxir/primitives/id"
	"gitlab.com/elixxir/server/cryptops/realtime"
	"gitlab.com/elixxir/server/globals"
	"gitlab.com/elixxir/server/services"
	"time"
)

// Blank struct for implementing services.BatchTransmission
type RealtimeIdentifyHandler struct{}

// TransmissionHandler for RealtimeIdentifyMessages
func (h RealtimeIdentifyHandler) Handler(
	roundId string, batchSize uint64, slots []*services.Slot) {
	startTime := time.Now()
	jww.INFO.Printf("Starting RealtimeIdentify.Handler(RoundId: %s) at %s",
		roundId, startTime.Format(time.RFC3339))

	elapsed := startTime.Sub(globals.GlobalRoundMap.GetRound(roundId).
		CryptopStartTimes[globals.REAL_IDENTIFY])

	jww.DEBUG.Printf("RealtimeIdentify Crypto took %v ms for "+
		"RoundId %s", elapsed, roundId)

	// Create the RealtimeEncryptMessage
	msg := &pb.RealtimeEncryptMessage{
		RoundID: roundId,
		LastOp:  int32(globals.REAL_IDENTIFY),
		Slots:   make([]*pb.CmixMessage, batchSize),
	}

	// Get round
	round := globals.GlobalRoundMap.GetRound(roundId)
	if round == nil {
		jww.INFO.Printf("skipping round %s, because it's dead", roundId)
		return
	}

	// Iterate over the input slots
	cMixHash, _ := hash.NewCMixHash()
	for i := range slots {
		out := (*slots[i]).(*realtime.Slot)
		associatedData := out.AssociatedData.LeftpadBytes(uint64(format.TOTAL_LEN))
		encryptedMsg := round.LastNode.EncryptedMessage[i].Bytes()
		cMixHash.Reset()
		cMixHash.Write(associatedData)
		cMixHash.Write(encryptedMsg)
		// Convert to CmixMessage
		msgSlot := &pb.CmixMessage{
			SenderID:       id.ZeroID[:],
			AssociatedData: associatedData,
			MessagePayload: encryptedMsg,
			Salt:           cMixHash.Sum(nil),
		}

		// Append the RealtimeEncryptSlot to the RealtimeEncryptMessage
		msg.Slots[out.Slot] = msgSlot
	}

	// Advance internal state to the next phase
	globals.GlobalRoundMap.SetPhase(roundId, globals.REAL_ENCRYPT)

	// Send the first RealtimeEncrypt Message
	sendTime := time.Now()
	jww.INFO.Printf("Sending RealtimeEncrypt Messages to %v at %s",
		NextServer, sendTime.Format(time.RFC3339))
	node.SendRealtimeEncrypt(NextServer, msg)

	endTime := time.Now()
	jww.INFO.Printf("Finished RealtimeIdentify.Handler(RoundId: %s) in %d ms",
		roundId, (endTime.Sub(startTime))/time.Millisecond)
}
