package strategy

import "github.com/infera/infera/go/pkg/types"

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
