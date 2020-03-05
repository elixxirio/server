////////////////////////////////////////////////////////////////////////////////
// Copyright © 2019 Privategrity Corporation                                   /
//                                                                             /
// All rights reserved.                                                        /
////////////////////////////////////////////////////////////////////////////////

package node

import (
	"encoding/json"
	"github.com/pkg/errors"
	jww "github.com/spf13/jwalterweatherman"
	"gitlab.com/elixxir/comms/connect"
	"gitlab.com/elixxir/comms/mixmessages"
	"gitlab.com/elixxir/comms/network"
	"gitlab.com/elixxir/comms/node"
	"gitlab.com/elixxir/primitives/current"
	"gitlab.com/elixxir/primitives/id"
	"gitlab.com/elixxir/server/io"
	"gitlab.com/elixxir/server/server"
	"gitlab.com/elixxir/server/server/measure"
	"gitlab.com/elixxir/server/server/phase"
	"gitlab.com/elixxir/server/services"
	"time"
)

const errFailedToWait = "Waiting for %s failed."
const errCouldNotWait = "Could not wait for %s"

// ReceivePostRoundPublicKey from last node and sets it for the round
// for each node. Also starts precomputation decrypt phase with a
// batch
func ReceivePostRoundPublicKey(instance *server.Instance,
	pk *mixmessages.RoundPublicKey, auth *connect.Auth) error {
	ok, err := instance.GetStateMachine().WaitFor(current.PRECOMPUTING, 250)
	if err != nil {
		return errors.WithMessagef(err, errFailedToWait, current.PRECOMPUTING.String())
	}
	if !ok {
		return errors.Errorf(errCouldNotWait, current.PRECOMPUTING.String())
	}

	roundID := id.Round(pk.Round.ID)

	// Verify that auth is good and sender is last node
	expectedID := instance.GetTopology().GetLastNode().String()
	if !auth.IsAuthenticated || auth.Sender.GetId() != expectedID {
		jww.INFO.Printf("[%s]: RID %d ReceivePostRoundPublicKey failed auth "+
			"(expected ID: %s, received ID: %s, auth: %v)",
			instance, roundID, expectedID, auth.Sender.GetId(),
			auth.IsAuthenticated)
		return connect.AuthError(auth.Sender.GetId())
	}

	jww.INFO.Printf("[%s]: RID %d PostRoundPublicKey START", instance,
		roundID)

	rm := instance.GetRoundManager()

	tag := phase.PrecompShare.String() + "Verification"

	r, p, err := rm.HandleIncomingComm(roundID, tag)
	if err != nil {
		jww.FATAL.Panicf("[%s]: Error on reception of "+
			"PostRoundPublicKey comm, should be able to return: \n %+v",
			instance, err)
	}
	p.Measure(measure.TagVerification)

	err = io.PostRoundPublicKey(instance.GetGroup(), r.GetBuffer(), pk)
	if err != nil {
		jww.FATAL.Panicf("[%s]: Error on posting PostRoundPublicKey "+
			"to io, should be able to return: %+v", instance, err)
	}

	jww.INFO.Printf("[%s]: RID %d PostRoundPublicKey PK is: %s",
		instance, roundID, r.GetBuffer().CypherPublicKey.Text(16))

	p.UpdateFinalStates()

	jww.INFO.Printf("[%s]: RID %d PostRoundPublicKey END", instance,
		roundID)

	if r.GetTopology().IsFirstNode(instance.GetID()) {
		// We need to make a fake batch here because
		// we start the precomputation decrypt phase
		// afterwards.
		// This phase needs values of 1 for the keys & cypher
		// so we can apply modular multiplication afterwards.
		// Without this the ElGamal cryptop would need to
		// support this edge case.

		batchSize := r.GetBuffer().GetBatchSize()
		blankBatch := &mixmessages.Batch{}

		blankBatch.Round = pk.Round
		blankBatch.FromPhase = int32(phase.PrecompDecrypt)
		blankBatch.Slots = make([]*mixmessages.Slot, batchSize)

		for i := uint32(0); i < batchSize; i++ {
			blankBatch.Slots[i] = &mixmessages.Slot{
				EncryptedPayloadAKeys:     []byte{1},
				EncryptedPayloadBKeys:     []byte{1},
				PartialPayloadACypherText: []byte{1},
				PartialPayloadBCypherText: []byte{1},
			}
		}
		decrypt, err := r.GetPhase(phase.PrecompDecrypt)
		if err != nil {
			jww.FATAL.Panicf("Error on first node PostRoundPublicKey "+
				"comm, should be able to get decrypt phase: %+v", err)
		}

		jww.INFO.Printf("[%s]: RID %d PostRoundPublicKey FIRST NODE START PHASE \"%s\"", instance,
			roundID, decrypt.GetType())

		queued :=
			decrypt.AttemptToQueue(instance.GetResourceQueue().GetPhaseQueue())

		decrypt.Measure(measure.TagReceiveOnReception)

		if !queued {
			jww.FATAL.Panicf("Error on first node PostRoundPublicKey " +
				"comm, should be able to queue decrypt phase")
		}

		err = io.PostPhase(decrypt, blankBatch)

		if err != nil {
			jww.FATAL.Panicf("Error on first node PostRoundPublicKey "+
				"comm, should be able to post to decrypt phase: %+v", err)
		}
	}
	return nil
}

// ReceivePostPrecompResult handles the state checks and edge checks of
// receiving the result of the precomputation
func ReceivePostPrecompResult(instance *server.Instance, roundID uint64,
	slots []*mixmessages.Slot, auth *connect.Auth) error {
	ok, err := instance.GetStateMachine().WaitFor(current.PRECOMPUTING, 250)
	if err != nil {
		return errors.WithMessagef(err, errFailedToWait, current.PRECOMPUTING.String())
	}
	if !ok {
		return errors.Errorf(errCouldNotWait, current.PRECOMPUTING.String())
	}

	// Check for proper authentication and expected sender
	expectedID := instance.GetTopology().GetLastNode().String()
	if !auth.IsAuthenticated || auth.Sender.GetId() != expectedID {
		jww.INFO.Printf("[%s]: RID %d PostPrecompResult failed auth "+
			"(expected ID: %s, received ID: %s, auth: %v)",
			instance, roundID, expectedID, auth.Sender.GetId(),
			auth.IsAuthenticated)
		return connect.AuthError(auth.Sender.GetId())
	}

	jww.INFO.Printf("[%s]: RID %d PostPrecompResult START", instance,
		roundID)

	rm := instance.GetRoundManager()

	tag := phase.PrecompReveal.String() + "Verification"
	r, p, err := rm.HandleIncomingComm(id.Round(roundID), tag)
	if err != nil {
		jww.FATAL.Panicf("[%s]: Error on reception of "+
			"PostPrecompResult comm, should be able to return: \n %+v",
			instance, err)
	}
	p.Measure(measure.TagVerification)

	err = io.PostPrecompResult(r.GetBuffer(), instance.GetGroup(), slots)
	if err != nil {
		return errors.Wrapf(err,
			"Couldn't post precomp result for round %v", roundID)
	}

	p.UpdateFinalStates()

	// Update the state in a gofunc
	go func() {
		ok, err = instance.GetStateMachine().Update(current.STANDBY)
		if err != nil {
			jww.FATAL.Panicf("Failed to transition to state STANDBY: %+v", err)
		}
		if !ok {
			jww.FATAL.Panic("Could not transition to state STANDBY")
		}
	}()
	return nil
}

// ReceivePostPhase handles the state checks and edge checks of receiving a
// phase operation
func ReceivePostPhase(batch *mixmessages.Batch, instance *server.Instance, auth *connect.Auth) error {
	nodeID := instance.GetID()
	roundID := id.Round(batch.Round.ID)
	phaseTy := phase.Type(batch.FromPhase).String()

	rm := instance.GetRoundManager()
	r, err := rm.GetRound(roundID)
	if err != nil {
		return errors.WithMessagef(err, "Failed to get round %d", roundID)
	}

	topology := r.GetTopology()
	prevNodeID := topology.GetPrevNode(nodeID)

	// Check for proper authentication and if the sender
	// is the previous node in the circuit
	if !auth.IsAuthenticated || prevNodeID.String() != auth.Sender.GetId() {
		jww.FATAL.Panicf("Error on PostPhase: "+
			"Attempted communication by %+v has not been authenticated", auth.Sender)
	}

	// Waiting for correct phase
	ptype := r.GetCurrentPhaseType()
	toWait := shouldWait(ptype)
	if toWait == current.ERROR {
		return errors.Errorf("Phase %+s has not associated node activity", ptype)
	} else {
		ok, err := instance.GetStateMachine().WaitFor(toWait, 250)
		if err != nil {
			return errors.WithMessagef(err, errFailedToWait, toWait.String())
		}
		if !ok {
			return errors.Errorf(errCouldNotWait, toWait.String())
		}
	}

	//Check if the operation can be done and get the correct phase if it can
	_, p, err := rm.HandleIncomingComm(roundID, phaseTy)
	if err != nil {
		jww.FATAL.Panicf("[%s]: Error on reception of "+
			"PostPhase comm, should be able to return: \n %+v",
			instance, err)
	}
	p.Measure(measure.TagReceiveOnReception)

	jww.INFO.Printf("[%s]: RID %d PostPhase FROM \"%s\" FOR \"%s\" RECIEVE/START", instance,
		roundID, phaseTy, p.GetType())
	//queue the phase to be operated on if it is not queued yet
	p.AttemptToQueue(instance.GetResourceQueue().GetPhaseQueue())

	//HACK HACK HACK
	//The share phase needs a batchsize of 1, when it receives
	// from generation on the first node this will do the
	// conversion on the batch
	if p.GetType() == phase.PrecompShare && len(batch.Slots) != 1 {
		batch.Slots = batch.Slots[:1]
		batch.Slots[0].PartialRoundPublicCypherKey =
			instance.GetGroup().GetG().Bytes()
		jww.INFO.Printf("[%s]: RID %d PostPhase PRECOMP SHARE HACK "+
			"HACK HACK", instance, roundID)
	}

	batch.FromPhase = int32(p.GetType())

	//send the data to the phase
	err = io.PostPhase(p, batch)

	if err != nil {
		jww.FATAL.Panicf("Error on PostPhase comm, should be"+
			" able to return: %+v", err)
	}
	return nil
}

// ReceiveStreamPostPhase handles the state checks and edge checks of
// receiving a phase operation
func ReceiveStreamPostPhase(streamServer mixmessages.Node_StreamPostPhaseServer,
	instance *server.Instance, auth *connect.Auth) error {
	// Get batch info
	batchInfo, err := node.GetPostPhaseStreamHeader(streamServer)
	if err != nil {
		return err
	}
	roundID := id.Round(batchInfo.Round.ID)
	rm := instance.GetRoundManager()
	r, err := rm.GetRound(roundID)
	if err != nil {
		return errors.WithMessagef(err, "Failed to get round %d", roundID)
	}
	topology := r.GetTopology()

	// Check for proper authentication and expected sender
	nodeID := instance.GetID()
	prevNodeID := topology.GetPrevNode(nodeID)
	if !auth.IsAuthenticated || prevNodeID.String() != auth.Sender.GetId() {
		errMsg := errors.Errorf("[%s]: Reception of StreamPostPhase comm failed authentication: "+
			"(Expected ID: %+v, received id: %+v.\n Auth: %+v)", instance,
			prevNodeID, auth.Sender.GetId(), auth.IsAuthenticated)

		jww.ERROR.Println(errMsg)
		return errMsg

	}

	// Waiting for correct phase
	ptype := r.GetCurrentPhaseType()
	toWait := shouldWait(ptype)
	if toWait == current.ERROR {
		return errors.Errorf("Phase %+s has not associated node activity", ptype)
	} else {
		ok, err := instance.GetStateMachine().WaitFor(toWait, 250)
		if err != nil {
			return errors.WithMessagef(err, errFailedToWait, toWait.String())
		}
		if !ok {
			return errors.Errorf(errCouldNotWait, toWait.String())
		}
	}

	phaseTy := phase.Type(batchInfo.FromPhase).String()

	// Check if the operation can be done and get the correct
	// phase if it can
	_, p, err := rm.HandleIncomingComm(roundID, phaseTy)
	if err != nil {
		jww.FATAL.Panicf("[%s]: Error on reception of "+
			"StreamPostPhase comm, should be able to return: \n %+v",
			instance, err)
	}
	p.Measure(measure.TagReceiveOnReception)

	jww.INFO.Printf("[%s]: RID %d StreamPostPhase FROM \"%s\" TO \"%s\" RECIEVE/START", instance,
		roundID, phaseTy, p.GetType())

	//queue the phase to be operated on if it is not queued yet
	p.AttemptToQueue(instance.GetResourceQueue().GetPhaseQueue())

	strmErr := io.StreamPostPhase(p, batchInfo.BatchSize, streamServer)

	return strmErr

}

// Receive PostNewBatch comm from the gateway
// This should include an entire new batch that's ready for realtime processing
func ReceivePostNewBatch(instance *server.Instance,
	newBatch *mixmessages.Batch, auth *connect.Auth) error {
	// Check that authentication is good and the sender is our gateway, otherwise error
	if !auth.IsAuthenticated || auth.Sender.GetId() != instance.GetGateway().String() {
		jww.WARN.Printf("[%s]: ReceivePostNewBatch failed auth (sender ID: %s, auth: %v, expected: %s)",
			instance, auth.Sender.GetId(), auth.IsAuthenticated, instance.GetGateway().String())
		return connect.AuthError(auth.Sender.GetId())
	}

	// Wait for state to be REALTIME
	ok, err := instance.GetStateMachine().WaitFor(current.REALTIME, 250)
	if err != nil {
		return errors.WithMessagef(err, errFailedToWait, current.REALTIME.String())
	}
	if !ok {
		return errors.Errorf(errCouldNotWait, current.REALTIME.String())
	}

	// Get the roundinfo object
	ri := newBatch.Round
	rm := instance.GetRoundManager()
	rnd, err := rm.GetRound(ri.GetRoundId())
	if err != nil {
		return errors.WithMessage(err, "Failed to get round object from manager")
	}

	jww.INFO.Printf("[%s]: RID %d PostNewBatch START", instance,
		ri.ID)

	if uint32(len(newBatch.Slots)) != rnd.GetBuffer().GetBatchSize() {
		jww.FATAL.Panicf("[%s]: RID %d PostNewBatch ERROR - Gateway sent "+
			"batch with improper size", instance, newBatch.Round.ID)
	}

	p, err := rnd.GetPhase(phase.RealDecrypt)

	if err != nil {
		jww.FATAL.Panicf(
			"[%s]: RID %d Error on incoming PostNewBatch comm, could "+
				"not find phase \"%s\": %v", instance, newBatch.Round.ID,
			phase.RealDecrypt, err)
	}

	if p.GetState() != phase.Active {
		jww.FATAL.Panicf(
			"[%s]: RID %d Error on incoming PostNewBatch comm, phase "+
				"\"%s\" at incorrect state (\"%s\" vs \"Active\")", instance,
			newBatch.Round.ID, phase.RealDecrypt, p.GetState())
	}

	p.Measure(measure.TagReceiveOnReception)

	// Queue the phase if it hasn't been done yet
	p.AttemptToQueue(instance.GetResourceQueue().GetPhaseQueue())
	for i := range newBatch.Slots {
		jww.DEBUG.Printf("new Batch: %#v", newBatch.Slots[i])
	}
	err = io.PostPhase(p, newBatch)

	if err != nil {
		jww.FATAL.Panicf("[%s]: RID %d Error on incoming PostNewBatch comm at"+
			" io PostPhase: %+v", instance, newBatch.Round.ID, err)
	}

	jww.INFO.Printf("[%s]: RID %d PostNewBatch END", instance,
		newBatch.Round.ID)

	return nil
}

// ReceiveFinishRealtime handles the state checks and edge checks of
// receiving the signal that the realtime has completed
func ReceiveFinishRealtime(instance *server.Instance, msg *mixmessages.RoundInfo,
	auth *connect.Auth) error {

	ok, err := instance.GetStateMachine().WaitFor(current.REALTIME, 250)
	if err != nil {
		return errors.WithMessagef(err, errFailedToWait, current.REALTIME.String())
	}
	if !ok {
		return errors.Errorf(errCouldNotWait, current.REALTIME.String())
	}

	//check that the round should have finished and return it
	roundID := id.Round(msg.ID)

	expectedID := instance.GetTopology().GetLastNode()
	if !auth.IsAuthenticated || auth.Sender.GetId() != expectedID.String() {
		jww.INFO.Printf("[%s]: RID %d FinishRealtime failed auth "+
			"(expected ID: %s, received ID: %s, auth: %v)",
			instance, roundID, expectedID, auth.Sender.GetId(),
			auth.IsAuthenticated)
		return connect.AuthError(auth.Sender.GetId())
	}

	jww.INFO.Printf("[%s]: RID %d ReceiveFinishRealtime START",
		instance, roundID)

	rm := instance.GetRoundManager()
	r, err := rm.GetRound(roundID)
	if err != nil {
		return errors.Errorf("Failed to get round with round ID: %+v", roundID)
	}

	tag := phase.RealPermute.String() + "Verification"
	r, p, err := rm.HandleIncomingComm(id.Round(roundID), tag)
	if err != nil {
		jww.FATAL.Panicf("[%s]: Error on reception of "+
			"FinishRealtime comm, should be able to return: \n %+v",
			instance, err)
	}
	p.Measure(measure.TagVerification)

	go func() {

		p.UpdateFinalStates()

		if !instance.GetKeepBuffers() {

			//Delete the round and its data from the manager
			//Delay so it can be used by post round hanlders
			go func() {
				jww.INFO.Printf("[%s]: RID %d ReceiveFinishRealtime CLEARING "+
					"CMIX BUFFERS", instance, roundID)

				time.Sleep(time.Duration(60) * time.Second)
				r.GetBuffer().Erase()
				rm.DeleteRound(roundID)
			}()

		} else {
			jww.WARN.Printf("[%s]: RID %d ReceiveFinishRealtime MEMORY "+
				"LEAK - Round buffers not purged ", instance,
				roundID)
		}
	}()

	jww.INFO.Printf("[%s]: RID %d ReceiveFinishRealtime END", instance,
		roundID)

	jww.INFO.Printf("[%s]: RID %d Round took %v seconds",
		instance, roundID, time.Now().Sub(r.GetTimeStart()))

	//Send batch to Gateway Polling Receiver on last node
	if r.GetTopology().IsLastNode(instance.GetID()) {

	}

	//Send the finished signal on first node
	if r.GetTopology().IsFirstNode(instance.GetID()) {
		jww.INFO.Printf("[%s]: RID %d FIRST NODE ReceiveFinishRealtime"+
			" SENDING END ROUND SIGNAL", instance, roundID)

		instance.FinishRound(roundID)

	}
	select {
	case r.GetMeasurementsReadyChan() <- struct{}{}:
		k
	default:
	}

	return nil
}

// ReceiveGetMeasure finds the round in msg and response with a RoundMetrics message
func ReceiveGetMeasure(instance *server.Instance, msg *mixmessages.RoundInfo) (*mixmessages.RoundMetrics, error) {
	roundID := id.Round(msg.ID)

	rm := instance.GetRoundManager()

	// Check that the round exists, grab it
	r, err := rm.GetRound(roundID)
	if err != nil {
		return nil, err
	}

	t := time.NewTimer(500 * time.Millisecond)
	c := r.GetMeasurementsReadyChan()
	select {
	case <-c:
	case <-t.C:
		return nil, errors.New("Timer expired, could not " +
			"receive measurement")
	}

	// Get data for metrics object
	nodeId := instance.GetID()
	topology := instance.GetTopology()
	index := topology.GetNodeLocation(nodeId)
	numNodes := topology.Len()
	resourceMonitor := instance.GetResourceMonitor()

	resourceMetric := measure.ResourceMetric{}

	if resourceMonitor != nil {
		resourceMetric = *resourceMonitor.Get()
	}

	metrics := r.GetMeasurements(nodeId.String(), numNodes, index, resourceMetric)

	s, err := json.Marshal(metrics)

	ret := mixmessages.RoundMetrics{
		RoundMetricJSON: string(s),
	}

	return &ret, nil
}

// ReceiveRoundTripPing handles incoming round trip pings, stopping the ping when back at the first node
func ReceiveRoundTripPing(instance *server.Instance, msg *mixmessages.RoundTripPing) error {
	roundID := msg.Round.ID
	rm := instance.GetRoundManager()
	r, err := rm.GetRound(id.Round(roundID))
	if err != nil {
		err = errors.Errorf("ReceiveRoundTripPing could not get round: %+v", err)
		return err
	}

	//jww.INFO.Printf("Recieved RoundTripPing, payload size: %v", len(msg.Payload.Value))

	topology := r.GetTopology()
	myID := instance.GetID()

	if topology.IsFirstNode(myID) {
		err = r.StopRoundTrip()
		if err != nil {
			err = errors.Errorf("ReceiveRoundTrip failed to stop round trip: %+v", err)
			jww.ERROR.Println(err.Error())
			return err
		}
		return nil
	}

	// Pull the particular server host object from the commManager
	nextNodeID := topology.GetNextNode(myID)
	nextNodeIndex := topology.GetNodeLocation(nextNodeID)
	nextNode := topology.GetHostAtIndex(nextNodeIndex)

	//Send the round trip ping to the next node
	_, err = instance.GetNetwork().RoundTripPing(nextNode, roundID, msg.Payload)
	if err != nil {
		err = errors.Errorf("ReceiveRoundTripPing failed to send ping to next node: %+v", err)
		return err
	}

	return nil
}

func shouldWait(p phase.Type) current.Activity {
	if p == phase.PrecompShare || p == phase.PrecompGeneration ||
		p == phase.PrecompDecrypt || p == phase.PrecompReveal ||
		p == phase.PrecompPermute {
		return current.PRECOMPUTING
	} else if p == phase.RealDecrypt || p == phase.RealPermute {
		return current.REALTIME
	} else {
		return current.ERROR
	}
}

// Handles incomming Poll gateway responses, compares our NDF with the existing ndf
func RecievePoll(poll *mixmessages.ServerPoll, instance *server.Instance) (*mixmessages.ServerPollResponse, error) {

	res := mixmessages.ServerPollResponse{}

	network := instance.GetConsensus()
	//Compare partial NDF hash with instance and return the new one if they do not match
	isSame := instance.GetConsensus().GetPartialNdf().CompareHash(poll.GetPartial().Hash)
	if !isSame {
		res.PartialNDF = network.GetPartialNdf().GetPb()
	}

	//Compare Full NDF hash with instance and return the new one if they do not match
	isSame = network.GetFullNdf().CompareHash(poll.GetFull().Hash)
	if !isSame {
		res.FullNDF = network.GetFullNdf().GetPb()
	}

	//Check if any updates where made and get them
	round, err := network.GetRoundUpdates(int(poll.LastUpdate))
	res.Updates = round
	if err != nil {
		return nil, err
	}

	// Get the request for a new batch que and store it into res
	res.BatchRequest, _ = instance.GetRequestNewBatchQueue().Receive()

	// Get a Batch message and store it into res
	cr := instance.GetCompletedBatchQueue().Recieve()
	if cr != nil {
		r, err := instance.GetRoundManager().GetRound(cr.RoundID)
		if err != nil {
			jww.ERROR.Printf("Recieved completed batch for round %v that doesn't exist: %s", cr.RoundID, err)
		} else {
			res.Slots = make([]*mixmessages.Slot, r.GetBatchSize())
			// wait for everything from the channel then put it into a slot and return it
			for chunk := range cr.Receiver {
				for c := chunk.Begin(); c < chunk.End(); c++ {
					res.Slots[c] = cr.GetMessage(c)
				}
			}
		}
	}

	return &res, nil
}
