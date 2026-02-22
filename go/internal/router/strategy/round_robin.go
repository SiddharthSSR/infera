package strategy

import (
	"sync"
	"sync/atomic"

	"github.com/infera/infera/go/pkg/types"
)

// RoundRobin distributes requests evenly across all available workers.
type RoundRobin struct {
	counters map[string]*atomic.Uint64
	mu       sync.RWMutex
}

// NewRoundRobin creates a new RoundRobin strategy.
func NewRoundRobin() *RoundRobin {
	return &RoundRobin{
		counters: make(map[string]*atomic.Uint64),
	}
}

// Name returns the strategy identifier.
func (r *RoundRobin) Name() types.StrategyType {
	return types.StrategyRoundRobin
}

// Select chooses the next worker in rotation.
func (r *RoundRobin) Select(request *types.InferenceRequest, candidates []*types.WorkerInfo) (*Selection, error) {
	if len(candidates) == 0 {
		return nil, &NoEligibleWorkersError{ModelID: request.ModelID, Reason: "no eligible workers"}
	}

	eligible := make([]*types.WorkerInfo, 0, len(candidates))
	for _, w := range candidates {
		if w.IsHealthy() {
			eligible = append(eligible, w)
		}
	}

	if len(eligible) == 0 {
		return nil, &NoEligibleWorkersError{ModelID: request.ModelID, Reason: "no eligible workers"}
	}

	counter := r.getCounter(request.ModelID)
	idx := counter.Add(1) % uint64(len(eligible))
	selected := eligible[idx]

	return &Selection{
		Worker: selected,
		Score:  1.0,
		Decision: types.RoutingDecision{
			Strategy:            types.StrategyRoundRobin,
			Reason:              "Round Robin",
			CandidatesEvaluated: len(candidates),
			SelectedWorkerScore: 1.0,
		},
	}, nil
}

func (r *RoundRobin) getCounter(modelID string) *atomic.Uint64 {
	r.mu.RLock()
	counter, exists := r.counters[modelID]
	r.mu.RUnlock()

	if exists {
		return counter
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	counter, exists = r.counters[modelID]
	if exists {
		return counter
	}

	counter = &atomic.Uint64{}
	r.counters[modelID] = counter
	return counter
}
