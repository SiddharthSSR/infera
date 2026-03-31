"""Eval iteration logging and summary helpers for optimization loops."""

from __future__ import annotations

import json
from pathlib import Path
import re
import shlex
import subprocess
import uuid
from typing import Any

from .schema import EvalHistory, EvalIteration, utc_now_iso


OVERALL_KEY_CANDIDATES = (
    "overall_score",
    "overall",
    "overallscore",
    "score_overall",
)
LLM_AVERAGE_KEY_CANDIDATES = (
    "llm_average",
    "llm_average_score",
    "llmaverage",
    "llm_avg",
    "llmavg",
)

OVERALL_PATTERNS = (
    re.compile(r"overall(?:\s+score)?\s*[:=]\s*([0-9]+(?:\.[0-9]+)?)%?", re.IGNORECASE),
    re.compile(r"overall(?:\s+score)?\s+([0-9]+(?:\.[0-9]+)?)%?", re.IGNORECASE),
)
LLM_AVERAGE_PATTERNS = (
    re.compile(r"llm(?:\s+average|\s+avg)?(?:\s+score)?\s*[:=]\s*([0-9]+(?:\.[0-9]+)?)%?", re.IGNORECASE),
    re.compile(r"llm(?:\s+average|\s+avg)?(?:\s+score)?\s+([0-9]+(?:\.[0-9]+)?)%?", re.IGNORECASE),
)


def _normalize_key(value: str) -> str:
    return re.sub(r"[^a-z0-9]+", "", value.lower())


def _find_nested_score(payload: Any, candidates: tuple[str, ...]) -> float | None:
    normalized_candidates = {_normalize_key(candidate) for candidate in candidates}
    if isinstance(payload, dict):
        for key, value in payload.items():
            if _normalize_key(str(key)) in normalized_candidates:
                try:
                    return float(value)
                except (TypeError, ValueError):
                    pass
            nested = _find_nested_score(value, candidates)
            if nested is not None:
                return nested
    elif isinstance(payload, list):
        for item in payload:
            nested = _find_nested_score(item, candidates)
            if nested is not None:
                return nested
    return None


def parse_eval_scores(text: str) -> tuple[float | None, float | None]:
    stripped = text.strip()
    if stripped:
        try:
            payload = json.loads(stripped)
        except json.JSONDecodeError:
            payload = None
        if payload is not None:
            overall = _find_nested_score(payload, OVERALL_KEY_CANDIDATES)
            llm_average = _find_nested_score(payload, LLM_AVERAGE_KEY_CANDIDATES)
            if overall is not None or llm_average is not None:
                return overall, llm_average

    overall_score: float | None = None
    llm_average_score: float | None = None
    for pattern in OVERALL_PATTERNS:
        match = pattern.search(text)
        if match:
            overall_score = float(match.group(1))
            break
    for pattern in LLM_AVERAGE_PATTERNS:
        match = pattern.search(text)
        if match:
            llm_average_score = float(match.group(1))
            break
    return overall_score, llm_average_score


def load_eval_history(path: Path, *, history_id: str | None = None) -> EvalHistory:
    if path.exists():
        return EvalHistory.model_validate_json(path.read_text(encoding="utf-8"))
    return EvalHistory(history_id=history_id or path.stem or "eval-history", generated_at=utc_now_iso())


def write_eval_history(path: Path, history: EvalHistory) -> Path:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(history.model_dump_json(indent=2) + "\n", encoding="utf-8")
    return path


def best_scores(history: EvalHistory) -> dict[str, float | None]:
    overall_values = [item.overall_score for item in history.iterations if item.overall_score is not None]
    llm_values = [item.llm_average_score for item in history.iterations if item.llm_average_score is not None]
    return {
        "overall_score": max(overall_values) if overall_values else None,
        "llm_average_score": max(llm_values) if llm_values else None,
    }


def summarize_eval_history(history: EvalHistory) -> str:
    best = best_scores(history)
    lines = [
        "# Eval Iteration Summary",
        "",
        "## Current Best Scores",
        "",
        f"- overall score: {best['overall_score']:.2f}%" if best["overall_score"] is not None else "- overall score: unavailable",
        (
            f"- LLM average: {best['llm_average_score']:.2f}%"
            if best["llm_average_score"] is not None
            else "- LLM average: unavailable"
        ),
        "",
        "## Major Iterations",
        "",
    ]
    if not history.iterations:
        lines.append("- no eval iterations recorded yet")
    for item in history.iterations:
        label = item.label or item.iteration_id
        lines.append(
            "- "
            f"{label}: status={item.status}, overall={item.overall_score if item.overall_score is not None else 'n/a'}%, "
            f"llm_average={item.llm_average_score if item.llm_average_score is not None else 'n/a'}%, "
            f"change={item.change_summary or 'n/a'}"
        )
        if item.bottleneck:
            lines.append(f"  bottleneck: {item.bottleneck}")
    lines.extend(["", "## Remaining Risks Or Weak Spots", ""])
    risks: list[str] = []
    if history.iterations:
        latest_risks = history.iterations[-1].remaining_risks
        risks = latest_risks or []
    if not risks:
        seen: set[str] = set()
        for item in history.iterations:
            for risk in item.remaining_risks:
                if risk not in seen:
                    risks.append(risk)
                    seen.add(risk)
    if not risks:
        lines.append("- none recorded")
    else:
        for risk in risks:
            lines.append(f"- {risk}")
    return "\n".join(lines) + "\n"


def write_eval_summary(path: Path, history: EvalHistory) -> Path:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(summarize_eval_history(history), encoding="utf-8")
    return path


def record_eval_iteration(
    *,
    history_path: Path,
    summary_path: Path | None,
    label: str,
    eval_command: list[str],
    cwd: Path,
    change_summary: str,
    bottleneck: str,
    artifact_paths: list[str],
    remaining_risks: list[str],
    overall_target: float,
    llm_average_target: float,
) -> tuple[EvalHistory, EvalIteration, int]:
    completed = subprocess.run(eval_command, cwd=str(cwd), capture_output=True, text=True, check=False)
    combined_output = (completed.stdout or "") + ("\n" if completed.stdout and completed.stderr else "") + (completed.stderr or "")
    overall_score, llm_average_score = parse_eval_scores(combined_output)

    history = load_eval_history(history_path)
    iteration_id = uuid.uuid4().hex[:12]
    raw_output_path = history_path.parent / f"{history.history_id}-{iteration_id}-raw.txt"
    raw_output_path.write_text(combined_output, encoding="utf-8")
    status = "ok"
    if completed.returncode != 0:
        status = "failed"
    elif overall_score is None or llm_average_score is None:
        status = "parse_failed"

    iteration = EvalIteration(
        iteration_id=iteration_id,
        generated_at=utc_now_iso(),
        label=label,
        eval_command=eval_command,
        eval_command_display=shlex.join(eval_command),
        cwd=str(cwd),
        status=status,
        change_summary=change_summary,
        bottleneck=bottleneck,
        overall_score=overall_score,
        llm_average_score=llm_average_score,
        overall_target=overall_target,
        llm_average_target=llm_average_target,
        raw_output_path=str(raw_output_path),
        artifact_paths=artifact_paths,
        remaining_risks=remaining_risks,
    )
    history.iterations.append(iteration)
    history.generated_at = utc_now_iso()
    write_eval_history(history_path, history)
    if summary_path is not None:
        write_eval_summary(summary_path, history)

    exit_code = completed.returncode
    if exit_code == 0:
        if status != "ok":
            exit_code = 1
        elif (overall_score or 0.0) < overall_target or (llm_average_score or 0.0) < llm_average_target:
            exit_code = 2
    return history, iteration, exit_code
