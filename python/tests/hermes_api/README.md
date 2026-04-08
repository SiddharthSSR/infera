# Hermes API Test Suite

This package contains a production-oriented, API-first test harness for Hermes Agents.

## Folder Structure

```text
python/tests/hermes_api/
├── README.md
├── TEST_PLAN.md
├── assertions.py
├── client.py
├── config.py
├── conftest.py
├── fixture_loader.py
├── mock_api.py
├── models.py
├── reporting.py
├── scenario_runner.py
├── tool_catalog.py
├── data/
│   ├── condition_cases.json
│   ├── prompt_cases.json
│   ├── regression_cases.json
│   └── tool_cases.json
├── test_conditions.py
├── test_live_smoke.py
├── test_prompt_matrix.py
├── test_regressions.py
└── test_tool_matrix.py
```

## Design Choices

- `client.py` is the only place that knows how to talk to the API. Tests assert against typed models instead of hand-rolled dict indexing.
- `mock_api.py` implements the real transport interface used by `httpx.Client`, so the suite still exercises request building, uploads, polling, and error parsing at the HTTP boundary.
- `scenario_runner.py` centralizes payload construction, attachment uploads, and scenario execution so new cases only need fixture data, not new helper code.
- `data/*.json` stores reusable prompt, tool, condition, and regression fixtures so adding scenarios does not require editing test logic.
- `reporting.py` records per-scenario outcomes and prints a compact summary with expected vs actual tools after the pytest session.
- `test_live_smoke.py` is intentionally small. The broader matrix is deterministic and mock-backed; the live smoke tests are for deployment confidence, not exact tool-path reproducibility.
- API assumptions such as base paths, auth style, default model selection, and regression dataset overrides are isolated in `config.py`.

## Environment Variables

| Variable | Default | Purpose |
| --- | --- | --- |
| `HERMES_TEST_MODE` | `mock` | `mock` for deterministic transport, `live` for real API calls |
| `HERMES_API_BASE_URL` | `http://localhost:8080` | Base URL for Hermes API |
| `HERMES_API_TOKEN` | empty | Bearer token for live mode |
| `HERMES_API_MODEL` | `Qwen/Qwen2.5-7B-Instruct` | Model used when creating runs |
| `HERMES_API_TIMEOUT_SECONDS` | `10` | Per-request timeout |
| `HERMES_API_POLL_INTERVAL_SECONDS` | `0.05` | Run polling interval |
| `HERMES_API_MAX_WAIT_SECONDS` | `5` | Max poll duration for terminal runs |
| `HERMES_API_RATE_LIMIT_RETRIES` | `1` | Automatic retry count for HTTP 429 |
| `HERMES_API_REPORT_PATH` | unset | Optional JSON report output path |
| `HERMES_REGRESSION_DATASET` | unset | Optional override path for regression cases |

## Running the Tests

```bash
cd /Users/siddharthsingh/codingtensor/infera/python
pip install -e '.[dev]'
pytest tests/hermes_api -q
```

Live smoke:

```bash
cd /Users/siddharthsingh/codingtensor/infera/python
HERMES_TEST_MODE=live \
HERMES_API_BASE_URL=http://localhost:8080 \
HERMES_API_TOKEN=YOUR_KEY \
HERMES_API_MODEL=Qwen/Qwen2.5-7B-Instruct \
pytest tests/hermes_api/test_live_smoke.py -q
```
