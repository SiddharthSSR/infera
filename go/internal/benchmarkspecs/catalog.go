package benchmarkspecs

import (
	"embed"
	"encoding/json"
	"fmt"
	"slices"
	"sync"
)

//go:embed data/*.json
var embeddedCatalogFS embed.FS

type runtimePreset struct {
	TensorParallelSize   int     `json:"tensor_parallel_size"`
	MaxModelLen          int     `json:"max_model_len"`
	GPUMemoryUtilization string  `json:"gpu_memory_utilization"`
	EnableChunkedPrefill *bool   `json:"enable_chunked_prefill"`
	MaxNumBatchedTokens  int     `json:"max_num_batched_tokens"`
	MaxNumSeqs           int     `json:"max_num_seqs"`
	SwapSpace            float64 `json:"swap_space"`
	EnforceEager         *bool   `json:"enforce_eager"`
	NumSchedulerSteps    int     `json:"num_scheduler_steps"`
}

type specDecodingConfig struct {
	DraftModel    string `json:"draft_model"`
	NumSpecTokens int    `json:"num_spec_tokens"`
	NgramLookup   int    `json:"ngram_lookup"`
}

type runtimePresetOverride struct {
	GPUType     string        `json:"gpu_type"`
	GPUMinCount int           `json:"gpu_min_count"`
	Preset      runtimePreset `json:"preset"`
}

type modelHeuristic struct {
	Base         runtimePreset           `json:"base"`
	Overrides    []runtimePresetOverride `json:"overrides"`
	SpecDecoding *specDecodingConfig     `json:"spec_decoding"`
}

type RuntimeHeuristics struct {
	GPUDefaults    map[string]runtimePreset  `json:"gpu_defaults"`
	ModelOverrides map[string]modelHeuristic `json:"model_overrides"`
}

type ResolvedSpecDecoding struct {
	DraftModel    string
	NumSpecTokens int
	NgramLookup   int
}

type ResolvedRuntimeHeuristic struct {
	TensorParallelSize   int
	MaxModelLen          int
	GPUMemoryUtilization string
	EnableChunkedPrefill *bool
	MaxNumBatchedTokens  int
	MaxNumSeqs           int
	SwapSpace            float64
	EnforceEager         *bool
	NumSchedulerSteps    int
	SpecDecoding         *ResolvedSpecDecoding
}

type engineCatalog struct {
	Engines []struct {
		ID string `json:"id"`
	} `json:"engines"`
}

type hardwareCatalog struct {
	Hardware []struct {
		HardwareID        string   `json:"hardware_id"`
		Aliases           []string `json:"aliases"`
		ProviderSelectors []struct {
			Provider string `json:"provider"`
		} `json:"provider_selectors"`
	} `json:"hardware"`
}

type modelCatalog struct {
	Models []struct {
		ModelID             string `json:"model_id"`
		EngineCompatibility map[string]struct {
			Status string `json:"status"`
		} `json:"engine_compatibility"`
	} `json:"models"`
}

type workloadCatalog struct {
	Workloads []struct {
		ID string `json:"id"`
	} `json:"workloads"`
}

type benchmarkProfileCatalog struct {
	BenchmarkProfiles []struct {
		ID            string `json:"id"`
		ExecutionMode string `json:"execution_mode"`
	} `json:"benchmark_profiles"`
}

type suiteFilter struct {
	Engine           string `json:"engine"`
	Hardware         string `json:"hardware"`
	GPUCount         int    `json:"gpu_count"`
	Model            string `json:"model"`
	Workload         string `json:"workload"`
	BenchmarkProfile string `json:"benchmark_profile"`
	RuntimePreset    string `json:"runtime_preset"`
}

type Suite struct {
	SuiteID string `json:"suite_id"`
	Matrix  struct {
		Engines           []string `json:"engines"`
		Hardware          []string `json:"hardware"`
		GPUCounts         []int    `json:"gpu_counts"`
		Models            []string `json:"models"`
		Workloads         []string `json:"workloads"`
		BenchmarkProfiles []string `json:"benchmark_profiles"`
		RuntimePresets    []string `json:"runtime_presets"`
	} `json:"matrix"`
	RuntimePresets []struct {
		ID        string   `json:"id"`
		EngineIDs []string `json:"engine_ids"`
	} `json:"runtime_presets"`
	Include         []suiteFilter `json:"include"`
	Exclude         []suiteFilter `json:"exclude"`
	AttachTarget    any           `json:"attach_target"`
	DefaultProvider string        `json:"default_provider"`
}

type CatalogSet struct {
	Raw map[string]json.RawMessage

	Engines           map[string]struct{}
	Hardware          map[string]struct{}
	HardwareAliases   map[string]string
	HardwareProviders map[string][]string
	Models            map[string]map[string]string
	Workloads         map[string]struct{}
	BenchmarkProfiles map[string]string
}

var (
	catalogOnce         sync.Once
	cachedCatalog       *CatalogSet
	cachedCatalogErr    error
	heuristicsOnce      sync.Once
	cachedHeuristics    *RuntimeHeuristics
	cachedHeuristicsErr error
)

func LoadRuntimeHeuristics() (*RuntimeHeuristics, error) {
	heuristicsOnce.Do(func() {
		raw, err := embeddedCatalogFS.ReadFile("data/runtime_heuristics.json")
		if err != nil {
			cachedHeuristicsErr = err
			return
		}
		var heuristics RuntimeHeuristics
		if err := json.Unmarshal(raw, &heuristics); err != nil {
			cachedHeuristicsErr = err
			return
		}
		cachedHeuristics = &heuristics
	})
	return cachedHeuristics, cachedHeuristicsErr
}

func ResolveRuntimeHeuristic(modelID string, gpuType string, gpuCount int) (ResolvedRuntimeHeuristic, bool, error) {
	heuristics, err := LoadRuntimeHeuristics()
	if err != nil {
		return ResolvedRuntimeHeuristic{}, false, err
	}

	preset := runtimePreset{}
	if gpuPreset, ok := heuristics.GPUDefaults[gpuType]; ok {
		preset = mergeRuntimePreset(preset, gpuPreset)
	}

	var spec *ResolvedSpecDecoding
	if modelID != "" {
		if modelPreset, ok := heuristics.ModelOverrides[modelID]; ok {
			preset = mergeRuntimePreset(preset, modelPreset.Base)
			for _, override := range modelPreset.Overrides {
				if override.GPUType == gpuType && gpuCount >= override.GPUMinCount {
					preset = mergeRuntimePreset(preset, override.Preset)
					break
				}
			}
			if modelPreset.SpecDecoding != nil {
				spec = &ResolvedSpecDecoding{
					DraftModel:    modelPreset.SpecDecoding.DraftModel,
					NumSpecTokens: modelPreset.SpecDecoding.NumSpecTokens,
					NgramLookup:   modelPreset.SpecDecoding.NgramLookup,
				}
			}
		}
	}

	if runtimePresetIsZero(preset) && spec == nil {
		return ResolvedRuntimeHeuristic{}, false, nil
	}

	return ResolvedRuntimeHeuristic{
		TensorParallelSize:   preset.TensorParallelSize,
		MaxModelLen:          preset.MaxModelLen,
		GPUMemoryUtilization: preset.GPUMemoryUtilization,
		EnableChunkedPrefill: preset.EnableChunkedPrefill,
		MaxNumBatchedTokens:  preset.MaxNumBatchedTokens,
		MaxNumSeqs:           preset.MaxNumSeqs,
		SwapSpace:            preset.SwapSpace,
		EnforceEager:         preset.EnforceEager,
		NumSchedulerSteps:    preset.NumSchedulerSteps,
		SpecDecoding:         spec,
	}, true, nil
}

func mergeRuntimePreset(base runtimePreset, overlay runtimePreset) runtimePreset {
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

func runtimePresetIsZero(preset runtimePreset) bool {
	return preset == (runtimePreset{})
}

func LoadCatalogSet() (*CatalogSet, error) {
	catalogOnce.Do(func() {
		rawFiles := map[string]json.RawMessage{}
		set := &CatalogSet{
			Raw:               rawFiles,
			Engines:           map[string]struct{}{},
			Hardware:          map[string]struct{}{},
			HardwareAliases:   map[string]string{},
			HardwareProviders: map[string][]string{},
			Models:            map[string]map[string]string{},
			Workloads:         map[string]struct{}{},
			BenchmarkProfiles: map[string]string{},
		}

		type catalogBundle struct {
			Engines           engineCatalog           `json:"engines"`
			Hardware          hardwareCatalog         `json:"hardware"`
			Models            modelCatalog            `json:"models"`
			Workloads         workloadCatalog         `json:"workloads"`
			BenchmarkProfiles benchmarkProfileCatalog `json:"benchmark_profiles"`
		}

		raw, err := embeddedCatalogFS.ReadFile("data/catalog_bundle.json")
		if err != nil {
			cachedCatalogErr = err
			return
		}
		rawFiles["catalog_bundle.json"] = slices.Clone(raw)
		var bundle catalogBundle
		if err := json.Unmarshal(raw, &bundle); err != nil {
			cachedCatalogErr = fmt.Errorf("parse catalog bundle: %w", err)
			return
		}
		if cachedCatalogErr != nil {
			return
		}

		for _, engine := range bundle.Engines.Engines {
			set.Engines[engine.ID] = struct{}{}
		}
		for _, item := range bundle.Hardware.Hardware {
			set.Hardware[item.HardwareID] = struct{}{}
			set.HardwareAliases[item.HardwareID] = item.HardwareID
			for _, alias := range item.Aliases {
				set.HardwareAliases[alias] = item.HardwareID
			}
			for _, selector := range item.ProviderSelectors {
				set.HardwareProviders[item.HardwareID] = append(set.HardwareProviders[item.HardwareID], selector.Provider)
			}
		}
		for _, item := range bundle.Models.Models {
			compat := map[string]string{}
			for engineID, hint := range item.EngineCompatibility {
				compat[engineID] = hint.Status
			}
			set.Models[item.ModelID] = compat
		}
		for _, workload := range bundle.Workloads.Workloads {
			set.Workloads[workload.ID] = struct{}{}
		}
		for _, profile := range bundle.BenchmarkProfiles.BenchmarkProfiles {
			set.BenchmarkProfiles[profile.ID] = profile.ExecutionMode
		}
		cachedCatalog = set
	})
	return cachedCatalog, cachedCatalogErr
}

func CatalogPayload() (map[string]json.RawMessage, error) {
	set, err := LoadCatalogSet()
	if err != nil {
		return nil, err
	}
	return set.Raw, nil
}

func ValidateSuite(raw []byte) (map[string]any, error) {
	set, err := LoadCatalogSet()
	if err != nil {
		return nil, err
	}
	var suite Suite
	if err := json.Unmarshal(raw, &suite); err != nil {
		return nil, err
	}
	type validationRun struct {
		Engine           string `json:"engine"`
		Hardware         string `json:"hardware"`
		GPUCount         int    `json:"gpu_count"`
		Model            string `json:"model"`
		Workload         string `json:"workload"`
		BenchmarkProfile string `json:"benchmark_profile"`
		RuntimePreset    string `json:"runtime_preset"`
		Status           string `json:"status"`
	}
	runPresets := map[string][]string{}
	for _, preset := range suite.RuntimePresets {
		runPresets[preset.ID] = preset.EngineIDs
	}
	if len(runPresets) == 0 {
		runPresets["baseline"] = nil
	}
	statusCounts := map[string]int{}
	runs := make([]validationRun, 0)
	for _, engine := range suite.Matrix.Engines {
		for _, hardware := range suite.Matrix.Hardware {
			for _, gpuCount := range suite.Matrix.GPUCounts {
				for _, model := range suite.Matrix.Models {
					for _, workload := range suite.Matrix.Workloads {
						for _, profile := range suite.Matrix.BenchmarkProfiles {
							for _, preset := range suite.Matrix.RuntimePresets {
								status := "ready"
								if _, ok := set.Engines[engine]; !ok {
									status = "invalid"
								}
								resolvedHardware := set.HardwareAliases[hardware]
								if resolvedHardware == "" {
									status = "invalid"
								}
								if _, ok := set.Workloads[workload]; !ok {
									status = "invalid"
								}
								executionMode := set.BenchmarkProfiles[profile]
								if executionMode == "" {
									status = "invalid"
								}
								if _, ok := set.Models[model]; !ok {
									status = "invalid"
								}
								if allowedEngines, ok := runPresets[preset]; ok && len(allowedEngines) > 0 && !slices.Contains(allowedEngines, engine) {
									status = "unsupported"
								}
								if executionMode == "attach" && suite.AttachTarget == nil {
									status = "invalid"
								}
								if status == "ready" && suite.DefaultProvider != "" {
									if !slices.Contains(set.HardwareProviders[resolvedHardware], suite.DefaultProvider) {
										status = "blocked"
									}
								}
								if status == "ready" {
									compatibility := set.Models[model]
									modelStatus, ok := compatibility[engine]
									if !ok || modelStatus == "unverified" {
										status = "unverified"
									} else if modelStatus == "unsupported" {
										status = "unsupported"
									}
								}
								statusCounts[status]++
								runs = append(runs, validationRun{
									Engine:           engine,
									Hardware:         hardware,
									GPUCount:         gpuCount,
									Model:            model,
									Workload:         workload,
									BenchmarkProfile: profile,
									RuntimePreset:    preset,
									Status:           status,
								})
							}
						}
					}
				}
			}
		}
	}
	return map[string]any{
		"suite_id":      suite.SuiteID,
		"run_count":     len(runs),
		"status_counts": statusCounts,
		"runs":          runs,
	}, nil
}

func CompareResultIndexes(rawIndexes [][]byte, objective string) (map[string]any, error) {
	type warmSummary struct {
		CacheReuseMode        string  `json:"cache_reuse_mode"`
		TTFTP50MS             float64 `json:"ttft_p50_ms"`
		AggregateTotalTokSP50 float64 `json:"aggregate_total_tok_s_p50"`
		TPOTP50MS             float64 `json:"tpot_p50_ms"`
		Failures              int     `json:"failures"`
	}
	type resultRecord struct {
		RunID         string        `json:"run_id"`
		EngineID      string        `json:"engine_id"`
		HardwareID    string        `json:"hardware_id"`
		RuntimePreset string        `json:"runtime_preset_id"`
		WarmSummaries []warmSummary `json:"warm_summaries"`
	}
	type resultIndex struct {
		Results []resultRecord `json:"results"`
	}
	entries := make([]map[string]any, 0)
	for _, raw := range rawIndexes {
		var index resultIndex
		if err := json.Unmarshal(raw, &index); err != nil {
			return nil, err
		}
		for _, record := range index.Results {
			var warm *warmSummary
			for i, summary := range record.WarmSummaries {
				if summary.CacheReuseMode == "affinity" {
					warm = &record.WarmSummaries[i]
					break
				}
			}
			if warm == nil && len(record.WarmSummaries) > 0 {
				warm = &record.WarmSummaries[0]
			}
			score := -1.0
			reason := ""
			if warm != nil {
				switch objective {
				case "max_throughput":
					score = warm.AggregateTotalTokSP50
					reason = "higher aggregate_total_tok_s_p50 is better"
				case "lowest_ttft":
					score = -warm.TTFTP50MS
					reason = "lower ttft_p50_ms is better"
				case "best_tpot":
					score = -warm.TPOTP50MS
					reason = "lower tpot_p50_ms is better"
				default:
					score = (warm.AggregateTotalTokSP50 / 1000.0) - (warm.TTFTP50MS / 1000.0) - (warm.TPOTP50MS / 100.0) - float64(warm.Failures)
					reason = "balanced score favors throughput and penalizes TTFT, TPOT, and failures"
				}
			}
			entries = append(entries, map[string]any{
				"run_id":            record.RunID,
				"engine_id":         record.EngineID,
				"hardware_id":       record.HardwareID,
				"runtime_preset_id": record.RuntimePreset,
				"objective":         objective,
				"score":             score,
				"reason":            reason,
			})
		}
	}
	slices.SortFunc(entries, func(a, b map[string]any) int {
		as, _ := a["score"].(float64)
		bs, _ := b["score"].(float64)
		switch {
		case as > bs:
			return -1
		case as < bs:
			return 1
		default:
			return 0
		}
	})
	return map[string]any{
		"objective": objective,
		"entries":   entries,
	}, nil
}
