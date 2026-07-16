# Infera Worker

Python-based inference worker for the Infera platform. Supports vLLM for high-throughput GPU inference.

## Installation

```bash
pip install -e .
```

## Usage

```bash
# With mock engine (no GPU)
INFERA_ENGINE=mock python -m infera_worker.cli

# With vLLM (requires GPU)
INFERA_ENGINE=vllm python -m infera_worker.cli
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `INFERA_ENGINE` | `vllm` | Engine: `mock`, `vllm` |
| `INFERA_HTTP_PORT` | `8081` | HTTP server port |
| `INFERA_ROUTER_ADDRESS` | - | Gateway address for registration |
| `INFERA_PRELOAD_MODELS` | - | Models to load on startup |
| `INFERA_ALLOWED_MODELS` | `INFERA_PRELOAD_MODELS` | Model IDs accepted by `POST /models/load` |
| `INFERA_MODEL_CACHE_SIZE` | `2` | Maximum number of loaded models |
| `INFERA_TRUST_REMOTE_CODE` | `false` | Opt in to reviewed Hugging Face custom code |
| `INFERA_WORKER_SHARED_TOKEN` | - | Required worker API token (sent as `X-Worker-Token`) |

## API Endpoints

- `POST /infer` - Run inference
- `POST /infer/stream` - Streaming inference
- `GET /health` - Health check
- `GET /models` - List loaded models
- `POST /models/load` - Load a model
- `POST /models/unload` - Unload a model
- `GET /stats` - Worker statistics

`GET /health` is the only unauthenticated endpoint. All other endpoints require an
`X-Worker-Token` header matching `INFERA_WORKER_SHARED_TOKEN`; they fail closed when the
token is unset. `POST /models/load` accepts only `model_id`, and that ID must be present in
`INFERA_ALLOWED_MODELS` (or the preload list when no separate allowlist is configured).
