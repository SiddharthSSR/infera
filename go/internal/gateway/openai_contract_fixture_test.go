package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/infera/infera/go/internal/router"
	"github.com/infera/infera/go/pkg/types"
)

func TestToInferenceRequestMatchesSharedRequestFixture(t *testing.T) {
	g := New(DefaultConfig(), nil, nil)

	var req ChatCompletionRequest
	if err := json.Unmarshal(loadOpenAIChatFixtureBytes(t, OpenAIChatFixtureRequestToolCalls), &req); err != nil {
		t.Fatalf("decode request fixture: %v", err)
	}

	httpReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	httpReq.Header.Set(HeaderRequestID, "req-openai-request-fixture")
	inferenceReq := g.toInferenceRequest(httpReq, &req)

	if inferenceReq.RequestID == "req-openai-request-fixture" || inferenceReq.ClientRequestID != "req-openai-request-fixture" {
		t.Fatalf("expected distinct execution and client request ids, got execution=%q client=%q", inferenceReq.RequestID, inferenceReq.ClientRequestID)
	}
	if inferenceReq.ModelID != "model-1" {
		t.Fatalf("expected model-1, got %q", inferenceReq.ModelID)
	}
	if inferenceReq.Stream {
		t.Fatal("expected non-streaming request")
	}
	if inferenceReq.Parameters.MaxTokens != 128 {
		t.Fatalf("expected max_tokens=128, got %d", inferenceReq.Parameters.MaxTokens)
	}
	if inferenceReq.Parameters.Temperature != 0.25 {
		t.Fatalf("expected temperature=0.25, got %f", inferenceReq.Parameters.Temperature)
	}
	if inferenceReq.Parameters.TopP != 0.9 {
		t.Fatalf("expected top_p=0.9, got %f", inferenceReq.Parameters.TopP)
	}
	if inferenceReq.Parameters.PresencePenalty != 0.4 {
		t.Fatalf("expected presence_penalty=0.4, got %f", inferenceReq.Parameters.PresencePenalty)
	}
	if inferenceReq.Parameters.FrequencyPenalty != 0.3 {
		t.Fatalf("expected frequency_penalty=0.3, got %f", inferenceReq.Parameters.FrequencyPenalty)
	}
	if inferenceReq.Parameters.Seed == nil || *inferenceReq.Parameters.Seed != 42 {
		t.Fatalf("expected seed=42, got %+v", inferenceReq.Parameters.Seed)
	}
	if !reflect.DeepEqual(inferenceReq.Parameters.StopSequences, []string{"<END>", "</tool>"}) {
		t.Fatalf("expected stop sequences, got %#v", inferenceReq.Parameters.StopSequences)
	}
	if len(inferenceReq.Messages) != 3 {
		t.Fatalf("expected three messages, got %d", len(inferenceReq.Messages))
	}
	if len(inferenceReq.Messages[1].ToolCalls) != 1 {
		t.Fatalf("expected assistant tool calls, got %+v", inferenceReq.Messages[1].ToolCalls)
	}
	if inferenceReq.Messages[1].ToolCalls[0].Function.Name != "web_search" {
		t.Fatalf("expected web_search tool call, got %+v", inferenceReq.Messages[1].ToolCalls[0])
	}
	if inferenceReq.Messages[2].ToolCallID != "call_1" {
		t.Fatalf("expected tool_call_id call_1, got %q", inferenceReq.Messages[2].ToolCallID)
	}
	if len(inferenceReq.Tools) != 1 || inferenceReq.Tools[0].Function.Name != "web_search" {
		t.Fatalf("expected forwarded tools, got %+v", inferenceReq.Tools)
	}
	assertJSONEqual(t, req.ToolChoice, inferenceReq.ToolChoice)
}

func TestWriteChatCompletionResponseMatchesSharedFixture(t *testing.T) {
	r := router.New(router.DefaultConfig())
	defer r.Stop()

	g := New(DefaultConfig(), r, nil)
	rec := httptest.NewRecorder()

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

	g.writeChatCompletionResponse(rec, req.RequestID, req.ModelID, req, resp)

	assertOpenAIChatFixtureEqual(t, OpenAIChatFixtureResponseToolCalls, rec.Body.Bytes())
}

func TestHandleChatCompletionsRejectsInvalidStopWithSharedErrorFixture(t *testing.T) {
	g := New(DefaultConfig(), router.New(router.DefaultConfig()), nil)
	rec := httptest.NewRecorder()
	body := `{"model":"model-1","messages":[{"role":"user","content":"say hello"}],"stop":123}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))

	g.handleChatCompletions(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertOpenAIChatFixtureEqual(t, OpenAIChatFixtureErrorInvalidRequest, rec.Body.Bytes())
}

func TestHandleStreamingInferenceMatchesSharedFixtures(t *testing.T) {
	r := router.New(router.DefaultConfig())
	defer r.Stop()

	g := New(DefaultConfig(), r, nil)
	client := NewWorkerClient("http://localhost:8081")
	client.streamingHTTPClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/infer/stream" {
			t.Fatalf("expected /infer/stream request, got %s", r.URL.Path)
		}

		var builder strings.Builder
		encoder := json.NewEncoder(&builder)
		if err := encoder.Encode(map[string]any{
			"delta": "",
			"tool_calls": []map[string]any{
				{
					"index": 0,
					"id":    "call_1",
					"type":  OpenAIChatToolTypeFunction,
					"function": map[string]any{
						"name":      "web_search",
						"arguments": `{"query":"Go scheduler"}`,
					},
				},
			},
		}); err != nil {
			t.Fatalf("encode tool-call chunk: %v", err)
		}
		if err := encoder.Encode(map[string]any{
			"delta":         "",
			"finish_reason": "tool_calls",
			"usage": map[string]int{
				"prompt_tokens":     5,
				"completion_tokens": 1,
				"total_tokens":      6,
			},
		}); err != nil {
			t.Fatalf("encode final chunk: %v", err)
		}

		return jsonHTTPResponse(http.StatusOK, builder.String()), nil
	})

	req := &types.InferenceRequest{
		RequestID: "req-openai-fixture",
		ModelID:   "model-1",
		Messages: []types.Message{
			{Role: types.RoleUser, Content: "search go scheduler"},
		},
		Parameters: types.DefaultInferenceParameters(),
		Stream:     true,
	}

	rec := httptest.NewRecorder()
	result := g.handleStreamingInference(
		rec,
		httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(context.Background()),
		client,
		req,
		req.ModelID,
	)

	if result.Status != "success" {
		t.Fatalf("expected success status, got %q", result.Status)
	}
	if result.Usage.TotalTokens != 6 {
		t.Fatalf("expected token count 6, got %d", result.Usage.TotalTokens)
	}

	events := sseEvents(rec.Body.String())
	if len(events) != 4 {
		t.Fatalf("expected 4 SSE events, got %d: %#v", len(events), events)
	}

	assertOpenAIChatFixtureEqual(t, OpenAIChatFixtureStreamInitialChunk, []byte(events[0]))
	assertOpenAIChatFixtureEqual(t, OpenAIChatFixtureStreamToolCallsChunk, []byte(events[1]))
	assertOpenAIChatFixtureEqual(t, OpenAIChatFixtureStreamFinalChunk, []byte(events[2]))

	if events[3] != "[DONE]" {
		t.Fatalf("expected final DONE event, got %q", events[3])
	}
}

func TestHandleChatCompletionsStreamingWorkerErrorBeforeCommitMatchesSharedFixture(t *testing.T) {
	const modelID = "model-1"

	g := newGatewayWithTestWorker(t, modelID, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/infer/stream" {
			t.Fatalf("unexpected worker path: %s", r.URL.Path)
		}
		return jsonHTTPResponse(http.StatusServiceUnavailable, "upstream unavailable"), nil
	}))

	body := `{"model":"model-1","stream":true,"messages":[{"role":"user","content":"say hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	g.handleChatCompletions(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("expected json content-type before SSE commit, got %q", got)
	}
	assertOpenAIChatFixtureEqual(t, OpenAIChatFixtureErrorInferenceError, rec.Body.Bytes())
}

func loadOpenAIChatFixtureBytes(t *testing.T, name string) []byte {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "contracts", "openai_chat", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func assertOpenAIChatFixtureEqual(t *testing.T, fixtureName string, got []byte) {
	t.Helper()

	want := decodeJSONMap(t, loadOpenAIChatFixtureBytes(t, fixtureName))
	gotValue := decodeJSONMap(t, got)
	normalizeCreatedField(want)
	normalizeCreatedField(gotValue)

	if !reflect.DeepEqual(want, gotValue) {
		t.Fatalf(
			"json mismatch for %s\nwant: %s\ngot: %s",
			fixtureName,
			strings.TrimSpace(string(loadOpenAIChatFixtureBytes(t, fixtureName))),
			strings.TrimSpace(string(got)),
		)
	}
}

func decodeJSONMap(t *testing.T, payload []byte) map[string]any {
	t.Helper()

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	return decoded
}

func normalizeCreatedField(value map[string]any) {
	if _, ok := value["created"]; ok {
		value["created"] = float64(0)
	}
}
