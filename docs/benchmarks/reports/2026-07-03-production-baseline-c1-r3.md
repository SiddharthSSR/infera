# Production Baseline c1/r3

Date: 2026-07-03

## Purpose

This is the first tiny production baseline for `infera-bench`. It verifies that the benchmark CLI can measure a healthy production Infera deployment in both streaming and non-streaming modes. This run is smoke-sized and is not a load test.

## Production Environment

- Base URL: `https://inferai.co.in`
- Provider: RunPod
- Provider/instance ID: `260fqg9610xven`
- Worker ID: `dcff7025-916a-4a53-aaaa-f7c2fd7570de`
- GPU: `A100_80GB`
- Model: `Qwen/Qwen2.5-7B-Instruct`
- Hourly cost: `$1.19/hr`

## Production Health

- Public health status: `healthy`
- Workers: `1`
- Healthy workers: `1`
- Release verification: passed

## Operational Finding

The previous RunPod worker, `52uwxf7gdw5ebv`, was terminated and production degraded to zero workers before restoration. This failure mode shows that worker lifecycle monitoring and zero-worker alerting should be future work.

## Benchmark Scope

- Scope: smoke-sized baseline, not a load test
- Concurrency: `1`
- Measured requests: `3`
- Warmup requests: `1`
- Modes: streaming and non-streaming

## Streaming Results

| Metric | Value |
| --- | ---: |
| Successes | `3` |
| Errors | `0` |
| Error rate | `0.00%` |
| Requests/sec | `0.78` |
| Tokens/sec | `n/a` |
| Latency p50/p95/p99 | `1359.3 / 1362.0 / 1362.0 ms` |
| TTFT p50/p95/p99 | `334.7 / 336.3 / 336.3 ms` |
| Approx TPOT p50/p95/p99 | `10.6 / 13.2 / 99.3 ms` |
| Representative errors | none |

## Non-Streaming Results

| Metric | Value |
| --- | ---: |
| Successes | `3` |
| Errors | `0` |
| Error rate | `0.00%` |
| Requests/sec | `1.45` |
| Tokens/sec | `98.32` |
| Latency p50/p95/p99 | `624.2 / 896.1 / 896.1 ms` |
| TTFT / Approx TPOT | `n/a` |
| Representative errors | none |

## Interpretation

- The benchmark CLI is functioning against production.
- Production API authentication works with a smoke API key.
- Worker registration works after restoration.
- Streaming TTFT is measurable.
- Non-streaming total latency and tokens/sec are measurable.
- This baseline should not be used to claim maximum throughput.

## Limitations

- Only 3 measured requests per mode.
- Only concurrency 1.
- Only one model.
- Only one provider.
- Only one GPU type.
- Approx TPOT is inter-delta timing, not exact token-level TPOT.
- Cost per request and route decision metrics are not implemented yet.

## Next Benchmark Steps

- Run c1/c2/c4 with small request counts.
- Add a long-context workload.
- Capture route decision logging.
- Add cost-per-token estimation.
- Add a zero-worker alert/failure-mode test.
