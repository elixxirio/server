////////////////////////////////////////////////////////////////////////////////
// Copyright © 2019 Privategrity Corporation                                   /
//                                                                             /
// All rights reserved.                                                        /
////////////////////////////////////////////////////////////////////////////////

package measure

import (
	"encoding/json"
	"testing"
)

// Test basic use of RoundMetrics & json conversion
func TestRoundMetrics(t *testing.T) {
	mockMetrics := NewRoundMetrics("NODE_TEST_ID", 3, 5, 4)

	m := new(Metrics)
	m.Measure("test-tag")
	mockMetrics.AddPhase("test-phase", *m)

	j, err := mockMetrics.MarshallJSON()
	if err != nil {
		t.Errorf("RoundMetrics failed to marshall into JSON: %+v", err)
	}

	remadeMetrics := new(RoundMetrics)
	err = json.Unmarshal(j, remadeMetrics)
	if err != nil {
		t.Errorf("Returned JSON string failed to re-marshall into RoundMetrics")
	}

	if len(remadeMetrics.PhaseMetrics) == 0 {
		t.Error("Lost phase metrics during transformation")
	}
}
