#!/usr/bin/env python3
"""benchmark-chat.py - lightweight chat benchmark for Infera/OpenAI-compatible endpoints.

Measures:
- client-observed TTFT via streaming
- total non-stream latency
- total stream latency
- prompt/completion/total token counts
- approximate decode tokens/sec
- optional cost/query and tokens/sec/$ when hourly cost is supplied
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path
import statistics
import sys
import time
import urllib.error
import urllib.request
from dataclasses import dataclass

DEFAULT_BASE_URL = "https://inferai.co.in"

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
}


@dataclass
class StreamResult:
    ttft_ms: float
    total_ms: float
    content: str


@dataclass
class NonStreamResult:
    total_ms: float
    prompt_tokens: int
    completion_tokens: int
    total_tokens: int
    content: str


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
        "--preset",
        choices=["short", "medium", "long", "all"],
        default="all",
        help="Prompt preset to run (default: %(default)s)",
    )
    parser.add_argument(
        "--runs",
        type=int,
        default=3,
        help="Number of repetitions per preset (default: %(default)s)",
    )
    parser.add_argument(
        "--max-tokens",
        type=int,
        default=256,
        help="Completion token limit (default: %(default)s)",
    )
    parser.add_argument(
        "--temperature",
        type=float,
        default=0.2,
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
        help="Optional hourly infra cost to estimate cost/query and tokens/sec/$",
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
    return parser.parse_args()


def parse_extra_headers(raw_headers: list[str]) -> dict[str, str]:
    headers: dict[str, str] = {}
    for raw_header in raw_headers:
        name, sep, value = raw_header.partition(":")
        if sep == "" or not name.strip():
            raise ValueError(f"invalid header {raw_header!r}; expected 'Name: Value'")
        headers[name.strip()] = value.strip()
    return headers


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
                    if first_content_ms is None:
                        first_content_ms = (time.perf_counter() - started) * 1000.0
    total_ms = (time.perf_counter() - started) * 1000.0
    return StreamResult(
        ttft_ms=first_content_ms or total_ms,
        total_ms=total_ms,
        content="".join(pieces),
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


def summarize_rows(rows: list[dict[str, float | int | str]]) -> dict[str, float]:
    ttft_values = [float(row["ttft_ms"]) for row in rows]
    stream_total_values = [float(row["stream_total_ms"]) for row in rows]
    non_stream_values = [float(row["non_stream_total_ms"]) for row in rows]
    decode_values = [float(row["decode_tok_s"]) for row in rows if float(row["decode_tok_s"]) > 0]
    return {
        "ttft_p50_ms": median(ttft_values),
        "ttft_p95_ms": pct(ttft_values, 0.95),
        "ttft_avg_ms": mean(ttft_values),
        "stream_total_p50_ms": median(stream_total_values),
        "non_stream_total_p50_ms": median(non_stream_values),
        "decode_tok_s_p50": median(decode_values),
        "decode_tok_s_p95": pct(decode_values, 0.95),
    }


def print_summary(name: str, rows: list[dict[str, float | int | str]]) -> None:
    summary = summarize_rows(rows)
    print(f"\n[{name}]")
    print(
        "  "
        f"TTFT p50={summary['ttft_p50_ms']:.1f}ms "
        f"p95={summary['ttft_p95_ms']:.1f}ms "
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


def build_result_row(
    run_index: int,
    stream: StreamResult,
    non_stream: NonStreamResult,
    cost_per_hour: float | None,
) -> dict[str, float | int | str]:
    decode_window_s = max((stream.total_ms - stream.ttft_ms) / 1000.0, 0.001)
    decode_tok_s = non_stream.completion_tokens / decode_window_s if non_stream.completion_tokens else 0.0
    total_tok_s = non_stream.total_tokens / max(non_stream.total_ms / 1000.0, 0.001) if non_stream.total_tokens else 0.0
    row: dict[str, float | int | str] = {
        "run": run_index,
        "ttft_ms": round(stream.ttft_ms, 2),
        "stream_total_ms": round(stream.total_ms, 2),
        "non_stream_total_ms": round(non_stream.total_ms, 2),
        "prompt_tokens": non_stream.prompt_tokens,
        "completion_tokens": non_stream.completion_tokens,
        "total_tokens": non_stream.total_tokens,
        "decode_tok_s": round(decode_tok_s, 4),
        "total_tok_s": round(total_tok_s, 4),
    }
    if cost_per_hour is not None:
        query_cost = cost_per_hour * (non_stream.total_ms / 1000.0) / 3600.0
        row["cost_query_usd"] = round(query_cost, 8)
        row["decode_tok_s_per_dollar_hour"] = round(decode_tok_s / cost_per_hour, 4) if cost_per_hour > 0 else 0.0
    return row


def build_output_payload(
    base_url: str,
    model: str,
    runs: int,
    results: dict[str, list[dict[str, float | int | str]]],
    cost_per_hour: float | None,
) -> dict[str, object]:
    return {
        "base_url": base_url,
        "model": model,
        "runs": runs,
        "presets": results,
        "cost_per_hour": cost_per_hour,
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
    presets = list(PROMPTS) if args.preset == "all" else [args.preset]
    results: dict[str, list[dict[str, float | int | str]]] = {}

    for preset in presets:
        prompt = PROMPTS[preset]
        rows: list[dict[str, float | int | str]] = []
        print(f"Running preset={preset} runs={args.runs} model={args.model}")
        for run_index in range(1, args.runs + 1):
            try:
                non_stream = run_non_stream(
                    args.base_url,
                    args.api_key,
                    args.model,
                    prompt,
                    args.max_tokens,
                    args.temperature,
                    args.timeout,
                    extra_headers,
                )
                stream = run_stream(
                    args.base_url,
                    args.api_key,
                    args.model,
                    prompt,
                    args.max_tokens,
                    args.temperature,
                    args.timeout,
                    extra_headers,
                )
            except urllib.error.HTTPError as exc:
                print(f"run {run_index}: HTTP {exc.code} {exc.reason}", file=sys.stderr)
                return 1
            except urllib.error.URLError as exc:
                print(f"run {run_index}: request failed: {exc}", file=sys.stderr)
                return 1

            row = build_result_row(run_index, stream, non_stream, args.cost_per_hour)
            rows.append(row)
            print(
                "  "
                f"run={run_index} ttft={row['ttft_ms']}ms "
                f"stream_total={row['stream_total_ms']}ms "
                f"non_stream_total={row['non_stream_total_ms']}ms "
                f"completion_tokens={row['completion_tokens']} "
                f"decode_tok/s={row['decode_tok_s']}"
            )
        results[preset] = rows
        print_summary(preset, rows)

    if args.json_output:
        output_path = write_json_output(
            args.json_output,
            build_output_payload(
                args.base_url,
                args.model,
                args.runs,
                results,
                args.cost_per_hour,
            ),
        )
        print(f"\nWrote JSON results to {output_path}")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
