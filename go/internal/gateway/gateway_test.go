package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/infera/infera/go/internal/audit"
	"github.com/infera/infera/go/internal/auth"
	"github.com/infera/infera/go/internal/providers"
	"github.com/infera/infera/go/internal/router"
	"github.com/infera/infera/go/pkg/types"
)

type runPodLinkTestProvider struct {
	instances   map[string]*providers.Instance
	workerToken string
}

func newRunPodLinkTestProvider() *runPodLinkTestProvider {
	return &runPodLinkTestProvider{
		instances: make(map[string]*providers.Instance),
	}
}

func (p *runPodLinkTestProvider) Name() providers.ProviderType {
	return providers.ProviderRunPod
}

func (p *runPodLinkTestProvider) Provision(ctx context.Context, req *providers.ProvisionRequest) (*providers.Instance, error) {
	p.workerToken = req.WorkerToken
	instance := &providers.Instance{
		ID:         "inst-runpod-link",
		ProviderID: "uxh9he0pyoqpho",
		Provider:   providers.ProviderRunPod,
		Name:       req.Name,
		Status:     providers.InstanceStatusRunning,
		GPUType:    req.GPUType,
		GPUCount:   req.GPUCount,
		Models:     append([]string(nil), req.Models...),
		Engine:     req.Engine,
		CreatedAt:  time.Now().UTC(),
	}
	p.instances[instance.ID] = instance
	return instance, nil
}

func (p *runPodLinkTestProvider) Terminate(ctx context.Context, instanceID string) error {
	return nil
}

func (p *runPodLinkTestProvider) Start(ctx context.Context, instanceID string) error {
	return nil
}

func (p *runPodLinkTestProvider) Stop(ctx context.Context, instanceID string) error {
	return nil
}

func (p *runPodLinkTestProvider) GetInstance(ctx context.Context, instanceID string) (*providers.Instance, error) {
	for _, instance := range p.instances {
		if instance.ID == instanceID || instance.ProviderID == instanceID {
			return instance, nil
		}
	}
	return nil, &providers.ProviderError{
		Provider: providers.ProviderRunPod,
		Code:     providers.ProviderErrorNotFound,
		Message:  "instance not found",
	}
}

func (p *runPodLinkTestProvider) ListInstances(ctx context.Context) ([]*providers.Instance, error) {
	instances := make([]*providers.Instance, 0, len(p.instances))
	for _, instance := range p.instances {
		instances = append(instances, instance)
	}
	return instances, nil
}

func (p *runPodLinkTestProvider) ListOfferings(ctx context.Context) ([]*providers.GPUOffering, error) {
	return nil, nil
}

func (p *runPodLinkTestProvider) GetStatus(ctx context.Context) (*providers.ProviderStatus, error) {
	return &providers.ProviderStatus{
		Provider:  providers.ProviderRunPod,
		Connected: true,
	}, nil
}

func (p *runPodLinkTestProvider) WaitForReady(ctx context.Context, instanceID string) error {
	return nil
}

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

func TestRegisterWorkerRejectsIncompatibleRolloutIdentity(t *testing.T) {
	r := router.New(router.DefaultConfig())
	defer r.Stop()

	config := DefaultConfig()
	config.WorkerSharedToken = "secret-token"
	config.ReleaseID = "release-1"
	config.WorkerProtocolVersion = "1"
	config.RequireMatchingWorkerRelease = true
	g := New(config, r, nil)
	handler := g.requireWorkerToken(g.handleRegisterWorker)

	tests := []struct {
		name string
		body string
		code string
	}{
		{
			name: "protocol mismatch",
			body: `{"worker_id":"w1","address":"localhost:8081","status":"healthy","release_id":"release-1","protocol_version":"2"}`,
			code: "worker_protocol_mismatch",
		},
		{
			name: "release mismatch",
			body: `{"worker_id":"w1","address":"localhost:8081","status":"healthy","release_id":"release-0","protocol_version":"1"}`,
			code: "worker_release_mismatch",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/workers/register", strings.NewReader(tc.body))
			req.Header.Set("X-Worker-Token", "secret-token")
			rec := httptest.NewRecorder()
			handler(rec, req)
			if rec.Code != http.StatusConflict {
				t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.code) {
				t.Fatalf("expected error code %q, got %s", tc.code, rec.Body.String())
			}
		})
	}
}

func TestHandleRegisterWorkerLinksRunPodInstanceByProxyAddress(t *testing.T) {
	r := router.New(router.DefaultConfig())
	defer r.Stop()

	manager, err := providers.NewManager(providers.ManagerConfig{
		DefaultProvider: providers.ProviderMock,
		WorkerImage:     "ghcr.io/infera/infera-worker:v1.0.0",
	})
	if err != nil {
		t.Fatalf("providers.NewManager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	runpod := newRunPodLinkTestProvider()
	manager.RegisterProvider(runpod)

	instance, err := manager.Provision(context.Background(), &providers.ProvisionRequest{
		Name:     "runpod-like-instance",
		Provider: providers.ProviderRunPod,
		GPUType:  providers.GPUL40S,
		GPUCount: 1,
		Models:   []string{"Qwen/Qwen2.5-7B-Instruct"},
	})
	if err != nil {
		t.Fatalf("manager.Provision: %v", err)
	}

	g := New(Config{WorkerSharedToken: "secret-token"}, r, manager)
	handler := g.requireWorkerToken(g.handleRegisterWorker)

	body := `{"worker_id":"w1","address":"uxh9he0pyoqpho-8081.proxy.runpod.net","status":"healthy","tags":{"provider":"runpod"},"loaded_models":[{"model_id":"Qwen/Qwen2.5-7B-Instruct","version":"main"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/workers/register", strings.NewReader(body))
	req.Header.Set("X-Worker-Token", runpod.workerToken)
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
	client, err := g.getWorkerClient(context.Background(), "w1")
	if err != nil {
		t.Fatalf("getWorkerClient: %v", err)
	}
	client.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("X-Worker-Token"); got != runpod.workerToken {
			t.Fatalf("expected deployment-bound worker token, got %q", got)
		}
		return jsonHTTPResponse(http.StatusOK, `{"request_id":"req-1","model_id":"Qwen/Qwen2.5-7B-Instruct","choices":[],"usage":{},"latency":{}}`), nil
	})
	if _, err := client.InferWithContext(context.Background(), &types.InferenceRequest{
		RequestID:  "req-1",
		ModelID:    "Qwen/Qwen2.5-7B-Instruct",
		Parameters: types.DefaultInferenceParameters(),
	}); err != nil {
		t.Fatalf("managed worker inference: %v", err)
	}

	replacement := httptest.NewRequest(http.MethodPost, "/api/workers/register", strings.NewReader(`{"worker_id":"w2","address":"uxh9he0pyoqpho-8081.proxy.runpod.net","status":"healthy"}`))
	replacement.Header.Set("X-Worker-Token", runpod.workerToken)
	replacementRec := httptest.NewRecorder()
	handler(replacementRec, replacement)
	if replacementRec.Code != http.StatusForbidden {
		t.Fatalf("expected deployment-bound credential to reject worker identity change, got %d body=%s", replacementRec.Code, replacementRec.Body.String())
	}
	if strings.Contains(replacementRec.Body.String(), "already bound") || !strings.Contains(replacementRec.Body.String(), "worker_identity_mismatch") {
		t.Fatalf("identity conflict response was unstable or leaked details: %s", replacementRec.Body.String())
	}
	if _, found, err := r.GetWorker(context.Background(), "w2"); err != nil || found {
		t.Fatal("rejected identity change mutated the worker registry")
	}
}

func TestWorkerHeartbeatIdentityConflictReturnsStableForbidden(t *testing.T) {
	r := router.New(router.DefaultConfig())
	defer r.Stop()
	manager, err := providers.NewManager(providers.ManagerConfig{
		DefaultProvider: providers.ProviderMock,
		WorkerImage:     "ghcr.io/infera/infera-worker:v1.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	provider := newRunPodLinkTestProvider()
	manager.RegisterProvider(provider)
	instance, err := manager.Provision(context.Background(), &providers.ProvisionRequest{
		Name: "heartbeat-conflict", Provider: providers.ProviderRunPod,
		GPUType: providers.GPUL40S, GPUCount: 1, Models: []string{"model-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.LinkWorker(instance.ID, "worker-a"); err != nil {
		t.Fatal(err)
	}
	g := New(DefaultConfig(), r, manager)
	handler := g.requireWorkerToken(g.handleWorkerHeartbeat)
	req := httptest.NewRequest(http.MethodPost, "/api/workers/heartbeat", strings.NewReader(`{"worker_id":"worker-b","stats":{}}`))
	req.Header.Set("X-Worker-Token", provider.workerToken)
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "worker_identity_mismatch") || strings.Contains(rec.Body.String(), "worker-a") {
		t.Fatalf("unexpected heartbeat identity response: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestWorkerControlStateFailureReturnsGenericUnavailable(t *testing.T) {
	g := New(DefaultConfig(), nil, nil)
	rec := httptest.NewRecorder()
	g.writeWorkerControlError(rec, fmt.Errorf("%w: postgres password=do-not-leak", providers.ErrControlStateUnavailable))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "worker_control_state_unavailable") {
		t.Fatalf("unexpected unavailable response: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "postgres") || strings.Contains(rec.Body.String(), "password") {
		t.Fatalf("control-state response leaked internal details: %s", rec.Body.String())
	}
}

func TestHandleWorkerHeartbeatRepairsMissingInstanceLink(t *testing.T) {
	r := router.New(router.DefaultConfig())
	defer r.Stop()

	manager, err := providers.NewManager(providers.ManagerConfig{
		DefaultProvider: providers.ProviderMock,
		WorkerImage:     "ghcr.io/infera/infera-worker:v1.0.0",
	})
	if err != nil {
		t.Fatalf("providers.NewManager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	runpod := newRunPodLinkTestProvider()
	manager.RegisterProvider(runpod)

	instance, err := manager.Provision(context.Background(), &providers.ProvisionRequest{
		Name:     "heartbeat-link-instance",
		Provider: providers.ProviderRunPod,
		GPUType:  providers.GPUL40S,
		GPUCount: 1,
		Models:   []string{"Qwen/Qwen2.5-7B-Instruct"},
	})
	if err != nil {
		t.Fatalf("manager.Provision: %v", err)
	}

	if err := r.RegisterWorker(context.Background(), &types.WorkerInfo{
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
	req.Header.Set("X-Worker-Token", runpod.workerToken)
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

func TestToInferenceRequestSeparatesExecutionIdentityFromTenantClientIdentity(t *testing.T) {
	g := New(DefaultConfig(), nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set(HeaderRequestID, "client-retry-key")
	req = req.WithContext(auth.ContextWithKey(req.Context(), &auth.KeyRecord{
		ID:          "key-42",
		WorkspaceID: "ws_alpha",
	}))

	first := g.toInferenceRequest(req, &ChatCompletionRequest{Model: "approved-model"})
	second := g.toInferenceRequest(req, &ChatCompletionRequest{Model: "approved-model"})

	if first.RequestID == "" || first.RequestID == second.RequestID {
		t.Fatalf("expected unique server execution IDs, got %q and %q", first.RequestID, second.RequestID)
	}
	if first.RequestID == "client-retry-key" || first.ClientRequestID != "client-retry-key" {
		t.Fatalf("expected separate server and client request IDs, got server=%q client=%q", first.RequestID, first.ClientRequestID)
	}
	if first.WorkspaceID != "ws_alpha" {
		t.Fatalf("expected workspace-scoped routing identity, got %q", first.WorkspaceID)
	}
}

func TestHandlePrometheusWorkerTargets(t *testing.T) {
	r := router.New(router.DefaultConfig())
	defer r.Stop()

	if err := r.RegisterWorker(context.Background(), &types.WorkerInfo{
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
	if err := r.RegisterWorker(context.Background(), &types.WorkerInfo{
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

func TestHandleMetricsExposesWorkerCounts(t *testing.T) {
	r := router.New(router.DefaultConfig())
	defer r.Stop()

	if err := r.RegisterWorker(context.Background(), &types.WorkerInfo{
		WorkerID: "worker-healthy",
		Address:  "worker-healthy:8081",
		Status:   types.WorkerStatusHealthy,
	}); err != nil {
		t.Fatalf("RegisterWorker healthy: %v", err)
	}
	if err := r.RegisterWorker(context.Background(), &types.WorkerInfo{
		WorkerID: "worker-unhealthy",
		Address:  "worker-unhealthy:8081",
		Status:   types.WorkerStatusUnhealthy,
	}); err != nil {
		t.Fatalf("RegisterWorker unhealthy: %v", err)
	}

	g := New(DefaultConfig(), r, nil)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	g.handleMetrics(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"infera_workers_total 2",
		"infera_healthy_workers_total 1",
		"infera_unhealthy_workers_total 1",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected metrics output to contain %q, got:\n%s", want, body)
		}
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
	client := NewWorkerClient("http://localhost:8081")
	client.streamingHTTPClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var builder strings.Builder
		encoder := json.NewEncoder(&builder)
		if err := encoder.Encode(map[string]any{"delta": ""}); err != nil {
			t.Fatalf("encode leading empty chunk: %v", err)
		}
		if err := encoder.Encode(map[string]any{"delta": "", "usage": map[string]int{"completion_tokens": 1}}); err != nil {
			t.Fatalf("encode leading usage-only chunk: %v", err)
		}
		if err := encoder.Encode(map[string]any{
			"delta": "Hello",
			"usage": map[string]int{
				"prompt_tokens": 5,
			},
		}); err != nil {
			t.Fatalf("encode first chunk: %v", err)
		}
		if err := encoder.Encode(map[string]any{
			"delta": " world",
			"usage": map[string]int{
				"prompt_tokens":     5,
				"completion_tokens": 2,
				"total_tokens":      7,
			},
		}); err != nil {
			t.Fatalf("encode second chunk: %v", err)
		}
		if err := encoder.Encode(map[string]any{"delta": "", "finish_reason": "stop"}); err != nil {
			t.Fatalf("encode terminal chunk: %v", err)
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

	result := g.handleStreamingInference(rec, httpReq, client, req, "model-1", "least_loaded", time.Now())

	if result.Status != "success" {
		t.Fatalf("expected success status, got %q", result.Status)
	}
	if result.Usage.TotalTokens != 7 {
		t.Fatalf("expected recomputed token count 7, got %d", result.Usage.TotalTokens)
	}
	if got := histogramCountForLabels(t, g.metrics, "infera_gateway_slo_v1_ttft_seconds", map[string]string{
		"measurement":      "exact",
		"model":            "model-1",
		"routing_strategy": "least_loaded",
		"stream":           "true",
	}); got != 1 {
		t.Fatalf("expected exact streaming TTFT sample, got %d", got)
	}
	if got := testutil.ToFloat64(g.metrics.sloMeasurements.WithLabelValues("tpot", "model-1", "least_loaded", "true", "derived")); got != 1 {
		t.Fatalf("expected derived streaming TPOT measurement count=1, got %v", got)
	}
	if got := histogramCountForLabels(t, g.metrics, "infera_gateway_slo_v1_tpot_seconds", map[string]string{
		"measurement":      "derived",
		"model":            "model-1",
		"routing_strategy": "least_loaded",
		"stream":           "true",
	}); got != 1 {
		t.Fatalf("expected only the interval between two usable outputs to produce TPOT, got %d samples", got)
	}
}

func TestWorkerClientInferWithContextForwardsAndDecodesToolCalls(t *testing.T) {
	client := NewWorkerClient("http://localhost:8081")
	client.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/infer" {
			t.Fatalf("expected /infer request, got %s", r.URL.Path)
		}
		var payload WorkerInferRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(payload.Tools) != 1 || payload.Tools[0].Function.Name != "web_search" {
			t.Fatalf("expected forwarded tools, got %+v", payload.Tools)
		}
		if string(payload.ToolChoice) != `{"type":"function","function":{"name":"web_search"}}` {
			t.Fatalf("expected forwarded tool_choice, got %s", string(payload.ToolChoice))
		}
		if len(payload.Messages) != 3 || len(payload.Messages[1].ToolCalls) != 1 || payload.Messages[2].ToolCallID != "call_1" {
			t.Fatalf("expected tool-call message history, got %+v", payload.Messages)
		}

		return jsonHTTPResponse(http.StatusOK, `{
			"request_id":"req-1",
			"model_id":"model-1",
			"choices":[{
				"index":0,
				"message":{
					"role":"assistant",
					"content":"",
					"tool_calls":[{
						"id":"call_2",
						"type":"function",
						"function":{"name":"web_search","arguments":"{\"query\":\"rust\"}"}
					}]
				},
				"finish_reason":"tool_calls"
			}],
			"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6},
			"latency":{"queue_ms":1,"inference_ms":2,"total_ms":3,"time_to_first_token_ms":1}
		}`), nil
	})

	resp, err := client.InferWithContext(context.Background(), &types.InferenceRequest{
		RequestID: "req-1",
		ModelID:   "model-1",
		Messages: []types.Message{
			{Role: types.RoleUser, Content: "search for go vs rust"},
			{Role: types.RoleAssistant, ToolCalls: []types.ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: types.FunctionCall{
					Name:      "web_search",
					Arguments: `{"query":"go"}`,
				},
			}}},
			{Role: types.RoleTool, Content: `{"ok":true}`, ToolCallID: "call_1"},
		},
		Parameters: types.DefaultInferenceParameters(),
		Tools: []types.ToolDefinition{{
			Type: "function",
			Function: types.FunctionSchema{
				Name: "web_search",
			},
		}},
		ToolChoice: json.RawMessage(`{"type":"function","function":{"name":"web_search"}}`),
	})
	if err != nil {
		t.Fatalf("InferWithContext: %v", err)
	}
	if len(resp.Choices) != 1 || len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected tool calls in response, got %+v", resp.Choices)
	}
	if resp.Choices[0].Message.ToolCalls[0].Function.Name != "web_search" {
		t.Fatalf("expected decoded tool call, got %+v", resp.Choices[0].Message.ToolCalls[0])
	}
}

func TestWorkerClientInferStreamForwardsAndDecodesToolCallChunks(t *testing.T) {
	client := NewWorkerClient("http://localhost:8081")
	client.streamingHTTPClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/infer/stream" {
			t.Fatalf("expected /infer/stream request, got %s", r.URL.Path)
		}
		var payload WorkerInferRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(payload.Tools) != 1 || payload.Tools[0].Function.Name != "web_search" {
			t.Fatalf("expected forwarded tools, got %+v", payload.Tools)
		}
		if len(payload.Messages) != 1 || len(payload.Messages[0].ToolCalls) != 1 {
			t.Fatalf("expected forwarded tool-call messages, got %+v", payload.Messages)
		}

		var builder strings.Builder
		encoder := json.NewEncoder(&builder)
		if err := encoder.Encode(map[string]any{
			"delta": "",
			"tool_calls": []map[string]any{
				{
					"index": 0,
					"id":    "call_1",
					"type":  "function",
					"function": map[string]any{
						"name":      "web_search",
						"arguments": `{"query":"go"}`,
					},
				},
			},
			"finish_reason": "tool_calls",
			"usage": map[string]int{
				"prompt_tokens":     5,
				"completion_tokens": 1,
				"total_tokens":      6,
			},
		}); err != nil {
			t.Fatalf("encode chunk: %v", err)
		}
		return jsonHTTPResponse(http.StatusOK, builder.String()), nil
	})

	chunks, err := client.InferStream(context.Background(), &types.InferenceRequest{
		RequestID: "req-1",
		ModelID:   "model-1",
		Messages: []types.Message{
			{Role: types.RoleAssistant, ToolCalls: []types.ToolCall{{
				ID:   "call_0",
				Type: "function",
				Function: types.FunctionCall{
					Name:      "web_search",
					Arguments: `{"query":"rust"}`,
				},
			}}},
		},
		Parameters: types.DefaultInferenceParameters(),
		Tools: []types.ToolDefinition{{
			Type: "function",
			Function: types.FunctionSchema{
				Name: "web_search",
			},
		}},
		ToolChoice: json.RawMessage(`{"type":"function","function":{"name":"web_search"}}`),
	})
	if err != nil {
		t.Fatalf("InferStream: %v", err)
	}

	var got []*types.TokenChunk
	for chunk := range chunks {
		got = append(got, chunk)
	}
	if len(got) != 1 {
		t.Fatalf("expected one chunk, got %d", len(got))
	}
	if len(got[0].ToolCalls) != 1 || got[0].ToolCalls[0].Function.Name != "web_search" {
		t.Fatalf("expected decoded tool-call deltas, got %+v", got[0].ToolCalls)
	}
	if got[0].FinishReason == nil || *got[0].FinishReason != types.FinishReasonToolCalls {
		t.Fatalf("expected tool_calls finish reason, got %+v", got[0].FinishReason)
	}
}

func TestHandleChatCompletions_RejectsWhenWorkspaceQuotaExceeded(t *testing.T) {
	r := router.New(router.DefaultConfig())
	defer r.Stop()

	if err := r.RegisterWorker(context.Background(), &types.WorkerInfo{
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

func TestWorkspaceQuotaAdmissionIsAtomicUnderConcurrency(t *testing.T) {
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
	workspace, err := authStore.CreateWorkspace("Concurrent Billing Team")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	requestLimit := int64(1)
	if _, err := authStore.UpsertWorkspaceQuota(workspace.ID, &requestLimit, nil, true); err != nil {
		t.Fatalf("UpsertWorkspaceQuota: %v", err)
	}
	rawKey, _, err := authStore.CreateKeyInWorkspace(workspace.ID, "workspace-admin", "admin")
	if err != nil {
		t.Fatalf("CreateKeyInWorkspace: %v", err)
	}
	key, err := authStore.ValidateKey(rawKey)
	if err != nil {
		t.Fatalf("ValidateKey: %v", err)
	}

	g := New(DefaultConfig(), nil, nil)
	g.SetAuthHandler(auth.NewHandler(authStore))
	if unavailable := g.enforceWorkspaceQuotaForKey(key, types.NewInferenceRequest("model-1", nil)); unavailable == nil || unavailable.Code != types.ErrorCode("quota_unavailable") {
		t.Fatalf("expected hard quota to fail closed without an accounting store, got %+v", unavailable)
	}
	g.SetAuditStore(auditStore)
	t.Cleanup(func() {
		if g.auditCh != nil {
			close(g.auditCh)
			g.auditWg.Wait()
			g.auditCh = nil
		}
	})

	start := make(chan struct{})
	results := make(chan *types.InferaError, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- g.enforceWorkspaceQuotaForKey(key, types.NewInferenceRequest("model-1", []types.Message{{Role: types.RoleUser, Content: "hello"}}))
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var admitted, rejected int
	for result := range results {
		if result == nil {
			admitted++
			continue
		}
		if result.Code != types.ErrorCode("quota_exceeded") {
			t.Fatalf("unexpected quota error: %+v", result)
		}
		rejected++
	}
	if admitted != 1 || rejected != 1 {
		t.Fatalf("expected one admission and one rejection, got admitted=%d rejected=%d", admitted, rejected)
	}
}
