"""Tests for the benchmark lab facade."""

from __future__ import annotations

from infera_bench.catalog import default_catalog_root
from infera_bench.lab import BenchmarkLab


def test_default_lab_uses_repo_catalog_root():
    lab = BenchmarkLab.default()

    assert lab.paths.catalog_root == default_catalog_root()
    assert lab.paths.workload_file == default_catalog_root() / "workloads.json"


def test_validate_suite_uses_single_facade_boundary(tmp_path):
    lab = BenchmarkLab.default()
    suite = lab.load_suite(default_catalog_root() / "suites" / "cross_engine_baseline.json")

    payload = lab.validate_suite(suite)

    assert payload["suite_id"] == "cross-engine-baseline"
    assert payload["run_count"] > 0
    assert "status_counts" in payload
