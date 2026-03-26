package providers

import (
	"strings"
	"testing"
)

func TestResolveWorkerImage(t *testing.T) {
	images := map[InferenceEngine]string{
		EngineSGLang:      "sglang-worker:v1",
		EngineTensorRTLLM: "trt-worker:v1",
	}

	if got := resolveWorkerImage(EngineSGLang, "default-worker:v1", images); got != "sglang-worker:v1" {
		t.Fatalf("expected engine-specific sglang image, got %q", got)
	}
	if got := resolveWorkerImage(EngineVLLM, "default-worker:v1", images); got != "default-worker:v1" {
		t.Fatalf("expected fallback default image, got %q", got)
	}
	if got := resolveWorkerImage("tensorrt-llm", "default-worker:v1", images); got != "trt-worker:v1" {
		t.Fatalf("expected normalized TensorRT image, got %q", got)
	}
}

func TestCloneWorkerImagesNormalizesAndDropsEmptyValues(t *testing.T) {
	cloned := cloneWorkerImages(map[InferenceEngine]string{
		EngineSGLang:   " sglang-worker:v1 ",
		"tensorrt-llm": "trt-worker:v1",
		EngineVLLM:     "   ",
	})

	if len(cloned) != 2 {
		t.Fatalf("expected 2 worker images, got %d", len(cloned))
	}
	if got := cloned[EngineSGLang]; got != "sglang-worker:v1" {
		t.Fatalf("expected trimmed sglang image, got %q", got)
	}
	if got := cloned[EngineTensorRTLLM]; got != "trt-worker:v1" {
		t.Fatalf("expected normalized TensorRT image, got %q", got)
	}
}

func TestValidateRuntimeOptionsRejectsUnknownKeys(t *testing.T) {
	err := ValidateRuntimeOptions(EngineVLLM, map[string]string{
		"UNEXPECTED_ENV": "1",
	})
	if err == nil {
		t.Fatal("expected unknown runtime option to be rejected")
	}
}

func TestValidateRuntimeOptionsAcceptsRecognizedKeys(t *testing.T) {
	err := ValidateRuntimeOptions(EngineSGLang, map[string]string{
		OptionSGLangMaxRunningRequests: "32",
	})
	if err != nil {
		t.Fatalf("expected recognized runtime option to pass validation, got %v", err)
	}

	err = ValidateRuntimeOptions(EngineVLLM, map[string]string{
		OptionVLLMEnablePrefixCaching: "true",
	})
	if err != nil {
		t.Fatalf("expected vllm prefix caching option to pass validation, got %v", err)
	}
}

func TestValidateRuntimeOptionsRejectsInvalidValue(t *testing.T) {
	err := ValidateRuntimeOptions(EngineVLLM, map[string]string{
		OptionVLLMGPUMemoryUtilization: "1.5",
	})
	if err == nil || !strings.Contains(err.Error(), OptionVLLMGPUMemoryUtilization) {
		t.Fatalf("expected invalid gpu memory utilization error, got %v", err)
	}
}

func TestValidateRuntimeOptionsRejectsUnsafeStringValue(t *testing.T) {
	err := ValidateRuntimeOptions(EngineVLLM, map[string]string{
		OptionVLLMSpeculativeModel: "draft-model;rm -rf /",
	})
	if err == nil || !strings.Contains(err.Error(), OptionVLLMSpeculativeModel) {
		t.Fatalf("expected unsafe speculative model error, got %v", err)
	}
}

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
	if got := req.Options[OptionVLLMMaxNumSeqs]; got != "16" {
		t.Fatalf("expected max num seqs 16 on L40S, got %q", got)
	}
	if got := req.Options[OptionVLLMNumSchedulerSteps]; got != "" {
		t.Fatalf("expected no scheduler-step override on L40S base preset, got %q", got)
	}
}

func TestApplyRuntimeDefaultsUsesGPUOverrides(t *testing.T) {
	cases := []struct {
		name               string
		model              string
		gpuType            GPUType
		gpuCount           int
		wantBatchedTokens  string
		wantMaxNumSeqs     string
		wantSchedulerSteps string
	}{
		{
			name:               "Qwen2.5-7B on A100-40",
			model:              "Qwen/Qwen2.5-7B-Instruct",
			gpuType:            GPUA100_40,
			gpuCount:           1,
			wantBatchedTokens:  "4096",
			wantMaxNumSeqs:     "32",
			wantSchedulerSteps: "4",
		},
		{
			name:               "Qwen2.5-7B on A100-80",
			model:              "Qwen/Qwen2.5-7B-Instruct",
			gpuType:            GPUA100_80,
			gpuCount:           1,
			wantBatchedTokens:  "8192",
			wantMaxNumSeqs:     "48",
			wantSchedulerSteps: "6",
		},
		{
			name:               "Qwen3-4B on H100",
			model:              "Qwen/Qwen3-4B-Thinking-2507",
			gpuType:            GPUH100,
			gpuCount:           1,
			wantBatchedTokens:  "8192",
			wantMaxNumSeqs:     "64",
			wantSchedulerSteps: "8",
		},
		{
			name:               "Kimi on 8xH100",
			model:              "moonshotai/Kimi-K2.5-Instruct",
			gpuType:            GPUH100,
			gpuCount:           8,
			wantBatchedTokens:  "4096",
			wantMaxNumSeqs:     "16",
			wantSchedulerSteps: "8",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := &ProvisionRequest{
				GPUType:  tc.gpuType,
				GPUCount: tc.gpuCount,
				Models:   []string{tc.model},
			}
			ApplyRuntimeDefaults(req)

			if got := req.Options[OptionVLLMMaxNumBatchedTokens]; got != tc.wantBatchedTokens {
				t.Fatalf("expected max num batched tokens %s, got %q", tc.wantBatchedTokens, got)
			}
			if got := req.Options[OptionVLLMMaxNumSeqs]; got != tc.wantMaxNumSeqs {
				t.Fatalf("expected max num seqs %s, got %q", tc.wantMaxNumSeqs, got)
			}
			if got := req.Options[OptionVLLMNumSchedulerSteps]; got != tc.wantSchedulerSteps {
				t.Fatalf("expected scheduler steps %s, got %q", tc.wantSchedulerSteps, got)
			}
		})
	}
}

func TestApplyRuntimeDefaultsUsesTensorParallelForMultiGPUPods(t *testing.T) {
	cases := []struct {
		name     string
		model    string
		gpuType  GPUType
		gpuCount int
		wantTP   string
	}{
		{
			name:     "known model on 2xA100-80",
			model:    "Qwen/Qwen2.5-7B-Instruct",
			gpuType:  GPUA100_80,
			gpuCount: 2,
			wantTP:   "2",
		},
		{
			name:     "unknown model on 4xH100",
			model:    "meta-llama/Llama-3.1-70B-Instruct",
			gpuType:  GPUH100,
			gpuCount: 4,
			wantTP:   "4",
		},
		{
			name:     "known model on 8xH100",
			model:    "moonshotai/Kimi-K2.5-Instruct",
			gpuType:  GPUH100,
			gpuCount: 8,
			wantTP:   "8",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := &ProvisionRequest{
				GPUType:  tc.gpuType,
				GPUCount: tc.gpuCount,
				Models:   []string{tc.model},
			}
			ApplyRuntimeDefaults(req)

			if got := req.Options[OptionVLLMTensorParallelSize]; got != tc.wantTP {
				t.Fatalf("expected tensor parallel size %s, got %q", tc.wantTP, got)
			}
		})
	}
}

func TestApplyRuntimeDefaultsDoesNotEnableTensorParallelOnSingleGPU(t *testing.T) {
	req := &ProvisionRequest{
		GPUType:  GPUA100_80,
		GPUCount: 1,
		Models:   []string{"meta-llama/Llama-3.1-8B-Instruct"},
	}

	ApplyRuntimeDefaults(req)

	if got := req.Options[OptionVLLMTensorParallelSize]; got != "" {
		t.Fatalf("expected no tensor parallel override on single GPU, got %q", got)
	}
}

func TestApplyRuntimeDefaultsForUnknownModelUsesGPUFallback(t *testing.T) {
	cases := []struct {
		name               string
		gpuType            GPUType
		wantGPUUtil        string
		wantBatchedTokens  string
		wantMaxNumSeqs     string
		wantSchedulerSteps string
	}{
		{
			name:               "L40S fallback",
			gpuType:            GPUL40S,
			wantGPUUtil:        "0.94",
			wantBatchedTokens:  "2048",
			wantMaxNumSeqs:     "16",
			wantSchedulerSteps: "",
		},
		{
			name:               "A100-80 fallback",
			gpuType:            GPUA100_80,
			wantGPUUtil:        "0.94",
			wantBatchedTokens:  "8192",
			wantMaxNumSeqs:     "48",
			wantSchedulerSteps: "6",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := &ProvisionRequest{
				GPUType:  tc.gpuType,
				GPUCount: 1,
				Models:   []string{"meta-llama/Llama-3.1-8B-Instruct"},
			}
			ApplyRuntimeDefaults(req)

			if got := req.Options[OptionVLLMGPUMemoryUtilization]; got != tc.wantGPUUtil {
				t.Fatalf("expected gpu memory utilization %s, got %q", tc.wantGPUUtil, got)
			}
			if got := req.Options[OptionVLLMEnableChunkedPrefill]; got != "true" {
				t.Fatalf("expected chunked prefill true, got %q", got)
			}
			if got := req.Options[OptionVLLMMaxNumBatchedTokens]; got != tc.wantBatchedTokens {
				t.Fatalf("expected max num batched tokens %s, got %q", tc.wantBatchedTokens, got)
			}
			if got := req.Options[OptionVLLMMaxNumSeqs]; got != tc.wantMaxNumSeqs {
				t.Fatalf("expected max num seqs %s, got %q", tc.wantMaxNumSeqs, got)
			}
			if got := req.Options[OptionVLLMNumSchedulerSteps]; got != tc.wantSchedulerSteps {
				t.Fatalf("expected scheduler steps %q, got %q", tc.wantSchedulerSteps, got)
			}
			if got := req.Options[OptionVLLMMaxModelLen]; got != "" {
				t.Fatalf("expected no max model len override for unknown model, got %q", got)
			}
			if got := req.Options[OptionVLLMSpeculativeModel]; got != "" {
				t.Fatalf("expected no speculative decoding preset for unknown model, got %q", got)
			}
		})
	}
}

func TestApplyRuntimeDefaultsPreservesExplicitOverrides(t *testing.T) {
	req := &ProvisionRequest{
		GPUType:  GPUL40S,
		GPUCount: 1,
		Models:   []string{"Qwen/Qwen3-4B-Thinking-2507"},
		Options: map[string]string{
			OptionVLLMTensorParallelSize:   "2",
			OptionVLLMMaxModelLen:          "8192",
			OptionVLLMGPUMemoryUtilization: "0.90",
			OptionVLLMEnableChunkedPrefill: "false",
			OptionVLLMMaxNumBatchedTokens:  "1024",
			OptionVLLMMaxNumSeqs:           "32",
			OptionVLLMSwapSpace:            "12",
			OptionVLLMEnforceEager:         "true",
			OptionVLLMNumSchedulerSteps:    "3",
		},
	}

	ApplyRuntimeDefaults(req)

	if got := req.Options[OptionVLLMMaxModelLen]; got != "8192" {
		t.Fatalf("expected explicit max model len to remain 8192, got %q", got)
	}
	if got := req.Options[OptionVLLMTensorParallelSize]; got != "2" {
		t.Fatalf("expected explicit tensor parallel override to remain 2, got %q", got)
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
	if got := req.Options[OptionVLLMMaxNumSeqs]; got != "32" {
		t.Fatalf("expected explicit max num seqs override to remain 32, got %q", got)
	}
	if got := req.Options[OptionVLLMSwapSpace]; got != "12" {
		t.Fatalf("expected explicit swap space override to remain 12, got %q", got)
	}
	if got := req.Options[OptionVLLMEnforceEager]; got != "true" {
		t.Fatalf("expected explicit enforce eager override to remain true, got %q", got)
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
		{
			name:          "Qwen2.5-14B on H100 gets draft model",
			model:         "Qwen/Qwen2.5-14B-Instruct",
			gpuType:       GPUH100,
			wantSpecModel: "Qwen/Qwen2.5-0.5B-Instruct",
			wantNumTokens: "5",
		},
		{
			name:          "Qwen2.5-32B on H100 gets draft model",
			model:         "Qwen/Qwen2.5-32B-Instruct",
			gpuType:       GPUH100,
			wantSpecModel: "Qwen/Qwen2.5-1.5B-Instruct",
			wantNumTokens: "5",
		},
		{
			name:          "Llama 3.1 8B on A100-80 gets ngram",
			model:         "meta-llama/Meta-Llama-3.1-8B-Instruct",
			gpuType:       GPUA100_80,
			wantSpecModel: "[ngram]",
			wantNumTokens: "4",
			wantNgram:     "4",
		},
		{
			name:          "Mistral 7B v0.3 on A100-40 gets ngram",
			model:         "mistralai/Mistral-7B-Instruct-v0.3",
			gpuType:       GPUA100_40,
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
			OptionVLLMTensorParallelSize:   "4",
			OptionVLLMEnableChunkedPrefill: "true",
			OptionVLLMMaxNumBatchedTokens:  "2048",
			OptionVLLMMaxNumSeqs:           "64",
			OptionVLLMSwapSpace:            "16",
			OptionVLLMEnforceEager:         "true",
			OptionVLLMNumSchedulerSteps:    "8",
		},
	}

	env := WorkerRuntimeEnv(req)
	if got := env[OptionVLLMTensorParallelSize]; got != "4" {
		t.Fatalf("expected tensor parallel env 4, got %q", got)
	}
	if got := env[OptionVLLMEnableChunkedPrefill]; got != "true" {
		t.Fatalf("expected chunked prefill env true, got %q", got)
	}
	if got := env[OptionVLLMMaxNumBatchedTokens]; got != "2048" {
		t.Fatalf("expected max num batched tokens env 2048, got %q", got)
	}
	if got := env[OptionVLLMMaxNumSeqs]; got != "64" {
		t.Fatalf("expected max num seqs env 64, got %q", got)
	}
	if got := env[OptionVLLMSwapSpace]; got != "16" {
		t.Fatalf("expected swap space env 16, got %q", got)
	}
	if got := env[OptionVLLMEnforceEager]; got != "true" {
		t.Fatalf("expected enforce eager env true, got %q", got)
	}
	if got := env[OptionVLLMNumSchedulerSteps]; got != "8" {
		t.Fatalf("expected num scheduler steps env 8, got %q", got)
	}
}

func TestApplyRuntimeDefaultsForSGLang(t *testing.T) {
	req := &ProvisionRequest{
		Engine:   EngineSGLang,
		GPUType:  GPUA100_80,
		GPUCount: 1,
		Models:   []string{"Qwen/Qwen2.5-7B-Instruct"},
	}

	ApplyRuntimeDefaults(req)

	if got := req.Options[OptionSGLangContextLength]; got != "32768" {
		t.Fatalf("expected sglang context length 32768, got %q", got)
	}
	if got := req.Options[OptionSGLangMemFractionStatic]; got != "0.94" {
		t.Fatalf("expected sglang mem fraction 0.94, got %q", got)
	}
	if got := req.Options[OptionSGLangChunkedPrefillSize]; got != "8192" {
		t.Fatalf("expected sglang chunked prefill 8192, got %q", got)
	}
	if got := req.Options[OptionSGLangMaxRunningRequests]; got != "48" {
		t.Fatalf("expected sglang max running requests 48, got %q", got)
	}
}

func TestApplyRuntimeDefaultsForTensorRTLLM(t *testing.T) {
	req := &ProvisionRequest{
		Engine:   EngineTensorRTLLM,
		GPUType:  GPUA100_80,
		GPUCount: 2,
		Models:   []string{"Qwen/Qwen2.5-7B-Instruct"},
	}

	ApplyRuntimeDefaults(req)

	if got := req.Options[OptionTensorRTLLMTensorParallelSize]; got != "2" {
		t.Fatalf("expected TensorRT-LLM TP size 2, got %q", got)
	}
	if got := req.Options[OptionTensorRTLLMMaxNumTokens]; got != "8192" {
		t.Fatalf("expected TensorRT-LLM max tokens 8192, got %q", got)
	}
	if got := req.Options[OptionTensorRTLLMMaxBatchSize]; got != "48" {
		t.Fatalf("expected TensorRT-LLM max batch size 48, got %q", got)
	}
}

func TestWorkerRuntimeEnvUsesEngineSpecificKeys(t *testing.T) {
	req := &ProvisionRequest{
		Engine: EngineSGLang,
		Options: map[string]string{
			OptionSGLangTPSize:             "2",
			OptionSGLangContextLength:      "32768",
			OptionSGLangMaxRunningRequests: "64",
			OptionVLLMTensorParallelSize:   "8",
		},
	}

	env := WorkerRuntimeEnv(req)
	if got := env[OptionSGLangTPSize]; got != "2" {
		t.Fatalf("expected sglang tp env 2, got %q", got)
	}
	if got := env[OptionSGLangContextLength]; got != "32768" {
		t.Fatalf("expected sglang context env 32768, got %q", got)
	}
	if got := env[OptionSGLangMaxRunningRequests]; got != "64" {
		t.Fatalf("expected sglang max running requests env 64, got %q", got)
	}
	if _, exists := env[OptionVLLMTensorParallelSize]; exists {
		t.Fatalf("expected vllm env keys to be omitted for sglang engine")
	}
}
