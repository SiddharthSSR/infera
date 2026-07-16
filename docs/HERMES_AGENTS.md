# Hermes Agents — Integration Guide

Infera ships three levels of Hermes agent capability: tool calling pass-through, a server-side agentic loop, and managed agent hosting with webhooks.

## Level 1: Tool Calling Pass-Through

OpenAI-compatible `tools`, `tool_choice`, and `tool_calls` fields are forwarded from the gateway to the worker engine.

### Gateway Request

```json
POST /v1/chat/completions
{
  "model": "hermes-3-llama-3.1-8b",
  "messages": [{"role": "user", "content": "What's the weather in SF?"}],
  "tools": [{
    "type": "function",
    "function": {
      "name": "get_weather",
      "parameters": {"type": "object", "properties": {"city": {"type": "string"}}}
    }
  }],
  "tool_choice": "auto"
}
```

### Worker Configuration

Set the `INFERA_TOOL_CALL_PARSER` environment variable to enable native tool call parsing. Valid values:

| Parser | Models |
|--------|--------|
| `hermes` | Hermes-3, NousResearch models |
| `mistral` | Mistral Instruct |
| `llama3_json` | Llama 3.x |
| `qwen25` | Qwen 2.5 |
| `jamba` | AI21 Jamba |
| `pythonic` | Models using Python-style calls |
| `internlm` | InternLM models |

vLLM additionally sets `enable_auto_tool_choice=True` when a parser is configured. SGLang and TensorRT-LLM pass the parser as `tool_call_parser`.

### Response with Tool Calls

When the model decides to call a tool, `finish_reason` is `"tool_calls"` and the assistant message contains:

```json
{
  "choices": [{
    "message": {
      "role": "assistant",
      "content": null,
      "tool_calls": [{
        "id": "call_abc123",
        "type": "function",
        "function": {"name": "get_weather", "arguments": "{\"city\":\"SF\"}"}
      }]
    },
    "finish_reason": "tool_calls"
  }]
}
```

## Level 2: Server-Side Agentic Loop

The built-in agent runtime in `go/internal/agents/` executes multi-step tool-calling loops autonomously.

### Creating a Run

```json
POST /api/agents/runs
{
  "definition_id": "hermes",
  "prompt": "Find recent papers about KV cache disaggregation",
  "mode": "research",
  "workspace_id": "ws_123"
}
```

**Modes**: `operations` (10 built-in tools), `research` (web search via DuckDuckGo), `multimodal` (vision/OCR with Tesseract).

**Built-in tools**: `read_file`, `write_file`, `list_directory`, `search_code`, `run_command`, `http_request`, `web_search`, `vision_analyze`, `create_artifact`, `update_artifact`.

### SSE Streaming

Stream run progress in real time instead of polling:

```
GET /api/agents/runs/{run_id}/stream
```

Events:
- `event: status` — initial hydration with full run state
- `event: step` — incremental step updates (tool calls, model outputs)
- `event: done` — terminal event with final run state

Example:
```
event: status
data: {"id":"run_abc","status":"running","steps":[...]}

event: step
data: {"step_index":3,"type":"tool_call","tool_name":"web_search","input":{...},"output":"..."}

event: done
data: {"id":"run_abc","status":"completed","result":"..."}
```

The SSE endpoint polls the store every 500ms for new steps.

### Hermes Agent Defaults

- **Max steps**: 8 (12 in deep mode)
- **Timeout**: 45s (90s in deep mode)
- **Max tokens**: 512 (1024 in deep mode)

## Level 3: Managed Agent Hosting

### Custom Definitions

Create workspace-scoped agent definitions with custom system prompts and tool selections:

```json
POST /api/agents/definitions
{
  "name": "Code Reviewer",
  "description": "Reviews code for quality issues",
  "system_prompt": "You are a code review assistant...",
  "tools": ["read_file", "search_code", "list_directory"],
  "workspace_id": "ws_123"
}
```

Tool names are validated against the built-in tool registry.

```
GET /api/agents/definitions?workspace_id=ws_123
DELETE /api/agents/definitions/{id}
```

### External API

API-key authenticated endpoint for running agents programmatically:

```json
POST /v1/agents/runs
Authorization: Bearer <api-key>
{
  "definition_id": "hermes",
  "prompt": "Analyze the auth module",
  "mode": "operations",
  "wait": true
}
```

When `wait=true`, the request blocks (up to 120s) until the run completes and returns the full result. When `wait=false` (default), returns immediately with the run ID for polling or SSE streaming.

### Webhooks

Register HMAC-SHA256 signed callbacks for run completion:

```json
POST /api/agents/webhooks
{
  "url": "https://example.com/hooks/infera",
  "secret": "whsec_abc123",
  "events": ["agent.run.completed"],
  "workspace_id": "ws_123"
}
```

Webhook deliveries include:
- **Header**: `X-Infera-Signature: sha256=<hex>` (HMAC-SHA256 of the JSON body using the webhook secret)
- **Header**: `X-Infera-Event: agent.run.completed`
- **Timeout**: 10s per delivery
- **Body**: Full run object with steps and result

```
GET /api/agents/webhooks?workspace_id=ws_123
DELETE /api/agents/webhooks/{id}
```

The webhook secret is never returned in API responses (`json:"-"`).

## Seeded Models

Two Hermes models are pre-seeded in the vault:

| Model | VRAM | Context | Tags |
|-------|------|---------|------|
| Hermes-3-Llama-3.1-8B | 18 GiB | 128K | chat, instruct, function-calling, agentic |
| Hermes-3-Llama-3.1-70B | 140 GiB | 128K | chat, instruct, function-calling, agentic |

## Key Files

| File | Purpose |
|------|---------|
| `go/internal/agents/runtime.go` | Agent orchestrator, `executeRun()` loop |
| `go/internal/agents/store.go` | SQLite store, migrations 1-4 |
| `go/internal/agents/types.go` | Run, Step, CustomDefinition, WebhookConfig |
| `go/internal/agents/hermes.go` | Hermes definition, tools, system prompts |
| `go/internal/gateway/agents_handlers.go` | All agent HTTP handlers |
| `go/internal/gateway/agent_capabilities.go` | DuckDuckGo search, Tesseract OCR |
| `go/internal/providers/runtime.go` | `INFERA_TOOL_CALL_PARSER` env var |
| `go/pkg/types/types.go` | Shared types (ToolCall, ToolDefinition) |
| `python/src/infera_worker/types.py` | Python mirror of shared types |
| `python/src/infera_worker/config.py` | `tool_call_parser` worker config |
