#!/usr/bin/env python3
"""Compare one or more benchmark result indexes by objective."""

from __future__ import annotations

import argparse
import json
from pathlib import Path
import sys


REPO_ROOT = Path(__file__).resolve().parents[1]
PYTHON_SRC = REPO_ROOT / "python" / "src"
if str(PYTHON_SRC) not in sys.path:
    sys.path.insert(0, str(PYTHON_SRC))

from infera_bench.lab import BenchmarkLab


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Compare benchmark result indexes.")
    parser.add_argument("indexes", nargs="+", help="Result index JSON files")
    parser.add_argument(
        "--objective",
        choices=["max_throughput", "lowest_ttft", "best_tpot", "balanced"],
        default="balanced",
        help="Ranking objective",
    )
    parser.add_argument("--json-output", default="", help="Optional JSON output path")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    lab = BenchmarkLab.default()
    indexes = lab.load_result_indexes([Path(path) for path in args.indexes])
    comparison = lab.compare_indexes(indexes, args.objective)
    payload = comparison.model_dump()
    if args.json_output:
        output_path = Path(args.json_output).expanduser()
        output_path.parent.mkdir(parents=True, exist_ok=True)
        output_path.write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")
    else:
        print(json.dumps(payload, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
