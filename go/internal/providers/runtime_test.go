package providers

import "testing"

func TestApplyRuntimeDefaultsForKnownModel(t *testing.T) {
	req := &ProvisionRequest{
		GPUType:  GPUL40S,
		GPUCount: 1,
		Models:   []string{"Qwen/Qwen2.5-7B-Instruct"},
	}

	ApplyRuntimeDefaults(req)

	if got := req.Options[OptionVLLMMaxModelLen]; got != "32768" {
		t.Fatalf("expected max model len 32768, got %q", got)
	}
	if got := req.Options[OptionVLLMGPUMemoryUtilization]; got != "0.94" {
		t.Fatalf("expected gpu memory utilization 0.94, got %q", got)
	}
	if got := req.Options[OptionVLLMEnableChunkedPrefill]; got != "true" {
		t.Fatalf("expected chunked prefill true, got %q", got)
	}
	if got := req.Options[OptionVLLMMaxNumBatchedTokens]; got != "2048" {
		t.Fatalf("expected max num batched tokens 2048, got %q", got)
	}
}

func TestApplyRuntimeDefaultsPreservesExplicitOverrides(t *testing.T) {
	req := &ProvisionRequest{
		GPUType:  GPUL40S,
		GPUCount: 1,
		Models:   []string{"Qwen/Qwen3-4B-Thinking-2507"},
		Options: map[string]string{
			OptionVLLMMaxModelLen:          "8192",
			OptionVLLMGPUMemoryUtilization: "0.90",
			OptionVLLMEnableChunkedPrefill: "false",
			OptionVLLMMaxNumBatchedTokens:  "1024",
			OptionVLLMNumSchedulerSteps:    "3",
		},
	}

	ApplyRuntimeDefaults(req)

	if got := req.Options[OptionVLLMMaxModelLen]; got != "8192" {
		t.Fatalf("expected explicit max model len to remain 8192, got %q", got)
	}
	if got := req.Options[OptionVLLMGPUMemoryUtilization]; got != "0.90" {
		t.Fatalf("expected explicit gpu memory utilization to remain 0.90, got %q", got)
	}
	if got := req.Options[OptionVLLMEnableChunkedPrefill]; got != "false" {
		t.Fatalf("expected explicit chunked prefill override to remain false, got %q", got)
	}
	if got := req.Options[OptionVLLMMaxNumBatchedTokens]; got != "1024" {
		t.Fatalf("expected explicit max num batched tokens override to remain 1024, got %q", got)
	}
	if got := req.Options[OptionVLLMNumSchedulerSteps]; got != "3" {
		t.Fatalf("expected explicit scheduler steps override to remain 3, got %q", got)
	}
}

func TestSpecDecodingInjectedOnLargeGPU(t *testing.T) {
	cases := []struct {
		name          string
		model         string
		gpuType       GPUType
		wantSpecModel string
		wantNumTokens string
		wantNgram     string
	}{
		{
			name:          "Qwen2.5-7B on A100-80 gets draft model",
			model:         "Qwen/Qwen2.5-7B-Instruct",
			gpuType:       GPUA100_80,
			wantSpecModel: "Qwen/Qwen2.5-0.5B-Instruct",
			wantNumTokens: "5",
		},
		{
			name:          "Qwen2.5-7B on A100-40 gets draft model",
			model:         "Qwen/Qwen2.5-7B-Instruct",
			gpuType:       GPUA100_40,
			wantSpecModel: "Qwen/Qwen2.5-0.5B-Instruct",
			wantNumTokens: "5",
		},
		{
			name:          "Qwen2.5-7B on L40S gets draft model",
			model:         "Qwen/Qwen2.5-7B-Instruct",
			gpuType:       GPUL40S,
			wantSpecModel: "Qwen/Qwen2.5-0.5B-Instruct",
			wantNumTokens: "5",
		},
		{
			name:          "Qwen3-4B on H100 gets ngram",
			model:         "Qwen/Qwen3-4B-Thinking-2507",
			gpuType:       GPUH100,
			wantSpecModel: "[ngram]",
			wantNumTokens: "4",
			wantNgram:     "4",
		},
		{
			name:          "Kimi on H100 gets ngram",
			model:         "moonshotai/Kimi-K2.5-Instruct",
			gpuType:       GPUH100,
			wantSpecModel: "[ngram]",
			wantNumTokens: "4",
			wantNgram:     "4",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := &ProvisionRequest{
				GPUType: tc.gpuType,
				Models:  []string{tc.model},
			}
			ApplyRuntimeDefaults(req)

			if got := req.Options[OptionVLLMSpeculativeModel]; got != tc.wantSpecModel {
				t.Errorf("speculative model: want %q got %q", tc.wantSpecModel, got)
			}
			if got := req.Options[OptionVLLMNumSpecTokens]; got != tc.wantNumTokens {
				t.Errorf("num speculative tokens: want %q got %q", tc.wantNumTokens, got)
			}
			if tc.wantNgram != "" {
				if got := req.Options[OptionVLLMNgramLookup]; got != tc.wantNgram {
					t.Errorf("ngram lookup: want %q got %q", tc.wantNgram, got)
				}
			}
		})
	}
}

func TestSpecDecodingNotInjectedOnSmallGPU(t *testing.T) {
	for _, gpuType := range []GPUType{GPURTX4090, GPURTX4080} {
		req := &ProvisionRequest{
			GPUType: gpuType,
			Models:  []string{"Qwen/Qwen2.5-7B-Instruct"},
		}
		ApplyRuntimeDefaults(req)

		if got := req.Options[OptionVLLMSpeculativeModel]; got != "" {
			t.Errorf("gpu %s: expected no spec model on small GPU, got %q", gpuType, got)
		}
	}
}

func TestSpecDecodingCallerOverrideRespected(t *testing.T) {
	req := &ProvisionRequest{
		GPUType: GPUA100_80,
		Models:  []string{"Qwen/Qwen2.5-7B-Instruct"},
		Options: map[string]string{
			OptionVLLMSpeculativeModel: "custom/my-draft-model",
			OptionVLLMNumSpecTokens:    "3",
		},
	}
	ApplyRuntimeDefaults(req)

	if got := req.Options[OptionVLLMSpeculativeModel]; got != "custom/my-draft-model" {
		t.Errorf("expected caller override to be preserved, got %q", got)
	}
	if got := req.Options[OptionVLLMNumSpecTokens]; got != "3" {
		t.Errorf("expected caller num spec tokens to be preserved, got %q", got)
	}
}

func TestValidateWorkerImageRef(t *testing.T) {
	cases := []struct {
		name    string
		image   string
		wantErr bool
	}{
		{name: "empty", image: "", wantErr: true},
		{name: "latest", image: "codingtensor/infera-worker:latest", wantErr: true},
		{name: "missing tag", image: "codingtensor/infera-worker", wantErr: true},
		{name: "tagged", image: "codingtensor/infera-worker:roadmap-123", wantErr: false},
		{name: "digest", image: "codingtensor/infera-worker@sha256:abcdef", wantErr: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateWorkerImageRef(tc.image)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for %q", tc.image)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error for %q, got %v", tc.image, err)
			}
		})
	}
}

func TestWorkerRuntimeEnvIncludesChunkedPrefillTunables(t *testing.T) {
	req := &ProvisionRequest{
		Options: map[string]string{
			OptionVLLMEnableChunkedPrefill: "true",
			OptionVLLMMaxNumBatchedTokens:  "2048",
			OptionVLLMNumSchedulerSteps:    "8",
		},
	}

	env := WorkerRuntimeEnv(req)
	if got := env[OptionVLLMEnableChunkedPrefill]; got != "true" {
		t.Fatalf("expected chunked prefill env true, got %q", got)
	}
	if got := env[OptionVLLMMaxNumBatchedTokens]; got != "2048" {
		t.Fatalf("expected max num batched tokens env 2048, got %q", got)
	}
	if got := env[OptionVLLMNumSchedulerSteps]; got != "8" {
		t.Fatalf("expected num scheduler steps env 8, got %q", got)
	}
}
