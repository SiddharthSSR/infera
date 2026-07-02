# Stabilization Release Notes Draft

## Title

`v1.3.1-stabilization`

## Summary

- Keeps `origin/main` / `v1.3.0` as the stable base.
- Defers the broad `origin/roadmap` stack until Hermes, benchmark lab, frontend rewrites, and engine-specific worker images are reviewed as complete units.
- Adds targeted release-readiness improvements for gateway backpressure visibility, worker health instability, production compose confidence, smoke verification, and deployment operator guidance.

## Highlights

- Gateway now records max in-flight inference rejections with `infera_gateway_inference_rejected_total{reason="overloaded"}`.
- Registry-driven worker health changes now emit `infera_gateway_worker_health_transitions_total`.
- Prometheus alerts, runbook entries, and Grafana overview panels cover overload rejections and worker health transitions.
- Smoke verification works without `INFERA_SMOKE_MODEL`, while still validating `/health`, `/api/health`, and authenticated `/v1/models`.
- Production compose smoke passed locally with gateway, frontend, Caddy ingress, health, models, and root document checks.
- Worker image pinning is validated before deploy with `scripts/validate-worker-image-pin.sh`.

## Included Work

### Reliability and Deployment

- Added a production worker image pin guard for missing, untagged, `:latest`, and malformed digest values.
- Added a production env validator that checks required variable names without printing secret values.
- Added explicit production compose render checks to deployment docs.
- Hardened production compose smoke ingress checks with retries after Caddy startup.
- Confirmed `docker-compose.prod.yml` renders with dummy required env values.
- Confirmed `scripts/compose-smoke-prod.sh` validates required env names and passes with Docker available.

### Observability

- Added worker health transition metrics, alert rules, and runbook guidance.
- Added overload rejection metrics, alert rules, and runbook guidance.
- Added Grafana panels for gateway inference rejections and worker health transitions.

### Gateway and Routing

- Recorded max in-flight inference rejection metrics before worker dispatch.
- Added registry health transition callbacks for missed-heartbeat unhealthy and removal events.
- Kept callbacks outside the registry mutex to avoid blocking registry operations.

### Worker Runtime

- Applied Ruff-compatible cleanup across the Python worker and focused tests.
- Kept the stable v1.3.0 worker runtime surface; broader modular engine work remains deferred.

### Documentation

- Added `docs/releases/STABILIZATION_RELEASE_REPORT.md`.
- Marked the March roadmap release checklist status as historical for stabilization purposes.
- Updated deployment docs for worker image pin validation and production compose rendering.

## Verification

- Go targeted gateway/router/provider/auth/vault/deployment tests passed, using `CGO_ENABLED=0` for non-SQLite router/provider/type packages affected by the local macOS Go 1.22.4 cgo test-binary issue.
- Python worker suite passed: `108 passed`.
- Python Ruff passed: `ruff check .`.
- Frontend tests passed: `22` test files, `113` tests.
- Frontend production build passed with the inherited large-chunk warning.
- Shell syntax checks passed for release, smoke, compose, backup, worker-target, worker-image-pin, and prod-env scripts.
- Grafana dashboard JSON parses.
- Prometheus alert YAML parses and includes the new release alerts.
- Production compose config renders with dummy required env values.
- Production compose smoke passed locally.
- The local production `.env` now validates and production compose renders with that env file.
- Local production compose smoke passed with the completed local `.env`.
- Local mock `smoke-test.sh` and `release-verify.sh` checks passed without `INFERA_SMOKE_MODEL`.
- Production droplet `infera-prod-1` is deployed from `task/stabilization-release` at `cbe0d24`.
- Public site, public gateway health, public Grafana health, and authenticated `/v1/models` pass on production.
- Production RunPod provider visibility is restored: `/api/providers` returns connected RunPod status and `/api/offerings` returns 150 offerings.
- Production gateway health is degraded only because no workers are registered; internal worker-target discovery returns `[]`.
- A 10-minute post-deploy watch completed after the rebuild with gateway/frontend healthy and only routine Caddy/Grafana log activity.

## Deploy Notes

- Set `INFERA_WORKER_IMAGE` to the exact release worker image tag or full digest.
- The locally configured worker image pin was validated without printing its value. Record the exact production worker image here before publishing release notes externally:
  - `INFERA_WORKER_IMAGE=<registry>/infera-worker:<tag-or-@sha256-digest>`
- Validate required env names without printing values:
  - `./scripts/validate-prod-env.sh`
- Render production compose with real env before deploying:
  - `docker compose -f docker-compose.prod.yml config --quiet`
- Run canary verification with a real smoke key:
  - `INFERA_SMOKE_API_KEY=<smoke-key> ./scripts/release-verify.sh <canary-url>`
- If validating live inference, set `INFERA_SMOKE_MODEL` and optionally `INFERA_SMOKE_STREAM=1`.

## Known Follow-ups

- Replace placeholder Alertmanager SMTP values with real mail credentials before relying on email notifications.
- Explicitly approve launching a paid RunPod worker if production should be kept non-degraded. The verified available smoke target is one `A100_80GB` worker using `NVIDIA A100 80GB PCIe` at about `$1.19/hr`.
- After a worker is running, run production worker-target discovery and one `/v1/chat/completions` smoke.
- Add or configure `VASTAI_API_KEY` before attempting Vast.ai live smoke.
