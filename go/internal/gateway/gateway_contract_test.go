package gateway

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/infera/infera/go/internal/audit"
	"github.com/infera/infera/go/internal/router"
	"github.com/infera/infera/go/pkg/types"
)

func TestHandleChatCompletionsPersistsExactUsageProvenance(t *testing.T) {
	const modelID = "model-usage"
	g := newGatewayWithTestWorker(t, modelID, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("X-Worker-Token"); got != "worker-secret" {
			t.Fatalf("expected authenticated worker request, got token %q", got)
		}
		if executionID := r.Header.Get(HeaderRequestID); executionID == "" || executionID == "req-usage-exact" {
			t.Fatalf("expected server execution id at worker boundary, got %q", executionID)
		}
		var workerReq WorkerInferRequest
		if err := json.NewDecoder(r.Body).Decode(&workerReq); err != nil {
			t.Fatalf("decode worker request: %v", err)
		}
		if workerReq.RequestID != r.Header.Get(HeaderRequestID) {
			t.Fatalf("worker request identity mismatch: body=%q header=%q", workerReq.RequestID, r.Header.Get(HeaderRequestID))
		}
		resp := WorkerInferResponse{RequestID: "req-worker", ModelID: modelID}
		resp.Choices = append(resp.Choices, struct {
			Index   int `json:"index"`
			Message struct {
				Role      string           `json:"role"`
				Content   string           `json:"content"`
				ToolCalls []types.ToolCall `json:"tool_calls,omitempty"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		}{})
		resp.Choices[0].Message.Role = "assistant"
		resp.Choices[0].Message.Content = "measured response"
		resp.Choices[0].FinishReason = "stop"
		resp.Usage.PromptTokens = 12
		resp.Usage.CompletionTokens = 4
		resp.Usage.TotalTokens = 16
		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("marshal response: %v", err)
		}
		return jsonHTTPResponse(http.StatusOK, string(data)), nil
	}))
	store := &stubAuditUsageStore{}
	g.SetAuditStore(store)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"model-usage","messages":[{"role":"user","content":"measure this"}]}`))
	req.Header.Set(HeaderRequestID, "req-usage-exact")
	rec := httptest.NewRecorder()
	g.handleChatCompletions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	retryReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"model-usage","messages":[{"role":"user","content":"measure this again"}]}`))
	retryReq.Header.Set(HeaderRequestID, "req-usage-exact")
	retryRec := httptest.NewRecorder()
	g.handleChatCompletions(retryRec, retryReq)
	if retryRec.Code != http.StatusOK {
		t.Fatalf("expected duplicate correlation request to execute, got %d body=%s", retryRec.Code, retryRec.Body.String())
	}
	close(g.auditCh)
	g.auditWg.Wait()
	g.auditCh = nil

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.appended) != 2 {
		t.Fatalf("expected two usage records, got %d", len(store.appended))
	}
	got := store.appended[0]
	if got.RequestID == "req-usage-exact" || got.ClientRequestID != "req-usage-exact" || got.PromptTokens != 12 || got.CompletionTokens != 4 || got.TokenCount != 16 || got.TokenSource != audit.TokenSourceExact {
		t.Fatalf("unexpected usage record: %+v", got)
	}
	if got.Cost.CostAccuracy != audit.CostAccuracyUnavailable {
		t.Fatalf("unmanaged worker price must be unavailable, got %+v", got.Cost)
	}
	if store.appended[1].RequestID == got.RequestID || store.appended[1].ClientRequestID != "req-usage-exact" {
		t.Fatalf("duplicate client request id reused execution identity: first=%+v second=%+v", got, store.appended[1])
	}
}

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
					Role      string           `json:"role"`
					Content   string           `json:"content"`
					ToolCalls []types.ToolCall `json:"tool_calls,omitempty"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{
				{
					Index: 0,
					Message: struct {
						Role      string           `json:"role"`
						Content   string           `json:"content"`
						ToolCalls []types.ToolCall `json:"tool_calls,omitempty"`
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
	if resp.Object != OpenAIChatCompletionObject {
		t.Fatalf("expected %s object, got %q", OpenAIChatCompletionObject, resp.Object)
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

func TestHandleChatCompletionsPassesOpenAIParametersToWorker(t *testing.T) {
	t.Parallel()

	const modelID = "meta-llama/Meta-Llama-3.1-8B-Instruct"

	g := newGatewayWithTestWorker(t, modelID, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var req WorkerInferRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode worker request: %v", err)
		}

		if req.Parameters.MaxTokens != 128 {
			t.Fatalf("expected max_tokens=128, got %d", req.Parameters.MaxTokens)
		}
		if req.Parameters.Temperature != 0.25 {
			t.Fatalf("expected temperature=0.25, got %f", req.Parameters.Temperature)
		}
		if req.Parameters.TopP != 0.9 {
			t.Fatalf("expected top_p=0.9, got %f", req.Parameters.TopP)
		}
		if req.Parameters.PresencePenalty != 0.4 {
			t.Fatalf("expected presence_penalty=0.4, got %f", req.Parameters.PresencePenalty)
		}
		if req.Parameters.FrequencyPenalty != 0.3 {
			t.Fatalf("expected frequency_penalty=0.3, got %f", req.Parameters.FrequencyPenalty)
		}
		if req.Parameters.Seed == nil || *req.Parameters.Seed != 42 {
			t.Fatalf("expected seed=42, got %+v", req.Parameters.Seed)
		}
		if len(req.Parameters.StopSequences) != 1 || req.Parameters.StopSequences[0] != "<END>" {
			t.Fatalf("expected stop_sequences to contain <END>, got %#v", req.Parameters.StopSequences)
		}

		resp := WorkerInferResponse{
			RequestID: req.RequestID,
			ModelID:   req.ModelID,
			Choices: []struct {
				Index   int `json:"index"`
				Message struct {
					Role      string           `json:"role"`
					Content   string           `json:"content"`
					ToolCalls []types.ToolCall `json:"tool_calls,omitempty"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{
				{
					Index: 0,
					Message: struct {
						Role      string           `json:"role"`
						Content   string           `json:"content"`
						ToolCalls []types.ToolCall `json:"tool_calls,omitempty"`
					}{Role: "assistant", Content: "ok"},
					FinishReason: "stop",
				},
			},
		}
		respBody, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("marshal worker response: %v", err)
		}
		return jsonHTTPResponse(http.StatusOK, string(respBody)), nil
	}))

	body := `{"model":"` + modelID + `","temperature":0.25,"top_p":0.9,"max_tokens":128,"stop":"<END>","seed":42,"presence_penalty":0.4,"frequency_penalty":0.3,"messages":[{"role":"user","content":"say hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	g.handleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleChatCompletionsRecordsBatchAndLatencyMetrics(t *testing.T) {
	t.Parallel()

	const modelID = "meta-llama/Meta-Llama-3.1-8B-Instruct"

	g := newGatewayWithTestWorker(t, modelID, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		resp := WorkerInferResponse{
			RequestID: "req-1",
			ModelID:   modelID,
			Choices: []struct {
				Index   int `json:"index"`
				Message struct {
					Role      string           `json:"role"`
					Content   string           `json:"content"`
					ToolCalls []types.ToolCall `json:"tool_calls,omitempty"`
				} `json:"message"`
				FinishReason string `json:"finish_reason"`
			}{
				{
					Index: 0,
					Message: struct {
						Role      string           `json:"role"`
						Content   string           `json:"content"`
						ToolCalls []types.ToolCall `json:"tool_calls,omitempty"`
					}{Role: "assistant", Content: "hello from metrics"},
					FinishReason: "stop",
				},
			},
		}
		resp.Usage.PromptTokens = 12
		resp.Usage.CompletionTokens = 4
		resp.Usage.TotalTokens = 16
		resp.Latency.TimeToFirstTokenMS = 120
		resp.Latency.TotalMS = 210

		respBody, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("marshal worker response: %v", err)
		}

		return jsonHTTPResponse(http.StatusOK, string(respBody)), nil
	}))

	body := `{"model":"` + modelID + `","messages":[{"role":"user","content":"measure metrics"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	g.handleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	if got := histogramCountForLabels(t, g.metrics, "infera_gateway_inference_ttft_seconds", map[string]string{
		"model":  modelID,
		"stream": "false",
	}); got != 1 {
		t.Fatalf("expected ttft metric count=1, got %d", got)
	}

	if got := histogramCountForLabels(t, g.metrics, "infera_gateway_inference_tpot_seconds", map[string]string{
		"model":  modelID,
		"stream": "false",
	}); got != 1 {
		t.Fatalf("expected tpot metric count=1, got %d", got)
	}

	if got := histogramCountForLabels(t, g.metrics, "infera_gateway_slo_v1_ttft_seconds", map[string]string{
		"measurement":      "derived",
		"model":            modelID,
		"routing_strategy": "least_loaded",
		"stream":           "false",
	}); got != 1 {
		t.Fatalf("expected derived SLO ttft metric count=1, got %d", got)
	}
	if got := testutil.ToFloat64(g.metrics.sloRequests.WithLabelValues(modelID, "least_loaded", "false", "success")); got != 1 {
		t.Fatalf("expected routed SLO success count=1, got %v", got)
	}

	// Batch metrics are only recorded when requests actually go through the
	// batcher (queue depth > 0 when they arrive). A single isolated request
	// takes the fast-path directly to the worker, so no batch metric is emitted.
}

func TestHandleChatCompletionsLogsRouteDecisionAndMetrics(t *testing.T) {
	const modelID = "meta-llama/Meta-Llama-3.1-8B-Instruct"

	var logs bytes.Buffer
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(http.StatusOK, `{
			"request_id":"req-worker",
			"model":"`+modelID+`",
			"choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6},
			"latency":{"total_ms":25}
		}`), nil
	})
	r := router.New(router.DefaultConfig())
	t.Cleanup(r.Stop)
	if err := r.RegisterWorker(&types.WorkerInfo{
		WorkerID: "worker-1",
		Address:  "http://localhost:8081",
		Status:   types.WorkerStatusHealthy,
		Tags:     map[string]string{"provider": "runpod", "gpu_type": "A100_80GB"},
		LoadedModels: []types.LoadedModel{
			{ModelID: modelID},
		},
		Stats: types.WorkerStats{
			QueueDepth:     0,
			ActiveRequests: 1,
			GPUUtilization: 0.25,
			P50LatencyMS:   623,
			P99LatencyMS:   900,
		},
	}); err != nil {
		t.Fatalf("register worker: %v", err)
	}
	g := New(DefaultConfig(), r, nil)
	g.log = slog.New(slog.NewJSONHandler(&logs, nil))
	g.workerClients["worker-1"] = &WorkerClient{
		address:             "http://localhost:8081",
		httpClient:          &http.Client{Transport: transport},
		streamingHTTPClient: &http.Client{Transport: transport},
		breaker:             NewCircuitBreaker(),
	}

	body := `{"model":"` + modelID + `","messages":[{"role":"user","content":"do not log this prompt"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer sk-do-not-log")
	rec := httptest.NewRecorder()

	g.handleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	logBody := logs.String()
	for _, want := range []string{
		`"msg":"route_decision"`,
		`"strategy":"least_loaded"`,
		`"selected_worker":"worker-1"`,
		`"selected_provider":"runpod"`,
		`"selected_gpu_type":"A100_80GB"`,
		`"candidates_evaluated":1`,
		`"worker_p50_latency_ms":623`,
	} {
		if !strings.Contains(logBody, want) {
			t.Fatalf("expected route log to contain %s, got logs:\n%s", want, logBody)
		}
	}
	for _, forbidden := range []string{"do not log this prompt", "sk-do-not-log", "Authorization"} {
		if strings.Contains(logBody, forbidden) {
			t.Fatalf("route decision log leaked %q in logs:\n%s", forbidden, logBody)
		}
	}

	if got := testutil.ToFloat64(g.metrics.routeDecisions.WithLabelValues("least_loaded", "success")); got != 1 {
		t.Fatalf("expected successful route decision metric=1, got %v", got)
	}
}

func TestHandleChatCompletionsRouteDecisionHeaderAbsentByDefault(t *testing.T) {
	t.Parallel()

	const modelID = "meta-llama/Meta-Llama-3.1-8B-Instruct"

	g := newGatewayWithTestWorker(t, modelID, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(http.StatusOK, `{
			"request_id":"req-worker",
			"model":"`+modelID+`",
			"choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]
		}`), nil
	}))

	body := `{"model":"` + modelID + `","messages":[{"role":"user","content":"say hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	g.handleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get(headerRouteDecision); got != "" {
		t.Fatalf("expected no route decision header by default, got %q", got)
	}
}

func TestHandleChatCompletionsRouteDecisionHeaderWhenRequestedIsSafe(t *testing.T) {
	t.Parallel()

	const modelID = "meta-llama/Meta-Llama-3.1-8B-Instruct"

	g := newGatewayWithTestWorker(t, modelID, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(http.StatusOK, `{
			"request_id":"req-worker",
			"model":"`+modelID+`",
			"choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]
		}`), nil
	}))

	body := `{"model":"` + modelID + `","messages":[{"role":"user","content":"do not expose this prompt"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer sk-do-not-expose")
	req.Header.Set(headerDebugRouteDecision, "true")
	rec := httptest.NewRecorder()

	g.handleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	rawHeader := rec.Header().Get(headerRouteDecision)
	if rawHeader == "" {
		t.Fatal("expected route decision header when requested")
	}
	metadata, rawJSON := decodeRouteDecisionHeader(t, rawHeader)
	if metadata["model"] != modelID {
		t.Fatalf("expected model %q, got %#v", modelID, metadata["model"])
	}
	if metadata["strategy"] != "least_loaded" {
		t.Fatalf("expected least_loaded strategy, got %#v", metadata["strategy"])
	}
	if metadata["selected_worker"] != "worker-1" {
		t.Fatalf("expected selected worker, got %#v", metadata["selected_worker"])
	}
	if metadata["candidates_evaluated"] != float64(1) {
		t.Fatalf("expected one candidate evaluated, got %#v", metadata["candidates_evaluated"])
	}
	for _, forbidden := range []string{"do not expose this prompt", "sk-do-not-expose", "Authorization", "messages"} {
		if strings.Contains(rawJSON, forbidden) {
			t.Fatalf("route decision header leaked %q in %s", forbidden, rawJSON)
		}
	}
}

func TestHandleChatCompletionsStreamingRouteDecisionHeaderWhenRequested(t *testing.T) {
	t.Parallel()

	const modelID = "meta-llama/Meta-Llama-3.1-8B-Instruct"

	g := newGatewayWithTestWorker(t, modelID, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/infer/stream" {
			t.Fatalf("unexpected worker path: %s", r.URL.Path)
		}
		return jsonHTTPResponse(http.StatusOK, "{\"delta\":\"hello\"}\n{\"finish_reason\":\"stop\"}\n"), nil
	}))

	body := `{"model":"` + modelID + `","stream":true,"messages":[{"role":"user","content":"say hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set(headerDebugRouteDecision, "true")
	rec := httptest.NewRecorder()

	g.handleChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("expected event-stream content-type, got %q", got)
	}
	rawHeader := rec.Header().Get(headerRouteDecision)
	if rawHeader == "" {
		t.Fatal("expected route decision header before stream body")
	}
	metadata, _ := decodeRouteDecisionHeader(t, rawHeader)
	if metadata["strategy"] != "least_loaded" || metadata["selected_worker"] != "worker-1" {
		t.Fatalf("unexpected streaming route metadata: %#v", metadata)
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
	store := &stubAuditUsageStore{}
	g.SetAuditStore(store)

	body := `{"model":"` + modelID + `","stream":true,"messages":[{"role":"user","content":"say hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set(HeaderRequestID, "stream-correlation-id")
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
	if initial.Object != OpenAIChatCompletionChunkObject {
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
	close(g.auditCh)
	g.auditWg.Wait()
	g.auditCh = nil
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.appended) != 1 || !store.appended[0].Stream || store.appended[0].RequestID == "stream-correlation-id" || store.appended[0].ClientRequestID != "stream-correlation-id" {
		t.Fatalf("streaming audit did not separate execution and correlation identities: %+v", store.appended)
	}
}

func TestHandleListModelsReturnsOpenAICompatibleList(t *testing.T) {
	t.Parallel()

	g := New(DefaultConfig(), router.New(router.DefaultConfig()), nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)

	g.handleListModels(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Object string                   `json:"object"`
		Data   []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode models payload: %v", err)
	}

	if payload.Object != "list" {
		t.Fatalf("expected list object, got %q", payload.Object)
	}
	if len(payload.Data) != 0 {
		t.Fatalf("expected empty model list, got %#v", payload.Data)
	}
}

func TestHandleListModelsIncludesCoreOpenAIFields(t *testing.T) {
	t.Parallel()

	r := router.New(router.DefaultConfig())
	t.Cleanup(r.Stop)
	if err := r.RegisterWorker(&types.WorkerInfo{
		WorkerID: "worker-1",
		Address:  "http://localhost:8081",
		Status:   types.WorkerStatusHealthy,
		LoadedModels: []types.LoadedModel{
			{ModelID: "b-model"},
			{ModelID: "a-model"},
		},
	}); err != nil {
		t.Fatalf("register worker: %v", err)
	}

	g := New(DefaultConfig(), r, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)

	g.handleListModels(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Object string                   `json:"object"`
		Data   []map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode models payload: %v", err)
	}

	if payload.Object != "list" {
		t.Fatalf("expected list object, got %q", payload.Object)
	}
	if len(payload.Data) != 2 {
		t.Fatalf("expected two models, got %d", len(payload.Data))
	}

	ids := make([]string, 0, len(payload.Data))
	for _, item := range payload.Data {
		if got := item["object"]; got != "model" {
			t.Fatalf("expected model object, got %#v", got)
		}
		if got := item["owned_by"]; got != "infera" {
			t.Fatalf("expected owned_by=infera, got %#v", got)
		}
		if _, ok := item["created"].(float64); !ok {
			t.Fatalf("expected created unix timestamp, got %#v", item["created"])
		}
		id, ok := item["id"].(string)
		if !ok || id == "" {
			t.Fatalf("expected non-empty id, got %#v", item["id"])
		}
		ids = append(ids, id)
	}

	sort.Strings(ids)
	if ids[0] != "a-model" || ids[1] != "b-model" {
		t.Fatalf("unexpected model ids: %#v", ids)
	}
}

func TestHandleListModelsRejectsNonGetRequestsWithOpenAIStyleError(t *testing.T) {
	t.Parallel()

	g := New(DefaultConfig(), router.New(router.DefaultConfig()), nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/models", nil)

	g.handleListModels(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	if payload["error"]["type"] != "method_not_allowed" {
		t.Fatalf("expected method_not_allowed type, got %#v", payload)
	}
	if payload["error"]["message"] == "" {
		t.Fatalf("expected non-empty error message, got %#v", payload)
	}
}

func TestHandleChatCompletionsRejectsInvalidStopType(t *testing.T) {
	t.Parallel()

	g := New(DefaultConfig(), router.New(router.DefaultConfig()), nil)
	rec := httptest.NewRecorder()
	body := `{"model":"meta-llama/Meta-Llama-3.1-8B-Instruct","messages":[{"role":"user","content":"say hello"}],"stop":123}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))

	g.handleChatCompletions(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	if payload["error"]["type"] != OpenAIChatErrorTypeInvalidRequest {
		t.Fatalf("expected %s type, got %#v", OpenAIChatErrorTypeInvalidRequest, payload)
	}
	if !strings.Contains(payload["error"]["message"], "stop") {
		t.Fatalf("expected stop-related message, got %#v", payload)
	}
}

func TestHandleChatCompletionsNoWorkersReturnsServiceUnavailable(t *testing.T) {
	r := router.New(router.DefaultConfig())
	t.Cleanup(r.Stop)
	g := New(DefaultConfig(), r, nil)
	var logs bytes.Buffer
	g.log = slog.New(slog.NewJSONHandler(&logs, nil))

	body := `{"model":"Qwen/Qwen2.5-7B-Instruct","messages":[{"role":"user","content":"do not log failed prompt"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer sk-failed-do-not-log")
	rec := httptest.NewRecorder()

	g.handleChatCompletions(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	if payload["error"]["code"] != "no_workers_available" {
		t.Fatalf("expected no_workers_available code, got %#v", payload)
	}
	if payload["error"]["type"] != "service_unavailable" {
		t.Fatalf("expected service_unavailable type, got %#v", payload)
	}
	if payload["error"]["retryable"] != true {
		t.Fatalf("expected retryable=true, got %#v", payload)
	}

	logBody := logs.String()
	for _, want := range []string{
		`"msg":"route_decision_failed"`,
		`"model":"Qwen/Qwen2.5-7B-Instruct"`,
		`"error_code":"no_workers_available"`,
		`"healthy_workers":0`,
	} {
		if !strings.Contains(logBody, want) {
			t.Fatalf("expected failed route log to contain %s, got logs:\n%s", want, logBody)
		}
	}
	for _, forbidden := range []string{"do not log failed prompt", "sk-failed-do-not-log", "Authorization"} {
		if strings.Contains(logBody, forbidden) {
			t.Fatalf("failed route decision log leaked %q in logs:\n%s", forbidden, logBody)
		}
	}
	if got := testutil.ToFloat64(g.metrics.routeDecisions.WithLabelValues("unknown", "failure")); got != 1 {
		t.Fatalf("expected failed route decision metric=1, got %v", got)
	}
}

func TestHandleChatCompletionsUnknownModelWithHealthyWorkersReturnsModelNotFound(t *testing.T) {
	g := newGatewayWithTestWorker(t, "known-model", roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Fatalf("unexpected worker dispatch for unknown model")
		return nil, nil
	}))

	body := `{"model":"unknown-model","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	g.handleChatCompletions(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	if payload["error"]["type"] != "model_not_found" {
		t.Fatalf("expected model_not_found type, got %#v", payload)
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
	if resp["error"]["type"] != OpenAIChatErrorTypeInferenceError {
		t.Fatalf("expected %s type, got %#v", OpenAIChatErrorTypeInferenceError, resp)
	}
	if strings.Contains(rec.Body.String(), "[DONE]") {
		t.Fatalf("expected no SSE trailer in json error response")
	}
}

func newGatewayWithTestWorker(t *testing.T, modelID string, transport http.RoundTripper) *Gateway {
	t.Helper()

	r := router.New(router.DefaultConfig())
	t.Cleanup(r.Stop)

	address := "http://localhost:8081"
	if err := r.RegisterWorker(&types.WorkerInfo{
		WorkerID:   "worker-1",
		SharedPool: true,
		Address:    address,
		Status:     types.WorkerStatusHealthy,
		LoadedModels: []types.LoadedModel{
			{ModelID: modelID},
		},
	}); err != nil {
		t.Fatalf("register worker: %v", err)
	}

	config := DefaultConfig()
	config.WorkerSharedToken = "worker-secret"
	g := New(config, r, nil)
	g.workerClients["worker-1"] = &WorkerClient{
		address:             address,
		workerToken:         config.WorkerSharedToken,
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

func decodeRouteDecisionHeader(t *testing.T, raw string) (map[string]any, string) {
	t.Helper()
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode route decision header: %v", err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatalf("unmarshal route decision header %q: %v", string(data), err)
	}
	return metadata, string(data)
}
