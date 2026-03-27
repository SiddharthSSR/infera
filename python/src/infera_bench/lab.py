"""High-level facade for the benchmark lab module.

This file is the preferred integration boundary for callers outside the
benchmark framework. It keeps scripts and future services insulated from the
internal package layout so the lab can be extracted later with minimal churn.
"""

from __future__ import annotations

from dataclasses import dataclass
import json
from pathlib import Path
import sys

from .adapters import build_adapter_registry
from .catalog import (
    BenchmarkCatalogBundle,
    default_catalog_root,
    load_catalog_bundle,
    load_suite,
)
from .matrix import expand_suite
from .orchestration import execute_suite
from .results import compare_result_indexes
from .schema import ExperimentResultIndex, ExperimentSuite, ResultComparison


@dataclass(frozen=True)
class BenchmarkLabPaths:
    catalog_root: Path
    workload_file: Path


class BenchmarkLab:
    """Facade over catalog, validation, execution, and comparison helpers."""

    def __init__(self, paths: BenchmarkLabPaths):
        self.paths = paths
        self._catalog: BenchmarkCatalogBundle | None = None

    @classmethod
    def default(cls) -> "BenchmarkLab":
        root = default_catalog_root()
        return cls(
            BenchmarkLabPaths(
                catalog_root=root,
                workload_file=root / "workloads.json",
            )
        )

    def load_catalog(self) -> BenchmarkCatalogBundle:
        if self._catalog is None:
            self._catalog = load_catalog_bundle(self.paths.catalog_root)
        return self._catalog

    def load_suite(self, path: Path) -> ExperimentSuite:
        return load_suite(path)

    def validate_suite(self, suite: ExperimentSuite) -> dict[str, object]:
        catalog = self.load_catalog()
        runs = expand_suite(suite, catalog, build_adapter_registry(catalog))
        return {
            "suite_id": suite.suite_id,
            "run_count": len(runs),
            "status_counts": {
                status: sum(1 for run in runs if run.compatibility_status == status)
                for status in ("ready", "unverified", "blocked", "unsupported", "invalid")
            },
            "runs": [run.model_dump() for run in runs],
        }

    def execute_suite(
        self,
        *,
        base_url: str,
        api_key: str,
        suite: ExperimentSuite,
        python_bin: str | None = None,
        cost_per_hour: float | None = None,
        health_insecure: bool = False,
        quiet_progress: bool = False,
        terminate_final_instance: bool = False,
        dry_run: bool = False,
        continue_on_error: bool = False,
    ):
        return execute_suite(
            base_url=base_url,
            api_key=api_key,
            suite=suite,
            catalog=self.load_catalog(),
            workload_file=self.paths.workload_file,
            python_bin=python_bin or sys.executable,
            cost_per_hour=cost_per_hour,
            health_insecure=health_insecure,
            quiet_progress=quiet_progress,
            terminate_final_instance=terminate_final_instance,
            dry_run=dry_run,
            continue_on_error=continue_on_error,
        )

    def load_result_indexes(self, paths: list[Path]) -> list[ExperimentResultIndex]:
        return [
            ExperimentResultIndex.model_validate(json.loads(path.expanduser().read_text(encoding="utf-8")))
            for path in paths
        ]

    def compare_indexes(self, indexes: list[ExperimentResultIndex], objective: str) -> ResultComparison:
        return compare_result_indexes(indexes, objective)
