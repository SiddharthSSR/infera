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

## InferaWorkerHeartbeatAuthRejected

1. Confirm gateway and worker shared token values match exactly.
2. Verify worker env was applied to running workers (reprovision if needed).
3. Check gateway logs for `Invalid worker token`.
4. Update secret and restart gateway/workers in coordinated rollout.
