"""Tests for benchmark suite matrix expansion."""

from __future__ import annotations

from infera_bench.adapters import build_adapter_registry
from infera_bench.catalog import default_catalog_root, load_catalog_bundle
from infera_bench.matrix import expand_suite
from infera_bench.schema import ExperimentSuite


def test_expand_suite_marks_tensorrt_qwen_as_unverified() -> None:
    bundle = load_catalog_bundle(default_catalog_root())
    suite = ExperimentSuite.model_validate(
        {
            "suite_id": "unit-suite",
            "matrix": {
                "engines": ["vllm", "tensorrt_llm"],
                "hardware": ["a100_80gb"],
                "gpu_counts": [1],
                "models": ["Qwen/Qwen2.5-7B-Instruct"],
                "workloads": ["mixed"],
                "benchmark_profiles": ["provision_full"],
                "runtime_presets": ["baseline"]
            },
            "runtime_presets": [
                {
                    "id": "baseline",
                    "display_name": "Baseline",
                    "parameters": {}
                }
            ]
        }
    )

    runs = expand_suite(suite, bundle, build_adapter_registry(bundle))

    assert len(runs) == 2
    statuses = {run.engine_id: run.compatibility_status for run in runs}
    assert statuses["vllm"] == "ready"
    assert statuses["tensorrt_llm"] == "unverified"


def test_expand_suite_blocks_when_provider_selector_missing() -> None:
    bundle = load_catalog_bundle(default_catalog_root())
    suite = ExperimentSuite.model_validate(
        {
            "suite_id": "provider-blocked",
            "default_provider": "lambda",
            "matrix": {
                "engines": ["vllm"],
                "hardware": ["a100_80gb"],
                "gpu_counts": [1],
                "models": ["Qwen/Qwen2.5-7B-Instruct"],
                "workloads": ["mixed"],
                "benchmark_profiles": ["provision_full"],
                "runtime_presets": ["baseline"]
            }
        }
    )

    runs = expand_suite(suite, bundle, build_adapter_registry(bundle))

    assert len(runs) == 1
    assert runs[0].compatibility_status == "blocked"
