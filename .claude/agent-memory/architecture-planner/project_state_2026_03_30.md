---
name: project_state_2026_03_30
description: Comprehensive project state snapshot as of 2026-03-30 — branch topology, component inventory, test coverage, gap status
type: project
---

## Branch Topology
- `main` is at commit 57394a8 (merged roadmap PR #67 on ~2026-03-24)
- `roadmap` is 1 commit behind main (Instances.tsx deletion)
- `codex/modular-inference-backend` has 17 commits ahead of main: multi-engine backend + inference performance lab
- ~65 task branches exist, most un-merged, spanning workspace, billing, observability, provider, docs, and frontend domains

## Key Component Sizes (lines)
- gateway.go: 1834 LOC — the monolith risk; handles inference, workers, CORS, health, stats, audit, prometheus targets
- auth/store.go: 1707 LOC — workspaces, memberships, quotas, API keys, service accounts
- providers/manager.go: 602 — provisioning orchestration
- router/router.go: 498 — worker selection
- deployments/store.go: 580 — deployment tracking
- vault/store.go: 395 — model catalog

## Routing Strategies Implemented
- round_robin, latency_based, least_loaded, engine-aware
- No heterogeneous/cost-aware routing yet

## Engine Support
- vllm (production-exercised), sglang, tensorrt_llm, mock
- Modular engine registry with EngineDefinition/EngineCapabilities
- Engine-specific Dockerfiles and worker images
- Engine-specific runtime config in providers/runtime.go (652 LOC)

## Test Coverage
- Python: 19 test files (engine, worker, benchmarking, config, metrics, auth)
- Go: 33 test files across gateway, router, auth, vault, providers, deployments, audit
- Frontend: 6 test files (Dashboard, ApiKeys, Login, Logs, Models)
- Missing: no integration/e2e test harness, no CI pipeline for GPU tests

## Gateway API Surface
- /v1/chat/completions, /v1/models (OpenAI compat)
- /api/workers/*, /api/health, /api/stats, /api/audit/usage
- /api/instances/*, /api/deployments/*, /api/offerings, /api/providers, /api/costs
- /api/benchmarks/* (catalog, validate, compare)
- /metrics, /internal/prometheus/worker-targets

## Frontend Pages
- Dashboard, Instances, Models, ApiKeys, Login, Logs, Playground, GettingStarted, PublicApiDocs, WorkspaceAdmin, AcceptInvitation

## Auth Model
- Roles: owner, admin, operator, developer, read_only, billing, user(legacy)
- Permissions: dashboard, keys, memberships, workspaces, provider_configs, quotas, usage, infrastructure, vault
- Principal types: human, service_account

**Why:** This snapshot captures the exact state for priority planning.
**How to apply:** Reference when scoping next work items; verify specifics against code before acting.
