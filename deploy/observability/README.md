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

## What To Watch After Deploy

Start with these panels in `Infera / Infera Overview`:

- `Inference TTFT p95 by Model (s)` to catch cold-start, routing, or prefill regressions.
- `Inference TPOT p95 by Model (s)` to catch decode-side slowdowns after the first token.
- `Batch Wait p95 by Model (s)` to see whether requests are stalling in the queue before dispatch.
- `Batch Size avg by Model` to confirm batching is actually coalescing useful work.

Alert expectations:

- `InferaInferenceTTFTHigh` should stay quiet during normal warm traffic.
- `InferaInferenceTPOTHigh` usually indicates saturated decode throughput or poor runtime config.
- `InferaBatchWaitHigh` means queueing delay is becoming user-visible and should be read alongside batch size.

Recommended post-deploy check:

1. Confirm the new alert rules appear in Prometheus and Alertmanager.
2. Generate a few chat requests against one hot model and verify the TTFT/TPOT panels move.
3. Provision enough concurrent traffic to form batches and confirm batch wait and batch size both populate.
4. If TTFT rises without batch wait rising, look at warm pool, model load time, and routing. If batch wait rises first, add capacity or reduce batch wait.

If you have fresh benchmark JSON from [`scripts/benchmark-chat.py`](/Users/siddharthsingh/codingtensor/infera/scripts/benchmark-chat.py), use [`scripts/suggest-alert-thresholds.py`](/Users/siddharthsingh/codingtensor/infera/scripts/suggest-alert-thresholds.py) to derive a first-pass TTFT/TPOT/batch-wait threshold set before editing Prometheus rules. The helper now emits a copy-paste model-specific Prometheus snippet as well as the raw threshold suggestions.
