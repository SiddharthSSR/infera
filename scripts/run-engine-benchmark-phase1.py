#!/usr/bin/env python3
"""Run the Phase 1 engine benchmark matrix for a single engine deployment."""

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


DEFAULT_BASE_URL = "https://inferai.co.in"
DEFAULT_OUTPUT_DIR = Path("/tmp/infera-engine-benchmarks")
SUPPORTED_ENGINES = ("vllm", "sglang", "tensorrt_llm")


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
    parser.add_argument("--gpu-count", type=int, default=1, help="GPU count (default: %(default)s)")
    parser.add_argument("--model", required=True, help="Model ID to benchmark")
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
    return parser.parse_args()


def now_iso() -> str:
    return datetime.now(timezone.utc).isoformat()


def log(message: str) -> None:
    print(f"[engine-phase1] {message}", flush=True)


def slugify(value: str) -> str:
    slug = value.strip().lower().replace("/", "-").replace("_", "-")
    while "--" in slug:
        slug = slug.replace("--", "-")
    return slug


def benchmark_output_dir(args: argparse.Namespace) -> Path:
    return Path(args.output_dir).expanduser() / slugify(args.engine)


def build_manifest_path(args: argparse.Namespace) -> Path:
    if args.json_output:
        return Path(args.json_output).expanduser()
    return benchmark_output_dir(args) / f"phase1-{slugify(args.engine)}-{slugify(args.gpu_type)}-manifest.json"


def build_warm_command(
    args: argparse.Namespace,
    *,
    cache_reuse_mode: str,
    output_path: Path,
) -> list[str]:
    command = [
        args.python_bin,
        "scripts/benchmark-chat.py",
        args.base_url,
        "--api-key",
        args.api_key,
        "--model",
        args.model,
        "--engine-label",
        args.engine,
        "--provider-label",
        args.provider,
        "--gpu-label",
        args.gpu_type,
        "--preset",
        args.preset,
        "--runs",
        str(args.warm_runs),
        "--warmup",
        str(args.warmup),
        "--concurrency",
        str(args.concurrency),
        "--cache-reuse-mode",
        cache_reuse_mode,
        "--json-output",
        str(output_path),
    ]
    if cache_reuse_mode == "affinity":
        command.extend(["--cache-key-prefix", args.cache_key_prefix])
    if args.cost_per_hour is not None:
        command.extend(["--cost-per-hour", str(args.cost_per_hour)])
    for header in args.benchmark_header:
        command.extend(["--header", header])
    return command


def build_cold_start_command(args: argparse.Namespace, *, output_path: Path) -> list[str]:
    command = [
        args.python_bin,
        "scripts/cold-start-benchmark.py",
        args.base_url,
        "--api-key",
        args.api_key,
        "--provider",
        args.provider,
        "--engine",
        args.engine,
        "--gpu-type",
        args.gpu_type,
        "--gpu-count",
        str(args.gpu_count),
        "--model",
        args.model,
        "--instance-name",
        f"{args.instance_name_prefix}-{slugify(args.engine)}-cold",
        "--json-output",
        str(output_path),
    ]
    if args.provider_gpu_type_id:
        command.extend(["--provider-gpu-type-id", args.provider_gpu_type_id])
    if args.health_insecure:
        command.append("--health-insecure")
    if args.quiet_progress:
        command.append("--quiet-progress")
    if args.terminate_final_instance:
        command.append("--terminate-final-instance")
    return command


def build_startup_health_command(args: argparse.Namespace, *, output_path: Path) -> list[str]:
    command = [
        args.python_bin,
        "scripts/capture-startup-health.py",
        args.base_url,
        "--api-key",
        args.api_key,
        "--provider",
        args.provider,
        "--engine",
        args.engine,
        "--gpu-type",
        args.gpu_type,
        "--gpu-count",
        str(args.gpu_count),
        "--model",
        args.model,
        "--instance-name",
        f"{args.instance_name_prefix}-{slugify(args.engine)}-startup",
        "--include-restart",
        "--json-output",
        str(output_path),
    ]
    if args.provider_gpu_type_id:
        command.extend(["--provider-gpu-type-id", args.provider_gpu_type_id])
    if args.health_insecure:
        command.append("--health-insecure")
    if args.quiet_progress:
        command.append("--quiet-progress")
    if args.terminate_final_instance:
        command.append("--terminate-final-instance")
    return command


def build_phase1_steps(args: argparse.Namespace) -> list[Phase1Step]:
    out_dir = benchmark_output_dir(args)
    gpu_slug = slugify(args.gpu_type)
    engine_slug = slugify(args.engine)
    steps: list[Phase1Step] = []

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

    return steps


def build_manifest(args: argparse.Namespace, step_results: list[Phase1StepResult]) -> dict[str, object]:
    return {
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
        "output_dir": str(benchmark_output_dir(args)),
        "notes": [
            (
                "Warm benchmark results are only valid when the active fleet serving the target model "
                "is deployed with the selected engine."
            )
        ],
        "steps": [asdict(result) for result in step_results],
    }


def write_json_output(path: Path, payload: dict[str, object]) -> Path:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")
    return path


def run_step(step: Phase1Step, *, dry_run: bool) -> Phase1StepResult:
    started_at = now_iso()
    if dry_run:
        return Phase1StepResult(
            name=step.name,
            category=step.category,
            output_path=step.output_path,
            command=step.command,
            command_display=shlex.join(step.command),
            started_at=started_at,
            finished_at=started_at,
            duration_ms=0,
            returncode=None,
            status="dry_run",
        )

    start_perf = time.perf_counter()
    completed = subprocess.run(step.command, check=False)
    finished_at = now_iso()
    duration_ms = int((time.perf_counter() - start_perf) * 1000)
    return Phase1StepResult(
        name=step.name,
        category=step.category,
        output_path=step.output_path,
        command=step.command,
        command_display=shlex.join(step.command),
        started_at=started_at,
        finished_at=finished_at,
        duration_ms=duration_ms,
        returncode=completed.returncode,
        status="ok" if completed.returncode == 0 else "failed",
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

    step_results: list[Phase1StepResult] = []
    exit_code = 0
    for step in steps:
        log(f"step={step.name} output={step.output_path}")
        log(f"command={shlex.join(step.command)}")
        result = run_step(step, dry_run=args.dry_run)
        step_results.append(result)
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
