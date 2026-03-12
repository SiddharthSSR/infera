package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/infera/infera/go/internal/router"
	"github.com/infera/infera/go/pkg/types"
)

func TestHandleChatCompletionsReturnsOpenAICompatibleResponse(t *testing.T) {
	t.Parallel()

	const modelID = "meta-llama/Meta-Llama-3.1-8B-Instruct"

	g := newGatewayWithTestWorker(t, modelID, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/infer" {
			t.Fatalf("unexpected worker path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected worker method: %s", r.Method)
		}

		var req WorkerInferRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode worker request: %v", err)
		}
		if req.Stream {
			t.Fatalf("expected non-streaming worker request")
		}
		if req.ModelID != modelID {
			t.Fatalf("expected model %q, got %q", modelID, req.ModelID)
		}

		resp := WorkerInferResponse{
			RequestID: req.RequestID,
			ModelID:   req.ModelID,
			Choices: []struct {
				Index   int `json:"index"`
				Message struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{
				{
					Index: 0,
					Message: struct {
						Role    string `json:"role"`
						Content string `json:"content"`
					}{
						Role:    "assistant",
						Content: "hello from worker",
					},
					FinishReason: "stop",
				},
			},
		}
		resp.Usage.PromptTokens = 11
		resp.Usage.CompletionTokens = 7
		resp.Usage.TotalTokens = 0

		respBody, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("marshal worker response: %v", err)
		}

		return jsonHTTPResponse(http.StatusOK, string(respBody)), nil
	}))

	body := `{"model":"` + modelID + `","messages":[{"role":"user","content":"say hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	g.handleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("expected json content-type, got %q", got)
	}

	var resp ChatCompletionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode gateway response: %v", err)
	}

	if !strings.HasPrefix(resp.ID, "chatcmpl-") {
		t.Fatalf("expected chat completion id, got %q", resp.ID)
	}
	if resp.Object != "chat.completion" {
		t.Fatalf("expected chat.completion object, got %q", resp.Object)
	}
	if resp.Model != modelID {
		t.Fatalf("expected model %q, got %q", modelID, resp.Model)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("expected one choice, got %d", len(resp.Choices))
	}
	if resp.Choices[0].Message.Role != "assistant" {
		t.Fatalf("expected assistant role, got %q", resp.Choices[0].Message.Role)
	}
	if resp.Choices[0].Message.Content != "hello from worker" {
		t.Fatalf("expected worker content, got %q", resp.Choices[0].Message.Content)
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Fatalf("expected stop finish_reason, got %q", resp.Choices[0].FinishReason)
	}
	if resp.Usage.PromptTokens != 11 || resp.Usage.CompletionTokens != 7 || resp.Usage.TotalTokens != 18 {
		t.Fatalf("unexpected usage: %+v", resp.Usage)
	}
}

func TestHandleChatCompletionsStreamingReturnsSSEChunksAndDone(t *testing.T) {
	t.Parallel()

	const modelID = "meta-llama/Meta-Llama-3.1-8B-Instruct"

	g := newGatewayWithTestWorker(t, modelID, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/infer/stream" {
			t.Fatalf("unexpected worker path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected worker method: %s", r.Method)
		}

		var req WorkerInferRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode worker request: %v", err)
		}
		if !req.Stream {
			t.Fatalf("expected streaming worker request")
		}

		var builder strings.Builder
		encoder := json.NewEncoder(&builder)
		if err := encoder.Encode(map[string]any{
			"delta": "Hello",
		}); err != nil {
			t.Fatalf("encode first chunk: %v", err)
		}
		if err := encoder.Encode(map[string]any{
			"delta":         " world",
			"finish_reason": "stop",
			"usage": map[string]int{
				"prompt_tokens":     5,
				"completion_tokens": 2,
				"total_tokens":      0,
			},
		}); err != nil {
			t.Fatalf("encode final chunk: %v", err)
		}

		return jsonHTTPResponse(http.StatusOK, builder.String()), nil
	}))

	body := `{"model":"` + modelID + `","stream":true,"messages":[{"role":"user","content":"say hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	g.handleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("expected event-stream content-type, got %q", got)
	}

	events := sseEvents(rec.Body.String())
	if len(events) != 4 {
		t.Fatalf("expected 4 SSE events, got %d: %#v", len(events), events)
	}
	if events[3] != "[DONE]" {
		t.Fatalf("expected final DONE event, got %q", events[3])
	}

	var initial ChatCompletionChunk
	if err := json.Unmarshal([]byte(events[0]), &initial); err != nil {
		t.Fatalf("decode initial chunk: %v", err)
	}
	if initial.Object != "chat.completion.chunk" {
		t.Fatalf("expected chunk object, got %q", initial.Object)
	}
	if initial.Choices[0].Delta.Role != "assistant" {
		t.Fatalf("expected initial assistant role chunk, got %+v", initial.Choices[0].Delta)
	}

	var contentChunk ChatCompletionChunk
	if err := json.Unmarshal([]byte(events[1]), &contentChunk); err != nil {
		t.Fatalf("decode content chunk: %v", err)
	}
	if got := contentChunk.Choices[0].Delta.Content; got != "Hello" {
		t.Fatalf("expected first delta content, got %q", got)
	}

	var finalChunk ChatCompletionChunk
	if err := json.Unmarshal([]byte(events[2]), &finalChunk); err != nil {
		t.Fatalf("decode final chunk: %v", err)
	}
	if got := finalChunk.Choices[0].Delta.Content; got != " world" {
		t.Fatalf("expected final delta content, got %q", got)
	}
	if finalChunk.Choices[0].FinishReason == nil || *finalChunk.Choices[0].FinishReason != "stop" {
		t.Fatalf("expected stop finish_reason, got %+v", finalChunk.Choices[0].FinishReason)
	}
}

func TestHandleChatCompletionsStreamingWorkerErrorBeforeCommitReturnsJSONError(t *testing.T) {
	t.Parallel()

	const modelID = "meta-llama/Meta-Llama-3.1-8B-Instruct"

	g := newGatewayWithTestWorker(t, modelID, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/infer/stream" {
			t.Fatalf("unexpected worker path: %s", r.URL.Path)
		}
		return jsonHTTPResponse(http.StatusServiceUnavailable, "upstream unavailable\n"), nil
	}))

	body := `{"model":"` + modelID + `","stream":true,"messages":[{"role":"user","content":"say hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	g.handleChatCompletions(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("expected json content-type before SSE commit, got %q", got)
	}

	var resp map[string]map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if resp["error"]["type"] != "inference_error" {
		t.Fatalf("expected inference_error type, got %#v", resp)
	}
	if strings.Contains(rec.Body.String(), "[DONE]") {
		t.Fatalf("expected no SSE trailer in json error response")
	}
}

func newGatewayWithTestWorker(t *testing.T, modelID string, transport http.RoundTripper) *Gateway {
	t.Helper()

	r := router.New(router.DefaultConfig())
	t.Cleanup(r.Stop)

	address := "worker.test:8081"
	if err := r.RegisterWorker(&types.WorkerInfo{
		WorkerID: "worker-1",
		Address:  address,
		Status:   types.WorkerStatusHealthy,
		LoadedModels: []types.LoadedModel{
			{ModelID: modelID},
		},
	}); err != nil {
		t.Fatalf("register worker: %v", err)
	}

	g := New(DefaultConfig(), r, nil)
	g.workerClients["worker-1"] = &WorkerClient{
		address:             address,
		httpClient:          &http.Client{Transport: transport},
		streamingHTTPClient: &http.Client{Transport: transport},
		breaker:             NewCircuitBreaker(),
	}
	return g
}

func sseEvents(body string) []string {
	parts := strings.Split(strings.TrimSpace(body), "\n\n")
	events := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		events = append(events, strings.TrimPrefix(part, "data: "))
	}
	return events
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func jsonHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body: io.NopCloser(strings.NewReader(body)),
	}
}
