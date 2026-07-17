# Cost attribution contract

Infera records request cost in the durable inference audit ledger. The immutable identity is `(workspace_id, request_id)`: an identical replay is idempotent and a replay with different usage, status, price, or cost data is rejected. Provider price changes therefore affect only later executions.

## Units and accuracy

- Provider prices are snapshotted as integer nanocurrency units per time unit. The current self-hosted provider contract is `USD` per `hour`, version `provider-instance-hourly-v1`.
- Attributed request cost is stored as integer nano-USD. API summaries convert this to USD and expose cost per attributed request, per attributed token, and per 1,000,000 attributed tokens.
- Token accuracy is `exact`, `mixed`, `estimated`, or `unknown`. A request is exact only when both worker-reported prompt and completion counts are present. Missing components use the existing character-based estimator and make the request mixed or estimated.
- Cost accuracy is `estimated` for active-instance amortization, `exact` only for future provider-supplied request charges, and `unavailable` when no positive price snapshot exists. Missing cost is never represented as zero-cost evidence.

## Active-instance amortization

`active_instance_time_share_v1` estimates a request as:

`instance_price_per_hour × request_elapsed_ms ÷ 3,600,000 ÷ observed_active_concurrency`

The router reports active requests before dispatch, so the current execution adds one. The concurrency observation is persisted with the price and result. Prices and arithmetic that are non-finite, non-positive, or outside the durable integer nano-USD range fail closed as `unavailable`; cost multiplication and half-up rounding use checked integer arithmetic.

Benchmark groups use the analogous `active_instance_group_time_share_v1`. A benchmark row is one paired sample containing two physical inference requests: its non-stream request followed by its stream request. The benchmark measures wall time around both complete concurrent phases, charges the active instance once for that combined window, and reports:

- `cost_per_request_usd`: group cost divided by the number of physical inference requests (`2 × row count`);
- `cost_per_paired_sample_usd`: group cost divided by row count;
- `cost_query_usd`: a deprecated compatibility alias for `cost_per_request_usd`;
- `cost_per_token_usd`: group cost divided by tokens from both requests. Non-stream usage is provider-reported; because the stream response has no final usage in this benchmark protocol, its prompt count reuses the paired non-stream prompt count and its completion count is estimated from delivered characters. `cost_token_accuracy` is therefore `estimated`.

The artifact also records the measured group wall milliseconds, physical request count, paired sample count, and token denominator. It never reconstructs the billed group window from a maximum per-request latency. A CLI price must be finite and strictly positive; invalid programmatic inputs or invalid group timing emit `cost_accuracy: unavailable` and no cost metrics.

This is an allocation estimate, not a provider invoice. Stale worker concurrency, idle time outside request windows, autoscaling overlap, startup time, storage, network charges, taxes, discounts, and provider billing granularity are not included.

## Retries, failures, streaming, and reconciliation

- Durable-write retries reuse the full immutable record and cannot add cost twice. Duplicate delivery is idempotent by execution identity; conflicting reuse fails.
- Quota reservations do not carry cost. Reservation deletion/reconciliation happens in the same transaction as the first audit write and cannot add another attribution.
- Once a worker is selected and a valid instance price exists, failed and canceled attempts retain their elapsed instance-time cost even when they are not billable usage. Pre-dispatch failures and missing-price attempts are `unavailable`.
- Streaming uses final worker usage when emitted; otherwise it estimates missing token components from the prompt and delivered text. Partial/failed streams retain the best observed usage and elapsed cost. Non-streaming uses worker usage with the same mixed/estimated fallback rules.
- Usage aggregates include failure cost in total request cost. The cost-per-token denominator includes tokens observed on cost-attributed attempts; unavailable-price attempts are excluded from both cost and that denominator, and their count remains visible in accuracy metadata.

Cost evidence is measurement only. Routing policy must not consume it until the separate cost-aware routing work is implemented and validated.
