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
	"time"

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

func TestGraphQLDoesNotClassifyCapacityFromUnstructuredMessage(t *testing.T) {
	provider, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return httpResponse(http.StatusOK, `{"errors":[{"message":"There is no GPU capacity available"}]}`), nil
	})

	_, err = provider.graphQL(context.Background(), "mutation { podFindAndDeployOnDemand { id } }", nil)
	var providerErr *providers.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Code != providers.ProviderErrorGraphQLError {
		t.Fatalf("expected unstructured error to remain %q, got %q", providers.ProviderErrorGraphQLError, providerErr.Code)
	}
	if providerErr.IsRetryable() {
		t.Fatal("unstructured GraphQL error must not enable capacity fallback")
	}
}

func TestGraphQLMapsKnownMachineResourceExhaustionToCapacityUnavailable(t *testing.T) {
	provider, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return httpResponse(http.StatusOK, `{"errors":[{"message":"This machine does not have the resources to deploy your pod. Please try a different machine"}]}`), nil
	})

	_, err = provider.graphQL(context.Background(), "mutation { podFindAndDeployOnDemand { id } }", nil)
	var providerErr *providers.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Code != providers.ProviderErrorCapacityUnavailable {
		t.Fatalf("expected capacity_unavailable, got %q", providerErr.Code)
	}
	if !providerErr.IsRetryable() {
		t.Fatal("known RunPod resource exhaustion must enable bounded capacity fallback")
	}
	if providerErr.HTTPStatus(http.StatusInternalServerError) != http.StatusServiceUnavailable {
		t.Fatalf("expected HTTP 503, got %d", providerErr.HTTPStatus(http.StatusInternalServerError))
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

func TestGetStatusCountsOnlyRunningPods(t *testing.T) {
	provider, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		if strings.Contains(string(body), "query GetPods") {
			return httpResponse(http.StatusOK, `{"data":{"myself":{"pods":[{"id":"running","desiredStatus":"RUNNING"},{"id":"stopped","desiredStatus":"EXITED"}]}}}`), nil
		}
		return httpResponse(http.StatusOK, `{"data":{"myself":{"id":"account-1","currentSpendPerHr":1.5,"machineQuota":4}}}`), nil
	})

	status, err := provider.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if !status.Connected || status.ActiveCount != 1 {
		t.Fatalf("expected one active pod, got %+v", status)
	}
}

func TestGetStatusFailsClosedWhenPodInventoryIsUnavailable(t *testing.T) {
	provider, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	requestCount := 0
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestCount++
		if requestCount == 1 {
			return httpResponse(http.StatusOK, `{"data":{"myself":{"id":"account-1","currentSpendPerHr":1.5,"machineQuota":4}}}`), nil
		}
		return httpResponse(http.StatusTooManyRequests, `{"errors":[{"message":"too many requests"}]}`), nil
	})

	status, err := provider.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if status.Connected || status.ErrorCode != providers.ProviderErrorRateLimited {
		t.Fatalf("expected unavailable inventory to fail closed, got %+v", status)
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

func TestProvisionReturnsCapacityUnavailableWithoutCreateMutation(t *testing.T) {
	provider, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	createCalls := 0
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		request := decodeGraphQLRequest(t, req)
		if strings.Contains(request.Query, "query GpuPlacementEvidence") {
			return httpResponse(http.StatusOK, `{"data":{"gpuTypes":[{"id":"NVIDIA L40S","displayName":"NVIDIA L40S","maxGpuCountCommunityCloud":0,"maxGpuCountSecureCloud":0}]}}`), nil
		}
		createCalls++
		return httpResponse(http.StatusOK, `{"data":{}}`), nil
	})

	_, err = provider.Provision(context.Background(), &providers.ProvisionRequest{
		Name:        "worker",
		GPUType:     providers.GPUL40S,
		GPUCount:    1,
		DockerImage: "custom/worker:v1",
	})
	var providerErr *providers.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Code != providers.ProviderErrorCapacityUnavailable {
		t.Fatalf("expected capacity_unavailable, got %q", providerErr.Code)
	}
	if createCalls != 0 {
		t.Fatalf("expected no create mutation, got %d", createCalls)
	}
}

func TestProvisionCreatesWhenStructuredCapacityIsPositive(t *testing.T) {
	provider, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	capacityCalls := 0
	createCalls := 0
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		request := decodeGraphQLRequest(t, req)
		if strings.Contains(request.Query, "query GpuPlacementEvidence") {
			capacityCalls++
			return httpResponse(http.StatusOK, `{"data":{"gpuTypes":[{"id":"NVIDIA L40S","displayName":"NVIDIA L40S","communityPrice":0.79,"maxGpuCountCommunityCloud":0,"maxGpuCountSecureCloud":1}]}}`), nil
		}
		createCalls++
		return httpResponse(http.StatusOK, `{"data":{"podFindAndDeployOnDemand":{"id":"pod-123","name":"worker","desiredStatus":"RUNNING","imageName":"custom/worker:v1","machineId":"machine-1","machine":{"gpuDisplayName":"NVIDIA L40S","costPerHr":0.79}}}}`), nil
	})

	if _, err := provider.Provision(context.Background(), &providers.ProvisionRequest{
		Name:        "worker",
		GPUType:     providers.GPUL40S,
		GPUCount:    1,
		DockerImage: "custom/worker:v1",
	}); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if capacityCalls != 1 || createCalls != 1 {
		t.Fatalf("expected one capacity query and one create mutation, got capacity=%d create=%d", capacityCalls, createCalls)
	}
}

func TestProvisionPreservesStructuredCapacityQueryError(t *testing.T) {
	provider, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	requests := 0
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		return httpResponse(http.StatusTooManyRequests, `{"errors":[{"message":"too many requests"}]}`), nil
	})

	_, err = provider.Provision(context.Background(), &providers.ProvisionRequest{
		Name:        "worker",
		GPUType:     providers.GPUL40S,
		GPUCount:    1,
		DockerImage: "custom/worker:v1",
	})
	var providerErr *providers.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Code != providers.ProviderErrorRateLimited {
		t.Fatalf("expected rate_limited to be preserved, got %q", providerErr.Code)
	}
	if requests != 1 {
		t.Fatalf("expected capacity query failure to stop before create, got %d requests", requests)
	}
}

func TestProvisionRejectsAdvertisedPriceAboveCapBeforeCreate(t *testing.T) {
	provider := newPriceTestProvider(t)
	createCalls := 0
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		request := decodeGraphQLRequest(t, req)
		if strings.Contains(request.Query, "query GpuPlacementEvidence") {
			return httpResponse(http.StatusOK, `{"data":{"gpuTypes":[{
				"id":"NVIDIA L40S",
				"displayName":"NVIDIA L40S",
				"communityPrice":0.45,
				"maxGpuCountCommunityCloud":2,
				"maxGpuCountSecureCloud":0,
				"lowestPrice2":{"uninterruptablePrice":0.81}
			}]}}`), nil
		}
		createCalls++
		return httpResponse(http.StatusOK, `{"data":{}}`), nil
	})

	_, err := provider.Provision(context.Background(), priceTestRequest(2, 0.80))
	var providerErr *providers.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != providers.ProviderErrorInvalidRequest {
		t.Fatalf("expected typed cap rejection, got %v", err)
	}
	if !strings.Contains(providerErr.Message, "advertised hourly price 0.81") {
		t.Fatalf("expected advertised price in error, got %q", providerErr.Message)
	}
	if createCalls != 0 {
		t.Fatalf("expected no create mutation, got %d", createCalls)
	}
}

func TestProvisionRejectsMissingAdvertisedPriceUnderCap(t *testing.T) {
	provider := newPriceTestProvider(t)
	createCalls := 0
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		request := decodeGraphQLRequest(t, req)
		if strings.Contains(request.Query, "query GpuPlacementEvidence") {
			return httpResponse(http.StatusOK, `{"data":{"gpuTypes":[{
				"id":"NVIDIA L40S",
				"displayName":"NVIDIA L40S",
				"communityPrice":0,
				"securePrice":0,
				"maxGpuCountCommunityCloud":1,
				"maxGpuCountSecureCloud":0
			}]}}`), nil
		}
		createCalls++
		return httpResponse(http.StatusOK, `{"data":{}}`), nil
	})

	_, err := provider.Provision(context.Background(), priceTestRequest(1, 1.00))
	var providerErr *providers.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != providers.ProviderErrorServiceUnavailable {
		t.Fatalf("expected unavailable price evidence error, got %v", err)
	}
	if createCalls != 0 {
		t.Fatalf("expected no create mutation, got %d", createCalls)
	}
}

func TestProvisionRejectsMismatchedLiveGPUTypeBeforeCreate(t *testing.T) {
	provider := newPriceTestProvider(t)
	createCalls := 0
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		request := decodeGraphQLRequest(t, req)
		if strings.Contains(request.Query, "query GpuPlacementEvidence") {
			return httpResponse(http.StatusOK, `{"data":{"gpuTypes":[{
				"id":"NVIDIA L40S",
				"displayName":"NVIDIA H100 PCIe",
				"communityPrice":0.79,
				"maxGpuCountCommunityCloud":1,
				"maxGpuCountSecureCloud":0
			}]}}`), nil
		}
		createCalls++
		return httpResponse(http.StatusOK, `{"data":{}}`), nil
	})

	_, err := provider.Provision(context.Background(), priceTestRequest(1, 1.00))
	var providerErr *providers.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != providers.ProviderErrorInvalidRequest {
		t.Fatalf("expected GPU type reconciliation error, got %v", err)
	}
	if createCalls != 0 {
		t.Fatalf("expected no create mutation, got %d", createCalls)
	}
}

func TestProvisionRejectsUnsupportedSpotModeBeforeProviderCalls(t *testing.T) {
	provider := newPriceTestProvider(t)
	providerCalls := 0
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		providerCalls++
		return httpResponse(http.StatusOK, `{"data":{}}`), nil
	})
	req := priceTestRequest(1, 1.00)
	req.SpotInstance = true

	_, err := provider.Provision(context.Background(), req)
	var providerErr *providers.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != providers.ProviderErrorInvalidRequest {
		t.Fatalf("expected unsupported spot error, got %v", err)
	}
	if providerCalls != 0 {
		t.Fatalf("expected no provider call for unsupported spot mode, got %d", providerCalls)
	}
}

func TestProvisionPersistsConfirmedPriceEvidenceAndDrift(t *testing.T) {
	provider := newPriceTestProvider(t)
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		request := decodeGraphQLRequest(t, req)
		switch {
		case strings.Contains(request.Query, "query GpuPlacementEvidence"):
			return httpResponse(http.StatusOK, `{"data":{"gpuTypes":[{
				"id":"NVIDIA L40S",
				"displayName":"NVIDIA L40S",
				"communityPrice":0.79,
				"maxGpuCountCommunityCloud":1,
				"maxGpuCountSecureCloud":0
			}]}}`), nil
		case strings.Contains(request.Query, "mutation CreatePod"):
			return httpResponse(http.StatusOK, `{"data":{"podFindAndDeployOnDemand":{
				"id":"pod-drift",
				"name":"worker",
				"desiredStatus":"RUNNING",
				"imageName":"custom/worker:v1",
				"machineId":"machine-1",
				"machine":{"gpuDisplayName":"NVIDIA L40S","costPerHr":0.83}
			}}}`), nil
		default:
			t.Fatalf("unexpected GraphQL operation: %s", request.Query)
			return nil, nil
		}
	})

	instance, err := provider.Provision(context.Background(), priceTestRequest(1, 0.85))
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if instance.CostPerHour != 0.83 {
		t.Fatalf("expected confirmed actual price 0.83, got %f", instance.CostPerHour)
	}
	if instance.Metadata[metadataCostCurrency] != costCurrencyUSD ||
		instance.Metadata[metadataCostUnit] != costUnitInstanceHour ||
		instance.Metadata[metadataCostSource] != costSourceRunPodMachine ||
		instance.Metadata[metadataCostAdvertised] != "0.79" ||
		instance.Metadata[metadataCostState] != costStateConfirmedDrift ||
		instance.Metadata[metadataCapacityState] != capacityStateAdvertised {
		t.Fatalf("unexpected cost evidence metadata: %#v", instance.Metadata)
	}
	if _, err := time.Parse(time.RFC3339Nano, instance.Metadata[metadataCostCapturedAt]); err != nil {
		t.Fatalf("invalid evidence capture time: %v", err)
	}
}

func TestProvisionTerminatesWhenConfirmedPriceExceedsCap(t *testing.T) {
	provider := newPriceTestProvider(t)
	terminateCalls := 0
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		request := decodeGraphQLRequest(t, req)
		switch {
		case strings.Contains(request.Query, "query GpuPlacementEvidence"):
			return placementEvidenceResponse(0.79), nil
		case strings.Contains(request.Query, "mutation CreatePod"):
			return createPodResponse("pod-over-cap", 0.82), nil
		case strings.Contains(request.Query, "mutation TerminatePod"):
			terminateCalls++
			return httpResponse(http.StatusOK, `{"data":{"podTerminate":true}}`), nil
		default:
			t.Fatalf("unexpected GraphQL operation: %s", request.Query)
			return nil, nil
		}
	})

	_, err := provider.Provision(context.Background(), priceTestRequest(1, 0.80))
	var providerErr *providers.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != providers.ProviderErrorInvalidRequest {
		t.Fatalf("expected typed confirmed cap violation, got %v", err)
	}
	if !strings.Contains(providerErr.Message, "confirmed hourly price 0.82") {
		t.Fatalf("expected confirmed price in error, got %q", providerErr.Message)
	}
	if terminateCalls != 1 {
		t.Fatalf("expected one termination, got %d", terminateCalls)
	}
}

func TestProvisionTerminatesWhenActualPriceCannotBeReconciled(t *testing.T) {
	provider := newPriceTestProvider(t)
	provider.pricePollInterval = time.Millisecond
	provider.pricePollTimeout = 5 * time.Millisecond
	getCalls := 0
	terminateCalls := 0
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		request := decodeGraphQLRequest(t, req)
		switch {
		case strings.Contains(request.Query, "query GpuPlacementEvidence"):
			return placementEvidenceResponse(0.79), nil
		case strings.Contains(request.Query, "mutation CreatePod"):
			return createPodResponse("pod-missing-price", 0), nil
		case strings.Contains(request.Query, "query GetPod"):
			getCalls++
			return httpResponse(http.StatusOK, `{"data":{"pod":{
				"id":"pod-missing-price",
				"name":"worker",
				"desiredStatus":"RUNNING",
				"machine":{"gpuDisplayName":"NVIDIA L40S","costPerHr":0}
			}}}`), nil
		case strings.Contains(request.Query, "mutation TerminatePod"):
			terminateCalls++
			return httpResponse(http.StatusOK, `{"data":{"podTerminate":true}}`), nil
		default:
			t.Fatalf("unexpected GraphQL operation: %s", request.Query)
			return nil, nil
		}
	})

	_, err := provider.Provision(context.Background(), priceTestRequest(1, 1.00))
	var providerErr *providers.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != providers.ProviderErrorTimeout {
		t.Fatalf("expected typed reconciliation timeout, got %v", err)
	}
	if getCalls == 0 || terminateCalls != 1 {
		t.Fatalf("expected bounded reconciliation and one termination, got get=%d terminate=%d", getCalls, terminateCalls)
	}
}

func TestProvisionPreservesPriceViolationWhenCleanupFails(t *testing.T) {
	provider := newPriceTestProvider(t)
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		request := decodeGraphQLRequest(t, req)
		switch {
		case strings.Contains(request.Query, "query GpuPlacementEvidence"):
			return placementEvidenceResponse(0.79), nil
		case strings.Contains(request.Query, "mutation CreatePod"):
			return createPodResponse("pod-cleanup-failure", 0.82), nil
		case strings.Contains(request.Query, "mutation TerminatePod"):
			return httpResponse(http.StatusOK, `{"errors":[{"message":"termination failed"}]}`), nil
		default:
			t.Fatalf("unexpected GraphQL operation: %s", request.Query)
			return nil, nil
		}
	})

	_, err := provider.Provision(context.Background(), priceTestRequest(1, 0.80))
	var providerErr *providers.ProviderError
	if !errors.As(err, &providerErr) || providerErr.Code != providers.ProviderErrorInvalidRequest {
		t.Fatalf("expected primary cap violation to remain typed, got %v", err)
	}
	if !strings.Contains(err.Error(), "terminate RunPod pod pod-cleanup-failure") ||
		!strings.Contains(err.Error(), "termination failed") {
		t.Fatalf("expected cleanup failure alongside primary violation, got %v", err)
	}
}

func TestRunPodOfferingPriceSelectsModeAndGPUCount(t *testing.T) {
	onDemandOne := 0.45
	spotOne := 0.21
	communityAvailable := 3
	secureAvailable := 0
	evidence := runpodGPUOfferingEvidence{
		CommunityPrice:         &onDemandOne,
		CommunitySpotPrice:     &spotOne,
		MaxGPUCountCommunity:   &communityAvailable,
		MaxGPUCountSecureCloud: &secureAvailable,
		LowestPrice2: &runpodLowestPrice{
			UninterruptablePrice: 0.81,
			MinimumBidPrice:      0.39,
		},
	}

	if got := evidence.priceFor(2, false); got != 0.81 {
		t.Fatalf("expected exact two-GPU on-demand price, got %f", got)
	}
	if got := evidence.priceFor(2, true); got != 0.39 {
		t.Fatalf("expected exact two-GPU spot price, got %f", got)
	}
	if got := evidence.priceFor(3, false); got != 1.35 {
		t.Fatalf("expected live per-GPU on-demand fallback for three GPUs, got %f", got)
	}
	if got := evidence.priceFor(3, true); got != 0.63 {
		t.Fatalf("expected live per-GPU spot fallback for three GPUs, got %f", got)
	}
}

func TestHourlyCapComparisonUsesNanoUSDDecimalBoundary(t *testing.T) {
	if exceedsHourlyCap(0.1+0.2, 0.3) {
		t.Fatal("equivalent decimal prices must not exceed the cap because of float representation")
	}
	if !exceedsHourlyCap(0.300000001, 0.3) {
		t.Fatal("one nano-USD/hour above the cap must be rejected")
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
		captured = decodeGraphQLRequest(t, req)
		if strings.Contains(captured.Query, "query GpuPlacementEvidence") {
			return httpResponse(http.StatusOK, `{"data":{"gpuTypes":[{"id":"NVIDIA L40S","displayName":"NVIDIA L40S","communityPrice":0.79,"maxGpuCountCommunityCloud":1,"maxGpuCountSecureCloud":0}]}}`), nil
		}
		return httpResponse(http.StatusOK, `{"data":{"podFindAndDeployOnDemand":{"id":"pod-123","name":"worker","desiredStatus":"RUNNING","imageName":"custom/worker:v1","machineId":"machine-1","machine":{"gpuDisplayName":"NVIDIA L40S","costPerHr":0.79}}}}`), nil
	})

	instance, err := provider.Provision(context.Background(), &providers.ProvisionRequest{
		Name:            "worker",
		GPUType:         providers.GPUL40S,
		GPUCount:        1,
		DockerImage:     "custom/worker:v1",
		Models:          []string{"meta-llama/Meta-Llama-3.1-8B-Instruct"},
		GatewayAddress:  "https://gateway.example.com",
		WorkerToken:     "worker-shared-token",
		ReleaseID:       "release-1",
		ProtocolVersion: "1",
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
	if got := input["cloudType"]; got != "ALL" {
		t.Fatalf("expected all reviewed RunPod cloud tiers, got %#v", got)
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
	assertEnvContains(t, env, "INFERA_RELEASE_ID", "release-1")
	assertEnvContains(t, env, "INFERA_WORKER_PROTOCOL_VERSION", "1")
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
		captured = decodeGraphQLRequest(t, req)
		if strings.Contains(captured.Query, "query GpuPlacementEvidence") {
			return httpResponse(http.StatusOK, `{"data":{"gpuTypes":[{"id":"NVIDIA A100 80GB PCIe","displayName":"NVIDIA A100 80GB PCIe","communityPrice":1.19,"maxGpuCountCommunityCloud":0,"maxGpuCountSecureCloud":1}]}}`), nil
		}
		return httpResponse(http.StatusOK, `{"data":{"podFindAndDeployOnDemand":{"id":"pod-123","name":"worker","desiredStatus":"RUNNING","imageName":"custom/worker:v1","machineId":"machine-1","machine":{"gpuDisplayName":"NVIDIA A100 80GB PCIe","costPerHr":1.19}}}}`), nil
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
		captured = decodeGraphQLRequest(t, req)
		if strings.Contains(captured.Query, "query GpuPlacementEvidence") {
			return httpResponse(http.StatusOK, `{"data":{"gpuTypes":[{"id":"NVIDIA L40S","displayName":"NVIDIA L40S","communityPrice":0.79,"maxGpuCountCommunityCloud":1,"maxGpuCountSecureCloud":0}]}}`), nil
		}
		return httpResponse(http.StatusOK, `{"data":{"podFindAndDeployOnDemand":{"id":"pod-123","name":"worker","desiredStatus":"RUNNING","imageName":"custom/worker:v1","machineId":"machine-1","machine":{"gpuDisplayName":"NVIDIA L40S","costPerHr":0.79}}}}`), nil
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

func TestListOfferingsDoesNotSubstituteStaticPriceEvidence(t *testing.T) {
	provider := newPriceTestProvider(t)
	provider.httpClient.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return httpResponse(http.StatusOK, `{"data":{"gpuTypes":[{
			"id":"gpu-l40s",
			"displayName":"NVIDIA L40S",
			"memoryInGb":48,
			"communityPrice":0,
			"securePrice":0,
			"communitySpotPrice":0,
			"secureSpotPrice":0,
			"maxGpuCountCommunityCloud":1,
			"maxGpuCountSecureCloud":0
		}]}}`), nil
	})

	offerings, err := provider.ListOfferings(context.Background())
	if err != nil {
		t.Fatalf("ListOfferings: %v", err)
	}
	if len(offerings) != 1 {
		t.Fatalf("expected one advertised offering, got %d", len(offerings))
	}
	if offerings[0].CostPerHour != 0 || offerings[0].SpotPrice != 0 {
		t.Fatalf("missing live prices must remain untrusted zero values, got %+v", offerings[0])
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
		captured = decodeGraphQLRequest(t, req)
		if strings.Contains(captured.Query, "query GpuPlacementEvidence") {
			return httpResponse(http.StatusOK, `{"data":{"gpuTypes":[{"id":"H200 SXM","displayName":"NVIDIA H200 SXM","communityPrice":2.69,"maxGpuCountCommunityCloud":1,"maxGpuCountSecureCloud":0}]}}`), nil
		}
		return httpResponse(http.StatusOK, `{"data":{"podFindAndDeployOnDemand":{"id":"pod-h200","name":"worker","desiredStatus":"RUNNING","imageName":"custom/worker:v1","machineId":"machine-1","machine":{"gpuDisplayName":"NVIDIA H200 SXM","costPerHr":2.69}}}}`), nil
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

func newPriceTestProvider(t *testing.T) *Provider {
	t.Helper()
	provider, err := New(Config{APIKey: "test-key"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return provider
}

func priceTestRequest(gpuCount int, maxCostHour float64) *providers.ProvisionRequest {
	return &providers.ProvisionRequest{
		Name:        "worker",
		GPUType:     providers.GPUL40S,
		GPUCount:    gpuCount,
		MaxCostHour: maxCostHour,
		DockerImage: "custom/worker:v1",
	}
}

func decodeGraphQLRequest(t *testing.T, req *http.Request) graphQLRequest {
	t.Helper()
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	var request graphQLRequest
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("json.Unmarshal request: %v", err)
	}
	return request
}

func placementEvidenceResponse(price float64) *http.Response {
	return httpResponse(http.StatusOK, `{"data":{"gpuTypes":[{
		"id":"NVIDIA L40S",
		"displayName":"NVIDIA L40S",
		"communityPrice":`+formatHourlyPrice(price)+`,
		"maxGpuCountCommunityCloud":1,
		"maxGpuCountSecureCloud":0
	}]}}`)
}

func createPodResponse(podID string, price float64) *http.Response {
	return httpResponse(http.StatusOK, `{"data":{"podFindAndDeployOnDemand":{
		"id":"`+podID+`",
		"name":"worker",
		"desiredStatus":"RUNNING",
		"imageName":"custom/worker:v1",
		"machineId":"machine-1",
		"machine":{"gpuDisplayName":"NVIDIA L40S","costPerHr":`+formatHourlyPrice(price)+`}
	}}}`)
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
