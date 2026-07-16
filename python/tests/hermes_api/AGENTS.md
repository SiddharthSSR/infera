# AGENTS.md

## Scope

`python/tests/hermes_api/` is the API-first Hermes test harness. It validates Hermes behavior through HTTP endpoints, with deterministic mock transport for broad coverage and a small live smoke layer for deployed gateways.

## Key Paths

- `client.py`: only place that knows endpoint paths and HTTP request details
- `scenario_runner.py`: shared execution/polling path for scenarios
- `mock_api.py`: deterministic HTTP-boundary mock transport
- `tool_catalog.py`: expected tool metadata
- `data/*.json`: reusable prompt, tool, condition, and regression fixtures
- `reporting.py`: per-scenario summaries and optional JSON report output
- `test_live_smoke.py`: minimal live deployment checks

## Working Rules

- Add new scenarios through `data/*.json` fixtures first. Only add new helper code when the fixture model cannot express the case.
- Keep endpoint knowledge in `client.py`; do not scatter raw URL construction across tests.
- Use the reporting fixture so failures show prompt/tool context in the terminal summary.
- Keep live tests focused on deployment confidence. Large prompt/tool matrices should stay mock-backed and deterministic.
- For live smoke, prefer selecting a loaded model dynamically instead of hardcoding one.

## Commands

- Deterministic suite: `pytest tests/hermes_api -q`
- Live smoke: `HERMES_TEST_MODE=live HERMES_API_BASE_URL=http://localhost:8080 HERMES_API_TOKEN=YOUR_KEY pytest tests/hermes_api/test_live_smoke.py -q`

## Environment

- `HERMES_TEST_MODE=mock` is the default.
- Optional env is centralized in `config.py`, including base URL, auth token, timeouts, report path, retries, and regression dataset override.

## Validation

- New Hermes capability work should add or update fixture-backed scenarios.
- If a live-only regression is fixed, add the smallest live smoke assertion that proves it without making the suite brittle.
