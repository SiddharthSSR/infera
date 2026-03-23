#!/usr/bin/env python3
"""Automate cold-start benchmark scenarios for Infera."""

from __future__ import annotations

import argparse
from dataclasses import asdict, dataclass, field
from datetime import datetime, timezone
import json
from pathlib import Path
import shutil
import ssl
import subprocess
import sys
import time
from typing import Any
import urllib.error
import urllib.request


DEFAULT_BASE_URL = "https://inferai.co.in"
DEFAULT_HEALTH_TEMPLATE = "https://{provider_id}-8081.proxy.runpod.net/health"
DEFAULT_POLL_INTERVAL_MS = 2000
DEFAULT_TIMEOUT_S = 900
PROBE_PROMPT = "Reply with the single word OK."
PROGRESS_LOG_INTERVAL_S = 15.0


@dataclass
class ScenarioTimes:
    t0_request_sent: int
    t1_instance_running: int | None = None
    t2_server_started: int | None = None
    t3_model_load_finished: int | None = None
    t4_worker_registered: int | None = None
    t5_first_successful_completion: int | None = None


@dataclass
class ProbeResult:
    total_ms: float
    prompt_tokens: int
    completion_tokens: int
    total_tokens: int
    content: str


@dataclass
class ScenarioResult:
    scenario: str
    instance_id: str
    provider_id: str
    worker_id: str | None
    worker_address: str | None
    health_url: str | None
    times: ScenarioTimes
    durations_ms: dict[str, int] = field(default_factory=dict)
    probe: ProbeResult | None = None
    health_snapshot: dict[str, Any] | None = None
    notes: list[str] = field(default_factory=list)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Run automated cold-start benchmark scenarios.")
    parser.add_argument(
        "base_url",
        nargs="?",
        default=DEFAULT_BASE_URL,
        help="Gateway base URL (default: %(default)s)",
    )
    parser.add_argument("--api-key", required=True, help="Gateway bearer token")
    parser.add_argument("--provider", default="runpod", help="Provider type (default: %(default)s)")
    parser.add_argument("--gpu-type", required=True, help="Infera GPU type, e.g. A100_80GB")
    parser.add_argument("--provider-gpu-type-id", default="", help="Exact provider GPU type identifier")
    parser.add_argument("--gpu-count", type=int, default=1, help="GPU count (default: %(default)s)")
    parser.add_argument("--model", required=True, help="Model ID to preload and probe")
    parser.add_argument("--instance-name", default="cold-start-bench", help="Instance name (default: %(default)s)")
    parser.add_argument(
        "--health-url-template",
        default=DEFAULT_HEALTH_TEMPLATE,
        help="Worker health URL template with {provider_id}, {instance_id}, {http_port}, {public_ip}",
    )
    parser.add_argument(
        "--poll-interval-ms",
        type=int,
        default=DEFAULT_POLL_INTERVAL_MS,
        help="Polling interval in milliseconds (default: %(default)s)",
    )
    parser.add_argument(
        "--timeout-s",
        type=int,
        default=DEFAULT_TIMEOUT_S,
        help="Timeout per scenario stage in seconds (default: %(default)s)",
    )
    parser.add_argument(
        "--probe-max-tokens",
        type=int,
        default=32,
        help="Max tokens for the first-success probe request (default: %(default)s)",
    )
    parser.add_argument(
        "--probe-temperature",
        type=float,
        default=0.0,
        help="Temperature for the first-success probe request (default: %(default)s)",
    )
    parser.add_argument(
        "--terminate-final-instance",
        action="store_true",
        help="Terminate the reused instance after all scenarios complete",
    )
    parser.add_argument(
        "--json-output",
        default=None,
        help="Optional path to write the full JSON report",
    )
    parser.add_argument(
        "--quiet-progress",
        action="store_true",
        help="Suppress live progress logs and only print the final summary",
    )
    parser.add_argument(
        "--health-insecure",
        action="store_true",
        help="Disable TLS verification for worker health URL polling",
    )
    return parser.parse_args()


def now_ms() -> int:
    return int(time.time() * 1000)


def parse_stage_timestamp_ms(value: str | None) -> int | None:
    if not value:
        return None
    try:
        parsed = datetime.fromisoformat(value)
    except ValueError:
        return None
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=timezone.utc)
    return int(parsed.timestamp() * 1000)


def log_progress(args: argparse.Namespace, message: str) -> None:
    if args.quiet_progress:
        return
    print(f"[cold-start] {message}", flush=True)


def build_headers(api_key: str, *, content_type: bool = False) -> dict[str, str]:
    headers = {
        "Authorization": f"Bearer {api_key}",
        "User-Agent": "infera-cold-start-benchmark/1.0",
        "Accept": "application/json",
    }
    if content_type:
        headers["Content-Type"] = "application/json"
    return headers


def request_json(
    method: str,
    url: str,
    *,
    api_key: str | None = None,
    payload: dict[str, Any] | None = None,
    timeout: int = 60,
    insecure: bool = False,
) -> dict[str, Any]:
    data = None
    headers: dict[str, str] = {}
    if api_key:
        headers.update(build_headers(api_key, content_type=payload is not None))
    else:
        headers["User-Agent"] = "infera-cold-start-benchmark/1.0"
        headers["Accept"] = "application/json"
        if payload is not None:
            headers["Content-Type"] = "application/json"

    if payload is not None:
        data = json.dumps(payload).encode("utf-8")

    request = urllib.request.Request(url, data=data, headers=headers, method=method)
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
    payload: dict[str, Any] | None = None,
    timeout: int = 60,
    insecure: bool = False,
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
        "User-Agent: infera-cold-start-benchmark/1.0",
        "-H",
        "Accept: application/json",
    ]
    if insecure and url.startswith("https://"):
        cmd.append("-k")
    if payload is not None:
        cmd.extend(["-H", "Content-Type: application/json", "-d", json.dumps(payload)])
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


def build_provision_payload(args: argparse.Namespace) -> dict[str, Any]:
    payload = {
        "name": args.instance_name,
        "provider": args.provider,
        "gpu_type": args.gpu_type,
        "gpu_count": args.gpu_count,
        "models": [args.model],
    }
    if args.provider_gpu_type_id:
        payload["provider_gpu_type_id"] = args.provider_gpu_type_id
    return payload


def format_health_url(template: str, instance: dict[str, Any]) -> str | None:
    if not template:
        return None
    provider_id = str(instance.get("provider_id") or "")
    instance_id = str(instance.get("id") or "")
    http_port = instance.get("http_port") or 8081
    public_ip = str(instance.get("public_ip") or "")
    if "{provider_id}" in template and not provider_id:
        return None
    return template.format(
        provider_id=provider_id,
        instance_id=instance_id,
        http_port=http_port,
        public_ip=public_ip,
    )


def wait_for_condition(
    fetch_fn,
    predicate_fn,
    *,
    description: str,
    timeout_s: int,
    poll_interval_ms: int,
    args: argparse.Namespace,
    summarize_fn=None,
) -> tuple[int, Any]:
    deadline = time.time() + timeout_s
    last_error: Exception | None = None
    started = time.time()
    next_log_at = started
    last_summary: str | None = None
    while time.time() < deadline:
        try:
            value = fetch_fn()
            if predicate_fn(value):
                elapsed = time.time() - started
                summary = summarize_fn(value) if summarize_fn is not None else None
                suffix = f" ({summary})" if summary else ""
                log_progress(args, f"{description}: ready after {elapsed:.1f}s{suffix}")
                return now_ms(), value
            summary = summarize_fn(value) if summarize_fn is not None else None
            if time.time() >= next_log_at:
                suffix = f" ({summary})" if summary else ""
                log_progress(args, f"{description}: waiting... {time.time() - started:.1f}s{suffix}")
                next_log_at = time.time() + PROGRESS_LOG_INTERVAL_S
            last_summary = summary
        except Exception as exc:  # pragma: no cover - exercised via polling loops
            last_error = exc
            if time.time() >= next_log_at:
                log_progress(args, f"{description}: waiting... {time.time() - started:.1f}s (last error: {exc})")
                next_log_at = time.time() + PROGRESS_LOG_INTERVAL_S
        time.sleep(poll_interval_ms / 1000.0)
    if last_error is not None:
        raise RuntimeError(f"timed out waiting for condition; last error: {last_error}") from last_error
    suffix = f"; last state: {last_summary}" if last_summary else ""
    raise RuntimeError(f"timed out waiting for condition: {description}{suffix}")


def fetch_instance(base_url: str, api_key: str, instance_id: str) -> dict[str, Any]:
    return request_json(
        "GET",
        f"{base_url.rstrip('/')}/api/instances/{instance_id}",
        api_key=api_key,
    )


def fetch_workers(base_url: str, api_key: str) -> dict[str, Any]:
    return request_json("GET", f"{base_url.rstrip('/')}/api/workers", api_key=api_key)


def wait_for_instance_status(
    base_url: str,
    api_key: str,
    instance_id: str,
    expected_status: str,
    *,
    args: argparse.Namespace,
    timeout_s: int,
    poll_interval_ms: int,
) -> tuple[int, dict[str, Any]]:
    return wait_for_condition(
        lambda: fetch_instance(base_url, api_key, instance_id),
        lambda payload: payload.get("status") == expected_status,
        description=f"instance {instance_id} status={expected_status}",
        timeout_s=timeout_s,
        poll_interval_ms=poll_interval_ms,
        args=args,
        summarize_fn=lambda payload: f"status={payload.get('status')} worker_id={payload.get('worker_id') or '-'}",
    )


def fetch_health(health_url: str, *, timeout: int, insecure: bool) -> dict[str, Any]:
    try:
        return request_json("GET", health_url, timeout=timeout, insecure=insecure)
    except Exception as exc:
        if "HTTP 403" not in str(exc) or "1010" not in str(exc):
            raise
    return request_json_via_curl("GET", health_url, timeout=timeout, insecure=insecure)


def wait_for_health_stages(
    health_url: str | None,
    *,
    timeout_s: int,
    poll_interval_ms: int,
    args: argparse.Namespace,
) -> tuple[int | None, int | None, dict[str, Any] | None, list[str]]:
    if not health_url:
        return None, None, None, ["health_url unavailable"]

    deadline = time.time() + timeout_s
    t2: int | None = None
    t3: int | None = None
    latest_payload: dict[str, Any] | None = None
    notes: list[str] = []
    started = time.time()
    next_log_at = started
    logged_t2 = False
    logged_t3 = False
    last_error: Exception | None = None

    while time.time() < deadline:
        try:
            payload = fetch_health(health_url, timeout=15, insecure=args.health_insecure)
            latest_payload = payload
            last_error = None
        except Exception:
            last_error = sys.exc_info()[1]
            if "HTTP 403" in str(last_error) and "1010" in str(last_error):
                notes.append("worker health polling blocked by upstream access policy (HTTP 403 / error code 1010)")
                log_progress(
                    args,
                    f"worker health {health_url}: blocked by upstream access policy; skipping T2/T3 capture",
                )
                break
            if time.time() >= next_log_at:
                log_progress(
                    args,
                    f"worker health {health_url}: waiting... {time.time() - started:.1f}s "
                    f"(last error: {last_error})",
                )
                next_log_at = time.time() + PROGRESS_LOG_INTERVAL_S
            time.sleep(poll_interval_ms / 1000.0)
            continue

        startup = payload.get("startup") or {}
        stages = startup.get("stages") or {}
        if t2 is None and stages.get("server_started"):
            t2 = parse_stage_timestamp_ms(stages.get("server_started")) or now_ms()
            logged_t2 = True
            log_progress(args, f"worker health {health_url}: observed server_started at {t2}")
        if (
            t3 is None
            and stages.get("model_load_finished")
            and payload.get("ready") is True
        ):
            t3 = parse_stage_timestamp_ms(stages.get("model_load_finished")) or now_ms()
            logged_t3 = True
            log_progress(args, f"worker health {health_url}: observed model_load_finished and ready at {t3}")
            break

        if payload.get("ready") is True and not stages:
            notes.append("worker health did not expose startup stages on live image")
            log_progress(args, f"worker health {health_url}: ready=true but startup stages unavailable")
            break

        if time.time() >= next_log_at:
            log_progress(
                args,
                "worker health "
                f"{health_url}: waiting... {time.time() - started:.1f}s "
                f"(ready={payload.get('ready')} live={payload.get('live')} state={payload.get('state')})",
            )
            next_log_at = time.time() + PROGRESS_LOG_INTERVAL_S

        time.sleep(poll_interval_ms / 1000.0)

    if t2 is None and not logged_t2:
        suffix = f" last_error={last_error}" if last_error is not None else ""
        log_progress(args, f"worker health {health_url}: server_started not observed.{suffix}")
    if t3 is None and not logged_t3:
        suffix = f" last_error={last_error}" if last_error is not None else ""
        log_progress(args, f"worker health {health_url}: model_load_finished not observed.{suffix}")

    return t2, t3, latest_payload, notes


def match_worker(workers_payload: dict[str, Any], instance: dict[str, Any]) -> dict[str, Any] | None:
    provider_id = str(instance.get("provider_id") or "")
    instance_worker_id = str(instance.get("worker_id") or "")
    for worker in workers_payload.get("workers") or []:
        address = str(worker.get("address") or "")
        worker_id = str(worker.get("worker_id") or "")
        if provider_id and provider_id in address:
            return worker
        if instance_worker_id and worker_id == instance_worker_id:
            return worker
    return None


def wait_for_worker_registration(
    base_url: str,
    api_key: str,
    instance: dict[str, Any],
    *,
    args: argparse.Namespace,
    timeout_s: int,
    poll_interval_ms: int,
) -> tuple[int, dict[str, Any]]:
    return wait_for_condition(
        lambda: fetch_workers(base_url, api_key),
        lambda payload: match_worker(payload, instance) is not None,
        description=f"worker registration for instance {instance.get('id')}",
        timeout_s=timeout_s,
        poll_interval_ms=poll_interval_ms,
        args=args,
        summarize_fn=lambda payload: f"workers={payload.get('total')}",
    )


def run_first_success_probe(
    base_url: str,
    api_key: str,
    model: str,
    *,
    max_tokens: int,
    temperature: float,
    timeout: int,
    args: argparse.Namespace,
) -> ProbeResult:
    payload = {
        "model": model,
        "messages": [{"role": "user", "content": PROBE_PROMPT}],
        "max_tokens": max_tokens,
        "temperature": temperature,
    }
    log_progress(args, f"first-success probe: sending completion request for model={model}")
    started = time.perf_counter()
    response = request_json(
        "POST",
        f"{base_url.rstrip('/')}/v1/chat/completions",
        api_key=api_key,
        payload=payload,
        timeout=timeout,
    )
    total_ms = (time.perf_counter() - started) * 1000.0
    usage = response.get("usage") or {}
    choices = response.get("choices") or []
    message = (choices[0].get("message") if choices else {}) or {}
    return ProbeResult(
        total_ms=total_ms,
        prompt_tokens=int(usage.get("prompt_tokens") or 0),
        completion_tokens=int(usage.get("completion_tokens") or 0),
        total_tokens=int(usage.get("total_tokens") or 0),
        content=str(message.get("content") or ""),
    )


def stop_instance(base_url: str, api_key: str, instance_id: str) -> dict[str, Any]:
    return request_json(
        "POST",
        f"{base_url.rstrip('/')}/api/instances/{instance_id}/stop",
        api_key=api_key,
    )


def start_instance(base_url: str, api_key: str, instance_id: str) -> dict[str, Any]:
    return request_json(
        "POST",
        f"{base_url.rstrip('/')}/api/instances/{instance_id}/start",
        api_key=api_key,
    )


def terminate_instance(base_url: str, api_key: str, instance_id: str) -> dict[str, Any]:
    return request_json(
        "DELETE",
        f"{base_url.rstrip('/')}/api/instances/{instance_id}",
        api_key=api_key,
    )


def compute_durations(times: ScenarioTimes) -> dict[str, int]:
    durations: dict[str, int] = {}
    if times.t1_instance_running is not None:
        durations["request_to_running_ms"] = times.t1_instance_running - times.t0_request_sent
    if times.t2_server_started is not None:
        durations["request_to_server_started_ms"] = times.t2_server_started - times.t0_request_sent
    if times.t2_server_started is not None and times.t1_instance_running is not None:
        running_to_server_started_ms = times.t2_server_started - times.t1_instance_running
        if running_to_server_started_ms >= 0:
            durations["running_to_server_started_ms"] = running_to_server_started_ms
    if times.t3_model_load_finished is not None and times.t2_server_started is not None:
        durations["server_to_model_ready_ms"] = times.t3_model_load_finished - times.t2_server_started
    if times.t4_worker_registered is not None:
        durations["request_to_registered_ms"] = times.t4_worker_registered - times.t0_request_sent
    if times.t5_first_successful_completion is not None:
        durations["request_to_first_success_ms"] = times.t5_first_successful_completion - times.t0_request_sent
    if times.t5_first_successful_completion is not None and times.t4_worker_registered is not None:
        durations["registered_to_first_success_ms"] = (
            times.t5_first_successful_completion - times.t4_worker_registered
        )
    return durations


def run_ready_path(
    *,
    scenario_name: str,
    base_url: str,
    api_key: str,
    model: str,
    instance: dict[str, Any],
    t0: int,
    args: argparse.Namespace,
) -> ScenarioResult:
    instance_id = str(instance["id"])
    provider_id = str(instance.get("provider_id") or "")
    times = ScenarioTimes(t0_request_sent=t0)
    log_progress(
        args,
        f"{scenario_name}: begin for instance_id={instance_id} provider_id={provider_id or '-'}",
    )

    t1, refreshed_instance = wait_for_instance_status(
        base_url,
        api_key,
        instance_id,
        "running",
        args=args,
        timeout_s=args.timeout_s,
        poll_interval_ms=args.poll_interval_ms,
    )
    times.t1_instance_running = t1
    log_progress(args, f"{scenario_name}: T1 instance_running={t1}")

    health_url = format_health_url(args.health_url_template, refreshed_instance)
    log_progress(args, f"{scenario_name}: health_url={health_url or 'unavailable'}")
    t2, t3, health_snapshot, notes = wait_for_health_stages(
        health_url,
        args=args,
        timeout_s=args.timeout_s,
        poll_interval_ms=args.poll_interval_ms,
    )
    times.t2_server_started = t2
    times.t3_model_load_finished = t3
    log_progress(args, f"{scenario_name}: T2={t2} T3={t3}")

    t4, workers_payload = wait_for_worker_registration(
        base_url,
        api_key,
        refreshed_instance,
        args=args,
        timeout_s=args.timeout_s,
        poll_interval_ms=args.poll_interval_ms,
    )
    times.t4_worker_registered = t4
    worker = match_worker(workers_payload, refreshed_instance)
    log_progress(
        args,
        f"{scenario_name}: T4 worker_registered={t4} worker_id={str((worker or {}).get('worker_id') or '-')}",
    )

    probe = run_first_success_probe(
        base_url,
        api_key,
        model,
        max_tokens=args.probe_max_tokens,
        temperature=args.probe_temperature,
        timeout=args.timeout_s,
        args=args,
    )
    times.t5_first_successful_completion = now_ms()
    log_progress(args, f"{scenario_name}: T5 first_successful_completion={times.t5_first_successful_completion}")

    return ScenarioResult(
        scenario=scenario_name,
        instance_id=instance_id,
        provider_id=provider_id,
        worker_id=str((worker or {}).get("worker_id") or "") or None,
        worker_address=str((worker or {}).get("address") or "") or None,
        health_url=health_url,
        times=times,
        durations_ms=compute_durations(times),
        probe=probe,
        health_snapshot=health_snapshot,
        notes=notes,
    )


def run_fresh_provision(args: argparse.Namespace) -> ScenarioResult:
    t0 = now_ms()
    log_progress(args, f"fresh_provision: T0 request_sent={t0}")
    log_progress(args, "fresh_provision: provisioning instance")
    response = request_json(
        "POST",
        f"{args.base_url.rstrip('/')}/api/instances/provision",
        api_key=args.api_key,
        payload=build_provision_payload(args),
        timeout=args.timeout_s,
    )
    instance = response["instance"]
    log_progress(
        args,
        "fresh_provision: provision accepted "
        f"instance_id={instance.get('id')} provider_id={instance.get('provider_id')}",
    )
    return run_ready_path(
        scenario_name="fresh_provision",
        base_url=args.base_url,
        api_key=args.api_key,
        model=args.model,
        instance=instance,
        t0=t0,
        args=args,
    )


def run_stopped_instance_start(args: argparse.Namespace, instance_id: str) -> ScenarioResult:
    log_progress(args, f"stopped_instance_start: stopping instance_id={instance_id}")
    stop_instance(args.base_url, args.api_key, instance_id)
    wait_for_instance_status(
        args.base_url,
        args.api_key,
        instance_id,
        "stopped",
        args=args,
        timeout_s=args.timeout_s,
        poll_interval_ms=args.poll_interval_ms,
    )

    t0 = now_ms()
    log_progress(args, f"stopped_instance_start: T0 request_sent={t0}")
    log_progress(args, f"stopped_instance_start: starting instance_id={instance_id}")
    start_instance(args.base_url, args.api_key, instance_id)
    instance = fetch_instance(args.base_url, args.api_key, instance_id)
    return run_ready_path(
        scenario_name="stopped_instance_start",
        base_url=args.base_url,
        api_key=args.api_key,
        model=args.model,
        instance=instance,
        t0=t0,
        args=args,
    )


def run_stopped_instance_reuse(
    args: argparse.Namespace,
    *,
    instance_id: str,
) -> ScenarioResult:
    log_progress(args, f"stopped_instance_reuse: stopping instance_id={instance_id}")
    stop_instance(args.base_url, args.api_key, instance_id)
    wait_for_instance_status(
        args.base_url,
        args.api_key,
        instance_id,
        "stopped",
        args=args,
        timeout_s=args.timeout_s,
        poll_interval_ms=args.poll_interval_ms,
    )

    t0 = now_ms()
    log_progress(args, f"stopped_instance_reuse: T0 request_sent={t0}")
    log_progress(args, "stopped_instance_reuse: provisioning matching instance for reuse")
    response = request_json(
        "POST",
        f"{args.base_url.rstrip('/')}/api/instances/provision",
        api_key=args.api_key,
        payload=build_provision_payload(args),
        timeout=args.timeout_s,
    )
    instance = response["instance"]
    log_progress(
        args,
        "stopped_instance_reuse: provision accepted "
        f"instance_id={instance.get('id')} provider_id={instance.get('provider_id')}",
    )
    result = run_ready_path(
        scenario_name="stopped_instance_reuse",
        base_url=args.base_url,
        api_key=args.api_key,
        model=args.model,
        instance=instance,
        t0=t0,
        args=args,
    )
    if result.instance_id != instance_id:
        result.notes.append(
            f"expected reuse of instance {instance_id}, got {result.instance_id}"
        )
    return result


def build_report(args: argparse.Namespace, scenarios: list[ScenarioResult]) -> dict[str, Any]:
    return {
        "base_url": args.base_url,
        "provider": args.provider,
        "gpu_type": args.gpu_type,
        "provider_gpu_type_id": args.provider_gpu_type_id,
        "gpu_count": args.gpu_count,
        "model": args.model,
        "instance_name": args.instance_name,
        "poll_interval_ms": args.poll_interval_ms,
        "timeout_s": args.timeout_s,
        "scenarios": [
            {
                **asdict(scenario),
                "probe": asdict(scenario.probe) if scenario.probe is not None else None,
            }
            for scenario in scenarios
        ],
    }


def write_json_output(path: str, payload: dict[str, Any]) -> Path:
    output_path = Path(path).expanduser().resolve()
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(json.dumps(payload, indent=2), encoding="utf-8")
    return output_path


def print_summary(scenarios: list[ScenarioResult]) -> None:
    for scenario in scenarios:
        print(f"[{scenario.scenario}]")
        print(f"  instance_id={scenario.instance_id} provider_id={scenario.provider_id}")
        if scenario.worker_id:
            print(f"  worker_id={scenario.worker_id} address={scenario.worker_address}")
        print(
            "  "
            + " ".join(
                [
                    f"T0={scenario.times.t0_request_sent}",
                    f"T1={scenario.times.t1_instance_running}",
                    f"T2={scenario.times.t2_server_started}",
                    f"T3={scenario.times.t3_model_load_finished}",
                    f"T4={scenario.times.t4_worker_registered}",
                    f"T5={scenario.times.t5_first_successful_completion}",
                ]
            )
        )
        for key, value in scenario.durations_ms.items():
            print(f"  {key}={value}")
        if scenario.probe is not None:
            print(
                f"  probe_total_ms={scenario.probe.total_ms:.2f} "
                f"prompt_tokens={scenario.probe.prompt_tokens} "
                f"completion_tokens={scenario.probe.completion_tokens}"
            )
        for note in scenario.notes:
            print(f"  note={note}")
        print()


def main() -> int:
    args = parse_args()
    scenarios: list[ScenarioResult] = []

    try:
        log_progress(
            args,
            f"benchmark start: provider={args.provider} gpu_type={args.gpu_type} model={args.model}",
        )
        fresh = run_fresh_provision(args)
        scenarios.append(fresh)

        started = run_stopped_instance_start(args, fresh.instance_id)
        scenarios.append(started)

        reused = run_stopped_instance_reuse(args, instance_id=fresh.instance_id)
        scenarios.append(reused)

        if args.terminate_final_instance:
            log_progress(args, f"terminating final instance_id={reused.instance_id}")
            terminate_instance(args.base_url, args.api_key, reused.instance_id)
            reused.notes.append("terminated final instance")
    except Exception as exc:
        if scenarios:
            print_summary(scenarios)
        print(f"error: {exc}", file=sys.stderr)
        return 1

    print_summary(scenarios)
    payload = build_report(args, scenarios)
    if args.json_output:
        output_path = write_json_output(args.json_output, payload)
        print(f"Wrote JSON report to {output_path}")
    else:
        print(json.dumps(payload, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
