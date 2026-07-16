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

The gateway serializes SQLite usage writes through a single writer. Request completion waits for an acknowledgement from that writer. Temporary failures are retried three times before the gateway emits `inference.audit_persist_failed` with the request ID. Graceful shutdown drains acknowledged writes before closing the database.

Migration version 3 removes duplicate legacy rows by retaining the newest row for each workspace/request pair before creating the unique idempotency index. Legacy successful events remain billable and are classified as having unknown accuracy.
