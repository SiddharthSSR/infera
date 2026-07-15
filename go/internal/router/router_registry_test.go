package router

import (
	"context"
	"testing"
	"time"

	"github.com/infera/infera/go/pkg/types"
)

type stubWorkerRegistry struct {
	workers            map[string]*types.WorkerInfo
	healthCheckerCalls int
}

func newStubWorkerRegistry(workers ...*types.WorkerInfo) *stubWorkerRegistry {
	indexed := make(map[string]*types.WorkerInfo, len(workers))
	for _, worker := range workers {
		indexed[worker.WorkerID] = worker.Clone()
	}
	return &stubWorkerRegistry{workers: indexed}
}

func (r *stubWorkerRegistry) Register(worker *types.WorkerInfo) error {
	r.workers[worker.WorkerID] = worker.Clone()
	return nil
}

func (r *stubWorkerRegistry) Deregister(workerID string) error {
	delete(r.workers, workerID)
	return nil
}

func (r *stubWorkerRegistry) UpdateWorkerStats(workerID string, stats types.WorkerStats) error {
	if worker, ok := r.workers[workerID]; ok {
		worker.UpdateStats(stats)
	}
	return nil
}

func (r *stubWorkerRegistry) UpdateWorkerModels(workerID string, models []types.LoadedModel) error {
	if worker, ok := r.workers[workerID]; ok {
		worker.LoadedModels = models
	}
	return nil
}

func (r *stubWorkerRegistry) Get(workerID string) (*types.WorkerInfo, bool) {
	worker, ok := r.workers[workerID]
	if !ok {
		return nil, false
	}
	return worker.Clone(), true
}

func (r *stubWorkerRegistry) GetWorkersForModel(modelID string) []*types.WorkerInfo {
	var workers []*types.WorkerInfo
	for _, worker := range r.workers {
		if worker.HasModel(modelID) {
			workers = append(workers, worker.Clone())
		}
	}
	return workers
}

func (r *stubWorkerRegistry) GetHealthyWorkersForModel(modelID string) []*types.WorkerInfo {
	var workers []*types.WorkerInfo
	for _, worker := range r.workers {
		if worker.IsHealthy() && worker.HasModel(modelID) {
			workers = append(workers, worker.Clone())
		}
	}
	return workers
}

func (r *stubWorkerRegistry) GetAllWorkers() []*types.WorkerInfo {
	var workers []*types.WorkerInfo
	for _, worker := range r.workers {
		workers = append(workers, worker.Clone())
	}
	return workers
}

func (r *stubWorkerRegistry) GetHealthyWorkers() []*types.WorkerInfo {
	var workers []*types.WorkerInfo
	for _, worker := range r.workers {
		if worker.IsHealthy() {
			workers = append(workers, worker.Clone())
		}
	}
	return workers
}

func (r *stubWorkerRegistry) Count() int {
	return len(r.workers)
}

func (r *stubWorkerRegistry) StartHealthChecker(ctx context.Context) {
	r.healthCheckerCalls++
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
		if registry.healthCheckerCalls == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if registry.healthCheckerCalls != 1 {
		t.Fatalf("expected injected registry health checker to start once, got %d", registry.healthCheckerCalls)
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
}
