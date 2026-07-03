# Route Logging Deployment Verification

## Problem

Route decision logging exists in source on `main`, but the expanded production benchmark traffic on 2026-07-03 did not emit route decision evidence from the live gateway:

- No `route_decision` or `route_decision_failed` events appeared in recent gateway logs.
- Production `/metrics` did not expose `infera_route_decisions_total`.
- Production `/metrics` did not expose `infera_route_candidates_evaluated`.

The likely cause is production image drift: the source branch contains route decision logging and metrics, but the live gateway image did not appear to have been redeployed with that instrumentation.

## Expected Production Behavior After Redeploy

After deploying the gateway from latest `main`, production should expose route decision instrumentation:

- Internal gateway `/metrics` includes `infera_route_decisions_total`.
- Internal gateway `/metrics` includes `infera_route_candidates_evaluated`.
- Gateway logs include `route_decision` for successful routing decisions.
- Gateway logs may include `route_decision_failed` when routing fails.
- Later benchmark traffic can correlate routing strategy, selected worker, and candidate count without exposing prompts, API keys, authorization headers, or provider secrets.

## Verification Steps

1. Deploy the gateway from latest `main`.
2. Confirm the production gateway image, tag, or commit if available from the deployment environment.
3. Check internal gateway `/metrics` for:
   - `infera_route_decisions_total`
   - `infera_route_candidates_evaluated`
4. Provision or start exactly one worker only if inference verification requires it.
5. Run one authenticated non-streaming smoke request.
6. Run one authenticated streaming smoke request.
7. Inspect gateway logs for `route_decision`.
8. Inspect gateway logs for `route_decision_failed` only if a controlled failed routing case is exercised.
9. Stop the worker after verification if it was started only for this test.

## Safety Constraints

- Do not print API keys.
- Do not print authorization headers.
- Do not commit secrets.
- Do not commit raw smoke or benchmark response bodies.
- Do not leave a paid worker running after verification.
- Do not run load tests.
- Do not create more than one worker.
- Do not change routing behavior as part of this verification.

## Success Condition

Verification succeeds when:

- Route decision metrics are visible in internal gateway `/metrics`.
- At least one `route_decision` log event is visible after one authenticated inference request.
- The log event includes safe routing metadata such as strategy, selected worker, and candidates evaluated.
- No prompt text, API keys, authorization headers, or provider secrets appear in route decision logs.
- Any worker started for verification is stopped afterward.
- Production image drift is resolved.

## Verification Result: 2026-07-03

Status: passed.

### Deployment

Production gateway was refreshed from latest `main` to resolve the image drift found during the expanded production benchmark.

| Field | Value |
| --- | --- |
| Source branch | `main` |
| Source commit | `a55e12f1adecdc42f1c15802fced17f23646595f` |
| Gateway image | `infera-gateway` |
| Gateway image ID | `sha256:51cdbb9475c7043592db2cd5f93c1d64e449ea677e7c741be4072abb2d01cfe4` |

Public `/health` responded after redeploy, and dashboard `/api/health` responded.

### Metrics Verification

Internal gateway `/metrics` exposed route decision metrics after the redeploy.

Before smoke traffic:

| Metric | Value |
| --- | ---: |
| `infera_route_candidates_evaluated_count` | `0` |

After one authenticated non-streaming request and one authenticated streaming request:

| Metric | Value |
| --- | ---: |
| `infera_route_decisions_total{result="success",strategy="least_loaded"}` | `2` |
| `infera_route_candidates_evaluated_count` | `2` |
| `infera_route_candidates_evaluated_sum` | `2` |

### Route Decision Log Verification

Gateway logs showed two `route_decision` events from the two smoke requests. `route_decision_failed` did not appear, which was expected because both requests routed successfully.

The sampled `route_decision` events included:

- `strategy`
- `selected_worker`
- `candidates_evaluated`
- `selected_provider`
- `worker_queue_depth`
- `worker_active_requests`
- `worker_load`

Worker latency fields were not present in the sampled events.

### Worker Used For Smoke Verification

Exactly one RunPod worker was started for verification:

| Field | Value |
| --- | --- |
| Provider instance ID | `kk09izf357hzk9` |
| Worker ID | `08c944e0-d684-4b3c-a0e4-9c3cefb724bc` |
| GPU | `A100_80GB` |
| Hourly cost | `$1.19/hr` |

Authenticated `/v1/models`, one non-streaming chat request, and one streaming chat request passed. The worker was stopped after verification.

### Final Production State

Final production health after stopping the verification worker:

| Field | Value |
| --- | --- |
| `status` | `degraded` |
| `workers` | `0` |
| `healthy_workers` | `0` |
| Running/provisioning/stopping instances | none |

The provider still reported `active_instances=2`, consistent with the known provider/gateway stopped-instance count mismatch.

### Routing Notes

Public `https://inferai.co.in/metrics` served frontend HTML during verification because Caddy does not route `/metrics` publicly to the gateway. Internal gateway metrics were verified through the production Docker network and are the source of truth for Prometheus scraping.

### Success Condition Status

- Route decision metrics were visible on internal gateway `/metrics`.
- `route_decision` log events appeared after authenticated smoke requests.
- Safe routing metadata was present in the route decision logs.
- No prompt text, API keys, authorization headers, provider credentials, or raw response bodies were included in this report.
- The verification worker was stopped.
- Production image drift was resolved for gateway route decision instrumentation.
