package router

import (
	"context"
	"testing"

	"github.com/infera/infera/go/pkg/types"
)

func TestRouteFiltersSameModelWorkersByWorkspace(t *testing.T) {
	config := DefaultConfig()
	config.EnableBatching = false
	r := New(config)
	defer r.Stop()
	for _, worker := range []*types.WorkerInfo{
		{WorkerID: "a", WorkspaceID: "ws_a", Status: types.WorkerStatusHealthy, LoadedModels: []types.LoadedModel{{ModelID: "shared"}}},
		{WorkerID: "b", WorkspaceID: "ws_b", Status: types.WorkerStatusHealthy, LoadedModels: []types.LoadedModel{{ModelID: "shared"}}},
	} {
		if err := r.RegisterWorker(worker); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ workspace, want string }{{"ws_a", "a"}, {"ws_b", "b"}} {
		routed, err := r.Route(context.Background(), &types.InferenceRequest{ModelID: "shared", WorkspaceID: tc.workspace})
		if err != nil {
			t.Fatalf("route %s: %v", tc.workspace, err)
		}
		if routed.WorkerID != tc.want {
			t.Fatalf("workspace %s routed to %s, want %s", tc.workspace, routed.WorkerID, tc.want)
		}
	}
}

func TestAffinityCannotCrossWorkspace(t *testing.T) {
	config := DefaultConfig()
	config.EnableBatching = false
	r := New(config)
	defer r.Stop()
	if err := r.RegisterWorker(&types.WorkerInfo{WorkerID: "a", WorkspaceID: "ws_a", Status: types.WorkerStatusHealthy, LoadedModels: []types.LoadedModel{{ModelID: "shared"}}}); err != nil {
		t.Fatal(err)
	}
	reqA := &types.InferenceRequest{ModelID: "shared", WorkspaceID: "ws_a", Metadata: map[string]string{types.MetadataAffinityKey: "same"}}
	if _, err := r.Route(context.Background(), reqA); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Route(context.Background(), &types.InferenceRequest{ModelID: "shared", WorkspaceID: "ws_b", Metadata: map[string]string{types.MetadataAffinityKey: "same"}}); err == nil {
		t.Fatal("expected workspace B to have no eligible worker")
	}
}
