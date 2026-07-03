# Route Decision Logging

Date: 2026-07-03

## Purpose

Route decision logging gives Infera a durable record of how each successful inference request was assigned to a worker, and why routing failed when no worker could be selected. This is a prerequisite for cost-aware routing, SLO-aware routing, and benchmark reports that explain performance by selected worker/provider rather than only aggregate latency.

This work does not change routing decisions. It exposes the existing decision made by the current strategy.

## Captured Fields

Successful `route_decision` events capture the stable routing decision fields that are available at gateway route time:

- `request_id`
- `model`
- `strategy`
- `selected_worker`
- `selected_provider`, when present in worker tags
- `selected_gpu_type`, when present in worker tags
- `reason`
- `candidates_evaluated`
- `worker_queue_depth`
- `worker_active_requests`
- `worker_p50_latency_ms`, when reported by the worker
- `worker_p99_latency_ms`, when reported by the worker
- `worker_load`
- `selected_worker_score`
- `decision_timestamp`

Failed `route_decision_failed` events capture:

- `request_id`
- `model`
- `error_code`
- `reason`
- `healthy_workers`

## Intentionally Omitted Fields

Route decision events must not include:

- prompt text,
- chat message content,
- API keys,
- authorization headers,
- worker shared tokens,
- provider credentials,
- raw provider API responses,
- user personal data.

Route logging should remain safe for normal operational log pipelines. If future debugging needs request-level context, it should use request IDs and audit records, not prompt or credential material.

## Metrics

The gateway exports:

- `infera_route_decisions_total{strategy,result}`: count of route decisions by strategy and result.
- `infera_route_candidates_evaluated`: histogram of candidate worker counts considered by route decisions.

The `strategy` label is set to `unknown` for failures that occur before a strategy can select a worker.

## Example Successful Log

```json
{
  "msg": "route_decision",
  "request_id": "req_123",
  "model": "Qwen/Qwen2.5-7B-Instruct",
  "strategy": "least_loaded",
  "selected_worker": "worker_abc",
  "selected_provider": "runpod",
  "selected_gpu_type": "A100_80GB",
  "reason": "selected worker with lowest load",
  "candidates_evaluated": 3,
  "worker_queue_depth": 0,
  "worker_active_requests": 1,
  "worker_p50_latency_ms": 623,
  "worker_load": 0.25,
  "selected_worker_score": 0.75,
  "decision_timestamp": "2026-07-03T00:00:00Z"
}
```

## Example Failed Log

```json
{
  "msg": "route_decision_failed",
  "request_id": "req_124",
  "model": "Qwen/Qwen2.5-7B-Instruct",
  "error_code": "no_workers_available",
  "reason": "No healthy workers are currently available to serve the requested model.",
  "healthy_workers": 0
}
```

## Benchmark Exposure

Route decisions are not exposed in public benchmark response metadata yet. There is no current safe debug header or endpoint that can return route decision data without expanding the public API surface.

Future benchmark work should add a safe mechanism for `infera-bench` to read route decision metadata, such as response headers or an authenticated debug endpoint. That mechanism must not expose prompt text, API keys, authorization headers, worker shared tokens, provider credentials, or raw provider responses.

## Future Routing Work

Route decision logging supports future optimization work by making these comparisons possible:

- latency and error rate by strategy,
- latency and error rate by selected provider,
- saturation behavior by candidate count,
- whether least-loaded choices actually reduce queue depth,
- whether latency-based routing improves TTFT or p95/p99 latency,
- whether future cost-aware routing reduces cost without harming SLOs.

Cost-aware and SLO-aware routing should use this data as evidence, not as an implementation shortcut. This task intentionally does not add new routing strategies.
