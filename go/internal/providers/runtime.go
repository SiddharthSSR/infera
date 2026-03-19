package providers

import (
	"fmt"
	"strings"
)

const (
	OptionVLLMMaxModelLen          = "INFERA_VLLM_MAX_MODEL_LEN"
	OptionVLLMGPUMemoryUtilization = "INFERA_VLLM_GPU_MEMORY_UTILIZATION"
	OptionVLLMEnableChunkedPrefill = "INFERA_VLLM_ENABLE_CHUNKED_PREFILL"
	OptionVLLMMaxNumBatchedTokens  = "INFERA_VLLM_MAX_NUM_BATCHED_TOKENS"
	OptionVLLMNumSchedulerSteps    = "INFERA_VLLM_NUM_SCHEDULER_STEPS"
	OptionVLLMSpeculativeModel     = "INFERA_VLLM_SPECULATIVE_MODEL"
	OptionVLLMNumSpecTokens        = "INFERA_VLLM_NUM_SPECULATIVE_TOKENS"
	OptionVLLMNgramLookup          = "INFERA_VLLM_NGRAM_PROMPT_LOOKUP_NUM_TOKENS"

	// largeGPUVRAMThresholdGB is the minimum VRAM (GB) required to enable a
	// real draft model. GPUs below this threshold get no speculative decoding.
	largeGPUVRAMThresholdGB = 40
)

type modelRuntimePreset struct {
	MaxModelLen          int
	GPUMemoryUtilization string
	EnableChunkedPrefill *bool
	MaxNumBatchedTokens  int
	NumSchedulerSteps    int
}

// specDecodingConfig defines speculative decoding for a model on large GPUs.
// When DraftModel is empty, ngram drafting is used (no extra model download).
// When DraftModel is set, it must be architecture-compatible with the target model.
type specDecodingConfig struct {
	DraftModel    string // HF model ID, or "" for ngram mode
	NumSpecTokens int    // tokens to speculate per forward pass
	NgramLookup   int    // context look-back for ngram; only used when DraftModel == ""
}

// ApplyRuntimeDefaults injects conservative runtime defaults for known models.
// Explicit caller-provided overrides always win.
func ApplyRuntimeDefaults(req *ProvisionRequest) {
	if req == nil || len(req.Models) == 0 {
		return
	}

	entry, ok := modelRuntimePresets[strings.TrimSpace(req.Models[0])]
	if !ok {
		return
	}

	preset, _ := runtimePresetForRequest(req)

	if req.Options == nil {
		req.Options = map[string]string{}
	}

	if strings.TrimSpace(req.Options[OptionVLLMMaxModelLen]) == "" && preset.MaxModelLen > 0 {
		req.Options[OptionVLLMMaxModelLen] = fmt.Sprintf("%d", preset.MaxModelLen)
	}
	if strings.TrimSpace(req.Options[OptionVLLMGPUMemoryUtilization]) == "" && preset.GPUMemoryUtilization != "" {
		req.Options[OptionVLLMGPUMemoryUtilization] = preset.GPUMemoryUtilization
	}
	if strings.TrimSpace(req.Options[OptionVLLMEnableChunkedPrefill]) == "" && preset.EnableChunkedPrefill != nil {
		req.Options[OptionVLLMEnableChunkedPrefill] = fmt.Sprintf("%t", *preset.EnableChunkedPrefill)
	}
	if strings.TrimSpace(req.Options[OptionVLLMMaxNumBatchedTokens]) == "" && preset.MaxNumBatchedTokens > 0 {
		req.Options[OptionVLLMMaxNumBatchedTokens] = fmt.Sprintf("%d", preset.MaxNumBatchedTokens)
	}
	if strings.TrimSpace(req.Options[OptionVLLMNumSchedulerSteps]) == "" && preset.NumSchedulerSteps > 0 {
		req.Options[OptionVLLMNumSchedulerSteps] = fmt.Sprintf("%d", preset.NumSchedulerSteps)
	}

	applySpecDecodingDefaults(req, entry.SpecDecoding)
}

// applySpecDecodingDefaults injects spec decoding options for large GPUs.
// It is a no-op on small GPUs or when spec is nil. Caller-provided overrides win.
func applySpecDecodingDefaults(req *ProvisionRequest, spec *specDecodingConfig) {
	if spec == nil {
		return
	}
	if !isLargeGPU(req.GPUType) {
		return
	}
	// Already explicitly configured by caller — don't override.
	if strings.TrimSpace(req.Options[OptionVLLMSpeculativeModel]) != "" {
		return
	}

	if spec.DraftModel != "" {
		// Real draft model mode.
		req.Options[OptionVLLMSpeculativeModel] = spec.DraftModel
		req.Options[OptionVLLMNumSpecTokens] = fmt.Sprintf("%d", spec.NumSpecTokens)
	} else if spec.NgramLookup > 0 {
		// Ngram mode — vLLM uses the sentinel string "[ngram]".
		req.Options[OptionVLLMSpeculativeModel] = "[ngram]"
		req.Options[OptionVLLMNumSpecTokens] = fmt.Sprintf("%d", spec.NumSpecTokens)
		req.Options[OptionVLLMNgramLookup] = fmt.Sprintf("%d", spec.NgramLookup)
	}
}

// isLargeGPU returns true when the GPU has enough VRAM for speculative decoding.
func isLargeGPU(gpuType GPUType) bool {
	spec, ok := GPUSpecs[gpuType]
	return ok && spec.VRAM >= largeGPUVRAMThresholdGB
}

// WorkerRuntimeEnv returns recognized worker runtime env vars derived from request options.
func WorkerRuntimeEnv(req *ProvisionRequest) map[string]string {
	if req == nil || len(req.Options) == 0 {
		return nil
	}

	env := make(map[string]string)
	for _, key := range []string{
		OptionVLLMMaxModelLen,
		OptionVLLMGPUMemoryUtilization,
		OptionVLLMEnableChunkedPrefill,
		OptionVLLMMaxNumBatchedTokens,
		OptionVLLMNumSchedulerSteps,
		OptionVLLMSpeculativeModel,
		OptionVLLMNumSpecTokens,
		OptionVLLMNgramLookup,
	} {
		if value := strings.TrimSpace(req.Options[key]); value != "" {
			env[key] = value
		}
	}

	if len(env) == 0 {
		return nil
	}
	return env
}

// ValidateWorkerImageRef requires a pinned worker image tag or digest for non-dev deploys.
func ValidateWorkerImageRef(image string) error {
	image = strings.TrimSpace(image)
	if image == "" {
		return fmt.Errorf("worker image is required; set INFERA_WORKER_IMAGE to a pinned tag or digest")
	}
	if strings.Contains(image, "@sha256:") {
		return nil
	}

	lastSlash := strings.LastIndex(image, "/")
	lastColon := strings.LastIndex(image, ":")
	if lastColon <= lastSlash {
		return fmt.Errorf("worker image %q must use an explicit non-latest tag or digest", image)
	}

	tag := strings.TrimSpace(image[lastColon+1:])
	if tag == "" || strings.EqualFold(tag, "latest") {
		return fmt.Errorf("worker image %q must not use the floating latest tag", image)
	}
	return nil
}

// modelRuntimePresetEntry is a static preset row. For models that need GPU-aware
// tuning, set GPUMinCount / GPUType and provide an Override preset; the base
// preset is used when those conditions are not met.
// SpecDecoding is applied automatically on large GPUs (VRAM >= largeGPUVRAMThresholdGB).
type modelRuntimePresetEntry struct {
	Base     modelRuntimePreset
	Override *struct {
		GPUType     GPUType
		GPUMinCount int
		Preset      modelRuntimePreset
	}
	SpecDecoding *specDecodingConfig
}

// modelRuntimePresets maps a HuggingFace model ID to its runtime preset.
// Add new entries here when onboarding a model — no Go logic changes required.
//
// SpecDecoding is injected automatically on large GPUs (VRAM >= largeGPUVRAMThresholdGB).
// DraftModel must share the same vocabulary and tokenizer as the target model.
// Use DraftModel="" for ngram mode when no architecture-compatible draft exists.
var modelRuntimePresets = map[string]modelRuntimePresetEntry{
	"Qwen/Qwen2.5-7B-Instruct": {
		Base: modelRuntimePreset{
			MaxModelLen:          32768,
			GPUMemoryUtilization: "0.94",
			EnableChunkedPrefill: boolPtr(true),
			MaxNumBatchedTokens:  2048,
		},
		// Qwen2.5-0.5B shares the same tokenizer and architecture family.
		SpecDecoding: &specDecodingConfig{
			DraftModel:    "Qwen/Qwen2.5-0.5B-Instruct",
			NumSpecTokens: 5,
		},
	},
	"Qwen/Qwen3-4B-Thinking-2507": {
		Base: modelRuntimePreset{
			MaxModelLen:          65536,
			GPUMemoryUtilization: "0.94",
			EnableChunkedPrefill: boolPtr(true),
			MaxNumBatchedTokens:  2048,
		},
		// No confirmed Qwen3 draft model yet; use ngram drafting as a safe fallback.
		SpecDecoding: &specDecodingConfig{
			DraftModel:    "",
			NumSpecTokens: 4,
			NgramLookup:   4,
		},
	},
	"moonshotai/Kimi-K2.5-Instruct": {
		Base: modelRuntimePreset{
			MaxModelLen:          16384,
			GPUMemoryUtilization: "0.95",
			EnableChunkedPrefill: boolPtr(true),
			MaxNumBatchedTokens:  2048,
		},
		Override: &struct {
			GPUType     GPUType
			GPUMinCount int
			Preset      modelRuntimePreset
		}{
			GPUType:     GPUH100,
			GPUMinCount: 8,
			Preset: modelRuntimePreset{
				MaxModelLen:          32768,
				GPUMemoryUtilization: "0.95",
				EnableChunkedPrefill: boolPtr(true),
				MaxNumBatchedTokens:  4096,
				NumSchedulerSteps:    8,
			},
		},
		// No public draft model; ngram drafting only.
		SpecDecoding: &specDecodingConfig{
			DraftModel:    "",
			NumSpecTokens: 4,
			NgramLookup:   4,
		},
	},
}

func runtimePresetForRequest(req *ProvisionRequest) (modelRuntimePreset, bool) {
	if req == nil || len(req.Models) == 0 {
		return modelRuntimePreset{}, false
	}

	entry, ok := modelRuntimePresets[strings.TrimSpace(req.Models[0])]
	if !ok {
		return modelRuntimePreset{}, false
	}

	if entry.Override != nil && req.GPUType == entry.Override.GPUType && req.GPUCount >= entry.Override.GPUMinCount {
		return entry.Override.Preset, true
	}
	return entry.Base, true
}

func boolPtr(v bool) *bool {
	return &v
}
