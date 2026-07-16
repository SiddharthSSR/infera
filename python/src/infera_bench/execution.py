"""Execution layer for generic benchmark experiments."""

from __future__ import annotations

import json
import shlex
import shutil
import ssl
import subprocess
import sys
import time
import urllib.error
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from .schema import ExecutionStepResult, ExperimentExecutionResult, ResolvedRunSpec, utc_now_iso

PROGRESS_LOG_INTERVAL_S = 15.0
HEALTH_REQUEST_TIMEOUT_S = 5


@dataclass
class ExecutionStep:
    name: str
    category: str
    output_path: str
    command: list[str]


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
    headers: dict[str, str] | None = None,
) -> dict[str, Any]:
    request_headers = {
        "User-Agent": "infera-bench/1.0",
        "Accept": "application/json",
    }
    if headers:
        request_headers.update(headers)
    request = urllib.request.Request(
        url,
        headers=request_headers,
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
    headers: dict[str, str] | None = None,
) -> dict[str, Any]:
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
        "User-Agent: infera-bench/1.0",
        "-H",
        "Accept: application/json",
    ]
    for name, value in sorted((headers or {}).items()):
        cmd.extend(["-H", f"{name}: {value}"])
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


def fetch_health(health_url: str, *, insecure: bool) -> dict[str, Any]:
    try:
        return request_json("GET", health_url, timeout=HEALTH_REQUEST_TIMEOUT_S, insecure=insecure)
    except Exception as exc:
        if "HTTP 403" not in str(exc) or "1010" not in str(exc):
            raise
    return request_json_via_curl(
        "GET", health_url, timeout=HEALTH_REQUEST_TIMEOUT_S, insecure=insecure
    )


def fetch_gateway_workers(base_url: str, api_key: str) -> dict[str, Any]:
    url = f"{base_url.rstrip('/')}/api/workers"
    headers = {"Authorization": f"Bearer {api_key}"}
    try:
        return request_json("GET", url, timeout=HEALTH_REQUEST_TIMEOUT_S, headers=headers)
    except Exception as exc:
        if "HTTP 403" not in str(exc) or "1010" not in str(exc):
            raise
    return request_json_via_curl("GET", url, timeout=HEALTH_REQUEST_TIMEOUT_S, headers=headers)


def terminate_instance(base_url: str, api_key: str, instance_id: str) -> dict[str, Any]:
    headers = {"Authorization": f"Bearer {api_key}"}
    url = f"{base_url.rstrip('/')}/api/instances/{instance_id}"
    try:
        return request_json("DELETE", url, timeout=60, headers=headers)
    except Exception as exc:
        if "pod not found to terminate" in str(exc).lower():
            return {"instance_id": instance_id, "success": False, "note": "already_absent"}
        raise


def lifecycle_record_for_step(step_name: str, output_path: str) -> dict[str, Any] | None:
    path = Path(output_path).expanduser()
    if not path.exists():
        return None
    payload = json.loads(path.read_text(encoding="utf-8"))
    records_key = (
        "captures"
        if step_name == "startup_health"
        else "scenarios"
        if step_name == "cold_start"
        else None
    )
    if records_key is None:
        return None
    records = payload.get(records_key) or []
    if not records:
        return None
    return records[-1]


def retained_health_url_for_step(step_name: str, output_path: str) -> str | None:
    record = lifecycle_record_for_step(step_name, output_path)
    if not record:
        return None
    value = record.get("health_url")
    return str(value) if value else None


def retained_instance_id_for_step(step_name: str, output_path: str) -> str | None:
    record = lifecycle_record_for_step(step_name, output_path)
    if not record:
        return None
    value = record.get("instance_id")
    return str(value) if value else None


def write_json_output(path: Path, payload: dict[str, object]) -> Path:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, indent=2) + "\n", encoding="utf-8")
    return path


def runtime_options_to_cli(runtime_options: dict[str, str]) -> list[str]:
    args: list[str] = []
    for key, value in sorted(runtime_options.items()):
        args.extend(["--runtime-option", f"{key}={value}"])
    return args


def build_warm_command(
    run_spec: ResolvedRunSpec,
    *,
    python_bin: str,
    base_url: str,
    api_key: str,
    workload_file: Path,
    cache_reuse_mode: str,
    output_path: Path,
    cost_per_hour: float | None,
    health_url: str | None,
    health_insecure: bool,
    health_sample_interval_ms: int,
) -> list[str]:
    legacy_prompt_preset = str(
        run_spec.generic_parameters.get("legacy_prompt_preset") or ""
    ).strip()
    warm_runs = int(run_spec.generic_parameters.get("warm_runs") or 3)
    warmup = int(run_spec.generic_parameters.get("warmup") or 0)
    concurrency = int(run_spec.generic_parameters.get("concurrency") or 1)
    max_tokens = run_spec.generic_parameters.get("max_tokens")
    temperature = run_spec.generic_parameters.get("temperature")

    command = [
        python_bin,
        "scripts/benchmark-chat.py",
        base_url,
        "--api-key",
        api_key,
        "--model",
        run_spec.model_id,
        "--engine-label",
        run_spec.engine_id,
        "--provider-label",
        run_spec.provider or "",
        "--gpu-label",
        run_spec.hardware_id,
        "--runs",
        str(warm_runs),
        "--warmup",
        str(warmup),
        "--concurrency",
        str(concurrency),
        "--cache-reuse-mode",
        cache_reuse_mode,
        "--json-output",
        str(output_path),
    ]
    if legacy_prompt_preset:
        command.extend(["--preset", legacy_prompt_preset])
    else:
        command.extend(["--workload-file", str(workload_file), "--workload", run_spec.workload_id])
    if max_tokens is not None:
        command.extend(["--max-tokens", str(max_tokens)])
    if temperature is not None:
        command.extend(["--temperature", str(temperature)])
    if cache_reuse_mode == "affinity":
        cache_key_prefix = (
            run_spec.attach_target.cache_key_prefix if run_spec.attach_target else "benchmark"
        )
        command.extend(["--cache-key-prefix", cache_key_prefix])
    if cost_per_hour is not None:
        command.extend(["--cost-per-hour", str(cost_per_hour)])
    for name, value in sorted({**run_spec.benchmark_headers, **run_spec.workload_headers}.items()):
        command.extend(["--header", f"{name}: {value}"])
    if health_url:
        command.extend(
            [
                "--sample-health-url",
                health_url,
                "--health-sample-interval-ms",
                str(health_sample_interval_ms),
            ]
        )
        if health_insecure:
            command.append("--health-insecure")
    return command


def build_cold_start_command(
    run_spec: ResolvedRunSpec,
    *,
    python_bin: str,
    base_url: str,
    api_key: str,
    output_path: Path,
    lifecycle_timeout_s: int,
    health_insecure: bool,
    quiet_progress: bool,
    terminate_final_instance: bool,
) -> list[str]:
    instance_name_prefix = str(
        run_spec.generic_parameters.get("instance_name_prefix") or run_spec.run_id
    ).strip()
    command = [
        python_bin,
        "scripts/cold-start-benchmark.py",
        base_url,
        "--api-key",
        api_key,
        "--provider",
        run_spec.provider or "runpod",
        "--engine",
        run_spec.engine_id,
        "--gpu-type",
        run_spec.hardware_id,
        "--gpu-count",
        str(run_spec.gpu_count),
        "--model",
        run_spec.model_id,
        "--instance-name",
        f"{instance_name_prefix}-cold",
        "--json-output",
        str(output_path),
        "--timeout-s",
        str(lifecycle_timeout_s),
    ]
    if run_spec.provider_gpu_type_id:
        command.extend(["--provider-gpu-type-id", run_spec.provider_gpu_type_id])
    for version in run_spec.allowed_cuda_versions:
        command.extend(["--allowed-cuda-version", version])
    command.extend(runtime_options_to_cli(run_spec.runtime_options))
    if health_insecure:
        command.append("--health-insecure")
    if quiet_progress:
        command.append("--quiet-progress")
    if terminate_final_instance:
        command.append("--terminate-final-instance")
    return command


def build_startup_health_command(
    run_spec: ResolvedRunSpec,
    *,
    python_bin: str,
    base_url: str,
    api_key: str,
    output_path: Path,
    lifecycle_timeout_s: int,
    health_insecure: bool,
    quiet_progress: bool,
    terminate_final_instance: bool,
) -> list[str]:
    instance_name_prefix = str(
        run_spec.generic_parameters.get("instance_name_prefix") or run_spec.run_id
    ).strip()
    command = [
        python_bin,
        "scripts/capture-startup-health.py",
        base_url,
        "--api-key",
        api_key,
        "--provider",
        run_spec.provider or "runpod",
        "--engine",
        run_spec.engine_id,
        "--gpu-type",
        run_spec.hardware_id,
        "--gpu-count",
        str(run_spec.gpu_count),
        "--model",
        run_spec.model_id,
        "--instance-name",
        f"{instance_name_prefix}-startup",
        "--include-restart",
        "--json-output",
        str(output_path),
        "--timeout-s",
        str(lifecycle_timeout_s),
    ]
    if run_spec.provider_gpu_type_id:
        command.extend(["--provider-gpu-type-id", run_spec.provider_gpu_type_id])
    for version in run_spec.allowed_cuda_versions:
        command.extend(["--allowed-cuda-version", version])
    command.extend(runtime_options_to_cli(run_spec.runtime_options))
    if health_insecure:
        command.append("--health-insecure")
    if quiet_progress:
        command.append("--quiet-progress")
    if terminate_final_instance:
        command.append("--terminate-final-instance")
    return command


def should_terminate_after_cold_start(*, terminate_final_instance: bool, stages: list[str]) -> bool:
    if not terminate_final_instance:
        return False
    if "startup_health" in stages:
        return True
    return "warm_none" not in stages and "warm_affinity" not in stages


def should_terminate_after_startup(*, terminate_final_instance: bool, stages: list[str]) -> bool:
    if not terminate_final_instance:
        return False
    return "warm_none" not in stages and "warm_affinity" not in stages


def run_step(step: ExecutionStep, *, dry_run: bool) -> ExecutionStepResult:
    started_at = utc_now_iso()
    if dry_run:
        return ExecutionStepResult(
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
    finished_at = utc_now_iso()
    duration_ms = int((time.perf_counter() - start_perf) * 1000)
    return ExecutionStepResult(
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


def wait_for_warm_registration(
    *,
    health_url: str,
    timeout_s: int,
    insecure: bool,
    log_prefix: str,
) -> None:
    started = time.time()
    deadline = started + timeout_s
    next_log_at = started
    last_error: Exception | None = None
    last_state: str | None = None

    while time.time() < deadline:
        try:
            payload = fetch_health(health_url, insecure=insecure)
            last_error = None
        except Exception as exc:
            last_error = exc
            if time.time() >= next_log_at:
                print(
                    f"[{log_prefix}] warm readiness {health_url}: waiting... {time.time() - started:.1f}s "
                    f"({summarize_health_poll_error(exc)})",
                    flush=True,
                )
                next_log_at = time.time() + PROGRESS_LOG_INTERVAL_S
            time.sleep(2.0)
            continue

        ready = payload.get("ready") is True
        gateway_registered = payload.get("gateway_registered") is True
        state = f"ready={payload.get('ready')} gateway_registered={payload.get('gateway_registered')} state={payload.get('state')}"
        last_state = state
        if ready and gateway_registered:
            print(
                f"[{log_prefix}] warm readiness {health_url}: ready after {time.time() - started:.1f}s ({state})",
                flush=True,
            )
            return
        if time.time() >= next_log_at:
            print(
                f"[{log_prefix}] warm readiness {health_url}: waiting... {time.time() - started:.1f}s ({state})",
                flush=True,
            )
            next_log_at = time.time() + PROGRESS_LOG_INTERVAL_S
        time.sleep(2.0)

    if last_error is not None:
        raise RuntimeError(
            f"timed out waiting for retained worker registration before warm runs; last error: {last_error}"
        ) from last_error
    raise RuntimeError(
        "timed out waiting for retained worker registration before warm runs"
        + (f"; last state: {last_state}" if last_state else "")
    )


def wait_for_model_registry_drain(
    *,
    base_url: str,
    api_key: str,
    model_id: str,
    timeout_s: int,
    poll_interval_ms: int,
    log_prefix: str,
) -> None:
    started = time.time()
    deadline = started + timeout_s
    next_log_at = started
    last_error: Exception | None = None
    last_state: str | None = None

    while time.time() < deadline:
        try:
            payload = fetch_gateway_workers(base_url, api_key)
            last_error = None
        except Exception as exc:
            last_error = exc
            if time.time() >= next_log_at:
                print(
                    f"[{log_prefix}] gateway drain for model={model_id}: waiting... {time.time() - started:.1f}s "
                    f"({exc})",
                    flush=True,
                )
                next_log_at = time.time() + PROGRESS_LOG_INTERVAL_S
            time.sleep(poll_interval_ms / 1000.0)
            continue

        workers = payload.get("workers") or []
        matching = [
            worker
            for worker in workers
            if model_id in [str(model) for model in (worker.get("models") or [])]
        ]
        addresses = [
            str(worker.get("address") or "") for worker in matching if worker.get("address")
        ]
        last_state = f"matching_workers={len(matching)} total={payload.get('total')} addresses={addresses[:3]}"
        if not matching:
            print(
                f"[{log_prefix}] gateway drain for model={model_id}: ready after {time.time() - started:.1f}s "
                f"({last_state})",
                flush=True,
            )
            return
        if time.time() >= next_log_at:
            print(
                f"[{log_prefix}] gateway drain for model={model_id}: waiting... {time.time() - started:.1f}s "
                f"({last_state})",
                flush=True,
            )
            next_log_at = time.time() + PROGRESS_LOG_INTERVAL_S
        time.sleep(poll_interval_ms / 1000.0)

    if last_error is not None:
        raise RuntimeError(
            f"timed out waiting for gateway drain; last error: {last_error}"
        ) from last_error
    raise RuntimeError(
        "timed out waiting for gateway drain"
        + (f"; last state: {last_state}" if last_state else "")
    )


class ProvisionExecutor:
    """Execute a resolved run by provisioning a fresh worker lifecycle."""

    def __init__(
        self,
        *,
        base_url: str,
        api_key: str,
        workload_file: Path,
        python_bin: str | None = None,
        cost_per_hour: float | None = None,
        health_insecure: bool = False,
        quiet_progress: bool = False,
        terminate_final_instance: bool = False,
        dry_run: bool = False,
        continue_on_error: bool = False,
    ):
        self.base_url = base_url
        self.api_key = api_key
        self.workload_file = workload_file
        self.python_bin = python_bin or sys.executable
        self.cost_per_hour = cost_per_hour
        self.health_insecure = health_insecure
        self.quiet_progress = quiet_progress
        self.terminate_final_instance = terminate_final_instance
        self.dry_run = dry_run
        self.continue_on_error = continue_on_error

    def build_steps(self, run_spec: ResolvedRunSpec, benchmark_profile) -> list[ExecutionStep]:
        output_dir = Path(run_spec.output_dir).expanduser()
        output_dir.mkdir(parents=True, exist_ok=True)
        steps: list[ExecutionStep] = []
        stages = list(benchmark_profile.stages)
        engine_slug = run_spec.engine_id.replace("_", "-")
        hardware_slug = run_spec.hardware_id.lower().replace("_", "-")
        if "cold_start" in stages:
            cold_path = output_dir / f"cold-start-{engine_slug}-{hardware_slug}.json"
            steps.append(
                ExecutionStep(
                    name="cold_start",
                    category="cold_start",
                    output_path=str(cold_path),
                    command=build_cold_start_command(
                        run_spec,
                        python_bin=self.python_bin,
                        base_url=self.base_url,
                        api_key=self.api_key,
                        output_path=cold_path,
                        lifecycle_timeout_s=benchmark_profile.lifecycle_timeout_s,
                        health_insecure=self.health_insecure,
                        quiet_progress=self.quiet_progress,
                        terminate_final_instance=should_terminate_after_cold_start(
                            terminate_final_instance=self.terminate_final_instance,
                            stages=stages,
                        ),
                    ),
                )
            )
        if "startup_health" in stages:
            startup_path = output_dir / f"startup-health-{engine_slug}-{hardware_slug}.json"
            steps.append(
                ExecutionStep(
                    name="startup_health",
                    category="startup_health",
                    output_path=str(startup_path),
                    command=build_startup_health_command(
                        run_spec,
                        python_bin=self.python_bin,
                        base_url=self.base_url,
                        api_key=self.api_key,
                        output_path=startup_path,
                        lifecycle_timeout_s=benchmark_profile.lifecycle_timeout_s,
                        health_insecure=self.health_insecure,
                        quiet_progress=self.quiet_progress,
                        terminate_final_instance=should_terminate_after_startup(
                            terminate_final_instance=self.terminate_final_instance,
                            stages=stages,
                        ),
                    ),
                )
            )
        for stage_name, cache_mode in (("warm_none", "none"), ("warm_affinity", "affinity")):
            if stage_name not in stages:
                continue
            if cache_mode not in {"none", "affinity"}:
                continue
            warm_path = (
                output_dir / f"infera-benchmark-{engine_slug}-{hardware_slug}-{cache_mode}.json"
            )
            steps.append(
                ExecutionStep(
                    name=stage_name,
                    category="warm",
                    output_path=str(warm_path),
                    command=[],
                )
            )
        return steps

    def execute(self, run_spec: ResolvedRunSpec, benchmark_profile) -> ExperimentExecutionResult:
        manifest_path = Path(run_spec.output_dir).expanduser() / f"{run_spec.run_id}-manifest.json"
        if run_spec.compatibility_status in {"invalid", "unsupported"}:
            result = ExperimentExecutionResult(
                run_spec=run_spec,
                generated_at=utc_now_iso(),
                status="skipped",
                manifest_path=str(manifest_path),
                notes=[issue.message for issue in run_spec.compatibility_issues],
            )
            write_json_output(manifest_path, result.model_dump())
            return result

        step_results: list[ExecutionStepResult] = []
        notes = [
            "Warm benchmark results are only valid when the active fleet serving the target model is deployed with the selected engine.",
        ]
        steps = self.build_steps(run_spec, benchmark_profile)
        retained_health_url: str | None = (
            run_spec.attach_target.health_url if run_spec.attach_target else None
        )
        retained_instance_id: str | None = None
        exit_status = "ok"
        skipped_warm_due_to_lifecycle = False

        for index, step in enumerate(steps):
            if step.category == "warm":
                if step.name == "warm_affinity" and "affinity" not in {"none", "affinity"}:
                    continue
                if retained_health_url is None:
                    for previous in reversed(step_results):
                        if (
                            previous.category in {"cold_start", "startup_health"}
                            and previous.status == "ok"
                        ):
                            retained_health_url = retained_health_url_for_step(
                                previous.name, previous.output_path
                            )
                            retained_instance_id = retained_instance_id_for_step(
                                previous.name, previous.output_path
                            )
                            if retained_health_url:
                                break
                if retained_health_url is None:
                    skipped_warm_due_to_lifecycle = True
                    exit_status = "failed" if exit_status == "ok" else exit_status
                    step_results.append(
                        ExecutionStepResult(
                            name=step.name,
                            category=step.category,
                            output_path=step.output_path,
                            command=step.command,
                            command_display="",
                            started_at=utc_now_iso(),
                            finished_at=utc_now_iso(),
                            duration_ms=0,
                            returncode=None,
                            status="skipped",
                        )
                    )
                    skip_note = (
                        "Skipped warm benchmark steps because no successful lifecycle step produced "
                        "a retained health URL."
                    )
                    if skip_note not in notes:
                        notes.append(skip_note)
                    continue
                if retained_health_url:
                    wait_for_warm_registration(
                        health_url=retained_health_url,
                        timeout_s=benchmark_profile.warm_ready_timeout_s,
                        insecure=self.health_insecure,
                        log_prefix="infera-bench",
                    )
                cache_mode = "affinity" if step.name == "warm_affinity" else "none"
                step.command = build_warm_command(
                    run_spec,
                    python_bin=self.python_bin,
                    base_url=self.base_url,
                    api_key=self.api_key,
                    workload_file=self.workload_file,
                    cache_reuse_mode=cache_mode,
                    output_path=Path(step.output_path),
                    cost_per_hour=self.cost_per_hour,
                    health_url=retained_health_url,
                    health_insecure=self.health_insecure,
                    health_sample_interval_ms=benchmark_profile.health_sample_interval_ms,
                )

            print(f"[infera-bench] step={step.name} output={step.output_path}", flush=True)
            print(f"[infera-bench] command={shlex.join(step.command)}", flush=True)
            result = run_step(step, dry_run=self.dry_run)
            step_results.append(result)
            if result.status == "failed":
                exit_status = "failed"
                if not self.continue_on_error:
                    break
            if (
                result.status == "ok"
                and step.category in {"cold_start", "startup_health"}
                and any(candidate.category == "warm" for candidate in steps[index + 1 :])
                and not any(
                    candidate.category in {"cold_start", "startup_health"}
                    for candidate in steps[index + 1 :]
                )
            ):
                retained_health_url = retained_health_url_for_step(step.name, step.output_path)
                retained_instance_id = retained_instance_id_for_step(step.name, step.output_path)

        if skipped_warm_due_to_lifecycle and retained_instance_id is None:
            retained_instance_id = next(
                (
                    retained_instance_id_for_step(previous.name, previous.output_path)
                    for previous in reversed(step_results)
                    if previous.category in {"cold_start", "startup_health"}
                ),
                None,
            )

        warm_steps_ran = any(step.category == "warm" for step in steps)
        if (
            not self.dry_run
            and self.terminate_final_instance
            and warm_steps_ran
            and retained_instance_id
        ):
            try:
                print(
                    f"[infera-bench] terminating retained lifecycle instance instance_id={retained_instance_id}",
                    flush=True,
                )
                terminate_instance(self.base_url, self.api_key, retained_instance_id)
            except Exception as exc:
                exit_status = "blocked"
                notes.append(
                    f"Failed to terminate retained lifecycle instance {retained_instance_id}: {exc}"
                )

        execution = ExperimentExecutionResult(
            run_spec=run_spec,
            generated_at=utc_now_iso(),
            status=exit_status if not self.dry_run else "dry_run",
            manifest_path=str(manifest_path),
            steps=step_results,
            notes=notes,
        )
        write_json_output(manifest_path, execution.model_dump())
        return execution


class AttachExecutor:
    """Execute warm-only measurements against an existing deployment."""

    def __init__(
        self,
        *,
        base_url: str,
        api_key: str,
        workload_file: Path,
        python_bin: str | None = None,
        cost_per_hour: float | None = None,
        health_insecure: bool = False,
        dry_run: bool = False,
    ):
        self.base_url = base_url
        self.api_key = api_key
        self.workload_file = workload_file
        self.python_bin = python_bin or sys.executable
        self.cost_per_hour = cost_per_hour
        self.health_insecure = health_insecure
        self.dry_run = dry_run

    def execute(self, run_spec: ResolvedRunSpec, benchmark_profile) -> ExperimentExecutionResult:
        manifest_path = Path(run_spec.output_dir).expanduser() / f"{run_spec.run_id}-manifest.json"
        output_dir = Path(run_spec.output_dir).expanduser()
        output_dir.mkdir(parents=True, exist_ok=True)
        steps: list[ExecutionStep] = []
        for stage_name, cache_mode in (("warm_none", "none"), ("warm_affinity", "affinity")):
            if stage_name not in benchmark_profile.stages:
                continue
            output_path = output_dir / f"{run_spec.run_id}-{cache_mode}.json"
            steps.append(
                ExecutionStep(
                    name=stage_name,
                    category="warm",
                    output_path=str(output_path),
                    command=build_warm_command(
                        run_spec,
                        python_bin=self.python_bin,
                        base_url=self.base_url,
                        api_key=self.api_key,
                        workload_file=self.workload_file,
                        cache_reuse_mode=cache_mode,
                        output_path=output_path,
                        cost_per_hour=self.cost_per_hour,
                        health_url=run_spec.attach_target.health_url
                        if run_spec.attach_target
                        else None,
                        health_insecure=self.health_insecure,
                        health_sample_interval_ms=benchmark_profile.health_sample_interval_ms,
                    ),
                )
            )
        step_results = [run_step(step, dry_run=self.dry_run) for step in steps]
        status = "ok"
        if any(step.status == "failed" for step in step_results):
            status = "failed"
        execution = ExperimentExecutionResult(
            run_spec=run_spec,
            generated_at=utc_now_iso(),
            status=status if not self.dry_run else "dry_run",
            manifest_path=str(manifest_path),
            steps=step_results,
        )
        write_json_output(manifest_path, execution.model_dump())
        return execution
