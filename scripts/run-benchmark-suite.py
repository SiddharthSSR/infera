#!/usr/bin/env python3
"""Run a config-driven benchmark suite for the inference performance lab."""

from __future__ import annotations

import argparse
from pathlib import Path
import sys


REPO_ROOT = Path(__file__).resolve().parents[1]
PYTHON_SRC = REPO_ROOT / "python" / "src"
if str(PYTHON_SRC) not in sys.path:
    sys.path.insert(0, str(PYTHON_SRC))

from infera_bench.catalog import default_catalog_root
from infera_bench.lab import BenchmarkLab, BenchmarkLabPaths


DEFAULT_BASE_URL = "https://inferai.co.in"
DEFAULT_SUITE_FILE = REPO_ROOT / "configs" / "benchmark_lab" / "suites" / "cross_engine_baseline.json"
DEFAULT_WORKLOAD_FILE = REPO_ROOT / "configs" / "benchmark_lab" / "workloads.json"


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Run a generic inference benchmark suite.")
    parser.add_argument("base_url", nargs="?", default=DEFAULT_BASE_URL, help="Gateway base URL")
    parser.add_argument("--api-key", required=True, help="Gateway bearer token")
    parser.add_argument("--suite-file", default=str(DEFAULT_SUITE_FILE), help="Experiment suite JSON file")
    parser.add_argument("--catalog-root", default=str(default_catalog_root()), help="Catalog root directory")
    parser.add_argument("--workload-file", default=str(DEFAULT_WORKLOAD_FILE), help="Workload catalog JSON file")
    parser.add_argument("--python-bin", default=sys.executable, help="Python executable for helper scripts")
    parser.add_argument("--cost-per-hour", type=float, default=None, help="Optional hourly infra cost")
    parser.add_argument("--health-insecure", action="store_true", help="Disable TLS verification for worker health polling")
    parser.add_argument("--quiet-progress", action="store_true", help="Suppress helper progress logs")
    parser.add_argument("--terminate-final-instance", action="store_true", help="Terminate retained lifecycle instances")
    parser.add_argument("--dry-run", action="store_true", help="Write manifests without executing helper scripts")
    parser.add_argument("--continue-on-error", action="store_true", help="Continue remaining runs after failures")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    lab = BenchmarkLab(
        BenchmarkLabPaths(
            catalog_root=Path(args.catalog_root).expanduser(),
            workload_file=Path(args.workload_file).expanduser(),
        )
    )
    suite = lab.load_suite(Path(args.suite_file))
    index, artifacts, execution_results = lab.execute_suite(
        base_url=args.base_url,
        api_key=args.api_key,
        suite=suite,
        python_bin=args.python_bin,
        cost_per_hour=args.cost_per_hour,
        health_insecure=args.health_insecure,
        quiet_progress=args.quiet_progress,
        terminate_final_instance=args.terminate_final_instance,
        dry_run=args.dry_run,
        continue_on_error=args.continue_on_error,
    )
    print(f"[benchmark-suite] result_index={artifacts['json']}")
    print(f"[benchmark-suite] summary_csv={artifacts['csv']}")
    print(f"[benchmark-suite] summary_markdown={artifacts['markdown']}")
    if any(result.status == "failed" for result in execution_results):
        return 1
    if any(result.status == "blocked" for result in execution_results):
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
