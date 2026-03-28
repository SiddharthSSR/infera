"""Tests for eval iteration logging helpers."""

from __future__ import annotations

import sys
from pathlib import Path

from infera_bench.evals import load_eval_history, parse_eval_scores, record_eval_iteration


def test_parse_eval_scores_from_json_payload():
    overall, llm_average = parse_eval_scores('{"scores":{"overall_score":91.4,"llm_average":92.1}}')

    assert overall == 91.4
    assert llm_average == 92.1


def test_parse_eval_scores_from_text_output():
    overall, llm_average = parse_eval_scores("Overall score: 89.5%\nLLM average: 90.75%")

    assert overall == 89.5
    assert llm_average == 90.75


def test_record_eval_iteration_writes_history_and_summary(tmp_path: Path):
    history_path = tmp_path / "history.json"
    summary_path = tmp_path / "summary.md"
    command = [
        sys.executable,
        "-c",
        "print('Overall score: 91.2%\\nLLM average: 92.4%')",
    ]

    history, iteration, exit_code = record_eval_iteration(
        history_path=history_path,
        summary_path=summary_path,
        label="iteration-1",
        eval_command=command,
        cwd=tmp_path,
        change_summary="Tuned workload sampling",
        bottleneck="tail latency on repeated prefix",
        artifact_paths=["/tmp/report.json"],
        remaining_risks=["Needs visual artifact inspection"],
        overall_target=90.0,
        llm_average_target=90.0,
    )

    assert exit_code == 0
    assert iteration.status == "ok"
    assert iteration.overall_score == 91.2
    assert iteration.llm_average_score == 92.4
    assert history_path.exists()
    assert summary_path.exists()
    summary_text = summary_path.read_text(encoding="utf-8")
    assert "Current Best Scores" in summary_text
    assert "Major Iterations" in summary_text
    assert "Remaining Risks Or Weak Spots" in summary_text
    reloaded = load_eval_history(history_path)
    assert len(reloaded.iterations) == 1


def test_record_eval_iteration_returns_threshold_exit_code(tmp_path: Path):
    history_path = tmp_path / "history.json"
    command = [
        sys.executable,
        "-c",
        "print('Overall score: 81.0%\\nLLM average: 84.0%')",
    ]

    _history, iteration, exit_code = record_eval_iteration(
        history_path=history_path,
        summary_path=None,
        label="iteration-2",
        eval_command=command,
        cwd=tmp_path,
        change_summary="Small improvement",
        bottleneck="scores still below target",
        artifact_paths=[],
        remaining_risks=["Overall score below target"],
        overall_target=90.0,
        llm_average_target=90.0,
    )

    assert iteration.status == "ok"
    assert exit_code == 2
