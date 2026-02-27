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

## API Endpoints

- `POST /infer` - Run inference
- `POST /infer/stream` - Streaming inference
- `GET /health` - Health check
- `GET /models` - List loaded models
- `POST /models/load` - Load a model
- `POST /models/unload` - Unload a model
- `GET /stats` - Worker statistics