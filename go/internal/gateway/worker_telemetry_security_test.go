package gateway

import (
	"context"
	"testing"

	"github.com/infera/infera/go/internal/router"
	"github.com/infera/infera/go/pkg/types"
)

func TestWorkerTelemetryIsWorkspaceScoped(t *testing.T) {
	r := router.New(router.DefaultConfig())
	defer r.Stop()
	for _, worker := range []*types.WorkerInfo{
		{WorkerID: "a", WorkspaceID: "ws_a", Status: types.WorkerStatusHealthy, LoadedModels: []types.LoadedModel{{ModelID: "ma"}}, Stats: types.WorkerStats{QueueDepth: 2}},
		{WorkerID: "b", WorkspaceID: "ws_b", Status: types.WorkerStatusHealthy, LoadedModels: []types.LoadedModel{{ModelID: "mb"}}, Stats: types.WorkerStats{QueueDepth: 99}},
	} {
		if err := r.RegisterWorker(context.Background(), worker); err != nil {
			t.Fatal(err)
		}
	}
	g := New(DefaultConfig(), r, nil)
	entries, err := g.listWorkerEntries(context.Background(), "ws_a")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0]["worker_id"] != "a" {
		t.Fatalf("unexpected workspace worker view: %#v", entries)
	}
	stats, err := g.statsPayload(context.Background(), "ws_a")
	if err != nil {
		t.Fatal(err)
	}
	requests := stats["requests"].(map[string]interface{})
	if requests["queue_depth"] != 2 {
		t.Fatalf("cross-workspace queue depth leaked into stats: %#v", requests)
	}
}
