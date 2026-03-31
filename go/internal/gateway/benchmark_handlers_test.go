package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/infera/infera/go/internal/auth"
)

func TestBenchmarkHandlers(t *testing.T) {
	h := setupTestHandlers(t)

	t.Run("catalog", func(t *testing.T) {
		req := authedRequest(httptest.NewRequest(http.MethodGet, "/api/benchmarks/catalog", nil), auth.RoleOperator)
		w := httptest.NewRecorder()

		h.handleBenchmarkCatalog(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("validate", func(t *testing.T) {
		body := []byte(`{
			"suite_id": "api-suite",
			"matrix": {
				"engines": ["vllm"],
				"hardware": ["a100_80gb"],
				"gpu_counts": [1],
				"models": ["Qwen/Qwen2.5-7B-Instruct"],
				"workloads": ["mixed"],
				"benchmark_profiles": ["provision_full"],
				"runtime_presets": ["baseline"]
			},
			"runtime_presets": [{"id": "baseline", "display_name": "Baseline", "parameters": {}}]
		}`)
		req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/benchmarks/validate", bytes.NewReader(body)), auth.RoleOperator)
		w := httptest.NewRecorder()

		h.handleBenchmarkValidate(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("compare", func(t *testing.T) {
		body := map[string]any{
			"objective": "lowest_ttft",
			"indexes": []map[string]any{
				{
					"results": []map[string]any{
						{
							"run_id":            "fast",
							"engine_id":         "vllm",
							"hardware_id":       "a100_80gb",
							"runtime_preset_id": "baseline",
							"warm_summaries": []map[string]any{
								{"cache_reuse_mode": "affinity", "ttft_p50_ms": 300, "aggregate_total_tok_s_p50": 10, "tpot_p50_ms": 9, "failures": 0},
							},
						},
					},
				},
			},
		}
		bodyBytes, _ := json.Marshal(body)
		req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/benchmarks/compare", bytes.NewReader(bodyBytes)), auth.RoleOperator)
		w := httptest.NewRecorder()

		h.handleBenchmarkCompare(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})
}
