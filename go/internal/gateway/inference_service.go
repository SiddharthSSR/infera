package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/infera/infera/go/internal/audit"
	"github.com/infera/infera/go/internal/auth"
	"github.com/infera/infera/go/pkg/types"
)

type inferenceExecutionResult struct {
	Response        *types.InferenceResponse
	WorkerID        string
	RoutingDecision types.RoutingDecision
}

func hashMessages(messages []types.Message) string {
	chatMessages := make([]ChatMessage, len(messages))
	for i, msg := range messages {
		chatMessages[i] = ChatMessage{
			Role:    string(msg.Role),
			Content: msg.Content,
			Name:    msg.Name,
		}
	}
	return hashPrompt(chatMessages)
}

func hashMessagePrefix(messages []types.Message, maxBytes int) string {
	chatMessages := make([]ChatMessage, len(messages))
	for i, msg := range messages {
		chatMessages[i] = ChatMessage{
			Role:    string(msg.Role),
			Content: msg.Content,
			Name:    msg.Name,
		}
	}
	return hashPromptPrefix(chatMessages, maxBytes)
}

func buildAffinityMetadataFromMessages(modelID string, messages []types.Message, explicitAffinity string, key *auth.KeyRecord, session *auth.SessionRecord) map[string]string {
	const prefixHashBytes = 1024

	metadata := map[string]string{}
	prefixHash := hashMessagePrefix(messages, prefixHashBytes)
	if prefixHash != "" {
		metadata[types.MetadataPromptPrefixHash] = prefixHash
	}

	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		modelID = "unknown-model"
	}
	switch {
	case strings.TrimSpace(explicitAffinity) != "":
		metadata[types.MetadataAffinityKey] = fmt.Sprintf("explicit:%s:%s", modelID, strings.TrimSpace(explicitAffinity))
		metadata[types.MetadataAffinitySource] = types.MetadataExplicitAffinity
	case session != nil:
		metadata[types.MetadataAffinityKey] = fmt.Sprintf("session:%s:%s:%s", session.ID, modelID, prefixHash)
		metadata[types.MetadataAffinitySource] = types.MetadataSessionAffinity
	case key != nil:
		metadata[types.MetadataAffinityKey] = fmt.Sprintf("key:%s:%s:%s", key.ID, modelID, prefixHash)
		metadata[types.MetadataAffinitySource] = types.MetadataAPIKeyAffinity
	}
	if len(metadata) == 0 || strings.TrimSpace(metadata[types.MetadataAffinityKey]) == "" {
		return nil
	}
	return metadata
}

func mergeInferenceMetadata(base, overlay map[string]string) map[string]string {
	switch {
	case len(base) == 0 && len(overlay) == 0:
		return nil
	case len(base) == 0:
		out := make(map[string]string, len(overlay))
		for k, v := range overlay {
			out[k] = v
		}
		return out
	case len(overlay) == 0:
		out := make(map[string]string, len(base))
		for k, v := range base {
			out[k] = v
		}
		return out
	default:
		out := make(map[string]string, len(base)+len(overlay))
		for k, v := range base {
			out[k] = v
		}
		for k, v := range overlay {
			out[k] = v
		}
		return out
	}
}

func (g *Gateway) newInferenceRequest(model string, messages []types.Message, params types.InferenceParameters, stream bool, explicitAffinity string, key *auth.KeyRecord, session *auth.SessionRecord, metadata map[string]string) *types.InferenceRequest {
	request := &types.InferenceRequest{
		RequestID:  uuid.New().String(),
		ModelID:    model,
		Messages:   messages,
		Parameters: params,
		Stream:     stream,
		Priority:   types.PriorityNormal,
		Metadata:   mergeInferenceMetadata(buildAffinityMetadataFromMessages(model, messages, explicitAffinity, key, session), metadata),
		CreatedAt:  time.Now(),
	}
	if key != nil {
		request.APIKeyID = key.ID
		request.WorkspaceID = normalizeWorkspaceIDForGateway(key.WorkspaceID)
	} else {
		request.WorkspaceID = auth.DefaultWorkspaceID
	}
	return request
}

func (g *Gateway) executeNonStreamingInference(ctx context.Context, key *auth.KeyRecord, req *types.InferenceRequest) (*inferenceExecutionResult, error) {
	current := atomic.AddInt64(&g.inFlightRequests, 1)
	defer atomic.AddInt64(&g.inFlightRequests, -1)

	if current > g.maxInFlightDefault {
		if g.metrics != nil {
			g.metrics.RecordInferenceRejected("overloaded")
		}
		return nil, types.NewInferaError(types.ErrorCode("overloaded"), "Server is overloaded. Please retry shortly.")
	}

	requestStart := time.Now()
	requestID := req.RequestID
	keyID := ""
	workspaceID := ""
	if key != nil {
		keyID = key.KeyPrefix
		workspaceID = key.WorkspaceID
	}
	promptHash := hashMessages(req.Messages)
	auditStatus := "unknown_error"
	auditTokenCount := 0
	auditUsage := usageMeasurement{TokenSource: audit.TokenSourceUnknown}
	auditWorkerID := ""
	auditErrorCode := ""
	defer func() {
		latencyMS := time.Since(requestStart).Milliseconds()
		attrs := []any{
			slog.String("request_id", requestID),
			slog.String("key_id", keyID),
			slog.String("model", req.ModelID),
			slog.String("worker_id", auditWorkerID),
			slog.Bool("stream", false),
			slog.Int("message_count", len(req.Messages)),
			slog.Int("token_count", auditTokenCount),
			slog.String("token_source", auditUsage.TokenSource),
			slog.String("prompt_hash", promptHash),
			slog.String("status", auditStatus),
			slog.Int64("latency_ms", latencyMS),
		}
		if auditErrorCode != "" {
			attrs = append(attrs, slog.String("error_code", auditErrorCode))
		}
		g.log.Info("inference.audit", attrs...)

		if g.auditCh != nil {
			rec := audit.InferenceAuditRecord{
				Timestamp:        requestStart.UTC(),
				RequestID:        requestID,
				KeyID:            keyID,
				WorkspaceID:      workspaceID,
				Model:            req.ModelID,
				WorkerID:         auditWorkerID,
				Stream:           false,
				MessageCount:     len(req.Messages),
				PromptTokens:     auditUsage.PromptTokens,
				CompletionTokens: auditUsage.CompletionTokens,
				TokenCount:       auditTokenCount,
				TokenSource:      auditUsage.TokenSource,
				PromptHash:       promptHash,
				Status:           auditStatus,
				ErrorCode:        auditErrorCode,
				LatencyMS:        latencyMS,
			}
			if err := g.enqueueAuditRecord(rec); err != nil {
				g.log.Error("inference.audit_persist_failed", slog.String("request_id", requestID), slog.String("error", err.Error()))
			}
		}

		if g.metrics != nil {
			g.metrics.RecordInference(false, auditStatus, auditTokenCount, time.Since(requestStart))
		}
	}()

	timeoutCtx, cancel := context.WithTimeout(ctx, g.config.InferenceTimeout)
	defer cancel()

	if err := g.enforceWorkspaceQuotaForKey(key, req); err != nil {
		auditStatus = "failed"
		auditErrorCode = string(err.Code)
		return nil, err
	}

	routed, err := g.router.Route(timeoutCtx, req)
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			auditStatus = "client_canceled"
			return nil, err
		case errors.Is(err, context.DeadlineExceeded):
			auditStatus = "failed"
			auditErrorCode = "inference_timeout"
			return nil, types.NewInferaError(types.ErrorCode("inference_timeout"), "Inference request timed out")
		}
		if inferaErr, ok := err.(*types.InferaError); ok {
			auditStatus = "failed"
			auditErrorCode = string(inferaErr.Code)
			g.logRouteDecisionFailed(req, string(inferaErr.Code), inferaErr.Message)
			return nil, inferaErr
		}
		auditStatus = "failed"
		auditErrorCode = "no_workers"
		g.logRouteDecisionFailed(req, "no_workers", err.Error())
		return nil, types.NewInferaError(types.ErrorCode("no_workers"), "No healthy workers available for model: "+req.ModelID)
	}
	g.logRouteDecision(routed.RoutingDecision)

	auditWorkerID = routed.WorkerID
	client, err := g.getWorkerClient(routed.WorkerID)
	if err != nil {
		auditStatus = "failed"
		auditErrorCode = "worker_unavailable"
		return nil, types.NewInferaError(types.ErrorCode("worker_unavailable"), err.Error())
	}

	resp, err := client.InferWithContext(timeoutCtx, req)
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			auditStatus = "client_canceled"
			return nil, err
		case errors.Is(err, context.DeadlineExceeded):
			auditStatus = "failed"
			auditErrorCode = "inference_timeout"
			return nil, types.NewInferaError(types.ErrorCode("inference_timeout"), "Inference request timed out")
		default:
			auditStatus = "failed"
			auditErrorCode = "inference_error"
			return nil, types.NewInferaError(types.ErrorCodeInternalError, err.Error())
		}
	}

	auditUsage = resolveUsageMeasurement(
		resp.Usage.PromptTokens,
		resp.Usage.CompletionTokens,
		resp.Usage.TotalTokens,
		req.TokenEstimate(),
		estimateCompletionTokens(resp),
	)
	if g.metrics != nil {
		g.recordNonStreamingLatencyMetrics(req.ModelID, resp, auditUsage.CompletionTokens)
	}

	auditTokenCount = auditUsage.TotalTokens
	auditStatus = "success"

	return &inferenceExecutionResult{
		Response:        resp,
		WorkerID:        routed.WorkerID,
		RoutingDecision: routed.RoutingDecision,
	}, nil
}

func (g *Gateway) enforceWorkspaceQuotaForKey(key *auth.KeyRecord, req *types.InferenceRequest) *types.InferaError {
	if g.authHandler == nil || g.auditStore == nil || key == nil || strings.TrimSpace(key.WorkspaceID) == "" {
		return nil
	}

	quota, err := g.quotaCache.getWorkspaceQuota(key.WorkspaceID, g.authHandler.Store().GetWorkspaceQuota)
	if err != nil {
		g.log.Warn("workspace.quota_lookup_failed",
			slog.String("workspace_id", key.WorkspaceID),
			slog.String("error", err.Error()),
		)
		return nil
	}
	if quota == nil || (!quota.EnforceHardLimits) || (quota.MonthlyRequestLimit == nil && quota.MonthlyTokenLimit == nil) {
		return nil
	}

	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	usage, err := g.quotaCache.getWorkspaceUsageSummary(key.WorkspaceID, monthStart, g.auditStore.UsageSummary)
	if err != nil {
		g.log.Warn("workspace.quota_usage_failed",
			slog.String("workspace_id", key.WorkspaceID),
			slog.String("error", err.Error()),
		)
		return nil
	}

	var summary audit.UsageSummary
	if usage != nil {
		summary = *usage
	}
	projectedRequests := summary.RequestCount + 1
	projectedTokens := summary.TokenCount + int64(req.TokenEstimate()+req.Parameters.MaxTokens)

	if quota.MonthlyRequestLimit != nil && projectedRequests > *quota.MonthlyRequestLimit {
		return types.NewInferaError(types.ErrorCode("quota_exceeded"),
			fmt.Sprintf("Workspace request quota exceeded for %s. Limit: %d requests/month.", key.WorkspaceName, *quota.MonthlyRequestLimit))
	}
	if quota.MonthlyTokenLimit != nil && projectedTokens > *quota.MonthlyTokenLimit {
		return types.NewInferaError(types.ErrorCode("quota_exceeded"),
			fmt.Sprintf("Workspace token quota exceeded for %s. Limit: %d tokens/month.", key.WorkspaceName, *quota.MonthlyTokenLimit))
	}
	return nil
}
