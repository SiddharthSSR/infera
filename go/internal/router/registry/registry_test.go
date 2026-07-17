package registry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/infera/infera/go/pkg/types"
)

func newTestRegistry() *WorkerRegistry {
	return NewWorkerRegistry(DefaultRegistryConfig())
}

var testContext = context.Background()

func workerCount(t *testing.T, r *WorkerRegistry) int {
	t.Helper()
	count, err := r.Count(testContext)
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	return count
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
		if err := r.Register(testContext, w); err != nil {
			t.Fatalf("Register failed: %v", err)
		}
		if workerCount(t, r) != 1 {
			t.Errorf("expected 1 worker, got %d", workerCount(t, r))
		}
		if w.RegistrationID == "" {
			t.Fatal("registry did not issue a registration identity")
		}
	})

	t.Run("empty ID returns error", func(t *testing.T) {
		w := makeWorker("", "localhost:8002", types.WorkerStatusHealthy, nil)
		if err := r.Register(testContext, w); err == nil {
			t.Error("expected error for empty worker ID")
		}
	})

	t.Run("overwrites existing worker", func(t *testing.T) {
		previousRegistrationID := wRegistrationID(t, r, "w1")
		w := makeWorker("w1", "localhost:9001", types.WorkerStatusHealthy, []string{"mistral-7b"})
		if err := r.Register(testContext, w); err != nil {
			t.Fatalf("Register failed: %v", err)
		}
		if workerCount(t, r) != 1 {
			t.Errorf("expected 1 worker (overwritten), got %d", workerCount(t, r))
		}

		got, _, err := r.Get(testContext, "w1")
		if err != nil {
			t.Fatal(err)
		}
		if got.Address != "localhost:9001" {
			t.Errorf("expected updated address, got %s", got.Address)
		}
		if got.RegistrationID == previousRegistrationID {
			t.Fatal("replacement reused the previous registration identity")
		}

		oldModelWorkers, err := r.GetWorkersForModel(testContext, "llama-8b")
		if err != nil {
			t.Fatal(err)
		}
		for _, worker := range oldModelWorkers {
			if worker.WorkerID == "w1" {
				t.Fatal("expected w1 to be removed from old model index after overwrite")
			}
		}

		newModelWorkers, err := r.GetWorkersForModel(testContext, "mistral-7b")
		if err != nil {
			t.Fatal(err)
		}
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
		if err := r.Register(testContext, w); err != nil {
			t.Fatalf("Register failed: %v", err)
		}

		if err := r.Deregister(testContext, "w1"); err != nil {
			t.Fatalf("Deregister failed: %v", err)
		}
		if workerCount(t, r) != 0 {
			t.Errorf("expected 0 workers, got %d", workerCount(t, r))
		}
	})

	t.Run("removes from model index", func(t *testing.T) {
		w := makeWorker("w2", "localhost:8002", types.WorkerStatusHealthy, []string{"llama-8b"})
		if err := r.Register(testContext, w); err != nil {
			t.Fatalf("Register failed: %v", err)
		}
		if err := r.Deregister(testContext, "w2"); err != nil {
			t.Fatalf("Deregister failed: %v", err)
		}

		workers, err := r.GetWorkersForModel(testContext, "llama-8b")
		if err != nil {
			t.Fatal(err)
		}
		if len(workers) != 0 {
			t.Errorf("expected 0 workers for model, got %d", len(workers))
		}
	})

	t.Run("nonexistent returns error", func(t *testing.T) {
		err := r.Deregister(testContext, "nonexistent")
		if !errors.Is(err, ErrWorkerNotFound) {
			t.Fatalf("expected ErrWorkerNotFound, got %v", err)
		}
	})
}

func wRegistrationID(t *testing.T, r *WorkerRegistry, workerID string) string {
	t.Helper()
	worker, found, err := r.Get(testContext, workerID)
	if err != nil || !found {
		t.Fatalf("Get(%s): found=%t err=%v", workerID, found, err)
	}
	return worker.RegistrationID
}

func TestOperationsRespectCanceledContexts(t *testing.T) {
	r := newTestRegistry()
	if err := r.Register(testContext, makeWorker("w1", "a:1", types.WorkerStatusHealthy, []string{"old"})); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, expire := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer expire()

	operations := []struct {
		name string
		run  func(context.Context) error
	}{
		{name: "register", run: func(ctx context.Context) error {
			return r.Register(ctx, makeWorker("w2", "a:2", types.WorkerStatusHealthy, nil))
		}},
		{name: "deregister", run: func(ctx context.Context) error { return r.Deregister(ctx, "w1") }},
		{name: "get", run: func(ctx context.Context) error { _, _, err := r.Get(ctx, "w1"); return err }},
		{name: "model read", run: func(ctx context.Context) error { _, err := r.GetWorkersForModel(ctx, "old"); return err }},
		{name: "snapshot", run: func(ctx context.Context) error { _, err := r.Snapshot(ctx); return err }},
		{name: "stats write", run: func(ctx context.Context) error {
			return r.UpdateWorkerStats(ctx, "w1", types.WorkerStats{QueueDepth: 99})
		}},
		{name: "models write", run: func(ctx context.Context) error {
			return r.UpdateWorkerModels(ctx, "w1", []types.LoadedModel{{ModelID: "new"}})
		}},
		{name: "count", run: func(ctx context.Context) error { _, err := r.Count(ctx); return err }},
	}
	for _, operation := range operations {
		for _, contextCase := range []struct {
			name string
			ctx  context.Context
			want error
		}{{name: "canceled", ctx: canceled, want: context.Canceled}, {name: "deadline", ctx: expired, want: context.DeadlineExceeded}} {
			t.Run(operation.name+"/"+contextCase.name, func(t *testing.T) {
				if err := operation.run(contextCase.ctx); !errors.Is(err, contextCase.want) {
					t.Fatalf("expected %v, got %v", contextCase.want, err)
				}
			})
		}
	}

	worker, found, err := r.Get(testContext, "w1")
	if err != nil || !found {
		t.Fatalf("canceled writes removed worker: found=%t err=%v", found, err)
	}
	if worker.Stats.QueueDepth != 0 || !worker.HasModel("old") || worker.HasModel("new") {
		t.Fatalf("canceled writes mutated worker: %#v", worker)
	}
	if _, found, err := r.Get(testContext, "w2"); err != nil || found {
		t.Fatalf("canceled registration mutated registry: found=%t err=%v", found, err)
	}
}

func TestRegisterRechecksDeadlineAfterWaitingForLock(t *testing.T) {
	r := newTestRegistry()
	r.mu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- r.Register(ctx, makeWorker("blocked", "a:1", types.WorkerStatusHealthy, nil))
	}()
	<-ctx.Done()
	r.mu.Unlock()

	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded after lock wait, got %v", err)
	}
	if _, found, err := r.Get(testContext, "blocked"); err != nil || found {
		t.Fatalf("expired registration mutated registry: found=%t err=%v", found, err)
	}
}

func TestGet(t *testing.T) {
	r := newTestRegistry()
	w := makeWorker("w1", "localhost:8001", types.WorkerStatusHealthy, []string{"llama-8b"})
	if err := r.Register(testContext, w); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	t.Run("returns clone, not reference", func(t *testing.T) {
		got, found, err := r.Get(testContext, "w1")
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			t.Fatal("expected worker to be found")
		}
		if got.WorkerID != "w1" {
			t.Errorf("expected w1, got %s", got.WorkerID)
		}
		// Modify clone should not affect registry
		got.Address = "modified"
		original, _, err := r.Get(testContext, "w1")
		if err != nil {
			t.Fatal(err)
		}
		if original.Address == "modified" {
			t.Error("modifying clone affected registry")
		}
	})

	t.Run("nonexistent returns false", func(t *testing.T) {
		_, found, err := r.Get(testContext, "nonexistent")
		if err != nil {
			t.Fatal(err)
		}
		if found {
			t.Error("expected not found")
		}
	})
}

func TestGetWorkersForModel(t *testing.T) {
	r := newTestRegistry()

	if err := r.Register(testContext, makeWorker("w1", "a:1", types.WorkerStatusHealthy, []string{"llama-8b", "mistral-7b"})); err != nil {
		t.Fatalf("Register w1 failed: %v", err)
	}
	if err := r.Register(testContext, makeWorker("w2", "a:2", types.WorkerStatusHealthy, []string{"llama-8b"})); err != nil {
		t.Fatalf("Register w2 failed: %v", err)
	}
	if err := r.Register(testContext, makeWorker("w3", "a:3", types.WorkerStatusHealthy, []string{"mistral-7b"})); err != nil {
		t.Fatalf("Register w3 failed: %v", err)
	}

	t.Run("returns workers with model", func(t *testing.T) {
		workers, err := r.GetWorkersForModel(testContext, "llama-8b")
		if err != nil {
			t.Fatal(err)
		}
		if len(workers) != 2 {
			t.Errorf("expected 2 workers for llama-8b, got %d", len(workers))
		}
	})

	t.Run("returns nil for unknown model", func(t *testing.T) {
		workers, err := r.GetWorkersForModel(testContext, "unknown")
		if err != nil {
			t.Fatal(err)
		}
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

	if err := r.Register(testContext, healthy); err != nil {
		t.Fatalf("Register healthy failed: %v", err)
	}
	if err := r.Register(testContext, unhealthy); err != nil {
		t.Fatalf("Register unhealthy failed: %v", err)
	}
	if err := r.Register(testContext, overloaded); err != nil {
		t.Fatalf("Register overloaded failed: %v", err)
	}

	workers, err := r.GetHealthyWorkersForModel(testContext, "llama-8b")
	if err != nil {
		t.Fatal(err)
	}
	if len(workers) != 1 {
		t.Errorf("expected 1 healthy worker, got %d", len(workers))
	}
	if workers[0].WorkerID != "w1" {
		t.Errorf("expected w1, got %s", workers[0].WorkerID)
	}
}

func TestUpdateWorkerStats(t *testing.T) {
	r := newTestRegistry()
	if err := r.Register(testContext, makeWorker("w1", "a:1", types.WorkerStatusHealthy, nil)); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	stats := types.WorkerStats{
		GPUUtilization:    0.75,
		MemoryUsedBytes:   8_000_000_000,
		MemoryTotalBytes:  16_000_000_000,
		RequestsPerSecond: 10.5,
		AvgLatencyMS:      50,
	}

	if err := r.UpdateWorkerStats(testContext, "w1", stats); err != nil {
		t.Fatalf("UpdateWorkerStats failed: %v", err)
	}

	got, _, err := r.Get(testContext, "w1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Stats.GPUUtilization != 0.75 {
		t.Errorf("expected GPU util 0.75, got %f", got.Stats.GPUUtilization)
	}

	// Nonexistent worker
	if err := r.UpdateWorkerStats(testContext, "nonexistent", stats); !errors.Is(err, ErrWorkerNotFound) {
		t.Fatalf("expected ErrWorkerNotFound, got %v", err)
	}
}

func TestUpdateWorkerModels(t *testing.T) {
	r := newTestRegistry()
	if err := r.Register(testContext, makeWorker("w1", "a:1", types.WorkerStatusHealthy, []string{"old-model"})); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	newModels := []types.LoadedModel{
		{ModelID: "new-model-a", LoadedAt: time.Now()},
		{ModelID: "new-model-b", LoadedAt: time.Now()},
	}
	if err := r.UpdateWorkerModels(testContext, "w1", newModels); err != nil {
		t.Fatalf("UpdateWorkerModels failed: %v", err)
	}

	// Old model should no longer be indexed
	workers, err := r.GetWorkersForModel(testContext, "old-model")
	if err != nil {
		t.Fatal(err)
	}
	if len(workers) != 0 {
		t.Error("old model should be removed from index")
	}

	// New models should be indexed
	workers, err = r.GetWorkersForModel(testContext, "new-model-a")
	if err != nil {
		t.Fatal(err)
	}
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
	if err := r.Register(testContext, w); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go r.StartHealthChecker(ctx)

	// Wait for health checker to run
	time.Sleep(50 * time.Millisecond)
	cancel()

	// Worker should have been removed (heartbeat was 200ms old, threshold is 100ms)
	if workerCount(t, r) != 0 {
		t.Errorf("expected stale worker to be removed, got %d", workerCount(t, r))
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
		if err := r.Register(testContext, w); err != nil {
			t.Fatalf("register worker: %v", err)
		}

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
		if err := r.Register(testContext, w); err != nil {
			t.Fatalf("register worker: %v", err)
		}

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
		if workerCount(t, r) != 0 {
			t.Fatalf("expected worker to be removed, got count %d", workerCount(t, r))
		}
	})
}

func TestGetAllWorkers(t *testing.T) {
	r := newTestRegistry()
	if err := r.Register(testContext, makeWorker("w1", "a:1", types.WorkerStatusHealthy, nil)); err != nil {
		t.Fatalf("Register w1 failed: %v", err)
	}
	if err := r.Register(testContext, makeWorker("w2", "a:2", types.WorkerStatusUnhealthy, nil)); err != nil {
		t.Fatalf("Register w2 failed: %v", err)
	}

	all, err := r.GetAllWorkers(testContext)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 workers, got %d", len(all))
	}
}

func TestGetHealthyWorkers(t *testing.T) {
	r := newTestRegistry()
	if err := r.Register(testContext, makeWorker("w1", "a:1", types.WorkerStatusHealthy, nil)); err != nil {
		t.Fatalf("Register w1 failed: %v", err)
	}
	if err := r.Register(testContext, makeWorker("w2", "a:2", types.WorkerStatusDegraded, nil)); err != nil {
		t.Fatalf("Register w2 failed: %v", err)
	}
	if err := r.Register(testContext, makeWorker("w3", "a:3", types.WorkerStatusUnhealthy, nil)); err != nil {
		t.Fatalf("Register w3 failed: %v", err)
	}

	healthy, err := r.GetHealthyWorkers(testContext)
	if err != nil {
		t.Fatal(err)
	}
	if len(healthy) != 2 {
		t.Errorf("expected 2 healthy (healthy+degraded), got %d", len(healthy))
	}
}
