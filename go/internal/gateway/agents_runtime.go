package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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

func attachmentByID(attachments []*agents.Attachment, attachmentID string) *agents.Attachment {
	attachmentID = strings.TrimSpace(attachmentID)
	for _, attachment := range attachments {
		if attachment.ID == attachmentID {
			return attachment
		}
	}
	return nil
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
			return g.listModelEntries(ctx)
		},
	}); err != nil {
		return nil, err
	}

	if err := runtime.RegisterTool(agents.ToolDefinition{
		Name:        "list_workers",
		Description: "List worker runtime health, loaded models, queue depth, latency, and memory statistics.",
		Permission:  auth.PermissionViewInfrastructure,
		Handler: func(ctx context.Context, call agents.ToolCallContext, arguments json.RawMessage) (any, error) {
			return g.listWorkerEntries(ctx, call.Run.WorkspaceID)
		},
	}); err != nil {
		return nil, err
	}

	if err := runtime.RegisterTool(agents.ToolDefinition{
		Name:        "get_gateway_stats",
		Description: "Read the aggregated gateway statistics snapshot for workers, requests, latency, GPU, and memory.",
		Permission:  auth.PermissionViewInfrastructure,
		Handler: func(ctx context.Context, call agents.ToolCallContext, arguments json.RawMessage) (any, error) {
			return g.statsPayload(ctx, call.Run.WorkspaceID)
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
			return g.instanceHandlers.listInstanceEntriesForWorkspace(call.Run.WorkspaceID)
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

	if err := runtime.RegisterTool(agents.ToolDefinition{
		Name:        "get_usage_summary",
		Description: "Read current-month workspace totals plus a 7-day daily trend for requests, tokens, successes, and errors.",
		Permission:  auth.PermissionViewUsage,
		Handler: func(ctx context.Context, call agents.ToolCallContext, arguments json.RawMessage) (any, error) {
			return g.usageSummaryPayload(call.Run.WorkspaceID, time.Now().UTC())
		},
	}); err != nil {
		return nil, err
	}

	if err := runtime.RegisterTool(agents.ToolDefinition{
		Name:        "get_quota_status",
		Description: "Read workspace quota settings and the current request or token pressure against this month's usage.",
		Permission:  auth.PermissionViewUsage,
		Handler: func(ctx context.Context, call agents.ToolCallContext, arguments json.RawMessage) (any, error) {
			return g.quotaStatusPayload(call.Run.WorkspaceID, time.Now().UTC())
		},
	}); err != nil {
		return nil, err
	}

	if err := runtime.RegisterTool(agents.ToolDefinition{
		Name:        "web_search",
		Description: "Search allowlisted official sources for external docs, status pages, or release notes. Arguments: {\"query\": string, \"topic\"?: string, \"max_results\"?: number}.",
		Modes:       []agents.RunMode{agents.RunModeResearch},
		Handler: func(ctx context.Context, call agents.ToolCallContext, arguments json.RawMessage) (any, error) {
			if g.webSearcher == nil {
				return nil, fmt.Errorf("web search is not configured")
			}
			var req struct {
				Query      string `json:"query"`
				Topic      string `json:"topic"`
				MaxResults int    `json:"max_results"`
			}
			if len(arguments) > 0 && strings.TrimSpace(string(arguments)) != "" {
				if err := json.Unmarshal(arguments, &req); err != nil {
					return nil, fmt.Errorf("invalid web_search arguments: %w", err)
				}
			}
			results, err := g.webSearcher.Search(ctx, WebSearchRequest{
				Query:      req.Query,
				Topic:      req.Topic,
				MaxResults: req.MaxResults,
			})
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"query":   strings.TrimSpace(req.Query),
				"topic":   strings.TrimSpace(req.Topic),
				"results": results,
			}, nil
		},
	}); err != nil {
		return nil, err
	}

	if err := runtime.RegisterTool(agents.ToolDefinition{
		Name:        "vision_analyze",
		Description: "Inspect an uploaded screenshot attachment. Arguments: {\"attachment_id\": string, \"question\"?: string, \"focus\"?: string}.",
		Modes:       []agents.RunMode{agents.RunModeMultimodal},
		Handler: func(ctx context.Context, call agents.ToolCallContext, arguments json.RawMessage) (any, error) {
			if g.visionAnalyzer == nil {
				return nil, fmt.Errorf("vision analysis is not configured")
			}
			var req struct {
				AttachmentID string `json:"attachment_id"`
				Question     string `json:"question"`
				Focus        string `json:"focus"`
			}
			if err := json.Unmarshal(arguments, &req); err != nil {
				return nil, fmt.Errorf("invalid vision_analyze arguments: %w", err)
			}
			attachment := attachmentByID(call.Attachments, req.AttachmentID)
			if attachment == nil {
				return nil, fmt.Errorf("attachment %q is unavailable for this run", req.AttachmentID)
			}
			return g.visionAnalyzer.Analyze(ctx, VisionAnalyzeRequest{
				Attachment: attachment,
				Question:   req.Question,
				Focus:      req.Focus,
			})
		},
	}); err != nil {
		return nil, err
	}

	if _, err := runtime.RecoverInterruptedRuns(); err != nil {
		return nil, err
	}

	return runtime, nil
}
