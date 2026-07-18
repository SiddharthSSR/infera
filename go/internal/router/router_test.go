package router

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/infera/infera/go/pkg/types"
)

// TestRouteSoloRequestAvoidsFullBatchWait verifies that a single non-streaming
// request no longer waits the full MaxBatchWaitMS window before dispatch.
func TestRouteSoloRequestAvoidsFullBatchWait(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EnableBatching = true
	cfg.MaxBatchSize = 8
	cfg.MaxBatchWaitMS = 500 // solo requests should dispatch far sooner than this

	r := New(cfg)
	defer r.Stop()

	if err := r.RegisterWorker(context.Background(), &types.WorkerInfo{
		WorkerID: "worker-1",
		Address:  "worker-1:8081",
		Status:   types.WorkerStatusHealthy,
		LoadedModels: []types.LoadedModel{
			{ModelID: "model-1"},
		},
	}); err != nil {
		t.Fatalf("register worker: %v", err)
	}

	start := time.Now()
	routed, err := r.Route(context.Background(), &types.InferenceRequest{
		RequestID: "req-1",
		ModelID:   "model-1",
		Messages:  []types.Message{{Role: types.RoleUser, Content: "hello"}},
		Priority:  types.PriorityNormal,
	})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if routed.WorkerID != "worker-1" {
		t.Fatalf("expected worker-1, got %s", routed.WorkerID)
	}
	// Should complete well under the full batch wait window because the
	// first-request batch timeout is shortened.
	if elapsed > 200*time.Millisecond {
		t.Fatalf("expected shortened route wait (<200ms), took %v", elapsed)
	}
}

func TestRouteDecisionIncludesSelectedWorkerSignals(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EnableBatching = false

	r := New(cfg)
	defer r.Stop()

	if err := r.RegisterWorker(context.Background(), &types.WorkerInfo{
		WorkerID: "worker-1",
		Address:  "worker-1:8081",
		Status:   types.WorkerStatusHealthy,
		Tags: map[string]string{
			"provider": "runpod",
			"gpu_type": "A100_80GB",
		},
		LoadedModels: []types.LoadedModel{{ModelID: "model-1"}},
		Stats: types.WorkerStats{
			QueueDepth:     2,
			ActiveRequests: 1,
			GPUUtilization: 0.25,
			P50LatencyMS:   623,
			P99LatencyMS:   900,
		},
	}); err != nil {
		t.Fatalf("register worker: %v", err)
	}

	routed, err := r.Route(context.Background(), &types.InferenceRequest{
		RequestID: "req-123",
		ModelID:   "model-1",
		Messages:  []types.Message{{Role: types.RoleUser, Content: "secret prompt"}},
		APIKeyID:  "sk-secret",
		Priority:  types.PriorityNormal,
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}

	decision := routed.RoutingDecision
	if decision.RequestID != "req-123" {
		t.Fatalf("expected request id req-123, got %q", decision.RequestID)
	}
	if decision.Model != "model-1" {
		t.Fatalf("expected model-1, got %q", decision.Model)
	}
	if decision.Strategy != types.StrategyLeastLoaded {
		t.Fatalf("expected least_loaded strategy, got %s", decision.Strategy)
	}
	if decision.SelectedWorker != "worker-1" {
		t.Fatalf("expected selected worker worker-1, got %q", decision.SelectedWorker)
	}
	if decision.SelectedProvider != "runpod" {
		t.Fatalf("expected provider runpod, got %q", decision.SelectedProvider)
	}
	if decision.SelectedGPUType != "A100_80GB" {
		t.Fatalf("expected gpu type A100_80GB, got %q", decision.SelectedGPUType)
	}
	if decision.CandidatesEvaluated != 1 {
		t.Fatalf("expected candidates evaluated=1, got %d", decision.CandidatesEvaluated)
	}
	if decision.WorkerQueueDepth == nil || *decision.WorkerQueueDepth != 2 {
		t.Fatalf("expected worker queue depth=2, got %#v", decision.WorkerQueueDepth)
	}
	if decision.WorkerActiveRequests == nil || *decision.WorkerActiveRequests != 1 {
		t.Fatalf("expected active requests=1, got %#v", decision.WorkerActiveRequests)
	}
	if decision.WorkerP50LatencyMS == nil || *decision.WorkerP50LatencyMS != 623 {
		t.Fatalf("expected p50 latency=623, got %#v", decision.WorkerP50LatencyMS)
	}
	if decision.WorkerP99LatencyMS == nil || *decision.WorkerP99LatencyMS != 900 {
		t.Fatalf("expected p99 latency=900, got %#v", decision.WorkerP99LatencyMS)
	}
	if decision.WorkerLoad == nil {
		t.Fatalf("expected worker load to be captured")
	}
	if decision.DecisionTimestamp.IsZero() {
		t.Fatalf("expected decision timestamp")
	}
}

func TestRouteMinCostUnderLatencySLOUsesTrustedResolver(t *testing.T) {
	now := time.Now().UTC()
	costs := map[string]int64{"worker-cheap": 400_000_000, "worker-fast": 900_000_000}
	resolved := make(map[string]int)
	cfg := DefaultConfig()
	cfg.EnableBatching = false
	cfg.DefaultStrategy = types.StrategyMinCostUnderLatencySLO
	cfg.LatencySLOMS = 500
	cfg.CostResolver = func(workerID string) (CostEvidence, bool, error) {
		resolved[workerID]++
		amount, ok := costs[workerID]
		return CostEvidence{AmountNanoPerHour: amount}, ok, nil
	}

	r := New(cfg)
	defer r.Stop()
	for _, worker := range []*types.WorkerInfo{
		{
			WorkerID: "worker-fast", Address: "fast:8081", Status: types.WorkerStatusHealthy,
			LoadedModels: []types.LoadedModel{{ModelID: "model-1"}},
			Stats:        types.WorkerStats{P99LatencyMS: 100, UpdatedAt: now},
			Tags:         map[string]string{"hourly_cost": "0.01"},
		},
		{
			WorkerID: "worker-cheap", Address: "cheap:8081", Status: types.WorkerStatusHealthy,
			LoadedModels: []types.LoadedModel{{ModelID: "model-1"}},
			Stats:        types.WorkerStats{P99LatencyMS: 450, UpdatedAt: now},
			Tags:         map[string]string{"hourly_cost": "9999"},
		},
	} {
		if err := r.RegisterWorker(context.Background(), worker); err != nil {
			t.Fatalf("register %s: %v", worker.WorkerID, err)
		}
	}

	routed, err := r.Route(context.Background(), &types.InferenceRequest{RequestID: "req-cost", ModelID: "model-1"})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if routed.WorkerID != "worker-cheap" {
		t.Fatalf("expected trusted resolver to select worker-cheap, got %s", routed.WorkerID)
	}
	if resolved["worker-cheap"] != 1 || resolved["worker-fast"] != 1 {
		t.Fatalf("expected authoritative lookup for both candidates, got %#v", resolved)
	}
	decision := routed.RoutingDecision
	if decision.SelectedCostNanoPerHour == nil || *decision.SelectedCostNanoPerHour != costs["worker-cheap"] {
		t.Fatalf("unexpected cost evidence: %+v", decision)
	}
}

func TestRouteMinCostUnderLatencySLOReturnsOverloadedWhenAllWorkersExceedSLO(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EnableBatching = false
	cfg.DefaultStrategy = types.StrategyMinCostUnderLatencySLO
	cfg.LatencySLOMS = 100
	cfg.CostResolver = func(string) (CostEvidence, bool, error) {
		return CostEvidence{AmountNanoPerHour: 400_000_000}, true, nil
	}
	r := New(cfg)
	defer r.Stop()
	if err := r.RegisterWorker(context.Background(), &types.WorkerInfo{
		WorkerID: "worker-over-slo", Address: "worker:8081", Status: types.WorkerStatusHealthy,
		LoadedModels: []types.LoadedModel{{ModelID: "model-1"}},
		Stats:        types.WorkerStats{P99LatencyMS: 101, UpdatedAt: time.Now().UTC()},
	}); err != nil {
		t.Fatalf("register worker: %v", err)
	}

	_, err := r.Route(context.Background(), &types.InferenceRequest{RequestID: "req-over-slo", ModelID: "model-1"})
	var inferaErr *types.InferaError
	if !errors.As(err, &inferaErr) || inferaErr.Code != types.ErrorCodeModelOverloaded {
		t.Fatalf("expected model_overloaded, got %T: %v", err, err)
	}
}

func TestRouteDecisionDoesNotExposePromptOrAPIKey(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EnableBatching = false

	r := New(cfg)
	defer r.Stop()

	if err := r.RegisterWorker(context.Background(), &types.WorkerInfo{
		WorkerID:     "worker-1",
		Address:      "worker-1:8081",
		Status:       types.WorkerStatusHealthy,
		LoadedModels: []types.LoadedModel{{ModelID: "model-1"}},
	}); err != nil {
		t.Fatalf("register worker: %v", err)
	}

	routed, err := r.Route(context.Background(), &types.InferenceRequest{
		RequestID: "req-123",
		ModelID:   "model-1",
		Messages:  []types.Message{{Role: types.RoleUser, Content: "do not log this prompt"}},
		APIKeyID:  "sk-do-not-log",
		Priority:  types.PriorityNormal,
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}

	payload, err := json.Marshal(routed.RoutingDecision)
	if err != nil {
		t.Fatalf("marshal decision: %v", err)
	}
	body := string(payload)
	for _, forbidden := range []string{"do not log this prompt", "sk-do-not-log"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("route decision leaked %q in %s", forbidden, body)
		}
	}
}

func TestRouteBatchedRequestsShareBatch(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EnableBatching = true
	cfg.MaxBatchSize = 2
	cfg.MaxBatchWaitMS = 500

	r := New(cfg)
	defer r.Stop()

	if err := r.RegisterWorker(context.Background(), &types.WorkerInfo{
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
	for _, rr := range routed {
		if rr.WorkerID != "worker-1" {
			t.Fatalf("expected worker-1, got %s", rr.WorkerID)
		}
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

	if err := r.RegisterWorker(context.Background(), worker1); err != nil {
		t.Fatalf("register worker1: %v", err)
	}
	if err := r.RegisterWorker(context.Background(), worker2); err != nil {
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

	if err := r.UpdateWorkerStats(context.Background(), "worker-1", types.WorkerStats{
		GPUUtilization:   0.70,
		MemoryTotalBytes: 100,
		MemoryUsedBytes:  70,
	}); err != nil {
		t.Fatalf("update worker1 stats: %v", err)
	}
	if err := r.UpdateWorkerStats(context.Background(), "worker-2", types.WorkerStats{
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
		if err := r.RegisterWorker(context.Background(), worker); err != nil {
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

	if err := r.UpdateWorkerStats(context.Background(), "worker-1", types.WorkerStats{
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
