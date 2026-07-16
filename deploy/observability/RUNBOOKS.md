# Infera Observability Runbooks

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

## InferaInferenceTTFTHigh

1. Check the `Inference TTFT p95 by Model` panel in Grafana and identify which model is affected.
2. Compare TTFT against `Batch Wait p95 by Model` to separate queueing delay from model prefill delay.
3. Confirm warm workers exist for the affected model and that recent requests are not cold-starting new capacity.
4. If TTFT regressed after a deploy, compare worker image, model preload behavior, and model cache persistence on RunPod.

## InferaInferenceTPOTHigh

1. Inspect the `Inference TPOT p95 by Model` panel and verify whether the issue is isolated to one model family.
2. Compare worker GPU utilization, active requests, and queue depth to determine whether decode is saturated.
3. Check batching behavior and KV-cache locality; if batch wait is low but TPOT is high, decode efficiency is the likely bottleneck.
4. If sustained, reduce concurrency for the affected model or benchmark a more suitable quantization/runtime config.

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

## Multiple gateway replicas rejected

1. Check `INFERA_GATEWAY_REPLICAS` and `INFERA_AUDIT_LEDGER_BACKEND` in the deployment secret set.
2. This release supports the SQLite audit/quota ledger only and therefore requires exactly one
   gateway replica. Do not place SQLite on a shared network filesystem as an HA workaround.
3. Restore `INFERA_GATEWAY_REPLICAS=1` to recover service safely.
4. Use INF-42 to track the shared transactional ledger, cross-replica quota tests, migration,
   backup, restore, and rollback work required before enabling active-active gateways.
