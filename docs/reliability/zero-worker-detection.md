# Zero-Worker Detection and Worker Lifecycle Reliability

Date: 2026-07-03

## Problem Statement

Infera can have a reachable gateway while inference is unavailable. The gateway process, frontend, TLS ingress, and authenticated metadata endpoints may all respond successfully while the worker plane has dropped to zero registered workers.

The observed failure mode has four parts:

- Worker count can drop to zero.
- Provider instance state can differ from gateway worker registry state.
- Benchmarks and users may see `model_not_found` even though the gateway is up.
- A provider can still report instance records that do not map cleanly to healthy registered workers.

The reliability target is to make these states explicit and actionable. Operators should not have to infer worker-plane failure from failed inference requests.

## Current Observed Failure

During the first production `infera-bench` baseline attempt, production was reachable but degraded:

- The previous RunPod worker, `52uwxf7gdw5ebv`, had been terminated.
- Public `/health` reported `status=degraded`, `workers=0`, and `healthy_workers=0`.
- Inference requests for `Qwen/Qwen2.5-7B-Instruct` failed with `model_not_found`.
- `/v1/models` still listed the target model from the vault, so model catalog availability was not the same as live serving availability.
- Provider state and gateway registry state did not align perfectly. After stopping the restored worker, `/api/instances` showed no running/provisioning/stopping instances and target `260fqg9610xven` was stopped, while `/api/providers` still reported `active_instances: 1`. Gateway health correctly reported zero workers.

Production was restored by provisioning exactly one RunPod `A100_80GB` worker for `Qwen/Qwen2.5-7B-Instruct`. Release verification then passed. The worker was later stopped intentionally for cost control.

## Desired Behavior

- `/health` should clearly expose the zero-worker degraded state.
- The dashboard should show a prominent zero-worker warning.
- Prometheus should expose total worker count and healthy worker count.
- Alertmanager should fire when healthy workers are zero for a configured duration.
- API errors should distinguish "model not registered because no workers are available" from "unknown model."
- The runbook should explain how to restore one worker safely.

## Metrics and Alerts

Existing metrics should be reused where available. P0 exposes worker registry counts directly from the gateway at scrape time.

Implemented P0 metrics:

- `infera_workers_total`: gauge for total workers currently registered in the gateway registry.
- `infera_healthy_workers_total`: gauge for healthy workers currently registered in the gateway registry.
- `infera_unhealthy_workers_total`: gauge for registered workers that are not healthy.
- `infera_gateway_worker_health_transitions_total`: existing counter for registry-driven worker health transition events.

Deferred metric ideas:

- `infera_model_not_found_total`: counter for inference route failures where no worker serves the requested model.
- provider/gateway reconciliation metrics for provider inventory that does not align with gateway worker registration.

Implemented P0 alert rule:

- `InferaZeroHealthyWorkers`: fires when `infera_healthy_workers_total == 0` for 3 minutes.

Future alert rules:

- `InferaWorkerRegistryEmpty`: fire when `infera_workers_total == 0` for a configured duration.
- `InferaModelNotFoundSpike`: fire when `infera_model_not_found_total` increases sharply for production traffic.
- `InferaProviderGatewayInstanceMismatch`: fire when provider inventory shows running or recently active instances while gateway worker count remains zero, or when provider state cannot be reconciled with registry state.

Alert labels should include environment and, where safe, provider/model dimensions. Do not include API keys, raw prompts, or user identifiers.

## API Behavior

Inference errors should distinguish these cases:

- Gateway down.
- Gateway up but zero workers.
- Workers exist but none are healthy.
- Workers exist but none serve the requested model.
- The model is genuinely unknown to the catalog.

Implemented P0 response when no healthy workers exist at all:

```json
{
  "error": {
    "code": "no_workers_available",
    "message": "No healthy workers are currently available to serve the requested model.",
    "type": "service_unavailable",
    "retryable": true
  }
}
```

This uses HTTP `503`. If healthy workers exist but none serve the requested model, the existing model unavailable/not-found behavior is preserved so a genuinely unknown model is still distinguishable from global zero-capacity.

## Dashboard Behavior

The dashboard should expose the worker-plane state directly:

- Total workers.
- Healthy workers.
- Models currently served by registered workers.
- Provider instances grouped by state.
- P0 warning banner when `workers=0` or `healthy_workers=0`:
  "No healthy inference workers are registered. The gateway is reachable, but inference requests will fail until a worker is restored."
- Warning when provider has active/stopped/recent instances but the gateway has no workers.
- Last worker heartbeat time.
- Last worker health transition event.
- Link to the zero-worker runbook.

The dashboard should avoid implying that a model is live just because it is present in the vault. Model catalog status and live serving status should be visually distinct.

## Runbook

Use this procedure when production is reachable but inference fails, or when `/health` reports zero workers.

1. Check public health:

   ```bash
   curl -fsS https://inferai.co.in/health
   ```

   Confirm `status`, `workers`, and `healthy_workers`.

2. Check provider instances through the authenticated Infera API:

   ```bash
   curl -fsS -H "Authorization: Bearer <admin-or-operator-key>" \
     https://inferai.co.in/api/instances
   ```

   Do not paste API keys into tickets, logs, or docs.

3. Check provider status:

   ```bash
   curl -fsS -H "Authorization: Bearer <admin-or-operator-key>" \
     https://inferai.co.in/api/providers
   ```

   Compare provider active counts with gateway worker count.

4. Verify provider instance state:

   - Running/provisioning instance with no gateway worker means worker startup, worker network, or registration is failing.
   - Stopped/terminated instance with zero workers means serving capacity is absent.
   - Provider active count mismatch should be treated as a reconciliation signal, not as proof of serving capacity.

   For RunPod-specific diagnosis of a provider-running instance that never registers with the gateway, see [RunPod Worker Registration Reliability](./runpod-worker-registration.md).

5. Inspect worker logs through the provider or deployment tooling.

   Look for:

   - model loading errors,
   - GPU allocation errors,
   - worker HTTP server startup,
   - gateway registration attempts,
   - heartbeat failures,
   - authentication failures,
   - wrong gateway URL.

6. Verify worker configuration:

   - `INFERA_ROUTER_ADDRESS` points to the production gateway.
   - `INFERA_WORKER_SHARED_TOKEN` matches the gateway configuration.
   - `INFERA_PRELOAD_MODELS` contains the intended model exactly.
   - Worker image is the intended pinned image.

7. Restart the existing worker if possible.

   Use the supported provider or Infera instance management path. Do not create duplicate workers unless the existing instance is stopped, terminated, or unrecoverable.

8. Provision exactly one worker if needed.

   For the current production benchmark baseline, the expected target is:

   - Provider: RunPod
   - GPU: `A100_80GB`
   - Provider GPU type: `NVIDIA A100 80GB PCIe`
   - Model: `Qwen/Qwen2.5-7B-Instruct`

9. Verify model availability:

   ```bash
   curl -fsS -H "Authorization: Bearer <smoke-key>" \
     https://inferai.co.in/v1/models
   ```

   Confirm the model is listed and, when dashboard support exists, that it is live-served by a healthy worker.

10. Run one non-streaming smoke request.

    Use a short prompt and small `max_tokens`.

11. Run one streaming smoke request.

    Confirm the stream emits at least one data chunk and `[DONE]`.

12. Stop the worker after benchmark work if cost control is needed.

    Use the supported Infera instance management path and verify `/health` degrades to zero workers intentionally.

## Implementation Roadmap

### P0

- Implemented: metrics expose total, healthy, and unhealthy worker counts.
- Implemented: `InferaZeroHealthyWorkers` fires when healthy workers remain zero for 3 minutes.
- Implemented: inference returns `503` with `no_workers_available` when no healthy workers exist.
- Implemented: dashboard shows a prominent zero-worker warning.
- Implemented: alert annotations link to this runbook.

### P1

- Add provider/gateway instance reconciliation warning.
- Add a model availability endpoint or dashboard section that separates catalog models from live-served models.
- Add release verification checks for zero-worker state before declaring production healthy.

### P2

- Add optional auto-recovery or provision-one-worker flow.
- Add a scheduled worker health audit.
- Add a cost-aware idle shutdown policy.

## Success Condition

A future operator should be able to distinguish:

- gateway down,
- gateway up but zero workers,
- worker exists but unhealthy,
- provider instance running but worker not registered,
- model genuinely unknown.

The operator should also have a clear, safe path to restore exactly one worker when production serving capacity is absent.
