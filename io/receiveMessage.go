////////////////////////////////////////////////////////////////////////////////
// Copyright © 2018 Privategrity Corporation                                   /
//                                                                             /
// All rights reserved.                                                        /
////////////////////////////////////////////////////////////////////////////////

package io

import (
	jww "github.com/spf13/jwalterweatherman"
	pb "gitlab.com/privategrity/comms/mixmessages"
	"gitlab.com/privategrity/crypto/cyclic"
	"gitlab.com/privategrity/server/cryptops/realtime"
	"gitlab.com/privategrity/server/globals"
)

type ReceiveMessageHandler struct{}

// Reception handler for ReceiveMessageFromClient
func (s ServerImpl) ReceiveMessageFromClient(msg *pb.CmixMessage) {
	jww.DEBUG.Printf("Received message from client: %v...\n", msg)

	// Verify message fields are within the global cyclic group
	recipientId := cyclic.NewIntFromBytes(msg.RecipientID)
	messagePayload := cyclic.NewIntFromBytes(msg.MessagePayload)
	if globals.Grp.Inside(recipientId) && globals.Grp.Inside(messagePayload) {
		jww.DEBUG.Printf("Adding message from client with sender id: %d",
			msg.SenderID)
		// Convert message to a Slot
		inputMsg := realtime.RealtimeSlot{
			Slot:               0, // Set in RunRealTime() in node/node.go
			CurrentID:          msg.SenderID,
			Message:            messagePayload,
			EncryptedRecipient: recipientId,
		}
		MessageCh <- &inputMsg
	} else {
		jww.ERROR.Printf("Received message is not in the group: MsgPayload %v " +
			"RecipientID %v", messagePayload.Text(10), recipientId.Text(10))
	}
}