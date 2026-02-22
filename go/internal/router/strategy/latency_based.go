package strategy

import (
	"math"

	"github.com/infera/infera/go/pkg/types"
)

// LatencyBased selects workers based on recent latency performance.
type LatencyBased struct {
	LatencyWeight float64
	LoadWeight    float64
}

// NewLatencyBased creates a new LatencyBased strategy.
func NewLatencyBased() *LatencyBased {
	return &LatencyBased{LatencyWeight: 0.7, LoadWeight: 0.3}
}

// Name returns the strategy identifier.
func (s *LatencyBased) Name() types.StrategyType {
	return types.StrategyLatencyBased
}

// Select chooses the worker with best latency/load combination
func (l *LatencyBased) Select(request *types.InferenceRequest, candidates []*types.WorkerInfo) (*Selection, error) {
	if len(candidates) == 0 {
		return nil, &NoEligibleWorkersError{ModelID: request.ModelID, Reason: "no candidates"}
	}

	eligible := make([]*types.WorkerInfo, 0, len(candidates))
	for _, w := range candidates {
		if w.IsHealthy() && w.HasCapacity() {
			eligible = append(eligible, w)
		}
	}

	if len(eligible) == 0 {
		return nil, &NoEligibleWorkersError{ModelID: request.ModelID, Reason: "no healthy workers"}
	}

	minLatency, maxLatency := math.MaxFloat64, 0.0
	minLoad, maxLoad := math.MaxFloat64, 0.0

	for _, w := range eligible {
		lat := w.Stats.P50LatencyMS
		load := w.CurrentLoad()

		if lat < minLatency {
			minLatency = lat
		}
		if lat > maxLatency {
			maxLatency = lat
		}
		if load < minLoad {
			minLoad = load
		}
		if load > maxLoad {
			maxLoad = load
		}
	}

	var bestWorker *types.WorkerInfo
	bestScore := math.MaxFloat64

	latencyRange := maxLatency - minLatency
	loadRange := maxLoad - minLoad

	for _, w := range eligible {
		normLatency := 0.0
		if latencyRange > 0 {
			normLatency = (w.Stats.P50LatencyMS - minLatency) / latencyRange
		}

		normLoad := 0.0
		if loadRange > 0 {
			normLoad = (w.CurrentLoad() - minLoad) / loadRange
		}

		score := (l.LatencyWeight * normLatency) + (l.LoadWeight * normLoad)

		if score < bestScore {
			bestScore = score
			bestWorker = w
		}
	}

	finalScore := 1.0 - bestScore
	return &Selection{
		Worker: bestWorker,
		Score:  finalScore,
		Decision: types.RoutingDecision{
			Strategy:            types.StrategyLatencyBased,
			Reason:              "Best Latency/Load",
			CandidatesEvaluated: len(candidates),
			SelectedWorkerScore: finalScore,
		},
	}, nil
}
