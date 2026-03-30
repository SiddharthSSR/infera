#!/usr/bin/env python3
"""Run one eval iteration, append history, and write a summary."""

from __future__ import annotations

import argparse
from pathlib import Path
import shlex
import sys


REPO_ROOT = Path(__file__).resolve().parents[1]
PYTHON_SRC = REPO_ROOT / "python" / "src"
if str(PYTHON_SRC) not in sys.path:
    sys.path.insert(0, str(PYTHON_SRC))

from infera_bench.lab import BenchmarkLab
from infera_bench.evals import best_scores


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Run a single eval iteration and record the results.")
    parser.add_argument("--history-file", required=True, help="Path to the eval history JSON file")
    parser.add_argument("--summary-file", default="", help="Optional path to a Markdown summary file")
    parser.add_argument("--label", default="", help="Short label for this iteration")
    parser.add_argument("--change-summary", default="", help="Short description of what changed")
    parser.add_argument("--bottleneck", default="", help="Known bottleneck if targets are still below threshold")
    parser.add_argument("--remaining-risk", action="append", default=[], help="Remaining risk or weak spot")
    parser.add_argument("--artifact-path", action="append", default=[], help="Artifact path produced by this iteration")
    parser.add_argument("--overall-target", type=float, default=90.0, help="Target overall score percentage")
    parser.add_argument("--llm-average-target", type=float, default=90.0, help="Target LLM average percentage")
    parser.add_argument("--cwd", default=str(REPO_ROOT), help="Working directory for the eval command")
    parser.add_argument(
        "--eval-command",
        required=True,
        help="Eval command to run. Use shell-style quoting; it will be split with shlex.",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    lab = BenchmarkLab.default()
    history_path = Path(args.history_file).expanduser()
    summary_path = Path(args.summary_file).expanduser() if args.summary_file else None
    eval_command = shlex.split(args.eval_command)
    if not eval_command:
        print("eval command must not be empty", file=sys.stderr)
        return 2

    history, iteration, exit_code = lab.record_eval_iteration(
        history_path=history_path,
        summary_path=summary_path,
        label=args.label,
        eval_command=eval_command,
        cwd=Path(args.cwd).expanduser(),
        change_summary=args.change_summary,
        bottleneck=args.bottleneck,
        artifact_paths=list(args.artifact_path),
        remaining_risks=list(args.remaining_risk),
        overall_target=args.overall_target,
        llm_average_target=args.llm_average_target,
    )
    best = best_scores(history)
    print(
        "[eval-iteration] "
        f"iteration={iteration.label or iteration.iteration_id} "
        f"status={iteration.status} "
        f"overall={iteration.overall_score if iteration.overall_score is not None else 'n/a'} "
        f"llm_average={iteration.llm_average_score if iteration.llm_average_score is not None else 'n/a'} "
        f"best_overall={best['overall_score'] if best['overall_score'] is not None else 'n/a'} "
        f"best_llm_average={best['llm_average_score'] if best['llm_average_score'] is not None else 'n/a'}"
    )
    print(f"[eval-iteration] history={history_path}")
    if summary_path is not None:
        print(f"[eval-iteration] summary={summary_path}")
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
