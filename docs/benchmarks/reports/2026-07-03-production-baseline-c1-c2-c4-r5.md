# Production Benchmark Baseline c1/c2/c4 r5

Date: 2026-07-03

## Purpose

Record the next controlled production baseline for `infera-bench` after the initial c1/r3 smoke-sized run. This is still not a load test. The run expands only to concurrency 1, 2, and 4 with five measured requests per level so the system has a small benchmark signal without sustained production pressure.

## Environment

| Field | Value |
| --- | --- |
| Base URL | `https://inferai.co.in` |
| Provider | RunPod |
| Provider instance ID | `bpahtvy174ri86` |
| Worker ID | `e71a733f-8a44-4044-9e9c-9cd7780de8a4` |
| GPU | `A100_80GB` |
| Model | `Qwen/Qwen2.5-7B-Instruct` |
| Hourly cost | `$1.19/hr` |

Before the benchmark, production health was verified as `status=healthy`, `workers=1`, and `healthy_workers=1`. Authenticated `/v1/models` listed the target model, and one non-streaming plus one streaming smoke request succeeded.

## Benchmark Scope

| Field | Value |
| --- | --- |
| Streaming concurrency | `1,2,4` |
| Non-streaming concurrency | `1,2,4` |
| Measured requests per concurrency | `5` |
| Warmup requests per concurrency | `1` |
| Workloads | `bench/workloads/streaming_chat.yaml`, `bench/workloads/short_chat.yaml` |
| Timeout | `120s` |
| Git commit | `eb44f96` |

## Streaming Results

Latency summarizes successful requests only. Failed request latency is reported separately by `infera-bench`. Approx TPOT is inter-delta timing for streaming chunks, not exact token-level TPOT.

| Concurrency | Requests | Successes | Errors | Error Rate | Req/s | Tok/s | Latency p50/p95/p99 ms | Failed latency p50/p95/p99 ms | TTFT p50/p95/p99 ms | Approx TPOT p50/p95/p99 ms | Representative errors |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | --- | --- | --- | --- |
| 1 | 5 | 5 | 0 | 0.00% | 0.69 | n/a | 1368.8 / 1697.5 / 1697.5 | n/a | 300.1 / 925.1 / 925.1 | 4.5 / 38.3 / 126.5 | none |
| 2 | 5 | 5 | 0 | 0.00% | 1.38 | n/a | 1303.4 / 1841.8 / 1841.8 | n/a | 300.5 / 928.9 / 928.9 | 7.8 / 29.1 / 102.2 | none |
| 4 | 5 | 5 | 0 | 0.00% | 2.23 | n/a | 1248.6 / 1856.3 / 1856.3 | n/a | 464.3 / 860.8 / 860.8 | 5.9 / 32.7 / 106.3 | none |

## Non-Streaming Results

| Concurrency | Requests | Successes | Errors | Error Rate | Req/s | Tok/s | Latency p50/p95/p99 ms | Failed latency p50/p95/p99 ms | TTFT / Approx TPOT | Representative errors |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | --- | --- | --- |
| 1 | 5 | 5 | 0 | 0.00% | 1.84 | 110.30 | 478.2 / 786.2 / 786.2 | n/a | n/a | none |
| 2 | 5 | 5 | 0 | 0.00% | 3.29 | 197.65 | 475.3 / 1042.3 / 1042.3 | n/a | n/a | none |
| 4 | 5 | 5 | 0 | 0.00% | 4.46 | 264.83 | 1035.7 / 1121.4 / 1121.4 | n/a | n/a | none |

## Route Decision Logging Evidence

The benchmark was run from main commit `eb44f96`, which includes route decision logging and route decision metrics in source. During post-run production inspection:

| Signal | Observed |
| --- | --- |
| `route_decision` log events | No events found in recent gateway logs |
| `route_decision_failed` log events | No events found in recent gateway logs |
| `strategy` field | Not observed in production logs |
| `selected_worker` field | Not observed in production logs |
| `candidates_evaluated` field | Not observed in production logs |
| Worker load / latency fields | Not observed in production logs |
| `infera_route_decisions_total` metric | Not exposed by live `/metrics` |
| `infera_route_candidates_evaluated` metric | Not exposed by live `/metrics` |

This indicates the live production gateway image was likely not redeployed with the route-decision instrumentation even though the source branch had been merged. No deployment changes were made during this benchmark task.

## Interpretation

- `infera-bench` successfully completed streaming and non-streaming baselines at c1, c2, and c4.
- Authentication, model listing, worker registration, and both inference modes worked with one RunPod A100_80GB worker.
- Streaming TTFT and Approx TPOT remained measurable at all tested concurrency levels.
- Non-streaming total latency and tokens/sec were measurable at all tested concurrency levels.
- The c4 results are useful as an early operational signal, but they should not be used to claim maximum throughput.
- Route decision logging needs a production image rollout before benchmark evidence can include live `route_decision` events or route metrics.

## Limitations

- Only five measured requests per mode and concurrency level.
- Only concurrency 1, 2, and 4 were tested.
- Only one model was tested.
- Only one provider and one GPU type were tested.
- Approx TPOT is inter-delta timing, not exact token-level TPOT.
- Cost per request, cost per token, and route decision metadata are not implemented in benchmark output yet.
- Live route decision logs and metrics were not observed because production did not appear to be running the newly instrumented gateway image.

## Next Steps

- Redeploy production gateway with the merged route-decision logging build and verify `route_decision` logs plus route metrics.
- Repeat c1/c2/c4 with the same small request counts after route-decision logging is visible.
- Add a long-context workload.
- Capture route decision metadata in benchmark reports through a safe header or debug endpoint.
- Add cost-per-token estimation.
- Add a zero-worker alert/failure-mode verification to release checks.
