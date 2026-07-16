#!/usr/bin/env python3
"""Run the Phase 1 engine benchmark matrix for a single engine deployment."""

from __future__ import annotations

import argparse
from dataclasses import asdict, dataclass
from datetime import datetime, timezone
import json
from pathlib import Path
import shlex
import shutil
import ssl
import subprocess
import sys
import time
import urllib.error
import urllib.request
from typing import Any

REPO_ROOT = Path(__file__).resolve().parents[1]
PYTHON_SRC = REPO_ROOT / "python" / "src"
if str(PYTHON_SRC) not in sys.path:
    sys.path.insert(0, str(PYTHON_SRC))

from infera_bench.catalog import default_catalog_root
from infera_bench.execution import (
    ExecutionStep as SharedExecutionStep,
    build_cold_start_command as build_shared_cold_start_command,
    build_startup_health_command as build_shared_startup_health_command,
    build_warm_command as build_shared_warm_command,
    retained_health_url_for_step as shared_retained_health_url_for_step,
    run_step as shared_run_step,
    wait_for_warm_registration as shared_wait_for_warm_registration,
    write_json_output as shared_write_json_output,
)
from infera_bench.schema import AttachTargetSpec, ResolvedRunSpec

DEFAULT_BASE_URL = "https://inferai.co.in"
DEFAULT_OUTPUT_DIR = Path("/tmp/infera-engine-benchmarks")
DEFAULT_WORKLOAD_FILE = default_catalog_root() / "workloads.json"
SUPPORTED_ENGINES = ("vllm", "sglang", "tensorrt_llm")
PROGRESS_LOG_INTERVAL_S = 15.0
HEALTH_REQUEST_TIMEOUT_S = 5


@dataclass
class Phase1Step:
    name: str
    category: str
    output_path: str
    command: list[str]


@dataclass
class Phase1StepResult:
    name: str
    category: str
    output_path: str
    command: list[str]
    command_display: str
    started_at: str | None
    finished_at: str | None
    duration_ms: int | None
    returncode: int | None
    status: str


def parse_runtime_options(values: list[str]) -> dict[str, str]:
    options: dict[str, str] = {}
    for raw_value in values:
        entry = str(raw_value).strip()
        if not entry:
            continue
        key, separator, value = entry.partition("=")
        key = key.strip()
        value = value.strip()
        if separator == "" or not key or not value:
            raise ValueError(f"runtime option must be KEY=VALUE, got: {raw_value}")
        options[key] = value
    return options


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Run the Phase 1 warm/cold/startup benchmark workflow for a single engine.",
    )
    parser.add_argument(
        "base_url",
        nargs="?",
        default=DEFAULT_BASE_URL,
        help="Gateway base URL (default: %(default)s)",
    )
    parser.add_argument("--api-key", required=True, help="Gateway bearer token")
    parser.add_argument(
        "--engine",
        required=True,
        choices=SUPPORTED_ENGINES,
        help="Inference engine to benchmark",
    )
    parser.add_argument("--provider", default="runpod", help="Provider label/type (default: %(default)s)")
    parser.add_argument("--gpu-type", required=True, help="Infera GPU type, e.g. A100_80GB")
    parser.add_argument("--provider-gpu-type-id", default="", help="Exact provider GPU type identifier")
    parser.add_argument(
        "--runtime-option",
        action="append",
        default=[],
        help="Provision runtime option in KEY=VALUE form. Can be repeated.",
    )
    parser.add_argument("--gpu-count", type=int, default=1, help="GPU count (default: %(default)s)")
    parser.add_argument("--model", required=True, help="Model ID to benchmark")
    parser.add_argument(
        "--phase-label",
        default="phase1",
        help="Label used in manifest naming and metadata (default: %(default)s)",
    )
    parser.add_argument(
        "--profile-name",
        default="",
        help="Optional profile label appended to output paths and manifest metadata.",
    )
    parser.add_argument(
        "--preset",
        default="conversation",
        help="Warm benchmark preset to run (default: %(default)s)",
    )
    parser.add_argument("--warm-runs", type=int, default=3, help="Measured warm groups (default: %(default)s)")
    parser.add_argument("--warmup", type=int, default=2, help="Warmup groups (default: %(default)s)")
    parser.add_argument("--concurrency", type=int, default=4, help="Warm benchmark concurrency (default: %(default)s)")
    parser.add_argument(
        "--cache-key-prefix",
        default="baseline",
        help="Affinity cache-key prefix for warm affinity runs (default: %(default)s)",
    )
    parser.add_argument(
        "--cost-per-hour",
        type=float,
        default=None,
        help="Optional hourly infra cost to include in warm benchmark output",
    )
    parser.add_argument(
        "--instance-name-prefix",
        default="engine-phase1",
        help="Prefix used when provisioning benchmark instances (default: %(default)s)",
    )
    parser.add_argument(
        "--output-dir",
        default=str(DEFAULT_OUTPUT_DIR),
        help="Directory for benchmark JSON outputs and manifest (default: %(default)s)",
    )
    parser.add_argument(
        "--python-bin",
        default=sys.executable,
        help="Python executable to use for helper scripts (default: current interpreter)",
    )
    parser.add_argument(
        "--benchmark-header",
        action="append",
        default=[],
        help="Extra header passed to benchmark-chat.py in 'Name: Value' form. Can be repeated.",
    )
    parser.add_argument(
        "--skip-warm",
        action="store_true",
        help="Skip both warm benchmark runs",
    )
    parser.add_argument(
        "--skip-cold-start",
        action="store_true",
        help="Skip the cold-start benchmark",
    )
    parser.add_argument(
        "--skip-startup-health",
        action="store_true",
        help="Skip startup-health capture",
    )
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
    parser.add_argument(
        "--quiet-progress",
        action="store_true",
        help="Suppress live progress logs from helper scripts where supported",
    )
    parser.add_argument(
        "--continue-on-error",
        action="store_true",
        help="Continue remaining steps even if one step fails",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print planned commands and write the manifest without executing them",
    )
    parser.add_argument(
        "--json-output",
        default=None,
        help="Optional path to write the orchestration manifest JSON",
    )
    parser.add_argument(
        "--warm-ready-timeout-s",
        type=int,
        default=180,
        help="How long to wait for the retained worker to register with the gateway before warm runs (default: %(default)s)",
    )
    args = parser.parse_args()
    parse_runtime_options(args.runtime_option)
    return args


def now_iso() -> str:
    return datetime.now(timezone.utc).isoformat()


def log(message: str) -> None:
    print(f"[engine-phase1] {message}", flush=True)


def slugify(value: str) -> str:
    slug = value.strip().lower().replace("/", "-").replace("_", "-")
    while "--" in slug:
        slug = slug.replace("--", "-")
    return slug


def summarize_health_poll_error(exc: Exception) -> str:
    message = str(exc)
    normalized = message.lower()

    if "http 404" in normalized:
        return "bootstrap in progress: worker health route not published yet (HTTP 404)"
    if "http 502" in normalized:
        return "bootstrap in progress: proxy upstream not ready yet (HTTP 502)"
    if "timed out" in normalized:
        return "bootstrap in progress: worker health endpoint not responding yet (timeout)"
    if "connection refused" in normalized:
        return "bootstrap in progress: worker health endpoint not accepting connections yet"
    if "connection reset" in normalized or "remote end closed connection" in normalized:
        return "bootstrap in progress: worker health endpoint restarted before responding"

    return message


def request_json(
    method: str,
    url: str,
    *,
    timeout: int = 60,
    insecure: bool = False,
) -> dict[str, object]:
    request = urllib.request.Request(
        url,
        headers={
            "User-Agent": "infera-engine-phase1/1.0",
            "Accept": "application/json",
        },
        method=method,
    )
    context = None
    if insecure and url.startswith("https://"):
        context = ssl._create_unverified_context()
    try:
        with urllib.request.urlopen(request, timeout=timeout, context=context) as response:
            body = response.read()
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"{method} {url} failed with HTTP {exc.code}: {body}") from exc
    except urllib.error.URLError as exc:
        raise RuntimeError(f"{method} {url} failed: {exc.reason}") from exc

    if not body:
        return {}
    return json.loads(body)


def request_json_via_curl(
    method: str,
    url: str,
    *,
    timeout: int = 60,
    insecure: bool = False,
) -> dict[str, object]:
    curl_path = shutil.which("curl")
    if curl_path is None:
        raise RuntimeError("curl is not installed")

    cmd = [
        curl_path,
        "-sS",
        "-X",
        method,
        "--max-time",
        str(timeout),
        "-H",
        "User-Agent: infera-engine-phase1/1.0",
        "-H",
        "Accept: application/json",
    ]
    if insecure and url.startswith("https://"):
        cmd.append("-k")
    cmd.append(url)

    result = subprocess.run(cmd, capture_output=True, text=True, check=False)
    if result.returncode != 0:
        stderr = result.stderr.strip() or "unknown curl error"
        raise RuntimeError(f"curl {method} {url} failed: {stderr}")

    body = result.stdout.strip()
    if not body:
        return {}
    try:
        return json.loads(body)
    except json.JSONDecodeError as exc:
        raise RuntimeError(f"curl {method} {url} returned non-JSON body: {body[:200]}") from exc


def fetch_health(health_url: str, *, insecure: bool) -> dict[str, object]:
    try:
        return request_json("GET", health_url, timeout=HEALTH_REQUEST_TIMEOUT_S, insecure=insecure)
    except Exception as exc:
        if "HTTP 403" not in str(exc) or "1010" not in str(exc):
            raise
    return request_json_via_curl("GET", health_url, timeout=HEALTH_REQUEST_TIMEOUT_S, insecure=insecure)


def benchmark_output_dir(args: argparse.Namespace) -> Path:
    output_dir = Path(args.output_dir).expanduser() / slugify(args.engine)
    profile_name = str(getattr(args, "profile_name", "") or "").strip()
    if profile_name:
        output_dir = output_dir / slugify(profile_name)
    return output_dir


def build_manifest_path(args: argparse.Namespace) -> Path:
    if args.json_output:
        return Path(args.json_output).expanduser()
    parts = [
        slugify(str(getattr(args, "phase_label", "phase1") or "phase1")),
        slugify(args.engine),
        slugify(args.gpu_type),
    ]
    profile_name = str(getattr(args, "profile_name", "") or "").strip()
    if profile_name:
        parts.append(slugify(profile_name))
    return benchmark_output_dir(args) / ("-".join(parts) + "-manifest.json")


def retained_health_url_for_step(step_name: str, output_path: str) -> str | None:
    return shared_retained_health_url_for_step(step_name, output_path)


def runtime_options_from_args(args: argparse.Namespace) -> dict[str, str]:
    return parse_runtime_options(list(getattr(args, "runtime_option", []) or []))


def benchmark_headers_from_args(args: argparse.Namespace) -> dict[str, str]:
    headers: dict[str, str] = {}
    for raw_value in list(getattr(args, "benchmark_header", []) or []):
        entry = str(raw_value).strip()
        if not entry:
            continue
        name, separator, value = entry.partition(":")
        if separator == "":
            raise ValueError(f"benchmark header must be 'Name: Value', got: {raw_value}")
        trimmed_name = name.strip()
        trimmed_value = value.strip()
        if not trimmed_name or not trimmed_value:
            raise ValueError(f"benchmark header must be 'Name: Value', got: {raw_value}")
        headers[trimmed_name] = trimmed_value
    return headers


def build_phase1_run_spec(args: argparse.Namespace) -> ResolvedRunSpec:
    phase_label = str(getattr(args, "phase_label", "phase1") or "phase1")
    profile_name = str(getattr(args, "profile_name", "") or "").strip()
    run_id_parts = [phase_label, args.engine, args.gpu_type]
    if profile_name:
        run_id_parts.append(profile_name)

    return ResolvedRunSpec(
        suite_id=phase_label,
        run_id=slugify("-".join(run_id_parts)),
        engine_id=args.engine,
        hardware_id=args.gpu_type,
        gpu_count=args.gpu_count,
        model_id=args.model,
        workload_id=args.preset,
        benchmark_profile_id="provision_full",
        runtime_preset_id=profile_name or "baseline",
        provider=args.provider,
        provider_gpu_type=args.provider_gpu_type_id or None,
        provider_gpu_type_id=args.provider_gpu_type_id or None,
        allowed_cuda_versions=[],
        execution_mode="provision",
        compatibility_status="ready",
        compatibility_issues=[],
        generic_parameters={
            "legacy_prompt_preset": args.preset,
            "warm_runs": args.warm_runs,
            "warmup": args.warmup,
            "concurrency": args.concurrency,
            "instance_name_prefix": f"{args.instance_name_prefix}-{slugify(args.engine)}",
        },
        runtime_options=runtime_options_from_args(args),
        output_dir=str(benchmark_output_dir(args)),
        benchmark_headers=benchmark_headers_from_args(args),
        workload_headers={},
        attach_target=AttachTargetSpec(cache_key_prefix=args.cache_key_prefix),
        tags=[args.engine, args.gpu_type, args.model, args.preset],
    )


def append_runtime_options(command: list[str], args: argparse.Namespace) -> None:
    for key, value in sorted(runtime_options_from_args(args).items()):
        command.extend(["--runtime-option", f"{key}={value}"])


def build_warm_command(
    args: argparse.Namespace,
    *,
    cache_reuse_mode: str,
    output_path: Path,
) -> list[str]:
    return build_shared_warm_command(
        build_phase1_run_spec(args),
        python_bin=args.python_bin,
        base_url=args.base_url,
        api_key=args.api_key,
        workload_file=DEFAULT_WORKLOAD_FILE,
        cache_reuse_mode=cache_reuse_mode,
        output_path=output_path,
        cost_per_hour=args.cost_per_hour,
        health_url=None,
        health_insecure=args.health_insecure,
        health_sample_interval_ms=5000,
    )


def build_cold_start_command(args: argparse.Namespace, *, output_path: Path) -> list[str]:
    return build_shared_cold_start_command(
        build_phase1_run_spec(args),
        python_bin=args.python_bin,
        base_url=args.base_url,
        api_key=args.api_key,
        output_path=output_path,
        lifecycle_timeout_s=int(getattr(args, "lifecycle_timeout_s", 900) or 900),
        health_insecure=args.health_insecure,
        quiet_progress=args.quiet_progress,
        terminate_final_instance=should_terminate_after_cold_start(args),
    )


def build_startup_health_command(args: argparse.Namespace, *, output_path: Path) -> list[str]:
    return build_shared_startup_health_command(
        build_phase1_run_spec(args),
        python_bin=args.python_bin,
        base_url=args.base_url,
        api_key=args.api_key,
        output_path=output_path,
        lifecycle_timeout_s=int(getattr(args, "lifecycle_timeout_s", 900) or 900),
        health_insecure=args.health_insecure,
        quiet_progress=args.quiet_progress,
        terminate_final_instance=should_terminate_after_startup_health(args),
    )


def should_terminate_after_cold_start(args: argparse.Namespace) -> bool:
    startup_health_requested = not args.skip_startup_health
    warm_requested = not args.skip_warm
    if not args.terminate_final_instance:
        return False
    if startup_health_requested:
        return True
    return not warm_requested


def should_terminate_after_startup_health(args: argparse.Namespace) -> bool:
    warm_requested = not args.skip_warm
    if not args.terminate_final_instance:
        return False
    return not warm_requested


def build_phase1_steps(args: argparse.Namespace) -> list[Phase1Step]:
    out_dir = benchmark_output_dir(args)
    gpu_slug = slugify(args.gpu_type)
    engine_slug = slugify(args.engine)
    steps: list[Phase1Step] = []

    if not args.skip_cold_start:
        cold_path = out_dir / f"cold-start-{engine_slug}-{gpu_slug}.json"
        steps.append(
            Phase1Step(
                name="cold_start",
                category="cold_start",
                output_path=str(cold_path),
                command=build_cold_start_command(args, output_path=cold_path),
            )
        )

    if not args.skip_startup_health:
        startup_path = out_dir / f"startup-health-{engine_slug}-{gpu_slug}.json"
        steps.append(
            Phase1Step(
                name="startup_health",
                category="startup_health",
                output_path=str(startup_path),
                command=build_startup_health_command(args, output_path=startup_path),
            )
        )

    if not args.skip_warm:
        none_path = out_dir / f"infera-benchmark-{engine_slug}-{gpu_slug}-none.json"
        affinity_path = out_dir / f"infera-benchmark-{engine_slug}-{gpu_slug}-affinity.json"
        steps.extend(
            [
                Phase1Step(
                    name="warm_none",
                    category="warm",
                    output_path=str(none_path),
                    command=build_warm_command(args, cache_reuse_mode="none", output_path=none_path),
                ),
                Phase1Step(
                    name="warm_affinity",
                    category="warm",
                    output_path=str(affinity_path),
                    command=build_warm_command(args, cache_reuse_mode="affinity", output_path=affinity_path),
                ),
            ]
        )

    return steps


def build_manifest(args: argparse.Namespace, step_results: list[Phase1StepResult]) -> dict[str, object]:
    notes = [
        (
            "Warm benchmark results are only valid when the active fleet serving the target model "
            "is deployed with the selected engine."
        )
    ]
    if args.terminate_final_instance and not args.skip_warm:
        notes.append(
            "Warm steps follow lifecycle provisioning, so the final startup-health or cold-start instance "
            "is retained for warm traffic instead of being terminated automatically."
        )
    return {
        "base_url": args.base_url,
        "phase_label": str(getattr(args, "phase_label", "phase1") or "phase1"),
        "profile_name": str(getattr(args, "profile_name", "") or ""),
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
        "output_dir": str(benchmark_output_dir(args)),
        "runtime_options": runtime_options_from_args(args),
        "notes": notes,
        "steps": [asdict(result) for result in step_results],
    }


def write_json_output(path: Path, payload: dict[str, object]) -> Path:
    return shared_write_json_output(path, payload)


def run_step(step: Phase1Step, *, dry_run: bool) -> Phase1StepResult:
    result = shared_run_step(
        SharedExecutionStep(
            name=step.name,
            category=step.category,
            output_path=step.output_path,
            command=step.command,
        ),
        dry_run=dry_run,
    )
    return Phase1StepResult(**result.model_dump())


def wait_for_warm_registration(step: Phase1Step, args: argparse.Namespace) -> None:
    health_url = retained_health_url_for_step(step.name, step.output_path)
    if not health_url:
        log(f"warm readiness wait skipped after step={step.name}: no retained health_url found")
        return
    shared_wait_for_warm_registration(
        health_url=health_url,
        timeout_s=args.warm_ready_timeout_s,
        insecure=args.health_insecure,
        log_prefix="engine-phase1",
    )


def main() -> int:
    args = parse_args()
    steps = build_phase1_steps(args)
    if not steps:
        print("No benchmark steps selected.", file=sys.stderr)
        return 2

    manifest_path = build_manifest_path(args)
    benchmark_output_dir(args).mkdir(parents=True, exist_ok=True)

    log(
        "Warm results require that only the selected engine is actively serving the target model "
        f"during this run: engine={args.engine} model={args.model}"
    )
    if args.terminate_final_instance and not args.skip_warm:
        log(
            "warm steps are scheduled after lifecycle steps, so the final provisioned instance will be "
            "kept alive for warm traffic and must be cleaned up separately if needed"
        )

    step_results: list[Phase1StepResult] = []
    exit_code = 0
    for step in steps:
        log(f"step={step.name} output={step.output_path}")
        log(f"command={shlex.join(step.command)}")
        result = run_step(step, dry_run=args.dry_run)
        step_results.append(result)
        remaining_steps = steps[len(step_results) :]
        warm_steps_remaining = any(candidate.category == "warm" for candidate in remaining_steps)
        lifecycle_steps_remaining = any(
            candidate.category in {"cold_start", "startup_health"} for candidate in remaining_steps
        )
        if (
            result.status == "ok"
            and not args.dry_run
            and step.category in {"cold_start", "startup_health"}
            and warm_steps_remaining
            and not lifecycle_steps_remaining
        ):
            try:
                wait_for_warm_registration(step, args)
            except Exception as exc:
                result.status = "failed"
                result.returncode = 1
                exit_code = 1
                log(f"step={step.name} warm-readiness check failed: {exc}")
                if not args.continue_on_error:
                    break
        if result.status == "failed":
            exit_code = result.returncode or 1
            log(f"step={step.name} failed with returncode={result.returncode}")
            if not args.continue_on_error:
                break

    manifest = build_manifest(args, step_results)
    written_path = write_json_output(manifest_path, manifest)
    log(f"Wrote manifest to {written_path}")
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
