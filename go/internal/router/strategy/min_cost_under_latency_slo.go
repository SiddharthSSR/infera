package strategy

import (
	"math"
	"strings"
	"time"

	"github.com/infera/infera/go/pkg/types"
)

const (
	defaultLatencySLOMS     = 2000
	defaultEvidenceMaxAge   = 2 * time.Minute
	futureClockTolerance    = 30 * time.Second
	fallbackMissingEvidence = "no_candidate_with_trusted_cost_and_fresh_latency_under_slo"
)

// MinCostUnderLatencySLO selects the lowest-price healthy worker whose fresh
// observed p99 latency satisfies the configured SLO.
type MinCostUnderLatencySLO struct {
	latencySLOMS   float64
	evidenceMaxAge time.Duration
	resolveCost    CostResolver
	now            func() time.Time
	fallback       *LeastLoaded
}

// NewMinCostUnderLatencySLO constructs the evidence-aware cost strategy.
func NewMinCostUnderLatencySLO(options EngineOptions) *MinCostUnderLatencySLO {
	latencySLOMS := options.LatencySLOMS
	if latencySLOMS <= 0 || math.IsNaN(latencySLOMS) || math.IsInf(latencySLOMS, 0) {
		latencySLOMS = defaultLatencySLOMS
	}
	evidenceMaxAge := options.EvidenceMaxAge
	if evidenceMaxAge <= 0 {
		evidenceMaxAge = defaultEvidenceMaxAge
	}
	return &MinCostUnderLatencySLO{
		latencySLOMS: latencySLOMS, evidenceMaxAge: evidenceMaxAge,
		resolveCost: options.CostResolver, now: time.Now, fallback: NewLeastLoaded(),
	}
}

func (s *MinCostUnderLatencySLO) Name() types.StrategyType {
	return types.StrategyMinCostUnderLatencySLO
}

func (s *MinCostUnderLatencySLO) Select(request *types.InferenceRequest, candidates []*types.WorkerInfo) (*Selection, error) {
	if len(candidates) == 0 {
		return nil, &NoEligibleWorkersError{ModelID: request.ModelID, Reason: "no candidates provided"}
	}

	now := s.now().UTC()
	var best *types.WorkerInfo
	var bestCost int64
	eligibleCount := 0
	for _, worker := range candidates {
		if worker == nil || !worker.IsHealthy() || !worker.HasCapacity() || !s.reliableLatency(worker.Stats, now) {
			continue
		}
		cost, ok := s.trustedCost(worker.WorkerID)
		if !ok {
			continue
		}
		eligibleCount++
		if best == nil || cost.AmountNanoPerHour < bestCost ||
			(cost.AmountNanoPerHour == bestCost && lessCostTieBreak(worker, best)) {
			best = worker
			bestCost = cost.AmountNanoPerHour
		}
	}

	slo := s.latencySLOMS
	if best != nil {
		return &Selection{
			Worker: best,
			Decision: types.RoutingDecision{
				Strategy:            types.StrategyMinCostUnderLatencySLO,
				Reason:              "selected lowest trusted hourly cost among workers satisfying the latency SLO",
				CandidatesEvaluated: len(candidates), LatencySLOMS: &slo,
				SelectedCostNanoPerHour: &bestCost, CostSLOEligibleCandidates: &eligibleCount,
			},
		}, nil
	}

	selection, err := s.fallback.Select(request, candidates)
	if err != nil {
		return nil, err
	}
	selection.Decision.Strategy = types.StrategyMinCostUnderLatencySLO
	selection.Decision.Reason = "fell back to least-loaded routing because cost or latency evidence was incomplete"
	selection.Decision.LatencySLOMS = &slo
	selection.Decision.CostSLOEligibleCandidates = &eligibleCount
	selection.Decision.FallbackReason = fallbackMissingEvidence
	return selection, nil
}

func (s *MinCostUnderLatencySLO) reliableLatency(stats types.WorkerStats, now time.Time) bool {
	p99 := stats.P99LatencyMS
	if p99 <= 0 || p99 > s.latencySLOMS || math.IsNaN(p99) || math.IsInf(p99, 0) || stats.UpdatedAt.IsZero() {
		return false
	}
	updatedAt := stats.UpdatedAt.UTC()
	return !updatedAt.Before(now.Add(-s.evidenceMaxAge)) && !updatedAt.After(now.Add(futureClockTolerance))
}

func (s *MinCostUnderLatencySLO) trustedCost(workerID string) (CostEvidence, bool) {
	if s.resolveCost == nil || strings.TrimSpace(workerID) == "" {
		return CostEvidence{}, false
	}
	evidence, ok, err := s.resolveCost(workerID)
	if err != nil || !ok || evidence.AmountNanoPerHour <= 0 {
		return CostEvidence{}, false
	}
	return evidence, true
}

func lessCostTieBreak(candidate, current *types.WorkerInfo) bool {
	if candidate.Stats.P99LatencyMS != current.Stats.P99LatencyMS {
		return candidate.Stats.P99LatencyMS < current.Stats.P99LatencyMS
	}
	if candidate.CurrentLoad() != current.CurrentLoad() {
		return candidate.CurrentLoad() < current.CurrentLoad()
	}
	return candidate.WorkerID < current.WorkerID
}
