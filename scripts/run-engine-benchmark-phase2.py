#!/usr/bin/env python3
"""Run named Phase 2 tuning profiles for a single engine."""

from __future__ import annotations

import argparse
from dataclasses import asdict, dataclass
from datetime import datetime, timezone
import json
from pathlib import Path
import shlex
import subprocess
import sys
import time
from typing import Any


DEFAULT_BASE_URL = "https://inferai.co.in"
DEFAULT_OUTPUT_DIR = Path("/tmp/infera-engine-benchmarks-phase2")
DEFAULT_BASELINE_PRESET = Path("scripts/benchmark-presets/engine-phase-1-conservative.json")
DEFAULT_TUNING_PRESET = Path("scripts/benchmark-presets/engine-phase-2-tuning-space.json")
DEFAULT_PHASE1_RUNNER = Path("scripts/run-engine-benchmark-phase1.py")
SUPPORTED_ENGINES = ("vllm", "sglang", "tensorrt_llm")


@dataclass
class Phase2Profile:
    name: str
    group: str
    description: str
    runtime_options: dict[str, str]


@dataclass
class Phase2ProfileResult:
    profile_name: str
    group: str
    description: str
    runtime_options: dict[str, str]
    manifest_path: str
    command: list[str]
    command_display: str
    started_at: str | None
    finished_at: str | None
    duration_ms: int | None
    returncode: int | None
    status: str


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Run named Phase 2 tuning profiles for a single engine.")
    parser.add_argument(
        "base_url",
        nargs="?",
        default=DEFAULT_BASE_URL,
        help="Gateway base URL (default: %(default)s)",
    )
    parser.add_argument("--api-key", required=True, help="Gateway bearer token")
    parser.add_argument("--engine", required=True, choices=SUPPORTED_ENGINES, help="Engine to benchmark")
    parser.add_argument("--provider", default="runpod", help="Provider label/type (default: %(default)s)")
    parser.add_argument("--gpu-type", required=True, help="Infera GPU type, e.g. A100_80GB")
    parser.add_argument("--provider-gpu-type-id", default="", help="Exact provider GPU type identifier")
    parser.add_argument("--gpu-count", type=int, default=1, help="GPU count (default: %(default)s)")
    parser.add_argument("--model", required=True, help="Model ID to benchmark")
    parser.add_argument("--preset", default="conversation", help="Warm benchmark preset (default: %(default)s)")
    parser.add_argument("--warm-runs", type=int, default=3, help="Measured warm groups (default: %(default)s)")
    parser.add_argument("--warmup", type=int, default=2, help="Warmup groups (default: %(default)s)")
    parser.add_argument("--concurrency", type=int, default=4, help="Warm benchmark concurrency (default: %(default)s)")
    parser.add_argument(
        "--cache-key-prefix",
        default="phase2",
        help="Affinity cache-key prefix for warm affinity runs (default: %(default)s)",
    )
    parser.add_argument("--cost-per-hour", type=float, default=None, help="Optional hourly infra cost")
    parser.add_argument(
        "--instance-name-prefix",
        default="engine-phase2",
        help="Prefix used when provisioning benchmark instances (default: %(default)s)",
    )
    parser.add_argument(
        "--output-dir",
        default=str(DEFAULT_OUTPUT_DIR),
        help="Directory for benchmark JSON outputs and manifests (default: %(default)s)",
    )
    parser.add_argument(
        "--python-bin",
        default=sys.executable,
        help="Python executable to use for helper scripts (default: current interpreter)",
    )
    parser.add_argument(
        "--phase1-runner",
        default=str(DEFAULT_PHASE1_RUNNER),
        help="Phase 1 runner script used to execute each profile (default: %(default)s)",
    )
    parser.add_argument(
        "--baseline-preset-file",
        default=str(DEFAULT_BASELINE_PRESET),
        help="Phase 1 conservative preset file (default: %(default)s)",
    )
    parser.add_argument(
        "--tuning-preset-file",
        default=str(DEFAULT_TUNING_PRESET),
        help="Phase 2 tuning preset file with named profiles (default: %(default)s)",
    )
    parser.add_argument(
        "--benchmark-header",
        action="append",
        default=[],
        help="Extra header passed through to benchmark-chat.py in 'Name: Value' form. Can be repeated.",
    )
    parser.add_argument("--profile", action="append", default=[], help="Phase 2 profile name to run. Can be repeated.")
    parser.add_argument("--all-profiles", action="store_true", help="Run every named profile for the selected engine.")
    parser.add_argument("--list-profiles", action="store_true", help="Print available profiles for the selected engine.")
    parser.add_argument("--skip-warm", action="store_true", help="Skip both warm benchmark runs")
    parser.add_argument("--skip-cold-start", action="store_true", help="Skip the cold-start benchmark")
    parser.add_argument("--skip-startup-health", action="store_true", help="Skip startup-health capture")
    parser.add_argument(
        "--terminate-final-instance",
        action="store_true",
        help="Terminate final provisioned instances in cold/startup helper runs",
    )
    parser.add_argument(
        "--health-insecure",
        action="store_true",
        help="Disable TLS verification for worker health polling in helper scripts",
    )
    parser.add_argument("--quiet-progress", action="store_true", help="Suppress helper progress logs where supported")
    parser.add_argument("--continue-on-error", action="store_true", help="Continue remaining profiles if one fails")
    parser.add_argument("--dry-run", action="store_true", help="Print planned commands and write the manifest")
    parser.add_argument("--json-output", default=None, help="Optional path to write the Phase 2 orchestration manifest")
    parser.add_argument(
        "--warm-ready-timeout-s",
        type=int,
        default=180,
        help="How long Phase 1 waits for retained worker registration before warm runs (default: %(default)s)",
    )
    args = parser.parse_args()
    if not args.list_profiles and not args.all_profiles and not args.profile:
        parser.error("select one or more --profile values, or pass --all-profiles")
    return args


def now_iso() -> str:
    return datetime.now(timezone.utc).isoformat()


def log(message: str) -> None:
    print(f"[engine-phase2] {message}", flush=True)


def slugify(value: str) -> str:
    slug = value.strip().lower().replace("/", "-").replace("_", "-")
    while "--" in slug:
        slug = slug.replace("--", "-")
    return slug


def load_json(path: Path) -> dict[str, Any]:
    return json.loads(path.read_text(encoding="utf-8"))


def manifest_path_for_profile(args: argparse.Namespace, profile_name: str) -> Path:
    engine_slug = slugify(args.engine)
    gpu_slug = slugify(args.gpu_type)
    profile_slug = slugify(profile_name)
    return Path(args.output_dir).expanduser() / engine_slug / profile_slug / (
        f"phase2-{engine_slug}-{gpu_slug}-{profile_slug}-manifest.json"
    )


def load_profile_config(args: argparse.Namespace) -> tuple[dict[str, Any], dict[str, Any], str | None]:
    baseline_payload = load_json(Path(args.baseline_preset_file).expanduser())
    tuning_payload = load_json(Path(args.tuning_preset_file).expanduser())
    blocked = ((tuning_payload.get("blocked_engines") or {}).get(args.engine) or {}).get("reason")
    return baseline_payload, tuning_payload, blocked


def build_profiles(args: argparse.Namespace, baseline_payload: dict[str, Any], tuning_payload: dict[str, Any]) -> list[Phase2Profile]:
    baseline_engine = ((baseline_payload.get("engines") or {}).get(args.engine) or {})
    baseline_env = dict((baseline_engine.get("worker_env") or {}))
    tuning_engine = ((tuning_payload.get("engines") or {}).get(args.engine) or {})
    profiles_payload = list(tuning_engine.get("profiles") or [])
    if not profiles_payload:
        raise ValueError(f"no named profiles defined for engine {args.engine}")

    selected_names = list(args.profile or [])
    if args.all_profiles:
        selected_names = [str(profile.get("name") or "") for profile in profiles_payload]
    if not selected_names:
        return []

    available: dict[str, dict[str, Any]] = {}
    for profile in profiles_payload:
        profile_name = str(profile.get("name") or "").strip()
        if profile_name:
            available[profile_name] = profile

    missing = [name for name in selected_names if name not in available]
    if missing:
        raise ValueError(
            f"unknown profile(s) for engine {args.engine}: {', '.join(missing)}; "
            f"available: {', '.join(sorted(available))}"
        )

    profiles: list[Phase2Profile] = []
    for name in selected_names:
        profile_payload = available[name]
        runtime_options = dict(baseline_env)
        runtime_options.update(profile_payload.get("worker_env") or {})
        profiles.append(
            Phase2Profile(
                name=name,
                group=str(profile_payload.get("group") or "baseline"),
                description=str(profile_payload.get("description") or ""),
                runtime_options=runtime_options,
            )
        )
    return profiles


def render_profiles(engine: str, baseline_payload: dict[str, Any], tuning_payload: dict[str, Any]) -> str:
    baseline_engine = ((baseline_payload.get("engines") or {}).get(engine) or {})
    tuning_engine = ((tuning_payload.get("engines") or {}).get(engine) or {})
    lines = [f"Engine: {engine}"]
    if reason := ((tuning_payload.get("blocked_engines") or {}).get(engine) or {}).get("reason"):
        lines.append(f"Blocked: {reason}")
        return "\n".join(lines)
    lines.append(f"Primary metrics: {', '.join(str(metric) for metric in tuning_engine.get('primary_metrics') or [])}")
    lines.append("Profiles:")
    baseline_env = dict((baseline_engine.get("worker_env") or {}))
    for profile in tuning_engine.get("profiles") or []:
        profile_name = str(profile.get("name") or "")
        group = str(profile.get("group") or "baseline")
        description = str(profile.get("description") or "")
        overrides = dict(profile.get("worker_env") or {})
        runtime_options = dict(baseline_env)
        runtime_options.update(overrides)
        lines.append(f"- {profile_name}: group={group}; {description}")
        lines.append("  runtime_options=" + json.dumps(runtime_options, sort_keys=True))
    return "\n".join(lines)


def build_phase1_command(args: argparse.Namespace, profile: Phase2Profile) -> list[str]:
    command = [
        args.python_bin,
        args.phase1_runner,
        args.base_url,
        "--api-key",
        args.api_key,
        "--engine",
        args.engine,
        "--provider",
        args.provider,
        "--gpu-type",
        args.gpu_type,
        "--gpu-count",
        str(args.gpu_count),
        "--model",
        args.model,
        "--preset",
        args.preset,
        "--warm-runs",
        str(args.warm_runs),
        "--warmup",
        str(args.warmup),
        "--concurrency",
        str(args.concurrency),
        "--cache-key-prefix",
        args.cache_key_prefix,
        "--instance-name-prefix",
        args.instance_name_prefix,
        "--output-dir",
        args.output_dir,
        "--phase-label",
        "phase2",
        "--profile-name",
        profile.name,
        "--json-output",
        str(manifest_path_for_profile(args, profile.name)),
        "--warm-ready-timeout-s",
        str(args.warm_ready_timeout_s),
    ]
    if args.provider_gpu_type_id:
        command.extend(["--provider-gpu-type-id", args.provider_gpu_type_id])
    if args.cost_per_hour is not None:
        command.extend(["--cost-per-hour", str(args.cost_per_hour)])
    for header in args.benchmark_header:
        command.extend(["--benchmark-header", header])
    if args.skip_warm:
        command.append("--skip-warm")
    if args.skip_cold_start:
        command.append("--skip-cold-start")
    if args.skip_startup_health:
        command.append("--skip-startup-health")
    if args.terminate_final_instance:
        command.append("--terminate-final-instance")
    if args.health_insecure:
        command.append("--health-insecure")
    if args.quiet_progress:
        command.append("--quiet-progress")
    if args.continue_on_error:
        command.append("--continue-on-error")
    if args.dry_run:
        command.append("--dry-run")
    for key, value in sorted(profile.runtime_options.items()):
        command.extend(["--runtime-option", f"{key}={value}"])
    return command


def run_profile(args: argparse.Namespace, profile: Phase2Profile) -> Phase2ProfileResult:
    command = build_phase1_command(args, profile)
    started_at = now_iso()
    if args.dry_run:
        return Phase2ProfileResult(
            profile_name=profile.name,
            group=profile.group,
            description=profile.description,
            runtime_options=profile.runtime_options,
            manifest_path=str(manifest_path_for_profile(args, profile.name)),
            command=command,
            command_display=shlex.join(command),
            started_at=started_at,
            finished_at=started_at,
            duration_ms=0,
            returncode=None,
            status="dry_run",
        )

    start_perf = time.perf_counter()
    completed = subprocess.run(command, check=False)
    finished_at = now_iso()
    duration_ms = int((time.perf_counter() - start_perf) * 1000)
    return Phase2ProfileResult(
        profile_name=profile.name,
        group=profile.group,
        description=profile.description,
        runtime_options=profile.runtime_options,
        manifest_path=str(manifest_path_for_profile(args, profile.name)),
        command=command,
        command_display=shlex.join(command),
        started_at=started_at,
        finished_at=finished_at,
        duration_ms=duration_ms,
        returncode=completed.returncode,
        status="ok" if completed.returncode == 0 else "failed",
    )


def write_output(path: Path, payload: dict[str, Any]) -> Path:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")
    return path


def build_manifest(args: argparse.Namespace, profiles: list[Phase2Profile], results: list[Phase2ProfileResult]) -> dict[str, Any]:
    return {
        "generated_at": now_iso(),
        "phase_label": "phase2",
        "base_url": args.base_url,
        "engine": args.engine,
        "provider": args.provider,
        "gpu_type": args.gpu_type,
        "provider_gpu_type_id": args.provider_gpu_type_id,
        "gpu_count": args.gpu_count,
        "model": args.model,
        "preset": args.preset,
        "warm_runs": args.warm_runs,
        "warmup": args.warmup,
        "concurrency": args.concurrency,
        "cost_per_hour": args.cost_per_hour,
        "output_dir": str(Path(args.output_dir).expanduser()),
        "baseline_preset_file": str(Path(args.baseline_preset_file).expanduser()),
        "tuning_preset_file": str(Path(args.tuning_preset_file).expanduser()),
        "profiles": [asdict(profile) for profile in profiles],
        "results": [asdict(result) for result in results],
    }


def main() -> int:
    args = parse_args()
    baseline_payload, tuning_payload, blocked_reason = load_profile_config(args)

    if args.list_profiles:
        print(render_profiles(args.engine, baseline_payload, tuning_payload))
        return 0

    if blocked_reason:
        print(
            f"{args.engine} is blocked for Phase 2 on the current benchmark target: {blocked_reason}",
            file=sys.stderr,
        )
        return 2

    try:
        profiles = build_profiles(args, baseline_payload, tuning_payload)
    except ValueError as exc:
        print(str(exc), file=sys.stderr)
        return 2

    results: list[Phase2ProfileResult] = []
    exit_code = 0
    for profile in profiles:
        log(f"profile={profile.name} group={profile.group}")
        result = run_profile(args, profile)
        results.append(result)
        if result.status == "failed":
            exit_code = result.returncode or 1
            log(f"profile={profile.name} failed with returncode={result.returncode}")
            if not args.continue_on_error:
                break

    manifest_path = (
        Path(args.json_output).expanduser()
        if args.json_output
        else Path(args.output_dir).expanduser() / slugify(args.engine) / f"phase2-{slugify(args.engine)}-profiles-manifest.json"
    )
    written_path = write_output(manifest_path, build_manifest(args, profiles, results))
    log(f"Wrote manifest to {written_path}")
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
