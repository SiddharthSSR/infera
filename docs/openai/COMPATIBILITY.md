# OpenAI Compatibility Guide

Infera exposes an OpenAI-compatible surface for the most common chat workflows.

## Supported Endpoints

### `GET /v1/models`

Returns an OpenAI-style model list payload:

- top-level `object: "list"`
- `data[]` items with:
  - `id`
  - `object`
  - `created`
  - `owned_by`

Infera may also include extra metadata fields such as:

- `loaded`
- `family`
- `parameters`
- `quantization`
- `vram_required`
- `max_context`
- `tags`
- `vault_status`

Clients that ignore unknown fields should work without changes.

### `POST /v1/chat/completions`

Supported request fields:

- `model`
- `messages`
- `temperature`
- `top_p`
- `max_tokens`
- `stop`
  - accepts either a single string or an array of strings
- `stream`
- `seed`
- `presence_penalty`
- `frequency_penalty`

Supported response fields:

- `id`
- `object`
- `created`
- `model`
- `choices`
- `usage`

Streaming responses use OpenAI-style Server-Sent Events:

- `Content-Type: text/event-stream`
- `data: {json chunk}`
- terminal `data: [DONE]`

## Compatibility Notes

- Unknown request fields are ignored unless they break JSON decoding.
- Infera currently supports `chat.completions`, not the legacy `completions` endpoint.
- Infera does not yet expose `embeddings`.
- `/v1/models` may include extra fields beyond the OpenAI baseline for the dashboard and operator workflows.

## Known Differences

- Error `type` values are Infera-specific strings such as:
  - `invalid_request`
  - `no_workers`
  - `worker_unavailable`
  - `inference_error`
  - `inference_timeout`
- Infera’s model catalog merges worker-loaded models and vault metadata.

## Validation Coverage

The gateway test suite currently verifies:

- non-streaming chat completion response shape
- SSE chunk shape and `[DONE]` termination
- model list response shape
- request parameter passthrough for supported OpenAI-style fields

## Official OpenAI SDK Example

Python:

```python
from openai import OpenAI

client = OpenAI(
    api_key="YOUR_INFERA_KEY",
    base_url="https://inferai.co.in/v1",
)

resp = client.chat.completions.create(
    model="meta-llama/Meta-Llama-3.1-8B-Instruct",
    messages=[
        {"role": "user", "content": "Say hello in one line."},
    ],
)

print(resp.choices[0].message.content)
```

Streaming:

```python
from openai import OpenAI

client = OpenAI(
    api_key="YOUR_INFERA_KEY",
    base_url="https://inferai.co.in/v1",
)

stream = client.chat.completions.create(
    model="meta-llama/Meta-Llama-3.1-8B-Instruct",
    messages=[
        {"role": "user", "content": "Explain SSE in one sentence."},
    ],
    stream=True,
)

for chunk in stream:
    delta = chunk.choices[0].delta.content or ""
    if delta:
        print(delta, end="")
```
