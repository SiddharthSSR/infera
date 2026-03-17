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
}

func TestApplyRuntimeDefaultsPreservesExplicitOverrides(t *testing.T) {
	req := &ProvisionRequest{
		GPUType:  GPUL40S,
		GPUCount: 1,
		Models:   []string{"Qwen/Qwen3-4B-Thinking-2507"},
		Options: map[string]string{
			OptionVLLMMaxModelLen:          "8192",
			OptionVLLMGPUMemoryUtilization: "0.90",
		},
	}

	ApplyRuntimeDefaults(req)

	if got := req.Options[OptionVLLMMaxModelLen]; got != "8192" {
		t.Fatalf("expected explicit max model len to remain 8192, got %q", got)
	}
	if got := req.Options[OptionVLLMGPUMemoryUtilization]; got != "0.90" {
		t.Fatalf("expected explicit gpu memory utilization to remain 0.90, got %q", got)
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
