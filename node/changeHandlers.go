///////////////////////////////////////////////////////////////////////////////
// Copyright © 2020 xx network SEZC                                          //
//                                                                           //
// Use of this source code is governed by a license that can be found in the //
// LICENSE file                                                              //
///////////////////////////////////////////////////////////////////////////////

package node

// ChangeHandlers contains the logic for every state within the state machine

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"github.com/golang/protobuf/proto"
	"github.com/pkg/errors"
	jww "github.com/spf13/jwalterweatherman"
	"gitlab.com/elixxir/comms/mixmessages"
	"gitlab.com/elixxir/crypto/cyclic"
	"gitlab.com/elixxir/primitives/current"
	"gitlab.com/elixxir/server/internal"
	"gitlab.com/elixxir/server/internal/phase"
	"gitlab.com/elixxir/server/internal/round"
	"gitlab.com/elixxir/server/internal/state"
	"gitlab.com/elixxir/server/permissioning"
	"gitlab.com/elixxir/server/storage"
	"gitlab.com/xx_network/comms/connect"
	"gitlab.com/xx_network/primitives/id"
	"gitlab.com/xx_network/primitives/ndf"
	"gitlab.com/xx_network/primitives/utils"
	"strconv"
	"strings"
	"time"
)

// Number of hard-coded users to create
var numDemoUsers = int(256)

func Dummy(from current.Activity) error {
	return nil
}

// NotStarted is the beginning state of state machine. Enters waiting upon successful completion
func NotStarted(instance *internal.Instance) error {
	// Start comms network
	ourDef := instance.GetDefinition()
	network := instance.GetNetwork()

	jww.INFO.Printf("Loading certificates from disk")
	// Get the Server and Gateway certificates from file, if they exist

	// Connect to the Permissioning Server without authentication
	params := connect.GetDefaultHostParams()
	params.AuthEnabled = false
	permHost, err := network.AddHost(&id.Permissioning,
		// instance.GetPermissioningAddress,
		ourDef.Permissioning.Address,
		ourDef.Permissioning.TlsCert,
		params)

	if err != nil {
		return errors.Errorf("Unable to connect to registration server: %+v", err)
	}

	// Determine if the node has registered already
	isRegistered := isRegistered(instance, permHost)

	// If the certificates were retrieved from file, so do not need to register
	if !isRegistered {
		instance.IsFirstRun()

		// Blocking call which waits until gateway
		// has first contacted its node
		// This ensures we have the correct gateway information
		jww.INFO.Printf("Waiting on contact from gateway...")
		instance.GetGatewayFirstContact().Receive()

		// Blocking call: begin Node registration
		jww.INFO.Printf("Registering with permissioning...")
		err = permissioning.RegisterNode(ourDef, instance, permHost)
		if err != nil {
			if strings.Contains(err.Error(), "Node with registration code") && strings.Contains(err.Error(), "has already been registered") {
				jww.FATAL.Panic("Node is already registered, Attempting re-registration is NOT secure")
			} else {
				return errors.Errorf("Failed to register node: %+v", err)
			}

		}
		jww.INFO.Printf("Node has registered with permissioning, waiting for network to continue")
	}

	// Disconnect the old Permissioning server to enable authentication
	permHost.Disconnect()
	network.RemoveHost(permHost.GetId())

	// Connect to the Permissioning Server with authentication enabled
	// the server does not have a signed cert, but the pemrissioning has its cert,
	// reverse authentication on connections just use the public key inside certs,
	// not the entire key chain, so even through the server does have a signed
	// cert, it can reverse auth with permissioning, allowing it to get the
	// full NDF
	// do this even if you have the certs to ensure the permissioning server is
	// ready for servers to connect to it
	params = connect.GetDefaultHostParams()
	params.MaxRetries = 0
	permHost, err = network.AddHost(&id.Permissioning,
		ourDef.Permissioning.Address, ourDef.Permissioning.TlsCert, params)
	if err != nil {
		return errors.Errorf("Unable to connect to registration server: %+v", err)
	}

	// Retry polling until an ndf is returned
	err = errors.Errorf(ndf.NO_NDF)
	// String to look for the check for a reverse contact error.
	// not panicking on these errors allows for better debugging
	cannotPingErr := "cannot be contacted"
	permissioningShuttingDownError := "transport is closing"

	pollDelay := 1 * time.Second

	for err != nil {
		var permResponse *mixmessages.PermissionPollResponse
		// Blocking call: Request ndf from permissioning
		permResponse, err = permissioning.PollPermissioning(permHost, instance, current.NOT_STARTED)
		if err == nil {
			//check if an NDF is returned
			if permResponse == nil || permResponse.FullNDF == nil || len(permResponse.FullNDF.Ndf) == 0 {
				err = errors.New("The NDF was not returned, " +
					"'permissioning is likely in the process of vetting the " +
					"node")
			} else {
				//update NDF
				err = permissioning.UpdateNDf(permResponse, instance)
				// find certs in NDF in order to detect that permissioning views
				// this server as online
				if err == nil && !permissioning.FindSelfInNdf(ourDef,
					instance.GetConsensus().GetFullNdf().Get()) {
					err = errors.New("Waiting to be included in the " +
						"network")
				}
			}
		}

		//if there is an error, print it
		if err != nil {
			jww.WARN.Printf("Poll of permissioning failed, will "+
				"try again in %s: %s", pollDelay, err)
		}
		//sleep in order to not overwhelm permissioning
		time.Sleep(pollDelay)
	}

	// Then we ping ourselfs to make sure we can communicate
	host, exists := instance.GetNetwork().GetHost(instance.GetID())
	if exists && host.IsOnline() {
		jww.DEBUG.Printf("Successfully contacted local address!")
	} else if exists {
		return errors.Errorf("unable to contact local address: %s",
			host.GetAddress())
	} else {
		return errors.Errorf("unable to find host to try contacting " +
			"the local address")
	}

	//init the database
	cmixGrp := instance.GetConsensus().GetCmixGroup()

	userDatabase := instance.GetStorage()
	if instance.GetDefinition().DevMode {
		//populate the dummy precanned users
		jww.INFO.Printf("Adding dummy users to registry")
		PopulateDummyUsers(userDatabase, cmixGrp)
	}

	jww.INFO.Printf("Adding dummy gateway sending user")
	//Add a dummy user for gateway
	dummy := &storage.Client{
		Id:           id.DummyUser.Marshal(),
		DhKey:        cmixGrp.NewIntFromBytes(id.DummyUser.Marshal()[:]).Bytes(),
		IsRegistered: true,
	}
	err = instance.GetStorage().UpsertClient(dummy)
	if err != nil {
		return err
	}

	jww.INFO.Printf("Waiting on communication from gateway to continue")

	// Atomically denote that gateway is ready for polling
	instance.SetGatewayAsReady()

	// Wait for signal that indicates that gateway is ready for polling to
	// continue in order to ensure the gateway is online
	instance.GetGatewayFirstPoll().Receive()

	jww.INFO.Printf("Communication from gateway received")

	// Once done with notStarted transition into waiting
	go func() {
		// Ensure that instance is in not started prior to transition
		cur, err := instance.GetStateMachine().WaitFor(1*time.Second, current.NOT_STARTED)
		if cur != current.NOT_STARTED || err != nil {
			roundErr := errors.Errorf("Server never transitioned to %v state: %+v", current.NOT_STARTED, err)
			instance.ReportNodeFailure(roundErr)
		}

		// if error passed in go to error
		if instance.GetRecoveredError() != nil {
			ok, err := instance.GetStateMachine().Update(current.ERROR)
			if !ok || err != nil {
				roundErr := errors.Errorf("Unable to transition to %v state: %+v", current.ERROR, err)
				instance.ReportNodeFailure(roundErr)
			}
		} else {
			// Transition state machine into waiting state
			ok, err := instance.GetStateMachine().Update(current.WAITING)
			if !ok || err != nil {
				roundErr := errors.Errorf("Unable to transition to %v state: %+v", current.WAITING, err)
				instance.ReportNodeFailure(roundErr)
			}
		}

		// Periodically re-poll permissioning
		// fixme we need to review the performance implications and possibly make this programmable
		ticker := time.NewTicker(200 * time.Millisecond)
		for range ticker.C {
			err := permissioning.Poll(instance)
			if err != nil {
				// do not error if the poll failed due to contact issues,
				// this allows for better debugging
				if strings.Contains(err.Error(), cannotPingErr) ||
					strings.Contains(err.Error(), permissioningShuttingDownError) {

					jww.ERROR.Printf("Your node is not online: %s", err.Error())
					time.Sleep(pollDelay)
				} else {
					// If we receive an error polling here, panic this thread
					roundErr := errors.Errorf("Received error polling for permisioning: %+v", err)
					instance.ReportNodeFailure(roundErr)
				}

			}
		}
	}()

	return nil
}

func Waiting(from current.Activity) error {
	// start waiting process
	return nil
}

// Precomputing does various business logic to prep for the start of a new round
func Precomputing(instance *internal.Instance) error {
	// Add round.queue to instance, get that here and use it to get new round
	// start pre-precomputation
	roundInfo, err := instance.GetCreateRoundQueue().Receive()
	if err != nil {
		jww.TRACE.Printf("Error with create round queue: %+v", err)
	}

	roundID := roundInfo.GetRoundId()
	roundTimeout := time.Duration(roundInfo.ResourceQueueTimeoutMillis) * time.Millisecond
	topology := roundInfo.GetTopology()
	// Extract topology from RoundInfo
	nodeIDs, err := id.NewIDListFromBytes(topology)
	if err != nil {
		return errors.Errorf("Unable to convert topology into a node list: %+v", err)
	}

	// fixme: this panics on error, external comm should not be able to crash server
	circuit := connect.NewCircuit(nodeIDs)

	for i := 0; i < circuit.Len(); i++ {
		nodeId := circuit.GetNodeAtIndex(i)
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
		roundTimeout, instance.GetStreamPool(),
		instance.GetDisableStreaming(),
		roundID)

	var override = func() {
		phaseOverrides := instance.GetPhaseOverrides()
		for toOverride, or := range phaseOverrides {
			phases[toOverride] = or
		}
	}
	if instance.GetOverrideRound() != -1 {
		if instance.GetOverrideRound() == int(roundID) {
			override()
		}
	} else {
		override()
	}

	//Build the round
	rnd, err := round.New(
		instance.GetConsensus().GetCmixGroup(),
		instance.GetStorage(),
		roundID, phases, phaseResponses,
		circuit,
		instance.GetID(),
		roundInfo.GetBatchSize(),
		instance.GetRngStreamGen(),
		instance.GetStreamPool(),
		instance.GetIP(),
		GetDefaultPanicHandler(instance, roundID),
		instance.GetClientReport())
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

func Standby(from current.Activity) error {
	// start standby process
	return nil

}

// Realtime checks if we are in the correct phase
func Realtime(instance *internal.Instance) error {
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

func Completed(from current.Activity) error {
	// start completed
	return nil
}

func Error(instance *internal.Instance) error {
	//If the error state was recovered from a restart, exit.
	if instance.GetRecoveredErrorUnsafe() != nil {
		return nil
	}

	// Check for error message on server instance
	msg := instance.GetRoundError()
	if msg == nil {
		jww.FATAL.Panic("No error found on instance")
	}
	/*
		nid, err := id.Unmarshal(msg.NodeId)
		if err != nil {
			return errors.WithMessage(err, "Failed to get node id from error")
		}

		wg := sync.WaitGroup{}
		numResponces := uint32(0)
		// If the error originated with us, send broadcast to other nodes
		if nid.Cmp(instance.GetID()) && msg.Id != 0 {
			r, err := instance.GetRoundManager().GetRound(id.Round(msg.Id))
			if err != nil {
				return errors.WithMessage(err, "Failed to get round id")
			}
			top := r.GetTopology()
			for i := 0; i < top.Len(); i++ {
				// Send to all nodes except self
				n := top.GetNodeAtIndex(i)
				if !instance.GetID().Cmp(n) {
					wg.Add(1)
					go func() {
						h, ok := instance.GetNetwork().GetHost(n)
						if !ok {
							jww.ERROR.Printf("Could not get host for node %s", n.String())
						}

						_, err := instance.SendRoundError(h, msg)
						if err != nil {
							err = errors.WithMessagef(err, "Failed to send error to node %s", n.String())
							jww.ERROR.Printf(err.Error())
						}
						atomic.AddUint32(&numResponces,1)
						wg.Done()
					}()
				}
			}

			// Wait until the error messages are sent, or timeout after 3 minutes
			notifyTeamMembers := make(chan struct{})
			notifyTimeout := 15 * time.Second
			timeout := time.NewTimer(notifyTimeout)

			go func() {
				wg.Wait()
				notifyTeamMembers <- struct{}{}
			}()

			select {
			case <-notifyTeamMembers:
			case <-timeout.C:
				jww.ERROR.Printf("Only %v/%v team members responded to the "+
					"error broadcast, timed out after %s",
					atomic.LoadUint32(&numResponces), top.Len(), notifyTimeout)
			}
		}
	*/
	b, err := proto.Marshal(msg)
	if err != nil {
		return errors.WithMessage(err, "Failed to marshal message into bytes")
	}

	bEncoded := base64.StdEncoding.EncodeToString(b)

	err = utils.WriteFile(instance.GetDefinition().RecoveredErrorPath, []byte(bEncoded), 0644, 0644)
	if err != nil {
		return errors.WithMessage(err, "Failed to write error to file")
	}

	err = instance.GetResourceQueue().Kill(5 * time.Second)
	if err != nil {
		return errors.WithMessage(err, "Resource queue kill timed out")
	}

	instance.GetPanicWrapper()(fmt.Sprintf(
		"Error encountered - closing server & writing error to %s: %s",
		instance.GetDefinition().RecoveredErrorPath, msg.Error))
	return nil
}

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

// Pings permissioning server to see if our registration code has already been registered
func isRegistered(serverInstance *internal.Instance, permHost *connect.Host) bool {

	// Request a client ndf from the permissioning server
	response, err := serverInstance.GetNetwork().SendRegistrationCheck(permHost,
		&mixmessages.RegisteredNodeCheck{
			ID: serverInstance.GetID().Bytes(),
		})
	if err != nil {
		jww.WARN.Printf("Error returned from Registration when node is looked up: %s", err.Error())
		return false
	}

	return response.IsRegistered

}

// PopulateDummyUsers creates dummy users to be manually inserted into the database
func PopulateDummyUsers(ur *storage.Storage, grp *cyclic.Group) {
	// Deterministically create named users for demo
	for i := 1; i < numDemoUsers; i++ {
		h := sha256.New()
		h.Reset()
		h.Write([]byte(strconv.Itoa(4000 + i)))
		usrID := new(id.ID)
		binary.BigEndian.PutUint64(usrID[:], uint64(i))
		usrID.SetType(id.User)

		// Generate user parameters
		ur.UpsertClient(&storage.Client{
			Id:           usrID.Marshal(),
			DhKey:        grp.NewIntFromBytes(h.Sum(nil)).Bytes(),
			IsRegistered: true,
		})
	}
	return
}
