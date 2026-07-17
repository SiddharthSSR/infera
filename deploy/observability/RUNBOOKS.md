# Infera Observability Runbooks

The versioned SLO definitions and exact/derived/unavailable measurement contract are in [SLO.md](SLO.md).

## InferaSLOAvailabilityBurn

1. Select the alert's `model` and `routing_strategy` in the `Infera Overview` dashboard. Confirm request rate is non-zero; absent traffic intentionally does not page.
2. Separate pre-route failures (`model="unknown"`, `routing_strategy="unknown"`) from post-route failures. Pre-route failures point to gateway quota, validation, router, or healthy-capacity problems.
3. Check gateway logs for the matching error codes and route-decision failures. High gateway CPU, in-flight load, or overload rejections indicates a gateway cause.
4. For routed failures, compare worker health transitions, queue depth, batch wait, and worker logs. Connection/read failures with healthy gateway load indicate a worker cause.
5. If workers are healthy but inference calls fail or slow across a provider, inspect provider instance/network health and capacity. Compare other providers before changing routing.
6. If requests fail with `quota_unavailable` or reservation/write errors, inspect the authorization store and audit ledger/PostgreSQL health. Preserve fail-closed quota behavior; do not bypass the ledger.
7. Use TTFT, TPOT, and end-to-end panels together: high TTFT with normal TPOT suggests routing, queueing, cold start, or prefill; normal TTFT with high TPOT suggests decode saturation; high end-to-end with both normal can indicate gateway transfer or ledger finalization overhead.

## InferaGatewayDown

1. Check container status:
   - `docker compose -f docker-compose.prod.yml ps`
2. Check gateway logs:
   - `docker compose -f docker-compose.prod.yml logs gateway --tail=300`
3. Check health from inside network:
   - `docker compose -f docker-compose.prod.yml exec prometheus wget -qO- http://gateway:8080/health`
4. If restart loop persists, inspect `data/` mount and required env vars.

## InferaGatewayHigh5xxRate

1. Inspect recent gateway errors:
   - `docker compose -f docker-compose.prod.yml logs gateway --since=15m`
2. Check Caddy upstream errors:
   - `docker compose -f docker-compose.prod.yml logs caddy --since=15m`
3. Check worker availability:
   - `curl -H "Authorization: Bearer $INFERA_ADMIN_KEY" https://inferai.co.in/api/workers`
4. If caused by recent deploy, roll back to previous known-good commit.

## InferaGatewayP95LatencyHigh

1. Validate current RPS and in-flight load in Grafana (`Infera Overview`).
2. Confirm worker capacity and loaded models.
3. Check provider-side latency and instance health.
4. If sustained, scale workers or reduce per-request token limits.

## InferaSLOTTFTSustainedHigh

1. Check the `SLO v1 TTFT Operational + 14d p95` panel in Grafana and identify the model, routing strategy, and measurement quality affected. The alert requires both 5-minute and 30-minute p95 to exceed 2 seconds for 10 minutes.
2. Confirm `SLO v1 Measurement Availability (14d)` still has usable samples. Unavailable or absent TTFT does not fire this alert.
3. Compare TTFT against `Batch Wait p95 by Model` to separate queueing delay from model prefill delay.
4. Confirm warm workers exist for the affected model and that recent requests are not cold-starting new capacity.
5. If TTFT regressed after a deploy, compare worker image, model preload behavior, and model cache persistence on RunPod.

## InferaSLOTPOTSustainedHigh

1. Inspect the `SLO v1 TPOT Operational + 14d p95` panel and verify whether the issue is isolated to one model family, routing strategy, or measurement quality. The alert requires both 5-minute and 30-minute p95 to exceed 100ms for 10 minutes.
2. Confirm `SLO v1 Measurement Availability (14d)` still has usable samples. Unavailable or absent TPOT does not fire this alert.
3. Compare worker GPU utilization, active requests, and queue depth to determine whether decode is saturated.
4. Check batching behavior and KV-cache locality; if batch wait is low but TPOT is high, decode efficiency is the likely bottleneck.
5. If sustained, reduce concurrency for the affected model or benchmark a more suitable quantization/runtime config.

## InferaBatchWaitHigh

1. Inspect `Batch Wait p95 by Model` and `Batch Size avg by Model` together to see whether queues are waiting without forming useful batches.
2. Confirm the affected model has enough healthy workers and that router load metrics are updating.
3. If batch size stays small while wait time rises, reduce `MaxBatchWaitMS` or add warm capacity for that model.
4. If wait time rises with large batches, decode throughput is saturated and additional worker capacity is likely needed.

## InferaGatewayOverloadRejections

1. Check `infera_gateway_http_in_flight_requests` against the configured `INFERA_MAX_IN_FLIGHT_REQUESTS`.
2. Compare overload rejection rate with `Batch Wait p95 by Model` and worker queue depth to identify whether pressure is gateway-local or worker-capacity related.
3. If worker capacity is healthy and gateway CPU/memory are saturated, scale the gateway or raise `INFERA_MAX_IN_FLIGHT_REQUESTS` cautiously.
4. If workers are saturated, add capacity for the affected models before raising the gateway limit.

## InferaWorkerHealthTransitionsHigh

1. Inspect gateway logs for `marking worker unhealthy after missed heartbeats` and `removing worker after missed heartbeats`.
2. Check whether transitions are concentrated on one provider, model, or worker image revision.
3. Verify gateway-to-worker network reachability and that workers are still sending heartbeats with the expected shared token.
4. If transitions started after a deploy, roll back the worker image or gateway revision and reprovision affected workers.

## InferaWorkerHeartbeatAuthRejected

1. Confirm gateway and worker shared token values match exactly.
2. Verify worker env was applied to running workers (reprovision if needed).
3. Check gateway logs for `Invalid worker token`.
4. Update secret and restart gateway/workers in coordinated rollout.

## Coordinated gateway and worker rollout

The executable rollout/rollback procedure, ownership, validation gates, recovery evidence, and
RPO/RTO targets are defined in `docs/operations/deployment-recovery.md`.

1. Build and publish pinned gateway and worker images from the same reviewed commit.
2. Set one `INFERA_RELEASE_ID` and `INFERA_WORKER_PROTOCOL_VERSION` for the release, then run
   `./scripts/validate-prod-env.sh` before changing the running stack.
3. Drain or stop existing provider workers before replacing the gateway. Old workers cannot be
   assumed compatible with a changed authentication or registration contract.
4. Deploy the gateway, reprovision workers from the pinned worker image, and confirm `/health`
   reports the expected release and protocol identity.
5. Run `INFERA_RELEASE_ID=... INFERA_WORKER_PROTOCOL_VERSION=... ./scripts/release-verify.sh`
   and verify worker registration, authenticated inference, and streaming inference.
6. Roll back gateway and worker images together. Do not combine a rolled-back gateway with
   workers from the failed release unless the protocol contract was explicitly proven compatible.
   Before rollback, confirm the target gateway supports the active control-state schema; never
   point an older incompatible binary at a database already migrated by the candidate.

## Audit ledger startup or quota failures

1. Check `INFERA_GATEWAY_REPLICAS`, `INFERA_CONTROL_STATE_DSN`,
   `INFERA_AUDIT_LEDGER_BACKEND`, and whether both DSN secrets are present. Never print a DSN or
   place SQLite on a shared filesystem.
2. For multiple replicas, confirm every replica uses `postgres` and the same database. Check
   PostgreSQL connectivity, TLS, connection capacity, storage, and transaction lock waits.
   Separately confirm every replica uses the same control-state database and encryption key.
3. `quota_unavailable` can mean `authHandler.Store().GetWorkspaceQuota` failed against the
   authorization/configuration store, or that the audit/PostgreSQL ledger failed during
   reservation. Check both stores and their logs. Preserve fail-closed behavior: do not disable
   hard limits or switch any replica to a local ledger as a workaround.
4. For cutover or rollback, follow `docs/operations/shared-audit-ledger.md`; do not run old SQLite
   writers alongside PostgreSQL writers.
