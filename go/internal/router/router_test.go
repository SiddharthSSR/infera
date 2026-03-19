package router

import (
	"context"
	"testing"
	"time"

	"github.com/infera/infera/go/pkg/types"
)

func TestRouteBatchedRequestsWaitForDispatchAndShareBatch(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EnableBatching = true
	cfg.MaxBatchSize = 2
	cfg.MaxBatchWaitMS = 500

	r := New(cfg)
	defer r.Stop()

	if err := r.RegisterWorker(&types.WorkerInfo{
		WorkerID: "worker-1",
		Address:  "worker-1:8081",
		Status:   types.WorkerStatusHealthy,
		LoadedModels: []types.LoadedModel{
			{ModelID: "model-1"},
		},
	}); err != nil {
		t.Fatalf("register worker: %v", err)
	}

	results := make(chan *types.RoutedRequest, 2)
	errs := make(chan error, 2)

	routeAsync := func(req *types.InferenceRequest) {
		routed, err := r.Route(context.Background(), req)
		if err != nil {
			errs <- err
			return
		}
		results <- routed
	}

	go routeAsync(&types.InferenceRequest{
		RequestID: "req-1",
		ModelID:   "model-1",
		Messages:  []types.Message{{Role: types.RoleUser, Content: "hello"}},
		Priority:  types.PriorityNormal,
	})

	select {
	case <-results:
		t.Fatal("expected first request to wait for batch dispatch")
	case err := <-errs:
		t.Fatalf("unexpected routing error: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	go routeAsync(&types.InferenceRequest{
		RequestID: "req-2",
		ModelID:   "model-1",
		Messages:  []types.Message{{Role: types.RoleUser, Content: "world"}},
		Priority:  types.PriorityNormal,
	})

	var routed []*types.RoutedRequest
	for len(routed) < 2 {
		select {
		case req := <-results:
			routed = append(routed, req)
		case err := <-errs:
			t.Fatalf("unexpected routing error: %v", err)
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for batched routes")
		}
	}

	if routed[0].BatchID == "" || routed[1].BatchID == "" {
		t.Fatalf("expected both requests to be batched, got %q and %q", routed[0].BatchID, routed[1].BatchID)
	}
	if routed[0].BatchID != routed[1].BatchID {
		t.Fatalf("expected shared batch id, got %q and %q", routed[0].BatchID, routed[1].BatchID)
	}
	if routed[0].BatchSize != 2 || routed[1].BatchSize != 2 {
		t.Fatalf("expected batch size 2 on both routed requests, got %d and %d", routed[0].BatchSize, routed[1].BatchSize)
	}
	if routed[0].BatchWaitMS < 0 || routed[1].BatchWaitMS < 0 {
		t.Fatalf("expected non-negative batch wait, got %d and %d", routed[0].BatchWaitMS, routed[1].BatchWaitMS)
	}
	if routed[0].WorkerID != "worker-1" || routed[1].WorkerID != "worker-1" {
		t.Fatalf("expected batched requests to route to worker-1, got %q and %q", routed[0].WorkerID, routed[1].WorkerID)
	}
}

func TestRoutePrefersAffinityWorkerWhenHealthy(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EnableBatching = false

	r := New(cfg)
	defer r.Stop()

	worker1 := &types.WorkerInfo{
		WorkerID: "worker-1",
		Address:  "worker-1:8081",
		Status:   types.WorkerStatusHealthy,
		LoadedModels: []types.LoadedModel{
			{ModelID: "model-1"},
		},
		Stats: types.WorkerStats{
			GPUUtilization:   0.20,
			MemoryTotalBytes: 100,
			MemoryUsedBytes:  20,
		},
	}
	worker2 := &types.WorkerInfo{
		WorkerID: "worker-2",
		Address:  "worker-2:8081",
		Status:   types.WorkerStatusHealthy,
		LoadedModels: []types.LoadedModel{
			{ModelID: "model-1"},
		},
		Stats: types.WorkerStats{
			GPUUtilization:   0.60,
			MemoryTotalBytes: 100,
			MemoryUsedBytes:  50,
		},
	}

	if err := r.RegisterWorker(worker1); err != nil {
		t.Fatalf("register worker1: %v", err)
	}
	if err := r.RegisterWorker(worker2); err != nil {
		t.Fatalf("register worker2: %v", err)
	}

	first, err := r.Route(context.Background(), &types.InferenceRequest{
		RequestID: "req-1",
		ModelID:   "model-1",
		Messages:  []types.Message{{Role: types.RoleUser, Content: "hello"}},
		Priority:  types.PriorityNormal,
		Metadata: map[string]string{
			types.MetadataAffinityKey: "affinity-1",
		},
	})
	if err != nil {
		t.Fatalf("first route: %v", err)
	}
	if first.WorkerID != "worker-1" {
		t.Fatalf("expected first request to choose worker-1, got %s", first.WorkerID)
	}

	if err := r.UpdateWorkerStats("worker-1", types.WorkerStats{
		GPUUtilization:   0.70,
		MemoryTotalBytes: 100,
		MemoryUsedBytes:  70,
	}); err != nil {
		t.Fatalf("update worker1 stats: %v", err)
	}
	if err := r.UpdateWorkerStats("worker-2", types.WorkerStats{
		GPUUtilization:   0.05,
		MemoryTotalBytes: 100,
		MemoryUsedBytes:  10,
	}); err != nil {
		t.Fatalf("update worker2 stats: %v", err)
	}

	second, err := r.Route(context.Background(), &types.InferenceRequest{
		RequestID: "req-2",
		ModelID:   "model-1",
		Messages:  []types.Message{{Role: types.RoleUser, Content: "hello again"}},
		Priority:  types.PriorityNormal,
		Metadata: map[string]string{
			types.MetadataAffinityKey: "affinity-1",
		},
	})
	if err != nil {
		t.Fatalf("second route: %v", err)
	}
	if second.WorkerID != "worker-1" {
		t.Fatalf("expected affinity to keep worker-1, got %s", second.WorkerID)
	}
	if second.RoutingDecision.Strategy != types.StrategyAffinity {
		t.Fatalf("expected affinity strategy, got %s", second.RoutingDecision.Strategy)
	}
}

func TestRouteFallsBackWhenAffinityWorkerLosesCapacity(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EnableBatching = false

	r := New(cfg)
	defer r.Stop()

	for _, worker := range []*types.WorkerInfo{
		{
			WorkerID: "worker-1",
			Address:  "worker-1:8081",
			Status:   types.WorkerStatusHealthy,
			LoadedModels: []types.LoadedModel{
				{ModelID: "model-1"},
			},
			Stats: types.WorkerStats{
				GPUUtilization:   0.10,
				MemoryTotalBytes: 100,
				MemoryUsedBytes:  10,
			},
		},
		{
			WorkerID: "worker-2",
			Address:  "worker-2:8081",
			Status:   types.WorkerStatusHealthy,
			LoadedModels: []types.LoadedModel{
				{ModelID: "model-1"},
			},
			Stats: types.WorkerStats{
				GPUUtilization:   0.20,
				MemoryTotalBytes: 100,
				MemoryUsedBytes:  20,
			},
		},
	} {
		if err := r.RegisterWorker(worker); err != nil {
			t.Fatalf("register worker %s: %v", worker.WorkerID, err)
		}
	}

	first, err := r.Route(context.Background(), &types.InferenceRequest{
		RequestID: "req-1",
		ModelID:   "model-1",
		Messages:  []types.Message{{Role: types.RoleUser, Content: "hello"}},
		Priority:  types.PriorityNormal,
		Metadata: map[string]string{
			types.MetadataAffinityKey: "affinity-2",
		},
	})
	if err != nil {
		t.Fatalf("first route: %v", err)
	}
	if first.WorkerID != "worker-1" {
		t.Fatalf("expected worker-1, got %s", first.WorkerID)
	}

	if err := r.UpdateWorkerStats("worker-1", types.WorkerStats{
		QueueDepth:       500,
		GPUUtilization:   0.95,
		MemoryTotalBytes: 100,
		MemoryUsedBytes:  95,
	}); err != nil {
		t.Fatalf("update worker1 overloaded stats: %v", err)
	}

	second, err := r.Route(context.Background(), &types.InferenceRequest{
		RequestID: "req-2",
		ModelID:   "model-1",
		Messages:  []types.Message{{Role: types.RoleUser, Content: "hello retry"}},
		Priority:  types.PriorityNormal,
		Metadata: map[string]string{
			types.MetadataAffinityKey: "affinity-2",
		},
	})
	if err != nil {
		t.Fatalf("second route: %v", err)
	}
	if second.WorkerID != "worker-2" {
		t.Fatalf("expected fallback to worker-2, got %s", second.WorkerID)
	}
}
