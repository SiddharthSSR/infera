# Observability Bootstrap

This directory contains baseline monitoring config for Infera production:

- Prometheus scrape + alert rules
- Grafana datasource and dashboard provisioning
- Starter dashboard for gateway + worker metrics

## Services

`docker-compose.prod.yml` now includes:

- `prometheus` on internal Docker network (`:9090`)
- `grafana` on internal Docker network (`:3000`)

Caddy routes `dashboard.inferai.co.in` to Grafana over TLS.

## Configure Worker Scrape Targets

Prometheus uses file-based discovery for worker metrics:

- File: `deploy/observability/prometheus/worker_targets.json`

Default is `[]` (no worker targets).

Use `deploy/observability/prometheus/worker_targets.example.json` as template.
Each target should be `host:port` where worker metrics are reachable at `/metrics`.

## Required Environment Variables

Set in `.env`:

- `GRAFANA_ADMIN_USER`
- `GRAFANA_ADMIN_PASSWORD`

## Quick Verify

After deploy:

```bash
docker compose -f docker-compose.prod.yml ps
curl -I https://dashboard.inferai.co.in
```

Then log in to Grafana and open the `Infera / Infera Overview` dashboard.
