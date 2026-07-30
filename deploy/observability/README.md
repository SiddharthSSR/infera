# Observability Bootstrap

This directory contains baseline monitoring config for Infera production:

- Prometheus scrape + alert rules
- Alertmanager routing + email templates
- Grafana datasource and dashboard provisioning
- Starter dashboard for gateway + worker metrics
- Versioned inference SLO definitions, recording rules, and multi-window burn alerts

The customer-facing contract is documented in [`SLO.md`](SLO.md). SLO v1 preserves exact, derived, and unavailable latency semantics instead of substituting zero for missing measurements.

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

## Production environment source

Production commands must first load the reviewed root-only pointer:

```bash
. /etc/infera/production-env-source
export INFERA_PRODUCTION_ENV_FILE
./scripts/validate-prod-env.sh
```

The pointer sets only `INFERA_PRODUCTION_ENV_FILE=/etc/infera/production.env`; it contains no
secret values. The target must be an absolute, root-owned regular file with no group/world access.
The shared deployment/recovery/Prometheus wrapper rejects symlinks, legacy env selectors,
duplicates, missing names, and ambient production-variable precedence. The source must contain the
full validator-required name set, including real Grafana and Alertmanager values, before go-live.

## Gmail SMTP Notes

For Gmail, use:

- `ALERT_SMTP_SMARTHOST=smtp.gmail.com:587`
- `ALERT_SMTP_USERNAME=<gmail-address>`
- `ALERT_SMTP_PASSWORD=<gmail-app-password>`

Use an App Password (not your normal Gmail account password).

## Quick Verify

After deploy:

```bash
. ./scripts/production-env-source.sh
production_compose -f docker-compose.prod.yml ps
curl --fail --silent --show-error --max-time 5 https://dashboard.inferai.co.in/api/health
production_compose -f docker-compose.prod.yml logs alertmanager --tail=100
```

For local or dev deployments, you can probe Grafana directly without DNS/TLS:

```bash
curl --fail --silent --show-error --max-time 5 http://localhost:3000/api/health
```

Then log in to Grafana and open the `Infera / Infera Overview` dashboard.

## Fail-closed SLO rule reload

Use `scripts/prometheus-safe-reload.sh` only while public ingress is already drained and both
gateway replicas report zero workers. The wrapper never drains or restores ingress and never
restarts Prometheus. It requires a new, empty mode-0700 evidence directory and validates the
currently reviewed contract before sending a container-local lifecycle reload:

- configured image `prom/prometheus:v2.55.1` with image ID
  `sha256:2659f4c2ebb718e7695cb9b25ffa7d6be64db013daba13e05c875451cf51b0d3`;
- `prometheus.yml` SHA-256
  `10b5521ca8a8f0c9c8113ed70ce6d6cf863a07619d3a4efa6f1a4072841d4b53`;
- baseline alert rules SHA-256
  `053732934fa41f2b59cfe0b73a1a1c7fb839f46527fa4cb15d6df14c3575419f`;
- SLO v1 rules SHA-256
  `cb2464e859dfd672e998fe3270d0008a52fd39c1657dbd1de4980d593ec390a2`;
- reviewed pre-SLO rollback rules SHA-256
  `f5b7774f0611bfb89f9d9ad56eabf6b67fc645cda52a4d4c12690a11b89e644b`.

Run it from the exact reviewed checkout:

```bash
umask 077
export INFERA_BASE_URL=https://inferai.co.in
source /etc/infera/production-env-source
export INFERA_PRODUCTION_ENV_FILE
export INFERA_PROMETHEUS_SOURCE_REVISION=<full-reviewed-git-sha>
export INFERA_PROMETHEUS_EVIDENCE_DIR=/secure/evidence/prometheus-reload-<utc-run-id>
./scripts/prometheus-safe-reload.sh
```

The wrapper requires that revision to be the clean checkout `HEAD` and records it on every
sanitized evidence line. Success means the same container and restart count remain in place,
`lastConfigTime` advanced, both expected targets stayed up, `InferaZeroHealthyWorkers` remained
the only firing alert, and all 44 rules in the four expected groups are healthy. A rejected reload
leaves the last good runtime unchanged and returns nonzero.

If the lifecycle endpoint accepts the candidate but semantic verification fails, the wrapper
renames the exact reviewed prior rules into the active glob, moves the candidate SLO file to
`rules/rollback/infera-slo-v1.candidate.disabled` with mode 0600, reloads once, and verifies the
two prior groups and 10 healthy rules. It then returns nonzero with ingress still drained. This
retained rollback state is intentional, not temporary residue. Do not restart Prometheus or retry
the wrapper. Reconcile the checkout to the exact reviewed candidate through the normal deployment
procedure, re-prove every precondition, and use a fresh empty evidence directory.

## What To Watch After Deploy

Start with these panels in `Infera / Infera Overview`:

- `SLO v1 Availability Attainment (14d)` for the actual 99% availability objective filtered by model and routing strategy.
- `SLO v1 Latency Objective Attainment (14d)` for the share of eligible E2E, TTFT, and TPOT samples meeting their p95 targets.
- `SLO v1 End-to-end/TTFT/TPOT Operational + 14d p95` panels to compare short-window diagnostics with objective-window values.
- `SLO v1 Measurement Availability (14d)` to distinguish exact, derived, and unavailable TTFT/TPOT requests.
- `Batch Wait p95 by Model (s)` to see whether requests are stalling in the queue before dispatch.
- `Batch Size avg by Model` to confirm batching is actually coalescing useful work.

Alert expectations:

- `InferaSLOAvailabilityFastBurn` pages only when both 5-minute and 1-hour windows exceed 14.4x the 1% error budget and recent traffic exists.
- `InferaSLOAvailabilitySlowBurn` warns only when both 30-minute and 6-hour windows exceed 6x the budget and recent traffic exists.
- No SLO burn alert fires merely because inference traffic is absent; `InferaGatewayDown` covers missing gateway telemetry.
- `InferaSLOTTFTSustainedHigh` requires elevated SLO-v1 p95 on both 5-minute and 30-minute windows with usable samples.
- `InferaSLOTPOTSustainedHigh` applies the same sustained-window contract to derived TPOT samples.
- `InferaBatchWaitHigh` means queueing delay is becoming user-visible and should be read alongside batch size.

Recommended post-deploy check:

1. Confirm the new alert rules appear in Prometheus and Alertmanager.
2. Generate a few chat requests against one hot model and verify the TTFT/TPOT panels move.
3. Provision enough concurrent traffic to form batches and confirm batch wait and batch size both populate.
4. If TTFT rises without batch wait rising, look at warm pool, model load time, and routing. If batch wait rises first, add capacity or reduce batch wait.

If you have fresh benchmark JSON from [`scripts/benchmark-chat.py`](/Users/siddharthsingh/codingtensor/infera/scripts/benchmark-chat.py), use [`scripts/suggest-alert-thresholds.py`](/Users/siddharthsingh/codingtensor/infera/scripts/suggest-alert-thresholds.py) to derive a first-pass TTFT/TPOT/batch-wait threshold set before editing Prometheus rules. The helper now emits a copy-paste model-specific Prometheus snippet as well as the raw threshold suggestions.
