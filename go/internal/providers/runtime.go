package providers

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/infera/infera/go/internal/benchmarkspecs"
)

const (
	OptionVLLMTensorParallelSize   = "INFERA_VLLM_TENSOR_PARALLEL_SIZE"
	OptionVLLMMaxModelLen          = "INFERA_VLLM_MAX_MODEL_LEN"
	OptionVLLMGPUMemoryUtilization = "INFERA_VLLM_GPU_MEMORY_UTILIZATION"
	OptionVLLMEnablePrefixCaching  = "INFERA_VLLM_ENABLE_PREFIX_CACHING"
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

var runtimeOptionTokenPattern = regexp.MustCompile(`^[A-Za-z0-9._/\-\[\]]+$`)

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

	preset, spec, found := runtimePresetForRequest(req)
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

	applySpecDecodingDefaults(req, spec)
}

func applySGLangRuntimeDefaults(req *ProvisionRequest) {
	if req == nil {
		return
	}
	preset, _, found := runtimePresetForRequest(req)
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
	if preset.EnableChunkedPrefill != nil && strings.TrimSpace(req.Options[OptionSGLangDisableCudaGraph]) == "" && preset.EnforceEager != nil {
		req.Options[OptionSGLangDisableCudaGraph] = fmt.Sprintf("%t", *preset.EnforceEager)
	}
}

func applyTensorRTLLMRuntimeDefaults(req *ProvisionRequest) {
	if req == nil {
		return
	}
	preset, _, found := runtimePresetForRequest(req)
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
	if strings.TrimSpace(req.Options[OptionTensorRTLLMEnableChunkedContext]) == "" && preset.EnableChunkedPrefill != nil {
		req.Options[OptionTensorRTLLMEnableChunkedContext] = fmt.Sprintf("%t", *preset.EnableChunkedPrefill)
	}
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

// AllowedRuntimeOptions returns the recognized runtime option keys for an engine.
func AllowedRuntimeOptions(engine InferenceEngine) []string {
	return slices.Clone(workerRuntimeOptionKeys(engine.OrDefault()))
}

// ValidateRuntimeOptions rejects unknown or empty runtime option entries.
func ValidateRuntimeOptions(engine InferenceEngine, options map[string]string) error {
	if len(options) == 0 {
		return nil
	}
	allowed := make(map[string]struct{}, len(workerRuntimeOptionKeys(engine)))
	for _, key := range workerRuntimeOptionKeys(engine) {
		allowed[key] = struct{}{}
	}
	for key, value := range options {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			return fmt.Errorf("runtime option key must not be empty")
		}
		if _, ok := allowed[trimmedKey]; !ok {
			return fmt.Errorf("unsupported runtime option %q for engine %s", trimmedKey, engine.OrDefault())
		}
		trimmedValue := strings.TrimSpace(value)
		if trimmedValue == "" {
			return fmt.Errorf("runtime option %q must not be empty", trimmedKey)
		}
		if err := validateRuntimeOptionValue(trimmedKey, trimmedValue); err != nil {
			return err
		}
	}
	return nil
}

func validateRuntimeOptionValue(key string, value string) error {
	switch key {
	case OptionVLLMTensorParallelSize,
		OptionVLLMMaxModelLen,
		OptionVLLMMaxNumBatchedTokens,
		OptionVLLMMaxNumSeqs,
		OptionSGLangTPSize,
		OptionSGLangContextLength,
		OptionSGLangChunkedPrefillSize,
		OptionSGLangMaxRunningRequests,
		OptionTensorRTLLMTensorParallelSize,
		OptionTensorRTLLMMaxBatchSize,
		OptionTensorRTLLMMaxNumTokens,
		OptionTensorRTLLMMaxBeamWidth:
		return validatePositiveInt(key, value)
	case OptionVLLMNumSchedulerSteps,
		OptionVLLMNumSpecTokens,
		OptionVLLMNgramLookup:
		return validateNonNegativeInt(key, value)
	case OptionVLLMGPUMemoryUtilization,
		OptionSGLangMemFractionStatic:
		return validateFloatRange(key, value, 0, 1, false)
	case OptionTensorRTLLMKVCacheFreeGPUMemoryFraction:
		return validateFloatRange(key, value, 0, 1, true)
	case OptionVLLMEnableChunkedPrefill,
		OptionVLLMEnablePrefixCaching,
		OptionVLLMEnforceEager,
		OptionSGLangDisableCudaGraph,
		OptionTensorRTLLMEnableChunkedContext:
		return validateBool(key, value)
	case OptionVLLMSwapSpace:
		return validateNonNegativeFloat(key, value)
	case OptionVLLMSpeculativeModel,
		OptionSGLangSchedulePolicy,
		OptionSGLangAttentionBackend,
		OptionSGLangSamplingBackend:
		return validateSafeToken(key, value)
	case OptionTensorRTLLMBackend:
		if !strings.EqualFold(value, "tensorrt") {
			return fmt.Errorf("runtime option %q must be \"tensorrt\"", key)
		}
		return nil
	default:
		return nil
	}
}

func validatePositiveInt(key string, value string) error {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fmt.Errorf("runtime option %q must be a positive integer", key)
	}
	return nil
}

func validateNonNegativeInt(key string, value string) error {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fmt.Errorf("runtime option %q must be a non-negative integer", key)
	}
	return nil
}

func validateFloatRange(key string, value string, min float64, max float64, includeZero bool) error {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fmt.Errorf("runtime option %q must be a decimal number", key)
	}
	if includeZero {
		if parsed < min || parsed > max {
			return fmt.Errorf("runtime option %q must be between %g and %g", key, min, max)
		}
		return nil
	}
	if parsed <= min || parsed > max {
		return fmt.Errorf("runtime option %q must be greater than %g and at most %g", key, min, max)
	}
	return nil
}

func validateNonNegativeFloat(key string, value string) error {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed < 0 {
		return fmt.Errorf("runtime option %q must be a non-negative decimal number", key)
	}
	return nil
}

func validateBool(key string, value string) error {
	if _, err := strconv.ParseBool(value); err != nil {
		return fmt.Errorf("runtime option %q must be a boolean", key)
	}
	return nil
}

func validateSafeToken(key string, value string) error {
	if len(value) > 255 || !runtimeOptionTokenPattern.MatchString(value) {
		return fmt.Errorf("runtime option %q contains unsupported characters", key)
	}
	return nil
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
			OptionVLLMEnablePrefixCaching,
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

func runtimePresetForRequest(req *ProvisionRequest) (modelRuntimePreset, *specDecodingConfig, bool) {
	if req == nil {
		return modelRuntimePreset{}, nil, false
	}

	modelID := ""
	if len(req.Models) > 0 {
		modelID = strings.TrimSpace(req.Models[0])
	}

	resolved, found, err := benchmarkspecs.ResolveRuntimeHeuristic(modelID, string(req.GPUType), req.GPUCount)
	if err != nil || !found {
		return modelRuntimePreset{}, nil, false
	}

	preset := modelRuntimePreset{
		TensorParallelSize:   resolved.TensorParallelSize,
		MaxModelLen:          resolved.MaxModelLen,
		GPUMemoryUtilization: resolved.GPUMemoryUtilization,
		EnableChunkedPrefill: resolved.EnableChunkedPrefill,
		MaxNumBatchedTokens:  resolved.MaxNumBatchedTokens,
		MaxNumSeqs:           resolved.MaxNumSeqs,
		SwapSpace:            resolved.SwapSpace,
		EnforceEager:         resolved.EnforceEager,
		NumSchedulerSteps:    resolved.NumSchedulerSteps,
	}

	var spec *specDecodingConfig
	if resolved.SpecDecoding != nil {
		spec = &specDecodingConfig{
			DraftModel:    resolved.SpecDecoding.DraftModel,
			NumSpecTokens: resolved.SpecDecoding.NumSpecTokens,
			NgramLookup:   resolved.SpecDecoding.NgramLookup,
		}
	}
	return preset, spec, true
}
