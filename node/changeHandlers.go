////////////////////////////////////////////////////////////////////////////////
// Copyright © 2020 Privategrity Corporation                                   /
//                                                                             /
// All rights reserved.                                                        /
////////////////////////////////////////////////////////////////////////////////
package node

// ChangeHandlers contains the logic for every state within the state machine

import (
	"fmt"
	"github.com/golang/protobuf/proto"
	"github.com/pkg/errors"
	jww "github.com/spf13/jwalterweatherman"
	"gitlab.com/elixxir/comms/connect"
	"gitlab.com/elixxir/comms/mixmessages"
	"gitlab.com/elixxir/primitives/current"
	"gitlab.com/elixxir/primitives/id"
	"gitlab.com/elixxir/primitives/ndf"
	"gitlab.com/elixxir/server/node/receivers"
	"gitlab.com/elixxir/server/permissioning"
	"gitlab.com/elixxir/server/server"
	"gitlab.com/elixxir/server/server/phase"
	"gitlab.com/elixxir/server/server/round"
	"gitlab.com/elixxir/server/server/state"
	"io/ioutil"
	"strings"
	"time"
)

func Dummy(from current.Activity) error {
	return nil
}

// NotStarted is the beginning state of state machine. Enters waiting upon successful completion
func NotStarted(instance *server.Instance, noTls bool) error {
	// Start comms network
	ourDef := instance.GetDefinition()
	network := instance.GetNetwork()

	// Connect to the Permissioning Server without authentication
	permHost, err := network.AddHost(id.PERMISSIONING,
		// instance.GetPermissioningAddress,
		ourDef.Permissioning.Address, ourDef.Permissioning.TlsCert, true, false)
	if err != nil {
		return errors.Errorf("Unable to connect to registration server: %+v", err)
	}

	// Blocking call: Begin Node registration
	err = permissioning.RegisterNode(ourDef, network, permHost)
	if err != nil {
		return errors.Errorf("Failed to register node: %+v", err)
	}

	// Disconnect the old permissioning server to enable authentication
	permHost.Disconnect()

	// Connect to the Permissioning Server with authentication enabled
	// the server does not have a signed cert, but the pemrissionign has its cert,
	// reverse authetnication on conenctiosn just use the public key inside certs,
	// not the entire key chain, so even through the server does have a signed
	// cert, it can reverse auth with permissioning, allowing it to get the
	// full NDF
	permHost, err = network.AddHost(id.PERMISSIONING,
		ourDef.Permissioning.Address, ourDef.Permissioning.TlsCert, true, true)
	if err != nil {
		return errors.Errorf("Unable to connect to registration server: %+v", err)
	}

	// Retry polling until an ndf is returned
	err = errors.Errorf(ndf.NO_NDF)

	for err != nil && strings.Contains(err.Error(), ndf.NO_NDF) {
		var permResponse *mixmessages.PermissionPollResponse
		// Blocking call: Request ndf from permissioning
		permResponse, err = permissioning.PollPermissioning(permHost, instance, current.NOT_STARTED)
		if err == nil {
			err = permissioning.UpdateNDf(permResponse, instance)
		}
	}

	jww.DEBUG.Printf("Recieved ndf for first time!")
	if err != nil {
		return errors.Errorf("Failed to get ndf: %+v", err)
	}
	// Atomically denote that gateway is ready for polling
	instance.SetGatewayAsReady()

	// Receive signal that indicates that gateway is ready for polling
	err = instance.GetGatewayFirstTime().Receive(instance.GetGatewayConnnectionTimeout())
	if err != nil {
		return errors.Errorf("Unable to receive from gateway channel: %+v", err)
	}

	// Parse the Ndf for the new signed certs from permissioning
	serverCert, gwCert, err := permissioning.FindSelfInNdf(ourDef, instance.GetConsensus().GetFullNdf().Get())
	if err != nil {
		return errors.Errorf("Failed to install ndf: %+v", err)
	}

	// Restart the network with these signed certs
	err = instance.RestartNetwork(receivers.NewImplementation, noTls, serverCert, gwCert)
	if err != nil {
		return errors.Errorf("Unable to restart network with new certificates: %+v", err)
	}

	// HACK HACK HACK
	// FIXME: we should not be coupling connections and server objects
	// Technically the servers can fail to bind for up to
	// a couple minutes (depending on operating system), but
	// in practice 10 seconds works
	time.Sleep(10 * time.Second)

	// Once done with notStarted transition into waiting
	go func() {
		// Ensure that instance is in not started prior to transition
		cur, err := instance.GetStateMachine().WaitFor(1*time.Second, current.NOT_STARTED)
		if cur != current.NOT_STARTED || err != nil {
			roundErr := errors.Errorf("Server never transitioned to %v state: %+v", current.NOT_STARTED, err)
			instance.ReportRoundFailure(roundErr)
		}

		// if error passed in go to error
		if instance.GetRecoveredError() != nil {
			ok, err := instance.GetStateMachine().Update(current.ERROR)
			if !ok || err != nil {
				roundErr := errors.Errorf("Unable to transition to %v state: %+v", current.ERROR, err)
				instance.ReportRoundFailure(roundErr)
			}
		} else {
			// Transition state machine into waiting state
			ok, err := instance.GetStateMachine().Update(current.WAITING)
			if !ok || err != nil {
				roundErr := errors.Errorf("Unable to transition to %v state: %+v", current.WAITING, err)
				instance.ReportRoundFailure(roundErr)
			}
		}

		// Periodically re-poll permissioning
		// fixme we need to review the performance implications and possibly make this programmable
		ticker := time.NewTicker(5 * time.Millisecond)
		for range ticker.C {
			err := permissioning.Poll(instance)
			if err != nil {
				// If we receive an error polling here, panic this thread
				roundErr := errors.Errorf("Received error polling for permisioning: %+v", err)
				instance.ReportRoundFailure(roundErr)
			}
		}
	}()

	return nil
}

// fixme: doc string
func Waiting(from current.Activity) error {
	// start waiting process
	return nil
}

// Precomputing does various business logic to prep for the start of a new round
func Precomputing(instance *server.Instance, newRoundTimeout time.Duration) error {
	// Add round.queue to instance, get that here and use it to get new round
	// start pre-precomputation
	roundInfo, err := instance.GetCreateRoundQueue().Receive()
	if err != nil {
		jww.TRACE.Printf("Error with create round queue: %+v", err)
	}

	roundID := roundInfo.GetRoundId()
	topology := roundInfo.GetTopology()
	// Extract topology from RoundInfo
	nodeIDs, err := id.NewNodeListFromStrings(topology)
	if err != nil {
		return errors.Errorf("Unable to convert topology into a node list: %+v", err)
	}

	// fixme: this panics on error, external comm should not be able to crash server
	circuit := connect.NewCircuit(nodeIDs)

	for i := 0; i < circuit.Len(); i++ {
		nodeId := circuit.GetNodeAtIndex(i).String()
		ourHost, ok := instance.GetNetwork().GetHost(nodeId)
		if !ok {
			return errors.Errorf("Host not available for node %s in round", circuit.GetNodeAtIndex(i))
		}
		circuit.AddHost(ourHost)
	}

	//Build the components of the round
	phases, phaseResponses := NewRoundComponents(
		instance.GetGraphGenerator(),
		circuit,
		instance.GetID(),
		instance,
		roundInfo.GetBatchSize(),
		newRoundTimeout, nil)

	phaseOverrides := instance.GetPhaseOverrides()
	for toOverride, override := range phaseOverrides {
		phases[toOverride] = override
	}

	//Build the round
	rnd, err := round.New(
		instance.GetConsensus().GetCmixGroup(),
		instance.GetUserRegistry(),
		roundID, phases, phaseResponses,
		circuit,
		instance.GetID(),
		roundInfo.GetBatchSize(),
		instance.GetRngStreamGen(),
		nil,
		instance.GetIP())
	if err != nil {
		return errors.WithMessage(err, "Failed to create new round")
	}

	//Add the round to the manager
	instance.GetRoundManager().AddRound(rnd)
	jww.INFO.Printf("[%+v]: RID %d CreateNewRound COMPLETE", instance,
		roundID)

	if circuit.IsFirstNode(instance.GetID()) {
		err := StartLocalPrecomp(instance, roundID)
		if err != nil {
			return errors.WithMessage(err, "Failed to TransmitCreateNewRound")
		}
	}

	return nil
}

// fixme: doc string
func Standby(from current.Activity) error {
	// start standby process
	return nil

}

// Realtime checks if we are in the correct phase
func Realtime(instance *server.Instance) error {
	// Get new realtime round info from queue
	roundInfo, err := instance.GetRealtimeRoundQueue().Receive()
	if err != nil {
		return errors.Errorf("Unable to receive from RealtimeRoundQueue: %+v", err)
	}

	// Get our round
	ourRound, err := instance.GetRoundManager().GetRound(roundInfo.GetRoundId())
	if err != nil {
		return errors.Errorf("Unable to get round from round info: %+v", err)
	}

	// Check for correct phase in round
	if ourRound.GetCurrentPhase().GetType() != phase.RealDecrypt {
		return errors.Errorf("Not in correct phase. Expected phase: %+v. "+
			"Current phase: %+v", phase.RealDecrypt, ourRound.GetCurrentPhase())
	}

	if ourRound.GetTopology().IsFirstNode(instance.GetID()) {
		err = instance.GetRequestNewBatchQueue().Send(roundInfo)
		if err != nil {
			return errors.Errorf("Unable to send to RequestNewBatch queue: %+v", err)
		}
	}

	return nil
}

// fixme: doc string
func Completed(from current.Activity) error {
	// start completed
	return nil
}

// fixme: doc string
func Error(instance *server.Instance) error {
	// start error
	//If the error state was recovered from a restart, exit.
	if instance.GetRecoveredError() != nil {
		return nil
	}

	// Check for error message on server instance
	msg := instance.GetRoundError()
	if msg == nil {
		jww.FATAL.Panic("No error found on instance")
	}

	nid, err := id.NewNodeFromString(msg.NodeId)
	if err != nil {
		return errors.WithMessage(err, "Failed to get node id from error")
	}

	// If the error originated with us, send broadcast to other nodes
	if nid.Cmp(instance.GetID()) {
		r, err := instance.GetRoundManager().GetRound(id.Round(msg.Id))
		if err != nil {
			return errors.WithMessage(err, "Failed to get round id")
		}
		top := r.GetTopology()
		for i := 0; i < top.Len(); i++ {
			n := top.GetNodeAtIndex(i)
			// Don't need to send back to self
			if !instance.GetID().Cmp(n) {
				h, ok := instance.GetNetwork().GetHost(n.String())
				if !ok {
					jww.ERROR.Printf("Could not get host for node %s", n.String())
				}

				_, err := instance.SendRoundError(h, msg)
				if err != nil {
					err := errors.WithMessagef(err, "Failed to send error to node %s", n.String())
					jww.ERROR.Printf(err.Error())
				}
			}
		}
	}

	b, err := proto.Marshal(msg)
	if err != nil {
		return errors.WithMessage(err, "Failed to marshal message into bytes")
	}

	err = ioutil.WriteFile(instance.RecoveredErrorFilePath, b, 0644)
	if err != nil {
		return errors.WithMessage(err, "Failed to write error to file")
	}

	err = instance.GetResourceQueue().Kill(time.Second)
	if err != nil {
		return errors.WithMessage(err, "Resource queue kill timed out")
	}

	instance.GetPanicWrapper()(fmt.Sprintf("Error encountered - closing server & writing error to file %s",
		instance.RecoveredErrorFilePath))
	return nil
}

// fixme: doc string
func Crash(from current.Activity) error {
	// start error
	return nil
}

// NewStateChanges creates a state table with dummy functions
func NewStateChanges() [current.NUM_STATES]state.Change {
	// Create the state change function table
	var stateChanges [current.NUM_STATES]state.Change

	stateChanges[current.NOT_STARTED] = Dummy
	stateChanges[current.WAITING] = Dummy
	stateChanges[current.PRECOMPUTING] = Dummy
	stateChanges[current.STANDBY] = Dummy
	stateChanges[current.REALTIME] = Dummy
	stateChanges[current.COMPLETED] = Dummy
	stateChanges[current.ERROR] = Dummy
	stateChanges[current.CRASH] = Dummy

	return stateChanges
}
