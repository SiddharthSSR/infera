# Observability Bootstrap

This directory contains baseline monitoring config for Infera production:

- Prometheus scrape + alert rules
- Alertmanager routing + email templates
- Grafana datasource and dashboard provisioning
- Starter dashboard for gateway + worker metrics

## Services

`docker-compose.prod.yml` now includes:

- `prometheus` on internal Docker network (`:9090`)
- `alertmanager` on internal Docker network (`:9093`)
- `grafana` on internal Docker network (`:3000`)

Caddy routes `dashboard.inferai.co.in` to Grafana over TLS.

## Worker Scrape Discovery

Prometheus now discovers worker metrics dynamically from the gateway.

- Discovery endpoint: `http://gateway:8080/internal/prometheus/worker-targets`
- Refresh interval: `30s`

Workers register themselves with the gateway, and healthy workers are exposed
to Prometheus automatically. A newly provisioned worker should appear on the
dashboard without manually editing `worker_targets.json`.

The old `deploy/observability/prometheus/worker_targets.json` file is no longer
the active source of truth for production scraping.

Discovery labels now include:

- `service`
- `env`
- `worker_id`
- `status`
- `provider` (when reported by the worker)
- `engine` (when reported by the worker)
- `version` (when reported by the worker)

Static build metadata is exposed via:

- `infera_gateway_info`
- `infera_worker_info`

## Required Environment Variables

Set in `.env`:

- `GRAFANA_ADMIN_USER`
- `GRAFANA_ADMIN_PASSWORD`
- `ALERT_EMAIL_TO`
- `ALERT_SMTP_FROM`
- `ALERT_SMTP_SMARTHOST`
- `ALERT_SMTP_USERNAME`
- `ALERT_SMTP_PASSWORD`
- These must be set to real SMTP/email values before go-live; production compose no longer supplies placeholder defaults.

## Gmail SMTP Notes

For Gmail, use:

- `ALERT_SMTP_SMARTHOST=smtp.gmail.com:587`
- `ALERT_SMTP_USERNAME=<gmail-address>`
- `ALERT_SMTP_PASSWORD=<gmail-app-password>`

Use an App Password (not your normal Gmail account password).

## Quick Verify

After deploy:

```bash
docker compose -f docker-compose.prod.yml ps
curl --fail --silent --show-error --max-time 5 https://dashboard.inferai.co.in/api/health
docker compose -f docker-compose.prod.yml logs alertmanager --tail=100
```

For local or dev deployments, you can probe Grafana directly without DNS/TLS:

```bash
curl --fail --silent --show-error --max-time 5 http://localhost:3000/api/health
```

Then log in to Grafana and open the `Infera / Infera Overview` dashboard.
