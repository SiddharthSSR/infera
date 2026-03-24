package providers

import (
	"testing"
	"time"
)

func TestProviderTypes(t *testing.T) {
	tests := []struct {
		name     string
		provider ProviderType
		expected string
	}{
		{"RunPod", ProviderRunPod, "runpod"},
		{"VastAI", ProviderVastAI, "vastai"},
		{"Lambda", ProviderLambda, "lambda"},
		{"Mock", ProviderMock, "mock"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.provider) != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, tt.provider)
			}
		})
	}
}

func TestGPUTypes(t *testing.T) {
	tests := []struct {
		name     string
		gpu      GPUType
		expected string
	}{
		{"RTX 4090", GPURTX4090, "RTX_4090"},
		{"RTX 4080", GPURTX4080, "RTX_4080"},
		{"A100 40GB", GPUA100_40, "A100_40GB"},
		{"A100 80GB", GPUA100_80, "A100_80GB"},
		{"H100", GPUH100, "H100"},
		{"L40S", GPUL40S, "L40S"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.gpu) != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, tt.gpu)
			}
		})
	}
}

func TestInferenceEngines(t *testing.T) {
	tests := []struct {
		name     string
		engine   InferenceEngine
		expected string
	}{
		{"vLLM", EngineVLLM, "vllm"},
		{"SGLang", EngineSGLang, "sglang"},
		{"TensorRTLLM", EngineTensorRTLLM, "tensorrt_llm"},
		{"Mock", EngineMock, "mock"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.engine) != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, tt.engine)
			}
		})
	}
}

func TestNormalizeInferenceEngine(t *testing.T) {
	cases := []struct {
		input string
		want  InferenceEngine
	}{
		{input: "", want: EngineVLLM},
		{input: "vllm", want: EngineVLLM},
		{input: "sglang", want: EngineSGLang},
		{input: "tensorrt-llm", want: EngineTensorRTLLM},
		{input: "TRTLLM", want: EngineTensorRTLLM},
	}

	for _, tc := range cases {
		if got := NormalizeInferenceEngine(tc.input); got != tc.want {
			t.Fatalf("expected %q to normalize to %q, got %q", tc.input, tc.want, got)
		}
	}
}

func TestInstanceStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   InstanceStatus
		expected string
	}{
		{"Pending", InstanceStatusPending, "pending"},
		{"Provisioning", InstanceStatusProvisioning, "provisioning"},
		{"Running", InstanceStatusRunning, "running"},
		{"Stopping", InstanceStatusStopping, "stopping"},
		{"Stopped", InstanceStatusStopped, "stopped"},
		{"Terminating", InstanceStatusTerminating, "terminating"},
		{"Terminated", InstanceStatusTerminated, "terminated"},
		{"Error", InstanceStatusError, "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.status) != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, tt.status)
			}
		})
	}
}

func TestGPUSpecs(t *testing.T) {
	// Test that all GPU types have specs defined
	gpuTypes := []GPUType{GPURTX4090, GPURTX4080, GPUA100_40, GPUA100_80, GPUH100, GPUL40S}

	for _, gpu := range gpuTypes {
		t.Run(string(gpu), func(t *testing.T) {
			spec, exists := GPUSpecs[gpu]
			if !exists {
				t.Errorf("GPU spec not found for %s", gpu)
				return
			}
			if spec.VRAM <= 0 {
				t.Errorf("Invalid VRAM for %s: %d", gpu, spec.VRAM)
			}
			if spec.MemoryBW <= 0 {
				t.Errorf("Invalid MemoryBW for %s: %d", gpu, spec.MemoryBW)
			}
		})
	}
}

func TestProviderError(t *testing.T) {
	err := &ProviderError{
		Provider:   ProviderMock,
		Code:       "test_error",
		Message:    "test message",
		StatusCode: 500,
		RetryAfter: 60,
	}

	t.Run("Error string", func(t *testing.T) {
		expected := "mock: test message"
		if err.Error() != expected {
			t.Errorf("expected %s, got %s", expected, err.Error())
		}
	})

	t.Run("IsRetryable - rate limited", func(t *testing.T) {
		retryableErr := &ProviderError{Code: "rate_limited"}
		if !retryableErr.IsRetryable() {
			t.Error("rate_limited should be retryable")
		}
	})

	t.Run("IsRetryable - service unavailable", func(t *testing.T) {
		retryableErr := &ProviderError{Code: "service_unavailable"}
		if !retryableErr.IsRetryable() {
			t.Error("service_unavailable should be retryable")
		}
	})

	t.Run("IsRetryable - timeout", func(t *testing.T) {
		retryableErr := &ProviderError{Code: "timeout"}
		if !retryableErr.IsRetryable() {
			t.Error("timeout should be retryable")
		}
	})

	t.Run("IsRetryable - not found", func(t *testing.T) {
		nonRetryableErr := &ProviderError{Code: "not_found"}
		if nonRetryableErr.IsRetryable() {
			t.Error("not_found should not be retryable")
		}
	})

	t.Run("HTTPStatus - rate limited", func(t *testing.T) {
		retryableErr := &ProviderError{Code: ProviderErrorRateLimited}
		if got := retryableErr.HTTPStatus(500); got != 429 {
			t.Fatalf("expected 429, got %d", got)
		}
	})

	t.Run("APIErrorType - missing api key", func(t *testing.T) {
		authErr := &ProviderError{Code: ProviderErrorMissingAPIKey}
		if got := authErr.APIErrorType(); got != "provider_auth_failed" {
			t.Fatalf("expected provider_auth_failed, got %q", got)
		}
	})
}

func TestProviderCapabilities(t *testing.T) {
	status := &ProviderStatus{
		Provider:  ProviderMock,
		Connected: true,
		Capabilities: ProviderCapabilities{
			SupportsSpot:         true,
			SupportsCustomImages: false,
			SupportsStartStop:    true,
		},
	}

	if !status.Capabilities.SupportsSpot {
		t.Fatal("expected supports spot")
	}
	if status.Capabilities.SupportsCustomImages {
		t.Fatal("expected custom image support to be false")
	}
	if !status.Capabilities.SupportsStartStop {
		t.Fatal("expected start/stop support")
	}
}

func TestInstance(t *testing.T) {
	now := time.Now()
	instance := &Instance{
		ID:           "test-123",
		ProviderID:   "mock-test-123",
		Provider:     ProviderMock,
		Name:         "test-instance",
		Status:       InstanceStatusRunning,
		GPUType:      GPURTX4090,
		GPUCount:     2,
		VCPU:         16,
		MemoryGB:     64,
		StorageGB:    200,
		PublicIP:     "192.168.1.1",
		SSHPort:      22,
		HTTPPort:     8080,
		CostPerHour:  0.80,
		SpotInstance: true,
		CreatedAt:    now,
		StartedAt:    &now,
	}

	t.Run("Basic fields", func(t *testing.T) {
		if instance.ID != "test-123" {
			t.Errorf("expected test-123, got %s", instance.ID)
		}
		if instance.GPUCount != 2 {
			t.Errorf("expected 2, got %d", instance.GPUCount)
		}
		if instance.CostPerHour != 0.80 {
			t.Errorf("expected 0.80, got %f", instance.CostPerHour)
		}
	})

	t.Run("Spot instance", func(t *testing.T) {
		if !instance.SpotInstance {
			t.Error("expected spot instance to be true")
		}
	})
}

func TestProvisionRequest(t *testing.T) {
	req := &ProvisionRequest{
		Name:         "my-worker",
		Provider:     ProviderRunPod,
		GPUType:      GPUH100,
		GPUCount:     4,
		Region:       "us-east-1",
		SpotInstance: true,
		MaxCostHour:  10.0,
		Models:       []string{"llama-3-70b", "mixtral-8x7b"},
	}

	t.Run("Basic fields", func(t *testing.T) {
		if req.Name != "my-worker" {
			t.Errorf("expected my-worker, got %s", req.Name)
		}
		if req.GPUCount != 4 {
			t.Errorf("expected 4, got %d", req.GPUCount)
		}
		if len(req.Models) != 2 {
			t.Errorf("expected 2 models, got %d", len(req.Models))
		}
	})
}

func TestGPUOffering(t *testing.T) {
	offering := &GPUOffering{
		Provider:    ProviderRunPod,
		GPUType:     GPUA100_80,
		GPUCount:    1,
		VCPU:        16,
		MemoryGB:    128,
		StorageGB:   500,
		CostPerHour: 2.00,
		SpotPrice:   1.00,
		Region:      "us-west-2",
		Available:   25,
	}

	t.Run("Spot discount", func(t *testing.T) {
		if offering.SpotPrice >= offering.CostPerHour {
			t.Error("spot price should be less than on-demand price")
		}
	})

	t.Run("Availability", func(t *testing.T) {
		if offering.Available <= 0 {
			t.Error("available should be positive")
		}
	})
}

func TestCostSummary(t *testing.T) {
	summary := &CostSummary{
		CurrentHourly:  5.50,
		TodayTotal:     45.00,
		MonthTotal:     350.00,
		ProjectedMonth: 500.00,
		ByProvider: map[string]float64{
			"runpod": 3.50,
			"vastai": 2.00,
		},
		ByGPU: map[string]float64{
			"RTX_4090":  1.50,
			"A100_80GB": 4.00,
		},
	}

	t.Run("Provider breakdown sums correctly", func(t *testing.T) {
		total := 0.0
		for _, cost := range summary.ByProvider {
			total += cost
		}
		if total != summary.CurrentHourly {
			t.Errorf("provider breakdown (%.2f) should equal current hourly (%.2f)", total, summary.CurrentHourly)
		}
	})

	t.Run("GPU breakdown sums correctly", func(t *testing.T) {
		total := 0.0
		for _, cost := range summary.ByGPU {
			total += cost
		}
		if total != summary.CurrentHourly {
			t.Errorf("GPU breakdown (%.2f) should equal current hourly (%.2f)", total, summary.CurrentHourly)
		}
	})
}
