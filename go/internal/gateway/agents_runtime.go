package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/infera/infera/go/internal/agents"
	"github.com/infera/infera/go/internal/auth"
	"github.com/infera/infera/go/pkg/types"
)

type gatewayModelRunner struct {
	gateway *Gateway
}

func (r *gatewayModelRunner) Run(ctx context.Context, req agents.ModelRunRequest) (*types.InferenceResponse, error) {
	inferenceReq := r.gateway.newInferenceRequest(
		req.Run.Model,
		req.Messages,
		req.Parameters,
		false,
		"",
		req.Actor,
		req.Session,
		map[string]string{
			types.MetadataAgentID:    req.Run.AgentID,
			types.MetadataAgentRunID: req.Run.ID,
		},
	)
	result, err := r.gateway.executeNonStreamingInference(ctx, req.Actor, inferenceReq)
	if err != nil {
		return nil, err
	}
	return result.Response, nil
}

func (g *Gateway) NewAgentsRuntime(store *agents.Store) (*agents.Runtime, error) {
	runtime := agents.NewRuntime(store, &gatewayModelRunner{gateway: g})
	if err := runtime.RegisterDefinition(agents.NewHermesDefinition()); err != nil {
		return nil, err
	}

	if err := runtime.RegisterTool(agents.ToolDefinition{
		Name:        "list_models",
		Description: "List model ids and loaded/vault metadata available in Infera.",
		Handler: func(ctx context.Context, call agents.ToolCallContext, arguments json.RawMessage) (any, error) {
			return g.listModelEntries()
		},
	}); err != nil {
		return nil, err
	}

	if err := runtime.RegisterTool(agents.ToolDefinition{
		Name:        "list_workers",
		Description: "List worker runtime health, loaded models, queue depth, latency, and memory statistics.",
		Permission:  auth.PermissionViewInfrastructure,
		Handler: func(ctx context.Context, call agents.ToolCallContext, arguments json.RawMessage) (any, error) {
			return g.listWorkerEntries(), nil
		},
	}); err != nil {
		return nil, err
	}

	if err := runtime.RegisterTool(agents.ToolDefinition{
		Name:        "get_gateway_stats",
		Description: "Read the aggregated gateway statistics snapshot for workers, requests, latency, GPU, and memory.",
		Permission:  auth.PermissionViewInfrastructure,
		Handler: func(ctx context.Context, call agents.ToolCallContext, arguments json.RawMessage) (any, error) {
			return g.statsPayload(), nil
		},
	}); err != nil {
		return nil, err
	}

	if err := runtime.RegisterTool(agents.ToolDefinition{
		Name:        "list_instances",
		Description: "List provisioned instances visible to the current workspace.",
		Permission:  auth.PermissionViewInfrastructure,
		Handler: func(ctx context.Context, call agents.ToolCallContext, arguments json.RawMessage) (any, error) {
			if g.instanceHandlers == nil {
				return nil, fmt.Errorf("instance handlers are not configured")
			}
			return g.instanceHandlers.listInstanceEntriesForWorkspace(call.Run.WorkspaceID), nil
		},
	}); err != nil {
		return nil, err
	}

	if err := runtime.RegisterTool(agents.ToolDefinition{
		Name:        "list_deployments",
		Description: "List recent deployment attempts visible to the current workspace. Optional argument: {\"limit\": <1-100>}.",
		Permission:  auth.PermissionViewInfrastructure,
		Handler: func(ctx context.Context, call agents.ToolCallContext, arguments json.RawMessage) (any, error) {
			if g.instanceHandlers == nil {
				return nil, fmt.Errorf("instance handlers are not configured")
			}
			limit := 25
			if len(arguments) > 0 && strings.TrimSpace(string(arguments)) != "" {
				var req struct {
					Limit int `json:"limit"`
				}
				if err := json.Unmarshal(arguments, &req); err != nil {
					return nil, fmt.Errorf("invalid list_deployments arguments: %w", err)
				}
				if req.Limit > 0 {
					limit = req.Limit
				}
			}
			attempts, err := g.instanceHandlers.listDeploymentEntries(call.Run.WorkspaceID, limit)
			if err != nil {
				return nil, err
			}
			if attempts == nil {
				return []any{}, nil
			}
			return attempts, nil
		},
	}); err != nil {
		return nil, err
	}

	if err := runtime.RegisterTool(agents.ToolDefinition{
		Name:        "get_provider_status",
		Description: "Read provider connectivity, quota, and capability status for the current workspace.",
		Permission:  auth.PermissionViewInfrastructure,
		Handler: func(ctx context.Context, call agents.ToolCallContext, arguments json.RawMessage) (any, error) {
			if g.instanceHandlers == nil {
				return nil, fmt.Errorf("instance handlers are not configured")
			}
			return g.instanceHandlers.listProviderEntries(ctx, call.Run.WorkspaceID), nil
		},
	}); err != nil {
		return nil, err
	}

	if _, err := runtime.RecoverInterruptedRuns(); err != nil {
		return nil, err
	}

	return runtime, nil
}
