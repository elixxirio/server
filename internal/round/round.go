///////////////////////////////////////////////////////////////////////////////
// Copyright © 2020 xx network SEZC                                          //
//                                                                           //
// Use of this source code is governed by a license that can be found in the //
// LICENSE file                                                              //
///////////////////////////////////////////////////////////////////////////////

package round

// round.go contains the round.Round object and its methods A round.Round indicates
// the round in a cMix network

import (
	"fmt"
	"github.com/pkg/errors"
	jww "github.com/spf13/jwalterweatherman"
	pb "git.xx.network/elixxir/comms/mixmessages"
	"git.xx.network/elixxir/crypto/cyclic"
	"git.xx.network/elixxir/crypto/fastRNG"
	"git.xx.network/elixxir/gpumathsgo"
	"git.xx.network/elixxir/server/internal/measure"
	"git.xx.network/elixxir/server/internal/phase"
	"git.xx.network/elixxir/server/services"
	"git.xx.network/elixxir/server/storage"
	"git.xx.network/xx_network/comms/connect"
	"git.xx.network/xx_network/primitives/id"
	"sync/atomic"
	"testing"
	"time"
)

type Round struct {
	id        id.Round
	buffer    *Buffer
	batchSize uint32

	topology *connect.Circuit
	state    *uint32

	//on first node and last node the phases vary
	phaseMap               map[phase.Type]int
	phases                 []phase.Phase
	numPhaseStates         uint32
	phaseStateUpdateSignal chan struct{}

	//holds responses to coms, how to check and process incoming comms
	responses phase.ResponseMap

	//holds round metrics data
	roundMetrics     measure.RoundMetrics
	metricsReadyChan chan struct{}

	// hold GPU stream references - should be populated if GPU in use,
	// should be nil if CPU only
	streamPool *gpumaths.StreamPool

	// Round trip info
	rtStarted   bool
	rtStartTime time.Time
	rtEndTime   time.Time
}

// New creates and initializes a new round, including all phases, topology,
// and batch size
func New(grp *cyclic.Group, storage *storage.Storage, id id.Round,
	phases []phase.Phase, responses phase.ResponseMap,
	circuit *connect.Circuit, nodeID *id.ID, batchSize uint32,
	rngStreamGen *fastRNG.StreamGenerator, streamPool *gpumaths.StreamPool,
	localIP string, errorHandler services.ErrorCallback, clientErr *ClientReport) (*Round, error) {

	if batchSize <= 0 {
		return nil, errors.New("Cannot make a round with a <=0 batch size")
	}

	roundMetrics := measure.NewRoundMetrics(id, batchSize)
	roundMetrics.IP = localIP
	round := Round{id: id, roundMetrics: roundMetrics, streamPool: streamPool}

	maxBatchSize := uint32(0)

	state := uint32(0)
	round.state = &state

	round.phaseStateUpdateSignal = make(chan struct{}, 1)

	for index, p := range phases {
		if p.GetGraph() != nil {
			p.GetGraph().Build(batchSize, errorHandler)
			if p.GetGraph().GetExpandedBatchSize() > maxBatchSize {
				maxBatchSize = p.GetGraph().GetExpandedBatchSize()
			}
		}

		// the NumStates-2 exists because the Initialized and verified states
		// are done implicitly as less then available / greater then computed
		// the +1 exists because the system needs an initialized state for the
		// first phase
		localStateOffset := uint32(index)*uint32(phase.NumStates-2) + 1

		// Build the function this phase will use to increment its state
		increment := func(from, to phase.State) bool {
			if from >= to {
				jww.ERROR.Printf("Cannot increment backwards from %s to %s",
					from, to)
				return false
			}
			// 1 is subtracted because Initialized doesnt hold a true state
			newState := localStateOffset + uint32(to) - 1
			expectedOld := localStateOffset + uint32(from) - 1

			//fmt.Printf("ExpectedOld: %v, ExpectedNew: %v, ActualOld: %v\n",
			//	expectedOld, newState, atomic.LoadUint32(round.state))

			success := atomic.CompareAndSwapUint32(round.state, expectedOld, newState)

			if success {
				select {
				case round.phaseStateUpdateSignal <- struct{}{}:
				default:
				}
			}

			return success
		}

		// Build the function this phase will use to get its state
		// -1 is at the end of all phase states because Initialized
		// is not counted as a state
		get := func() phase.State {
			currentState := int64(atomic.LoadUint32(round.state)) - int64(localStateOffset)
			if currentState < 0 {
				return 0
			} else if currentState > int64(phase.NumStates)-2 {
				return phase.NumStates - 1
			} else {
				return phase.State(currentState + 1)
			}
		}

		// Connect the phase to the round passing its state accessor functions
		p.ConnectToRound(id, increment, get)
	}

	// If there weren't any phases (can happen in some tests) the maxBatchSize
	// won't have been set yet, so here, make sure maxBatchSize is at least
	// batchSize
	if maxBatchSize < batchSize {
		jww.WARN.Print("Max batch size wasn't set. " +
			"Phases may be set up incorrectly.")
		maxBatchSize = batchSize
	}

	round.topology = circuit

	round.buffer = NewBuffer(grp, batchSize, maxBatchSize)
	round.buffer.InitCryptoFields(grp)
	round.phaseMap = make(map[phase.Type]int)

	if round.topology.IsLastNode(nodeID) {
		round.buffer.InitLastNode()
	}

	for index, p := range phases {
		// If in realDecrypt, we need to handle client specific errors
		if p.GetGraph() != nil {
			if p.GetType() == phase.RealDecrypt {
				p.GetGraph().Link(grp, round.GetBuffer(), storage, rngStreamGen, streamPool, clientErr, id)
			} else {
				// Other phases can operate normally
				p.GetGraph().Link(grp, round.GetBuffer(), storage, rngStreamGen, streamPool)

			}
		}

		round.phaseMap[p.GetType()] = index

	}

	round.phases = make([]phase.Phase, len(phases))

	copy(round.phases[:], phases[:])

	round.responses = responses

	//set the state of the first phase to available
	success := atomic.CompareAndSwapUint32(round.state, uint32(phase.Initialized), uint32(phase.Active))
	if !success {
		jww.FATAL.Println("phase state initialization failed")
	}

	round.metricsReadyChan = make(chan struct{}, 1)

	round.batchSize = batchSize

	return &round, nil
}

func NewDummyRound(roundId id.Round, batchSize uint32, t *testing.T) *Round {
	if t == nil {
		panic("Can not use NewDummyRound out side of testing")
	}
	list := []*id.ID{}

	for i := uint64(0); i < 8; i++ {
		node := id.NewIdFromUInt(i, id.Node, t)
		list = append(list, node)
	}

	top := *connect.NewCircuit(list)

	state := uint32(phase.Active)
	r := &Round{id: roundId, batchSize: batchSize, topology: &top, state: &state}
	return r
}

func NewDummyRoundWithTopology(roundId id.Round, batchSize uint32,
	topology *connect.Circuit, t *testing.T) *Round {
	r := NewDummyRound(roundId, batchSize, t)
	r.topology = topology
	return r
}

//GetID return the ID
func (r *Round) GetID() id.Round {
	return r.id
}

func (r *Round) GetTimeStart() time.Time {
	return r.roundMetrics.StartTime
}

func (r *Round) GetBuffer() *Buffer {
	return r.buffer
}

func (r *Round) GetBatchSize() uint32 {
	return r.batchSize
}

func (r *Round) GetPhase(p phase.Type) (phase.Phase, error) {
	i, ok := r.phaseMap[p]
	if !ok || i >= len(r.phases) || r.phases[i] == nil {
		return nil, errors.Errorf("Round %s missing phase type %s",
			r, p)
	}
	return r.phases[i], nil
}

func (r *Round) GetCurrentPhaseType() phase.Type {
	return phase.Type((atomic.LoadUint32(r.state) - 1) /
		(uint32(phase.NumStates) - 2))
}

func (r *Round) GetCurrentPhase() phase.Phase {
	return r.phases[r.GetCurrentPhaseType()]
}

func (r *Round) GetTopology() *connect.Circuit {
	return r.topology
}

// HandleIncomingComm checks that the incoming state is valid for the round
// and waits for it to be valid if it isnt
// TODO: check if it is behind the current state and return an error
func (r *Round) HandleIncomingComm(commTag string) (phase.Phase, error) {
	response, ok := r.responses[commTag]

	if !ok {
		errStr := fmt.Sprintf("The round does not have "+
			"a response to the given input, Round: %v, Input: %s", r.id, commTag)
		return nil, errors.Errorf(errStr)
	}

	phaseToCheck, err := r.GetPhase(response.GetPhaseLookup())

	if err != nil {
		jww.FATAL.Panicf("phase %s looked up up from response map "+
			"with tag %s does not exist in round", response.GetPhaseLookup(),
			commTag)
	}

	t := time.NewTimer(15 * time.Second)
	for {
		if response.CheckState(phaseToCheck.GetState()) {
			break
		}

		select {
		case <-t.C:
			return nil, errors.New(fmt.Sprintf("Time out on moving to phase %s state %s"+
				"round %v", phaseToCheck, response.String(), r.id))
		case <-r.phaseStateUpdateSignal:
		}
	}

	returnPhase, err := r.GetPhase(response.GetReturnPhase())
	if err != nil {
		jww.FATAL.Panicf("The requested phase could not be returned in the comm handler")
	}

	return returnPhase, nil

}

// Return a RoundMetrics objects for this round
func (r *Round) GetMeasurements(nid *id.ID, numNodes, index int,
	resourceMetric measure.ResourceMetric) measure.RoundMetrics {

	rm := r.roundMetrics
	rm.SetNodeID(nid)
	rm.SetNumNodes(numNodes)
	rm.SetIndex(index)
	rm.SetResourceMetrics(resourceMetric)

	// Add metrics for each phase in this round to the RoundMetrics
	for _, ph := range r.phases {
		phaseName := ph.GetType().String()
		phaseMeasure := ph.GetMeasure()
		phaseMeasure.NodeId = nid
		rm.AddPhase(phaseName, phaseMeasure)
	}

	// Set end time
	rm.EndTime = time.Now()

	return rm
}

func (r *Round) AddToDispatchDuration(delta time.Duration) {
	r.roundMetrics.DispatchDuration += delta
}

func (r *Round) GetMeasurementsReadyChan() chan struct{} {
	return r.metricsReadyChan
}

// String stringer interface implementation for rounds.
func (r *Round) String() string {
	currentPhase := r.GetCurrentPhase()
	return fmt.Sprintf("%d (%d - %s)", r.id, r.state, currentPhase)
}

// StartRoundTrip sets start time for rt ping
func (r *Round) StartRoundTrip(payload string) {
	t := time.Now()
	r.rtStartTime = t
	r.roundMetrics.RTPayload = payload
	r.rtStarted = true
}

// GetRTStart gets start time of rt ping
func (r *Round) GetRTStart() time.Time {
	return r.rtStartTime
}

// StopRoundTrip sets end time of rt ping
func (r *Round) StopRoundTrip() error {
	if !r.rtStarted {
		return errors.Errorf("StopRoundTrip: failed to stop round trip: round trip was never started")
	}

	r.rtEndTime = time.Now()
	duration := r.rtEndTime.Sub(r.rtStartTime)
	r.roundMetrics.RTDurationMilli = float64(duration.Nanoseconds()) / float64(1000000)
	jww.INFO.Printf("Round trip duration for round %d: %v ms",
		uint32(r.id), r.roundMetrics.RTDurationMilli)

	return nil
}

// GetRTEnd gets end time of rt ping
func (r *Round) GetRTEnd() time.Time {
	return r.rtEndTime
}

// GetRTPayload gets the payload info for the rt ping
func (r *Round) GetRTPayload() string {
	return r.roundMetrics.RTPayload
}

// AddFinalShareMessage adds to the message tracker final share piece from the sender
// If we have a message in there already, we error
func (r *Round) AddFinalShareMessage(piece *pb.SharePiece, origin *id.ID) error {
	return r.buffer.AddFinalShareMessage(piece, origin)
}

// GetPieceMessagesByNode gets final share piece message received by the
// specified nodeID
func (r *Round) GetPieceMessagesByNode(origin *id.ID) *pb.SharePiece {
	return r.buffer.GetPieceMessagesByNode(origin)
}

// UpdateFinalKeys adds a new key to the list of final keys
func (r *Round) UpdateFinalKeys(piece *cyclic.Int) []*cyclic.Int {
	return r.buffer.UpdateFinalKeys(piece)
}

func (r *Round) IncrementShares() uint32 {
	return r.buffer.IncrementShares()
}
