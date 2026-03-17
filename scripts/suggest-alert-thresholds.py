#!/usr/bin/env python3
"""Suggest alert thresholds from benchmark-chat JSON output."""

from __future__ import annotations

import argparse
import json
import math
from pathlib import Path
import re


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Suggest TTFT/TPOT/batch-wait alert thresholds from benchmark JSON."
    )
    parser.add_argument("benchmark_json", help="Path to benchmark-chat JSON output")
    parser.add_argument(
        "--headroom",
        type=float,
        default=1.5,
        help="Multiplier applied to measured p95 values (default: %(default)s)",
    )
    parser.add_argument(
        "--min-ttft-seconds",
        type=float,
        default=1.0,
        help="Minimum suggested TTFT threshold in seconds (default: %(default)s)",
    )
    parser.add_argument(
        "--min-tpot-seconds",
        type=float,
        default=0.03,
        help="Minimum suggested TPOT threshold in seconds (default: %(default)s)",
    )
    return parser.parse_args()


def percentile(values: list[float], pct: float) -> float:
    if not values:
        return 0.0
    ordered = sorted(values)
    index = int(round((len(ordered) - 1) * pct))
    return ordered[index]


def round_up(value: float, step: float) -> float:
    if value <= 0:
        return 0.0
    return math.ceil(value / step) * step


def slugify(value: str) -> str:
    return re.sub(r"[^a-z0-9]+", "-", value.lower()).strip("-") or "model"


def main() -> int:
    args = parse_args()
    payload = json.loads(Path(args.benchmark_json).read_text(encoding="utf-8"))
    presets = payload.get("presets") or {}

    ttft_ms: list[float] = []
    tpot_s: list[float] = []

    for rows in presets.values():
        for row in rows:
            ttft = float(row.get("ttft_ms") or 0.0)
            decode_tok_s = float(row.get("decode_tok_s") or 0.0)
            if ttft > 0:
                ttft_ms.append(ttft)
            if decode_tok_s > 0:
                tpot_s.append(1.0 / decode_tok_s)

    if not ttft_ms:
        raise SystemExit("No TTFT values found in benchmark JSON")

    measured_ttft_p95_s = percentile(ttft_ms, 0.95) / 1000.0
    measured_tpot_p95_s = percentile(tpot_s, 0.95) if tpot_s else 0.0

    suggested_ttft_s = max(
        args.min_ttft_seconds,
        round_up(measured_ttft_p95_s * args.headroom, 0.1),
    )
    suggested_tpot_s = max(
        args.min_tpot_seconds,
        round_up(measured_tpot_p95_s * args.headroom, 0.005),
    ) if measured_tpot_p95_s > 0 else args.min_tpot_seconds
    suggested_batch_wait_s = round_up(
        max(0.05, min(0.25, suggested_ttft_s * 0.1)),
        0.01,
    )

    model = payload.get("model") or "your/model"

    print("Suggested thresholds")
    print(f"  model: {model}")
    print(f"  measured_ttft_p95_seconds: {measured_ttft_p95_s:.3f}")
    print(f"  measured_tpot_p95_seconds: {measured_tpot_p95_s:.3f}")
    print(f"  suggested_ttft_threshold_seconds: {suggested_ttft_s:.2f}")
    print(f"  suggested_tpot_threshold_seconds: {suggested_tpot_s:.3f}")
    print(f"  suggested_batch_wait_threshold_seconds: {suggested_batch_wait_s:.2f}")
    print()
    print("Prometheus snippet")
    model_slug = slugify(model)
    print(
        f"""- alert: InferaInferenceTTFTHigh{model_slug.title().replace('-', '')}
  expr: |
    histogram_quantile(
      0.95,
      sum by (le, model) (rate(infera_gateway_inference_ttft_seconds_bucket{{model="{model}"}}[5m]))
    ) > {suggested_ttft_s:.2f}
  for: 5m
  labels:
    severity: warning
    service: gateway
  annotations:
    summary: Inference TTFT is high for {model}
    description: Model {model} has p95 time-to-first-token above {suggested_ttft_s:.2f}s for 5 minutes.

- alert: InferaInferenceTPOTHigh{model_slug.title().replace('-', '')}
  expr: |
    histogram_quantile(
      0.95,
      sum by (le, model) (rate(infera_gateway_inference_tpot_seconds_bucket{{model="{model}"}}[5m]))
    ) > {suggested_tpot_s:.3f}
  for: 10m
  labels:
    severity: warning
    service: gateway
  annotations:
    summary: Inference TPOT is high for {model}
    description: Model {model} has p95 time-per-output-token above {suggested_tpot_s:.3f}s for 10 minutes.

- alert: InferaBatchWaitHigh{model_slug.title().replace('-', '')}
  expr: |
    histogram_quantile(
      0.95,
      sum by (le, model) (rate(infera_gateway_batch_wait_seconds_bucket{{model="{model}"}}[5m]))
    ) > {suggested_batch_wait_s:.2f}
  for: 10m
  labels:
    severity: warning
    service: gateway
  annotations:
    summary: Batch wait is high for {model}
    description: Model {model} has p95 batch queue wait above {suggested_batch_wait_s:.2f}s for 10 minutes."""
    )

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
