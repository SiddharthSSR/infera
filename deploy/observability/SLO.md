# Inference SLO contract

## Version 1

Version 1 applies to requests accepted by the OpenAI-compatible chat-completions gateway. It is intentionally a gateway SLO: worker and provider telemetry are diagnostic signals, not alternate sources for the customer-facing indicators.

### Objectives

| Objective | Target | Eligible events | Good event |
| --- | --- | --- | --- |
| Availability | 99.0% over 14 days | Every streaming or non-streaming request that enters gateway inference execution | Final gateway inference outcome is `success` |
| End-to-end latency | p95 <= 10s over 14 days | Successful requests | Gateway wall-clock duration from accepted inference execution through the completed response path is <= 10s |
| TTFT | p95 <= 2s over 14 days | Successful requests with an exact or derived TTFT sample | Sample is <= 2s |
| TPOT | p95 <= 100ms over 14 days | Successful requests with a derived TPOT sample | Sample is <= 100ms |

The production rules publish rolling five-minute p50/p95/p99 operational views, 14-day p95 objective values, and 14-day good-event attainment ratios. The dashboard labels the short-window views as operational and reports objective attainment separately. The 14-day objective fits within the production Prometheus 15-day retention window.

### Measurement semantics

| Request mode | End-to-end | TTFT | TPOT |
| --- | --- | --- | --- |
| Streaming | `exact`: gateway wall clock | `exact`: gateway-observed time from inference execution start to the first usable worker output observation | `derived`: elapsed time between successive usable output observations; cumulative-token deltas expand samples only when the prior usable observation supplied a trustworthy cumulative baseline, otherwise the interval conservatively contributes one sample |
| Non-streaming | `exact`: gateway wall clock | `derived`: worker-reported internal TTFT; it is not client-observed and excludes gateway-to-worker routing time | `derived`: `(worker total - worker TTFT) / (completion tokens - 1)` |

A usable streaming output observation has a non-empty content delta or a non-empty generated tool function name/arguments delta. Empty, usage-only, finish-only, and tool-ID/type-only chunks are protocol metadata: they are still forwarded, but never start TTFT or advance TPOT. TTFT is `unavailable` when streaming completes without a usable output observation or a non-streaming response has no positive worker TTFT. TPOT is `unavailable` when fewer than two usable output observations exist, token count is insufficient, or worker timing is inconsistent. Unavailable requests increment `infera_gateway_slo_v1_latency_measurements_total` but never receive a fabricated zero-valued histogram sample.

### Labels, privacy, and cardinality

SLO source metrics expose only `model`, `routing_strategy`, `stream`, `outcome`, and measurement dimensions. They never include workspace, API key, tenant, prompt, request, worker, or provider identifiers.

The `model` label is populated only after the router selects a worker, which bounds successful and post-route series to models loaded by the controlled worker fleet. Pre-route failures use `model="unknown"`, so arbitrary client model strings cannot create time series. `routing_strategy` is restricted to `least_loaded`, `round_robin`, `latency_based`, `affinity`, or `unknown`. Recording rules aggregate away stream mode and retain only model, routing strategy, and measurement quality where diagnostically necessary.

### No-data behavior

No traffic is not success and not failure. Recording and dashboard queries preserve an absent series for ratios and latency quantiles when there are no eligible events. Grafana renders that state as `Unavailable (no data)`. Availability burn alerts require positive recent request rate and therefore remain inactive when traffic is absent. Use the gateway-down alert to detect missing gateway telemetry.
