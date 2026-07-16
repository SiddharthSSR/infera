package runpod

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/infera/infera/go/internal/providers"
)

func TestNewRequiresAPIKey(t *testing.T) {
	_, err := New(Config{})
	if err == nil {
		t.Fatal("expected missing API key error")
	}

	var providerErr *providers.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Code != "missing_api_key" {
		t.Fatalf("expected missing_api_key, got %q", providerErr.Code)
	}
}

func TestNewRejectsUnsafeEndpoints(t *testing.T) {
	for _, endpoint := range []string{"http://api.example.com/graphql", "https://127.0.0.1/graphql", "https://169.254.169.254/latest"} {
		if _, err := New(Config{APIKey: "key", Endpoint: endpoint}); err == nil {
			t.Fatalf("expected endpoint %q to be rejected", endpoint)
		}
	}
}

func TestGraphQLPreservesPublicCustomEndpoint(t *testing.T) {
	provider, err := New(Config{APIKey: "key", Endpoint: "https://provider.example.com/custom/graphql?region=us"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var gotURL, gotMethod string
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotURL, gotMethod = req.URL.String(), req.Method
		return httpResponse(http.StatusOK, `{"data":{}}`), nil
	})
	if _, err := provider.graphQL(context.Background(), "query { myself { id } }", nil); err != nil {
		t.Fatalf("graphQL: %v", err)
	}
	if gotMethod != http.MethodPost || gotURL != "https://provider.example.com/custom/graphql?region=us" {
		t.Fatalf("custom endpoint behavior changed: %s %s", gotMethod, gotURL)
	}
}

func TestGraphQLMapsRateLimitToRetryableProviderError(t *testing.T) {
	provider, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return httpResponse(http.StatusTooManyRequests, `{"errors":[{"message":"too many requests"}]}`), nil
	})

	_, err = provider.graphQL(context.Background(), "query { myself { id } }", nil)
	if err == nil {
		t.Fatal("expected rate_limited error")
	}

	var providerErr *providers.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Code != "rate_limited" {
		t.Fatalf("expected rate_limited, got %q", providerErr.Code)
	}
	if !providerErr.IsRetryable() {
		t.Fatal("rate_limited error should be retryable")
	}
}

func TestGraphQLMapsUnauthorizedToAuthFailed(t *testing.T) {
	provider, err := New(Config{APIKey: "invalid-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return httpResponse(http.StatusUnauthorized, `{"error":"sensitive upstream response"}`), nil
	})

	_, err = provider.graphQL(context.Background(), "query { myself { id } }", nil)
	if err == nil {
		t.Fatal("expected auth_failed error")
	}

	var providerErr *providers.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Code != providers.ProviderErrorAuthFailed {
		t.Fatalf("expected auth_failed, got %q", providerErr.Code)
	}
	if strings.Contains(providerErr.Message, "sensitive upstream response") {
		t.Fatalf("upstream response leaked through provider error: %q", providerErr.Message)
	}
}

func TestGraphQLRejectsOversizedBodiesBeforeStatusOrJSON(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			provider, err := New(Config{APIKey: "test-key"})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			body := strings.Repeat("x", int(providers.MaxProviderResponseBytes)+1)
			provider.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return httpResponse(status, body), nil
			})
			_, err = provider.graphQL(context.Background(), "query { myself { id } }", nil)
			var providerErr *providers.ProviderError
			if !errors.As(err, &providerErr) || providerErr.Code != providers.ProviderErrorResponseTooLarge {
				t.Fatalf("expected response_too_large, got %v", err)
			}
		})
	}
}

func TestGetStatusPreservesProviderErrorCode(t *testing.T) {
	provider, err := New(Config{APIKey: "invalid-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return httpResponse(http.StatusUnauthorized, `{}`), nil
	})

	status, err := provider.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if status.Connected {
		t.Fatal("expected disconnected status")
	}
	if status.ErrorCode != providers.ProviderErrorAuthFailed {
		t.Fatalf("expected auth_failed status, got %q", status.ErrorCode)
	}
}

func TestGetInstanceReturnsNotFoundWhenPodMissing(t *testing.T) {
	provider, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return httpResponse(http.StatusOK, `{"data":{"pod":null}}`), nil
	})

	_, err = provider.GetInstance(context.Background(), "pod-123")
	if err == nil {
		t.Fatal("expected not_found error")
	}

	var providerErr *providers.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Code != "not_found" {
		t.Fatalf("expected not_found, got %q", providerErr.Code)
	}
	if providerErr.Provider != providers.ProviderRunPod {
		t.Fatalf("expected provider runpod, got %q", providerErr.Provider)
	}
}

func TestProvisionUsesProvidedDockerImage(t *testing.T) {
	t.Setenv("INFERA_WORKER_SHARED_TOKEN", "worker-shared-token")
	t.Setenv("INFERA_GATEWAY_ADDRESS", "https://inferai.co.in")
	t.Setenv("HF_TOKEN", "platform-global-sentinel")

	provider, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var captured graphQLRequest
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("json.Unmarshal request: %v", err)
		}
		return httpResponse(http.StatusOK, `{"data":{"podFindAndDeployOnDemand":{"id":"pod-123","name":"worker","desiredStatus":"RUNNING","imageName":"custom/worker:v1","machineId":"machine-1","machine":{"gpuDisplayName":"NVIDIA L40S"}}}}`), nil
	})

	instance, err := provider.Provision(context.Background(), &providers.ProvisionRequest{
		Name:           "worker",
		GPUType:        providers.GPUL40S,
		GPUCount:       1,
		DockerImage:    "custom/worker:v1",
		Models:         []string{"meta-llama/Meta-Llama-3.1-8B-Instruct"},
		GatewayAddress: "https://gateway.example.com",
		WorkerToken:    "worker-shared-token",
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	if instance.ProviderID != "pod-123" {
		t.Fatalf("expected provider id pod-123, got %q", instance.ProviderID)
	}

	input, ok := captured.Variables["input"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected graphql input variables, got %#v", captured.Variables)
	}
	if got := input["imageName"]; got != "custom/worker:v1" {
		t.Fatalf("expected custom image, got %#v", got)
	}
	if got := input["volumeMountPath"]; got != "/workspace" {
		t.Fatalf("expected persistent workspace mount, got %#v", got)
	}
	if got := input["volumeInGb"]; got != float64(70) {
		t.Fatalf("expected volume size 70GB, got %#v", got)
	}

	env, ok := input["env"].([]interface{})
	if !ok {
		t.Fatalf("expected env array, got %#v", input["env"])
	}
	assertEnvContains(t, env, "INFERA_ROUTER_ADDRESS", "https://gateway.example.com")
	assertEnvContains(t, env, "INFERA_WORKER_SHARED_TOKEN", "worker-shared-token")
	assertEnvContains(t, env, "INFERA_ALLOWED_MODELS", `["meta-llama/Meta-Llama-3.1-8B-Instruct"]`)
	assertEnvContains(t, env, "XDG_CACHE_HOME", "/workspace/.cache")
	assertEnvContains(t, env, "HF_HOME", "/workspace/.cache/huggingface")
	assertEnvContains(t, env, "HUGGINGFACE_HUB_CACHE", "/workspace/.cache/huggingface/hub")
	assertEnvMissing(t, env, "HF_TOKEN")
	assertEnvMissing(t, env, "HUGGING_FACE_HUB_TOKEN")
}

func TestFactoryUsesWorkspaceSecretForHuggingFaceToken(t *testing.T) {
	provider, err := Factory(providers.ProviderConfig{Type: providers.ProviderRunPod, APIKey: "key", APISecret: "workspace-hf-token"})
	if err != nil {
		t.Fatalf("Factory: %v", err)
	}
	if got := provider.(*Provider).hfToken; got != "workspace-hf-token" {
		t.Fatalf("expected workspace secret, got %q", got)
	}
	env := appendHuggingFaceEnv(nil, provider.(*Provider).hfToken)
	assertStringEnvContains(t, env, "HF_TOKEN", "workspace-hf-token")
	assertStringEnvContains(t, env, "HUGGING_FACE_HUB_TOKEN", "workspace-hf-token")
}

func assertStringEnvContains(t *testing.T, env []map[string]string, key, want string) {
	t.Helper()
	for _, entry := range env {
		if entry["key"] == key && entry["value"] == want {
			return
		}
	}
	t.Fatalf("expected env to contain %s=%q", key, want)
}

func TestProvisionIncludesAllowedCudaVersions(t *testing.T) {
	provider, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var captured graphQLRequest
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("json.Unmarshal request: %v", err)
		}
		return httpResponse(http.StatusOK, `{"data":{"podFindAndDeployOnDemand":{"id":"pod-123","name":"worker","desiredStatus":"RUNNING","imageName":"custom/worker:v1","machineId":"machine-1","machine":{"gpuDisplayName":"NVIDIA A100 80GB PCIe"}}}}`), nil
	})

	instance, err := provider.Provision(context.Background(), &providers.ProvisionRequest{
		Name:                "worker",
		GPUType:             providers.GPUA100_80,
		GPUCount:            1,
		DockerImage:         "custom/worker:v1",
		AllowedCudaVersions: []string{"12.6", "12.7", "12.7", "12.8"},
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	input, ok := captured.Variables["input"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected graphql input variables, got %#v", captured.Variables)
	}
	allowed, ok := input["allowedCudaVersions"].([]interface{})
	if !ok {
		t.Fatalf("expected allowedCudaVersions array, got %#v", input["allowedCudaVersions"])
	}
	if len(allowed) != 3 || allowed[0] != "12.6" || allowed[1] != "12.7" || allowed[2] != "12.8" {
		t.Fatalf("unexpected allowedCudaVersions payload: %#v", allowed)
	}
	if got := instance.Metadata[metadataAllowedCudaVersions]; got != "12.6,12.7,12.8" {
		t.Fatalf("expected metadata to persist CUDA versions, got %q", got)
	}
}

func TestProvisionAddsRuntimeEnvOverridesForKnownModel(t *testing.T) {
	t.Setenv("INFERA_WORKER_SHARED_TOKEN", "worker-shared-token")

	provider, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var captured graphQLRequest
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("json.Unmarshal request: %v", err)
		}
		return httpResponse(http.StatusOK, `{"data":{"podFindAndDeployOnDemand":{"id":"pod-123","name":"worker","desiredStatus":"RUNNING","imageName":"custom/worker:v1","machineId":"machine-1","machine":{"gpuDisplayName":"NVIDIA L40S"}}}}`), nil
	})

	req := &providers.ProvisionRequest{
		Name:           "worker",
		GPUType:        providers.GPUL40S,
		GPUCount:       1,
		DockerImage:    "custom/worker:v1",
		Models:         []string{"Qwen/Qwen2.5-7B-Instruct"},
		GatewayAddress: "https://gateway.example.com",
	}
	providers.ApplyRuntimeDefaults(req)

	if _, err := provider.Provision(context.Background(), req); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	input, ok := captured.Variables["input"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected graphql input variables, got %#v", captured.Variables)
	}
	env, ok := input["env"].([]interface{})
	if !ok {
		t.Fatalf("expected env array, got %#v", input["env"])
	}
	assertEnvContains(t, env, providers.OptionVLLMMaxModelLen, "32768")
	assertEnvContains(t, env, providers.OptionVLLMGPUMemoryUtilization, "0.94")
}

func TestProvisionRejectsFloatingWorkerImage(t *testing.T) {
	provider, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = provider.Provision(context.Background(), &providers.ProvisionRequest{
		Name:        "worker",
		GPUType:     providers.GPUL40S,
		GPUCount:    1,
		DockerImage: "codingtensor/infera-worker:latest",
	})
	if err == nil {
		t.Fatal("expected floating worker image to be rejected")
	}
}

func TestListOfferingsUsesLiveRunPodValues(t *testing.T) {
	provider, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return httpResponse(http.StatusOK, `{"data":{"gpuTypes":[
			{"id":"gpu-4090","displayName":"NVIDIA GeForce RTX 4090","memoryInGb":24,"communityPrice":0.41,"securePrice":0.52,"communitySpotPrice":0.22,"secureSpotPrice":0.31,"maxGpuCountCommunityCloud":14,"maxGpuCountSecureCloud":0,"lowestPrice2":{"minimumBidPrice":0.40,"uninterruptablePrice":0.80},"lowestPrice4":{"minimumBidPrice":0.78,"uninterruptablePrice":1.56},"lowestPrice8":{"minimumBidPrice":1.52,"uninterruptablePrice":3.04}},
			{"id":"gpu-unknown","displayName":"NVIDIA Mystery GPU","memoryInGb":48,"communityPrice":0.99,"securePrice":1.10,"communitySpotPrice":0.45,"secureSpotPrice":0.55,"maxGpuCountCommunityCloud":8,"maxGpuCountSecureCloud":0},
			{"id":"gpu-h100","displayName":"NVIDIA H100 PCIe","memoryInGb":80,"communityPrice":1.99,"securePrice":2.20,"communitySpotPrice":1.09,"secureSpotPrice":1.30,"maxGpuCountCommunityCloud":0,"maxGpuCountSecureCloud":0}
		]}}`), nil
	})

	offerings, err := provider.ListOfferings(context.Background())
	if err != nil {
		t.Fatalf("ListOfferings: %v", err)
	}

	if len(offerings) != 9 {
		t.Fatalf("expected 9 live offerings, got %d", len(offerings))
	}

	offering := offerings[0]
	if offering.GPUType != providers.GPURTX4090 {
		t.Fatalf("expected RTX_4090, got %s", offering.GPUType)
	}
	if offering.DisplayName != "RTX 4090" {
		t.Fatalf("expected compact display name RTX 4090, got %q", offering.DisplayName)
	}
	if offering.ProviderGPUTypeID != "gpu-4090" {
		t.Fatalf("expected provider gpu id gpu-4090, got %q", offering.ProviderGPUTypeID)
	}
	if offering.CostPerHour != 0.41 {
		t.Fatalf("expected live cost 0.41, got %f", offering.CostPerHour)
	}
	if offering.SpotPrice != 0.22 {
		t.Fatalf("expected live spot price 0.22, got %f", offering.SpotPrice)
	}
	if offering.Available != 14 {
		t.Fatalf("expected live availability 14, got %d", offering.Available)
	}

	if offerings[1].GPUCount != 2 || offerings[1].CostPerHour != 0.80 {
		t.Fatalf("expected 2x RTX_4090 live offering with aliased pricing, got count=%d price=%f", offerings[1].GPUCount, offerings[1].CostPerHour)
	}
	if offerings[4].GPUCount != 14 {
		t.Fatalf("expected exact max count offering to be surfaced, got %d", offerings[4].GPUCount)
	}
	if math.Abs(offerings[4].CostPerHour-5.74) > 0.0001 {
		t.Fatalf("expected fallback scaled price for 14x offering, got %f", offerings[4].CostPerHour)
	}
	if offerings[5].GPUType != providers.GPUType("NVIDIA Mystery GPU") {
		t.Fatalf("expected unknown gpu to be preserved, got %q", offerings[5].GPUType)
	}
	if offerings[8].GPUCount != 8 {
		t.Fatalf("expected 8x unknown gpu offering to be surfaced, got %d", offerings[8].GPUCount)
	}
}

func TestListOfferingsReturnsErrorWhenLiveQueryFails(t *testing.T) {
	provider, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return httpResponse(http.StatusInternalServerError, `{"errors":[{"message":"boom"}]}`), nil
	})

	_, err = provider.ListOfferings(context.Background())
	if err == nil {
		t.Fatal("expected ListOfferings to return live query error")
	}

	var providerErr *providers.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Provider != providers.ProviderRunPod {
		t.Fatalf("expected runpod provider error, got %q", providerErr.Provider)
	}
}

func TestProvisionRejectsUnsupportedGPUType(t *testing.T) {
	provider, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = provider.Provision(context.Background(), &providers.ProvisionRequest{
		Name:    "worker",
		GPUType: providers.GPUType(""),
	})
	if err == nil {
		t.Fatal("expected unsupported GPU type error")
	}

	var providerErr *providers.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Code != "invalid_gpu_type" {
		t.Fatalf("expected invalid_gpu_type, got %q", providerErr.Code)
	}
}

func TestProvisionPassesThroughUnknownLiveGPUDisplayName(t *testing.T) {
	provider, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var captured graphQLRequest
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("json.Unmarshal request: %v", err)
		}
		return httpResponse(http.StatusOK, `{"data":{"podFindAndDeployOnDemand":{"id":"pod-h200","name":"worker","desiredStatus":"RUNNING","imageName":"custom/worker:v1","machineId":"machine-1","machine":{"gpuDisplayName":"NVIDIA H200 SXM"}}}}`), nil
	})

	_, err = provider.Provision(context.Background(), &providers.ProvisionRequest{
		Name:        "worker",
		GPUType:     providers.GPUType("H200 SXM"),
		GPUCount:    1,
		DockerImage: "custom/worker:v1",
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	input, ok := captured.Variables["input"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected graphql input variables, got %#v", captured.Variables)
	}
	if got := input["gpuTypeId"]; got != "H200 SXM" {
		t.Fatalf("expected raw gpuTypeId passthrough, got %#v", got)
	}
}

func TestStartWithInstanceOmitsAllowedCudaVersionsOnResume(t *testing.T) {
	provider, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var captured graphQLRequest
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("json.Unmarshal request: %v", err)
		}
		return httpResponse(http.StatusOK, `{"data":{"podResume":{"id":"pod-123","desiredStatus":"RUNNING"}}}`), nil
	})

	err = provider.StartWithInstance(context.Background(), &providers.Instance{
		ID:         "inst-1",
		ProviderID: "pod-123",
		GPUCount:   1,
		Metadata: map[string]string{
			metadataAllowedCudaVersions: "12.6,12.7,12.8",
		},
	})
	if err != nil {
		t.Fatalf("StartWithInstance: %v", err)
	}

	input, ok := captured.Variables["input"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected graphql input variables, got %#v", captured.Variables)
	}
	if _, exists := input["allowedCudaVersions"]; exists {
		t.Fatalf("expected resume payload to omit allowedCudaVersions, got %#v", input["allowedCudaVersions"])
	}
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

func assertEnvContains(t *testing.T, env []interface{}, key, want string) {
	t.Helper()
	for _, raw := range env {
		entry, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if entry["key"] == key {
			if entry["value"] != want {
				t.Fatalf("expected %s=%q, got %#v", key, want, entry["value"])
			}
			return
		}
	}
	t.Fatalf("expected env to contain %s", key)
}

func assertEnvMissing(t *testing.T, env []interface{}, key string) {
	t.Helper()
	for _, raw := range env {
		entry, ok := raw.(map[string]interface{})
		if ok && entry["key"] == key {
			t.Fatalf("expected env to omit %s", key)
		}
	}
}
