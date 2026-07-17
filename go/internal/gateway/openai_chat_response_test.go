package gateway

import (
	"encoding/json"
	"testing"

	"github.com/infera/infera/go/internal/audit"
	"github.com/infera/infera/go/pkg/types"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNonStreamingLatencyMarksUnavailableWithoutZeroSample(t *testing.T) {
	g := &Gateway{metrics: NewGatewayMetrics()}
	resp := &types.InferenceResponse{}
	resp.Latency.TotalMS = 20

	g.recordNonStreamingLatencyMetrics("model-1", "least_loaded", resp, 1)

	if got := testutil.ToFloat64(g.metrics.sloMeasurements.WithLabelValues("ttft", "model-1", "least_loaded", "false", "unavailable")); got != 1 {
		t.Fatalf("expected unavailable TTFT count=1, got %v", got)
	}
	if got := testutil.ToFloat64(g.metrics.sloMeasurements.WithLabelValues("tpot", "model-1", "least_loaded", "false", "unavailable")); got != 1 {
		t.Fatalf("expected unavailable TPOT count=1, got %v", got)
	}
	if got := histogramCountForLabels(t, g.metrics, "infera_gateway_slo_v1_ttft_seconds", map[string]string{"model": "model-1"}); got != 0 {
		t.Fatalf("unavailable TTFT must not fabricate a zero sample, got count=%d", got)
	}
}

func TestUsableOutputObservation(t *testing.T) {
	tests := []struct {
		name  string
		chunk *types.TokenChunk
		want  bool
	}{
		{name: "nil", chunk: nil, want: false},
		{name: "empty", chunk: &types.TokenChunk{}, want: false},
		{name: "content", chunk: &types.TokenChunk{Delta: " "}, want: true},
		{name: "usage only", chunk: &types.TokenChunk{Usage: &types.UsageStats{CompletionTokens: 2}}, want: false},
		{name: "finish only", chunk: &types.TokenChunk{FinishReason: finishReasonPtr(types.FinishReasonStop)}, want: false},
		{name: "tool metadata only", chunk: &types.TokenChunk{ToolCalls: []types.ToolCallChunkDelta{{ID: "call_1", Type: "function"}}}, want: false},
		{name: "tool name", chunk: &types.TokenChunk{ToolCalls: []types.ToolCallChunkDelta{{Function: types.FunctionDelta{Name: "search"}}}}, want: true},
		{name: "tool arguments", chunk: &types.TokenChunk{ToolCalls: []types.ToolCallChunkDelta{{Function: types.FunctionDelta{Arguments: `{}`}}}}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isUsableOutputObservation(test.chunk); got != test.want {
				t.Fatalf("isUsableOutputObservation()=%v, want %v", got, test.want)
			}
		})
	}
}

func finishReasonPtr(reason types.FinishReason) *types.FinishReason {
	return &reason
}

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
