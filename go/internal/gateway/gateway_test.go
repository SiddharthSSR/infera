package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/infera/infera/go/internal/audit"
	"github.com/infera/infera/go/internal/auth"
	"github.com/infera/infera/go/internal/providers"
	mockprovider "github.com/infera/infera/go/internal/providers/mock"
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

func TestHandleRegisterWorkerLinksRunPodInstanceByProxyAddress(t *testing.T) {
	r := router.New(router.DefaultConfig())
	defer r.Stop()

	manager, err := providers.NewManager(providers.ManagerConfig{
		DefaultProvider: providers.ProviderMock,
	})
	if err != nil {
		t.Fatalf("providers.NewManager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	runpod := mockprovider.New()
	manager.RegisterProvider(runpod)

	instance, err := manager.Provision(context.Background(), &providers.ProvisionRequest{
		Name:     "runpod-like-instance",
		Provider: providers.ProviderMock,
		GPUType:  providers.GPUL40S,
		GPUCount: 1,
		Models:   []string{"Qwen/Qwen2.5-7B-Instruct"},
	})
	if err != nil {
		t.Fatalf("manager.Provision: %v", err)
	}

	// Re-shape the mock instance so it looks like a RunPod-managed pod.
	instance.Provider = providers.ProviderRunPod
	instance.ProviderID = "uxh9he0pyoqpho"

	g := New(Config{WorkerSharedToken: "secret-token"}, r, manager)
	handler := g.requireWorkerToken(g.handleRegisterWorker)

	body := `{"worker_id":"w1","address":"uxh9he0pyoqpho-8081.proxy.runpod.net","status":"healthy","tags":{"provider":"runpod"},"loaded_models":[{"model_id":"Qwen/Qwen2.5-7B-Instruct","version":"main"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/workers/register", strings.NewReader(body))
	req.Header.Set("X-Worker-Token", "secret-token")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	linked, found := manager.GetInstance(instance.ID)
	if !found {
		t.Fatalf("expected instance %s to exist", instance.ID)
	}
	if linked.WorkerID != "w1" {
		t.Fatalf("expected instance worker_id to be linked to w1, got %q", linked.WorkerID)
	}
}

func TestHandleWorkerHeartbeatRepairsMissingInstanceLink(t *testing.T) {
	r := router.New(router.DefaultConfig())
	defer r.Stop()

	manager, err := providers.NewManager(providers.ManagerConfig{
		DefaultProvider: providers.ProviderMock,
	})
	if err != nil {
		t.Fatalf("providers.NewManager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	runpod := mockprovider.New()
	manager.RegisterProvider(runpod)

	instance, err := manager.Provision(context.Background(), &providers.ProvisionRequest{
		Name:     "heartbeat-link-instance",
		Provider: providers.ProviderMock,
		GPUType:  providers.GPUL40S,
		GPUCount: 1,
		Models:   []string{"Qwen/Qwen2.5-7B-Instruct"},
	})
	if err != nil {
		t.Fatalf("manager.Provision: %v", err)
	}

	instance.Provider = providers.ProviderRunPod
	instance.ProviderID = "uxh9he0pyoqpho"
	instance.WorkerID = ""

	if err := r.RegisterWorker(&types.WorkerInfo{
		WorkerID: "w1",
		Address:  "uxh9he0pyoqpho-8081.proxy.runpod.net",
		Status:   types.WorkerStatusHealthy,
		Tags: map[string]string{
			"provider": "runpod",
		},
	}); err != nil {
		t.Fatalf("RegisterWorker: %v", err)
	}

	g := New(Config{WorkerSharedToken: "secret-token"}, r, manager)
	handler := g.requireWorkerToken(g.handleWorkerHeartbeat)

	req := httptest.NewRequest(http.MethodPost, "/api/workers/heartbeat", strings.NewReader(`{"worker_id":"w1","stats":{"queue_depth":0,"active_requests":0}}`))
	req.Header.Set("X-Worker-Token", "secret-token")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	linked, found := manager.GetInstance(instance.ID)
	if !found {
		t.Fatalf("expected instance %s to exist", instance.ID)
	}
	if linked.WorkerID != "w1" {
		t.Fatalf("expected heartbeat to repair worker link to w1, got %q", linked.WorkerID)
	}
}

func TestToInferenceRequestBuildsExplicitAffinityMetadata(t *testing.T) {
	g := New(DefaultConfig(), nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("X-Infera-Affinity-Key", "chat-123")
	inferenceReq := g.toInferenceRequest(req, &ChatCompletionRequest{
		Model: "Qwen/Qwen2.5-7B-Instruct",
		Messages: []ChatMessage{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hello"},
		},
		Stream: true,
	})

	if inferenceReq.Metadata == nil {
		t.Fatal("expected affinity metadata")
	}
	if got := inferenceReq.Metadata[types.MetadataAffinitySource]; got != types.MetadataExplicitAffinity {
		t.Fatalf("expected explicit affinity source, got %q", got)
	}
	if got := inferenceReq.Metadata[types.MetadataAffinityKey]; !strings.Contains(got, "explicit:Qwen/Qwen2.5-7B-Instruct:chat-123") {
		t.Fatalf("expected explicit affinity key, got %q", got)
	}
	if got := inferenceReq.Metadata[types.MetadataPromptPrefixHash]; got == "" {
		t.Fatal("expected prompt prefix hash metadata")
	}
}

func TestToInferenceRequestBuildsAPIKeyAffinityMetadata(t *testing.T) {
	g := New(DefaultConfig(), nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req = req.WithContext(auth.ContextWithKey(req.Context(), &auth.KeyRecord{ID: "key-42"}))
	inferenceReq := g.toInferenceRequest(req, &ChatCompletionRequest{
		Model: "Qwen/Qwen2.5-7B-Instruct",
		Messages: []ChatMessage{
			{Role: "user", Content: "Explain caching."},
		},
	})

	if inferenceReq.Metadata == nil {
		t.Fatal("expected affinity metadata")
	}
	if got := inferenceReq.Metadata[types.MetadataAffinitySource]; got != types.MetadataAPIKeyAffinity {
		t.Fatalf("expected api-key affinity source, got %q", got)
	}
	if got := inferenceReq.Metadata[types.MetadataAffinityKey]; !strings.Contains(got, "key:key-42") {
		t.Fatalf("expected api-key affinity key, got %q", got)
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

func TestHandleChatCompletionsRecordsOverloadRejection(t *testing.T) {
	g := New(Config{MaxInFlight: 1}, nil, nil)
	atomic.StoreInt64(&g.inFlightRequests, 1)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"model-1","messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()
	g.handleChatCompletions(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Retry-After"); got != "5" {
		t.Fatalf("expected Retry-After header 5, got %q", got)
	}
	rejected := testutil.ToFloat64(g.metrics.inferenceRejected.WithLabelValues("overloaded"))
	if rejected != 1 {
		t.Fatalf("expected overload rejection metric=1, got %v", rejected)
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

func TestHandleChatCompletions_RejectsWhenWorkspaceQuotaExceeded(t *testing.T) {
	r := router.New(router.DefaultConfig())
	defer r.Stop()

	if err := r.RegisterWorker(&types.WorkerInfo{
		WorkerID:     "worker-1",
		Address:      "worker-1:8081",
		Status:       types.WorkerStatusHealthy,
		LoadedModels: []types.LoadedModel{{ModelID: "model-1"}},
	}); err != nil {
		t.Fatalf("RegisterWorker: %v", err)
	}

	authStore, err := auth.NewStore(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("auth.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = authStore.Close() })

	auditStore, err := audit.NewStore(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("audit.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = auditStore.Close() })

	workspace, err := authStore.CreateWorkspace("Billing Team")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	requestLimit := int64(1)
	if _, err := authStore.UpsertWorkspaceQuota(workspace.ID, &requestLimit, nil, true); err != nil {
		t.Fatalf("UpsertWorkspaceQuota: %v", err)
	}
	key, _, err := authStore.CreateKeyInWorkspace(workspace.ID, "workspace-admin", "admin")
	if err != nil {
		t.Fatalf("CreateKeyInWorkspace: %v", err)
	}
	record, err := authStore.ValidateKey(key)
	if err != nil {
		t.Fatalf("ValidateKey: %v", err)
	}

	now := time.Now().UTC()
	if err := auditStore.AppendInference(audit.InferenceAuditRecord{
		Timestamp:   now.Add(-time.Minute),
		RequestID:   "prior-req",
		KeyID:       record.KeyPrefix,
		WorkspaceID: workspace.ID,
		Model:       "model-1",
		Status:      "success",
		TokenCount:  10,
	}); err != nil {
		t.Fatalf("AppendInference: %v", err)
	}

	g := New(DefaultConfig(), r, nil)
	g.SetAuthHandler(auth.NewHandler(authStore))
	g.SetAuditStore(auditStore)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"model-1","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Authorization", "Bearer "+key)
	rec := httptest.NewRecorder()
	g.authHandler.RequireAuth(g.handleChatCompletions)(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "quota_exceeded") {
		t.Fatalf("expected quota_exceeded body, got %s", rec.Body.String())
	}
}
