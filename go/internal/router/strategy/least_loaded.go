// Package strategy provides routing algorithms for worker selection.
package strategy

import (
	"github.com/infera/infera/go/pkg/types"
)

// LeastLoaded selects the worker with the lowest load.
type LeastLoaded struct{}

// NewLeastLoaded creates a new LeastLoaded strategy.
func NewLeastLoaded() *LeastLoaded {
	return &LeastLoaded{}
}

// Name returns the strategy identifier.
func (s *LeastLoaded) Name() types.StrategyType {
	return types.StrategyLeastLoaded
}

// Select chooses the worker with the lowest current load.
func (s *LeastLoaded) Select(request *types.InferenceRequest, candidates []*types.WorkerInfo) (*Selection, error) {
	if len(candidates) == 0 {
		return nil, &NoEligibleWorkersError{
			ModelID: request.ModelID,
			Reason:  "no candidates provided",
		}
	}

	// Filter to workers with capacity
	eligible := make([]*types.WorkerInfo, 0, len(candidates))
	for _, w := range candidates {
		if w.HasCapacity() {
			eligible = append(eligible, w)
		}
	}

	if len(eligible) == 0 {
		return nil, &NoEligibleWorkersError{
			ModelID: request.ModelID,
			Reason:  "all workers at capacity",
		}
	}

	// Find the worker with lowest load
	var bestWorker *types.WorkerInfo
	bestScore := -1.0 // Start lower than any possible score

	for _, w := range eligible {
		load := w.CurrentLoad()
		score := 1.0 - load // Invert so higher is better

		// First worker or better score or same score with lower queue depth
		if bestWorker == nil || score > bestScore || (score == bestScore && w.Stats.QueueDepth < bestWorker.Stats.QueueDepth) {
			bestScore = score
			bestWorker = w
		}
	}

	return &Selection{
		Worker: bestWorker,
		Score:  bestScore,
		Decision: types.RoutingDecision{
			Strategy:            types.StrategyLeastLoaded,
			Reason:              "selected worker with lowest load",
			CandidatesEvaluated: len(candidates),
			SelectedWorkerScore: bestScore,
		},
	}, nil
}
