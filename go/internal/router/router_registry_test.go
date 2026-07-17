package router

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/infera/infera/go/pkg/types"
)

type stubWorkerRegistry struct {
	workers            map[string]*types.WorkerInfo
	healthCheckerCalls atomic.Int32
	snapshotCalls      atomic.Int32
	snapshotErr        error
	blockSnapshot      bool
}

func TestRouteFailsClosedWhenRegistrySnapshotFails(t *testing.T) {
	registry := newStubWorkerRegistry()
	registry.snapshotErr = errors.New("database address and secret detail")
	r := NewWithRegistry(DefaultConfig(), registry)
	defer r.Stop()

	_, err := r.Route(context.Background(), &types.InferenceRequest{
		RequestID: "req-registry-outage",
		ModelID:   "model-1",
	})
	var inferaErr *types.InferaError
	if !errors.As(err, &inferaErr) {
		t.Fatalf("expected typed registry error, got %v", err)
	}
	if inferaErr.Code != types.ErrorCodeWorkerRegistryUnavailable {
		t.Fatalf("expected worker_registry_unavailable, got %s", inferaErr.Code)
	}
	if strings.Contains(inferaErr.Message, "secret") {
		t.Fatalf("registry error leaked internal detail: %q", inferaErr.Message)
	}
	if calls := registry.snapshotCalls.Load(); calls != 1 {
		t.Fatalf("expected one failed snapshot attempt, got %d", calls)
	}
}

func TestRoutePreservesRegistryContextErrors(t *testing.T) {
	for _, contextErr := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(contextErr.Error(), func(t *testing.T) {
			workerState := newStubWorkerRegistry()
			workerState.snapshotErr = contextErr
			r := NewWithRegistry(DefaultConfig(), workerState)
			defer r.Stop()

			_, err := r.Route(context.Background(), &types.InferenceRequest{RequestID: "req-context", ModelID: "model-1"})
			if !errors.Is(err, contextErr) {
				t.Fatalf("expected %v, got %v", contextErr, err)
			}
		})
	}
}

func newStubWorkerRegistry(workers ...*types.WorkerInfo) *stubWorkerRegistry {
	indexed := make(map[string]*types.WorkerInfo, len(workers))
	for _, worker := range workers {
		indexed[worker.WorkerID] = worker.Clone()
	}
	return &stubWorkerRegistry{workers: indexed}
}

func (r *stubWorkerRegistry) Register(_ context.Context, worker *types.WorkerInfo) error {
	r.workers[worker.WorkerID] = worker.Clone()
	return nil
}

func (r *stubWorkerRegistry) Deregister(_ context.Context, workerID string) error {
	delete(r.workers, workerID)
	return nil
}

func (r *stubWorkerRegistry) UpdateWorkerStats(_ context.Context, workerID string, stats types.WorkerStats) error {
	if worker, ok := r.workers[workerID]; ok {
		worker.UpdateStats(stats)
	}
	return nil
}

func (r *stubWorkerRegistry) UpdateWorkerModels(_ context.Context, workerID string, models []types.LoadedModel) error {
	if worker, ok := r.workers[workerID]; ok {
		worker.LoadedModels = models
	}
	return nil
}

func (r *stubWorkerRegistry) Heartbeat(_ context.Context, _ string, workerID string, stats types.WorkerStats, models []types.LoadedModel, replaceModels bool) (*types.WorkerInfo, error) {
	worker, ok := r.workers[workerID]
	if !ok {
		return nil, errors.New("worker not found")
	}
	worker.UpdateStats(stats)
	if replaceModels {
		worker.LoadedModels = models
	}
	return worker.Clone(), nil
}

func (r *stubWorkerRegistry) Snapshot(ctx context.Context) ([]*types.WorkerInfo, error) {
	r.snapshotCalls.Add(1)
	if r.blockSnapshot {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if r.snapshotErr != nil {
		return nil, r.snapshotErr
	}
	var workers []*types.WorkerInfo
	for _, worker := range r.workers {
		workers = append(workers, worker.Clone())
	}
	return workers, nil
}

func TestBatchDispatchBoundsRegistrySnapshot(t *testing.T) {
	workerState := newStubWorkerRegistry()
	workerState.blockSnapshot = true
	config := DefaultConfig()
	config.RequestTimeoutMS = 20
	r := NewWithRegistry(config, workerState)
	defer r.Stop()

	started := time.Now()
	r.onBatchReady(&types.BatchContext{
		BatchID:   "batch-timeout",
		ModelID:   "model-1",
		CreatedAt: started,
	})
	elapsed := time.Since(started)
	if elapsed < 10*time.Millisecond || elapsed > 500*time.Millisecond {
		t.Fatalf("expected bounded batch registry lookup near configured timeout, elapsed %s", elapsed)
	}
	if calls := workerState.snapshotCalls.Load(); calls != 1 {
		t.Fatalf("expected one batch snapshot attempt, got %d", calls)
	}
}

func (r *stubWorkerRegistry) StartHealthChecker(ctx context.Context) {
	r.healthCheckerCalls.Add(1)
	<-ctx.Done()
}

func TestNewWithRegistryUsesInjectedWorkerRegistry(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EnableBatching = false

	registry := newStubWorkerRegistry(&types.WorkerInfo{
		WorkerID: "worker-1",
		Address:  "worker-1:8081",
		Status:   types.WorkerStatusHealthy,
		LoadedModels: []types.LoadedModel{
			{ModelID: "model-1"},
		},
	})

	r := NewWithRegistry(cfg, registry)
	defer r.Stop()

	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if registry.healthCheckerCalls.Load() == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if calls := registry.healthCheckerCalls.Load(); calls != 1 {
		t.Fatalf("expected injected registry health checker to start once, got %d", calls)
	}

	routed, err := r.Route(context.Background(), &types.InferenceRequest{
		RequestID: "req-injected",
		ModelID:   "model-1",
		Messages:  []types.Message{{Role: types.RoleUser, Content: "hello"}},
		Priority:  types.PriorityNormal,
	})
	if err != nil {
		t.Fatalf("route with injected registry: %v", err)
	}
	if routed.WorkerID != "worker-1" {
		t.Fatalf("expected injected registry worker-1, got %s", routed.WorkerID)
	}
	if calls := registry.snapshotCalls.Load(); calls != 1 {
		t.Fatalf("expected one registry snapshot for one route decision, got %d", calls)
	}
}
