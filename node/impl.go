////////////////////////////////////////////////////////////////////////////////
// Copyright © 2019 Privategrity Corporation                                   /
//                                                                             /
// All rights reserved.                                                        /
////////////////////////////////////////////////////////////////////////////////

// Package io impl.go implements server utility functions needed to work
// with the comms library
package node

import (
	"gitlab.com/elixxir/comms/connect"
	"gitlab.com/elixxir/comms/mixmessages"
	"gitlab.com/elixxir/comms/node"
	"gitlab.com/elixxir/server/io"
	"gitlab.com/elixxir/server/server"
	"time"
)

// NewImplementation creates a new implementation of the server.
// When a function is added to comms, you'll need to point to it here.
func NewImplementation(instance *server.Instance) *node.Implementation {

	impl := node.NewImplementation()

	impl.Functions.CreateNewRound = func(message *mixmessages.RoundInfo, auth *connect.Auth) error {
		return ReceiveCreateNewRound(instance, message, auth)
	}

	impl.Functions.GetMeasure = func(message *mixmessages.RoundInfo,
		auth *connect.Auth) (*mixmessages.RoundMetrics, error) {
		return ReceiveGetMeasure(instance, message)
	}

	impl.Functions.PostPhase = func(batch *mixmessages.Batch, auth *connect.Auth) {
		ReceivePostPhase(batch, instance)
	}

	impl.Functions.StreamPostPhase = func(streamServer mixmessages.Node_StreamPostPhaseServer, auth *connect.Auth) error {
		return ReceiveStreamPostPhase(streamServer, instance)
	}

	impl.Functions.PostRoundPublicKey = func(pk *mixmessages.RoundPublicKey, auth *connect.Auth) {
		ReceivePostRoundPublicKey(instance, pk)
	}

	impl.Functions.GetRoundBufferInfo = func(auth *connect.Auth) (int, error) {
		return io.GetRoundBufferInfo(instance.GetCompletedPrecomps(), time.Second)
	}

	impl.Functions.GetCompletedBatch = func(auth *connect.Auth) (batch *mixmessages.Batch, e error) {
		return io.GetCompletedBatch(instance.GetCompletedBatchQueue(), time.Second)
	}

	// Receive finish realtime should gather metrics if first node
	if instance.IsFirstNode() {
		impl.Functions.FinishRealtime = func(message *mixmessages.RoundInfo, auth *connect.Auth) error {
			return ReceiveFinishRealtime(instance, message)
		}
	} else {
		impl.Functions.FinishRealtime = func(message *mixmessages.RoundInfo, auth *connect.Auth) error {
			return ReceiveFinishRealtime(instance, message)
		}
	}

	impl.Functions.RequestNonce = func(salt []byte, RSAPubKey string,
		DHPubKey, RSASignedByRegistration, DHSignedByClientRSA []byte, auth *connect.Auth) ([]byte, []byte, error) {
		return io.RequestNonce(instance, salt, RSAPubKey, DHPubKey,
			RSASignedByRegistration, DHSignedByClientRSA, auth)
	}

	impl.Functions.ConfirmRegistration = func(UserID, Signature []byte, auth *connect.Auth) ([]byte, error) {
		return io.ConfirmRegistration(instance, UserID, Signature, auth)
	}
	impl.Functions.PostPrecompResult = func(roundID uint64, slots []*mixmessages.Slot, auth *connect.Auth) error {
		return ReceivePostPrecompResult(instance, roundID, slots)
	}
	impl.Functions.PostNewBatch = func(newBatch *mixmessages.Batch, auth *connect.Auth) error {
		return ReceivePostNewBatch(instance, newBatch)
	}

	impl.Functions.SendRoundTripPing = func(ping *mixmessages.RoundTripPing, auth *connect.Auth) error {
		return ReceiveRoundTripPing(instance, ping)
	}

	impl.Functions.AskOnline = func() error {
		for instance.Online == false {
			time.Sleep(250 * time.Millisecond)
		}
		return nil
	}
	return impl
}
