////////////////////////////////////////////////////////////////////////////////
// Copyright © 2018 Privategrity Corporation                                   /
//                                                                             /
// All rights reserved.                                                        /
////////////////////////////////////////////////////////////////////////////////

package io

import (
	jww "github.com/spf13/jwalterweatherman"
	"gitlab.com/privategrity/server/globals"
	"time"
)

// When a round is set to ERROR because it timed out, other io functions
// (transmission and reception handlers) can cause panics when setting the
// phase to something earlier. Functions that set a phase should call
// this function deferred.
// This can't be right... can it?
func recoverSetPhasePanic(roundId string) {
	if r := recover(); r != nil {
		jww.WARN.Printf("Recovered from panic: %v", r)
		globals.GlobalRoundMap.GetRound(roundId).SetPhase(globals.ERROR)
	}
}

// Errors a round after a certain time if its precomputation isn't done
func timeoutPrecomputation(roundId string, timeout time.Duration) {
	round := globals.GlobalRoundMap.GetRound(roundId)
	success := false
	timer := time.AfterFunc(timeout, func() {
		if !success && round.GetPhase() < globals.PRECOMP_COMPLETE {
			// Precomp wasn't totally complete before timeout. Set it to error
			jww.ERROR.Printf("Precomputation incomplete: Timing out round %v"+
				" on node %v with phase %v", roundId, globals.NodeID(0),
				round.GetPhase().String())
			round.SetPhase(globals.ERROR)
		}
	})
	go func() {
		round.WaitUntilPhase(globals.PRECOMP_COMPLETE)
		jww.INFO.Printf("Waited until phase %v"+
			" on node %v for round %v", round.GetPhase().String(), globals.NodeID(0),
			roundId)
		success = true
		timer.Stop()
	}()
}

// Errors a round after a certain time if its realtime process isn't done
func timeoutRealtime(roundId string, timeout time.Duration) {
	round := globals.GlobalRoundMap.GetRound(roundId)
	success := false
	timer := time.AfterFunc(timeout, func() {
		if !success && round.GetPhase() < globals.REAL_COMPLETE {
			// Realtime wasn't totally complete before timeout. Set it to error
			jww.ERROR.Printf("Realtime incomplete: Timing out round %v on node"+
				" %v with phase %v", roundId, globals.NodeID(0), round.GetPhase().String())
			round.SetPhase(globals.ERROR)
		}
	})
	go func() {
		round.WaitUntilPhase(globals.REAL_COMPLETE)
		jww.INFO.Printf("Waited until phase %v"+
			" on node %v for round %v", round.GetPhase().String(), globals.NodeID(0),
			roundId)
		success = true
		timer.Stop()
	}()
}
