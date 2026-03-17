package providers

import (
	"fmt"
	"strings"
)

const (
	OptionVLLMMaxModelLen          = "INFERA_VLLM_MAX_MODEL_LEN"
	OptionVLLMGPUMemoryUtilization = "INFERA_VLLM_GPU_MEMORY_UTILIZATION"
)

type modelRuntimePreset struct {
	MaxModelLen          int
	GPUMemoryUtilization string
}

// ApplyRuntimeDefaults injects conservative runtime defaults for known models.
// Explicit caller-provided overrides always win.
func ApplyRuntimeDefaults(req *ProvisionRequest) {
	if req == nil || len(req.Models) == 0 {
		return
	}

	preset, ok := runtimePresetForRequest(req)
	if !ok {
		return
	}

	if req.Options == nil {
		req.Options = map[string]string{}
	}

	if strings.TrimSpace(req.Options[OptionVLLMMaxModelLen]) == "" && preset.MaxModelLen > 0 {
		req.Options[OptionVLLMMaxModelLen] = fmt.Sprintf("%d", preset.MaxModelLen)
	}
	if strings.TrimSpace(req.Options[OptionVLLMGPUMemoryUtilization]) == "" && preset.GPUMemoryUtilization != "" {
		req.Options[OptionVLLMGPUMemoryUtilization] = preset.GPUMemoryUtilization
	}
}

// WorkerRuntimeEnv returns recognized worker runtime env vars derived from request options.
func WorkerRuntimeEnv(req *ProvisionRequest) map[string]string {
	if req == nil || len(req.Options) == 0 {
		return nil
	}

	env := make(map[string]string)
	for _, key := range []string{OptionVLLMMaxModelLen, OptionVLLMGPUMemoryUtilization} {
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

func runtimePresetForRequest(req *ProvisionRequest) (modelRuntimePreset, bool) {
	if req == nil || len(req.Models) == 0 {
		return modelRuntimePreset{}, false
	}

	modelID := strings.TrimSpace(req.Models[0])
	switch modelID {
	case "Qwen/Qwen2.5-7B-Instruct":
		return modelRuntimePreset{
			MaxModelLen:          32768,
			GPUMemoryUtilization: "0.94",
		}, true
	case "Qwen/Qwen3-4B-Thinking-2507":
		return modelRuntimePreset{
			MaxModelLen:          65536,
			GPUMemoryUtilization: "0.94",
		}, true
	case "moonshotai/Kimi-K2.5-Instruct":
		maxLen := 16384
		if req.GPUType == GPUH100 && req.GPUCount >= 8 {
			maxLen = 32768
		}
		return modelRuntimePreset{
			MaxModelLen:          maxLen,
			GPUMemoryUtilization: "0.95",
		}, true
	default:
		return modelRuntimePreset{}, false
	}
}
