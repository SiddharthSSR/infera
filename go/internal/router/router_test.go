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
