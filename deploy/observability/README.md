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
- `ALERT_EMAIL_TO`
- `ALERT_SMTP_FROM`
- `ALERT_SMTP_SMARTHOST`
- `ALERT_SMTP_USERNAME`
- `ALERT_SMTP_PASSWORD`

Default receiver is `codingtensor@gmail.com` unless overridden.

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
curl -I https://dashboard.inferai.co.in
docker compose -f docker-compose.prod.yml logs alertmanager --tail=100
```

Then log in to Grafana and open the `Infera / Infera Overview` dashboard.
