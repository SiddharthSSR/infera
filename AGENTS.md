# AGENTS.md

## Repo Scope

- `go/`: gateway API, routing, auth, providers, deployments, Hermes runtime
- `python/`: worker runtime, engine integrations, benchmark tooling, Python tests
- `frontend/`: React/Vite dashboard and playground
- `deploy/`: production Docker, ingress, and observability assets

Hermes changes are usually cross-stack: backend runtime in `go/`, worker/API contracts in `go/` and `python/`, playground UX in `frontend/`.

## Working Rules

- Inspect the target area first. Check for a nearer `AGENTS.md` before editing.
- Make the smallest change that fits existing abstractions.
- Reuse existing helpers, stores, API clients, and shared UI components.
- Keep configuration env-driven. Do not hardcode secrets, base URLs, model IDs, or deployment-only values unless the file is explicitly a test fixture.
- If you touch public API shapes, update all affected layers: backend types/handlers, frontend API/types, and any Python contract or smoke tests.
- Keep formatting-only changes separate unless they are already part of the requested scope.

## Shared Commands

- Install dependencies: `make deps`
- Full test pass: `make test`
- Full lint pass: `make lint`
- Run gateway locally: `INFERA_WORKER_SHARED_TOKEN=dev-token make run-gateway`
- Run frontend locally: `make run-frontend`
- Run a connected mock worker: `INFERA_WORKER_SHARED_TOKEN=dev-token make run-worker-connected`

## Validation

- Run the narrowest relevant tests for each touched stack. Run broader checks when you change shared contracts.
- Update or add tests with behavior changes; do not rely on manual verification alone.
- If a validation step cannot be run, say exactly what was not verified and why.
- Leave unrelated files untouched unless the user asked for broader cleanup.

## Nested AGENTS

- More specific `AGENTS.md` files override this file.
