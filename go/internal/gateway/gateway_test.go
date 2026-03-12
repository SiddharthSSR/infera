package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/infera/infera/go/internal/router"
	"github.com/infera/infera/go/pkg/types"
)

func TestHandleCORSAllowedOrigin(t *testing.T) {
	g := New(Config{
		EnableCORS:     true,
		AllowedOrigins: []string{"https://app.example.com"},
	}, nil, nil)

	handler := g.handleCORS(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodOptions, "/api/health", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("expected allow origin header to match request origin, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("expected credentials to be enabled for explicit origin, got %q", got)
	}
}

func TestHandleCORSDisallowedOrigin(t *testing.T) {
	g := New(Config{
		EnableCORS:     true,
		AllowedOrigins: []string{"https://app.example.com"},
	}, nil, nil)

	handler := g.handleCORS(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodOptions, "/api/health", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for disallowed origin, got %d", rec.Code)
	}
}

func TestHandleCORSWildcardOrigin(t *testing.T) {
	g := New(Config{
		EnableCORS:     true,
		AllowedOrigins: []string{"*"},
	}, nil, nil)

	handler := g.handleCORS(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("expected wildcard allow origin, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("expected credentials header to be empty for wildcard origin, got %q", got)
	}
}

func TestRequireWorkerTokenOnRegister(t *testing.T) {
	r := router.New(router.DefaultConfig())
	defer r.Stop()

	g := New(Config{WorkerSharedToken: "secret-token"}, r, nil)
	handler := g.requireWorkerToken(g.handleRegisterWorker)

	body := `{"worker_id":"w1","address":"localhost:8081","status":"healthy","loaded_models":[]}`

	reqNoToken := httptest.NewRequest(http.MethodPost, "/api/workers/register", strings.NewReader(body))
	recNoToken := httptest.NewRecorder()
	handler(recNoToken, reqNoToken)
	if recNoToken.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", recNoToken.Code)
	}

	reqWithToken := httptest.NewRequest(http.MethodPost, "/api/workers/register", strings.NewReader(body))
	reqWithToken.Header.Set("X-Worker-Token", "secret-token")
	recWithToken := httptest.NewRecorder()
	handler(recWithToken, reqWithToken)
	if recWithToken.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid token, got %d", recWithToken.Code)
	}
}

func TestHandlePrometheusWorkerTargets(t *testing.T) {
	r := router.New(router.DefaultConfig())
	defer r.Stop()

	if err := r.RegisterWorker(&types.WorkerInfo{
		WorkerID: "worker-1",
		Address:  "abc-8081.proxy.runpod.net",
		Status:   types.WorkerStatusHealthy,
		Tags: map[string]string{
			"provider": "runpod",
			"engine":   "vllm",
			"version":  "test-version",
			"env":      "test",
		},
	}); err != nil {
		t.Fatalf("RegisterWorker healthy: %v", err)
	}
	if err := r.RegisterWorker(&types.WorkerInfo{
		WorkerID: "worker-2",
		Address:  "10.0.0.5:8081",
		Status:   types.WorkerStatusUnhealthy,
	}); err != nil {
		t.Fatalf("RegisterWorker unhealthy: %v", err)
	}

	g := New(DefaultConfig(), r, nil)
	req := httptest.NewRequest(http.MethodGet, "/internal/prometheus/worker-targets", nil)
	rec := httptest.NewRecorder()
	g.handlePrometheusWorkerTargets(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload []struct {
		Targets []string          `json:"targets"`
		Labels  map[string]string `json:"labels"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("expected 1 healthy worker target, got %d", len(payload))
	}
	if got := payload[0].Targets[0]; got != "abc-8081.proxy.runpod.net" {
		t.Fatalf("expected runpod target, got %q", got)
	}
	if got := payload[0].Labels["__scheme__"]; got != "https" {
		t.Fatalf("expected https scheme for runpod target, got %q", got)
	}
	if got := payload[0].Labels["provider"]; got != "runpod" {
		t.Fatalf("expected provider label, got %q", got)
	}
	if got := payload[0].Labels["engine"]; got != "vllm" {
		t.Fatalf("expected engine label, got %q", got)
	}
	if got := payload[0].Labels["version"]; got != "test-version" {
		t.Fatalf("expected version label, got %q", got)
	}
}

func TestInternalOnlyHandlerAllowsPrivateAndBlocksPublic(t *testing.T) {
	g := New(DefaultConfig(), nil, nil)
	handler := g.internalOnlyHandler(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	privateReq := httptest.NewRequest(http.MethodGet, "/internal/prometheus/worker-targets", nil)
	privateReq.RemoteAddr = "10.0.0.8:12345"
	privateRec := httptest.NewRecorder()
	handler(privateRec, privateReq)
	if privateRec.Code != http.StatusNoContent {
		t.Fatalf("expected private request to pass, got %d", privateRec.Code)
	}

	publicReq := httptest.NewRequest(http.MethodGet, "/internal/prometheus/worker-targets", nil)
	publicReq.RemoteAddr = "8.8.8.8:12345"
	publicRec := httptest.NewRecorder()
	handler(publicRec, publicReq)
	if publicRec.Code != http.StatusForbidden {
		t.Fatalf("expected public request to be forbidden, got %d body=%s", publicRec.Code, publicRec.Body.String())
	}
}

func TestUsageTotalTokensFallsBackToComponentSum(t *testing.T) {
	if got := usageTotalTokens(12, 34, 0); got != 46 {
		t.Fatalf("expected component sum fallback, got %d", got)
	}
	if got := usageTotalTokens(12, 34, 99); got != 99 {
		t.Fatalf("expected explicit total to win, got %d", got)
	}
}

func TestHandleStreamingInferenceRecomputesFinalTokenCountFromObservedChunks(t *testing.T) {
	g := New(DefaultConfig(), nil, nil)
	client := NewWorkerClient("worker.test:8081")
	client.streamingHTTPClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var builder strings.Builder
		encoder := json.NewEncoder(&builder)
		if err := encoder.Encode(map[string]any{
			"delta": "Hello",
			"usage": map[string]int{
				"prompt_tokens": 5,
			},
		}); err != nil {
			t.Fatalf("encode first chunk: %v", err)
		}
		if err := encoder.Encode(map[string]any{
			"delta":         " world",
			"finish_reason": "stop",
		}); err != nil {
			t.Fatalf("encode second chunk: %v", err)
		}
		return jsonHTTPResponse(http.StatusOK, builder.String()), nil
	})

	req := &types.InferenceRequest{
		RequestID: "req-1",
		ModelID:   "model-1",
		Messages: []types.Message{
			{Role: types.RoleUser, Content: "say hello"},
		},
	}
	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(context.Background())

	tokenCount, status := g.handleStreamingInference(rec, httpReq, client, req, "model-1")

	if status != "success" {
		t.Fatalf("expected success status, got %q", status)
	}
	if tokenCount != 7 {
		t.Fatalf("expected recomputed token count 7, got %d", tokenCount)
	}
}
