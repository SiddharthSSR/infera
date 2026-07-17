package gateway

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/infera/infera/go/internal/auth"
	"github.com/infera/infera/go/internal/router"
	"github.com/infera/infera/go/internal/router/registry"
	"github.com/infera/infera/go/pkg/types"
)

type failingWorkerRegistry struct {
	snapshotErr error
	writeErr    error
}

type sequencedWorkerRegistry struct {
	*failingWorkerRegistry
	worker *types.WorkerInfo
	calls  atomic.Int32
}

type closeTrackingTransport struct {
	closeCalls atomic.Int32
}

func (t *closeTrackingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("unexpected request")
}

func (t *closeTrackingTransport) CloseIdleConnections() {
	t.closeCalls.Add(1)
}

func (r *sequencedWorkerRegistry) Snapshot(context.Context) ([]*types.WorkerInfo, error) {
	if r.calls.Add(1) == 1 {
		return []*types.WorkerInfo{r.worker.Clone()}, nil
	}
	return nil, r.snapshotErr
}

func (r *failingWorkerRegistry) Register(context.Context, *types.WorkerInfo) error {
	return r.writeErr
}

func (r *failingWorkerRegistry) Deregister(context.Context, string) error {
	return r.writeErr
}

func (r *failingWorkerRegistry) UpdateWorkerStats(context.Context, string, types.WorkerStats) error {
	return r.writeErr
}

func (r *failingWorkerRegistry) UpdateWorkerModels(context.Context, string, []types.LoadedModel) error {
	return r.writeErr
}

func (r *failingWorkerRegistry) Snapshot(context.Context) ([]*types.WorkerInfo, error) {
	return nil, r.snapshotErr
}

func (r *failingWorkerRegistry) StartHealthChecker(ctx context.Context) {
	<-ctx.Done()
}

func newGatewayWithWorkerRegistry(t *testing.T, workerState *failingWorkerRegistry) *Gateway {
	t.Helper()
	config := router.DefaultConfig()
	config.EnableBatching = false
	r := router.NewWithRegistry(config, workerState)
	t.Cleanup(r.Stop)
	return New(DefaultConfig(), r, nil)
}

func TestGatewayWorkerRegistryReadsFailClosed(t *testing.T) {
	internalDetail := "postgres://user:secret@database.internal/control"
	g := newGatewayWithWorkerRegistry(t, &failingWorkerRegistry{snapshotErr: errors.New(internalDetail)})
	authorized := auth.ContextWithKey(context.Background(), &auth.KeyRecord{
		WorkspaceID: "ws-test",
		Role:        auth.RoleOwner,
		Status:      "active",
	})

	tests := []struct {
		name    string
		path    string
		handler http.HandlerFunc
		ctx     context.Context
	}{
		{name: "health", path: "/health", handler: g.handleHealth, ctx: context.Background()},
		{name: "discovery", path: "/internal/prometheus/worker-targets", handler: g.handlePrometheusWorkerTargets, ctx: context.Background()},
		{name: "models", path: "/v1/models", handler: g.handleListModels, ctx: context.Background()},
		{name: "admin workers", path: "/api/workers", handler: g.handleGetWorkers, ctx: authorized},
		{name: "admin stats", path: "/api/stats", handler: g.handleGetStats, ctx: authorized},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, test.path, nil).WithContext(test.ctx)
			recorder := httptest.NewRecorder()
			test.handler(recorder, req)
			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("expected 503, got %d: %s", recorder.Code, recorder.Body.String())
			}
			body := recorder.Body.String()
			if !strings.Contains(body, "worker_registry_unavailable") {
				t.Fatalf("expected generic registry error, got %s", body)
			}
			if strings.Contains(body, "secret") || strings.Contains(body, "database.internal") {
				t.Fatalf("response exposed registry internals: %s", body)
			}
		})
	}
}

func TestGatewayRegistryContextErrorsPreserveHTTPBoundarySemantics(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{name: "canceled", err: context.Canceled, wantStatus: http.StatusOK, wantBody: ""},
		{name: "deadline", err: context.DeadlineExceeded, wantStatus: http.StatusGatewayTimeout, wantBody: "request_timeout"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := newGatewayWithWorkerRegistry(t, &failingWorkerRegistry{snapshotErr: test.err})
			handlers := []struct {
				name    string
				method  string
				path    string
				body    string
				handler http.HandlerFunc
			}{
				{name: "discovery", method: http.MethodGet, path: "/internal/prometheus/worker-targets", handler: g.handlePrometheusWorkerTargets},
				{name: "models", method: http.MethodGet, path: "/v1/models", handler: g.handleListModels},
				{name: "health", method: http.MethodGet, path: "/health", handler: g.handleHealth},
				{name: "agent model check", method: http.MethodPost, path: "/api/agents/runs", body: `{"model":"model-1","input":"hello"}`, handler: g.handleCreateAgentRun},
			}
			for _, endpoint := range handlers {
				t.Run(endpoint.name, func(t *testing.T) {
					req := httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader(endpoint.body))
					recorder := httptest.NewRecorder()
					endpoint.handler(recorder, req)
					if recorder.Code != test.wantStatus {
						t.Fatalf("expected %d, got %d: %s", test.wantStatus, recorder.Code, recorder.Body.String())
					}
					body := recorder.Body.String()
					if (test.wantBody == "" && body != "") || (test.wantBody != "" && !strings.Contains(body, test.wantBody)) {
						t.Fatalf("expected body %q, got %q", test.wantBody, body)
					}
				})
			}
		})
	}
}

func TestChatCompletionsRegistryOutageReturnsSanitized503(t *testing.T) {
	g := newGatewayWithWorkerRegistry(t, &failingWorkerRegistry{snapshotErr: errors.New("secret database host")})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(
		`{"model":"model-1","messages":[{"role":"user","content":"hello"}]}`,
	))
	recorder := httptest.NewRecorder()

	g.handleChatCompletions(recorder, req)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), string(types.ErrorCodeWorkerRegistryUnavailable)) {
		t.Fatalf("expected worker registry error, got %s", recorder.Body.String())
	}
	for _, forbidden := range []string{"secret", "database host", "model_not_found", "no_workers"} {
		if strings.Contains(recorder.Body.String(), forbidden) {
			t.Fatalf("response was not sanitized: %s", recorder.Body.String())
		}
	}
}

func TestWorkerClientPreservesRegistryOutageAfterSuccessfulRoute(t *testing.T) {
	worker := &types.WorkerInfo{
		WorkerID:     "worker-1",
		Address:      "http://worker.internal:8081",
		Status:       types.WorkerStatusHealthy,
		LoadedModels: []types.LoadedModel{{ModelID: "model-1"}},
	}
	workerState := &sequencedWorkerRegistry{
		failingWorkerRegistry: &failingWorkerRegistry{snapshotErr: errors.New("secret database host")},
		worker:                worker,
	}
	config := router.DefaultConfig()
	config.EnableBatching = false
	r := router.NewWithRegistry(config, workerState)
	t.Cleanup(r.Stop)
	g := New(DefaultConfig(), r, nil)
	g.workerClients[worker.WorkerID] = newWorkerClient(worker.Address, "cached-token")

	routed, err := r.Route(context.Background(), &types.InferenceRequest{RequestID: "req-1", ModelID: "model-1"})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	_, err = g.getWorkerClient(context.Background(), routed.WorkerID)
	if !errors.Is(err, errWorkerRegistryUnavailable) {
		t.Fatalf("expected preserved registry outage, got %v", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("client error exposed registry internals: %v", err)
	}
}

func TestInferencePreservesRegistryDeadlineAfterSuccessfulRoute(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "non-streaming", true: "streaming"}[stream], func(t *testing.T) {
			worker := &types.WorkerInfo{
				WorkerID:     "worker-1",
				SharedPool:   true,
				Address:      "http://worker.internal:8081",
				Status:       types.WorkerStatusHealthy,
				LoadedModels: []types.LoadedModel{{ModelID: "model-1"}},
			}
			workerState := &sequencedWorkerRegistry{
				failingWorkerRegistry: &failingWorkerRegistry{snapshotErr: context.DeadlineExceeded},
				worker:                worker,
			}
			config := router.DefaultConfig()
			config.EnableBatching = false
			r := router.NewWithRegistry(config, workerState)
			t.Cleanup(r.Stop)
			g := New(DefaultConfig(), r, nil)

			body := `{"model":"model-1","messages":[{"role":"user","content":"hello"}]`
			if stream {
				body += `,"stream":true`
			}
			body += `}`
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
			recorder := httptest.NewRecorder()
			g.handleChatCompletions(recorder, req)

			if recorder.Code != http.StatusGatewayTimeout {
				t.Fatalf("expected 504, got %d: %s", recorder.Code, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), "inference_timeout") {
				t.Fatalf("expected inference timeout, got %s", recorder.Body.String())
			}
			if strings.Contains(recorder.Body.String(), string(types.ErrorCodeWorkerRegistryUnavailable)) {
				t.Fatalf("deadline was relabeled as registry outage: %s", recorder.Body.String())
			}
		})
	}
}

func TestNonStreamingInferencePreservesRegistryCancellationAfterRoute(t *testing.T) {
	worker := &types.WorkerInfo{
		WorkerID:     "worker-1",
		SharedPool:   true,
		Address:      "http://worker.internal:8081",
		Status:       types.WorkerStatusHealthy,
		LoadedModels: []types.LoadedModel{{ModelID: "model-1"}},
	}
	workerState := &sequencedWorkerRegistry{
		failingWorkerRegistry: &failingWorkerRegistry{snapshotErr: context.Canceled},
		worker:                worker,
	}
	config := router.DefaultConfig()
	config.EnableBatching = false
	r := router.NewWithRegistry(config, workerState)
	t.Cleanup(r.Stop)
	g := New(DefaultConfig(), r, nil)

	_, err := g.executeNonStreamingInference(context.Background(), nil, &types.InferenceRequest{RequestID: "req-canceled", ModelID: "model-1"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected preserved cancellation, got %v", err)
	}
}

func TestWorkerClientCacheTracksRegistrationAndCredentialIdentity(t *testing.T) {
	r := router.New(router.DefaultConfig())
	t.Cleanup(r.Stop)
	registeredAt := time.Now()
	worker := &types.WorkerInfo{
		WorkerID:     "worker-1",
		Address:      "http://worker-a.internal:8081",
		Status:       types.WorkerStatusHealthy,
		LoadedModels: []types.LoadedModel{{ModelID: "model-1"}},
		RegisteredAt: registeredAt,
	}
	if err := r.RegisterWorker(context.Background(), worker); err != nil {
		t.Fatal(err)
	}
	gatewayConfig := DefaultConfig()
	gatewayConfig.WorkerSharedToken = "token-a"
	g := New(gatewayConfig, r, nil)

	clientA, err := g.getWorkerClient(context.Background(), worker.WorkerID)
	if err != nil {
		t.Fatal(err)
	}
	replacement := worker.Clone()
	replacement.Address = "http://worker-b.internal:8081"
	replacement.RegisteredAt = registeredAt.Add(time.Second)
	if err := r.RegisterWorker(context.Background(), replacement); err != nil {
		t.Fatal(err)
	}
	clientB, err := g.getWorkerClient(context.Background(), worker.WorkerID)
	if err != nil {
		t.Fatal(err)
	}
	if clientB == clientA || clientB.address != replacement.Address {
		t.Fatalf("address change reused stale client: old=%p new=%p address=%q", clientA, clientB, clientB.address)
	}
	replacementRegistrationID := replacement.RegistrationID

	// Credential changes become visible to other replicas through a fresh
	// registration generation. Avoid polling the credential store on every
	// inference while still rebuilding for worker-ID reuse or token rotation.
	g.config.WorkerSharedToken = "token-b"
	unchanged, err := g.getWorkerClient(context.Background(), worker.WorkerID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged != clientB || unchanged.workerToken != "token-a" {
		t.Fatalf("unchanged registration unexpectedly rebuilt client: old=%p new=%p token=%q", clientB, unchanged, unchanged.workerToken)
	}

	rotated := replacement.Clone()
	rotated.RegisteredAt = replacement.RegisteredAt
	if err := r.RegisterWorker(context.Background(), rotated); err != nil {
		t.Fatal(err)
	}
	if rotated.RegistrationID == replacementRegistrationID {
		t.Fatal("same-millisecond re-registration reused opaque registration identity")
	}
	clientC, err := g.getWorkerClient(context.Background(), worker.WorkerID)
	if err != nil {
		t.Fatal(err)
	}
	if clientC == clientB || clientC.workerToken != "token-b" {
		t.Fatalf("credential change reused stale client: old=%p new=%p token=%q", clientB, clientC, clientC.workerToken)
	}
}

func TestWorkerClientResolutionNeverOverwritesNewerCachedRegistration(t *testing.T) {
	g := New(DefaultConfig(), router.New(router.DefaultConfig()), nil)
	t.Cleanup(g.router.Stop)
	observed := newRegisteredWorkerClient("http://worker-a.internal:8081", "token-a", "registration-a")
	newer := newRegisteredWorkerClient("http://worker-c.internal:8081", "token-c", "registration-c")
	replacement := newRegisteredWorkerClient("http://worker-b.internal:8081", "token-b", "registration-b")
	g.workerClients["worker-1"] = newer

	current, installed := g.installWorkerClientIfUnchanged("worker-1", observed, replacement)
	if installed || current != newer {
		t.Fatalf("stale cache observation installed=%t current=%p, want newer %p", installed, current, newer)
	}
	if cached := g.workerClients["worker-1"]; cached != newer {
		t.Fatalf("stale registry snapshot overwrote newer cached client: got %p want %p", cached, newer)
	}
}

func TestWorkerClientResolutionFailsClosedWhenRegistrationChangesDuringCredentialLookup(t *testing.T) {
	r := router.New(router.DefaultConfig())
	t.Cleanup(r.Stop)
	worker := &types.WorkerInfo{
		WorkerID:     "worker-1",
		Address:      "http://worker-b.internal:8081",
		Status:       types.WorkerStatusHealthy,
		LoadedModels: []types.LoadedModel{{ModelID: "model-1"}},
	}
	if err := r.RegisterWorker(context.Background(), worker); err != nil {
		t.Fatal(err)
	}
	g := New(DefaultConfig(), r, nil)
	g.workerClients[worker.WorkerID] = newRegisteredWorkerClient(worker.Address, "token-a", "registration-a")

	credentialStarted := make(chan struct{})
	releaseCredential := make(chan struct{})
	g.workerCredentialResolver = func(string) (string, error) {
		close(credentialStarted)
		<-releaseCredential
		return "token-b", nil
	}
	result := make(chan error, 1)
	go func() {
		_, err := g.getWorkerClient(context.Background(), worker.WorkerID)
		result <- err
	}()
	<-credentialStarted

	newRegistration := worker.Clone()
	newRegistration.Address = "http://worker-c.internal:8081"
	if err := r.RegisterWorker(context.Background(), newRegistration); err != nil {
		t.Fatal(err)
	}
	newerClient := newRegisteredWorkerClient(newRegistration.Address, "token-c", newRegistration.RegistrationID)
	g.workerClientsMu.Lock()
	g.workerClients[worker.WorkerID] = newerClient
	g.workerClientsMu.Unlock()
	close(releaseCredential)

	if err := <-result; err == nil || !strings.Contains(err.Error(), "registration changed") {
		t.Fatalf("expected registration-change failure, got %v", err)
	}
	if cached := g.workerClients[worker.WorkerID]; cached != newerClient {
		t.Fatalf("stale resolution overwrote concurrent registration: got %p want %p", cached, newerClient)
	}
}

func TestWorkerClientCacheClosesDisplacedTransports(t *testing.T) {
	g := New(DefaultConfig(), router.New(router.DefaultConfig()), nil)
	t.Cleanup(g.router.Stop)

	registerTransport := &closeTrackingTransport{}
	g.workerClients["worker-1"] = &WorkerClient{
		httpClient:          &http.Client{Transport: registerTransport},
		streamingHTTPClient: &http.Client{Transport: registerTransport},
	}
	g.registerWorkerClient("worker-1", "http://worker.internal:8081", "token", "registration-1")
	if calls := registerTransport.closeCalls.Load(); calls != 2 {
		t.Fatalf("register replacement closed idle transports %d times, want 2", calls)
	}

	removeTransport := &closeTrackingTransport{}
	g.workerClients["worker-1"] = &WorkerClient{
		httpClient:          &http.Client{Transport: removeTransport},
		streamingHTTPClient: &http.Client{Transport: removeTransport},
	}
	g.RemoveWorkerClient("worker-1")
	if calls := removeTransport.closeCalls.Load(); calls != 2 {
		t.Fatalf("removal closed idle transports %d times, want 2", calls)
	}
}

func TestRegisterWorkerClientRejectsAddressOutsideRegistration(t *testing.T) {
	r := router.New(router.DefaultConfig())
	t.Cleanup(r.Stop)
	worker := &types.WorkerInfo{WorkerID: "worker-1", Address: "http://worker.internal:8081", Status: types.WorkerStatusHealthy}
	if err := r.RegisterWorker(context.Background(), worker); err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.WorkerSharedToken = "token"
	g := New(config, r, nil)

	if err := g.RegisterWorkerClient(worker.WorkerID, "http://attacker.internal:8081"); err == nil {
		t.Fatal("expected mismatched address to be rejected")
	}
	if _, exists := g.workerClients[worker.WorkerID]; exists {
		t.Fatal("mismatched address populated worker client cache")
	}
}

func TestHeartbeatAcknowledgesFalseOnlyForMissingRegistration(t *testing.T) {
	body := []byte(`{"worker_id":"worker-1","stats":{}}`)

	t.Run("registry outage returns sanitized 503", func(t *testing.T) {
		g := newGatewayWithWorkerRegistry(t, &failingWorkerRegistry{writeErr: errors.New("secret database failure")})
		req := httptest.NewRequest(http.MethodPost, "/api/workers/heartbeat", bytes.NewReader(body))
		recorder := httptest.NewRecorder()
		g.handleWorkerHeartbeat(recorder, req)
		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %d: %s", recorder.Code, recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), "secret") {
			t.Fatalf("response exposed registry internals: %s", recorder.Body.String())
		}
	})

	t.Run("missing registration is acknowledged false", func(t *testing.T) {
		g := newGatewayWithWorkerRegistry(t, &failingWorkerRegistry{writeErr: registry.ErrWorkerNotFound})
		req := httptest.NewRequest(http.MethodPost, "/api/workers/heartbeat", bytes.NewReader(body))
		recorder := httptest.NewRecorder()
		g.handleWorkerHeartbeat(recorder, req)
		if recorder.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
		}
		if !strings.Contains(recorder.Body.String(), `"acknowledged":false`) {
			t.Fatalf("expected acknowledged false, got %s", recorder.Body.String())
		}
	})

	t.Run("registration removed after stats update is acknowledged false", func(t *testing.T) {
		g := newGatewayWithWorkerRegistry(t, &failingWorkerRegistry{})
		req := httptest.NewRequest(http.MethodPost, "/api/workers/heartbeat", bytes.NewReader(body))
		recorder := httptest.NewRecorder()
		g.handleWorkerHeartbeat(recorder, req)
		if recorder.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
		}
		if !strings.Contains(recorder.Body.String(), `"acknowledged":false`) {
			t.Fatalf("expected acknowledged false, got %s", recorder.Body.String())
		}
	})
}
