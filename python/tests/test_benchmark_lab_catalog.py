"""Tests for benchmark catalog loading."""

from __future__ import annotations

from pathlib import Path

from infera_bench.catalog import default_catalog_root, load_catalog_bundle, load_suite


def test_load_catalog_bundle_indexes_catalogs() -> None:
    bundle = load_catalog_bundle(default_catalog_root())

    assert "vllm" in bundle.engines
    assert bundle.resolve_hardware("A100_80GB").hardware_id == "a100_80gb"
    assert bundle.resolve_model("Qwen/Qwen2.5-7B-Instruct").family == "qwen2.5"
    assert bundle.resolve_workload("mixed").display_name == "Mixed Workload"
    assert bundle.resolve_benchmark_profile("provision_full").execution_mode == "provision"


def test_load_suite_reads_repo_authored_suite() -> None:
    suite = load_suite(default_catalog_root() / "suites" / "cross_engine_baseline.json")

    assert suite.suite_id == "cross-engine-baseline"
    assert suite.matrix.engines == ["vllm", "sglang", "tensorrt_llm"]
    assert suite.matrix.runtime_presets == ["baseline"]
