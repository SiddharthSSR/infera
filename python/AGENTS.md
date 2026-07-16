# AGENTS.md

## Scope

`python/` contains the inference worker, engine adapters, benchmark tooling, and Python-side tests. It also contains the Hermes API contract suite in `tests/hermes_api/`.

## Key Paths

- `src/infera_worker/`: worker server, HTTP API, config, runtime logic
- `src/infera_worker/engines/`: engine implementations (`mock`, `vllm`, and others)
- `src/infera_bench/`: benchmark adapters and helpers
- `tests/`: worker and script tests
- `tests/hermes_api/`: API-first Hermes contract and smoke suite

## Commands

- Install editable dev environment: `pip install -e '.[dev]'`
- Run all Python tests: `pytest -q`
- Run Hermes API suite: `pytest tests/hermes_api -q`
- Lint: `ruff check .`
- Run worker with mock engine: `INFERA_ENGINE=mock python -m infera_worker.cli`
- Run worker with vLLM: `INFERA_ENGINE=vllm python -m infera_worker.cli`

## Working Rules

- Keep API/config parsing centralized instead of spreading env access across modules.
- Prefer typed models and shared helpers over raw dict manipulation in tests and client code.
- Keep engine-specific behavior inside engine modules unless the abstraction genuinely changes.
- If worker request/response shapes change, update the Go gateway contract path and Hermes live/mock tests in the same change.
- Treat benchmark tooling as separate from worker runtime behavior; avoid incidental edits there when working on inference APIs.

## Pitfalls

- Some engines are optional extras; do not assume GPU-only dependencies are available in every dev environment.
- The Hermes API suite has both deterministic mock tests and live smoke tests; use the right mode for the goal.
- `src/infera_worker.egg-info/` exists in-repo; avoid depending on generated metadata changes unless you are intentionally changing packaging.

## Validation

- Run the narrowest relevant `pytest` targets plus `ruff check` for touched areas.
- If worker contracts change, cover both unit tests and any impacted Hermes API tests.
- Keep live smoke tests small and deployment-oriented; broader behavior matrices belong in mock-backed tests.
