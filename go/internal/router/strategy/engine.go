package strategy

import (
	"fmt"
	"sync"

	"github.com/infera/infera/go/pkg/types"
)

// Engine manages routing strategies.
type Engine struct {
	strategies      map[types.StrategyType]Strategy
	defaultStrategy types.StrategyType
	mu              sync.RWMutex
}

// NewEngine creates a new strategy engine with default strategy.
func NewEngine(defaultStrategy types.StrategyType) *Engine {
	e := &Engine{
		strategies:      make(map[types.StrategyType]Strategy),
		defaultStrategy: defaultStrategy,
	}

	e.Register(NewLeastLoaded())
	e.Register(NewRoundRobin())
	e.Register(NewLatencyBased())

	return e
}

// Register adds a strategy to the engine
func (e *Engine) Register(s Strategy) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.strategies[s.Name()] = s
}

// SelectWorker selects a worker using the default strategy.
func (e *Engine) SelectWorker(request *types.InferenceRequest, candidates []*types.WorkerInfo) (*Selection, error) {
	return e.SelectWorkerWithStrategy(request, candidates, e.defaultStrategy)
}

// SelectWorkerWithStrategy selects worker with a specific strategy.
func (e *Engine) SelectWorkerWithStrategy(
	request *types.InferenceRequest,
	candidates []*types.WorkerInfo,
	strategyType types.StrategyType,
) (*Selection, error) {
	e.mu.RLock()
	strategy, exists := e.strategies[strategyType]
	defer e.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("strategy %s not registered", strategyType)
	}

	return strategy.Select(request, candidates)
}

// DefaultStrategy returns the current default strategy type.
func (e *Engine) DefaultStrategy() types.StrategyType {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.defaultStrategy
}

// AvailableStrategies returns all registered strategy types.
func (e *Engine) AvailableStrategies() []types.StrategyType {
	e.mu.RLock()
	defer e.mu.RUnlock()

	strategies := make([]types.StrategyType, 0, len(e.strategies))
	for strategy := range e.strategies {
		strategies = append(strategies, strategy)
	}
	return strategies
}
