package vastai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/infera/infera/go/internal/providers"
)

func TestNewRequiresAPIKey(t *testing.T) {
	_, err := New(Config{})
	if err == nil {
		t.Fatal("expected missing api key error")
	}

	var providerErr *providers.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Code != providers.ProviderErrorMissingAPIKey {
		t.Fatalf("expected %q, got %q", providers.ProviderErrorMissingAPIKey, providerErr.Code)
	}
}

func TestNewRejectsUnsafeEndpoints(t *testing.T) {
	for _, endpoint := range []string{"http://api.example.com", "https://127.0.0.1", "https://169.254.169.254/latest"} {
		if _, err := New(Config{APIKey: "key", Endpoint: endpoint}); err == nil {
			t.Fatalf("expected endpoint %q to be rejected", endpoint)
		}
	}
}

func TestDoJSONPreservesPublicCustomEndpoint(t *testing.T) {
	provider, err := New(Config{APIKey: "key", Endpoint: "https://provider.example.com/custom"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var gotURL, gotMethod string
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotURL, gotMethod = req.URL.String(), req.Method
		return httpResponse(http.StatusOK, `[]`), nil
	})
	var out []any
	if err := provider.doJSON(context.Background(), http.MethodGet, "/instances", nil, &out); err != nil {
		t.Fatalf("doJSON: %v", err)
	}
	if gotMethod != http.MethodGet || gotURL != "https://provider.example.com/custom/instances" {
		t.Fatalf("custom endpoint behavior changed: %s %s", gotMethod, gotURL)
	}
}

func TestDoJSONMapsAuthFailure(t *testing.T) {
	provider := newTestProvider(t)
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return httpResponse(http.StatusUnauthorized, `{"error":"forbidden"}`), nil
	})

	err := provider.doJSON(context.Background(), http.MethodGet, "/instances", nil, nil)
	if err == nil {
		t.Fatal("expected auth failure")
	}

	var providerErr *providers.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Code != providers.ProviderErrorAuthFailed {
		t.Fatalf("expected auth_failed, got %q", providerErr.Code)
	}
}

func TestDoJSONRejectsOversizedBodiesBeforeStatusOrJSON(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			provider := newTestProvider(t)
			body := strings.Repeat("x", int(providers.MaxProviderResponseBytes)+1)
			provider.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return httpResponse(status, body), nil
			})
			err := provider.doJSON(context.Background(), http.MethodGet, "/instances", nil, &map[string]any{})
			var providerErr *providers.ProviderError
			if !errors.As(err, &providerErr) || providerErr.Code != providers.ProviderErrorResponseTooLarge {
				t.Fatalf("expected response_too_large, got %v", err)
			}
		})
	}
}

func TestGetInstanceReturnsNotFoundWhenMissing(t *testing.T) {
	provider := newTestProvider(t)
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return httpResponse(http.StatusNotFound, `not found`), nil
	})

	_, err := provider.GetInstance(context.Background(), "inst-missing")
	if err == nil {
		t.Fatal("expected not_found error")
	}

	var providerErr *providers.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Code != providers.ProviderErrorNotFound {
		t.Fatalf("expected not_found, got %q", providerErr.Code)
	}
}

func TestProvisionUsesSelectedOfferAndEnv(t *testing.T) {
	t.Setenv("INFERA_WORKER_SHARED_TOKEN", "shared-token")
	t.Setenv("HF_TOKEN", "platform-global-sentinel")

	provider := newTestProvider(t)
	var captured map[string]any
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/offers":
			return httpResponse(http.StatusOK, `[{"id":"offer-1","gpu_type":"L40S","gpu_count":1,"cost_per_hour":0.7,"spot_price":0.4,"region":"global","available":1}]`), nil
		case "/instances":
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("ReadAll: %v", err)
			}
			if err := json.Unmarshal(body, &captured); err != nil {
				t.Fatalf("json.Unmarshal request: %v", err)
			}
			return httpResponse(http.StatusOK, `{"id":"inst-1","name":"worker","status":"running","gpu_type":"L40S","gpu_count":1,"cost_per_hour":0.7,"public_ip":"1.2.3.4","http_port":8081}`), nil
		default:
			t.Fatalf("unexpected path %s", req.URL.Path)
			return nil, nil
		}
	})

	instance, err := provider.Provision(context.Background(), &providers.ProvisionRequest{
		Name:           "worker",
		WorkspaceID:    "ws_123",
		GPUType:        providers.GPUL40S,
		GPUCount:       1,
		DockerImage:    "custom/worker:v1",
		Models:         []string{"meta-llama/Meta-Llama-3.1-8B-Instruct"},
		GatewayAddress: "https://inferai.co.in",
		WorkerToken:    "shared-token",
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	if instance.ProviderID != "inst-1" {
		t.Fatalf("expected provider id inst-1, got %q", instance.ProviderID)
	}
	if got := captured["offer_id"]; got != "offer-1" {
		t.Fatalf("expected selected offer-1, got %#v", got)
	}
	env, ok := captured["env"].(map[string]any)
	if !ok {
		t.Fatalf("expected env map, got %#v", captured["env"])
	}
	if env["INFERA_ROUTER_ADDRESS"] != "https://inferai.co.in" {
		t.Fatalf("expected router address in env, got %#v", env["INFERA_ROUTER_ADDRESS"])
	}
	if env["INFERA_WORKER_SHARED_TOKEN"] != "shared-token" {
		t.Fatalf("expected worker token in env, got %#v", env["INFERA_WORKER_SHARED_TOKEN"])
	}
	if env["INFERA_ALLOWED_MODELS"] != `["meta-llama/Meta-Llama-3.1-8B-Instruct"]` {
		t.Fatalf("expected approved models in env, got %#v", env["INFERA_ALLOWED_MODELS"])
	}
	if _, ok := env["HF_TOKEN"]; ok {
		t.Fatal("tenant payload included platform-global HF_TOKEN")
	}
	if _, ok := env["HUGGING_FACE_HUB_TOKEN"]; ok {
		t.Fatal("tenant payload included platform-global HUGGING_FACE_HUB_TOKEN")
	}
}

func TestFactoryUsesWorkspaceSecretForHuggingFaceToken(t *testing.T) {
	provider, err := Factory(providers.ProviderConfig{Type: providers.ProviderVastAI, APIKey: "key", APISecret: "workspace-hf-token"})
	if err != nil {
		t.Fatalf("Factory: %v", err)
	}
	if got := provider.(*Provider).hfToken; got != "workspace-hf-token" {
		t.Fatalf("expected workspace secret, got %q", got)
	}
	env := provider.(*Provider).buildEnv(&providers.ProvisionRequest{})
	if env["HF_TOKEN"] != "workspace-hf-token" || env["HUGGING_FACE_HUB_TOKEN"] != "workspace-hf-token" {
		t.Fatalf("expected workspace token compatibility aliases, got %#v", env)
	}
}

func TestProvisionAddsRuntimeEnvOverridesForKnownModel(t *testing.T) {
	provider := newTestProvider(t)
	var captured map[string]any
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/offers":
			return httpResponse(http.StatusOK, `[{"id":"offer-1","gpu_type":"L40S","gpu_count":1,"cost_per_hour":0.7,"spot_price":0.4,"region":"global","available":1}]`), nil
		case "/instances":
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("ReadAll: %v", err)
			}
			if err := json.Unmarshal(body, &captured); err != nil {
				t.Fatalf("json.Unmarshal request: %v", err)
			}
			return httpResponse(http.StatusOK, `{"id":"inst-1","name":"worker","status":"running","gpu_type":"L40S","gpu_count":1,"cost_per_hour":0.7}`), nil
		default:
			t.Fatalf("unexpected path %s", req.URL.Path)
			return nil, nil
		}
	})

	req := &providers.ProvisionRequest{
		Name:        "worker",
		GPUType:     providers.GPUL40S,
		GPUCount:    1,
		DockerImage: "custom/worker:v1",
		Models:      []string{"Qwen/Qwen3-4B-Thinking-2507"},
	}
	providers.ApplyRuntimeDefaults(req)

	if _, err := provider.Provision(context.Background(), req); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	env, ok := captured["env"].(map[string]any)
	if !ok {
		t.Fatalf("expected env map, got %#v", captured["env"])
	}
	if env[providers.OptionVLLMMaxModelLen] != "65536" {
		t.Fatalf("expected max model len override, got %#v", env[providers.OptionVLLMMaxModelLen])
	}
	if env[providers.OptionVLLMGPUMemoryUtilization] != "0.94" {
		t.Fatalf("expected gpu memory utilization override, got %#v", env[providers.OptionVLLMGPUMemoryUtilization])
	}
}

func TestVastAIProviderConformance(t *testing.T) {
	originalPollInterval := pollInterval
	originalReadyTimeout := readyTimeout
	pollInterval = 5 * time.Millisecond
	readyTimeout = 200 * time.Millisecond
	defer func() {
		pollInterval = originalPollInterval
		readyTimeout = originalReadyTimeout
	}()

	provider := newTestProvider(t)
	provider.httpClient.Transport = newFakeVastTransport()

	runProviderConformanceSuite(t, provider)
}

func runProviderConformanceSuite(t *testing.T, provider providers.Provider) {
	t.Helper()

	ctx := context.Background()
	req := &providers.ProvisionRequest{
		Name:        "vast-worker",
		GPUType:     providers.GPURTX4090,
		GPUCount:    1,
		DockerImage: "custom/worker:v1",
		Models:      []string{"meta-llama/Meta-Llama-3.1-8B-Instruct"},
	}

	instance, err := provider.Provision(ctx, req)
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}
	if instance.Provider != providers.ProviderVastAI {
		t.Fatalf("expected provider vastai, got %q", instance.Provider)
	}

	got, err := provider.GetInstance(ctx, instance.ProviderID)
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}
	if got.ProviderID != instance.ProviderID {
		t.Fatalf("expected provider id %q, got %q", instance.ProviderID, got.ProviderID)
	}

	listed, err := provider.ListInstances(ctx)
	if err != nil {
		t.Fatalf("ListInstances failed: %v", err)
	}
	if len(listed) == 0 {
		t.Fatal("expected at least one instance")
	}

	offerings, err := provider.ListOfferings(ctx)
	if err != nil {
		t.Fatalf("ListOfferings failed: %v", err)
	}
	if len(offerings) == 0 {
		t.Fatal("expected offerings")
	}

	status, err := provider.GetStatus(ctx)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if !status.Connected {
		t.Fatal("expected connected status")
	}

	if err := provider.WaitForReady(ctx, instance.ProviderID); err != nil {
		t.Fatalf("WaitForReady failed: %v", err)
	}

	if err := provider.Stop(ctx, instance.ProviderID); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	stopped, err := provider.GetInstance(ctx, instance.ProviderID)
	if err != nil {
		t.Fatalf("GetInstance after Stop failed: %v", err)
	}
	if stopped.Status != providers.InstanceStatusStopped {
		t.Fatalf("expected stopped, got %q", stopped.Status)
	}

	if err := provider.Start(ctx, instance.ProviderID); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	restarted, err := provider.GetInstance(ctx, instance.ProviderID)
	if err != nil {
		t.Fatalf("GetInstance after Start failed: %v", err)
	}
	if restarted.Status != providers.InstanceStatusRunning {
		t.Fatalf("expected running after start, got %q", restarted.Status)
	}

	if err := provider.Terminate(ctx, instance.ProviderID); err != nil {
		t.Fatalf("Terminate failed: %v", err)
	}
	terminated, err := provider.GetInstance(ctx, instance.ProviderID)
	if err != nil {
		t.Fatalf("GetInstance after Terminate failed: %v", err)
	}
	if terminated.Status != providers.InstanceStatusTerminated {
		t.Fatalf("expected terminated, got %q", terminated.Status)
	}

	_, err = provider.GetInstance(ctx, "missing-instance")
	if err == nil {
		t.Fatal("expected not_found error")
	}
	var providerErr *providers.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Code != providers.ProviderErrorNotFound {
		t.Fatalf("expected not_found, got %q", providerErr.Code)
	}
}

func newTestProvider(t *testing.T) *Provider {
	t.Helper()
	provider, err := New(Config{
		APIKey:   "vast-test-key",
		Endpoint: "https://vast.example.test",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return provider
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func httpResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type fakeVastTransport struct {
	mu        sync.Mutex
	instances map[string]*vastInstance
}

func newFakeVastTransport() *fakeVastTransport {
	return &fakeVastTransport{
		instances: map[string]*vastInstance{},
	}
}

func (f *fakeVastTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch {
	case req.Method == http.MethodGet && req.URL.Path == "/offers":
		return httpResponse(http.StatusOK, `[{"id":"offer-4090","gpu_type":"RTX_4090","gpu_count":1,"vcpu":8,"memory_gb":32,"storage_gb":80,"cost_per_hour":0.32,"spot_price":0.21,"region":"global","available":1}]`), nil
	case req.Method == http.MethodPost && req.URL.Path == "/instances":
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, err
		}
		instance := &vastInstance{
			ID:          "vast-inst-1",
			Name:        payload["name"].(string),
			Status:      "running",
			GPUType:     providers.GPURTX4090,
			GPUCount:    1,
			VCPU:        8,
			MemoryGB:    32,
			StorageGB:   80,
			CostPerHour: 0.32,
			PublicIP:    "5.6.7.8",
			HTTPPort:    8081,
			SSHPort:     22,
			Region:      "global",
			CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		}
		f.instances[instance.ID] = instance
		resp, _ := json.Marshal(instance)
		return httpResponse(http.StatusOK, string(resp)), nil
	case req.Method == http.MethodGet && req.URL.Path == "/instances":
		out := make([]*vastInstance, 0, len(f.instances))
		for _, instance := range f.instances {
			out = append(out, cloneInstance(instance))
		}
		resp, _ := json.Marshal(out)
		return httpResponse(http.StatusOK, string(resp)), nil
	case req.Method == http.MethodGet && strings.HasPrefix(req.URL.Path, "/instances/"):
		id := strings.TrimPrefix(req.URL.Path, "/instances/")
		id = strings.TrimSuffix(id, "/")
		instance, ok := f.instances[id]
		if !ok {
			return httpResponse(http.StatusNotFound, `missing`), nil
		}
		resp, _ := json.Marshal(instance)
		return httpResponse(http.StatusOK, string(resp)), nil
	case req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/stop"):
		id := strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/instances/"), "/stop")
		instance, ok := f.instances[id]
		if !ok {
			return httpResponse(http.StatusNotFound, `missing`), nil
		}
		instance.Status = "stopped"
		return httpResponse(http.StatusOK, `{}`), nil
	case req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/start"):
		id := strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/instances/"), "/start")
		instance, ok := f.instances[id]
		if !ok {
			return httpResponse(http.StatusNotFound, `missing`), nil
		}
		instance.Status = "running"
		return httpResponse(http.StatusOK, `{}`), nil
	case req.Method == http.MethodDelete && strings.HasPrefix(req.URL.Path, "/instances/"):
		id := strings.TrimPrefix(req.URL.Path, "/instances/")
		id = strings.TrimSuffix(id, "/")
		instance, ok := f.instances[id]
		if !ok {
			return httpResponse(http.StatusNotFound, `missing`), nil
		}
		instance.Status = "terminated"
		return httpResponse(http.StatusOK, `{}`), nil
	default:
		return httpResponse(http.StatusNotFound, `unhandled`), nil
	}
}

func cloneInstance(instance *vastInstance) *vastInstance {
	cloned := *instance
	if instance.Env != nil {
		cloned.Env = make(map[string]string, len(instance.Env))
		for key, value := range instance.Env {
			cloned.Env[key] = value
		}
	}
	return &cloned
}
