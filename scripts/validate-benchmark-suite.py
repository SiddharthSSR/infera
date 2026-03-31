#!/usr/bin/env python3
"""Validate and expand a generic benchmark suite without executing it."""

from __future__ import annotations

import argparse
import json
from pathlib import Path
import sys


REPO_ROOT = Path(__file__).resolve().parents[1]
PYTHON_SRC = REPO_ROOT / "python" / "src"
if str(PYTHON_SRC) not in sys.path:
    sys.path.insert(0, str(PYTHON_SRC))

from infera_bench.catalog import default_catalog_root
from infera_bench.lab import BenchmarkLab, BenchmarkLabPaths


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Validate a benchmark suite.")
    parser.add_argument("--suite-file", required=True, help="Experiment suite JSON file")
    parser.add_argument("--catalog-root", default=str(default_catalog_root()), help="Catalog root directory")
    parser.add_argument("--json-output", default="", help="Optional JSON output path")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    lab = BenchmarkLab(
        BenchmarkLabPaths(
            catalog_root=Path(args.catalog_root).expanduser(),
            workload_file=Path(args.catalog_root).expanduser() / "workloads.json",
        )
    )
    suite = lab.load_suite(Path(args.suite_file))
    payload = lab.validate_suite(suite)
    if args.json_output:
        path = Path(args.json_output).expanduser()
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")
    else:
        print(json.dumps(payload, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
