package strategy

import (
	"testing"
	"time"

	"github.com/infera/infera/go/pkg/types"
)

func makeWorker(id string, gpuUtil float64, queueDepth int, latency float64) *types.WorkerInfo {
	return &types.WorkerInfo{
		WorkerID: id,
		Address:  "localhost:800" + id,
		Status:   types.WorkerStatusHealthy,
		Stats: types.WorkerStats{
			GPUUtilization:   gpuUtil,
			QueueDepth:       queueDepth,
			MemoryUsedBytes:  4_000_000_000,
			MemoryTotalBytes: 16_000_000_000,
			P50LatencyMS:     latency,
			AvgLatencyMS:     latency,
		},
		LoadedModels:    []types.LoadedModel{{ModelID: "llama-8b", LoadedAt: time.Now()}},
		LastHealthCheck: time.Now(),
		Tags:            map[string]string{},
	}
}

func makeRequest(model string) *types.InferenceRequest {
	return &types.InferenceRequest{
		RequestID: "req-1",
		ModelID:   model,
	}
}

// ============================================================================
// LeastLoaded
// ============================================================================

func TestLeastLoaded(t *testing.T) {
	s := NewLeastLoaded()

	if s.Name() != types.StrategyLeastLoaded {
		t.Errorf("expected least_loaded, got %s", s.Name())
	}

	t.Run("selects lowest load worker", func(t *testing.T) {
		candidates := []*types.WorkerInfo{
			makeWorker("1", 0.8, 10, 50),
			makeWorker("2", 0.2, 2, 50),
			makeWorker("3", 0.5, 5, 50),
		}

		sel, err := s.Select(makeRequest("llama-8b"), candidates)
		if err != nil {
			t.Fatalf("Select failed: %v", err)
		}
		if sel.Worker.WorkerID != "2" {
			t.Errorf("expected worker 2 (lowest load), got %s", sel.Worker.WorkerID)
		}
		if sel.Score <= 0 {
			t.Error("expected positive score")
		}
	})

	t.Run("breaks tie by queue depth", func(t *testing.T) {
		candidates := []*types.WorkerInfo{
			makeWorker("1", 0.3, 10, 50),
			makeWorker("2", 0.3, 2, 50),
		}

		sel, _ := s.Select(makeRequest("llama-8b"), candidates)
		if sel.Worker.WorkerID != "2" {
			t.Errorf("expected worker 2 (lower queue), got %s", sel.Worker.WorkerID)
		}
	})

	t.Run("empty candidates returns error", func(t *testing.T) {
		_, err := s.Select(makeRequest("llama-8b"), nil)
		if err == nil {
			t.Error("expected error for empty candidates")
		}
		if _, ok := err.(*NoEligibleWorkersError); !ok {
			t.Errorf("expected NoEligibleWorkersError, got %T", err)
		}
	})

	t.Run("all overloaded returns error", func(t *testing.T) {
		overloaded := makeWorker("1", 1.0, 100, 50)
		overloaded.Stats.ErrorRate = 0.2

		_, err := s.Select(makeRequest("llama-8b"), []*types.WorkerInfo{overloaded})
		if err == nil {
			t.Error("expected error for all overloaded")
		}
	})
}

// ============================================================================
// RoundRobin
// ============================================================================

func TestRoundRobin(t *testing.T) {
	s := NewRoundRobin()

	if s.Name() != types.StrategyRoundRobin {
		t.Errorf("expected round_robin, got %s", s.Name())
	}

	t.Run("cycles through workers", func(t *testing.T) {
		candidates := []*types.WorkerInfo{
			makeWorker("1", 0.3, 2, 50),
			makeWorker("2", 0.3, 2, 50),
			makeWorker("3", 0.3, 2, 50),
		}

		seen := make(map[string]int)
		for i := 0; i < 9; i++ {
			sel, err := s.Select(makeRequest("rr-model"), candidates)
			if err != nil {
				t.Fatalf("Select failed on iteration %d: %v", i, err)
			}
			seen[sel.Worker.WorkerID]++
		}

		// Each worker should be selected 3 times
		for id, count := range seen {
			if count != 3 {
				t.Errorf("worker %s selected %d times, expected 3", id, count)
			}
		}
	})

	t.Run("empty candidates returns error", func(t *testing.T) {
		_, err := s.Select(makeRequest("model"), nil)
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("skips unhealthy workers", func(t *testing.T) {
		candidates := []*types.WorkerInfo{
			makeWorker("1", 0.3, 2, 50),
		}
		candidates[0].Status = types.WorkerStatusUnhealthy

		_, err := s.Select(makeRequest("model"), candidates)
		if err == nil {
			t.Error("expected error when all unhealthy")
		}
	})
}

// ============================================================================
// LatencyBased
// ============================================================================

func TestLatencyBased(t *testing.T) {
	s := NewLatencyBased()

	if s.Name() != types.StrategyLatencyBased {
		t.Errorf("expected latency_based, got %s", s.Name())
	}

	t.Run("selects lowest latency worker", func(t *testing.T) {
		candidates := []*types.WorkerInfo{
			makeWorker("1", 0.3, 2, 200), // high latency
			makeWorker("2", 0.3, 2, 10),  // low latency
			makeWorker("3", 0.3, 2, 100), // medium latency
		}

		sel, err := s.Select(makeRequest("model"), candidates)
		if err != nil {
			t.Fatalf("Select failed: %v", err)
		}
		if sel.Worker.WorkerID != "2" {
			t.Errorf("expected worker 2 (lowest latency), got %s", sel.Worker.WorkerID)
		}
	})

	t.Run("considers load in selection", func(t *testing.T) {
		candidates := []*types.WorkerInfo{
			makeWorker("1", 0.1, 1, 50),  // low load, medium latency
			makeWorker("2", 0.9, 50, 10), // high load, low latency
		}

		sel, _ := s.Select(makeRequest("model"), candidates)
		// With default weights (latency 70%, load 30%), low latency should win.
		if sel.Worker.WorkerID != "2" {
			t.Errorf("expected worker 2 (latency-weighted), got %s", sel.Worker.WorkerID)
		}
	})

	t.Run("allows tuning weights to prioritize load", func(t *testing.T) {
		custom := NewLatencyBased()
		custom.LatencyWeight = 0.2
		custom.LoadWeight = 0.8

		candidates := []*types.WorkerInfo{
			makeWorker("1", 0.1, 1, 50),  // low load, medium latency
			makeWorker("2", 0.9, 50, 10), // high load, low latency
		}

		sel, err := custom.Select(makeRequest("model"), candidates)
		if err != nil {
			t.Fatalf("Select failed: %v", err)
		}
		if sel.Worker.WorkerID != "1" {
			t.Errorf("expected worker 1 (load-prioritized), got %s", sel.Worker.WorkerID)
		}
	})

	t.Run("empty candidates returns error", func(t *testing.T) {
		_, err := s.Select(makeRequest("model"), nil)
		if err == nil {
			t.Error("expected error")
		}
	})

	t.Run("single worker returns it", func(t *testing.T) {
		candidates := []*types.WorkerInfo{makeWorker("1", 0.3, 2, 50)}

		sel, err := s.Select(makeRequest("model"), candidates)
		if err != nil {
			t.Fatalf("Select failed: %v", err)
		}
		if sel.Worker.WorkerID != "1" {
			t.Errorf("expected worker 1, got %s", sel.Worker.WorkerID)
		}
	})
}

// ============================================================================
// Engine
// ============================================================================

func TestEngine(t *testing.T) {
	e := NewEngine(types.StrategyLeastLoaded)

	t.Run("default strategy", func(t *testing.T) {
		if e.DefaultStrategy() != types.StrategyLeastLoaded {
			t.Errorf("expected least_loaded, got %s", e.DefaultStrategy())
		}
	})

	t.Run("available strategies", func(t *testing.T) {
		strategies := e.AvailableStrategies()
		if len(strategies) != 3 {
			t.Errorf("expected 3 strategies, got %d", len(strategies))
		}
	})

	t.Run("select with default strategy", func(t *testing.T) {
		candidates := []*types.WorkerInfo{makeWorker("1", 0.3, 2, 50)}
		sel, err := e.SelectWorker(makeRequest("model"), candidates)
		if err != nil {
			t.Fatalf("SelectWorker failed: %v", err)
		}
		if sel.Decision.Strategy != types.StrategyLeastLoaded {
			t.Errorf("expected least_loaded, got %s", sel.Decision.Strategy)
		}
	})

	t.Run("select with specific strategy", func(t *testing.T) {
		candidates := []*types.WorkerInfo{makeWorker("1", 0.3, 2, 50)}
		sel, err := e.SelectWorkerWithStrategy(makeRequest("model"), candidates, types.StrategyRoundRobin)
		if err != nil {
			t.Fatalf("SelectWorkerWithStrategy failed: %v", err)
		}
		if sel.Decision.Strategy != types.StrategyRoundRobin {
			t.Errorf("expected round_robin, got %s", sel.Decision.Strategy)
		}
	})

	t.Run("unregistered strategy returns error", func(t *testing.T) {
		_, err := e.SelectWorkerWithStrategy(makeRequest("model"), nil, "unknown")
		if err == nil {
			t.Error("expected error for unregistered strategy")
		}
	})
}

func TestNoEligibleWorkersError(t *testing.T) {
	err := &NoEligibleWorkersError{ModelID: "test-model", Reason: "all busy"}
	msg := err.Error()
	if msg != "no eligible workers for model test-model: all busy" {
		t.Errorf("unexpected error message: %s", msg)
	}
}
