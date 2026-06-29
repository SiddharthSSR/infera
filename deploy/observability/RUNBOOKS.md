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
