#!/usr/bin/env python3
"""Summarize untuned Phase 1 benchmark outputs across inference engines."""

from __future__ import annotations

import argparse
from datetime import datetime, timezone
import json
from pathlib import Path
import statistics
import sys
from typing import Any


DEFAULT_INPUT_ROOT = Path("/tmp/infera-engine-benchmarks")
SUPPORTED_ENGINES = ("vllm", "sglang", "tensorrt_llm")
EXPECTED_PHASE1_STEPS = ("warm_none", "warm_affinity", "cold_start", "startup_health")
SCENARIO_ORDER = {
    "fresh_provision": 0,
    "stopped_instance_start": 1,
    "stopped_instance_reuse": 2,
}
CAPTURE_ORDER = {
    "fresh_provision": 0,
    "stopped_instance_start": 1,
}
CACHE_REUSE_ORDER = {
    "none": 0,
    "affinity": 1,
}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Render a comparable untuned Phase 1 baseline from per-engine benchmark artifacts.",
    )
    parser.add_argument(
        "inputs",
        nargs="*",
        default=[str(DEFAULT_INPUT_ROOT)],
        help=(
            "Manifest paths or directories to scan for phase1-*-manifest.json files "
            f"(default: {DEFAULT_INPUT_ROOT})"
        ),
    )
    parser.add_argument(
        "--engine",
        action="append",
        choices=SUPPORTED_ENGINES,
        default=[],
        help="Limit the report to one or more engines. Defaults to all supported engines.",
    )
    parser.add_argument(
        "--markdown-output",
        default=None,
        help="Optional path to write the rendered Markdown report.",
    )
    parser.add_argument(
        "--json-output",
        default=None,
        help="Optional path to write the structured summary JSON.",
    )
    parser.add_argument(
        "--blocked-engine",
        action="append",
        default=[],
        help="Mark an engine as blocked in ENGINE=reason form. Can be repeated.",
    )
    return parser.parse_args()


def now_iso() -> str:
    return datetime.now(timezone.utc).isoformat()


def load_json(path: Path) -> dict[str, Any]:
    return json.loads(path.read_text(encoding="utf-8"))


def parse_blocked_engines(values: list[str]) -> dict[str, str]:
    blocked: dict[str, str] = {}
    for raw_value in values:
        entry = str(raw_value).strip()
        if not entry:
            continue
        engine, separator, reason = entry.partition("=")
        engine = engine.strip()
        reason = reason.strip()
        if separator == "" or engine not in SUPPORTED_ENGINES or not reason:
            raise ValueError(f"blocked engine must be ENGINE=reason with a supported engine, got: {raw_value}")
        blocked[engine] = reason
    return blocked


def discover_manifest_paths(inputs: list[str]) -> list[Path]:
    manifest_paths: list[Path] = []
    for raw_input in inputs:
        path = Path(raw_input).expanduser()
        if not path.exists():
            raise FileNotFoundError(f"input path does not exist: {path}")
        if path.is_file():
            manifest_paths.append(path.resolve())
            continue
        manifest_paths.extend(sorted(candidate.resolve() for candidate in path.rglob("phase1-*-manifest.json")))
    unique_paths: list[Path] = []
    seen: set[Path] = set()
    for path in manifest_paths:
        if path in seen:
            continue
        seen.add(path)
        unique_paths.append(path)
    return unique_paths


def median(values: list[float]) -> float:
    return statistics.median(values) if values else 0.0


def pct(values: list[float], percentile: float) -> float:
    if not values:
        return 0.0
    sorted_values = sorted(values)
    index = int(round((len(sorted_values) - 1) * percentile))
    return sorted_values[index]


def _group_representatives(rows: list[dict[str, Any]]) -> list[dict[str, Any]]:
    groups: dict[int, dict[str, Any]] = {}
    for row in rows:
        group_run = row.get("group_run", row.get("run", 0))
        groups.setdefault(int(group_run), row)
    return list(groups.values())


def summarize_warm_rows(rows: list[dict[str, Any]]) -> dict[str, float]:
    ttft_values = [float(row["ttft_ms"]) for row in rows]
    stream_total_values = [float(row["stream_total_ms"]) for row in rows]
    non_stream_total_values = [float(row["non_stream_total_ms"]) for row in rows]
    decode_values = [float(row["decode_tok_s"]) for row in rows if float(row["decode_tok_s"]) > 0]
    cost_values = [
        float(row.get("cost_per_request_usd", row.get("cost_query_usd", 0.0)))
        for row in rows
        if float(row.get("cost_per_request_usd", row.get("cost_query_usd", 0.0))) > 0
    ]
    group_rows = _group_representatives(rows)
    aggregate_decode_values = [
        float(row["aggregate_decode_tok_s"])
        for row in group_rows
        if float(row.get("aggregate_decode_tok_s", 0.0)) > 0
    ]
    return {
        "ttft_p50_ms": median(ttft_values),
        "ttft_p95_ms": pct(ttft_values, 0.95),
        "stream_total_p50_ms": median(stream_total_values),
        "non_stream_total_p50_ms": median(non_stream_total_values),
        "decode_tok_s_p50": median(decode_values),
        "aggregate_decode_tok_s_p50": median(aggregate_decode_values),
        "cost_query_usd_p50": median(cost_values),
    }


def subtract_if_present(start: Any, end: Any) -> int | None:
    if start is None or end is None:
        return None
    return int(end) - int(start)


def extract_model_load_metadata(snapshot: dict[str, Any] | None, model_id: str | None) -> dict[str, Any]:
    startup = (snapshot or {}).get("startup") or {}
    metadata = startup.get("metadata") or {}
    model_loads = metadata.get("model_loads") or {}
    if model_id and model_id in model_loads:
        return dict(model_loads[model_id])
    if len(model_loads) == 1:
        return dict(next(iter(model_loads.values())))
    return {}


def infer_engine_init_duration(startup_snapshot: dict[str, Any] | None) -> tuple[str | None, int | None]:
    durations = ((startup_snapshot or {}).get("startup") or {}).get("durations_ms") or {}
    for key in sorted(durations):
        if not key.endswith("_engine_init_finished"):
            continue
        stage_name = key[: -len("_finished")]
        start_key = f"{stage_name}_started"
        finished_value = durations.get(key)
        started_value = durations.get(start_key)
        if finished_value is None:
            continue
        if started_value is None:
            return stage_name, int(finished_value)
        return stage_name, int(finished_value) - int(started_value)
    return None, None


def choose_preset(payload: dict[str, Any], preferred: str | None) -> tuple[str | None, list[dict[str, Any]]]:
    presets = payload.get("presets") or {}
    if preferred and preferred in presets:
        return preferred, list(presets[preferred] or [])
    if not presets:
        return None, []
    preset_name = sorted(presets)[0]
    return preset_name, list(presets[preset_name] or [])


def collect_manifest_candidates(
    manifest_paths: list[Path],
) -> tuple[dict[str, tuple[Path, dict[str, Any]]], list[str]]:
    candidates: dict[str, list[tuple[Path, dict[str, Any]]]] = {}
    gaps: list[str] = []
    for manifest_path in manifest_paths:
        payload = load_json(manifest_path)
        engine = str(payload.get("engine") or "").strip()
        if not engine:
            gaps.append(f"ignored manifest without engine field: {manifest_path}")
            continue
        candidates.setdefault(engine, []).append((manifest_path, payload))

    selected: dict[str, tuple[Path, dict[str, Any]]] = {}
    for engine, items in candidates.items():
        if len(items) == 1:
            selected[engine] = items[0]
            continue
        newest = max(items, key=lambda item: item[0].stat().st_mtime_ns)
        selected[engine] = newest
        ignored = ", ".join(str(path) for path, _payload in sorted(items[:-1], key=lambda item: item[0]))
        gaps.append(f"{engine}: multiple manifests found; using latest {newest[0]} and ignoring {ignored}")
    return selected, gaps


def summarize_engine_manifest(
    manifest_path: Path,
    manifest: dict[str, Any],
) -> tuple[dict[str, Any], list[dict[str, Any]], list[dict[str, Any]], list[dict[str, Any]], list[str]]:
    engine = str(manifest.get("engine") or "")
    provider = str(manifest.get("provider") or "")
    gpu_type = str(manifest.get("gpu_type") or "")
    gpu_count = int(manifest.get("gpu_count") or 0)
    model = str(manifest.get("model") or "")
    preset = str(manifest.get("preset") or "")
    concurrency = int(manifest.get("concurrency") or 0)
    cost_per_hour = manifest.get("cost_per_hour")
    manifest_notes = [str(note) for note in manifest.get("notes") or []]

    step_map = {str(step.get("name")): step for step in manifest.get("steps") or []}
    gaps: list[str] = []
    for step_name in EXPECTED_PHASE1_STEPS:
        if step_name not in step_map:
            gaps.append(f"{engine}: manifest is missing expected step {step_name}")

    warm_rows: list[dict[str, Any]] = []
    cold_rows: list[dict[str, Any]] = []
    startup_rows: list[dict[str, Any]] = []

    for step_name, cache_reuse_mode in (("warm_none", "none"), ("warm_affinity", "affinity")):
        step = step_map.get(step_name)
        if step is None:
            continue
        if step.get("status") != "ok":
            gaps.append(f"{engine}: {step_name} step status={step.get('status')}")
            continue
        output_path = Path(str(step.get("output_path") or "")).expanduser()
        if not output_path.exists():
            gaps.append(f"{engine}: {step_name} output missing at {output_path}")
            continue
        payload = load_json(output_path)
        preset_name, rows = choose_preset(payload, preset)
        if not rows:
            gaps.append(f"{engine}: {step_name} output at {output_path} did not include warm rows")
            continue
        summary = summarize_warm_rows(rows)
        warm_rows.append(
            {
                "engine": engine,
                "provider": str(payload.get("provider") or provider),
                "gpu_type": str(payload.get("gpu_type") or gpu_type),
                "gpu_count": gpu_count,
                "model": str(payload.get("model") or model),
                "workload": preset_name,
                "concurrency": int(payload.get("concurrency") or concurrency),
                "cache_reuse_mode": cache_reuse_mode,
                "ttft_p50_ms": summary["ttft_p50_ms"],
                "ttft_p95_ms": summary["ttft_p95_ms"],
                "stream_total_p50_ms": summary["stream_total_p50_ms"],
                "non_stream_total_p50_ms": summary["non_stream_total_p50_ms"],
                "decode_tok_s_p50": summary["decode_tok_s_p50"],
                "aggregate_decode_tok_s_p50": summary["aggregate_decode_tok_s_p50"],
                "cost_query_usd_p50": summary["cost_query_usd_p50"],
                "rows": len(rows),
                "cost_per_hour": payload.get("cost_per_hour", cost_per_hour),
                "source_path": str(output_path),
            }
        )

    cold_step = step_map.get("cold_start")
    if cold_step is not None:
        if cold_step.get("status") != "ok":
            gaps.append(f"{engine}: cold_start step status={cold_step.get('status')}")
        else:
            output_path = Path(str(cold_step.get("output_path") or "")).expanduser()
            if not output_path.exists():
                gaps.append(f"{engine}: cold_start output missing at {output_path}")
            else:
                payload = load_json(output_path)
                for scenario in payload.get("scenarios") or []:
                    snapshot = scenario.get("health_snapshot") or {}
                    model_metadata = extract_model_load_metadata(snapshot, model)
                    cold_rows.append(
                        {
                            "engine": engine,
                            "provider": str(payload.get("provider") or provider),
                            "gpu_type": str(payload.get("provider_gpu_type_id") or payload.get("gpu_type") or gpu_type),
                            "gpu_count": int(payload.get("gpu_count") or gpu_count),
                            "model": str(payload.get("model") or model),
                            "scenario": str(scenario.get("scenario") or ""),
                            "request_to_running_ms": (scenario.get("durations_ms") or {}).get("request_to_running_ms"),
                            "request_to_server_started_ms": (scenario.get("durations_ms") or {}).get(
                                "request_to_server_started_ms"
                            ),
                            "server_to_model_ready_ms": (scenario.get("durations_ms") or {}).get(
                                "server_to_model_ready_ms"
                            ),
                            "request_to_registered_ms": (scenario.get("durations_ms") or {}).get(
                                "request_to_registered_ms"
                            ),
                            "request_to_first_success_ms": (scenario.get("durations_ms") or {}).get(
                                "request_to_first_success_ms"
                            ),
                            "registered_to_first_success_ms": (scenario.get("durations_ms") or {}).get(
                                "registered_to_first_success_ms"
                            ),
                            "memory_used_bytes": snapshot.get("memory_used_bytes"),
                            "memory_total_bytes": snapshot.get("memory_total_bytes"),
                            "gateway_registered": snapshot.get("gateway_registered"),
                            "hf_cache_exists": model_metadata.get("inferred_hf_repo_cache_exists"),
                            "hf_snapshot_count": model_metadata.get("inferred_hf_snapshot_count"),
                            "source_path": str(output_path),
                            "notes": [str(note) for note in scenario.get("notes") or []],
                        }
                    )

    startup_step = step_map.get("startup_health")
    if startup_step is not None:
        if startup_step.get("status") != "ok":
            gaps.append(f"{engine}: startup_health step status={startup_step.get('status')}")
        else:
            output_path = Path(str(startup_step.get("output_path") or "")).expanduser()
            if not output_path.exists():
                gaps.append(f"{engine}: startup_health output missing at {output_path}")
            else:
                payload = load_json(output_path)
                for capture in payload.get("captures") or []:
                    snapshot = capture.get("health_snapshot") or {}
                    model_metadata = extract_model_load_metadata(snapshot, model)
                    engine_init_stage, engine_init_duration_ms = infer_engine_init_duration(snapshot)
                    startup_durations = ((snapshot.get("startup") or {}).get("durations_ms") or {})
                    startup_rows.append(
                        {
                            "engine": engine,
                            "provider": str(payload.get("provider") or provider),
                            "gpu_type": str(payload.get("gpu_type") or gpu_type),
                            "gpu_count": int(payload.get("gpu_count") or gpu_count),
                            "model": str(payload.get("model") or model),
                            "capture": str(capture.get("label") or ""),
                            "request_to_server_started_ms": subtract_if_present(
                                capture.get("t0_request_sent"),
                                capture.get("t2_server_started"),
                            ),
                            "server_to_model_ready_ms": subtract_if_present(
                                capture.get("t2_server_started"),
                                capture.get("t3_model_load_finished"),
                            ),
                            "request_to_model_ready_ms": subtract_if_present(
                                capture.get("t0_request_sent"),
                                capture.get("t3_model_load_finished"),
                            ),
                            "worker_ready_internal_ms": startup_durations.get("worker_ready")
                            or startup_durations.get("model_load_finished"),
                            "engine_init_stage": engine_init_stage,
                            "engine_init_duration_ms": engine_init_duration_ms,
                            "memory_used_bytes": snapshot.get("memory_used_bytes"),
                            "memory_total_bytes": snapshot.get("memory_total_bytes"),
                            "gateway_registered": snapshot.get("gateway_registered"),
                            "local_model_path_exists": model_metadata.get("local_model_path_exists"),
                            "hf_cache_exists": model_metadata.get("inferred_hf_repo_cache_exists"),
                            "hf_snapshot_count": model_metadata.get("inferred_hf_snapshot_count"),
                            "source_path": str(output_path),
                            "notes": [str(note) for note in capture.get("notes") or []],
                        }
                    )

    engine_summary = {
        "engine": engine,
        "provider": provider,
        "gpu_type": gpu_type,
        "gpu_count": gpu_count,
        "model": model,
        "workload": preset,
        "concurrency": concurrency,
        "cost_per_hour": cost_per_hour,
        "manifest_path": str(manifest_path),
        "step_names": sorted(step_map),
        "notes": manifest_notes,
    }
    return engine_summary, warm_rows, cold_rows, startup_rows, gaps


def build_report(
    manifest_paths: list[Path],
    expected_engines: list[str],
    blocked_engines: dict[str, str] | None = None,
) -> dict[str, Any]:
    blocked_engines = dict(blocked_engines or {})
    selected_manifests, manifest_gaps = collect_manifest_candidates(manifest_paths)

    warm_rows: list[dict[str, Any]] = []
    cold_rows: list[dict[str, Any]] = []
    startup_rows: list[dict[str, Any]] = []
    engine_summaries: list[dict[str, Any]] = []
    blocked_rows: list[dict[str, Any]] = []
    gaps: list[str] = list(manifest_gaps)

    for engine in expected_engines:
        if engine in blocked_engines:
            selected = selected_manifests.get(engine)
            manifest_path = str(selected[0]) if selected else ""
            manifest_payload = selected[1] if selected else {}
            blocked_rows.append(
                {
                    "engine": engine,
                    "reason": blocked_engines[engine],
                    "manifest_path": manifest_path,
                    "step_names": sorted(str(step.get("name")) for step in (manifest_payload.get("steps") or [])),
                }
            )
            continue
        selected = selected_manifests.get(engine)
        if selected is None:
            gaps.append(f"{engine}: no phase 1 manifest discovered")
            continue
        manifest_path, manifest_payload = selected
        engine_summary, engine_warm_rows, engine_cold_rows, engine_startup_rows, engine_gaps = summarize_engine_manifest(
            manifest_path,
            manifest_payload,
        )
        engine_summaries.append(engine_summary)
        warm_rows.extend(engine_warm_rows)
        cold_rows.extend(engine_cold_rows)
        startup_rows.extend(engine_startup_rows)
        gaps.extend(engine_gaps)

    warm_rows.sort(key=lambda row: (expected_engines.index(row["engine"]), CACHE_REUSE_ORDER.get(row["cache_reuse_mode"], 99)))
    cold_rows.sort(key=lambda row: (expected_engines.index(row["engine"]), SCENARIO_ORDER.get(row["scenario"], 99)))
    startup_rows.sort(key=lambda row: (expected_engines.index(row["engine"]), CAPTURE_ORDER.get(row["capture"], 99)))
    engine_summaries.sort(key=lambda row: expected_engines.index(row["engine"]))
    blocked_rows.sort(key=lambda row: expected_engines.index(row["engine"]))

    return {
        "generated_at": now_iso(),
        "inputs": [str(path) for path in manifest_paths],
        "expected_engines": expected_engines,
        "engines": engine_summaries,
        "blocked_engines": blocked_rows,
        "warm_matrix": warm_rows,
        "cold_start_matrix": cold_rows,
        "startup_health_matrix": startup_rows,
        "gaps": gaps,
    }


def bytes_to_gib(value: Any) -> float | None:
    if value is None:
        return None
    return float(value) / float(1024**3)


def format_ms(value: Any) -> str:
    if value is None:
        return ""
    return f"{int(value):,}"


def format_float(value: Any, digits: int = 2) -> str:
    if value is None:
        return ""
    return f"{float(value):.{digits}f}"


def format_memory(value_used: Any, value_total: Any) -> str:
    used_gib = bytes_to_gib(value_used)
    total_gib = bytes_to_gib(value_total)
    if used_gib is None and total_gib is None:
        return ""
    if used_gib is None:
        return f"/{total_gib:.1f} GiB"
    if total_gib is None:
        return f"{used_gib:.1f} GiB"
    return f"{used_gib:.1f}/{total_gib:.1f} GiB"


def format_bool(value: Any) -> str:
    if value is None:
        return ""
    return "yes" if bool(value) else "no"


def render_table(headers: list[str], rows: list[list[str]]) -> list[str]:
    lines = [
        "| " + " | ".join(headers) + " |",
        "| " + " | ".join("---" for _header in headers) + " |",
    ]
    for row in rows:
        lines.append("| " + " | ".join(row) + " |")
    return lines


def render_markdown(report: dict[str, Any]) -> str:
    lines: list[str] = []
    lines.append("# Untuned Engine Phase 1 Baseline")
    lines.append("")
    lines.append(f"Generated: `{report['generated_at']}`")
    lines.append("")
    lines.append("Expected engines: " + ", ".join(f"`{engine}`" for engine in report["expected_engines"]))
    lines.append("")

    if report["blocked_engines"]:
        lines.append("## Blocked Engines")
        lines.append("")
        blocked_rows = [
            [
                str(row["engine"]),
                str(row["reason"]),
                str(row["manifest_path"]),
            ]
            for row in report["blocked_engines"]
        ]
        lines.extend(render_table(["Engine", "Reason", "Manifest"], blocked_rows))
        lines.append("")

    lines.append("## Warm Baseline")
    lines.append("")
    if report["warm_matrix"]:
        warm_rows = [
            [
                str(row["engine"]),
                str(row["provider"]),
                str(row["gpu_type"]),
                str(row["gpu_count"]),
                str(row["model"]),
                str(row["workload"]),
                str(row["concurrency"]),
                str(row["cache_reuse_mode"]),
                format_float(row["ttft_p50_ms"], 1),
                format_float(row["ttft_p95_ms"], 1),
                format_float(row["stream_total_p50_ms"], 1),
                format_float(row["decode_tok_s_p50"], 2),
                format_float(row["aggregate_decode_tok_s_p50"], 2),
                format_float(row["cost_query_usd_p50"], 6),
                f"rows={row['rows']}",
            ]
            for row in report["warm_matrix"]
        ]
        lines.extend(
            render_table(
                [
                    "Engine",
                    "Provider",
                    "GPU",
                    "GPU Count",
                    "Model",
                    "Workload",
                    "Concurrency",
                    "Cache Reuse",
                    "TTFT p50 ms",
                    "TTFT p95 ms",
                    "Stream total p50 ms",
                    "Decode tok/s p50",
                    "Aggregate decode tok/s p50",
                    "Cost/query p50 USD",
                    "Notes",
                ],
                warm_rows,
            )
        )
    else:
        lines.append("_No warm benchmark outputs discovered._")
    lines.append("")

    lines.append("## Cold-Start Baseline")
    lines.append("")
    if report["cold_start_matrix"]:
        cold_rows = [
            [
                str(row["engine"]),
                str(row["provider"]),
                str(row["gpu_type"]),
                str(row["scenario"]),
                format_ms(row["request_to_running_ms"]),
                format_ms(row["request_to_server_started_ms"]),
                format_ms(row["server_to_model_ready_ms"]),
                format_ms(row["request_to_first_success_ms"]),
                format_memory(row["memory_used_bytes"], row["memory_total_bytes"]),
                f"gateway_registered={format_bool(row['gateway_registered'])}",
            ]
            for row in report["cold_start_matrix"]
        ]
        lines.extend(
            render_table(
                [
                    "Engine",
                    "Provider",
                    "GPU",
                    "Scenario",
                    "Request->Running ms",
                    "Request->Server ms",
                    "Server->Ready ms",
                    "Request->First Success ms",
                    "Memory",
                    "Notes",
                ],
                cold_rows,
            )
        )
    else:
        lines.append("_No cold-start outputs discovered._")
    lines.append("")

    lines.append("## Startup Health")
    lines.append("")
    if report["startup_health_matrix"]:
        startup_rows = [
            [
                str(row["engine"]),
                str(row["capture"]),
                format_ms(row["request_to_server_started_ms"]),
                format_ms(row["server_to_model_ready_ms"]),
                format_ms(row["worker_ready_internal_ms"]),
                format_ms(row["engine_init_duration_ms"]),
                format_memory(row["memory_used_bytes"], row["memory_total_bytes"]),
                format_bool(row["local_model_path_exists"]),
                format_bool(row["hf_cache_exists"]),
                str(row["hf_snapshot_count"] if row["hf_snapshot_count"] is not None else ""),
                f"gateway_registered={format_bool(row['gateway_registered'])}",
            ]
            for row in report["startup_health_matrix"]
        ]
        lines.extend(
            render_table(
                [
                    "Engine",
                    "Capture",
                    "Request->Server ms",
                    "Server->Ready ms",
                    "Worker Ready ms",
                    "Engine Init ms",
                    "Memory",
                    "Local Model Path",
                    "HF Cache",
                    "Snapshots",
                    "Notes",
                ],
                startup_rows,
            )
        )
    else:
        lines.append("_No startup-health outputs discovered._")
    lines.append("")

    lines.append("## Gaps")
    lines.append("")
    if report["gaps"]:
        for gap in report["gaps"]:
            lines.append(f"- {gap}")
    else:
        lines.append("- none")
    lines.append("")
    return "\n".join(lines)


def write_output(path: str, content: str) -> Path:
    output_path = Path(path).expanduser().resolve()
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(content, encoding="utf-8")
    return output_path


def main() -> int:
    args = parse_args()
    expected_engines = args.engine or list(SUPPORTED_ENGINES)

    try:
        manifest_paths = discover_manifest_paths(args.inputs)
    except FileNotFoundError as exc:
        print(str(exc), file=sys.stderr)
        return 2

    if not manifest_paths:
        print("no phase 1 manifests found", file=sys.stderr)
        return 2

    try:
        blocked_engines = parse_blocked_engines(args.blocked_engine)
    except ValueError as exc:
        print(str(exc), file=sys.stderr)
        return 2

    report = build_report(manifest_paths, expected_engines, blocked_engines)
    markdown = render_markdown(report)

    if args.markdown_output:
        markdown_path = write_output(args.markdown_output, markdown + "\n")
        print(f"Wrote Markdown report to {markdown_path}")
    else:
        print(markdown)

    if args.json_output:
        json_path = write_output(args.json_output, json.dumps(report, indent=2) + "\n")
        print(f"Wrote JSON summary to {json_path}")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
