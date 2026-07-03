# Safe Route Decision Metadata

Date: 2026-07-03

## Purpose

Route decision metrics and logs prove how the gateway routed inference requests, but benchmark reports need request-level route context without reading production logs. Safe route decision metadata gives `infera-bench` an opt-in response header that can connect latency, TTFT, throughput, and errors to the routing strategy and selected worker.

This does not change routing behavior and does not add cost-aware or SLO-aware routing.

## Requesting Metadata

Authenticated clients can request route metadata by sending:

```http
X-Infera-Debug-Route: true
```

When the header is present, the gateway responds with:

```http
X-Infera-Route-Decision: <base64url-json>
```

The value is compact JSON encoded with unpadded base64url so it is safe to carry in an HTTP response header. The header is omitted by default.

## Exposed Fields

The header may include these fields when available:

- `request_id`
- `model`
- `strategy`
- `selected_worker`
- `selected_provider`
- `selected_gpu_type`
- `reason`
- `candidates_evaluated`
- `worker_queue_depth`
- `worker_active_requests`
- `worker_p50_latency_ms`
- `worker_p95_latency_ms`
- `worker_load`
- `decision_timestamp`

Unavailable values are omitted. The gateway does not fabricate latency or load signals that the router did not have at decision time.

## Intentionally Excluded Fields

The metadata header must not include:

- prompt text;
- chat message content;
- API keys;
- authorization headers;
- worker shared tokens;
- provider credentials;
- raw internal logs;
- raw provider API responses;
- request or response bodies.

`selected_worker` is exposed as the gateway worker ID because this endpoint is already authenticated and the header is opt-in. If worker IDs become sensitive across tenants, this should be replaced with a stable hashed or truncated worker reference before enabling broader customer-facing use.

## Benchmark Usage

`infera-bench --capture-route-decision` sends `X-Infera-Debug-Route: true`, decodes `X-Infera-Route-Decision`, and stores the safe route fields per sample in the JSON report.

Markdown reports summarize:

- strategies observed;
- selected workers observed;
- candidates evaluated p50/p95/p99;
- missing route metadata count.

The flag is disabled by default so normal benchmark runs do not request additional response metadata.

## Example Decoded Header

```json
{
  "request_id": "req_123",
  "model": "Qwen/Qwen2.5-7B-Instruct",
  "strategy": "least_loaded",
  "selected_worker": "worker_abc",
  "selected_provider": "runpod",
  "selected_gpu_type": "A100_80GB",
  "reason": "selected healthy worker with lowest load",
  "candidates_evaluated": 1,
  "worker_queue_depth": 0,
  "worker_active_requests": 1,
  "worker_load": 0.25,
  "decision_timestamp": "2026-07-03T00:00:00Z"
}
```

## Future Work

Production verification should happen in a separate task after this change is deployed. That verification should run a tiny benchmark with `--capture-route-decision`, confirm benchmark reports include route summaries, and stop any worker started for the test.
