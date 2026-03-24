package providers

import (
	"fmt"
	"strings"
)

const (
	OptionVLLMTensorParallelSize   = "INFERA_VLLM_TENSOR_PARALLEL_SIZE"
	OptionVLLMMaxModelLen          = "INFERA_VLLM_MAX_MODEL_LEN"
	OptionVLLMGPUMemoryUtilization = "INFERA_VLLM_GPU_MEMORY_UTILIZATION"
	OptionVLLMEnableChunkedPrefill = "INFERA_VLLM_ENABLE_CHUNKED_PREFILL"
	OptionVLLMMaxNumBatchedTokens  = "INFERA_VLLM_MAX_NUM_BATCHED_TOKENS"
	OptionVLLMMaxNumSeqs           = "INFERA_VLLM_MAX_NUM_SEQS"
	OptionVLLMSwapSpace            = "INFERA_VLLM_SWAP_SPACE"
	OptionVLLMEnforceEager         = "INFERA_VLLM_ENFORCE_EAGER"
	OptionVLLMNumSchedulerSteps    = "INFERA_VLLM_NUM_SCHEDULER_STEPS"
	OptionVLLMSpeculativeModel     = "INFERA_VLLM_SPECULATIVE_MODEL"
	OptionVLLMNumSpecTokens        = "INFERA_VLLM_NUM_SPECULATIVE_TOKENS"
	OptionVLLMNgramLookup          = "INFERA_VLLM_NGRAM_PROMPT_LOOKUP_NUM_TOKENS"

	OptionSGLangTPSize             = "INFERA_SGLANG_TP_SIZE"
	OptionSGLangMemFractionStatic  = "INFERA_SGLANG_MEM_FRACTION_STATIC"
	OptionSGLangContextLength      = "INFERA_SGLANG_CONTEXT_LENGTH"
	OptionSGLangChunkedPrefillSize = "INFERA_SGLANG_CHUNKED_PREFILL_SIZE"
	OptionSGLangMaxRunningRequests = "INFERA_SGLANG_MAX_RUNNING_REQUESTS"
	OptionSGLangSchedulePolicy     = "INFERA_SGLANG_SCHEDULE_POLICY"
	OptionSGLangAttentionBackend   = "INFERA_SGLANG_ATTENTION_BACKEND"
	OptionSGLangSamplingBackend    = "INFERA_SGLANG_SAMPLING_BACKEND"
	OptionSGLangDisableCudaGraph   = "INFERA_SGLANG_DISABLE_CUDA_GRAPH"

	OptionTensorRTLLMTensorParallelSize           = "INFERA_TENSORRT_LLM_TENSOR_PARALLEL_SIZE"
	OptionTensorRTLLMMaxBatchSize                 = "INFERA_TENSORRT_LLM_MAX_BATCH_SIZE"
	OptionTensorRTLLMMaxNumTokens                 = "INFERA_TENSORRT_LLM_MAX_NUM_TOKENS"
	OptionTensorRTLLMMaxBeamWidth                 = "INFERA_TENSORRT_LLM_MAX_BEAM_WIDTH"
	OptionTensorRTLLMKVCacheFreeGPUMemoryFraction = "INFERA_TENSORRT_LLM_KV_CACHE_FREE_GPU_MEMORY_FRACTION"
	OptionTensorRTLLMEnableChunkedContext         = "INFERA_TENSORRT_LLM_ENABLE_CHUNKED_CONTEXT"
	OptionTensorRTLLMBackend                      = "INFERA_TENSORRT_LLM_BACKEND"

	// largeGPUVRAMThresholdGB is the minimum VRAM (GB) required to enable a
	// real draft model. GPUs below this threshold get no speculative decoding.
	largeGPUVRAMThresholdGB = 40
)

type modelRuntimePreset struct {
	TensorParallelSize   int
	MaxModelLen          int
	GPUMemoryUtilization string
	EnableChunkedPrefill *bool
	MaxNumBatchedTokens  int
	MaxNumSeqs           int
	SwapSpace            float64
	EnforceEager         *bool
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

// ApplyRuntimeDefaults injects conservative runtime defaults.
// Explicit caller-provided overrides always win.
func ApplyRuntimeDefaults(req *ProvisionRequest) {
	if req == nil {
		return
	}
	req.Engine = req.Engine.OrDefault()
	switch req.Engine {
	case EngineSGLang:
		applySGLangRuntimeDefaults(req)
	case EngineTensorRTLLM:
		applyTensorRTLLMRuntimeDefaults(req)
	default:
		applyVLLMRuntimeDefaults(req)
	}
}

func applyVLLMRuntimeDefaults(req *ProvisionRequest) {
	if req == nil {
		return
	}

	entry := modelRuntimePresetEntryForRequest(req)
	preset, found := runtimePresetForRequest(req)
	if !found {
		return
	}

	if req.Options == nil {
		req.Options = map[string]string{}
	}

	if strings.TrimSpace(req.Options[OptionVLLMMaxModelLen]) == "" && preset.MaxModelLen > 0 {
		req.Options[OptionVLLMMaxModelLen] = fmt.Sprintf("%d", preset.MaxModelLen)
	}
	if strings.TrimSpace(req.Options[OptionVLLMTensorParallelSize]) == "" {
		if preset.TensorParallelSize > 0 {
			req.Options[OptionVLLMTensorParallelSize] = fmt.Sprintf("%d", preset.TensorParallelSize)
		} else if tpSize := defaultTensorParallelSize(req); tpSize > 1 {
			req.Options[OptionVLLMTensorParallelSize] = fmt.Sprintf("%d", tpSize)
		}
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
	if strings.TrimSpace(req.Options[OptionVLLMMaxNumSeqs]) == "" && preset.MaxNumSeqs > 0 {
		req.Options[OptionVLLMMaxNumSeqs] = fmt.Sprintf("%d", preset.MaxNumSeqs)
	}
	if strings.TrimSpace(req.Options[OptionVLLMSwapSpace]) == "" && preset.SwapSpace > 0 {
		req.Options[OptionVLLMSwapSpace] = fmt.Sprintf("%g", preset.SwapSpace)
	}
	if strings.TrimSpace(req.Options[OptionVLLMEnforceEager]) == "" && preset.EnforceEager != nil {
		req.Options[OptionVLLMEnforceEager] = fmt.Sprintf("%t", *preset.EnforceEager)
	}
	if strings.TrimSpace(req.Options[OptionVLLMNumSchedulerSteps]) == "" && preset.NumSchedulerSteps > 0 {
		req.Options[OptionVLLMNumSchedulerSteps] = fmt.Sprintf("%d", preset.NumSchedulerSteps)
	}

	applySpecDecodingDefaults(req, entry.SpecDecoding)
}

func applySGLangRuntimeDefaults(req *ProvisionRequest) {
	if req == nil {
		return
	}
	entry := modelRuntimePresetEntryForRequest(req)
	preset, found := runtimePresetForRequest(req)
	if !found {
		return
	}
	if req.Options == nil {
		req.Options = map[string]string{}
	}
	if strings.TrimSpace(req.Options[OptionSGLangContextLength]) == "" && preset.MaxModelLen > 0 {
		req.Options[OptionSGLangContextLength] = fmt.Sprintf("%d", preset.MaxModelLen)
	}
	if strings.TrimSpace(req.Options[OptionSGLangTPSize]) == "" {
		if preset.TensorParallelSize > 0 {
			req.Options[OptionSGLangTPSize] = fmt.Sprintf("%d", preset.TensorParallelSize)
		} else if tpSize := defaultTensorParallelSize(req); tpSize > 1 {
			req.Options[OptionSGLangTPSize] = fmt.Sprintf("%d", tpSize)
		}
	}
	if strings.TrimSpace(req.Options[OptionSGLangMemFractionStatic]) == "" && preset.GPUMemoryUtilization != "" {
		req.Options[OptionSGLangMemFractionStatic] = preset.GPUMemoryUtilization
	}
	if strings.TrimSpace(req.Options[OptionSGLangChunkedPrefillSize]) == "" && preset.MaxNumBatchedTokens > 0 {
		req.Options[OptionSGLangChunkedPrefillSize] = fmt.Sprintf("%d", preset.MaxNumBatchedTokens)
	}
	if strings.TrimSpace(req.Options[OptionSGLangMaxRunningRequests]) == "" && preset.MaxNumSeqs > 0 {
		req.Options[OptionSGLangMaxRunningRequests] = fmt.Sprintf("%d", preset.MaxNumSeqs)
	}
	if entry.Base.EnableChunkedPrefill != nil && strings.TrimSpace(req.Options[OptionSGLangDisableCudaGraph]) == "" && preset.EnforceEager != nil {
		req.Options[OptionSGLangDisableCudaGraph] = fmt.Sprintf("%t", *preset.EnforceEager)
	}
}

func applyTensorRTLLMRuntimeDefaults(req *ProvisionRequest) {
	if req == nil {
		return
	}
	entry := modelRuntimePresetEntryForRequest(req)
	preset, found := runtimePresetForRequest(req)
	if !found {
		return
	}
	if req.Options == nil {
		req.Options = map[string]string{}
	}
	if strings.TrimSpace(req.Options[OptionTensorRTLLMTensorParallelSize]) == "" {
		if preset.TensorParallelSize > 0 {
			req.Options[OptionTensorRTLLMTensorParallelSize] = fmt.Sprintf("%d", preset.TensorParallelSize)
		} else if tpSize := defaultTensorParallelSize(req); tpSize > 1 {
			req.Options[OptionTensorRTLLMTensorParallelSize] = fmt.Sprintf("%d", tpSize)
		}
	}
	if strings.TrimSpace(req.Options[OptionTensorRTLLMMaxNumTokens]) == "" && preset.MaxNumBatchedTokens > 0 {
		req.Options[OptionTensorRTLLMMaxNumTokens] = fmt.Sprintf("%d", preset.MaxNumBatchedTokens)
	}
	if strings.TrimSpace(req.Options[OptionTensorRTLLMMaxBatchSize]) == "" && preset.MaxNumSeqs > 0 {
		req.Options[OptionTensorRTLLMMaxBatchSize] = fmt.Sprintf("%d", preset.MaxNumSeqs)
	}
	if strings.TrimSpace(req.Options[OptionTensorRTLLMEnableChunkedContext]) == "" && entry.Base.EnableChunkedPrefill != nil {
		req.Options[OptionTensorRTLLMEnableChunkedContext] = fmt.Sprintf("%t", *entry.Base.EnableChunkedPrefill)
	}
}

func modelRuntimePresetEntryForRequest(req *ProvisionRequest) modelRuntimePresetEntry {
	if req == nil || len(req.Models) == 0 {
		return modelRuntimePresetEntry{}
	}
	entry, ok := modelRuntimePresets[strings.TrimSpace(req.Models[0])]
	if !ok {
		return modelRuntimePresetEntry{}
	}
	return entry
}

func mergeRuntimePreset(base modelRuntimePreset, overlay modelRuntimePreset) modelRuntimePreset {
	merged := base
	if overlay.TensorParallelSize > 0 {
		merged.TensorParallelSize = overlay.TensorParallelSize
	}
	if overlay.MaxModelLen > 0 {
		merged.MaxModelLen = overlay.MaxModelLen
	}
	if overlay.GPUMemoryUtilization != "" {
		merged.GPUMemoryUtilization = overlay.GPUMemoryUtilization
	}
	if overlay.EnableChunkedPrefill != nil {
		merged.EnableChunkedPrefill = overlay.EnableChunkedPrefill
	}
	if overlay.MaxNumBatchedTokens > 0 {
		merged.MaxNumBatchedTokens = overlay.MaxNumBatchedTokens
	}
	if overlay.MaxNumSeqs > 0 {
		merged.MaxNumSeqs = overlay.MaxNumSeqs
	}
	if overlay.SwapSpace > 0 {
		merged.SwapSpace = overlay.SwapSpace
	}
	if overlay.EnforceEager != nil {
		merged.EnforceEager = overlay.EnforceEager
	}
	if overlay.NumSchedulerSteps > 0 {
		merged.NumSchedulerSteps = overlay.NumSchedulerSteps
	}
	return merged
}

func defaultTensorParallelSize(req *ProvisionRequest) int {
	if req == nil || req.GPUCount <= 1 {
		return 0
	}
	switch req.GPUType {
	case GPUA100_40, GPUA100_80, GPUH100:
		return req.GPUCount
	default:
		return 0
	}
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
	for _, key := range workerRuntimeOptionKeys(req.Engine.OrDefault()) {
		if value := strings.TrimSpace(req.Options[key]); value != "" {
			env[key] = value
		}
	}

	if len(env) == 0 {
		return nil
	}
	return env
}

func workerRuntimeOptionKeys(engine InferenceEngine) []string {
	switch engine.OrDefault() {
	case EngineSGLang:
		return []string{
			OptionSGLangTPSize,
			OptionSGLangMemFractionStatic,
			OptionSGLangContextLength,
			OptionSGLangChunkedPrefillSize,
			OptionSGLangMaxRunningRequests,
			OptionSGLangSchedulePolicy,
			OptionSGLangAttentionBackend,
			OptionSGLangSamplingBackend,
			OptionSGLangDisableCudaGraph,
		}
	case EngineTensorRTLLM:
		return []string{
			OptionTensorRTLLMTensorParallelSize,
			OptionTensorRTLLMMaxBatchSize,
			OptionTensorRTLLMMaxNumTokens,
			OptionTensorRTLLMMaxBeamWidth,
			OptionTensorRTLLMKVCacheFreeGPUMemoryFraction,
			OptionTensorRTLLMEnableChunkedContext,
			OptionTensorRTLLMBackend,
		}
	default:
		return []string{
			OptionVLLMTensorParallelSize,
			OptionVLLMMaxModelLen,
			OptionVLLMGPUMemoryUtilization,
			OptionVLLMEnableChunkedPrefill,
			OptionVLLMMaxNumBatchedTokens,
			OptionVLLMMaxNumSeqs,
			OptionVLLMSwapSpace,
			OptionVLLMEnforceEager,
			OptionVLLMNumSchedulerSteps,
			OptionVLLMSpeculativeModel,
			OptionVLLMNumSpecTokens,
			OptionVLLMNgramLookup,
		}
	}
}

func cloneWorkerImages(input map[InferenceEngine]string) map[InferenceEngine]string {
	if len(input) == 0 {
		return nil
	}
	cloned := make(map[InferenceEngine]string, len(input))
	for engine, image := range input {
		normalizedEngine := engine.OrDefault()
		trimmedImage := strings.TrimSpace(image)
		if trimmedImage == "" {
			continue
		}
		cloned[normalizedEngine] = trimmedImage
	}
	if len(cloned) == 0 {
		return nil
	}
	return cloned
}

func resolveWorkerImage(engine InferenceEngine, defaultImage string, images map[InferenceEngine]string) string {
	normalizedEngine := engine.OrDefault()
	if image := strings.TrimSpace(images[normalizedEngine]); image != "" {
		return image
	}
	return strings.TrimSpace(defaultImage)
}

// ValidateWorkerImageRef requires a pinned worker image tag or digest for non-dev deploys.
func ValidateWorkerImageRef(image string) error {
	image = strings.TrimSpace(image)
	if image == "" {
		return fmt.Errorf("worker image is required; set INFERA_WORKER_IMAGE or an engine-specific INFERA_WORKER_IMAGE_<ENGINE> value to a pinned tag or digest")
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

// modelRuntimePresetOverride is a GPU-aware preset override for a specific tier.
type modelRuntimePresetOverride struct {
	GPUType     GPUType
	GPUMinCount int
	Preset      modelRuntimePreset
}

// modelRuntimePresetEntry is a static preset row. For models that need GPU-aware
// tuning, provide one or more overrides. The base preset is used when none match.
// SpecDecoding is applied automatically on large GPUs (VRAM >= largeGPUVRAMThresholdGB).
type modelRuntimePresetEntry struct {
	Base         modelRuntimePreset
	Overrides    []modelRuntimePresetOverride
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
			MaxNumSeqs:           8,
		},
		Overrides: []modelRuntimePresetOverride{
			{
				GPUType:     GPUL40S,
				GPUMinCount: 1,
				Preset: modelRuntimePreset{
					MaxModelLen:          32768,
					GPUMemoryUtilization: "0.94",
					EnableChunkedPrefill: boolPtr(true),
					MaxNumBatchedTokens:  2048,
					MaxNumSeqs:           16,
				},
			},
			{
				GPUType:     GPUA100_40,
				GPUMinCount: 1,
				Preset: modelRuntimePreset{
					MaxModelLen:          32768,
					GPUMemoryUtilization: "0.94",
					EnableChunkedPrefill: boolPtr(true),
					MaxNumBatchedTokens:  4096,
					MaxNumSeqs:           32,
					NumSchedulerSteps:    4,
				},
			},
			{
				GPUType:     GPUA100_80,
				GPUMinCount: 1,
				Preset: modelRuntimePreset{
					MaxModelLen:          32768,
					GPUMemoryUtilization: "0.94",
					EnableChunkedPrefill: boolPtr(true),
					MaxNumBatchedTokens:  8192,
					MaxNumSeqs:           48,
					NumSchedulerSteps:    6,
				},
			},
			{
				GPUType:     GPUH100,
				GPUMinCount: 1,
				Preset: modelRuntimePreset{
					MaxModelLen:          32768,
					GPUMemoryUtilization: "0.94",
					EnableChunkedPrefill: boolPtr(true),
					MaxNumBatchedTokens:  8192,
					MaxNumSeqs:           64,
					NumSchedulerSteps:    8,
				},
			},
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
			MaxNumSeqs:           8,
		},
		Overrides: []modelRuntimePresetOverride{
			{
				GPUType:     GPUL40S,
				GPUMinCount: 1,
				Preset: modelRuntimePreset{
					MaxModelLen:          65536,
					GPUMemoryUtilization: "0.94",
					EnableChunkedPrefill: boolPtr(true),
					MaxNumBatchedTokens:  2048,
					MaxNumSeqs:           16,
				},
			},
			{
				GPUType:     GPUA100_40,
				GPUMinCount: 1,
				Preset: modelRuntimePreset{
					MaxModelLen:          65536,
					GPUMemoryUtilization: "0.94",
					EnableChunkedPrefill: boolPtr(true),
					MaxNumBatchedTokens:  4096,
					MaxNumSeqs:           32,
					NumSchedulerSteps:    4,
				},
			},
			{
				GPUType:     GPUA100_80,
				GPUMinCount: 1,
				Preset: modelRuntimePreset{
					MaxModelLen:          65536,
					GPUMemoryUtilization: "0.94",
					EnableChunkedPrefill: boolPtr(true),
					MaxNumBatchedTokens:  8192,
					MaxNumSeqs:           48,
					NumSchedulerSteps:    6,
				},
			},
			{
				GPUType:     GPUH100,
				GPUMinCount: 1,
				Preset: modelRuntimePreset{
					MaxModelLen:          65536,
					GPUMemoryUtilization: "0.94",
					EnableChunkedPrefill: boolPtr(true),
					MaxNumBatchedTokens:  8192,
					MaxNumSeqs:           64,
					NumSchedulerSteps:    8,
				},
			},
		},
		// No confirmed Qwen3 draft model yet; use ngram drafting as a safe fallback.
		SpecDecoding: &specDecodingConfig{
			DraftModel:    "",
			NumSpecTokens: 4,
			NgramLookup:   4,
		},
	},
	"Qwen/Qwen2.5-14B-Instruct": {
		SpecDecoding: &specDecodingConfig{
			DraftModel:    "Qwen/Qwen2.5-0.5B-Instruct",
			NumSpecTokens: 5,
		},
	},
	"Qwen/Qwen2.5-32B-Instruct": {
		SpecDecoding: &specDecodingConfig{
			DraftModel:    "Qwen/Qwen2.5-1.5B-Instruct",
			NumSpecTokens: 5,
		},
	},
	"meta-llama/Meta-Llama-3.1-8B-Instruct": {
		SpecDecoding: &specDecodingConfig{
			DraftModel:    "",
			NumSpecTokens: 4,
			NgramLookup:   4,
		},
	},
	"mistralai/Mistral-7B-Instruct-v0.2": {
		SpecDecoding: &specDecodingConfig{
			DraftModel:    "",
			NumSpecTokens: 4,
			NgramLookup:   4,
		},
	},
	"mistralai/Mistral-7B-Instruct-v0.3": {
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
			MaxNumSeqs:           4,
		},
		Overrides: []modelRuntimePresetOverride{
			{
				GPUType:     GPUH100,
				GPUMinCount: 8,
				Preset: modelRuntimePreset{
					MaxModelLen:          32768,
					GPUMemoryUtilization: "0.95",
					EnableChunkedPrefill: boolPtr(true),
					MaxNumBatchedTokens:  4096,
					MaxNumSeqs:           16,
					NumSchedulerSteps:    8,
				},
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

var gpuRuntimePresets = map[GPUType]modelRuntimePreset{
	GPURTX4080: {
		GPUMemoryUtilization: "0.90",
		EnableChunkedPrefill: boolPtr(true),
		MaxNumBatchedTokens:  2048,
		MaxNumSeqs:           8,
	},
	GPURTX4090: {
		GPUMemoryUtilization: "0.90",
		EnableChunkedPrefill: boolPtr(true),
		MaxNumBatchedTokens:  2048,
		MaxNumSeqs:           8,
	},
	GPUL40S: {
		GPUMemoryUtilization: "0.94",
		EnableChunkedPrefill: boolPtr(true),
		MaxNumBatchedTokens:  2048,
		MaxNumSeqs:           16,
	},
	GPUA100_40: {
		GPUMemoryUtilization: "0.94",
		EnableChunkedPrefill: boolPtr(true),
		MaxNumBatchedTokens:  4096,
		MaxNumSeqs:           32,
		NumSchedulerSteps:    4,
	},
	GPUA100_80: {
		GPUMemoryUtilization: "0.94",
		EnableChunkedPrefill: boolPtr(true),
		MaxNumBatchedTokens:  8192,
		MaxNumSeqs:           48,
		NumSchedulerSteps:    6,
	},
	GPUH100: {
		GPUMemoryUtilization: "0.95",
		EnableChunkedPrefill: boolPtr(true),
		MaxNumBatchedTokens:  8192,
		MaxNumSeqs:           64,
		NumSchedulerSteps:    8,
	},
}

func runtimePresetForRequest(req *ProvisionRequest) (modelRuntimePreset, bool) {
	if req == nil {
		return modelRuntimePreset{}, false
	}

	preset, ok := gpuRuntimePresets[req.GPUType]
	if !ok {
		preset = modelRuntimePreset{}
	}

	entry := modelRuntimePresetEntryForRequest(req)
	if entry.Base != (modelRuntimePreset{}) || entry.SpecDecoding != nil || len(entry.Overrides) > 0 {
		preset = mergeRuntimePreset(preset, entry.Base)
		for _, override := range entry.Overrides {
			if req.GPUType == override.GPUType && req.GPUCount >= override.GPUMinCount {
				preset = mergeRuntimePreset(preset, override.Preset)
				break
			}
		}
	}

	if preset == (modelRuntimePreset{}) {
		return modelRuntimePreset{}, false
	}
	return preset, true
}

func boolPtr(v bool) *bool {
	return &v
}
