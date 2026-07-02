# Stabilization Release Report

Date: 2026-06-30
Last updated: 2026-07-02

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

This branch keeps the `v1.3.0` production hardening already on `origin/main`, then adds ten small release-readiness changes:

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
- `cbe0d24 fix(gateway): fall back to env provider config`
  - Preserves workspace-specific provider credentials when configured.
  - Falls back to globally registered env provider credentials when a workspace has no provider override.
  - Restores production `RUNPOD_API_KEY` visibility for default-workspace provider, offering, and provisioning APIs.
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
- `ENV_FILE=/Users/siddharthsingh/codingtensor/infera/.env ./scripts/validate-prod-env.sh`
  - Result: passed after filling missing local production keys. Values were not printed.
- `docker compose --env-file /Users/siddharthsingh/codingtensor/infera/.env -f docker-compose.prod.yml config --quiet`
  - Result: passed with the completed local `.env`.
- `set -a; . /Users/siddharthsingh/codingtensor/infera/.env; set +a; REMOVE_COMPOSE_VOLUMES=true SMOKE_TIMEOUT=180 ./scripts/compose-smoke-prod.sh`
  - Result: passed with the completed local `.env`. Gateway and frontend built, gateway and frontend became healthy, Caddy started, ingress `/health`, authenticated `/v1/models`, and root HTML checks passed.
- `GOCACHE=/private/tmp/infera-go-cache go test ./internal/auth ./internal/providers ./cmd/gateway -count=1`
  - Result: `./internal/auth` passed and `./cmd/gateway` compiled; `./internal/providers` hit the known local macOS cgo test-binary `dyld: missing LC_UUID load command` issue.
- `CGO_ENABLED=0 GOCACHE=/private/tmp/infera-go-cache go test ./internal/providers -count=1`
  - Result: passed.
- `GOCACHE=/private/tmp/infera-go-cache go test ./internal/gateway -count=1`
  - Result: passed.

Not completed:

- Full `go test ./...` was not used as the primary validation command because macOS Go 1.22.4 produced `dyld: missing LC_UUID load command` for several non-SQLite test binaries with cgo enabled. SQLite-backed packages were tested with cgo enabled; router, provider, and shared type packages were tested with `CGO_ENABLED=0`.
- Live RunPod worker provisioning smoke passed after explicit approval for ongoing GPU cost. One `A100_80GB` worker was launched on RunPod with `provider_gpu_type_id="NVIDIA A100 80GB PCIe"` and model `Qwen/Qwen2.5-7B-Instruct`; instance/provider ID `52uwxf7gdw5ebv`. The worker registered with the gateway and production `/health` reported `status: healthy`, `workers: 1`, `healthy_workers: 1`. The worker was intentionally left running.
- Live inference smoke passed against `Qwen/Qwen2.5-7B-Instruct`: `/v1/chat/completions` returned `200`, object `chat.completion`, content preview `OK`, and usage `{prompt_tokens: 36, completion_tokens: 2, total_tokens: 38}`.
- Vast.ai live smoke was not run because no `VASTAI_API_KEY` is configured in production.

## Production Droplet Audit

Checked on 2026-07-02:

- DigitalOcean production droplet found: `infera-prod-1`, public IP `157.245.103.209`, region `blr1`, tags `infera` and `production`.
- Production compose project found at `/opt/infera`; six services are running: gateway, frontend, Caddy, Prometheus, Grafana, and Alertmanager.
- Deployed code is `task/stabilization-release` at `cbe0d24`.
- Production `.env` on the droplet has all required production variables present, including `INFERA_ADMIN_KEY`, `INFERA_WORKER_SHARED_TOKEN`, `INFERA_WORKER_IMAGE`, Grafana credentials, Alertmanager SMTP settings, `RUNPOD_API_KEY`, and `HF_TOKEN`. Values were not printed.
- `docker compose -f docker-compose.prod.yml config --quiet` passes on the droplet. Docker Compose warns that `VASTAI_API_KEY` is unset and defaults to blank.
- Internal gateway auth smoke with the deployed `INFERA_ADMIN_KEY` passes: `/v1/models` returns a JSON `data` array with 13 model records.
- Public checks pass for `https://inferai.co.in/`, `https://inferai.co.in/health`, `https://inferai.co.in/api/health`, `https://dashboard.inferai.co.in/`, and `https://dashboard.inferai.co.in/api/health`.
- Public authenticated `/v1/models` passes with the deployed production admin key and returns 13 model records.
- RunPod provider status now passes through the production API: `connected: true`, `active_instances: 0`, and account ID is returned. Values were not printed beyond non-secret status fields.
- Production offerings now pass through the API: `/api/offerings` returns 150 RunPod offerings, including `NVIDIA A100 80GB PCIe` availability at about `$1.19/hr` for one GPU.
- Gateway health is healthy after the approved RunPod worker launch: `/health` reported `workers: 1`, `healthy_workers: 1`.
- Worker registration is confirmed by the gateway health response after launching RunPod instance `52uwxf7gdw5ebv`.
- Public `/internal/prometheus/worker-targets` returns frontend HTML, not JSON, because Caddy does not route `/internal/*` publicly. This is expected for public ingress but means `release-verify.sh` needs `INFERA_GATEWAY_INTERNAL_URL` to be run on the host or against a trusted internal endpoint.
- Authenticated public smoke with the local `.env` key fails with `401`; the local key is not the deployed production admin key.
- Gateway and frontend were rebuilt/restarted from `task/stabilization-release`; both became healthy.
- 10-minute post-deploy watch completed from `2026-07-02T06:12:16Z` to `2026-07-02T06:22:16Z`. Gateway and frontend remained healthy, and Caddy/Grafana logs showed routine maintenance/check activity only.
- Live RunPod `A100_80GB` worker launch executed after explicit approval and was left running: instance/provider ID `52uwxf7gdw5ebv`.

## Remaining Manual Production Checks

- Replace placeholder Alertmanager SMTP values in `.env` with real mail credentials before relying on production email notifications.
- Decide how long to keep RunPod instance `52uwxf7gdw5ebv` running; it was intentionally left running after smoke approval and continues to incur GPU cost.
- Add or configure a `VASTAI_API_KEY` before attempting Vast.ai live smoke.
