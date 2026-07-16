package gateway

import (
	"encoding/json"
	"testing"

	"github.com/infera/infera/go/pkg/types"
)

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
