# Hermes API Test Plan

## Goals

- Validate Hermes behavior strictly through its API endpoints.
- Cover every currently registered built-in tool.
- Exercise prompt categories, negative conditions, and regression datasets.
- Keep the suite deterministic in CI while still supporting live smoke coverage.

## Coverage Matrix

### Tool coverage

- `list_models`
- `list_workers`
- `get_gateway_stats`
- `list_instances`
- `list_deployments`
- `get_provider_status`
- `get_usage_summary`
- `get_quota_status`
- `web_search`
- `vision_analyze`

### Prompt coverage

- simple factual prompts
- tool-calling prompts
- multi-step reasoning prompts
- ambiguous prompts
- prompts with missing information
- prompts that should fall back or clarify
- adversarial or invalid prompts
- prompts where multiple tools are plausibly relevant

### Condition coverage

- missing required request parameters
- invalid attachment MIME types
- attachments in the wrong mode
- invalid tool arguments inside a run
- tool failure with graceful final response
- partial success after one tool fails
- cancellation and run-list ordering
- rate limit + retry
- malformed detail response
- transport timeout simulation
- external wait timeout

## Execution Model

- Default mode: deterministic `httpx.MockTransport` that serves the real Hermes HTTP contract.
- Live mode: optional smoke against a deployed Hermes environment.
- Reporting: every scenario records prompt, expected tools, actual tools, and outcome.
