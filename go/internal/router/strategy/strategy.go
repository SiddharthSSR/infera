package strategy

import (
	"time"

	"github.com/infera/infera/go/pkg/types"
)

// CostEvidence is gateway-owned price evidence for a managed worker. The
// resolver is responsible for admitting only trusted, versioned snapshots.
type CostEvidence struct {
	AmountNanoPerHour int64
	CapturedAt        time.Time
}

// CostResolver resolves trusted price evidence by authoritative worker ID.
type CostResolver func(workerID string) (CostEvidence, bool, error)

// EngineOptions configures strategies that need external evidence.
type EngineOptions struct {
	LatencySLOMS   float64
	EvidenceMaxAge time.Duration
	CostResolver   CostResolver
}

// Strategy defines the interface for routing strategies.
type Strategy interface {
	Name() types.StrategyType
	Select(request *types.InferenceRequest, candidates []*types.WorkerInfo) (*Selection, error)
}

// Selection represents the result of a strategy selection.
type Selection struct {
	Worker   *types.WorkerInfo
	Score    float64
	Decision types.RoutingDecision
}

// NoEligibleWorkersError is returned when no workers are eligible for a request.
type NoEligibleWorkersError struct {
	ModelID string
	Reason  string
}

func (e *NoEligibleWorkersError) Error() string {
	return "no eligible workers for model " + e.ModelID + ": " + e.Reason
}
