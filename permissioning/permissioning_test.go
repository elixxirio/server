////////////////////////////////////////////////////////////////////////////////
// Copyright © 2018 Privategrity Corporation                                   /
//                                                                             /
// All rights reserved.                                                        /
////////////////////////////////////////////////////////////////////////////////

package permissioning

import (
	"bytes"
	"fmt"
	pb "gitlab.com/elixxir/comms/mixmessages"
	"gitlab.com/elixxir/comms/node"
	"gitlab.com/elixxir/comms/testkeys"
	"gitlab.com/elixxir/primitives/current"
	"gitlab.com/elixxir/primitives/id"
	"gitlab.com/elixxir/primitives/states"
	"gitlab.com/elixxir/primitives/utils"
	"gitlab.com/elixxir/server/node/receivers"
	"gitlab.com/elixxir/server/server"
	"gitlab.com/elixxir/server/server/state"
	"gitlab.com/elixxir/server/services"
	"gitlab.com/elixxir/server/testUtil"
	"math/rand"
	"reflect"
	"testing"
	"time"
)

//// Full-stack happy path test for the node registration logic
func TestRegisterNode(t *testing.T) {

	cert, _ := utils.ReadFile(testkeys.GetNodeCertPath())
	key, _ := utils.ReadFile(testkeys.GetNodeKeyPath())

	// Set up id's and address
	nodeId = id.NewNodeFromUInt(uint64(0), t)
	nodeAddr := fmt.Sprintf("0.0.0.0:%d", 7000+rand.Intn(1000)+cnt)
	pAddr = fmt.Sprintf("0.0.0.0:%d", 2000+rand.Intn(1000))

	cnt++

	gAddr := fmt.Sprintf("0.0.0.0:%d", 4000+rand.Intn(1000)+cnt)

	// Build the node
	emptyNdf := builEmptydMockNdf()

	// Initialize definition
	def := &server.Definition{
		Flags:         server.Flags{},
		ID:            nodeId,
		PublicKey:     nil,
		PrivateKey:    nil,
		TlsCert:       cert,
		TlsKey:        key,
		Address:       nodeAddr,
		LogPath:       "",
		MetricLogPath: "",
		Gateway: server.GW{
			ID:      nodeId.NewGateway(),
			Address: gAddr,
			TlsCert: cert,
		},

		UserRegistry: nil,
		Permissioning: server.Perm{
			TlsCert:          []byte(testUtil.RegCert),
			Address:          pAddr,
			RegistrationCode: "",
		},

		GraphGenerator:  services.GraphGenerator{},
		ResourceMonitor: nil,
		FullNDF:         emptyNdf,
		PartialNDF:      emptyNdf,
	}

	// Create state machine
	sm := state.NewMachine(dummyStates)
	ok, err := sm.Update(current.WAITING)
	if !ok || err != nil {
		t.Errorf("Failed to prep state machine: %+v", err)
	}

	// Add handler for instance
	impl := func(i *server.Instance) *node.Implementation {
		return receivers.NewImplementation(i)
	}

	// Generate instance
	instance, err := server.CreateServerInstance(def, impl, sm, true)
	if err != nil {
		t.Errorf("Unable to create instance: %+v", err)
	}

	// Add permissioning as a host
	_, err = instance.GetNetwork().AddHost(id.PERMISSIONING, def.Permissioning.Address,
		def.Permissioning.TlsCert, false, false)
	if err != nil {
		t.Errorf("Failed to add permissioning host: %+v", err)
	}

	// Start up permissioning server
	permComms, err := startPermisioning()
	if err != nil {
		t.Errorf("Couldn't create permissioning server: %+v", err)
	}
	defer permComms.Shutdown()

	// Fetch permissioning host
	permHost, ok := instance.GetNetwork().GetHost(id.PERMISSIONING)
	if !ok {
		t.Errorf("Could not get permissioning host. Was it added?")
	}

	// Register node with permissioning
	err = RegisterNode(def, instance.GetNetwork(), permHost)
	if err != nil {
		t.Errorf("Failed to register node: %+v", err)
	}

}

// Happy path: Test polling
func TestPoll(t *testing.T) {
	// Create instance
	instance, err := createServerInstance(t)
	if err != nil {
		t.Errorf("Couldn't create instance: %+v", err)
	}

	// Start up permissioning server
	permComms, err := startPermisioning()
	if err != nil {
		t.Errorf("Couldn't create permissioning server: %+v", err)
	}
	defer permComms.Shutdown()

	// Poll the permissioning server for updates
	err = Poll(instance)
	if err != nil {
		t.Errorf("Failed to poll for ndf: %+v", err)
	}

	// Fetch the full ndf
	receivedFullNdf, err := instance.GetConsensus().GetFullNdf().Get().Marshal()
	if err != nil {
		t.Errorf("Failed to marshall full ndf: %+v", err)
	}

	// Fetch the partial ndf
	receivedPartialNdf, err := instance.GetConsensus().GetPartialNdf().Get().Marshal()
	if err != nil {
		t.Errorf("Failed to marshall partial ndf: %+v", err)
	}

	// Take the expected partial and full ndf
	expectedFullNdf, _ := testUtil.NDF.Marshal()
	expectedPartialNdf, _ := testUtil.NDF.StripNdf().Marshal()

	if !reflect.DeepEqual(receivedFullNdf, expectedFullNdf) {
		t.Errorf("Failed to build ndf in instance!"+
			"\n\tExpected: %+v"+
			"\n\n\n\tReceived: %+v", string(expectedFullNdf), string(receivedFullNdf))
	}

	// Check the partial ndf
	if !bytes.Equal(receivedPartialNdf, expectedPartialNdf) {
		t.Errorf("Failed to poll ndf correctly."+
			"\n\tExpected: %+v"+
			"\n\tReceived: %+v", string(expectedPartialNdf), string(receivedPartialNdf))
	}

	if instance.GetStateMachine().Get().String() != current.WAITING.String() {
		t.Errorf("In unexpected state after polling!"+
			"\n\tExpected: %+v"+
			"\n\tReceived: %+v", current.WAITING, instance.GetStateMachine().Get())
	}

}

// Happy path: Pings the mock registration server for a poll response
func TestRetrieveState(t *testing.T) {
	// Create server instance
	instance, err := createServerInstance(t)
	if err != nil {
		t.Errorf("Couldn't create instance: %+v", err)
	}
	defer instance.GetNetwork().Shutdown()

	// Create permissioning server
	permComms, err := startPermisioning()
	if err != nil {
		t.Errorf("Couldn't create permissioning server")
	}
	defer permComms.Shutdown()

	// Add retrieve permissioning host from instance
	permHost, _ := instance.GetNetwork().GetHost(id.PERMISSIONING)

	// Ping permissioning for a state update
	response, err := RetrieveState(permHost, instance)
	if err != nil {
		t.Errorf("Failed to poll for ndf: %+v", err)
	}

	// Pull the partial and full from the ndf
	partialNdfResponse := response.PartialNDF.Ndf
	fullNdfResponse := response.FullNDF.Ndf

	// Take the expected partial and full ndf
	expectedPartialNdf, _ := testUtil.NDF.StripNdf().Marshal()
	expectedFullNdf, _ := testUtil.NDF.Marshal()

	// Check the partial ndf
	if !bytes.Equal(partialNdfResponse, expectedPartialNdf) {
		t.Errorf("Failed to poll ndf correctly."+
			"\n\tExpected: %+v"+
			"\n\tReceived: %+v", string(expectedPartialNdf), string(partialNdfResponse))
	}

	// Check the full ndf
	if !bytes.Equal(fullNdfResponse, expectedFullNdf) {
		t.Errorf("Failed to poll ndf correctly."+
			"\n\tExpected: %+v"+
			"\n\tReceived: %+v", string(expectedFullNdf), string(fullNdfResponse))
	}
}

// Happy path: Transfer from not started to precomputing, then from standby to realtime
func TestUpdateInternalState(t *testing.T) {
	numUpdates := uint64(0)

	// Create server instance
	instance, err := createServerInstance(t)
	if err != nil {
		t.Errorf("Couldn't create instance: %+v", err)
	}

	// Create a topology for round info
	nodeOne := id.NewNodeFromUInt(uint64(0), t).String()
	nodeTwo := id.NewNodeFromUInt(uint64(1), t).String()
	nodeThree := id.NewNodeFromUInt(uint64(2), t).String()
	ourTopology := []string{nodeOne, nodeTwo, nodeThree}

	// Construct round info message
	precompRoundInfo := &pb.RoundInfo{
		ID:       0,
		UpdateID: numUpdates,
		State:    uint32(states.PRECOMPUTING),
		Topology: ourTopology,
	}

	// Increment updates id for next message
	numUpdates++

	// Set the signature field of the round info
	signRoundInfo(precompRoundInfo)

	// Set up the ndf's
	fullNdf, _ := setupFullNdf()
	stripNdf, _ := setupPartialNdf()

	// ------------------- TRANSFER FROM WAITING TO PRECOMP ---------------------------------------

	// Construct permissioning poll response
	mockPollResponse := &pb.PermissionPollResponse{
		FullNDF:    fullNdf,
		PartialNDF: stripNdf,
		Updates:    []*pb.RoundInfo{precompRoundInfo},
	}

	// Update internal state with mock response
	err = UpdateInternalState(mockPollResponse, instance)
	if err != nil {
		t.Errorf("Failed to update internal state: %+v", err)
	}

	// Fetch the instance's full ndf
	receivedFullNdf, err := instance.GetConsensus().GetFullNdf().Get().Marshal()
	if err != nil {
		t.Errorf("Failed to marshal internal full ndf: %+v", err)
	}

	// Check that full ndf was properly updated
	if !reflect.DeepEqual(receivedFullNdf, fullNdf.Ndf) {
		t.Errorf("Full ndf mismatch after updating internal state."+
			"\n\tExpected: %+v"+
			"\n\tReceived: %+v", fullNdf.Ndf, receivedFullNdf)
	}

	// Fetch the instance's partial ndf
	receivedPartialNdf, err := instance.GetConsensus().GetPartialNdf().Get().Marshal()
	if err != nil {
		t.Errorf("Failed to marshal internal full ndf: %+v", err)
	}

	// Check that partial ndf was properly updated
	if !reflect.DeepEqual(receivedPartialNdf, stripNdf.Ndf) {
		t.Errorf("Full ndf mismatch after updating internal state."+
			"\n\tExpected: %+v"+
			"\n\tReceived: %+v", stripNdf.Ndf, receivedPartialNdf)
	}

	// Check that the state was changed
	if instance.GetStateMachine().Get() != current.PRECOMPUTING {
		t.Errorf("Unexpected state after updating internally. "+
			"\n\tExpected state: %+v"+
			"\n\tReceived state: %+v", current.PRECOMPUTING, instance.GetStateMachine().Get())
	}

	// ----------------------- TRANSFER FROM STANDBY TO REALTIME ---------------------------------------

	ok, err := instance.GetStateMachine().Update(current.STANDBY)
	if !ok || err != nil {
		t.Errorf("Failed to transition to standby state: %+v", err)
	}

	// Create a time stamp in which to transfer stats
	ourTime := time.Now().Add(500 * time.Millisecond).UnixNano()
	timestamps := make([]uint64, states.FAILED)
	timestamps[states.REALTIME] = uint64(ourTime)

	// Construct round info message
	realtimeRoundInfo := &pb.RoundInfo{
		ID:         0,
		UpdateID:   numUpdates,
		State:      uint32(states.REALTIME),
		Topology:   ourTopology,
		Timestamps: timestamps,
	}

	// Increment updates id for next message
	numUpdates++

	// Set the signature field of the round info
	signRoundInfo(realtimeRoundInfo)

	// Construct permissioning poll response
	mockPollResponse = &pb.PermissionPollResponse{
		FullNDF:    fullNdf,
		PartialNDF: stripNdf,
		Updates:    []*pb.RoundInfo{realtimeRoundInfo},
	}

	// Update internal state with mock response
	err = UpdateInternalState(mockPollResponse, instance)
	if err != nil {
		t.Errorf("Failed to update internal state: %+v", err)
	}

	// Wait for the WaitForRealtime go routine to update the state
	time.Sleep(500 * time.Millisecond)

	// Check that the state was changed
	if instance.GetStateMachine().Get() != current.REALTIME {
		t.Errorf("Unexpected state after updating internally. "+
			"\n\tExpected state: %+v"+
			"\n\tReceived state: %+v", current.REALTIME, instance.GetStateMachine().Get())
	}

}

// Smoke tests the state transitions that contain no actual logic
func TestUpdateInternalState_Smoke(t *testing.T) {
	numUpdates := uint64(0)

	// Create server instance
	instance, err := createServerInstance(t)
	if err != nil {
		t.Errorf("Couldn't create instance: %+v", err)
	}

	// Create a topology for round info
	nodeOne := id.NewNodeFromUInt(uint64(0), t).String()
	nodeTwo := id.NewNodeFromUInt(uint64(1), t).String()
	nodeThree := id.NewNodeFromUInt(uint64(2), t).String()
	ourTopology := []string{nodeOne, nodeTwo, nodeThree}

	// ------------------------------- PENDING TEST ------------------------------------------------------------
	pendingRoundInfo := &pb.RoundInfo{
		ID:       0,
		UpdateID: numUpdates,
		State:    uint32(states.PENDING),
		Topology: ourTopology,
	}

	// Increment updates id for next message
	numUpdates++

	// Set the signature field of the round info
	signRoundInfo(pendingRoundInfo)

	// Set up the ndf's
	fullNdf, _ := setupFullNdf()
	stripNdf, _ := setupPartialNdf()

	// Construct permissioning poll response
	mockPollResponse := &pb.PermissionPollResponse{
		FullNDF:    fullNdf,
		PartialNDF: stripNdf,
		Updates:    []*pb.RoundInfo{pendingRoundInfo},
	}

	// Update internal state with mock response
	err = UpdateInternalState(mockPollResponse, instance)
	if err != nil {
		t.Errorf("Failed to update internal state: %+v", err)
	}

	// ------------------------------- STANDBY TESTING ------------------------------------------------------------
	standbyRoundInfo := &pb.RoundInfo{
		ID:       0,
		UpdateID: numUpdates,
		State:    uint32(states.STANDBY),
		Topology: ourTopology,
	}

	// Increment updates id for next message
	numUpdates++

	// Set the signature field of the round info
	signRoundInfo(standbyRoundInfo)

	// Construct permissioning poll response
	mockPollResponse = &pb.PermissionPollResponse{
		FullNDF:    fullNdf,
		PartialNDF: stripNdf,
		Updates:    []*pb.RoundInfo{standbyRoundInfo},
	}

	// Update internal state with mock response
	err = UpdateInternalState(mockPollResponse, instance)
	if err != nil {
		t.Errorf("Failed to update internal state: %+v", err)
	}

	// ------------------------------- COMPLETED TESTING ------------------------------------------------------------
	completedRoundInfo := &pb.RoundInfo{
		ID:       0,
		UpdateID: numUpdates,
		State:    uint32(states.COMPLETED),
		Topology: ourTopology,
	}

	// Increment updates id for next message
	numUpdates++

	// Set the signature field of the round info
	signRoundInfo(completedRoundInfo)

	// Construct permissioning poll response
	mockPollResponse = &pb.PermissionPollResponse{
		FullNDF:    fullNdf,
		PartialNDF: stripNdf,
		Updates:    []*pb.RoundInfo{completedRoundInfo},
	}

	// Update internal state with mock response
	err = UpdateInternalState(mockPollResponse, instance)
	if err != nil {
		t.Errorf("Failed to update internal state: %+v", err)
	}
}

// Error path: Pass in a state that is unexpected in the round info,
// Attempt to update round in which our node is not a team-member
func TestUpdateInternalState_Error(t *testing.T) {
	// Create server instance
	instance, err := createServerInstance(t)
	if err != nil {
		t.Errorf("Couldn't create instance: %+v", err)
	}

	// Create a topology for round info
	nodeOne := id.NewNodeFromUInt(uint64(0), t).String()
	nodeTwo := id.NewNodeFromUInt(uint64(1), t).String()
	nodeThree := id.NewNodeFromUInt(uint64(2), t).String()
	ourTopology := []string{nodeOne, nodeTwo, nodeThree}

	// ------------------- Enter an unexpected state -------------------------------------

	// Construct round info message
	NumStateRoundInfo := &pb.RoundInfo{
		ID:       0,
		UpdateID: 4,
		// Attempt to turn to a state that doesn't exist (there are only NUM_STATES - 1 states)
		State:    uint32(states.NUM_STATES),
		Topology: ourTopology,
	}

	// Set the signature field of the round info
	signRoundInfo(NumStateRoundInfo)

	// Set up the ndf's
	fullNdf, _ := setupFullNdf()
	stripNdf, _ := setupPartialNdf()

	// Construct permissioning poll response
	mockPollResponse := &pb.PermissionPollResponse{
		FullNDF:    fullNdf,
		PartialNDF: stripNdf,
		Updates:    []*pb.RoundInfo{NumStateRoundInfo},
	}

	// Update internal state with mock response
	err = UpdateInternalState(mockPollResponse, instance)
	if err == nil {
		t.Errorf("Expected error path. Attempted to transfer to an unknown state")
	}

	//  --------------- Non team member test case -----------------------------------------

	// Exclude our node from the topology
	badTopology := []string{nodeTwo, nodeThree}

	// Construct round info message
	theirRoundInfo := &pb.RoundInfo{
		ID:       0,
		UpdateID: 4,
		State:    uint32(states.PRECOMPUTING),
		Topology: badTopology,
	}

	// Set the signature field of the round info
	signRoundInfo(theirRoundInfo)

	// Construct permissioning poll response
	mockPollResponse = &pb.PermissionPollResponse{
		FullNDF:    fullNdf,
		PartialNDF: stripNdf,
		Updates:    []*pb.RoundInfo{theirRoundInfo},
	}

	// Update internal state with mock response
	err = UpdateInternalState(mockPollResponse, instance)
	if err == nil {
		t.Errorf("Expected error path. Should not be able to update a round in which we aren't a team" +
			"memeber")
	}

}

//Full-stack happy path test for the node registration logic
func TestRegistration(t *testing.T) {

	gwConnected := make(chan struct{})
	permDone := make(chan struct{})

	// Pull certs and key
	cert, _ := utils.ReadFile(testkeys.GetNodeCertPath())
	key, _ := utils.ReadFile(testkeys.GetNodeKeyPath())

	// Generate id's and addresses
	nodeId = id.NewNodeFromUInt(uint64(0), t)
	nodeAddr := fmt.Sprintf("0.0.0.0:%d", 7000+rand.Intn(1000)+cnt)
	pAddr = fmt.Sprintf("0.0.0.0:%d", 2000+rand.Intn(1000))
	cnt++
	gAddr := fmt.Sprintf("0.0.0.0:%d", 4000+rand.Intn(1000)+cnt)

	// Build the node
	emptyNdf := builEmptydMockNdf()

	// Initialize definition
	def := &server.Definition{
		Flags:         server.Flags{},
		ID:            nodeId,
		PublicKey:     nil,
		PrivateKey:    nil,
		TlsCert:       cert,
		TlsKey:        key,
		Address:       nodeAddr,
		LogPath:       "",
		MetricLogPath: "",
		Gateway: server.GW{
			ID:      nodeId.NewGateway(),
			Address: gAddr,
			TlsCert: cert,
		},
		UserRegistry: nil,
		Permissioning: server.Perm{
			TlsCert:          []byte(testUtil.RegCert),
			Address:          pAddr,
			RegistrationCode: "",
		},
		GraphGenerator:  services.GraphGenerator{},
		ResourceMonitor: nil,
		FullNDF:         emptyNdf,
		PartialNDF:      emptyNdf,
	}

	// Create state machine
	sm := state.NewMachine(dummyStates)
	ok, err := sm.Update(current.WAITING)
	if !ok || err != nil {
		t.Errorf("Failed to prep state machine: %+v", err)
	}

	// Add handler for instance
	impl := func(i *server.Instance) *node.Implementation {
		return receivers.NewImplementation(i)
	}

	// Generate instance
	instance, err := server.CreateServerInstance(def, impl, sm, true)
	if err != nil {
		t.Errorf("Unable to create instance: %+v", err)
	}

	// Add permissioning as a host
	_, err = instance.GetNetwork().AddHost(id.PERMISSIONING, def.Permissioning.Address,
		def.Permissioning.TlsCert, false, false)
	if err != nil {
		t.Errorf("Failed to add permissioning host: %+v", err)
	}

	// Boot up permissioning server
	permComms, err := startPermisioning()
	if err != nil {
		t.Errorf("Couldn't create permissioning server: %+v", err)
	}
	defer permComms.Shutdown()

	// In go func
	go func() {
		// fixme: have gateway testing supported for a full stack test?
		//time.Sleep(1 * time.Second)
		//gAddr := fmt.Sprintf("0.0.0.0:%d", 5000+rand.Intn(1000))
		//gHandler := gateway.Handler(&mockGateway{})
		//gwComms = gateway.StartGateway(nodeId.NewGateway().String(), gAddr, gHandler, cert, key)
		//_, err := gwComms.AddHost(nodeId.String(), nodeAddr, cert, false, false)
		//if err != nil {
		//	t.Errorf("Failed to add gateway host")
		//}
		//if err != nil {
		//	t.Fatalf("Gateway could not connect to node")
		//}
		gwConnected <- struct{}{}
	}()

	// Register the node in a separate thread and notify when finished
	go func() {
		// Fetch permissioning host
		permHost, err := instance.GetNetwork().AddHost(id.PERMISSIONING, def.Permissioning.Address, def.Permissioning.TlsCert, true, false)
		if err != nil {
			t.Errorf("Unable to connect to registration server: %+v", err)
		}

		// Register with node
		err = RegisterNode(def, instance.GetNetwork(), permHost)
		if err != nil {
			t.Error(err)
		}
		// Blocking call: Request ndf from permissioning
		err = Poll(instance)
		if err != nil {
			t.Errorf("Failed to get ndf: %+v", err)
		}

		// Parse the Ndf
		serverCert, gwCert, err := FindSelfInNdf(def, instance.GetConsensus().GetFullNdf().Get())
		if err != nil {
			t.Errorf("Failed to install ndf: %+v", err)
		}

		// Restart the network with new certs
		def.TlsCert = []byte(serverCert)
		def.Gateway.TlsCert = []byte(gwCert)
		instance.RestartNetwork(impl, def, true)
		permDone <- struct{}{}

	}()
	// wait for gateway to connect
	<-gwConnected

	// fixme: have gateway testing supported for a full stack test?
	////poll server from gateway
	//numPolls := 0
	//for {
	//	if numPolls == 10 {
	//		t.Fatalf("Gateway could not get cert from server")
	//	}
	//	numPolls++
	//	nodeHost, _ := gwComms.GetHost(nodeId.String())
	//
	//	//emptyNdf, _ := builEmptydMockNdf().Marshal()
	//
	//	serverPoll := &pb.ServerPoll{
	//	}
	//
	//	msg, err := gwComms.SendPoll(nodeHost, serverPoll)
	//	if err != nil {
	//		t.Errorf("Error on polling signed certs: %+v", err)
	//	} else if bytes.Compare(msg.Id, make([]byte, 0)) != 0 { //&& msg.Ndf.Ndf !=  {
	//		break
	//	}
	//}

	//wait for server to finish
	<-permDone
}

func TestPoll_MultipleRoundupdates(t *testing.T) {
	// Create instance
	instance, err := createServerInstance(t)
	if err != nil {
		t.Errorf("Couldn't create instance: %+v", err)
	}

	// Start up permissioning server which will return multiple round updates
	permComms, err := startMultipleRoundUpdatesPermissioning()
	if err != nil {
		t.Errorf("Couldn't create permissioning server: %+v", err)
	}
	defer permComms.Shutdown()

	// Poll the permissioning server for updates
	err = Poll(instance)
	if err != nil {
		t.Errorf("Failed to poll for ndf: %+v", err)
	}

	// todo: check internal state for changes appropriate to permissioning response

}
