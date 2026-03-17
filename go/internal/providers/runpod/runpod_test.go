package runpod

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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
	assertEnvContains(t, env, "XDG_CACHE_HOME", "/workspace/.cache")
	assertEnvContains(t, env, "HF_HOME", "/workspace/.cache/huggingface")
	assertEnvContains(t, env, "HUGGINGFACE_HUB_CACHE", "/workspace/.cache/huggingface/hub")
}

func TestListOfferingsUsesLiveRunPodValues(t *testing.T) {
	provider, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return httpResponse(http.StatusOK, `{"data":{"gpuTypes":[
			{"id":"gpu-4090","displayName":"NVIDIA GeForce RTX 4090","memoryInGb":24,"communityPrice":0.41,"securePrice":0.52,"communitySpotPrice":0.22,"secureSpotPrice":0.31,"maxGpuCountCommunityCloud":14,"maxGpuCountSecureCloud":0},
			{"id":"gpu-unknown","displayName":"NVIDIA Mystery GPU","memoryInGb":48,"communityPrice":0.99,"securePrice":1.10,"communitySpotPrice":0.45,"secureSpotPrice":0.55,"maxGpuCountCommunityCloud":8,"maxGpuCountSecureCloud":0},
			{"id":"gpu-h100","displayName":"NVIDIA H100 PCIe","memoryInGb":80,"communityPrice":1.99,"securePrice":2.20,"communitySpotPrice":1.09,"secureSpotPrice":1.30,"maxGpuCountCommunityCloud":0,"maxGpuCountSecureCloud":0}
		]}}`), nil
	})

	offerings, err := provider.ListOfferings(context.Background())
	if err != nil {
		t.Fatalf("ListOfferings: %v", err)
	}

	if len(offerings) != 1 {
		t.Fatalf("expected 1 supported live offering, got %d", len(offerings))
	}

	offering := offerings[0]
	if offering.GPUType != providers.GPURTX4090 {
		t.Fatalf("expected RTX_4090, got %s", offering.GPUType)
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
		GPUType: providers.GPUType("UNKNOWN_GPU"),
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
