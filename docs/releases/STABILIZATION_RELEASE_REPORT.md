# Stabilization Release Report

Date: 2026-06-29

## Branch

- Stabilization branch: `task/stabilization-release`
- Base: `origin/main` / `v1.3.0` (`57394a8`)
- Rationale: `origin/main` is the last known stable baseline. `origin/roadmap` and the original checkout contain a large mixed stack of Hermes, modular engines, benchmark lab, frontend refactors, generated contracts, and uncommitted work. Those changes should land later as complete, tested units rather than being carried into a stabilization release blindly.

## Changes Kept

This branch keeps the `v1.3.0` production hardening already on `origin/main`, then adds three small release-readiness commits:

- `c09cd52 chore(release): add worker health transition metrics`
  - Adds registry-driven worker health transition events.
  - Exposes `infera_gateway_worker_health_transitions_total`.
  - Adds `InferaWorkerHealthTransitionsHigh` alert and runbook guidance.
- `c9fc306 chore(release): expose gateway overload rejections`
  - Exposes `infera_gateway_inference_rejected_total{reason="overloaded"}`.
  - Records gateway max in-flight backpressure rejections.
  - Adds `InferaGatewayOverloadRejections` alert and runbook guidance.
- `43aabe7 fix(release): allow model-less smoke verification`
  - Aligns `scripts/smoke-test.sh` with README/deployment docs so `INFERA_SMOKE_MODEL` is optional.
  - Keeps health and `/v1/models` smoke coverage when chat checks are skipped.
- Additional release guard:
  - Adds a release guard that rejects missing, untagged, or `:latest` worker images.
  - Runs the guard from the production compose smoke path before Docker startup.

## Deferred

- Hermes agents/runtime and playground work from `origin/roadmap`.
- Modular multi-engine worker backend, including SGLang and TensorRT-LLM adapters.
- Benchmark lab expansion and generated benchmark artifacts.
- Large frontend componentization/contract-generation changes from the dirty roadmap worktree.
- Engine-specific worker image matrix beyond the stable `INFERA_WORKER_IMAGE` path already required by production compose.

Reasons for deferral:

- The roadmap work is broad and cross-stack.
- Several pieces were untracked or generated in the original checkout.
- Audit findings identified known correctness risks in Python runtime behavior, frontend tests, and generated contract rollout if committed partially.

## Validation Run

Passed:

- `go test ./internal/auth ./internal/deployments ./internal/gateway ./internal/vault -count=1`
- `go test ./internal/audit ./internal/migrate -count=1`
- `CGO_ENABLED=0 go test ./internal/router/... -count=1`
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
- `bash -n scripts/smoke-test.sh scripts/release-verify.sh scripts/compose-smoke-prod.sh scripts/build-docker.sh scripts/backup-sqlite.sh scripts/validate-worker-targets.sh scripts/validate-worker-image-pin.sh`
- `bash scripts/validate-worker-image-pin.sh ghcr.io/example/infera-worker:v1.3.0`
- `bash scripts/validate-worker-image-pin.sh ghcr.io/example/infera-worker@sha256:0123456789abcdef`
- `bash scripts/validate-worker-image-pin.sh ghcr.io/example/infera-worker:latest`
  - Result: failed as expected because `latest` is not production-pinned.
- `INFERA_SMOKE_API_KEY=inf_test SMOKE_TIMEOUT=3 ./scripts/smoke-test.sh http://127.0.0.1:18080`
  - Result: passed against a local mock health/models server with no `INFERA_SMOKE_MODEL`.
- `INFERA_SMOKE_API_KEY=inf_test VERIFY_TIMEOUT=3 SMOKE_TIMEOUT=3 INFERA_DASHBOARD_URL=http://127.0.0.1:18081 INFERA_GATEWAY_INTERNAL_URL=http://127.0.0.1:18081 ./scripts/release-verify.sh http://127.0.0.1:18081`
  - Result: passed against a local mock app/dashboard/gateway server with worker-target discovery and no `INFERA_SMOKE_MODEL`.
- `docker compose -f docker-compose.prod.yml config --quiet` with dummy required env vars.
- `git diff --check`

Not completed:

- Full `go test ./...` was not used as the primary validation command because macOS Go 1.22.4 produced `dyld: missing LC_UUID load command` for several non-SQLite test binaries with cgo enabled. SQLite-backed packages were tested with cgo enabled; router, provider, and shared type packages were tested with `CGO_ENABLED=0`.
- `REMOVE_COMPOSE_VOLUMES=true SMOKE_TIMEOUT=180 ./scripts/compose-smoke-prod.sh` was attempted but did not run because Docker could not connect to the local daemon at `unix:///Users/siddharthsingh/.docker/run/docker.sock`.

## Remaining Manual Production Checks

- Set the production `INFERA_WORKER_IMAGE` to the exact release tag or digest and record it in release notes.
- Render production compose with real env values, not dummy values.
- Run `scripts/release-verify.sh` against the canary deployment with a real `INFERA_SMOKE_API_KEY`.
- If a live model should be checked, set `INFERA_SMOKE_MODEL` and optionally `INFERA_SMOKE_STREAM=1`.
- Confirm worker discovery returns targets from `/internal/prometheus/worker-targets`.
- Run one live RunPod or Vast.ai provisioning smoke if provider credentials are available.
- Watch gateway, Caddy, Prometheus, Grafana, and Alertmanager logs for at least 10-15 minutes after canary deploy.
