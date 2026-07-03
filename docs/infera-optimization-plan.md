# Infera Optimization Plan

This document defines how Infera should move from a working inference platform to a measurably optimized inference control plane. It is intentionally documentation-only: it does not introduce benchmark code, deployment changes, or routing behavior changes.

## 1. Current Production State

- Production URL: `https://inferai.co.in`
- Dashboard URL: `https://dashboard.inferai.co.in`
- Current verified status: production verification passed after the stabilization release. The verified checks covered public site health, dashboard health, internal worker discovery, authenticated `/v1/models`, non-streaming chat completions, and streaming chat completions.
- Deployed branch and commit: `task/stabilization-release` at `0eb5a96`, also pushed to `main`.
- Active provider: RunPod.
- Active GPU type: `A100_80GB`, provisioned as `NVIDIA A100 80GB PCIe`.
- Active model: `Qwen/Qwen2.5-7B-Instruct`.
- Active worker: RunPod instance/provider ID `52uwxf7gdw5ebv`.

Known operational follow-ups:

- Decide how long to keep RunPod instance `52uwxf7gdw5ebv` running; it was intentionally left running and continues to incur GPU cost.
- Add or configure `VASTAI_API_KEY` before attempting Vast.ai live smoke.
- Verify Alertmanager SMTP delivery with real mail credentials before relying on production email notifications.
- Continue to treat `origin/roadmap` as deferred work until Hermes, benchmark lab, frontend rewrites, and engine-specific worker images are reviewed as complete units.

## 2. What Infera Currently Optimizes

This section describes mechanisms present in the current stabilization branch. It does not claim that all of them have been benchmarked as optimal.

### vLLM-backed inference

The worker runtime is configured to use `INFERA_ENGINE=vllm` by default and the provider launch paths pass vLLM-oriented worker settings. This gives Infera the baseline efficiency properties of vLLM, including continuous batching and efficient KV-cache management inside the worker process.

Current limitation: Infera has not yet produced controlled TTFT, TPOT, throughput, or cost-per-token benchmark reports for the production model/GPU pair.

### Prefix caching, if configured

The worker config exposes `INFERA_VLLM_ENABLE_PREFIX_CACHING`, defaulting to enabled. The gateway also computes a prompt-prefix hash and uses it in affinity metadata. Together, these create a path for repeated-prefix requests to land on the same worker and benefit from vLLM-side prefix cache reuse when the worker has retained useful KV state.

Current limitation: the gateway does not yet measure prefix-cache hit rate, does not expose KV-cache locality as a routing signal, and does not prove cache benefit with benchmark data.

### Chunked prefill, if configured

The worker config exposes `INFERA_VLLM_ENABLE_CHUNKED_PREFILL`, defaulting to enabled. This allows the vLLM scheduler to avoid letting long prefill work block decode-heavy traffic when the underlying vLLM version and model configuration support it.

Current limitation: Infera does not yet report chunked-prefill impact directly. Any claimed benefit must come from future TTFT, TPOT, and throughput benchmarks.

### Gateway batching

The router has a batching manager enabled by default for non-streaming, non-high-priority requests. It groups requests per model with configurable max batch size and max wait, records batch size and batch wait metrics, and dispatches a sealed batch to a selected worker.

Current limitation: this is gateway-level request coalescing. It should be measured separately from vLLM's internal scheduler behavior. Streaming requests and high-priority requests bypass gateway batching.

### Least-loaded routing

The default strategy is `least_loaded`. It filters for workers with capacity and selects the worker with the lowest current load, using queue depth as a tie-breaker. This is the current default production routing behavior.

Current limitation: it optimizes load distribution, not cost, p95/p99 latency, TTFT, TPOT, or SLO attainment.

### Latency-based routing

The strategy engine includes `latency_based`, which scores eligible workers using a weighted combination of recent p50 latency and load. It is available in code as a strategy, but least-loaded remains the default.

Current limitation: latency-based routing currently uses p50-oriented worker stats. It does not optimize p95/p99 tails, TTFT, TPOT, or latency SLO compliance.

### Worker affinity

The gateway builds affinity metadata from an explicit `X-Infera-Affinity-Key`, authenticated session, or API key plus prompt-prefix hash. The router stores affinity bindings with a TTL and prefers the bound healthy worker when it still has capacity and serves the model.

Current limitation: this is sticky worker affinity, not full cache-locality-aware routing. Infera does not yet record affinity hit rate, prefix-cache hit rate, or measured latency savings.

### Worker health checks

Workers register with the gateway and report heartbeat/stats. The registry health checker marks workers unhealthy after missed heartbeats and removes workers after longer heartbeat gaps. The stabilization release added worker health transition metrics and alerting.

Current limitation: health state protects availability, but it does not yet feed a formal SLO-routing policy.

### Gateway backpressure

The gateway enforces max in-flight request limits. When overloaded, it rejects excess inference requests and records overload rejection metrics. This prevents the gateway from silently accumulating unbounded work.

Current limitation: backpressure is a protection mechanism. It does not automatically provision capacity or choose cheaper/slower workers under load.

### Circuit breaker behavior

The gateway maintains per-worker circuit breakers. After repeated failures, a worker circuit opens and fails fast; after a jittered reset interval, the breaker permits a half-open probe. Success closes the circuit, while failure reopens it.

Current limitation: circuit breaker state is not yet part of a rich route decision object exposed to dashboards or benchmark reports.

### Prometheus/Grafana observability

Production compose includes Prometheus, Alertmanager, and Grafana. The stabilization release added metrics and alerts for overload rejections and worker health transitions. Existing metrics also cover gateway request behavior, inference totals, batch size, and batch wait.

Route decision logging is now the next foundation for optimization work. The gateway emits structured route decision logs and Prometheus metrics for route decisions and candidates evaluated. See `docs/optimization/route-decision-logging.md`.

Current limitation: Infera does not yet expose the main optimization metrics as first-class dashboards and reports: TTFT, TPOT, p95/p99 route latency by strategy, route decision quality, cache hit rate, and cost per 1M tokens.

### Cost tracking

Provider instances carry hourly cost metadata, and the provider manager tracks active and historical instance cost. The gateway exposes cost summary APIs for dashboard use.

Current limitation: cost tracking is instance-level. It is not yet request-level cost attribution, cost-per-token reporting, or cost-aware routing.

## 3. What Infera Does Not Optimize Yet

Infera should not claim the following capabilities until they are implemented and benchmarked:

- Cost-aware routing: selecting among healthy workers/providers based on expected marginal cost.
- SLO-aware routing: choosing routes that maximize the chance of meeting a declared latency or availability SLO.
- p95/p99 tail-latency routing: using tail latency, not only p50 latency or average load, as a dispatch signal.
- TTFT/TPOT benchmark reporting: producing repeatable reports for time to first token and time per output token.
- KV-cache/prefix-cache locality-aware routing: explicitly routing repeated prefixes to workers likely to have useful KV state and measuring hit/miss impact.
- Provider-level cost comparison: comparing RunPod, Vast.ai, and future providers on normalized throughput and cost per token.
- Automatic right-sizing of GPU choice: selecting A100, H100, L40S, RTX 4090, or other GPU classes based on model, workload, SLO, and cost.
- Kubernetes-native deployment and autoscaling: Helm charts, Kubernetes health semantics, horizontal/vertical autoscaling, node pools, and cluster-native metrics.

## 4. Definition of "Optimized" for Infera

For Infera, optimization means measured improvement against explicit workload and cost baselines. A change is not optimized just because it adds a strategy or tuning flag.

Optimization should be measured by:

- Lower TTFT: reduced time from request acceptance to first generated token.
- Lower TPOT: reduced time per output token after generation starts.
- Higher throughput: more completed requests/sec and tokens/sec at a fixed error budget.
- Lower cost per request: lower estimated infrastructure cost per successful inference request.
- Lower cost per 1M tokens: lower normalized cost for generated and, where possible, input tokens.
- Lower p95/p99 latency: lower tail latency, not only lower averages.
- Lower error rate under load: fewer 429, 5xx, timeout, circuit-open, and worker-overloaded outcomes at a given concurrency.
- Predictable behavior under concurrency: stable latency and error curves as concurrency increases, with clear saturation points.

Every optimization claim should include:

- model,
- GPU type,
- provider,
- worker image,
- route strategy,
- workload file,
- concurrency level,
- request count,
- streaming mode,
- timestamp,
- before/after comparison.

## 5. Benchmark Harness Design

The future tool should be named `infera-bench`. This section is a design target only; it should not be implemented until the document is reviewed.

### CLI Inputs

The harness should support:

- configurable base URL, for example `--base-url https://inferai.co.in`;
- API key, preferably through `INFERA_BENCH_API_KEY` or `--api-key-file`;
- model, for example `--model Qwen/Qwen2.5-7B-Instruct`;
- concurrency levels, for example `--concurrency 1,4,8,16,32`;
- prompt workload file, for example `--workload workloads/short_chat.yaml`;
- streaming and non-streaming modes;
- request count per concurrency level;
- warmup request count;
- output JSON report path;
- output Markdown report path;
- optional run metadata such as provider, GPU type, worker image, git commit, and route strategy.

### Metrics To Collect

The harness should collect at least:

- TTFT p50/p95/p99;
- TPOT p50/p95/p99;
- end-to-end latency p50/p95/p99;
- requests/sec;
- tokens/sec;
- error rate;
- selected worker;
- selected provider;
- route strategy;
- route reason;
- estimated cost per request;
- estimated cost per 1M tokens.

### Measurement Rules

- TTFT should be measured only for streaming responses unless the gateway returns explicit first-token timing for non-streaming responses.
- TPOT should be computed from streamed token deltas when available. For non-streaming responses, TPOT should be marked unavailable unless the worker/gateway returns enough timing detail.
- Error rate should include HTTP failures, JSON parse failures, schema mismatches, timeouts, circuit-open responses, and incomplete streams.
- Cost estimates should identify whether they are provider-list-price estimates, active-instance amortized estimates, or measured cost records.
- Reports should include enough metadata for a later engineer to reproduce the run.

### Output Shape

The JSON report should have a stable schema. A minimal top-level shape:

```json
{
  "run_id": "bench_2026_07_02_001",
  "started_at": "2026-07-02T14:30:00Z",
  "base_url": "https://inferai.co.in",
  "model": "Qwen/Qwen2.5-7B-Instruct",
  "workload": "short_chat.yaml",
  "streaming": true,
  "git_commit": "0eb5a96",
  "results": []
}
```

The Markdown report should summarize workload, environment, key metrics, saturation point, and comparison against a named baseline.

## 6. Benchmark Workloads

Initial workload files should live under a future benchmark workload directory. Proposed files:

- `short_chat.yaml`: short single-turn prompts that measure low-latency interactive behavior.
- `long_context.yaml`: long prompts with larger prefill cost, used to measure TTFT and chunked-prefill behavior.
- `streaming_chat.yaml`: streaming prompts that measure TTFT, stream stability, and TPOT.
- `burst_traffic.yaml`: short bursts at high concurrency, used to test backpressure, queueing, and failure behavior.
- `mixed_prompts.yaml`: a mixed interactive workload with short, medium, and long prompts to expose scheduling and routing tradeoffs.

Each workload should define:

- prompt ID,
- messages,
- expected max output tokens,
- temperature and sampling settings,
- tags such as `short`, `long_context`, `streaming`, `burst`, or `cache_candidate`,
- optional expected prefix group for future cache-affinity analysis.

## 7. Routing Optimization Roadmap

Routing changes should follow benchmark instrumentation. Do not add new strategies until `infera-bench` can prove whether they improve the target metric.

### `route_decision_logging`

Status: implemented as the instrumentation foundation. The router enriches the existing route decision object with selected worker, strategy, reason, candidate count, selected provider/GPU metadata when available, and key worker load/latency signals. The gateway logs `route_decision` and `route_decision_failed` events and exports route decision metrics.

This is not a routing strategy. It exists so future cost-aware and SLO-aware strategies can be evaluated with evidence.

### `min_latency`

Goal: minimize expected request latency.

Signals:

- recent worker p50 latency;
- eventually p95/p99 latency;
- active request count;
- queue depth;
- circuit breaker state;
- model loaded status;
- provider/region health.

Use when: the caller cares about best-effort low latency but has not declared a strict SLO or cost target.

### `least_loaded`

Goal: spread work across healthy capacity.

Signals:

- current load;
- active request count;
- queue depth;
- max concurrent requests;
- worker health.

Use when: the system needs a stable default that avoids hot spots and does not require cost/SLO inputs.

### `min_cost`

Goal: minimize estimated inference cost.

Signals:

- provider cost per hour;
- measured tokens/sec for the model on that GPU type;
- estimated input and output tokens;
- active instance amortization;
- spot/on-demand mode;
- expected cold-start or queueing penalty.

Use when: latency requirements are loose and the workload is batch-oriented.

### `min_cost_under_latency_slo`

Goal: choose the cheapest candidate that is likely to meet a latency SLO.

Signals:

- all `min_cost` signals;
- p95/p99 latency by model/GPU/provider;
- current queue depth;
- route-specific historical SLO attainment;
- timeout budget remaining.

Use when: a caller has a clear latency threshold and cost matters after the threshold is satisfied.

### `slo_aware`

Goal: maximize SLO attainment across mixed traffic classes.

Signals:

- request priority or SLO class;
- per-class TTFT and end-to-end latency targets;
- recent p95/p99 latency;
- worker saturation;
- error rate;
- circuit breaker state;
- backpressure state.

Use when: interactive, batch, and internal workloads share the same gateway.

### `cache_affinity`

Goal: prefer workers likely to have reusable KV/cache state for the prompt prefix or session.

Signals:

- prompt-prefix hash;
- explicit affinity key;
- session ID or API key affinity;
- prefix-cache hit/miss history;
- worker health and capacity;
- last-seen timestamp for the prefix;
- model loaded status.

Use when: multi-turn chat or repeated-prefix workloads are common enough to justify sticky routing.

## 8. Required Routing Decision Object

The current `RoutingDecision` is intentionally small. Future benchmarking and dashboard work need a richer object that explains why a request went to a worker and what alternatives were considered.

Proposed JSON structure:

```json
{
  "request_id": "req_123",
  "model": "Qwen/Qwen2.5-7B-Instruct",
  "selected_worker": "worker_abc",
  "selected_provider": "runpod",
  "selected_gpu_type": "NVIDIA A100 80GB PCIe",
  "strategy": "least_loaded",
  "reason": "selected worker with lowest load",
  "candidates_evaluated": 3,
  "worker_queue_depth": 0,
  "worker_active_requests": 1,
  "worker_p50_latency_ms": 623.0,
  "worker_p95_latency_ms": 900.0,
  "estimated_cost_per_1m_tokens": 0.0,
  "decision_timestamp": "2026-07-02T14:30:00Z"
}
```

Additional fields can be added later, but the first version should keep the schema stable enough for benchmark reports and dashboard tables.

Implementation notes:

- `estimated_cost_per_1m_tokens` should be nullable or explicitly marked as estimated until request-level cost attribution exists.
- `worker_p95_latency_ms` should not be fabricated from p50. If p95 is unavailable, report `null`.
- The route decision should be logged for every inference request and included in benchmark output.
- The dashboard should display route decisions without exposing API keys, prompt text, or customer-sensitive metadata.

## 9. Next Implementation Tasks

### P0

- Implement `infera-bench` with configurable base URL, API key, model, concurrency levels, workload file, streaming mode, request count, and JSON/Markdown output.
- Add route decision logging for every inference request.
- Add a benchmark JSON schema and validate reports against it.
- Add benchmark workload files: `short_chat.yaml`, `long_context.yaml`, `streaming_chat.yaml`, `burst_traffic.yaml`, and `mixed_prompts.yaml`.

### P1

- Add cost-aware routing after request-level cost estimates are available.
- Add SLO-aware routing after p95/p99 and TTFT/TPOT measurements are available.
- Expose route decisions in dashboard/logs.
- Add benchmark comparison reports that compare a candidate run against a named baseline.

### P2

- Add a Kubernetes/Helm deployment path.
- Add provider cost comparison across RunPod, Vast.ai, and any future providers.
- Add cache-affinity routing with measured prefix-cache locality and hit-rate reporting.
