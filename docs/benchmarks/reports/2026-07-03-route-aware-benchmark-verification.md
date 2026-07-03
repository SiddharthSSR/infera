# Route-Aware Benchmark Verification

Date: 2026-07-03

Linear issue: `INF-29`

## Purpose

Verify that route decision metrics and `route_decision` logs are emitted during small production benchmark traffic after the production gateway refresh. This was not a load test.

## Production Environment

| Field | Value |
| --- | --- |
| Base URL | `https://inferai.co.in` |
| Provider | RunPod |
| Provider instance ID | `kk09izf357hzk9` |
| Worker ID | `c2eb68c9-e951-4186-9f4d-7c3ea7e390ea` |
| GPU | `A100_80GB` |
| Hourly cost | `$1.19/hr` |
| Model | `Qwen/Qwen2.5-7B-Instruct` |
| Git commit | `88578e0` |

Before the benchmark, production health was verified as `status=healthy`, `workers=1`, and `healthy_workers=1`. Authenticated `/v1/models` listed the target model. One non-streaming smoke request and one streaming smoke request succeeded before the benchmark.

## Benchmark Scope

| Field | Value |
| --- | --- |
| Streaming concurrency | `1,2,4` |
| Non-streaming concurrency | `1,2,4` |
| Measured requests per concurrency | `5` |
| Warmup requests per concurrency | `1` |
| Workloads | `bench/workloads/streaming_chat.yaml`, `bench/workloads/short_chat.yaml` |
| Timeout | `120s` |

## Streaming Results

Latency summarizes successful requests only. Failed request latency is reported separately by `infera-bench`. Approx TPOT is inter-delta timing for streaming chunks, not exact token-level TPOT.

| Concurrency | Requests | Successes | Errors | Error Rate | Req/s | Tok/s | Latency p50/p95/p99 ms | Failed latency p50/p95/p99 ms | TTFT p50/p95/p99 ms | Approx TPOT p50/p95/p99 ms | Representative errors |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | --- | --- | --- | --- |
| 1 | 5 | 5 | 0 | 0.00% | 0.74 | n/a | 1246.9 / 2188.9 / 2188.9 | n/a | 337.8 / 1168.9 / 1168.9 | 9.9 / 15.7 / 93.9 | none |
| 2 | 5 | 5 | 0 | 0.00% | 1.36 | n/a | 1259.4 / 1833.2 / 1833.2 | n/a | 308.0 / 836.9 / 836.9 | 10.0 / 16.2 / 97.5 | none |
| 4 | 5 | 5 | 0 | 0.00% | 2.16 | n/a | 1267.7 / 1875.0 / 1875.0 | n/a | 282.6 / 892.9 / 892.9 | 9.7 / 17.5 / 94.9 | none |

## Non-Streaming Results

| Concurrency | Requests | Successes | Errors | Error Rate | Req/s | Tok/s | Latency p50/p95/p99 ms | Failed latency p50/p95/p99 ms | TTFT / Approx TPOT | Representative errors |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | --- | --- | --- |
| 1 | 5 | 5 | 0 | 0.00% | 2.00 | 119.87 | 466.8 / 678.7 / 678.7 | n/a | n/a | none |
| 2 | 5 | 5 | 0 | 0.00% | 3.23 | 193.82 | 503.2 / 1025.4 / 1025.4 | n/a | n/a | none |
| 4 | 5 | 5 | 0 | 0.00% | 5.84 | 354.83 | 502.2 / 688.2 / 688.2 | n/a | n/a | none |

## Route Metric Evidence

Metrics were read from internal gateway `/metrics`, not public `https://inferai.co.in/metrics`, because public `/metrics` is routed to the frontend.

| Metric | Before | After | Delta |
| --- | ---: | ---: | ---: |
| `infera_route_decisions_total{result="success",strategy="least_loaded"}` | 2 | 25 | +23 |
| `infera_route_decisions_total{result="success",strategy="affinity"}` | 0 | 15 | +15 |
| Total successful route decisions | 2 | 40 | +38 |
| `infera_route_candidates_evaluated_count` | 2 | 40 | +38 |
| `infera_route_candidates_evaluated_sum` | 2 | 40 | +38 |

The `+38` total route-decision delta covers the pre-benchmark smoke requests plus streaming and non-streaming benchmark traffic, including warmups.

## Route Decision Log Evidence

Recent gateway logs showed `38` `route_decision` events and no `route_decision_failed` events for the verification window.

Observed route decision fields:

- `strategy`
- `selected_worker`
- `candidates_evaluated`
- `selected_provider`
- `worker_queue_depth`
- `worker_active_requests`
- `worker_load`
- latency fields were present in sampled events after worker latency stats became available

No prompt text, API keys, authorization headers, provider credentials, raw JSON responses, or secrets are included in this report.

## Interpretation

- `infera-bench` completed the small c1/c2/c4 route-aware verification in streaming and non-streaming modes.
- All measured requests succeeded with `0.00%` error rate.
- Route decision metrics increased during verification traffic.
- Gateway logs emitted route decision events with strategy, selected worker, provider, candidate count, and worker load signals.
- Both `least_loaded` and `affinity` successful route decisions were observed. This is expected because the gateway can use affinity routing when affinity metadata is available and valid.
- Route decision exposure in benchmark output remains deferred; route evidence currently comes from internal metrics and operational logs.

## Limitations

- Only five measured requests per mode and concurrency level.
- Only concurrency 1, 2, and 4 were tested.
- Only one model, one provider, and one GPU type were tested.
- This benchmark does not establish maximum throughput.
- Cost per request, cost per token, and route decision metadata in benchmark output are not implemented yet.
- Approx TPOT is inter-delta timing, not exact token-level TPOT.

## Next Steps

- Add safe route decision metadata exposure for `infera-bench`.
- Add route-aware benchmark reporting once safe metadata exposure exists.
- Add provider/gateway instance mismatch warning.
- Add release verification for the zero-worker state.
- Design cost-per-token estimation before implementing cost-aware routing.
