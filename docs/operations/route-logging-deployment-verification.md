# Route Logging Deployment Verification

## Problem

Route decision logging exists in source on `main`, but the expanded production benchmark traffic on 2026-07-03 did not emit route decision evidence from the live gateway:

- No `route_decision` or `route_decision_failed` events appeared in recent gateway logs.
- Production `/metrics` did not expose `infera_route_decisions_total`.
- Production `/metrics` did not expose `infera_route_candidates_evaluated`.

The likely cause is production image drift: the source branch contains route decision logging and metrics, but the live gateway image did not appear to have been redeployed with that instrumentation.

## Expected Production Behavior After Redeploy

After deploying the gateway from latest `main`, production should expose route decision instrumentation:

- `/metrics` includes `infera_route_decisions_total`.
- `/metrics` includes `infera_route_candidates_evaluated`.
- Gateway logs include `route_decision` for successful routing decisions.
- Gateway logs may include `route_decision_failed` when routing fails.
- Later benchmark traffic can correlate routing strategy, selected worker, and candidate count without exposing prompts, API keys, authorization headers, or provider secrets.

## Verification Steps

1. Deploy the gateway from latest `main`.
2. Confirm the production gateway image, tag, or commit if available from the deployment environment.
3. Check production `/metrics` for:
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

- Route decision metrics are visible in production `/metrics`.
- At least one `route_decision` log event is visible after one authenticated inference request.
- The log event includes safe routing metadata such as strategy, selected worker, and candidates evaluated.
- No prompt text, API keys, authorization headers, or provider secrets appear in route decision logs.
- Any worker started for verification is stopped afterward.
- Production image drift is resolved.
