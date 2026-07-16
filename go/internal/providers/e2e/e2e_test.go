package e2e

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

func TestNewRequiresAuthInputsAndOptions(t *testing.T) {
	_, err := New(Config{})
	if err == nil {
		t.Fatal("expected missing api key error")
	}

	_, err = New(Config{
		APIKey:    "key",
		AuthToken: "token",
		Options: map[string]string{
			optionTeamID:    "team",
			optionProjectID: "project",
		},
	})
	if err == nil {
		t.Fatal("expected invalid config error for missing active_iam")
	}

	provider, err := New(Config{
		APIKey:    "key",
		AuthToken: "token",
		Options: map[string]string{
			optionActiveIAM: "iam",
			optionTeamID:    "team",
			optionProjectID: "project",
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if provider.endpoint != defaultEndpoint {
		t.Fatalf("expected default endpoint %q, got %q", defaultEndpoint, provider.endpoint)
	}
}

func TestNewRejectsUnsafeEndpoints(t *testing.T) {
	options := map[string]string{optionActiveIAM: "iam", optionTeamID: "team", optionProjectID: "project"}
	for _, endpoint := range []string{"http://api.example.com", "https://127.0.0.1", "https://169.254.169.254/latest"} {
		if _, err := New(Config{APIKey: "key", AuthToken: "token", Options: options, Endpoint: endpoint}); err == nil {
			t.Fatalf("expected endpoint %q to be rejected", endpoint)
		}
	}
}

func TestDoJSONRejectsOversizedResponse(t *testing.T) {
	provider := newTestProvider(t)
	provider.httpClient.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return httpResponse(http.StatusInternalServerError, strings.Repeat("x", int(providers.MaxProviderResponseBytes)+1)), nil
	})
	err := provider.doJSON(context.Background(), http.MethodGet, "/notebooks", nil, nil, nil)
	var providerErr *providers.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != providers.ProviderErrorResponseTooLarge {
		t.Fatalf("expected response_too_large, got %v", err)
	}
}

func TestListOfferingsDecodesPlans(t *testing.T) {
	provider := newTestProvider(t)
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !strings.Contains(req.URL.Path, "/plans/") {
			t.Fatalf("expected plans path, got %s", req.URL.Path)
		}
		return httpResponse(http.StatusOK, `{"data":{"items":[{"id":"plan-h100","name":"H100 x2","gpu_name":"H100","gpu_count":2,"vcpu":32,"memory_gb":180,"storage_gb":200,"price_per_hour":4.8,"location":"Delhi","available":3}]}}`), nil
	})

	offerings, err := provider.ListOfferings(context.Background())
	if err != nil {
		t.Fatalf("ListOfferings: %v", err)
	}
	if len(offerings) != 1 {
		t.Fatalf("expected 1 offering, got %d", len(offerings))
	}
	if offerings[0].Provider != providers.ProviderE2E {
		t.Fatalf("expected provider e2e, got %s", offerings[0].Provider)
	}
	if offerings[0].GPUType != providers.GPUH100 {
		t.Fatalf("expected H100 mapping, got %s", offerings[0].GPUType)
	}
	if offerings[0].ProviderGPUTypeID != "plan-h100" {
		t.Fatalf("expected provider gpu type id, got %q", offerings[0].ProviderGPUTypeID)
	}
}

func TestProvisionUsesPublicImageAndRuntimeEnv(t *testing.T) {
	t.Setenv("INFERA_WORKER_SHARED_TOKEN", "unused")

	provider := newTestProvider(t)
	var captured map[string]any
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}
		return httpResponse(http.StatusOK, `{"data":{"id":"nb-1","name":"worker","status":"running","gpu_name":"L40S","gpu_count":1,"vcpu":16,"memory_gb":64,"storage_gb":120,"public_url":"https://gpu-worker.example.com:8443","price_per_hour":1.25,"location":"Delhi"}}`), nil
	})

	instance, err := provider.Provision(context.Background(), &providers.ProvisionRequest{
		Name:              "worker",
		ProviderGPUTypeID: "plan-l40s",
		GPUType:           providers.GPUL40S,
		GPUCount:          1,
		DockerImage:       "ghcr.io/codingtensor/infera-worker-e2e:v1",
		GatewayAddress:    "https://gateway.example.com",
		Models:            []string{"Qwen/Qwen2.5-7B-Instruct"},
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if instance.ProviderID != "nb-1" {
		t.Fatalf("expected provider id nb-1, got %q", instance.ProviderID)
	}
	if got := captured["image_type"]; got != "public" {
		t.Fatalf("expected public image type, got %#v", got)
	}
	if got := captured["image_url"]; got != "ghcr.io/codingtensor/infera-worker-e2e:v1" {
		t.Fatalf("expected image url to round-trip, got %#v", got)
	}
	env, ok := captured["env"].(map[string]any)
	if !ok {
		t.Fatalf("expected env object, got %#v", captured["env"])
	}
	if env["INFERA_ROUTER_ADDRESS"] != "https://gateway.example.com" {
		t.Fatalf("expected gateway address env, got %#v", env["INFERA_ROUTER_ADDRESS"])
	}
	if env["INFERA_HTTP_PORT"] != "8081" {
		t.Fatalf("expected http port env, got %#v", env["INFERA_HTTP_PORT"])
	}
}

func TestGetStatusMapsAuthFailureToDisconnectedState(t *testing.T) {
	provider := newTestProvider(t)
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return httpResponse(http.StatusUnauthorized, `{"message":"invalid token"}`), nil
	})

	status, err := provider.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if status.Connected {
		t.Fatal("expected disconnected status")
	}
	if status.ErrorCode != providers.ProviderErrorAuthFailed {
		t.Fatalf("expected auth_failed, got %q", status.ErrorCode)
	}
}

func TestGetInstanceReturnsNotFound(t *testing.T) {
	provider := newTestProvider(t)
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return httpResponse(http.StatusNotFound, `{"message":"not found"}`), nil
	})

	_, err := provider.GetInstance(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected not found error")
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
		APIKey:    "key",
		AuthToken: "token",
		Options: map[string]string{
			optionActiveIAM: "iam-1",
			optionTeamID:    "team-1",
			optionProjectID: "proj-1",
			optionLocation:  "Delhi",
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return provider
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func httpResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body: io.NopCloser(strings.NewReader(body)),
	}
}
