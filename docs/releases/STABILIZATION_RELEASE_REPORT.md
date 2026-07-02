# Stabilization Release Report

Date: 2026-06-30

## Branch

- Stabilization branch: `task/stabilization-release`
- Base: `origin/main` / `v1.3.0` (`57394a8`)
- Release notes draft: [STABILIZATION_RELEASE_NOTES.md](/Users/siddharthsingh/codingtensor/infera/docs/releases/STABILIZATION_RELEASE_NOTES.md)
- Rationale: `origin/main` is the last known stable baseline. `origin/roadmap` and the original checkout contain a large mixed stack of Hermes, modular engines, benchmark lab, frontend refactors, generated contracts, and uncommitted work. Those changes should land later as complete, tested units rather than being carried into a stabilization release blindly.
- Refreshed remote audit after `git fetch origin --prune` on 2026-06-30:
  - `origin/main` remains `57394a8` (`v1.3.0`).
  - `origin/roadmap` is `a9714db` and remains a broad mixed branch: 259 files changed versus `origin/main`, about 44k insertions, including Hermes agents/runtime, benchmark lab, frontend UI rewrites, worker engine images, generated/test artifacts, and recent login page refinements.
  - The original checkout at `/Users/siddharthsingh/codingtensor/infera` is still a dirty `task/reduce-control-plane-polling` worktree with many modified and untracked roadmap files; it was not edited by this stabilization branch.

## Changes Kept

This branch keeps the `v1.3.0` production hardening already on `origin/main`, then adds nine small release-readiness changes:

- `c09cd52 chore(release): add worker health transition metrics`
  - Adds registry-driven worker health transition events.
  - Exposes `infera_gateway_worker_health_transitions_total`.
  - Adds `InferaWorkerHealthTransitionsHigh` alert and runbook guidance.
- `c9fc306 chore(release): expose gateway overload rejections`
  - Exposes `infera_gateway_inference_rejected_total{reason="overloaded"}`.
  - Records gateway max in-flight backpressure rejections.
  - Adds `InferaGatewayOverloadRejections` alert and runbook guidance.
- Additional observability update:
  - Adds dashboard panels for gateway inference rejections and worker health transitions.
- `43aabe7 fix(release): allow model-less smoke verification`
  - Aligns `scripts/smoke-test.sh` with README/deployment docs so `INFERA_SMOKE_MODEL` is optional.
  - Keeps health and `/v1/models` smoke coverage when chat checks are skipped.
- `c356e02 chore(python): satisfy worker lint checks`
  - Applies Ruff-compatible cleanup across Python worker and focused tests.
  - Keeps the existing worker runtime surface intact while making `ruff check .` pass.
- `56b5f5b chore(release): validate worker image pinning`
  - Adds a release guard that rejects missing, untagged, or `:latest` worker images.
  - Runs the guard from the production compose smoke path before Docker startup.
- `de242c8 chore(release): validate production env inputs`
  - Adds `scripts/validate-prod-env.sh` to check required production variable names without printing secret values.
  - Reuses worker image pin validation for `INFERA_WORKER_IMAGE`.
  - Runs from the production compose smoke path before Docker startup.
- Compose smoke hardening:
  - Retries ingress checks so Caddy can finish accepting HTTP traffic after the container reaches `running`.
- Documentation alignment:
  - Adds the worker image validator to README and roadmap release checklist deployment steps.
  - Adds an explicit production compose render gate to README and `DEPLOYMENT_CHECKLIST.md`.
  - Marks the roadmap checklist's March release-candidate status as historical and points stabilization operators back to this report.
  - Adds a stabilization-specific release notes draft and points release hygiene at it.

## Deferred

- Hermes agents/runtime and playground work from `origin/roadmap`.
- Modular multi-engine worker backend, including SGLang and TensorRT-LLM adapters.
- Benchmark lab expansion and generated benchmark artifacts.
- Large frontend componentization/contract-generation changes from the dirty roadmap worktree.
- Latest `origin/roadmap` login-page animation/refinement commits (`a9714db`, `7c738ba`) because they are UI polish on top of the larger roadmap stack, not isolated release-risk fixes.
- Engine-specific worker image matrix beyond the stable `INFERA_WORKER_IMAGE` path already required by production compose.

Reasons for deferral:

- The roadmap work is broad and cross-stack.
- Several pieces were untracked or generated in the original checkout.
- Audit findings identified known correctness risks in Python runtime behavior, frontend tests, and generated contract rollout if committed partially.

## Validation Run

Passed:

- `go test ./internal/auth ./internal/deployments ./internal/gateway ./internal/vault -count=1`
- `go test ./internal/audit ./internal/migrate -count=1`
- `GOCACHE=/private/tmp/infera-go-cache go test ./internal/gateway -count=1`
- `CGO_ENABLED=0 go test ./internal/router/... -count=1`
- `CGO_ENABLED=0 GOCACHE=/private/tmp/infera-go-cache go test ./internal/router/... -count=1`
- `CGO_ENABLED=0 go test ./internal/providers/... ./pkg/types/... -count=1`
- `npm run test:run` in `frontend`
  - Result: 22 test files passed, 113 tests passed.
- `npm run build` in `frontend`
  - Result: production build passed.
  - Note: Vite still warns that the main JS chunk is larger than 500 kB; this is inherited and non-blocking for this stabilization branch.
- `npm run lint` in `frontend`
  - Result: exits successfully with warnings.
- `/private/tmp/infera-py312-venv/bin/pytest -q` in `python`
  - Result: 108 passed.
- `/private/tmp/infera-py312-venv/bin/ruff check .` in `python`
  - Result: all checks passed.
- `PYTHONPYCACHEPREFIX=/private/tmp/infera-pycache /opt/homebrew/bin/python3.12 -m py_compile $(find python/src python/tests -type f -name '*.py' -print)`
  - Result: Python worker source and tests syntax-compile successfully with Python 3.12.
- `bash -n scripts/smoke-test.sh scripts/release-verify.sh scripts/compose-smoke-prod.sh scripts/build-docker.sh scripts/backup-sqlite.sh scripts/validate-worker-targets.sh scripts/validate-worker-image-pin.sh scripts/validate-prod-env.sh`
- `python3 -m json.tool deploy/observability/grafana/dashboards/infera-overview.json`
- `python3` YAML load of `deploy/observability/prometheus/rules/infera-alerts.yml`
  - Result: parsed successfully and confirmed `InferaGatewayOverloadRejections` and `InferaWorkerHealthTransitionsHigh` exist.
- `rg -n 'validate-worker-image-pin|non-\`latest\`|sha256' README.md docs/releases/ROADMAP_MAIN_RELEASE_CHECKLIST.md`
- `rg -n "2026-06-30|STABILIZATION_RELEASE_REPORT|Do not promote" docs/releases/ROADMAP_MAIN_RELEASE_CHECKLIST.md`
- `rg -n "Stabilization Release Notes Draft|INFERA_WORKER_IMAGE|Production compose smoke passed" docs/releases/STABILIZATION_RELEASE_NOTES.md`
- `rg -n "STABILIZATION_RELEASE_NOTES|RELEASE_TEMPLATE" DEPLOYMENT_CHECKLIST.md`
- `git fetch origin --prune`
- `git diff --stat origin/main...origin/roadmap`
- `bash scripts/validate-worker-image-pin.sh ghcr.io/example/infera-worker:v1.3.0`
- `bash scripts/validate-worker-image-pin.sh ghcr.io/example/infera-worker@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef`
- `bash scripts/validate-worker-image-pin.sh ghcr.io/example/infera-worker@sha256:0123456789abcdef`
  - Result: failed as expected because the digest is not a full SHA-256 hex digest.
- `bash scripts/validate-worker-image-pin.sh ghcr.io/example/infera-worker:latest`
  - Result: failed as expected because `latest` is not production-pinned.
- `ENV_FILE=/tmp/infera-prod-env-test ./scripts/validate-prod-env.sh`
  - Result: passed with dummy required env values and did not print secret values.
- `ENV_FILE=/tmp/does-not-exist INFERA_ADMIN_KEY=... INFERA_ALLOWED_ORIGINS=... INFERA_GATEWAY_ADDRESS=... INFERA_WORKER_SHARED_TOKEN=... INFERA_WORKER_IMAGE=ghcr.io/example/infera-worker:v1.3.0 GRAFANA_ADMIN_USER=... GRAFANA_ADMIN_PASSWORD=... ALERT_EMAIL_TO=... ALERT_SMTP_FROM=... ALERT_SMTP_SMARTHOST=... ALERT_SMTP_USERNAME=... ALERT_SMTP_PASSWORD=... ./scripts/validate-prod-env.sh`
  - Result: passed using exported env values and did not print secret values.
- `ENV_FILE=/tmp/infera-prod-env-missing ./scripts/validate-prod-env.sh`
  - Result: failed as expected and printed only missing variable names.
- `ENV_FILE=/tmp/infera-prod-env-latest ./scripts/validate-prod-env.sh`
  - Result: failed as expected through worker image pin validation.
- `INFERA_SMOKE_API_KEY=inf_test SMOKE_TIMEOUT=3 ./scripts/smoke-test.sh http://127.0.0.1:18080`
  - Result: passed against a local mock health/models server with no `INFERA_SMOKE_MODEL`.
- `INFERA_SMOKE_API_KEY=inf_test VERIFY_TIMEOUT=3 SMOKE_TIMEOUT=3 INFERA_DASHBOARD_URL=http://127.0.0.1:18081 INFERA_GATEWAY_INTERNAL_URL=http://127.0.0.1:18081 ./scripts/release-verify.sh http://127.0.0.1:18081`
  - Result: passed against a local mock app/dashboard/gateway server with worker-target discovery and no `INFERA_SMOKE_MODEL`.
- `docker compose -f docker-compose.prod.yml config --quiet` with dummy required env vars.
- `rg -n "docker compose -f docker-compose.prod.yml config --quiet" README.md DEPLOYMENT_CHECKLIST.md`
- `REMOVE_COMPOSE_VOLUMES=true SMOKE_TIMEOUT=180 ./scripts/compose-smoke-prod.sh`
  - Result: passed. Production env validation ran without printing values, gateway and frontend images built, gateway and frontend health checks passed, Caddy started, ingress `/health`, authenticated `/v1/models`, and root HTML checks passed.
- `git diff --check`
- `set -a; . /Users/siddharthsingh/codingtensor/infera/.env; set +a; ./scripts/validate-worker-image-pin.sh`
  - Result: the locally configured `INFERA_WORKER_IMAGE` is pinned to an explicit non-`latest` tag or digest. The exact value was not printed.

Not completed:

- Full `go test ./...` was not used as the primary validation command because macOS Go 1.22.4 produced `dyld: missing LC_UUID load command` for several non-SQLite test binaries with cgo enabled. SQLite-backed packages were tested with cgo enabled; router, provider, and shared type packages were tested with `CGO_ENABLED=0`.
- Real production env validation now passes from `/Users/siddharthsingh/codingtensor/infera/.env` after filling the missing local production keys. Values were not printed.
- Real production compose render now passes with `docker compose --env-file /Users/siddharthsingh/codingtensor/infera/.env -f docker-compose.prod.yml config --quiet`.
- Local production compose smoke now passes with the completed local `.env`: gateway and frontend built, gateway and frontend became healthy, Caddy started, ingress `/health`, authenticated `/v1/models`, and root HTML checks passed.
- Canary unauthenticated reachability was attempted against the configured `INFERA_BASE_URL` from `.env`; the ngrok URL responded `404` for `/`, `/health`, and `/api/health`, so it is not currently pointing at the expected Infera app/gateway routes.
- Authenticated canary verification was not run because the approval reviewer blocked sending the local admin/smoke key as a bearer token to the configured external ngrok URL until that destination is explicitly approved as trusted.
- Live RunPod/Vast.ai smoke still could not run honestly from local state. A RunPod key is present, but the configured canary URL is not serving Infera routes and no trusted gateway endpoint is available for authenticated provisioning/inference verification. No Vast.ai key was present.
- Post-deploy log watch could not run against production because no production deployment target or remote log access was available in local configuration. Local compose smoke logs were exercised by the smoke script before cleanup.
- `task/stabilization-release` has been published to `origin/task/stabilization-release` after explicit approval to export the branch to GitHub.

## Remaining Manual Production Checks

- Replace placeholder Alertmanager SMTP values in `.env` with real mail credentials before relying on production email notifications.
- Point `INFERA_BASE_URL`, `INFERA_DASHBOARD_URL`, and `INFERA_GATEWAY_INTERNAL_URL` at a trusted deployment that serves Infera routes.
- Run `scripts/release-verify.sh` against the trusted canary deployment with `INFERA_SMOKE_API_KEY`.
- If a live model should be checked, set `INFERA_SMOKE_MODEL` and optionally `INFERA_SMOKE_STREAM=1`.
- Confirm worker discovery returns targets from `/internal/prometheus/worker-targets`.
- Run one live RunPod or Vast.ai provisioning smoke if provider credentials are available.
- Watch gateway, Caddy, Prometheus, Grafana, and Alertmanager logs for at least 10-15 minutes after canary deploy.
