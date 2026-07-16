# AGENTS.md

## Scope

`deploy/` holds production-oriented Dockerfiles, ingress config, and observability assets used by `docker-compose.prod.yml` and release verification scripts.

## Key Paths

- `docker/`: Dockerfiles and runtime entrypoints for gateway, frontend, and worker images
- `caddy/Caddyfile`: public ingress routing
- `observability/`: Prometheus, Alertmanager, Grafana, and runbooks
- `../docker-compose.prod.yml`: production compose stack definition
- `../scripts/compose-smoke-prod.sh`: CI smoke for prod compose
- `../scripts/release-verify.sh`: post-deploy verification

## Working Rules

- Keep environment variable names consistent across compose, Dockerfiles, scripts, and docs.
- Treat observability changes as operational contract changes: update runbooks or README notes when alerting, routing, or discovery behavior changes.
- Production worker scraping is gateway-driven dynamic discovery. Do not reintroduce static production dependence on `observability/prometheus/worker_targets.json`.
- Prefer validating compose/config/script alignment over making isolated file edits.

## Commands

- Validate compose config: `docker compose -f docker-compose.prod.yml config`
- Run prod compose smoke: `bash ./scripts/compose-smoke-prod.sh`
- Run release verification: `bash ./scripts/release-verify.sh https://inferai.co.in`

## Pitfalls

- `docker-compose.prod.yml` requires real env vars for admin auth, allowed origins, worker token, Grafana credentials, and SMTP settings.
- `scripts/compose-smoke-prod.sh` injects smoke defaults in CI; do not copy those defaults into production config.
- Caddy, Grafana, and the gateway are coupled in the production stack; routing or health-check edits need end-to-end verification.

## Validation

- Validate compose syntax after deploy/config edits.
- Run the relevant smoke or release verification script when behavior changes affect ingress, health checks, or observability.
- Note any required secret or environment updates explicitly in the change summary.
