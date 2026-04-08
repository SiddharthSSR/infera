# AGENTS.md

## Scope

`go/` contains the gateway and core control plane: HTTP API handlers, routing, auth, deployments, provider integration, audit, vault/model registry, and the Hermes runtime.

## Key Paths

- `cmd/gateway/`: gateway entrypoint
- `internal/gateway/`: HTTP handlers, inference service, worker client, read views
- `internal/agents/`: Hermes runtime, registry, store, types
- `internal/auth/`, `internal/audit/`, `internal/deployments/`, `internal/providers/`, `internal/router/`, `internal/vault/`
- `pkg/types/`: shared request/response and routing types

## Commands

- Run tests: `go test ./...`
- Run focused gateway/agent tests: `go test ./internal/gateway ./internal/agents -count=1`
- Run the gateway locally: `INFERA_WORKER_SHARED_TOKEN=dev-token go run ./cmd/gateway`
- Lint: `golangci-lint run`

## Working Rules

- Keep handlers thin. Shared logic belongs in `internal/*`, not duplicated across handlers.
- Reuse `internal/gateway/read_views.go` and related helpers instead of making self-HTTP calls from the gateway.
- Preserve auth, quota, audit, and latency accounting when changing inference or Hermes execution paths.
- If you change OpenAI-compatible inference behavior, inspect both `internal/gateway/worker_client.go` and the Python worker serialization path.
- Hermes stores structured run data only. Do not add chain-of-thought persistence or display-oriented fields to backend contracts unless explicitly requested.

## Pitfalls

- Local runs require `INFERA_WORKER_SHARED_TOKEN`; the gateway exits without it.
- Running from `go/` writes local state under `go/data/`.
- Provider and deployment changes often affect tests in multiple packages; do not validate only one package if contracts move.

## Validation

- Run focused `go test` commands for touched packages at minimum.
- If you change shared request/response contracts, run `go test ./...` unless blocked.
- Update tests in the same change when modifying handlers, stores, or Hermes runtime behavior.
