package registry

import (
	"context"
	"testing"
	"time"

	"github.com/infera/infera/go/pkg/types"
)

func newTestRegistry() *WorkerRegistry {
	return NewWorkerRegistry(DefaultRegistryConfig())
}

func makeWorker(id, address string, status types.WorkerStatus, models []string) *types.WorkerInfo {
	loaded := make([]types.LoadedModel, len(models))
	for i, m := range models {
		loaded[i] = types.LoadedModel{ModelID: m, LoadedAt: time.Now()}
	}
	return &types.WorkerInfo{
		WorkerID:        id,
		Address:         address,
		Status:          status,
		LoadedModels:    loaded,
		LastHealthCheck: time.Now(),
		RegisteredAt:    time.Now(),
		Tags:            map[string]string{},
	}
}

func TestRegister(t *testing.T) {
	r := newTestRegistry()

	t.Run("registers worker", func(t *testing.T) {
		w := makeWorker("w1", "localhost:8001", types.WorkerStatusHealthy, []string{"llama-8b"})
		if err := r.Register(w); err != nil {
			t.Fatalf("Register failed: %v", err)
		}
		if r.Count() != 1 {
			t.Errorf("expected 1 worker, got %d", r.Count())
		}
	})

	t.Run("empty ID returns error", func(t *testing.T) {
		w := makeWorker("", "localhost:8002", types.WorkerStatusHealthy, nil)
		if err := r.Register(w); err == nil {
			t.Error("expected error for empty worker ID")
		}
	})

	t.Run("overwrites existing worker", func(t *testing.T) {
		w := makeWorker("w1", "localhost:9001", types.WorkerStatusHealthy, []string{"mistral-7b"})
		if err := r.Register(w); err != nil {
			t.Fatalf("Register failed: %v", err)
		}
		if r.Count() != 1 {
			t.Errorf("expected 1 worker (overwritten), got %d", r.Count())
		}

		got, _ := r.Get("w1")
		if got.Address != "localhost:9001" {
			t.Errorf("expected updated address, got %s", got.Address)
		}

		oldModelWorkers := r.GetWorkersForModel("llama-8b")
		for _, worker := range oldModelWorkers {
			if worker.WorkerID == "w1" {
				t.Fatal("expected w1 to be removed from old model index after overwrite")
			}
		}

		newModelWorkers := r.GetWorkersForModel("mistral-7b")
		foundW1 := false
		for _, worker := range newModelWorkers {
			if worker.WorkerID == "w1" {
				foundW1 = true
			}
		}
		if !foundW1 {
			t.Fatal("expected w1 to be indexed for mistral-7b after overwrite")
		}
	})
}

func TestDeregister(t *testing.T) {
	r := newTestRegistry()

	t.Run("deregisters worker", func(t *testing.T) {
		w := makeWorker("w1", "localhost:8001", types.WorkerStatusHealthy, []string{"llama-8b"})
		r.Register(w)

		if err := r.Deregister("w1"); err != nil {
			t.Fatalf("Deregister failed: %v", err)
		}
		if r.Count() != 0 {
			t.Errorf("expected 0 workers, got %d", r.Count())
		}
	})

	t.Run("removes from model index", func(t *testing.T) {
		w := makeWorker("w2", "localhost:8002", types.WorkerStatusHealthy, []string{"llama-8b"})
		r.Register(w)
		r.Deregister("w2")

		workers := r.GetWorkersForModel("llama-8b")
		if len(workers) != 0 {
			t.Errorf("expected 0 workers for model, got %d", len(workers))
		}
	})

	t.Run("nonexistent returns error", func(t *testing.T) {
		err := r.Deregister("nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent worker")
		}
	})
}

func TestGet(t *testing.T) {
	r := newTestRegistry()
	w := makeWorker("w1", "localhost:8001", types.WorkerStatusHealthy, []string{"llama-8b"})
	r.Register(w)

	t.Run("returns clone, not reference", func(t *testing.T) {
		got, found := r.Get("w1")
		if !found {
			t.Fatal("expected worker to be found")
		}
		if got.WorkerID != "w1" {
			t.Errorf("expected w1, got %s", got.WorkerID)
		}
		// Modify clone should not affect registry
		got.Address = "modified"
		original, _ := r.Get("w1")
		if original.Address == "modified" {
			t.Error("modifying clone affected registry")
		}
	})

	t.Run("nonexistent returns false", func(t *testing.T) {
		_, found := r.Get("nonexistent")
		if found {
			t.Error("expected not found")
		}
	})
}

func TestGetWorkersForModel(t *testing.T) {
	r := newTestRegistry()

	r.Register(makeWorker("w1", "a:1", types.WorkerStatusHealthy, []string{"llama-8b", "mistral-7b"}))
	r.Register(makeWorker("w2", "a:2", types.WorkerStatusHealthy, []string{"llama-8b"}))
	r.Register(makeWorker("w3", "a:3", types.WorkerStatusHealthy, []string{"mistral-7b"}))

	t.Run("returns workers with model", func(t *testing.T) {
		workers := r.GetWorkersForModel("llama-8b")
		if len(workers) != 2 {
			t.Errorf("expected 2 workers for llama-8b, got %d", len(workers))
		}
	})

	t.Run("returns nil for unknown model", func(t *testing.T) {
		workers := r.GetWorkersForModel("unknown")
		if workers != nil {
			t.Errorf("expected nil for unknown model, got %d workers", len(workers))
		}
	})
}

func TestGetHealthyWorkersForModel(t *testing.T) {
	r := newTestRegistry()

	healthy := makeWorker("w1", "a:1", types.WorkerStatusHealthy, []string{"llama-8b"})
	unhealthy := makeWorker("w2", "a:2", types.WorkerStatusUnhealthy, []string{"llama-8b"})
	overloaded := makeWorker("w3", "a:3", types.WorkerStatusHealthy, []string{"llama-8b"})
	overloaded.Stats.GPUUtilization = 1.0
	overloaded.Stats.ErrorRate = 0.2

	r.Register(healthy)
	r.Register(unhealthy)
	r.Register(overloaded)

	workers := r.GetHealthyWorkersForModel("llama-8b")
	if len(workers) != 1 {
		t.Errorf("expected 1 healthy worker, got %d", len(workers))
	}
	if workers[0].WorkerID != "w1" {
		t.Errorf("expected w1, got %s", workers[0].WorkerID)
	}
}

func TestUpdateWorkerStats(t *testing.T) {
	r := newTestRegistry()
	r.Register(makeWorker("w1", "a:1", types.WorkerStatusHealthy, nil))

	stats := types.WorkerStats{
		GPUUtilization:    0.75,
		MemoryUsedBytes:   8_000_000_000,
		MemoryTotalBytes:  16_000_000_000,
		RequestsPerSecond: 10.5,
		AvgLatencyMS:      50,
	}

	if err := r.UpdateWorkerStats("w1", stats); err != nil {
		t.Fatalf("UpdateWorkerStats failed: %v", err)
	}

	got, _ := r.Get("w1")
	if got.Stats.GPUUtilization != 0.75 {
		t.Errorf("expected GPU util 0.75, got %f", got.Stats.GPUUtilization)
	}

	// Nonexistent worker
	if err := r.UpdateWorkerStats("nonexistent", stats); err == nil {
		t.Error("expected error for nonexistent worker")
	}
}

func TestUpdateWorkerModels(t *testing.T) {
	r := newTestRegistry()
	r.Register(makeWorker("w1", "a:1", types.WorkerStatusHealthy, []string{"old-model"}))

	newModels := []types.LoadedModel{
		{ModelID: "new-model-a", LoadedAt: time.Now()},
		{ModelID: "new-model-b", LoadedAt: time.Now()},
	}
	if err := r.UpdateWorkerModels("w1", newModels); err != nil {
		t.Fatalf("UpdateWorkerModels failed: %v", err)
	}

	// Old model should no longer be indexed
	workers := r.GetWorkersForModel("old-model")
	if len(workers) != 0 {
		t.Error("old model should be removed from index")
	}

	// New models should be indexed
	workers = r.GetWorkersForModel("new-model-a")
	if len(workers) != 1 {
		t.Errorf("expected 1 worker for new-model-a, got %d", len(workers))
	}
}

func TestHealthChecker(t *testing.T) {
	cfg := RegistryConfig{
		HealthCheckInterval: 10 * time.Millisecond,
		UnhealthyThreshold:  50 * time.Millisecond,
		RemovalThreshold:    100 * time.Millisecond,
	}
	r := NewWorkerRegistry(cfg)

	// Register with stale heartbeat
	w := makeWorker("stale", "a:1", types.WorkerStatusHealthy, []string{"model"})
	w.LastHealthCheck = time.Now().Add(-200 * time.Millisecond)
	r.Register(w)

	ctx, cancel := context.WithCancel(context.Background())
	go r.StartHealthChecker(ctx)

	// Wait for health checker to run
	time.Sleep(50 * time.Millisecond)
	cancel()

	// Worker should have been removed (heartbeat was 200ms old, threshold is 100ms)
	if r.Count() != 0 {
		t.Errorf("expected stale worker to be removed, got %d", r.Count())
	}
}

func TestHealthTransitionCallback(t *testing.T) {
	cfg := RegistryConfig{
		HealthCheckInterval: time.Hour,
		UnhealthyThreshold:  50 * time.Millisecond,
		RemovalThreshold:    100 * time.Millisecond,
	}

	t.Run("marks unhealthy", func(t *testing.T) {
		r := NewWorkerRegistry(cfg)
		w := makeWorker("stale-unhealthy", "a:1", types.WorkerStatusHealthy, []string{"model"})
		w.LastHealthCheck = time.Now().Add(-75 * time.Millisecond)
		r.Register(w)

		var transitions []HealthTransition
		r.OnHealthTransition(func(transition HealthTransition) {
			transitions = append(transitions, transition)
		})

		r.checkWorkerHealth()

		if len(transitions) != 1 {
			t.Fatalf("expected 1 transition, got %d", len(transitions))
		}
		if transitions[0].Event != HealthTransitionMarkedUnhealthy {
			t.Fatalf("expected marked_unhealthy event, got %s", transitions[0].Event)
		}
		if transitions[0].FromStatus != types.WorkerStatusHealthy || transitions[0].ToStatus != types.WorkerStatusUnhealthy {
			t.Fatalf("unexpected status transition: %s -> %s", transitions[0].FromStatus, transitions[0].ToStatus)
		}
	})

	t.Run("removes stale worker", func(t *testing.T) {
		r := NewWorkerRegistry(cfg)
		w := makeWorker("stale-removed", "a:1", types.WorkerStatusUnhealthy, []string{"model"})
		w.LastHealthCheck = time.Now().Add(-150 * time.Millisecond)
		r.Register(w)

		var transitions []HealthTransition
		r.OnHealthTransition(func(transition HealthTransition) {
			transitions = append(transitions, transition)
		})

		r.checkWorkerHealth()

		if len(transitions) != 1 {
			t.Fatalf("expected 1 transition, got %d", len(transitions))
		}
		if transitions[0].Event != HealthTransitionRemoved {
			t.Fatalf("expected removed event, got %s", transitions[0].Event)
		}
		if transitions[0].FromStatus != types.WorkerStatusUnhealthy || transitions[0].ToStatus != types.WorkerStatusOffline {
			t.Fatalf("unexpected status transition: %s -> %s", transitions[0].FromStatus, transitions[0].ToStatus)
		}
		if r.Count() != 0 {
			t.Fatalf("expected worker to be removed, got count %d", r.Count())
		}
	})
}

func TestGetAllWorkers(t *testing.T) {
	r := newTestRegistry()
	r.Register(makeWorker("w1", "a:1", types.WorkerStatusHealthy, nil))
	r.Register(makeWorker("w2", "a:2", types.WorkerStatusUnhealthy, nil))

	all := r.GetAllWorkers()
	if len(all) != 2 {
		t.Errorf("expected 2 workers, got %d", len(all))
	}
}

func TestGetHealthyWorkers(t *testing.T) {
	r := newTestRegistry()
	r.Register(makeWorker("w1", "a:1", types.WorkerStatusHealthy, nil))
	r.Register(makeWorker("w2", "a:2", types.WorkerStatusDegraded, nil))
	r.Register(makeWorker("w3", "a:3", types.WorkerStatusUnhealthy, nil))

	healthy := r.GetHealthyWorkers()
	if len(healthy) != 2 {
		t.Errorf("expected 2 healthy (healthy+degraded), got %d", len(healthy))
	}
}
