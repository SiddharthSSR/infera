# Usage metering and reconciliation

Infera records one durable usage event per logical inference request. The idempotency key is `(workspace_id, request_id)`, so replaying a request ID within the same workspace cannot create a second billable record. The same request ID may be used independently in another workspace.

## Metering semantics

- `attempts` counts every persisted terminal request outcome, including failures and cancellations.
- `requests` counts billable successful requests only.
- `tokens` counts tokens from billable successful requests only.
- `successes` and `errors` preserve operational outcomes; failed requests do not inflate billable request or token totals.
- Quota checks use billable `requests` and `tokens`, not total attempts.

Each event attributes the workspace, API key prefix, model, selected worker, streaming mode, terminal status, error code, latency, prompt tokens, completion tokens, and total tokens.

## Token accuracy

Token totals are classified as:

- `exact`: the worker reported both prompt and completion token counts.
- `estimated`: neither component was reported, so Infera estimated both.
- `mixed`: the worker reported only part of the usage or only an exact total, while at least one displayed component was estimated.
- `unknown`: no successful token measurement was available, normally for failed attempts.

The usage API returns `exact_requests`, `estimated_requests`, `exact_tokens`, and `estimated_tokens`. For aggregation, mixed and unknown billable measurements are included in the estimated fields so customer-visible totals never imply more precision than the underlying event supports.

## Reconciliation

`GET /api/audit/usage` and the workspace usage read view include a reconciliation object. A healthy response has:

```json
{
  "status": "ok",
  "discrepancies": []
}
```

Possible discrepancy codes are:

- `attempt_status_mismatch`: attempts do not equal successes plus errors.
- `request_accuracy_mismatch`: billable requests do not equal exact plus estimated requests.
- `token_accuracy_mismatch`: billable tokens do not equal exact plus estimated tokens.

Treat any `mismatch` response as a metering incident. Preserve the database and gateway logs, stop billing exports for the affected period, and compare the affected workspace and time range against individual `inference_audit` rows before correcting data.

## Persistence behavior

The gateway serializes usage writes through a per-process writer. Request completion waits for an acknowledgement from that writer. Temporary failures are retried three times before the gateway emits `inference.audit_persist_failed` with the request ID. Graceful shutdown drains acknowledged writes before closing the database. In multi-replica deployments, PostgreSQL provides the shared durability and concurrency boundary; SQLite remains restricted to one replica.

Migration version 3 removes duplicate legacy rows by retaining the newest row for each workspace/request pair before creating the unique idempotency index. Legacy successful events remain billable and are classified as having unknown accuracy.

PostgreSQL enforces first-write semantics with a unique `(workspace_id, request_id)` key. Quota reservation retries are keyed by execution ID, and transaction-scoped advisory locks serialize each execution and workspace quota period before committed plus in-flight usage is evaluated.

## Cost summary read view

`GET /api/costs` reads provider-infrastructure spend from a shared cost-session
ledger without changing its response shape. Each session is keyed by workspace,
instance, and UTC start time, and stores the immutable v1 provider price
snapshot in integer nano-USD per hour. Identical lifecycle retries are
idempotent; conflicting starts or stops fail reconciliation.

Every request first reconciles the tenant's durable managed-instance state with
the shared sessions, then calculates:

- `current_hourly`, `by_provider`, and `by_gpu` from active durable instances;
- `today_total` and `month_total` by splitting session overlap across UTC
  half-open windows; and
- `projected_month` from the current UTC month total and day.

This ledger preserves provider-infrastructure semantics, including paid idle
capacity. It is intentionally separate from request-attributed
`inference_audit.cost_nano`, so token/cost accuracy classifications and
first-write inference identities remain unchanged.

The endpoint returns `503` when durable instance state or the shared ledger is
unavailable, when lifecycle evidence conflicts, or when the requested month
predates the shared ledger's coverage marker. Consumers must show the result as
unavailable rather than converting it to zero.

Migration version 7 creates the shared session schema and coverage marker. It
does not import legacy `costs.db` rows: those floating-point stop-date
aggregates lack lifecycle identities, UTC interval boundaries, and reliable
cross-replica deduplication. Production historical repair therefore requires a
separately reviewed, stop-the-world export and import with backups,
deterministic tenant-scoped deduplication, interval reconstruction, and
post-import reconciliation. Until that repair is approved, pre-cutover month
queries fail closed. Do not merge replica-local ledgers or repair production
rows in place.
