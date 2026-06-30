# Roadmap to Main Release Checklist

Use this checklist only when intentionally promoting the full `roadmap` branch to `main`.
For the stabilization release candidate, use [STABILIZATION_RELEASE_REPORT.md](/Users/siddharthsingh/codingtensor/infera/docs/releases/STABILIZATION_RELEASE_REPORT.md) instead.

## Release Status

As of `2026-03-24`, `roadmap` was considered a reasonable release candidate pending one production canary pass and confirmation of production environment values.

As of the refreshed `2026-06-30` stabilization audit, `origin/roadmap` has grown into a broad mixed branch with Hermes agents/runtime, benchmark lab, worker engine images, frontend rewrites, generated/test artifacts, and newer login-page polish. Do not promote it directly as the stabilization release without a fresh branch-wide audit, green validation, and canary pass.

Cleanup already completed on `roadmap`:

- removed tracked SQLite WAL/SHM artifacts:
  - `go/data/vault.db-shm`
  - `go/data/vault.db-wal`
- removed tracked compiled Go binary:
  - `go/gateway`
- tightened ignore rules in [.gitignore](/Users/siddharthsingh/codingtensor/infera/.gitignore) for local Go build artifacts

## Validated Checks

These checks were run successfully on `roadmap`:

- `python/venv/bin/python -m pytest python/tests/test_capture_startup_health.py python/tests/test_cold_start_benchmark.py python/tests/test_benchmark_chat.py python/tests/test_worker.py python/tests/test_vllm_runtime_config.py python/tests/test_http_server_metrics.py -q`
  - result: `68 passed`
- `go test ./internal/auth ./internal/deployments ./internal/gateway ./internal/providers/... ./internal/router/... ./internal/vault ./pkg/types/...`
  - result: all packages `ok`
- `npm run test:run` in [frontend](/Users/siddharthsingh/codingtensor/infera/frontend)
  - result: `22` test files passed, `113` tests passed
- `npm run build` in [frontend](/Users/siddharthsingh/codingtensor/infera/frontend)
  - result: production build succeeded
- `docker compose -f docker-compose.prod.yml config`
  - initial run failed only because required production env vars were not set
- `docker compose -f docker-compose.prod.yml config` with dummy required env vars
  - result: config rendered successfully
- `./scripts/compose-smoke-prod.sh`
  - result: local production-compose smoke passed after release-script fixes for env export, local ingress routing, and isolated smoke data mount
- `git diff --check`
  - result: clean

## Remaining Merge Gates

These should be confirmed before merging `roadmap` into `main`:

1. Pin the production worker image.
   - Confirm [deploy/docker/Dockerfile.worker.vllm](/Users/siddharthsingh/codingtensor/infera/deploy/docker/Dockerfile.worker.vllm) has produced the intended image.
   - Set `INFERA_WORKER_IMAGE` in production to a pinned tag or digest, not `latest`.
   - Validate it before deploy:

     ```bash
     ./scripts/validate-worker-image-pin.sh
     ```

2. Confirm required production env values.
   Required by [docker-compose.prod.yml](/Users/siddharthsingh/codingtensor/infera/docker-compose.prod.yml):
   - `INFERA_ADMIN_KEY`
   - `INFERA_ALLOWED_ORIGINS`
   - `INFERA_GATEWAY_ADDRESS`
   - `INFERA_WORKER_SHARED_TOKEN`
   - `INFERA_WORKER_IMAGE`
   - `GRAFANA_ADMIN_USER`
   - `GRAFANA_ADMIN_PASSWORD`
   - `ALERT_EMAIL_TO`
   - `ALERT_SMTP_FROM`
   - `ALERT_SMTP_SMARTHOST`
   - `ALERT_SMTP_USERNAME`
   - `ALERT_SMTP_PASSWORD`

3. Decide the intended production values for optional runtime tuning env vars.
   These currently default blank if unset:
   - `INFERA_ENABLE_BATCHING`
   - `INFERA_MAX_BATCH_SIZE`
   - `INFERA_MAX_BATCH_WAIT_MS`
   - `INFERA_AFFINITY_TTL_SECONDS`
   - `INFERA_RATE_LIMIT_REQUESTS_PER_MINUTE`
   - `INFERA_RATE_LIMIT_BURST_SIZE`
   - `INFERA_MAX_IN_FLIGHT_REQUESTS`

4. Review one frontend follow-up.
   - Current build succeeds, but Vite warns that the main JS chunk is larger than `500 kB`.
   - Treat this as non-blocking for this release unless deploy bandwidth or page-load regression is observed.

5. Run a canary deployment and smoke verification.

## Canary Checklist

Run this on the production-like target before merging to `main`:

1. Record the exact release inputs.
   - `git rev-parse HEAD`
   - worker image digest
   - gateway/frontend compose revision

2. Render compose with real production env.
   - `docker compose -f docker-compose.prod.yml config >/tmp/infera-prod-config.yaml`

3. Deploy the canary stack.
   - `docker compose -f docker-compose.prod.yml up -d --build`
   - `docker compose -f docker-compose.prod.yml ps`

4. Verify service health.
   - gateway `/health`
   - dashboard `/api/health`
   - Grafana login
   - Alertmanager container healthy

5. Run the bundled post-deploy checks.
   - [scripts/release-verify.sh](/Users/siddharthsingh/codingtensor/infera/scripts/release-verify.sh) against the canary URL
   - [scripts/smoke-test.sh](/Users/siddharthsingh/codingtensor/infera/scripts/smoke-test.sh) with a valid smoke API key

6. Verify worker discovery and one live inference path.
   - `GET /internal/prometheus/worker-targets`
   - `GET /v1/models`
   - one `POST /v1/chat/completions` request

7. Run one short infrastructure sanity check on RunPod.
   - provision one `A100_80GB` worker
   - confirm `/health` reaches `ready=true`
   - confirm one completion succeeds

8. Watch for regressions for at least 10-15 minutes.
   - gateway logs
   - caddy logs
   - Prometheus target health
   - Grafana overview dashboard

Canary success criteria:

- public site and dashboard healthy
- authenticated gateway paths succeed
- worker discovery returns targets
- one live inference succeeds end-to-end
- no sustained restart loop or alert storm

## Merge Checklist

Use this immediately before opening the final `roadmap -> main` PR or merge:

- [ ] `roadmap` contains the latest merged performance and runtime work
- [ ] cleanup commits are already present on `roadmap`
- [ ] Python validation pass is green
- [ ] Go validation pass is green
- [ ] frontend tests and production build are green
- [ ] production compose renders with real env
- [ ] worker image tag/digest is pinned
- [ ] canary deployment passed
- [ ] release notes below are adjusted for the final version number
- [ ] tag plan is prepared

## Release Note Draft

Title:

- `vX.Y.Z`

Summary:

- ships the current roadmap branch to production with upgraded gateway, provider runtime, worker startup diagnostics, observability, and dashboard UX improvements
- adds benchmark tooling and measured RunPod `A100_80GB` baselines for warm and cold inference paths
- improves worker readiness and startup visibility, making the remaining performance bottleneck explicit: first-boot model artifact population and vLLM initialization

Highlights:

- reliability and deployment hardening across gateway, providers, and worker startup
- improved dashboard and workspace UX, including richer instance and model runtime views
- production observability additions with Prometheus, Grafana, and Alertmanager wiring
- OpenAI-compatible API and routing improvements with stronger contract and regression coverage
- benchmark workflows for warm throughput, cold starts, and startup-health capture

Included work:

### Reliability and Deployment

- provider/runtime coverage broadened for active GPU paths
- worker image/runtime compatibility hardened for vLLM startup args
- production compose and observability deployment paths tightened

### Observability

- dashboard, Prometheus rules, and runbooks expanded
- worker startup stages exposed in `/health`
- startup cache diagnostics added for model-load analysis

### API and Gateway

- gateway, worker discovery, and instance lifecycle handling improved
- router and batching behavior refined
- additional test coverage across gateway/provider/router contracts

### Frontend and UX

- dashboard, instances, models, API keys, login, and docs flows expanded
- shared status and metadata presentation components added
- better mobile and desktop coverage through focused tests

### Performance and Benchmarking

- `RunPod A100_80GB` warm baseline captured
- cold-start benchmark helper added and validated
- restart/reuse cold-start path reduced and documented
- affinity routing benchmarked for the current workload and documented as not outperforming default routing in the measured sample

Verification:

- Python targeted release suite passed
- Go targeted release suite passed
- frontend tests passed
- frontend production build passed
- compose config rendered successfully with required env provided

Known follow-ups:

- seed hot-model Hugging Face artifacts for first boot to reduce fresh-provision cold start
- revisit frontend chunk splitting if production asset size becomes a user-facing problem

## Merge Command Sequence

```bash
git checkout main
git pull --ff-only origin main
git merge --ff-only roadmap
git push origin main
git tag -a vX.Y.Z -m "vX.Y.Z release"
git push origin vX.Y.Z
```
