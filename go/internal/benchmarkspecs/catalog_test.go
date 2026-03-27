package benchmarkspecs

import (
	"encoding/json"
	"testing"
)

func TestLoadCatalogSet(t *testing.T) {
	set, err := LoadCatalogSet()
	if err != nil {
		t.Fatalf("LoadCatalogSet: %v", err)
	}
	if _, ok := set.Engines["vllm"]; !ok {
		t.Fatalf("expected vllm in catalog")
	}
	if got := set.HardwareAliases["A100_80GB"]; got != "a100_80gb" {
		t.Fatalf("expected A100_80GB alias to resolve to a100_80gb, got %q", got)
	}
}

func TestValidateSuite(t *testing.T) {
	payload := []byte(`{
		"suite_id": "test-suite",
		"matrix": {
			"engines": ["vllm", "tensorrt_llm"],
			"hardware": ["a100_80gb"],
			"gpu_counts": [1],
			"models": ["Qwen/Qwen2.5-7B-Instruct"],
			"workloads": ["mixed"],
			"benchmark_profiles": ["provision_full"],
			"runtime_presets": ["baseline"]
		},
		"runtime_presets": [{"id": "baseline", "display_name": "Baseline", "parameters": {}}]
	}`)
	result, err := ValidateSuite(payload)
	if err != nil {
		t.Fatalf("ValidateSuite: %v", err)
	}
	statusCounts, ok := result["status_counts"].(map[string]int)
	if ok {
		if statusCounts["ready"] == 0 {
			t.Fatalf("expected at least one ready run, got %#v", statusCounts)
		}
		return
	}
	encoded, _ := json.Marshal(result["status_counts"])
	decoded := map[string]int{}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode status_counts: %v", err)
	}
	if decoded["ready"] == 0 {
		t.Fatalf("expected at least one ready run, got %#v", decoded)
	}
}

func TestCompareResultIndexes(t *testing.T) {
	raw := []byte(`{
		"results": [
			{
				"run_id": "fast",
				"engine_id": "vllm",
				"hardware_id": "a100_80gb",
				"runtime_preset_id": "baseline",
				"warm_summaries": [{"cache_reuse_mode": "affinity", "ttft_p50_ms": 300, "aggregate_total_tok_s_p50": 10, "tpot_p50_ms": 8, "failures": 0}]
			},
			{
				"run_id": "slow",
				"engine_id": "sglang",
				"hardware_id": "a100_80gb",
				"runtime_preset_id": "baseline",
				"warm_summaries": [{"cache_reuse_mode": "affinity", "ttft_p50_ms": 600, "aggregate_total_tok_s_p50": 10, "tpot_p50_ms": 8, "failures": 0}]
			}
		]
	}`)
	result, err := CompareResultIndexes([][]byte{raw}, "lowest_ttft")
	if err != nil {
		t.Fatalf("CompareResultIndexes: %v", err)
	}
	entries, ok := result["entries"].([]map[string]any)
	if ok && len(entries) > 0 {
		if entries[0]["run_id"] != "fast" {
			t.Fatalf("expected fast to rank first, got %#v", entries[0])
		}
		return
	}
	encoded, _ := json.Marshal(result["entries"])
	var decoded []map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode entries: %v", err)
	}
	if decoded[0]["run_id"] != "fast" {
		t.Fatalf("expected fast to rank first, got %#v", decoded[0])
	}
}

func TestResolveRuntimeHeuristic(t *testing.T) {
	resolved, found, err := ResolveRuntimeHeuristic("Qwen/Qwen2.5-7B-Instruct", "A100_80GB", 1)
	if err != nil {
		t.Fatalf("ResolveRuntimeHeuristic: %v", err)
	}
	if !found {
		t.Fatalf("expected runtime heuristic to resolve")
	}
	if resolved.MaxModelLen != 32768 {
		t.Fatalf("expected max model len 32768, got %d", resolved.MaxModelLen)
	}
	if resolved.MaxNumBatchedTokens != 8192 {
		t.Fatalf("expected batched tokens 8192, got %d", resolved.MaxNumBatchedTokens)
	}
	if resolved.SpecDecoding == nil || resolved.SpecDecoding.DraftModel != "Qwen/Qwen2.5-0.5B-Instruct" {
		t.Fatalf("expected draft model spec decoding, got %#v", resolved.SpecDecoding)
	}
}
