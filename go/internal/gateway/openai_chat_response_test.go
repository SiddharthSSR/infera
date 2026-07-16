package gateway

import (
	"encoding/json"
	"testing"

	"github.com/infera/infera/go/internal/audit"
	"github.com/infera/infera/go/pkg/types"
)

func TestResolveUsageMeasurementTracksAccuracy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		actualPrompt        int
		actualCompletion    int
		actualTotal         int
		estimatedPrompt     int
		estimatedCompletion int
		want                usageMeasurement
	}{
		{
			name:         "exact components",
			actualPrompt: 12, actualCompletion: 4, actualTotal: 16,
			estimatedPrompt: 10, estimatedCompletion: 3,
			want: usageMeasurement{PromptTokens: 12, CompletionTokens: 4, TotalTokens: 16, TokenSource: audit.TokenSourceExact},
		},
		{
			name:            "estimated components",
			estimatedPrompt: 10, estimatedCompletion: 3,
			want: usageMeasurement{PromptTokens: 10, CompletionTokens: 3, TotalTokens: 13, TokenSource: audit.TokenSourceEstimated},
		},
		{
			name:         "mixed components",
			actualPrompt: 12, estimatedPrompt: 10, estimatedCompletion: 3,
			want: usageMeasurement{PromptTokens: 12, CompletionTokens: 3, TotalTokens: 15, TokenSource: audit.TokenSourceMixed},
		},
		{
			name:        "exact total with estimated breakdown",
			actualTotal: 20, estimatedPrompt: 10, estimatedCompletion: 3,
			want: usageMeasurement{PromptTokens: 10, CompletionTokens: 3, TotalTokens: 20, TokenSource: audit.TokenSourceMixed},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := resolveUsageMeasurement(tt.actualPrompt, tt.actualCompletion, tt.actualTotal, tt.estimatedPrompt, tt.estimatedCompletion)
			if got != tt.want {
				t.Fatalf("resolveUsageMeasurement() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestBuildChatCompletionResponseMatchesSharedFixture(t *testing.T) {
	t.Parallel()

	req := &types.InferenceRequest{
		RequestID: "req-openai-fixture",
		ModelID:   "model-1",
		Messages: []types.Message{
			{Role: types.RoleUser, Content: "search go scheduler"},
		},
		Parameters: types.DefaultInferenceParameters(),
	}
	resp := &types.InferenceResponse{
		RequestID: "req-openai-fixture",
		ModelID:   "model-1",
		Choices: []types.Choice{
			{
				Index: 0,
				Message: types.Message{
					Role:    types.RoleAssistant,
					Content: "",
					ToolCalls: []types.ToolCall{{
						ID:   "call_1",
						Type: OpenAIChatToolTypeFunction,
						Function: types.FunctionCall{
							Name:      "web_search",
							Arguments: `{"query":"Go scheduler"}`,
						},
					}},
				},
				FinishReason: types.FinishReasonToolCalls,
			},
		},
		Usage: types.UsageStats{
			PromptTokens:     5,
			CompletionTokens: 1,
			TotalTokens:      6,
		},
	}

	got, completionTokens := buildChatCompletionResponse(req.RequestID, req.ModelID, req, resp)
	if completionTokens != 1 {
		t.Fatalf("expected completionTokens=1, got %d", completionTokens)
	}

	data, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	assertOpenAIChatFixtureEqual(t, OpenAIChatFixtureResponseToolCalls, data)
}

func TestBuildChatCompletionResponseFallsBackToEstimatedUsage(t *testing.T) {
	t.Parallel()

	req := &types.InferenceRequest{
		RequestID: "req-estimate",
		ModelID:   "model-1",
		Messages: []types.Message{
			{Role: types.RoleUser, Content: "12345678"},
		},
		Parameters: types.DefaultInferenceParameters(),
	}
	resp := &types.InferenceResponse{
		RequestID: "req-estimate",
		ModelID:   "model-1",
		Choices: []types.Choice{
			{
				Index: 0,
				Message: types.Message{
					Role:    types.RoleAssistant,
					Content: "abcdefghi",
				},
				FinishReason: types.FinishReasonStop,
			},
		},
	}

	got, completionTokens := buildChatCompletionResponse(req.RequestID, req.ModelID, req, resp)

	if got.Usage.PromptTokens != req.TokenEstimate() {
		t.Fatalf("expected prompt token estimate %d, got %d", req.TokenEstimate(), got.Usage.PromptTokens)
	}
	if completionTokens != 2 {
		t.Fatalf("expected completionTokens=2, got %d", completionTokens)
	}
	if got.Usage.CompletionTokens != 2 {
		t.Fatalf("expected usage completion tokens=2, got %d", got.Usage.CompletionTokens)
	}
	if got.Usage.TotalTokens != req.TokenEstimate()+2 {
		t.Fatalf("expected total tokens=%d, got %d", req.TokenEstimate()+2, got.Usage.TotalTokens)
	}
}

func TestBuildInitialChatCompletionChunkMatchesSharedFixture(t *testing.T) {
	t.Parallel()

	chunk := buildInitialChatCompletionChunk("chatcmpl-req-openai-fixture", 0, "model-1")

	data, err := json.Marshal(chunk)
	if err != nil {
		t.Fatalf("marshal chunk: %v", err)
	}

	assertOpenAIChatFixtureEqual(t, OpenAIChatFixtureStreamInitialChunk, data)
}

func TestBuildStreamingChatCompletionChunkMatchesSharedToolCallFixture(t *testing.T) {
	t.Parallel()

	chunk := buildStreamingChatCompletionChunk(
		"chatcmpl-req-openai-fixture",
		0,
		"model-1",
		&types.TokenChunk{
			Index: 0,
			Delta: "",
			ToolCalls: []types.ToolCallChunkDelta{{
				Index: 0,
				ID:    "call_1",
				Type:  OpenAIChatToolTypeFunction,
				Function: types.FunctionDelta{
					Name:      "web_search",
					Arguments: `{"query":"Go scheduler"}`,
				},
			}},
		},
	)

	data, err := json.Marshal(chunk)
	if err != nil {
		t.Fatalf("marshal chunk: %v", err)
	}

	assertOpenAIChatFixtureEqual(t, OpenAIChatFixtureStreamToolCallsChunk, data)
}

func TestBuildStreamingChatCompletionChunkMatchesSharedFinalFixture(t *testing.T) {
	t.Parallel()

	finishReason := types.FinishReasonToolCalls
	chunk := buildStreamingChatCompletionChunk(
		"chatcmpl-req-openai-fixture",
		0,
		"model-1",
		&types.TokenChunk{
			Index:        1,
			Delta:        "",
			FinishReason: &finishReason,
		},
	)

	data, err := json.Marshal(chunk)
	if err != nil {
		t.Fatalf("marshal chunk: %v", err)
	}

	assertOpenAIChatFixtureEqual(t, OpenAIChatFixtureStreamFinalChunk, data)
}
