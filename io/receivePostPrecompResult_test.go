///////////////////////////////////////////////////////////////////////////////
// Copyright © 2020 xx network SEZC                                          //
//                                                                           //
// Use of this source code is governed by a license that can be found in the //
// LICENSE file                                                              //
///////////////////////////////////////////////////////////////////////////////

package io

import (
	"gitlab.com/elixxir/comms/connect"
	"gitlab.com/elixxir/comms/mixmessages"
	"gitlab.com/elixxir/primitives/current"
	"gitlab.com/elixxir/primitives/id"
	"gitlab.com/elixxir/server/globals"
	"gitlab.com/elixxir/server/internal"
	"gitlab.com/elixxir/server/internal/measure"
	"gitlab.com/elixxir/server/internal/phase"
	"gitlab.com/elixxir/server/internal/round"
	"gitlab.com/elixxir/server/internal/state"
	"gitlab.com/elixxir/server/testUtil"
	"testing"
	"time"
)

// Shows that ReceivePostPrecompResult panics when the round isn't in
// the round manager
func TestPostPrecompResultFunc_Error_NoRound(t *testing.T) {
	instance, topology, _ := createMockInstance(t, 1, current.PRECOMPUTING)

	// Build a host around the last node
	lastNodeIndex := topology.Len() - 1
	lastNodeId := topology.GetNodeAtIndex(lastNodeIndex)
	fakeHost, err := connect.NewHost(lastNodeId, "", nil, true, true)
	if err != nil {
		t.Errorf("Failed to create fakeHost, %s", err)
	}

	auth := &connect.Auth{
		IsAuthenticated: true,
		Sender:          fakeHost,
	}

	// We haven't set anything up,
	// so this should panic because the round can't be found
	err = ReceivePostPrecompResult(instance, 1, []*mixmessages.Slot{}, auth)

	if err == nil {
		t.Error("Didn't get an error from a nonexistent round")
	}
}

// Shows that ReceivePostPrecompResult returns an error when there are a wrong
// number of slots in the message
func TestPostPrecompResultFunc_Error_WrongNumSlots(t *testing.T) {
	// Smoke tests the management part of PostPrecompResult
	instance, topology, grp := createMockInstance(t, 1, current.PRECOMPUTING)

	roundID := id.Round(45)
	// Is this the right setup for the response?
	response := phase.NewResponse(
		phase.ResponseDefinition{
			PhaseAtSource:  phase.PrecompReveal,
			ExpectedStates: []phase.State{phase.Active},
			PhaseToExecute: phase.PrecompReveal},
	)
	responseMap := make(phase.ResponseMap)
	responseMap[phase.PrecompReveal.String()+"Verification"] = response
	// This is quite a bit of setup...
	p := testUtil.InitMockPhase(t)
	p.Ptype = phase.PrecompReveal
	rnd, err := round.New(grp,
		instance.GetUserRegistry(), roundID, []phase.Phase{p}, responseMap,
		topology, topology.GetNodeAtIndex(0), 3,
		instance.GetRngStreamGen(), nil, "0.0.0.0", nil)
	if err != nil {
		t.Errorf("Failed to create new round: %+v", err)
	}
	instance.GetRoundManager().AddRound(rnd)

	// Build a host around the last node
	lastNodeIndex := topology.Len() - 1
	lastNodeId := topology.GetNodeAtIndex(lastNodeIndex)
	fakeHost, err := connect.NewHost(lastNodeId, "", nil, true, true)
	if err != nil {
		t.Errorf("Failed to create fakeHost, %s", err)
	}

	auth := &connect.Auth{
		IsAuthenticated: true,
		Sender:          fakeHost,
	}

	// This should give an error because we give it fewer slots than are in the
	// batch
	err = ReceivePostPrecompResult(instance, uint64(roundID), []*mixmessages.Slot{}, auth)

	if err == nil {
		t.Error("Didn't get an error from the wrong number of slots")
	}
}

// Shows that PostPrecompResult puts the completed precomputation on the
// channel on the first node when it has valid data
// Shows that PostPrecompResult doesn't result in errors on the other nodes
func TestPostPrecompResultFunc(t *testing.T) {
	// Smoke tests the management part of PostPrecompResult
	grp := initImplGroup()
	const numNodes = 5
	nodeIDs := BuildMockNodeIDs(5, t)

	// Set up all the instances
	var instances []*internal.Instance
	topology := connect.NewCircuit(nodeIDs)
	for i := 0; i < numNodes; i++ {
		def := internal.Definition{
			UserRegistry:    &globals.UserMap{},
			ResourceMonitor: &measure.ResourceMonitor{},
			FullNDF:         testUtil.NDF,
			PartialNDF:      testUtil.NDF,
		}
		def.ID = topology.GetNodeAtIndex(1)

		m := state.NewTestMachine(dummyStates, current.PRECOMPUTING, t)

		instance, _ := internal.CreateServerInstance(&def, NewImplementation,
			m, "1.1.0")
		rnd, err := round.New(grp, nil, id.Round(0), make([]phase.Phase, 0),
			make(phase.ResponseMap), topology, topology.GetNodeAtIndex(0),
			3, instance.GetRngStreamGen(), nil, "0.0.0.0", nil)
		if err != nil {
			t.Errorf("Failed to create new round: %+v", err)
		}
		instance.GetRoundManager().AddRound(rnd)
		instances = append(instances, instance)
	}

	// Set up a round on all the instances
	roundID := id.Round(45)
	for i := 0; i < numNodes; i++ {
		response := phase.NewResponse(phase.ResponseDefinition{
			PhaseAtSource:  phase.PrecompReveal,
			ExpectedStates: []phase.State{phase.Active},
			PhaseToExecute: phase.PrecompReveal})

		responseMap := make(phase.ResponseMap)
		responseMap[phase.PrecompReveal.String()+"Verification"] = response
		// This is quite a bit of setup...
		p := testUtil.InitMockPhase(t)
		p.Ptype = phase.PrecompReveal
		rnd, err := round.New(grp,
			instances[i].GetUserRegistry(), roundID,
			[]phase.Phase{p}, responseMap, topology, topology.GetNodeAtIndex(i),
			3, instances[i].GetRngStreamGen(), nil, "0.0.0.0", nil)
		if err != nil {
			t.Errorf("Failed to create new round: %+v", err)
		}
		instances[i].GetRoundManager().AddRound(rnd)
	}

	// Initially, there should be zero rounds on the precomp queue
	//if len(instances[0].GetCompletedPrecomps().CompletedPrecomputations) != 0 {
	//	t.Error("Expected completed precomps to be empty")
	//}

	// Build a host around the last node
	lastNodeIndex := topology.Len() - 1
	lastNodeId := topology.GetNodeAtIndex(lastNodeIndex)
	fakeHost, err := connect.NewHost(lastNodeId, "", nil, true, true)
	if err != nil {
		t.Errorf("Failed to create fakeHost, %s", err)
	}

	auth := &connect.Auth{
		IsAuthenticated: true,
		Sender:          fakeHost,
	}

	// Since we give this 3 slots with the correct fields populated,
	// it should work without errors on all nodes
	for i := 0; i < numNodes; i++ {
		inst := instances[i]
		err := ReceivePostPrecompResult(inst, uint64(roundID),
			[]*mixmessages.Slot{{
				PartialPayloadACypherText: grp.NewInt(3).Bytes(),
				PartialPayloadBCypherText: grp.NewInt(4).Bytes(),
			}, {
				PartialPayloadACypherText: grp.NewInt(3).Bytes(),
				PartialPayloadBCypherText: grp.NewInt(4).Bytes(),
			}, {
				PartialPayloadACypherText: grp.NewInt(3).Bytes(),
				PartialPayloadBCypherText: grp.NewInt(4).Bytes(),
			}}, auth)

		if err != nil {
			t.Errorf("Error posting precomp on node %v: %v", i, err)
		}
		time.Sleep(time.Second)
		if inst.GetStateMachine().Get() != current.STANDBY {
			t.Errorf("Instance did not transition to standby")
		}
	}
}

// Tests that ReceivePostPrecompResult() returns an error when isAuthenticated
// is set to false in the Auth object.
func TestReceivePostPrecompResult_NoAuth(t *testing.T) {
	instance, topology := mockServerInstance(t, current.PRECOMPUTING)

	fakeHost, err := connect.NewHost(topology.GetNodeAtIndex(0), "", nil, true, true)
	if err != nil {
		t.Errorf("Failed to create fakeHost, %s", err)
	}
	auth := connect.Auth{
		IsAuthenticated: false,
		Sender:          fakeHost,
	}
	err = ReceivePostPrecompResult(instance, 0, []*mixmessages.Slot{}, &auth)

	if err == nil {
		t.Errorf("ReceivePostPrecompResult: did not error with IsAuthenticated false")
	}
}

// Tests that ReceivePostPrecompResult() returns an error when Sender is set to
// the wrong sender in the Auth object.
func TestPostPrecompResult_WrongSender(t *testing.T) {
	instance, _ := mockServerInstance(t, current.PRECOMPUTING)

	newID := id.NewIdFromString("bad", id.Node, t)
	fakeHost, err := connect.NewHost(newID, "", nil, true, true)
	if err != nil {
		t.Errorf("Failed to create fakeHost, %s", err)
	}
	auth := connect.Auth{
		IsAuthenticated: true,
		Sender:          fakeHost,
	}
	err = ReceivePostPrecompResult(instance, 0, []*mixmessages.Slot{}, &auth)

	if err == nil {
		t.Errorf("ReceivePostPrecompResult: did not error with wrong host")
	}
}