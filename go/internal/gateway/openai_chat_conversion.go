package gateway

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/infera/infera/go/internal/auth"
	"github.com/infera/infera/go/pkg/types"
)

func buildAffinityMetadata(r *http.Request, req *ChatCompletionRequest) map[string]string {
	messages := make([]types.Message, len(req.Messages))
	for i, msg := range req.Messages {
		messages[i] = types.Message{
			Role:    types.Role(msg.Role),
			Content: msg.Content,
			Name:    msg.Name,
		}
	}
	return buildAffinityMetadataFromMessages(
		req.Model,
		messages,
		strings.TrimSpace(r.Header.Get("X-Infera-Affinity-Key")),
		auth.KeyFromContext(r.Context()),
		auth.SessionFromContext(r.Context()),
	)
}

func (g *Gateway) toInferenceRequest(r *http.Request, req *ChatCompletionRequest) *types.InferenceRequest {
	messages := make([]types.Message, len(req.Messages))
	for i, msg := range req.Messages {
		messages[i] = types.Message{
			Role:       types.Role(msg.Role),
			Content:    msg.Content,
			Name:       msg.Name,
			ToolCalls:  convertToolCalls(msg.ToolCalls),
			ToolCallID: msg.ToolCallID,
		}
	}

	params := types.DefaultInferenceParameters()
	if req.Temperature != nil {
		params.Temperature = *req.Temperature
	}
	if req.TopP != nil {
		params.TopP = *req.TopP
	}
	if req.MaxTokens != nil {
		params.MaxTokens = *req.MaxTokens
	}
	if req.Stop != nil {
		params.StopSequences = []string(req.Stop)
	}
	if req.Seed != nil {
		params.Seed = req.Seed
	}
	if req.PresencePenalty != nil {
		params.PresencePenalty = *req.PresencePenalty
	}
	if req.FrequencyPenalty != nil {
		params.FrequencyPenalty = *req.FrequencyPenalty
	}

	workspaceID := auth.DefaultWorkspaceID
	if key := auth.KeyFromContext(r.Context()); key != nil {
		workspaceID = normalizeWorkspaceIDForGateway(key.WorkspaceID)
	}
	return &types.InferenceRequest{
		RequestID:       uuid.New().String(),
		ClientRequestID: clientRequestID(r),
		ModelID:         req.Model,
		Messages:        messages,
		Parameters:      params,
		Stream:          req.Stream,
		Priority:        types.PriorityNormal,
		Metadata:        buildAffinityMetadata(r, req),
		CreatedAt:       time.Now(),
		Tools:           convertToolDefinitions(req.Tools),
		ToolChoice:      req.ToolChoice,
		WorkspaceID:     workspaceID,
	}
}

// convertToolCalls unmarshals a slice of raw JSON tool call objects into typed ToolCall values.
// Items that fail to unmarshal are silently dropped; we prefer degraded pass-through
// over rejecting the whole request for a single malformed entry.
func convertToolCalls(raw []json.RawMessage) []types.ToolCall {
	if len(raw) == 0 {
		return nil
	}
	result := make([]types.ToolCall, 0, len(raw))
	for _, r := range raw {
		var tc types.ToolCall
		if err := json.Unmarshal(r, &tc); err == nil {
			result = append(result, tc)
		}
	}
	return result
}

// convertToolDefinitions unmarshals a slice of raw JSON tool definition objects into typed
// ToolDefinition values. Items that fail to unmarshal are silently dropped.
func convertToolDefinitions(raw []json.RawMessage) []types.ToolDefinition {
	if len(raw) == 0 {
		return nil
	}
	result := make([]types.ToolDefinition, 0, len(raw))
	for _, r := range raw {
		var td types.ToolDefinition
		if err := json.Unmarshal(r, &td); err == nil {
			result = append(result, td)
		}
	}
	return result
}

// marshalToolCalls serializes typed ToolCall values back into raw JSON for the API response.
// A nil or empty input returns nil so the field is omitted from the JSON output.
func marshalToolCalls(calls []types.ToolCall) []json.RawMessage {
	if len(calls) == 0 {
		return nil
	}
	result := make([]json.RawMessage, len(calls))
	for i, tc := range calls {
		data, _ := json.Marshal(tc)
		result[i] = data
	}
	return result
}

// marshalToolCallChunkDeltas serializes streaming tool-call deltas into raw JSON slices.
func marshalToolCallChunkDeltas(deltas []types.ToolCallChunkDelta) []json.RawMessage {
	if len(deltas) == 0 {
		return nil
	}
	result := make([]json.RawMessage, len(deltas))
	for i, d := range deltas {
		data, _ := json.Marshal(d)
		result[i] = data
	}
	return result
}

func clientRequestID(r *http.Request) string {
	if r != nil {
		return strings.TrimSpace(r.Header.Get(HeaderRequestID))
	}
	return ""
}
