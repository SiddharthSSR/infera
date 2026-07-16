package gateway

import (
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
		if err := r.RegisterWorker(worker); err != nil { t.Fatal(err) }
	}
	g := New(DefaultConfig(), r, nil)
	entries := g.listWorkerEntries("ws_a")
	if len(entries) != 1 || entries[0]["worker_id"] != "a" { t.Fatalf("unexpected workspace worker view: %#v", entries) }
	stats := g.statsPayload("ws_a")
	requests := stats["requests"].(map[string]interface{})
	if requests["queue_depth"] != 2 { t.Fatalf("cross-workspace queue depth leaked into stats: %#v", requests) }
}
