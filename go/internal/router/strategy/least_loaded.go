package strategy

import "github.com/infera/infera/go/pkg/types"

// LeastLoaded selects the worker with the least load.
type LeastLoaded struct{}

// NewLeastLoaded creates a new least loaded strategy.
func NewLeastLoaded() *LeastLoaded {
	return &LeastLoaded{}
}

// Name returns the strategy identifier.
func (l *LeastLoaded) Name() types.StrategyType {
	return types.StrategyLeastLoaded
}

// Select chooses the worker with the lowest current load.
func (l *LeastLoaded) Select(request *types.InferenceRequest, candidates []*types.WorkerInfo) (*Selection, error) {
	if len(candidates) == 0 {
		return nil, &NoEligibleWorkersError{ModelID: request.ModelID, Reason: "no candidates"}
	}

	eligible := make([]*types.WorkerInfo, 0, len(candidates))
	for _, w := range candidates {
		if w.HasCapacity() {
			eligible = append(eligible, w)
		}
	}

	if len(eligible) == 0 {
		return nil, &NoEligibleWorkersError{ModelID: request.ModelID, Reason: "all at capacity"}
	}

	var bestWorker *types.WorkerInfo
	bestScore := 2.0

	for _, w := range eligible {
		score := 1.0 - w.CurrentLoad()
		if score > bestScore || (score == bestScore && w.Stats.QueueDepth < bestWorker.Stats.QueueDepth) {
			bestScore = score
			bestWorker = w
		}
	}

	return &Selection{
		Worker: bestWorker,
		Score:  bestScore,
		Decision: types.RoutingDecision{
			Strategy:            types.StrategyLeastLoaded,
			Reason:              "Lowest Load",
			CandidatesEvaluated: len(candidates),
			SelectedWorkerScore: bestScore,
		},
	}, nil
}
