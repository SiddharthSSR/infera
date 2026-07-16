"""Result summarization, serialization, and comparison for benchmark runs."""

from __future__ import annotations

import csv
import json
import statistics
from pathlib import Path
from typing import Any

from .schema import (
    ExperimentExecutionResult,
    ExperimentResultIndex,
    ExperimentResultRecord,
    LifecycleMetricSummary,
    ResultComparison,
    ResultComparisonEntry,
    WarmMetricSummary,
    utc_now_iso,
)


def _load_json(path: Path) -> dict[str, Any]:
    return json.loads(path.read_text(encoding="utf-8"))


def median(values: list[float]) -> float:
    return statistics.median(values) if values else 0.0


def pct(values: list[float], percentile: float) -> float:
    if not values:
        return 0.0
    sorted_values = sorted(values)
    index = int(round((len(sorted_values) - 1) * percentile))
    return sorted_values[index]


def _choose_workload_rows(payload: dict[str, Any]) -> tuple[str, list[dict[str, Any]]]:
    presets = payload.get("presets") or {}
    if not presets:
        return "", []
    if len(presets) == 1:
        name, rows = next(iter(presets.items()))
        return str(name), list(rows or [])
    name = sorted(presets)[0]
    return str(name), list(presets.get(name) or [])


def summarize_warm_output(path: Path, cache_reuse_mode: str) -> WarmMetricSummary:
    payload = _load_json(path)
    workload, rows = _choose_workload_rows(payload)
    ttft_values = [float(row.get("ttft_ms", 0.0)) for row in rows]
    stream_totals = [float(row.get("stream_total_ms", 0.0)) for row in rows]
    non_stream_totals = [float(row.get("non_stream_total_ms", 0.0)) for row in rows]
    decode_values = [
        float(row.get("decode_tok_s", 0.0))
        for row in rows
        if float(row.get("decode_tok_s", 0.0)) > 0
    ]
    aggregate_decode_values = [
        float(row.get("aggregate_decode_tok_s", 0.0))
        for row in rows
        if float(row.get("aggregate_decode_tok_s", 0.0)) > 0
    ]
    aggregate_total_values = [
        float(row.get("aggregate_total_tok_s", 0.0))
        for row in rows
        if float(row.get("aggregate_total_tok_s", 0.0)) > 0
    ]
    tpot_values = [
        float(row.get("tpot_ms", 0.0)) for row in rows if float(row.get("tpot_ms", 0.0)) > 0
    ]
    itl_values = [
        float(row.get("itl_ms", 0.0)) for row in rows if float(row.get("itl_ms", 0.0)) > 0
    ]
    failures = sum(1 for row in rows if row.get("status") == "failed")
    health_sampling = payload.get("health_sampling") or {}
    request_throughput_rps = 0.0
    if rows:
        total_requests = len(rows)
        group_window_ms = max((max(non_stream_totals) if non_stream_totals else 0.0), 1.0)
        request_throughput_rps = total_requests / (group_window_ms / 1000.0)
    return WarmMetricSummary(
        cache_reuse_mode=cache_reuse_mode,  # type: ignore[arg-type]
        workload=workload,
        ttft_p50_ms=median(ttft_values),
        ttft_p95_ms=pct(ttft_values, 0.95),
        stream_total_p50_ms=median(stream_totals),
        non_stream_total_p50_ms=median(non_stream_totals),
        decode_tok_s_p50=median(decode_values),
        aggregate_decode_tok_s_p50=median(aggregate_decode_values),
        aggregate_total_tok_s_p50=median(aggregate_total_values),
        tpot_p50_ms=median(tpot_values),
        itl_p50_ms=median(itl_values),
        request_throughput_rps=request_throughput_rps,
        peak_memory_used_bytes=int(health_sampling.get("peak_memory_used_bytes") or 0),
        health_sample_count=int(health_sampling.get("sample_count") or 0),
        failures=failures,
        source_path=str(path),
    )


def summarize_lifecycle_output(path: Path, step_name: str) -> LifecycleMetricSummary:
    payload = _load_json(path)
    summary: dict[str, Any] = {}
    if step_name == "cold_start":
        scenarios = list(payload.get("scenarios") or [])
        if scenarios:
            scenario = scenarios[-1]
            durations = scenario.get("durations_ms") or {}
            summary = {
                "request_to_running_ms": durations.get("request_to_running_ms"),
                "request_to_server_started_ms": durations.get("request_to_server_started_ms"),
                "server_to_model_ready_ms": durations.get("server_to_model_ready_ms"),
                "request_to_registered_ms": durations.get("request_to_registered_ms"),
                "request_to_first_success_ms": durations.get("request_to_first_success_ms"),
            }
    elif step_name == "startup_health":
        captures = list(payload.get("captures") or [])
        if captures:
            capture = captures[-1]
            summary = {
                "t1_instance_running": capture.get("t1_instance_running"),
                "t2_server_started": capture.get("t2_server_started"),
                "t3_model_load_finished": capture.get("t3_model_load_finished"),
            }
    return LifecycleMetricSummary(stage=step_name, summary=summary, source_path=str(path))


def build_result_record(execution: ExperimentExecutionResult) -> ExperimentResultRecord:
    warm_summaries: list[WarmMetricSummary] = []
    lifecycle_summaries: list[LifecycleMetricSummary] = []
    for step in execution.steps:
        if step.status != "ok":
            continue
        output_path = Path(step.output_path).expanduser()
        if not output_path.exists():
            continue
        if step.category == "warm":
            cache_reuse_mode = "affinity" if step.name == "warm_affinity" else "none"
            warm_summaries.append(summarize_warm_output(output_path, cache_reuse_mode))
        elif step.category in {"cold_start", "startup_health"}:
            lifecycle_summaries.append(summarize_lifecycle_output(output_path, step.name))
    return ExperimentResultRecord(
        run_id=execution.run_spec.run_id,
        suite_id=execution.run_spec.suite_id,
        status=execution.status,
        compatibility_status=execution.run_spec.compatibility_status,
        engine_id=execution.run_spec.engine_id,
        hardware_id=execution.run_spec.hardware_id,
        gpu_count=execution.run_spec.gpu_count,
        model_id=execution.run_spec.model_id,
        workload_id=execution.run_spec.workload_id,
        benchmark_profile_id=execution.run_spec.benchmark_profile_id,
        runtime_preset_id=execution.run_spec.runtime_preset_id,
        runtime_options=execution.run_spec.runtime_options,
        generic_parameters=execution.run_spec.generic_parameters,
        notes=list(execution.notes),
        warm_summaries=warm_summaries,
        lifecycle_summaries=lifecycle_summaries,
        manifest_path=execution.manifest_path,
    )


def build_result_index(
    *,
    suite_id: str,
    catalog_root: Path,
    execution_results: list[ExperimentExecutionResult],
) -> ExperimentResultIndex:
    results: list[ExperimentResultRecord] = []
    blocked: list[ExperimentResultRecord] = []
    skipped: list[ExperimentResultRecord] = []
    for execution in execution_results:
        record = build_result_record(execution)
        if execution.status == "blocked":
            blocked.append(record)
        elif execution.status == "skipped":
            skipped.append(record)
        else:
            results.append(record)
    return ExperimentResultIndex(
        generated_at=utc_now_iso(),
        suite_id=suite_id,
        catalog_root=str(catalog_root),
        results=results,
        blocked=blocked,
        skipped=skipped,
    )


def _record_score(record: ExperimentResultRecord, objective: str) -> tuple[float, str]:
    warm = next(
        (item for item in record.warm_summaries if item.cache_reuse_mode == "affinity"), None
    )
    if warm is None:
        warm = record.warm_summaries[0] if record.warm_summaries else None
    if warm is None:
        return float("-inf"), "no warm summary"
    if objective == "max_throughput":
        return warm.aggregate_total_tok_s_p50, "higher aggregate_total_tok_s_p50 is better"
    if objective == "lowest_ttft":
        return -warm.ttft_p50_ms, "lower ttft_p50_ms is better"
    if objective == "best_tpot":
        return -warm.tpot_p50_ms if warm.tpot_p50_ms > 0 else float(
            "-inf"
        ), "lower tpot_p50_ms is better"
    # balanced
    score = (
        (warm.aggregate_total_tok_s_p50 / 1000.0)
        - (warm.ttft_p50_ms / 1000.0)
        - (warm.tpot_p50_ms / 100.0 if warm.tpot_p50_ms > 0 else 0.0)
        - float(warm.failures)
    )
    return score, "balanced score favors throughput and penalizes TTFT, TPOT, and failures"


def compare_result_indexes(
    indexes: list[ExperimentResultIndex], objective: str
) -> ResultComparison:
    entries: list[ResultComparisonEntry] = []
    for index in indexes:
        for record in index.results:
            score, reason = _record_score(record, objective)
            entries.append(
                ResultComparisonEntry(
                    run_id=record.run_id,
                    objective=objective,  # type: ignore[arg-type]
                    score=score,
                    reason=reason,
                    record=record,
                )
            )
    entries.sort(key=lambda item: item.score, reverse=True)
    return ResultComparison(generated_at=utc_now_iso(), objective=objective, entries=entries)


def _comparison_winner_groups(
    comparison: ResultComparison,
) -> dict[tuple[str, str, str], ResultComparisonEntry]:
    winners: dict[tuple[str, str, str], ResultComparisonEntry] = {}
    for entry in comparison.entries:
        key = (
            entry.record.model_id,
            entry.record.hardware_id,
            entry.record.workload_id,
        )
        winners.setdefault(key, entry)
    return winners


def format_comparison_markdown(comparison: ResultComparison, *, top_k: int = 10) -> str:
    lines = [
        f"# Benchmark Comparison: {comparison.objective}",
        "",
        f"Generated: {comparison.generated_at}",
        "",
        "## Overall Ranking",
        "",
    ]
    if not comparison.entries:
        lines.append("- no comparable results")
    for idx, entry in enumerate(comparison.entries[:top_k], start=1):
        record = entry.record
        lines.append(
            f"{idx}. `{record.run_id}` "
            f"(engine={record.engine_id}, hardware={record.hardware_id}, model={record.model_id}, "
            f"workload={record.workload_id}, preset={record.runtime_preset_id}) "
            f"score={entry.score:.4f}"
        )
    lines.extend(["", "## Winners By Model / Hardware / Workload", ""])
    winners = _comparison_winner_groups(comparison)
    if not winners:
        lines.append("- no winners available")
    for (model_id, hardware_id, workload_id), entry in sorted(winners.items()):
        record = entry.record
        lines.append(
            f"- model={model_id} hardware={hardware_id} workload={workload_id}: "
            f"`{record.run_id}` (engine={record.engine_id}, preset={record.runtime_preset_id}, score={entry.score:.4f})"
        )
    return "\n".join(lines) + "\n"


def write_comparison_markdown(comparison: ResultComparison, path: Path, *, top_k: int = 10) -> Path:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(format_comparison_markdown(comparison, top_k=top_k), encoding="utf-8")
    return path


def write_result_artifacts(index: ExperimentResultIndex, output_root: Path) -> dict[str, Path]:
    output_root.mkdir(parents=True, exist_ok=True)
    json_path = output_root / f"{index.suite_id}-result-index.json"
    csv_path = output_root / f"{index.suite_id}-summary.csv"
    markdown_path = output_root / f"{index.suite_id}-summary.md"
    json_path.write_text(index.model_dump_json(indent=2) + "\n", encoding="utf-8")

    with csv_path.open("w", encoding="utf-8", newline="") as handle:
        writer = csv.DictWriter(
            handle,
            fieldnames=[
                "run_id",
                "engine_id",
                "hardware_id",
                "gpu_count",
                "model_id",
                "workload_id",
                "benchmark_profile_id",
                "runtime_preset_id",
                "compatibility_status",
                "status",
                "cache_reuse_mode",
                "ttft_p50_ms",
                "ttft_p95_ms",
                "stream_total_p50_ms",
                "aggregate_decode_tok_s_p50",
                "aggregate_total_tok_s_p50",
                "tpot_p50_ms",
                "itl_p50_ms",
                "request_throughput_rps",
                "peak_memory_used_bytes",
            ],
        )
        writer.writeheader()
        for record in index.results:
            if not record.warm_summaries:
                writer.writerow(
                    {
                        "run_id": record.run_id,
                        "engine_id": record.engine_id,
                        "hardware_id": record.hardware_id,
                        "gpu_count": record.gpu_count,
                        "model_id": record.model_id,
                        "workload_id": record.workload_id,
                        "benchmark_profile_id": record.benchmark_profile_id,
                        "runtime_preset_id": record.runtime_preset_id,
                        "compatibility_status": record.compatibility_status,
                        "status": record.status,
                    }
                )
                continue
            for warm in record.warm_summaries:
                writer.writerow(
                    {
                        "run_id": record.run_id,
                        "engine_id": record.engine_id,
                        "hardware_id": record.hardware_id,
                        "gpu_count": record.gpu_count,
                        "model_id": record.model_id,
                        "workload_id": record.workload_id,
                        "benchmark_profile_id": record.benchmark_profile_id,
                        "runtime_preset_id": record.runtime_preset_id,
                        "compatibility_status": record.compatibility_status,
                        "status": record.status,
                        "cache_reuse_mode": warm.cache_reuse_mode,
                        "ttft_p50_ms": warm.ttft_p50_ms,
                        "ttft_p95_ms": warm.ttft_p95_ms,
                        "stream_total_p50_ms": warm.stream_total_p50_ms,
                        "aggregate_decode_tok_s_p50": warm.aggregate_decode_tok_s_p50,
                        "aggregate_total_tok_s_p50": warm.aggregate_total_tok_s_p50,
                        "tpot_p50_ms": warm.tpot_p50_ms,
                        "itl_p50_ms": warm.itl_p50_ms,
                        "request_throughput_rps": warm.request_throughput_rps,
                        "peak_memory_used_bytes": warm.peak_memory_used_bytes,
                    }
                )

    lines = [f"# Benchmark Suite: {index.suite_id}", "", f"Generated: {index.generated_at}", ""]
    for record in index.results:
        lines.append(f"## {record.run_id}")
        lines.append(
            f"- engine={record.engine_id} hardware={record.hardware_id} gpu_count={record.gpu_count} "
            f"model={record.model_id} workload={record.workload_id} preset={record.runtime_preset_id}"
        )
        lines.append(f"- status={record.status} compatibility={record.compatibility_status}")
        for warm in record.warm_summaries:
            lines.append(
                f"- warm[{warm.cache_reuse_mode}] ttft_p50={warm.ttft_p50_ms:.1f}ms "
                f"aggregate_total_tok_s_p50={warm.aggregate_total_tok_s_p50:.2f} "
                f"peak_memory_used_bytes={warm.peak_memory_used_bytes}"
            )
        lines.append("")
    markdown_path.write_text("\n".join(lines).strip() + "\n", encoding="utf-8")
    return {"json": json_path, "csv": csv_path, "markdown": markdown_path}
