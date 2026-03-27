#!/usr/bin/env python3
"""Capture worker startup health snapshots for fresh and restarted instances."""

from __future__ import annotations

import argparse
from dataclasses import asdict, dataclass
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
HEALTH_REQUEST_TIMEOUT_S = 5
PROGRESS_LOG_INTERVAL_S = 15.0
DEFAULT_TENSORRT_RUNPOD_ALLOWED_CUDA_VERSIONS = ("12.6", "12.7", "12.8")


@dataclass
class HealthCapture:
    label: str
    instance_id: str
    provider_id: str
    health_url: str | None
    t0_request_sent: int
    t1_instance_running: int | None
    t2_server_started: int | None
    t3_model_load_finished: int | None
    health_snapshot: dict[str, Any] | None
    notes: list[str]


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Capture worker /health startup snapshots.")
    parser.add_argument(
        "base_url",
        nargs="?",
        default=DEFAULT_BASE_URL,
        help="Gateway base URL (default: %(default)s)",
    )
    parser.add_argument("--api-key", required=True, help="Gateway bearer token")
    parser.add_argument("--provider", default="runpod", help="Provider type (default: %(default)s)")
    parser.add_argument("--engine", default="vllm", help="Inference engine to provision (default: %(default)s)")
    parser.add_argument("--gpu-type", required=True, help="Infera GPU type, e.g. A100_80GB")
    parser.add_argument("--provider-gpu-type-id", default="", help="Exact provider GPU type identifier")
    parser.add_argument(
        "--allowed-cuda-version",
        action="append",
        default=[],
        help="Allowed RunPod CUDA version. Can be repeated. Defaults to 12.6+ for TensorRT-LLM on RunPod.",
    )
    parser.add_argument(
        "--runtime-option",
        action="append",
        default=[],
        help="Provision runtime option in KEY=VALUE form. Can be repeated.",
    )
    parser.add_argument("--gpu-count", type=int, default=1, help="GPU count (default: %(default)s)")
    parser.add_argument("--model", required=True, help="Model ID to preload")
    parser.add_argument(
        "--instance-name",
        default="cache-probe-bench",
        help="Instance name (default: %(default)s)",
    )
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
        help="Timeout per stage in seconds (default: %(default)s)",
    )
    parser.add_argument(
        "--health-insecure",
        action="store_true",
        help="Disable TLS verification for worker health polling",
    )
    parser.add_argument(
        "--include-restart",
        action="store_true",
        help="Also stop/start the same instance and capture a restarted health snapshot",
    )
    parser.add_argument(
        "--terminate-final-instance",
        action="store_true",
        help="Terminate the instance after all captures complete",
    )
    parser.add_argument(
        "--json-output",
        default=None,
        help="Optional path to write the JSON report",
    )
    parser.add_argument(
        "--quiet-progress",
        action="store_true",
        help="Suppress live progress logs and only print the final summary",
    )
    return parser.parse_args()


def now_ms() -> int:
    return int(time.time() * 1000)


def parse_stage_timestamp_ms(value: str | None) -> int | None:
    if not value:
        return None
    normalized = value
    if normalized.endswith("Z"):
        normalized = normalized[:-1] + "+00:00"
    if "." in normalized:
        main, rest = normalized.split(".", 1)
        tz_sep = "+" if "+" in rest else "-" if "-" in rest else None
        if tz_sep is not None:
            frac, tz = rest.split(tz_sep, 1)
            normalized = f"{main}.{frac[:6]}{tz_sep}{tz}"
        else:
            normalized = f"{main}.{rest[:6]}"
    try:
        parsed = datetime.fromisoformat(normalized)
    except ValueError:
        return None
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=timezone.utc)
    return int(parsed.timestamp() * 1000)


def log_progress(args: argparse.Namespace, message: str) -> None:
    if not args.quiet_progress:
        print(f"[startup-health] {message}", flush=True)


def summarize_health_poll_error(exc: Exception) -> str:
    """Classify transient proxy/bootstrap errors so progress logs read as expected startup states."""
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


def fatal_health_payload_reason(payload: dict[str, Any] | None) -> str | None:
    """Return a fatal startup reason when the worker has already entered an error state."""
    if not isinstance(payload, dict):
        return None

    state = str(payload.get("state") or "")
    live = payload.get("live")
    if state != "error" and live is not False:
        return None

    startup = payload.get("startup") or {}
    metadata = startup.get("metadata") or {}
    gpu_preflight = metadata.get("gpu_preflight") or {}
    startup_error = metadata.get("startup_error") or {}

    if isinstance(gpu_preflight, dict) and gpu_preflight.get("status") == "failed":
        error_type = str(gpu_preflight.get("error_type") or "RuntimeError")
        error_message = str(gpu_preflight.get("error") or "unknown GPU runtime failure")
        return f"worker entered error state during startup: gpu_preflight failed: {error_type}: {error_message}"

    if isinstance(startup_error, dict) and startup_error.get("message"):
        error_type = str(startup_error.get("type") or "RuntimeError")
        error_message = str(startup_error.get("message") or "unknown startup failure")
        return f"worker entered error state during startup: {error_type}: {error_message}"

    return f"worker entered error state during startup: state={state or 'error'}"


def build_headers(api_key: str, *, content_type: bool = False) -> dict[str, str]:
    headers = {
        "Authorization": f"Bearer {api_key}",
        "User-Agent": "infera-startup-health/1.0",
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
        headers["User-Agent"] = "infera-startup-health/1.0"
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
        "User-Agent: infera-startup-health/1.0",
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


def fetch_health(health_url: str, *, timeout: int, insecure: bool) -> dict[str, Any]:
    try:
        return request_json("GET", health_url, timeout=timeout, insecure=insecure)
    except Exception as exc:
        if "HTTP 403" not in str(exc) or "1010" not in str(exc):
            raise
    return request_json_via_curl("GET", health_url, timeout=timeout, insecure=insecure)


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


def build_provision_payload(args: argparse.Namespace) -> dict[str, Any]:
    payload = {
        "name": args.instance_name,
        "provider": args.provider,
        "engine": args.engine,
        "gpu_type": args.gpu_type,
        "gpu_count": args.gpu_count,
        "models": [args.model],
    }
    if args.provider_gpu_type_id:
        payload["provider_gpu_type_id"] = args.provider_gpu_type_id
    if allowed_cuda_versions := resolve_allowed_cuda_versions(args):
        payload["allowed_cuda_versions"] = allowed_cuda_versions
    if runtime_options := resolve_runtime_options(args):
        payload["options"] = runtime_options
    return payload


def resolve_allowed_cuda_versions(args: argparse.Namespace) -> list[str]:
    explicit = [str(value).strip() for value in getattr(args, "allowed_cuda_version", []) if str(value).strip()]
    if explicit:
        return list(dict.fromkeys(explicit))
    if str(args.provider).strip().lower() == "runpod" and str(args.engine).strip().lower() == "tensorrt_llm":
        return list(DEFAULT_TENSORRT_RUNPOD_ALLOWED_CUDA_VERSIONS)
    return []


def resolve_runtime_options(args: argparse.Namespace) -> dict[str, str]:
    options: dict[str, str] = {}
    for raw_value in getattr(args, "runtime_option", []) or []:
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
        except Exception as exc:
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
    return request_json("GET", f"{base_url.rstrip('/')}/api/instances/{instance_id}", api_key=api_key)


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


def wait_for_health_ready(
    health_url: str | None,
    *,
    timeout_s: int,
    poll_interval_ms: int,
    args: argparse.Namespace,
) -> tuple[int | None, int | None, dict[str, Any] | None, list[str]]:
    if not health_url:
        return None, None, None, ["health_url unavailable"]

    deadline = time.time() + timeout_s
    latest_payload: dict[str, Any] | None = None
    notes: list[str] = []
    started = time.time()
    next_log_at = started
    last_error: Exception | None = None

    while time.time() < deadline:
        try:
            latest_payload = fetch_health(
                health_url,
                timeout=HEALTH_REQUEST_TIMEOUT_S,
                insecure=args.health_insecure,
            )
            last_error = None
        except Exception as exc:
            last_error = exc
            if "HTTP 403" in str(exc) and "1010" in str(exc):
                notes.append("worker health polling blocked by upstream access policy (HTTP 403 / error code 1010)")
                return None, None, None, notes
            if time.time() >= next_log_at:
                log_progress(
                    args,
                    "worker health "
                    f"{health_url}: waiting... {time.time() - started:.1f}s "
                    f"({summarize_health_poll_error(exc)})",
                )
                next_log_at = time.time() + PROGRESS_LOG_INTERVAL_S
            time.sleep(poll_interval_ms / 1000.0)
            continue

        startup = latest_payload.get("startup") or {}
        stages = startup.get("stages") or {}
        t2 = parse_stage_timestamp_ms(stages.get("server_started"))
        t3 = parse_stage_timestamp_ms(stages.get("model_load_finished"))
        fatal_reason = fatal_health_payload_reason(latest_payload)
        if fatal_reason:
            raise RuntimeError(fatal_reason)
        if latest_payload.get("ready") is True:
            log_progress(args, f"worker health {health_url}: ready=true")
            return t2, t3, latest_payload, notes

        if time.time() >= next_log_at:
            log_progress(
                args,
                "worker health "
                f"{health_url}: waiting... {time.time() - started:.1f}s "
                f"(ready={latest_payload.get('ready')} live={latest_payload.get('live')} state={latest_payload.get('state')})",
            )
            next_log_at = time.time() + PROGRESS_LOG_INTERVAL_S
        time.sleep(poll_interval_ms / 1000.0)

    if last_error is not None:
        raise RuntimeError(f"timed out waiting for worker health; last error: {last_error}") from last_error
    raise RuntimeError(f"timed out waiting for worker health readiness: {health_url}")


def stop_instance(base_url: str, api_key: str, instance_id: str) -> None:
    request_json("POST", f"{base_url.rstrip('/')}/api/instances/{instance_id}/stop", api_key=api_key)


def start_instance(base_url: str, api_key: str, instance_id: str) -> None:
    request_json("POST", f"{base_url.rstrip('/')}/api/instances/{instance_id}/start", api_key=api_key)


def terminate_instance(base_url: str, api_key: str, instance_id: str) -> None:
    request_json("DELETE", f"{base_url.rstrip('/')}/api/instances/{instance_id}", api_key=api_key)


def capture_health_snapshot(
    *,
    label: str,
    base_url: str,
    api_key: str,
    instance: dict[str, Any],
    t0: int,
    args: argparse.Namespace,
) -> HealthCapture:
    instance_id = str(instance["id"])
    provider_id = str(instance.get("provider_id") or "")
    log_progress(args, f"{label}: begin for instance_id={instance_id} provider_id={provider_id or '-'}")

    t1, refreshed_instance = wait_for_instance_status(
        base_url,
        api_key,
        instance_id,
        "running",
        args=args,
        timeout_s=args.timeout_s,
        poll_interval_ms=args.poll_interval_ms,
    )
    health_url = format_health_url(args.health_url_template, refreshed_instance)
    log_progress(args, f"{label}: health_url={health_url or 'unavailable'}")
    t2, t3, health_snapshot, notes = wait_for_health_ready(
        health_url,
        args=args,
        timeout_s=args.timeout_s,
        poll_interval_ms=args.poll_interval_ms,
    )
    return HealthCapture(
        label=label,
        instance_id=instance_id,
        provider_id=provider_id,
        health_url=health_url,
        t0_request_sent=t0,
        t1_instance_running=t1,
        t2_server_started=t2,
        t3_model_load_finished=t3,
        health_snapshot=health_snapshot,
        notes=notes,
    )


def build_report(args: argparse.Namespace, captures: list[HealthCapture]) -> dict[str, Any]:
    return {
        "base_url": args.base_url,
        "provider": args.provider,
        "engine": args.engine,
        "gpu_type": args.gpu_type,
        "provider_gpu_type_id": args.provider_gpu_type_id,
        "gpu_count": args.gpu_count,
        "model": args.model,
        "instance_name": args.instance_name,
        "poll_interval_ms": args.poll_interval_ms,
        "timeout_s": args.timeout_s,
        "captures": [asdict(capture) for capture in captures],
    }


def write_json_output(path: str, payload: dict[str, Any]) -> Path:
    output_path = Path(path).expanduser().resolve()
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(json.dumps(payload, indent=2), encoding="utf-8")
    return output_path


def print_summary(captures: list[HealthCapture]) -> None:
    for capture in captures:
        print(f"[{capture.label}]")
        print(f"  instance_id={capture.instance_id} provider_id={capture.provider_id}")
        print(
            "  "
            + " ".join(
                [
                    f"T0={capture.t0_request_sent}",
                    f"T1={capture.t1_instance_running}",
                    f"T2={capture.t2_server_started}",
                    f"T3={capture.t3_model_load_finished}",
                ]
            )
        )
        if capture.health_snapshot is not None:
            print(f"  health_url={capture.health_url}")
            startup = capture.health_snapshot.get("startup") or {}
            metadata = startup.get("metadata") or {}
            print(f"  startup_metadata_keys={','.join(sorted(metadata)) or '-'}")
        for note in capture.notes:
            print(f"  note={note}")
        print()


def main() -> int:
    args = parse_args()
    captures: list[HealthCapture] = []
    instance_id: str | None = None

    try:
        t0 = now_ms()
        log_progress(args, f"fresh_provision: T0 request_sent={t0}")
        response = request_json(
            "POST",
            f"{args.base_url.rstrip('/')}/api/instances/provision",
            api_key=args.api_key,
            payload=build_provision_payload(args),
            timeout=args.timeout_s,
        )
        instance = response["instance"]
        instance_id = str(instance["id"])
        captures.append(
            capture_health_snapshot(
                label="fresh_provision",
                base_url=args.base_url,
                api_key=args.api_key,
                instance=instance,
                t0=t0,
                args=args,
            )
        )

        if args.include_restart:
            log_progress(args, f"restart: stopping instance_id={instance_id}")
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
            restart_t0 = now_ms()
            log_progress(args, f"restart: T0 request_sent={restart_t0}")
            start_instance(args.base_url, args.api_key, instance_id)
            restarted_instance = fetch_instance(args.base_url, args.api_key, instance_id)
            captures.append(
                capture_health_snapshot(
                    label="stopped_instance_start",
                    base_url=args.base_url,
                    api_key=args.api_key,
                    instance=restarted_instance,
                    t0=restart_t0,
                    args=args,
                )
            )

        if args.terminate_final_instance and instance_id is not None:
            log_progress(args, f"terminating instance_id={instance_id}")
            terminate_instance(args.base_url, args.api_key, instance_id)
            captures[-1].notes.append("terminated final instance")
    except Exception as exc:
        if captures:
            print_summary(captures)
        print(f"error: {exc}", file=sys.stderr)
        return 1

    print_summary(captures)
    payload = build_report(args, captures)
    if args.json_output:
        output_path = write_json_output(args.json_output, payload)
        print(f"Wrote JSON report to {output_path}")
    else:
        print(json.dumps(payload, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
