#!/usr/bin/env python3
"""benchmark-chat.py - lightweight chat benchmark for Infera/OpenAI-compatible endpoints.

Measures:
- client-observed TTFT via streaming
- total non-stream latency
- total stream latency
- prompt/completion/total token counts
- approximate decode tokens/sec
- estimated USD cost/request and cost/token with explicit accuracy metadata
"""

from __future__ import annotations

import argparse
from concurrent.futures import ThreadPoolExecutor
import json
from pathlib import Path
import ssl
import statistics
import sys
import threading
import time
import urllib.error
import urllib.request
from dataclasses import dataclass, field
from typing import Any

DEFAULT_BASE_URL = "https://inferai.co.in"
DEFAULT_WORKLOAD_FILE = Path(__file__).resolve().parents[1] / "configs" / "benchmark_lab" / "workloads.json"
DEFAULT_RUNS = 3
DEFAULT_CONCURRENCY = 1
DEFAULT_WARMUP = 0
DEFAULT_MAX_TOKENS = 256
DEFAULT_TEMPERATURE = 0.2

PROMPTS = {
    "short": "What is the capital of France? Answer in one short sentence.",
    "medium": (
        "Explain how a content delivery network works and why it improves latency "
        "for end users. Keep the answer under 180 words."
    ),
    "long": (
        "You are given repeated background context about a distributed inference platform. "
        + " ".join(
            [
                (
                    "The platform provisions GPU workers, routes OpenAI-compatible chat requests, "
                    "tracks cost, and needs better cache reuse and warm-start behavior."
                )
                for _ in range(120)
            ]
        )
        + " Summarize the three most important optimization priorities in 120 words."
    ),
    "conversation": (
        "System context: You are assisting an inference-platform team that is optimizing TTFT, "
        "decode throughput, batching, cache locality, and warm-start behavior. "
        + " ".join(
            [
                (
                    "The platform provisions GPU workers, routes OpenAI-compatible chat requests, "
                    "tracks worker queue depth, and wants session affinity for cache reuse."
                )
                for _ in range(80)
            ]
        )
        + " Prior messages in the same conversation:\n"
        + "\n".join(
            [
                "User: We saw TTFT spikes under concurrent chat load.",
                "Assistant: That usually points to queueing, prefill contention, or poor cache locality.",
                "User: We enabled chunked prefill and reduced batch wait.",
                "Assistant: Good. Now compare no-affinity traffic against session-sticky traffic.",
                "User: The same users ask follow-up questions over the same shared context.",
                "Assistant: Then routing stability matters because repeated prompts can reuse cached prefixes.",
                "User: We want the next measurement to focus on real multi-turn behavior, not one-off prompts.",
            ]
        )
        + "\nLatest user turn: Based on this conversation, give three practical next steps to improve latency without changing hardware. Keep it under 150 words."
    ),
}


@dataclass
class StreamResult:
    ttft_ms: float
    total_ms: float
    content: str
    event_intervals_ms: list[float] = field(default_factory=list)


@dataclass
class NonStreamResult:
    total_ms: float
    prompt_tokens: int
    completion_tokens: int
    total_tokens: int
    content: str


@dataclass
class HealthSamplingSummary:
    sample_count: int = 0
    error_count: int = 0
    peak_memory_used_bytes: int = 0


class HealthSampler:
    def __init__(self, url: str, *, insecure: bool, interval_ms: int):
        self.url = url
        self.insecure = insecure
        self.interval_ms = max(interval_ms, 200)
        self.summary = HealthSamplingSummary()
        self._stop = threading.Event()
        self._thread = threading.Thread(target=self._run, daemon=True)

    def start(self) -> None:
        self._thread.start()

    def stop(self) -> HealthSamplingSummary:
        self._stop.set()
        self._thread.join(timeout=max(self.interval_ms / 1000.0, 2.0))
        return self.summary

    def _run(self) -> None:
        while not self._stop.is_set():
            try:
                payload = request_json("GET", self.url, timeout=5, insecure=self.insecure)
                self.summary.sample_count += 1
                self.summary.peak_memory_used_bytes = max(
                    self.summary.peak_memory_used_bytes,
                    int(payload.get("memory_used_bytes") or 0),
                )
            except Exception:
                self.summary.error_count += 1
            self._stop.wait(self.interval_ms / 1000.0)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Benchmark Infera chat completions.")
    parser.add_argument(
        "base_url",
        nargs="?",
        default=DEFAULT_BASE_URL,
        help="Base URL for the gateway (default: %(default)s)",
    )
    parser.add_argument(
        "--api-key",
        default=None,
        help="Bearer token for authenticated endpoints",
    )
    parser.add_argument(
        "--model",
        required=True,
        help="Model ID to benchmark",
    )
    parser.add_argument(
        "--engine-label",
        default="",
        help="Optional engine label to include in benchmark output metadata",
    )
    parser.add_argument(
        "--provider-label",
        default="",
        help="Optional provider label to include in benchmark output metadata",
    )
    parser.add_argument(
        "--gpu-label",
        default="",
        help="Optional GPU label to include in benchmark output metadata",
    )
    parser.add_argument(
        "--preset",
        choices=["short", "medium", "long", "conversation", "all"],
        default="all",
        help="Prompt preset to run (default: %(default)s)",
    )
    parser.add_argument(
        "--workload-file",
        default=str(DEFAULT_WORKLOAD_FILE),
        help="Optional workload catalog JSON file (default: %(default)s)",
    )
    parser.add_argument(
        "--workload",
        default="",
        help="Optional workload ID from the workload catalog. When set, its prompt and default run settings are used.",
    )
    parser.add_argument(
        "--runs",
        type=int,
        default=DEFAULT_RUNS,
        help="Number of repetitions per preset (default: %(default)s)",
    )
    parser.add_argument(
        "--concurrency",
        type=int,
        default=DEFAULT_CONCURRENCY,
        help="Number of concurrent clients per run group (default: %(default)s)",
    )
    parser.add_argument(
        "--warmup",
        type=int,
        default=DEFAULT_WARMUP,
        help="Number of warmup run groups to execute and discard before measuring (default: %(default)s)",
    )
    parser.add_argument(
        "--cache-reuse-mode",
        choices=["none", "affinity"],
        default="none",
        help="How to preserve repeated-prefix routing across runs (default: %(default)s)",
    )
    parser.add_argument(
        "--cache-key-prefix",
        default="benchmark",
        help="Prefix used when generating synthetic cache-reuse keys (default: %(default)s)",
    )
    parser.add_argument(
        "--max-tokens",
        type=int,
        default=DEFAULT_MAX_TOKENS,
        help="Completion token limit (default: %(default)s)",
    )
    parser.add_argument(
        "--temperature",
        type=float,
        default=DEFAULT_TEMPERATURE,
        help="Temperature (default: %(default)s)",
    )
    parser.add_argument(
        "--timeout",
        type=int,
        default=180,
        help="HTTP timeout in seconds (default: %(default)s)",
    )
    parser.add_argument(
        "--cost-per-hour",
        type=float,
        default=None,
        help="Optional active-instance price in USD/hour for amortized cost metrics",
    )
    parser.add_argument(
        "--json-output",
        default=None,
        help="Optional path to write full benchmark JSON",
    )
    parser.add_argument(
        "--header",
        action="append",
        default=[],
        help="Optional extra HTTP header in 'Name: Value' form. Can be repeated.",
    )
    parser.add_argument(
        "--sample-health-url",
        default="",
        help="Optional worker /health URL to sample while the warm benchmark is running.",
    )
    parser.add_argument(
        "--health-sample-interval-ms",
        type=int,
        default=5000,
        help="Sampling interval for --sample-health-url (default: %(default)s)",
    )
    parser.add_argument(
        "--health-insecure",
        action="store_true",
        help="Disable TLS verification for --sample-health-url polling",
    )
    return parser.parse_args()


def parse_extra_headers(raw_headers: list[str]) -> dict[str, str]:
    headers: dict[str, str] = {}
    for raw_header in raw_headers:
        name, sep, value = raw_header.partition(":")
        if sep == "" or not name.strip():
            raise ValueError(f"invalid header {raw_header!r}; expected 'Name: Value'")
        headers[name.strip()] = value.strip()
    return headers


def request_json(method: str, url: str, *, timeout: int, insecure: bool) -> dict[str, Any]:
    request = urllib.request.Request(
        url,
        headers={"User-Agent": "infera-benchmark-chat/1.0", "Accept": "application/json"},
        method=method,
    )
    context = None
    if insecure and url.startswith("https://"):
        context = ssl._create_unverified_context()
    with urllib.request.urlopen(request, timeout=timeout, context=context) as response:
        body = response.read()
    if not body:
        return {}
    return json.loads(body)


def load_workloads(path: str) -> dict[str, dict[str, Any]]:
    payload = json.loads(Path(path).expanduser().read_text(encoding="utf-8"))
    workloads = payload.get("workloads") or []
    return {str(item.get("id")): dict(item) for item in workloads if item.get("id")}


def resolve_prompt_text(prompt_source: dict[str, Any]) -> str:
    prompt_text = str(prompt_source.get("prompt_text") or "").strip()
    if prompt_text:
        return prompt_text
    preset = str(prompt_source.get("prompt_preset") or "").strip()
    if preset and preset in PROMPTS:
        return PROMPTS[preset]
    raise ValueError("workload does not define prompt_text or a known prompt_preset")


def build_headers(api_key: str | None, stream: bool, extra_headers: dict[str, str] | None = None) -> dict[str, str]:
    headers = {
        "Content-Type": "application/json",
    }
    if stream:
        headers["Accept"] = "text/event-stream"
    if api_key:
        headers["Authorization"] = f"Bearer {api_key}"
    if extra_headers:
        headers.update(extra_headers)
    return headers


def build_request_headers(
    extra_headers: dict[str, str] | None,
    cache_reuse_mode: str,
    cache_key_prefix: str,
    preset: str,
    client_index: int,
) -> dict[str, str] | None:
    headers = dict(extra_headers or {})
    if cache_reuse_mode == "affinity":
        affinity_prefix = headers.get("X-Infera-Affinity-Key", f"{cache_key_prefix}:{preset}")
        headers["X-Infera-Affinity-Key"] = f"{affinity_prefix}:client-{client_index}"
    return headers or None


def build_payload(model: str, prompt: str, max_tokens: int, temperature: float, stream: bool) -> bytes:
    payload = {
        "model": model,
        "messages": [{"role": "user", "content": prompt}],
        "max_tokens": max_tokens,
        "temperature": temperature,
    }
    if stream:
        payload["stream"] = True
    return json.dumps(payload).encode("utf-8")


def run_non_stream(
    base_url: str,
    api_key: str | None,
    model: str,
    prompt: str,
    max_tokens: int,
    temperature: float,
    timeout: int,
    extra_headers: dict[str, str] | None,
) -> NonStreamResult:
    request = urllib.request.Request(
        f"{base_url.rstrip('/')}/v1/chat/completions",
        data=build_payload(model, prompt, max_tokens, temperature, stream=False),
        headers=build_headers(api_key, stream=False, extra_headers=extra_headers),
        method="POST",
    )
    started = time.perf_counter()
    with urllib.request.urlopen(request, timeout=timeout) as response:
        body = response.read()
    total_ms = (time.perf_counter() - started) * 1000.0
    payload = json.loads(body)
    usage = payload.get("usage") or {}
    choices = payload.get("choices") or []
    message = (choices[0].get("message") if choices else {}) or {}
    return NonStreamResult(
        total_ms=total_ms,
        prompt_tokens=int(usage.get("prompt_tokens") or 0),
        completion_tokens=int(usage.get("completion_tokens") or 0),
        total_tokens=int(usage.get("total_tokens") or 0),
        content=str(message.get("content") or ""),
    )


def run_stream(
    base_url: str,
    api_key: str | None,
    model: str,
    prompt: str,
    max_tokens: int,
    temperature: float,
    timeout: int,
    extra_headers: dict[str, str] | None,
) -> StreamResult:
    request = urllib.request.Request(
        f"{base_url.rstrip('/')}/v1/chat/completions",
        data=build_payload(model, prompt, max_tokens, temperature, stream=True),
        headers=build_headers(api_key, stream=True, extra_headers=extra_headers),
        method="POST",
    )
    started = time.perf_counter()
    first_content_ms: float | None = None
    pieces: list[str] = []
    event_times_ms: list[float] = []
    with urllib.request.urlopen(request, timeout=timeout) as response:
        for raw_line in response:
            line = raw_line.decode("utf-8", errors="replace").strip()
            if not line.startswith("data: "):
                continue
            data = line[6:]
            if data == "[DONE]":
                break
            payload = json.loads(data)
            for choice in payload.get("choices") or []:
                delta = choice.get("delta") or {}
                content = delta.get("content")
                if content:
                    pieces.append(content)
                    event_time_ms = (time.perf_counter() - started) * 1000.0
                    event_times_ms.append(event_time_ms)
                    if first_content_ms is None:
                        first_content_ms = event_time_ms
    total_ms = (time.perf_counter() - started) * 1000.0
    event_intervals_ms = [
        round(event_times_ms[index] - event_times_ms[index - 1], 2)
        for index in range(1, len(event_times_ms))
        if event_times_ms[index] - event_times_ms[index - 1] >= 0
    ]
    return StreamResult(
        ttft_ms=first_content_ms or total_ms,
        total_ms=total_ms,
        content="".join(pieces),
        event_intervals_ms=event_intervals_ms,
    )


def median(values: list[float]) -> float:
    return statistics.median(values) if values else 0.0


def mean(values: list[float]) -> float:
    return statistics.fmean(values) if values else 0.0


def pct(values: list[float], percentile: float) -> float:
    if not values:
        return 0.0
    sorted_values = sorted(values)
    index = int(round((len(sorted_values) - 1) * percentile))
    return sorted_values[index]


def _group_representatives(rows: list[dict[str, float | int | str]]) -> list[dict[str, float | int | str]]:
    groups: dict[int, dict[str, float | int | str]] = {}
    for row in rows:
        group_run = row.get("group_run")
        if group_run is None:
            group_run = row.get("run", 0)
        groups.setdefault(int(group_run), row)
    return list(groups.values())


def summarize_rows(rows: list[dict[str, float | int | str]]) -> dict[str, float]:
    ttft_values = [float(row["ttft_ms"]) for row in rows]
    stream_total_values = [float(row["stream_total_ms"]) for row in rows]
    non_stream_values = [float(row["non_stream_total_ms"]) for row in rows]
    decode_values = [float(row["decode_tok_s"]) for row in rows if float(row["decode_tok_s"]) > 0]
    tpot_values = [float(row.get("tpot_ms", 0.0)) for row in rows if float(row.get("tpot_ms", 0.0)) > 0]
    itl_values = [float(row.get("itl_ms", 0.0)) for row in rows if float(row.get("itl_ms", 0.0)) > 0]
    group_rows = _group_representatives(rows)
    aggregate_decode_values = [
        float(row["aggregate_decode_tok_s"])
        for row in group_rows
        if float(row.get("aggregate_decode_tok_s", 0.0)) > 0
    ]
    aggregate_total_values = [
        float(row["aggregate_total_tok_s"])
        for row in group_rows
        if float(row.get("aggregate_total_tok_s", 0.0)) > 0
    ]
    contention_values = [
        float(row["contention_ratio"])
        for row in group_rows
        if float(row.get("contention_ratio", 0.0)) > 0
    ]
    return {
        "ttft_p50_ms": median(ttft_values),
        "ttft_p95_ms": pct(ttft_values, 0.95),
        "ttft_p99_ms": pct(ttft_values, 0.99),
        "ttft_avg_ms": mean(ttft_values),
        "stream_total_p50_ms": median(stream_total_values),
        "non_stream_total_p50_ms": median(non_stream_values),
        "decode_tok_s_p50": median(decode_values),
        "decode_tok_s_p95": pct(decode_values, 0.95),
        "aggregate_decode_tok_s_p50": median(aggregate_decode_values),
        "aggregate_decode_tok_s_p95": pct(aggregate_decode_values, 0.95),
        "aggregate_total_tok_s_p50": median(aggregate_total_values),
        "contention_ratio_p50": median(contention_values),
        "tpot_p50_ms": median(tpot_values),
        "itl_p50_ms": median(itl_values),
    }


def print_summary(name: str, rows: list[dict[str, float | int | str]]) -> None:
    summary = summarize_rows(rows)
    print(f"\n[{name}]")
    print(
        "  "
        f"TTFT p50={summary['ttft_p50_ms']:.1f}ms "
        f"p95={summary['ttft_p95_ms']:.1f}ms "
        f"p99={summary['ttft_p99_ms']:.1f}ms "
        f"avg={summary['ttft_avg_ms']:.1f}ms"
    )
    print(
        "  "
        f"stream_total p50={summary['stream_total_p50_ms']:.1f}ms "
        f"non_stream_total p50={summary['non_stream_total_p50_ms']:.1f}ms"
    )
    if summary["decode_tok_s_p50"] > 0:
        print(
            "  "
            f"decode_tok/s p50={summary['decode_tok_s_p50']:.2f} "
            f"p95={summary['decode_tok_s_p95']:.2f}"
        )
    if summary["aggregate_decode_tok_s_p50"] > 0:
        print(
            "  "
            f"aggregate_decode_tok/s p50={summary['aggregate_decode_tok_s_p50']:.2f} "
            f"p95={summary['aggregate_decode_tok_s_p95']:.2f} "
            f"aggregate_total_tok/s p50={summary['aggregate_total_tok_s_p50']:.2f} "
            f"contention_ratio p50={summary['contention_ratio_p50']:.3f}"
        )
    if summary["tpot_p50_ms"] > 0:
        print(
            "  "
            f"tpot p50={summary['tpot_p50_ms']:.2f}ms "
            f"itl p50={summary['itl_p50_ms']:.2f}ms"
        )


def build_result_row(
    run_index: int,
    client_index: int,
    concurrency: int,
    stream: StreamResult,
    non_stream: NonStreamResult,
    cost_per_hour: float | None,
) -> dict[str, float | int | str]:
    decode_window_s = max((stream.total_ms - stream.ttft_ms) / 1000.0, 0.001)
    decode_tok_s = non_stream.completion_tokens / decode_window_s if non_stream.completion_tokens else 0.0
    total_tok_s = non_stream.total_tokens / max(non_stream.total_ms / 1000.0, 0.001) if non_stream.total_tokens else 0.0
    tpot_ms = (
        max(stream.total_ms - stream.ttft_ms, 0.0) / max(non_stream.completion_tokens, 1)
        if non_stream.completion_tokens
        else 0.0
    )
    itl_ms = median(stream.event_intervals_ms) if stream.event_intervals_ms else tpot_ms
    row: dict[str, float | int | str] = {
        "run": run_index,
        "group_run": run_index,
        "client_index": client_index,
        "concurrency": concurrency,
        "ttft_ms": round(stream.ttft_ms, 2),
        "stream_total_ms": round(stream.total_ms, 2),
        "non_stream_total_ms": round(non_stream.total_ms, 2),
        "prompt_tokens": non_stream.prompt_tokens,
        "completion_tokens": non_stream.completion_tokens,
        "total_tokens": non_stream.total_tokens,
        "decode_tok_s": round(decode_tok_s, 4),
        "total_tok_s": round(total_tok_s, 4),
        "tpot_ms": round(tpot_ms, 4),
        "itl_ms": round(itl_ms, 4),
        "stream_event_count": len(stream.event_intervals_ms) + (1 if stream.content else 0),
    }
    if cost_per_hour is not None:
        row["cost_accuracy"] = "estimated"
        row["cost_attribution_method"] = "active_instance_group_time_share_v1"
        row["price_currency"] = "USD"
        row["price_time_unit"] = "hour"
        row["price_amount"] = cost_per_hour
        row["decode_tok_s_per_dollar_hour"] = round(decode_tok_s / cost_per_hour, 4) if cost_per_hour > 0 else 0.0
    else:
        row["cost_accuracy"] = "unavailable"
    return row


def _run_phase_concurrently(
    runner,
    concurrency: int,
    base_url: str,
    api_key: str | None,
    model: str,
    prompt: str,
    max_tokens: int,
    temperature: float,
    timeout: int,
    extra_headers: dict[str, str] | None,
    cache_reuse_mode: str,
    cache_key_prefix: str,
    preset: str,
):
    with ThreadPoolExecutor(max_workers=concurrency) as executor:
        futures = [
            (
                client_index,
                executor.submit(
                    runner,
                    base_url,
                    api_key,
                    model,
                    prompt,
                    max_tokens,
                    temperature,
                    timeout,
                    build_request_headers(
                        extra_headers,
                        cache_reuse_mode,
                        cache_key_prefix,
                        preset,
                        client_index,
                    ),
                ),
            )
            for client_index in range(1, concurrency + 1)
        ]
        return [(client_index, future.result()) for client_index, future in futures]


def annotate_group_metrics(
    rows: list[dict[str, float | int | str]],
    health_summary: HealthSamplingSummary | None,
    cost_per_hour: float | None = None,
) -> None:
    if not rows:
        return
    aggregate_decode_tok_s = sum(float(row["completion_tokens"]) for row in rows) / max(
        max((float(row["stream_total_ms"]) - float(row["ttft_ms"])) / 1000.0, 0.001) for row in rows
    )
    aggregate_total_tok_s = sum(float(row["total_tokens"]) for row in rows) / max(
        max(float(row["non_stream_total_ms"]) / 1000.0, 0.001) for row in rows
    )
    isolated_decode_tok_s = sum(float(row["decode_tok_s"]) for row in rows)
    contention_ratio = aggregate_decode_tok_s / isolated_decode_tok_s if isolated_decode_tok_s > 0 else 0.0
    group_cost_usd = 0.0
    cost_per_request_usd = 0.0
    cost_per_token_usd = 0.0
    if cost_per_hour is not None and cost_per_hour >= 0:
        # Concurrent requests share one active instance. Charge the group once
        # for its wall-clock window, then amortize evenly across requests.
        group_window_s = max(float(row["non_stream_total_ms"]) for row in rows) / 1000.0
        group_cost_usd = cost_per_hour * group_window_s / 3600.0
        cost_per_request_usd = group_cost_usd / len(rows)
        total_tokens = sum(int(row["total_tokens"]) for row in rows)
        cost_per_token_usd = group_cost_usd / total_tokens if total_tokens > 0 else 0.0
    for row in rows:
        row["aggregate_decode_tok_s"] = round(aggregate_decode_tok_s, 4)
        row["aggregate_total_tok_s"] = round(aggregate_total_tok_s, 4)
        row["contention_ratio"] = round(contention_ratio, 4)
        if cost_per_hour is not None:
            row["cost_query_usd"] = round(cost_per_request_usd, 9)
            row["cost_per_token_usd"] = round(cost_per_token_usd, 12)
            row["cost_per_1m_tokens_usd"] = round(cost_per_token_usd * 1_000_000, 6)
            row["cost_group_usd"] = round(group_cost_usd, 9)
            row["cost_observed_concurrency"] = len(rows)
        if health_summary is not None:
            row["peak_memory_used_bytes"] = health_summary.peak_memory_used_bytes
            row["health_sample_count"] = health_summary.sample_count
            row["health_sample_error_count"] = health_summary.error_count


def run_benchmark_group(
    run_index: int,
    concurrency: int,
    base_url: str,
    api_key: str | None,
    model: str,
    prompt: str,
    max_tokens: int,
    temperature: float,
    timeout: int,
    extra_headers: dict[str, str] | None,
    cache_reuse_mode: str,
    cache_key_prefix: str,
    preset: str,
    cost_per_hour: float | None,
    sample_health_url: str | None = None,
    health_insecure: bool = False,
    health_sample_interval_ms: int = 5000,
    *,
    return_health_summary: bool = False,
) -> list[dict[str, float | int | str]] | tuple[list[dict[str, float | int | str]], HealthSamplingSummary]:
    sampler: HealthSampler | None = None
    if sample_health_url:
        sampler = HealthSampler(
            sample_health_url,
            insecure=health_insecure,
            interval_ms=health_sample_interval_ms,
        )
        sampler.start()
    non_stream_results = _run_phase_concurrently(
        run_non_stream,
        concurrency,
        base_url,
        api_key,
        model,
        prompt,
        max_tokens,
        temperature,
        timeout,
        extra_headers,
        cache_reuse_mode,
        cache_key_prefix,
        preset,
    )
    stream_results = _run_phase_concurrently(
        run_stream,
        concurrency,
        base_url,
        api_key,
        model,
        prompt,
        max_tokens,
        temperature,
        timeout,
        extra_headers,
        cache_reuse_mode,
        cache_key_prefix,
        preset,
    )
    stream_by_client = {client_index: result for client_index, result in stream_results}
    rows = [
        build_result_row(
            run_index,
            client_index,
            concurrency,
            stream_by_client[client_index],
            non_stream_result,
            cost_per_hour,
        )
        for client_index, non_stream_result in non_stream_results
    ]
    health_summary = sampler.stop() if sampler is not None else HealthSamplingSummary()
    annotate_group_metrics(rows, health_summary, cost_per_hour)
    if return_health_summary:
        return rows, health_summary
    return rows


def build_output_payload(
    base_url: str,
    model: str,
    engine_label: str,
    provider_label: str,
    gpu_label: str,
    runs: int,
    concurrency: int,
    warmup: int,
    cache_reuse_mode: str,
    results: dict[str, list[dict[str, float | int | str]]],
    cost_per_hour: float | None,
    health_sampling: dict[str, int] | None = None,
) -> dict[str, object]:
    return {
        "base_url": base_url,
        "model": model,
        "engine": engine_label or None,
        "provider": provider_label or None,
        "gpu_type": gpu_label or None,
        "runs": runs,
        "concurrency": concurrency,
        "warmup": warmup,
        "cache_reuse_mode": cache_reuse_mode,
        "presets": results,
        "cost_per_hour": cost_per_hour,
        "price_snapshot": ({
            "version": "benchmark-cli-hourly-v1",
            "amount": cost_per_hour,
            "currency": "USD",
            "time_unit": "hour",
            "accuracy": "estimated",
            "attribution_method": "active_instance_group_time_share_v1",
        } if cost_per_hour is not None else {
            "accuracy": "unavailable",
        }),
        "health_sampling": health_sampling or {},
    }


def write_json_output(path: str, payload: dict[str, object]) -> Path:
    output_path = Path(path)
    output_path.parent.mkdir(parents=True, exist_ok=True)
    with output_path.open("w", encoding="utf-8") as fh:
        json.dump(payload, fh, indent=2)
    return output_path


def main() -> int:
    args = parse_args()
    try:
        extra_headers = parse_extra_headers(args.header)
    except ValueError as exc:
        print(str(exc), file=sys.stderr)
        return 1
    workload_config: dict[str, Any] | None = None
    prompt_lookup = dict(PROMPTS)
    if args.workload:
        workloads = load_workloads(args.workload_file)
        workload_config = workloads.get(args.workload)
        if workload_config is None:
            print(f"unknown workload {args.workload!r} in {args.workload_file}", file=sys.stderr)
            return 1
        prompt_lookup = {args.workload: resolve_prompt_text(workload_config)}
        if args.runs == DEFAULT_RUNS:
            args.runs = int(workload_config.get("measured_runs") or args.runs)
        if args.warmup == DEFAULT_WARMUP:
            args.warmup = int(workload_config.get("warmup_runs") or args.warmup)
        if args.concurrency == DEFAULT_CONCURRENCY:
            args.concurrency = int(workload_config.get("concurrency") or args.concurrency)
        if args.max_tokens == DEFAULT_MAX_TOKENS:
            args.max_tokens = int(workload_config.get("max_tokens") or args.max_tokens)
        if args.temperature == DEFAULT_TEMPERATURE:
            args.temperature = float(workload_config.get("temperature") or args.temperature)
        if not extra_headers and workload_config.get("headers"):
            extra_headers = {
                str(key): str(value)
                for key, value in dict(workload_config.get("headers") or {}).items()
            }
        presets = [args.workload]
    else:
        presets = list(prompt_lookup) if args.preset == "all" else [args.preset]
    results: dict[str, list[dict[str, float | int | str]]] = {}
    aggregate_health = {"sample_count": 0, "error_count": 0, "peak_memory_used_bytes": 0}

    for preset in presets:
        prompt = prompt_lookup[preset]
        rows: list[dict[str, float | int | str]] = []
        print(
            f"Running preset={preset} runs={args.runs} warmup={args.warmup} "
            f"concurrency={args.concurrency} cache_reuse_mode={args.cache_reuse_mode} model={args.model}"
        )
        for warmup_index in range(1, args.warmup + 1):
            try:
                _warmup_rows, warmup_health = run_benchmark_group(
                    warmup_index,
                    args.concurrency,
                    args.base_url,
                    args.api_key,
                    args.model,
                    prompt,
                    args.max_tokens,
                    args.temperature,
                    args.timeout,
                    extra_headers,
                    args.cache_reuse_mode,
                    args.cache_key_prefix,
                    preset,
                    args.cost_per_hour,
                    args.sample_health_url or None,
                    args.health_insecure,
                    args.health_sample_interval_ms,
                    return_health_summary=True,
                )
                aggregate_health["sample_count"] += warmup_health.sample_count
                aggregate_health["error_count"] += warmup_health.error_count
                aggregate_health["peak_memory_used_bytes"] = max(
                    aggregate_health["peak_memory_used_bytes"],
                    warmup_health.peak_memory_used_bytes,
                )
            except urllib.error.HTTPError as exc:
                print(f"warmup {warmup_index}: HTTP {exc.code} {exc.reason}", file=sys.stderr)
                return 1
            except urllib.error.URLError as exc:
                print(f"warmup {warmup_index}: request failed: {exc}", file=sys.stderr)
                return 1
            print(f"  warmup={warmup_index}/{args.warmup} complete")
        for run_index in range(1, args.runs + 1):
            try:
                group_rows, group_health = run_benchmark_group(
                    run_index,
                    args.concurrency,
                    args.base_url,
                    args.api_key,
                    args.model,
                    prompt,
                    args.max_tokens,
                    args.temperature,
                    args.timeout,
                    extra_headers,
                    args.cache_reuse_mode,
                    args.cache_key_prefix,
                    preset,
                    args.cost_per_hour,
                    args.sample_health_url or None,
                    args.health_insecure,
                    args.health_sample_interval_ms,
                    return_health_summary=True,
                )
            except urllib.error.HTTPError as exc:
                print(f"run {run_index}: HTTP {exc.code} {exc.reason}", file=sys.stderr)
                return 1
            except urllib.error.URLError as exc:
                print(f"run {run_index}: request failed: {exc}", file=sys.stderr)
                return 1

            rows.extend(group_rows)
            aggregate_health["sample_count"] += group_health.sample_count
            aggregate_health["error_count"] += group_health.error_count
            aggregate_health["peak_memory_used_bytes"] = max(
                aggregate_health["peak_memory_used_bytes"],
                group_health.peak_memory_used_bytes,
            )
            if args.concurrency == 1:
                row = group_rows[0]
                print(
                    "  "
                    f"run={run_index} ttft={row['ttft_ms']}ms "
                    f"stream_total={row['stream_total_ms']}ms "
                    f"non_stream_total={row['non_stream_total_ms']}ms "
                    f"completion_tokens={row['completion_tokens']} "
                    f"decode_tok/s={row['decode_tok_s']}"
                )
            else:
                group_summary = summarize_rows(group_rows)
                print(
                    "  "
                    f"group={run_index} clients={args.concurrency} "
                    f"ttft_p50={group_summary['ttft_p50_ms']:.1f}ms "
                    f"ttft_p95={group_summary['ttft_p95_ms']:.1f}ms "
                    f"aggregate_decode_tok/s={group_summary['aggregate_decode_tok_s_p50']:.2f} "
                    f"aggregate_total_tok/s={group_summary['aggregate_total_tok_s_p50']:.2f}"
                )
        results[preset] = rows
        print_summary(preset, rows)

    if args.json_output:
        output_path = write_json_output(
            args.json_output,
            build_output_payload(
                args.base_url,
                args.model,
                args.engine_label,
                args.provider_label,
                args.gpu_label,
                args.runs,
                args.concurrency,
                args.warmup,
                args.cache_reuse_mode,
                results,
                args.cost_per_hour,
                aggregate_health,
            ),
        )
        print(f"\nWrote JSON results to {output_path}")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
