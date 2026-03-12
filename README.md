# Infera

> AI Inference Platform — Cost-efficient LLM serving at scale

Infera lets you provision GPU instances from cloud providers (RunPod, Vast.ai), automatically deploy inference workers with models like Llama and Mistral, and serve them through an OpenAI-compatible API.

## Features

- 🚀 **One-click GPU provisioning** from RunPod (Vast.ai coming soon)
- 🤖 **Automatic model loading** — Select a model, it loads on startup
- 🔌 **OpenAI-compatible API** — Drop-in replacement for your apps
- 📊 **Real-time dashboard** — Monitor instances, costs, and workers
- 💰 **Cost tracking** — See spend by provider, GPU type, hourly/daily/monthly
- ⚡ **vLLM-powered** — High-throughput inference with continuous batching

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                       Frontend (React)                          │
│              Dashboard • Instance Management • Chat              │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                       Gateway (Go)                              │
│        OpenAI API • Worker Registry • Instance Management       │
└─────────────────────────────────────────────────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        ▼                     ▼                     ▼
┌──────────────┐      ┌──────────────┐      ┌──────────────┐
│    Router    │      │   Instance   │      │     Cost     │
│              │      │   Manager    │      │   Tracker    │
│ Load Balance │      │  Provision   │      │   $/hour     │
│   Batching   │      │  Terminate   │      │   Billing    │
└──────────────┘      └──────────────┘      └──────────────┘
        │                     │
        │                     ▼
        │             ┌──────────────┐
        │             │  Providers   │
        │             │ RunPod/Vast  │
        │             └──────────────┘
        │                     │
        └─────────────────────┼─────────────────────┐
                              ▼                     ▼
                    ┌─────────────────────────────────────────────┐
                    │              Python Workers                  │
                    │         vLLM • Mock • Custom Engines         │
                    └─────────────────────────────────────────────┘
```

## Quick Start

### Prerequisites

- Go 1.22+
- Python 3.11+
- Node.js 20+
- Docker (optional)

### 1. Install Dependencies

```bash
make deps
```

### 2. Start the Gateway

```bash
# Mock provider only (for testing)
INFERA_WORKER_SHARED_TOKEN=your_worker_shared_token make run-gateway

# With RunPod
INFERA_WORKER_SHARED_TOKEN=your_worker_shared_token make run-gateway-runpod RUNPOD_API_KEY=your_key
```

### 3. Start the Frontend

```bash
# In a new terminal
make run-frontend
```

### 4. Open the Dashboard

Go to **http://localhost:5173**

---

## Production Deployment (DigitalOcean + Caddy)

This repository includes production deployment files:

- `docker-compose.prod.yml`
- `deploy/caddy/Caddyfile`

### 1. DNS and Firewall

Point your domain to the VM public IP:

- `A inferai.co.in -> <VM_IP>`

Open inbound ports:

- `22` (SSH from your IP only)
- `80` (public)
- `443` (public)

### 2. Prepare `.env`

Use `.env.example` as a base and set:

```bash
RUNPOD_API_KEY=...
INFERA_ADMIN_KEY=...
INFERA_GATEWAY_ADDRESS=https://inferai.co.in
INFERA_ALLOWED_ORIGINS=https://inferai.co.in
INFERA_WORKER_SHARED_TOKEN=<long-random-token>
INFERA_WORKER_IMAGE=<registry>/infera-worker:<pinned-tag>
GRAFANA_ADMIN_USER=admin
GRAFANA_ADMIN_PASSWORD=<strong-password>
ALERT_EMAIL_TO=alerts@your-domain.com
ALERT_SMTP_FROM=alerts@your-domain.com
ALERT_SMTP_SMARTHOST=smtp.gmail.com:587
ALERT_SMTP_USERNAME=alerts@your-domain.com
ALERT_SMTP_PASSWORD=<gmail-app-password>
HF_TOKEN=... # optional
```

Notes:

- Keep `INFERA_WORKER_SHARED_TOKEN` identical on gateway and workers.
- Avoid using `latest` for worker image in production.
- Alertmanager values in `docker-compose.prod.yml` are required in production; do not leave them as placeholders.

### 3. Deploy

```bash
git checkout v1-production
git pull origin v1-production
docker compose -f docker-compose.prod.yml up -d --build
docker compose -f docker-compose.prod.yml ps
```

### 4. Verify

```bash
curl -I https://inferai.co.in
curl -I https://inferai.co.in/health
curl -I https://dashboard.inferai.co.in
curl -i https://inferai.co.in/api/stats
```

Expected:

- `/` and `/health` return `200`
- `dashboard.inferai.co.in` returns `200` and serves Grafana login
- `/api/stats` returns `401` without API key

### 5. Monitoring Bootstrap

Production compose now includes Prometheus, Alertmanager, and Grafana:

- Prometheus config: `deploy/observability/prometheus/prometheus.yml`
- Alert rules: `deploy/observability/prometheus/rules/infera-alerts.yml`
- Alertmanager routing: `deploy/observability/alertmanager/alertmanager.yml`
- Alertmanager templates: `deploy/observability/alertmanager/templates/`
- Grafana provisioning: `deploy/observability/grafana/provisioning/`
- Starter dashboard: `deploy/observability/grafana/dashboards/infera-overview.json`
- Runbooks: `deploy/observability/RUNBOOKS.md`

For worker scraping, Prometheus now discovers targets dynamically from:

- `http://gateway:8080/internal/prometheus/worker-targets`

Healthy workers that register with the gateway should appear automatically.

### 6. Worker Registration Checklist

If `/health` shows `workers: 0`:

1. Confirm worker can reach gateway:
   `curl -i https://inferai.co.in/health`
2. Confirm worker has runtime env:
   `INFERA_ROUTER_ADDRESS`, `INFERA_WORKER_SHARED_TOKEN`
3. Confirm gateway and worker token hashes match.
4. Recreate gateway and reprovision workers (env is applied at worker creation time).

---

## Running with RunPod (Real GPUs)

### Step 1: Get your RunPod API Key

1. Go to https://runpod.io and sign up
2. Navigate to **Settings → API Keys**
3. Create a new API key

### Step 2: Set up ngrok (for worker registration)

Workers need to connect back to your gateway. Use ngrok to expose it:

```bash
# Terminal 1: Start ngrok
make ngrok
# Note the URL: https://abc123.ngrok-free.app
```

### Step 3: Start the Gateway with RunPod

```bash
# Terminal 2: Start gateway
make run-gateway-runpod \
  INFERA_WORKER_SHARED_TOKEN=your_worker_shared_token \
  RUNPOD_API_KEY=your_key \
  INFERA_GATEWAY_ADDRESS=abc123.ngrok-free.app
```

### Step 4: Start the Frontend

```bash
# Terminal 3: Start frontend
make run-frontend
```

### Step 5: Provision an Instance

1. Open http://localhost:5173
2. Click **"+ New Instance"**
3. Select **RunPod** as provider
4. Select **RTX 4090** (~$0.44/hr)
5. Select **Mistral 7B Instruct** as the model
6. Click **Provision Instance**

The instance will:
- Start on RunPod (~1-2 min)
- Download the model (~5-10 min)
- Load to GPU and register with gateway
- Be ready to serve requests!

---

## Makefile Commands

### Development

| Command | Description |
|---------|-------------|
| `make deps` | Install all dependencies |
| `INFERA_WORKER_SHARED_TOKEN=xxx make run-gateway` | Start gateway (mock only) |
| `INFERA_WORKER_SHARED_TOKEN=xxx make run-gateway-runpod RUNPOD_API_KEY=xxx` | Start gateway with RunPod |
| `make run-frontend` | Start frontend dev server |
| `make run-worker` | Start local mock worker |
| `make run-worker-connected` | Start worker connected to gateway |
| `make dev` | Show development instructions |

### Build & Test

| Command | Description |
|---------|-------------|
| `make build` | Build gateway and frontend |
| `make test` | Run all tests |
| `make go-test` | Run Go tests |
| `make python-test` | Run Python tests |
| `make frontend-test-run` | Run frontend tests |
| `make lint` | Run all linters |

### Docker

| Command | Description |
|---------|-------------|
| `make docker-build-worker` | Build worker Docker image |
| `make docker-push DOCKER_USERNAME=xxx` | Push to Docker Hub |
| `make docker-up` | Start all services with Docker Compose |
| `make docker-down` | Stop Docker Compose services |

### Utilities

| Command | Description |
|---------|-------------|
| `make ngrok` | Start ngrok tunnel |
| `make status` | Check if services are running |
| `make check-providers` | List available providers |
| `make check-offerings` | List GPU offerings |
| `make clean` | Clean build artifacts |
| `make help` | Show all commands |

---

## API Endpoints

### OpenAI-Compatible

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/chat/completions` | POST | Chat completion (streaming supported) |
| `/v1/models` | GET | List available models |

### Instance Management

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/instances` | GET | List GPU instances |
| `/api/instances/provision` | POST | Create new instance |
| `/api/instances/{id}` | GET | Get instance details |
| `/api/instances/{id}` | DELETE | Terminate instance |
| `/api/instances/{id}/start` | POST | Start stopped instance |
| `/api/instances/{id}/stop` | POST | Stop running instance |
| `/api/offerings` | GET | List GPU offerings |
| `/api/providers` | GET | List provider status |
| `/api/costs` | GET | Get cost summary |

### Worker Management

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/workers` | GET | List workers |
| `/api/workers/register` | POST | Register worker |
| `/api/workers/heartbeat` | POST | Worker heartbeat |
| `/api/workers/{id}` | DELETE | Deregister worker |
| `/api/stats` | GET | Cluster stats |
| `/health` | GET | Health check |

---

## Environment Variables

### Gateway

| Variable | Default | Description |
|----------|---------|-------------|
| `RUNPOD_API_KEY` | — | RunPod API key |
| `VASTAI_API_KEY` | — | Vast.ai API key |
| `INFERA_ADMIN_KEY` | auto-generated | Admin API key bootstrap (recommended to set explicitly in production) |
| `INFERA_GATEWAY_ADDRESS` | — | Public gateway URL workers use for registration/heartbeat |
| `INFERA_ALLOWED_ORIGINS` | `*` | CORS allowlist (comma-separated) |
| `INFERA_WORKER_SHARED_TOKEN` | — | Shared secret required for worker register/heartbeat |
| `INFERA_WORKER_IMAGE` | — | Custom worker Docker image |
| `INFERA_DEFAULT_MODEL` | `mistralai/Mistral-7B-Instruct-v0.2` | Default model to load |
| `INFERA_GITHUB_REPO` | — | GitHub repo to install worker from |
| `HF_TOKEN` | — | HuggingFace token (for gated models) |

### Worker

| Variable | Default | Description |
|----------|---------|-------------|
| `INFERA_ENGINE` | `vllm` | Engine type: `mock`, `vllm` |
| `INFERA_HTTP_PORT` | `8081` | Worker HTTP port |
| `INFERA_ROUTER_ADDRESS` | — | Gateway address for registration |
| `INFERA_PRELOAD_MODELS` | — | Models to load on startup |
| `INFERA_LOG_LEVEL` | `INFO` | Log level |
| `HF_TOKEN` | — | HuggingFace token |

---

## Project Structure

```
infera/
├── go/                          # Go gateway
│   ├── cmd/
│   │   ├── gateway/             # Gateway entrypoint
│   │   ├── router/              # Router entrypoint
│   │   └── vault/               # Vault entrypoint
│   ├── internal/
│   │   ├── gateway/             # HTTP API handlers & worker client
│   │   ├── router/              # Request routing & load balancing
│   │   │   ├── batcher/         # Request batching
│   │   │   ├── registry/        # Worker registry
│   │   │   └── strategy/        # Routing strategies (least loaded, round robin, latency)
│   │   └── providers/           # GPU provider integrations
│   │       ├── runpod/          # RunPod API client
│   │       ├── vastai/          # Vast.ai API client (stub)
│   │       └── mock/            # Mock provider for testing
│   └── pkg/types/               # Shared types (routing, worker, types)
│
├── python/                      # Python inference worker
│   ├── src/infera_worker/
│   │   ├── cli.py               # CLI entrypoint
│   │   ├── worker.py            # Core worker logic
│   │   ├── http_server.py       # HTTP API + gateway registration
│   │   ├── engine.py            # Engine abstraction
│   │   ├── engines/
│   │   │   └── vllm_engine.py   # vLLM integration
│   │   ├── config.py            # Configuration
│   │   └── types.py             # Type definitions
│   ├── tests/                   # Python tests
│   ├── Dockerfile               # Full vLLM worker image
│   └── Dockerfile.light         # Lightweight mock worker
│
├── frontend/                    # React dashboard
│   └── src/
│       ├── components/          # UI components (Chat, Costs, Workers, etc.)
│       ├── hooks/               # React Query hooks
│       ├── lib/                 # API client & utilities
│       ├── pages/               # Page components (Dashboard, Instances, Playground, etc.)
│       └── types/               # TypeScript types
│
├── proto/                       # Protobuf definitions
│   ├── common.proto
│   ├── gateway.proto
│   ├── inference.proto
│   ├── router.proto
│   ├── vault.proto
│   └── worker.proto
│
├── deploy/
│   ├── caddy/
│   │   └── Caddyfile               # Production reverse proxy config
│   └── docker/
│       ├── Dockerfile.frontend
│       ├── Dockerfile.gateway
│       ├── Dockerfile.worker.vllm
│       └── nginx.conf
│
├── scripts/                     # Utility scripts
│   └── build-docker.sh
│
├── docker-compose.yml           # Local development
├── docker-compose.prod.yml      # Production deployment (gateway + frontend + caddy)
└── Makefile                     # Build & run commands
```

---

## Supported Models

| Model | Size | Min GPU | HF Token Required |
|-------|------|---------|-------------------|
| `mistralai/Mistral-7B-Instruct-v0.2` | ~14GB | RTX 4090 | No |
| `microsoft/phi-2` | ~6GB | RTX 4080 | No |
| `meta-llama/Llama-3-8B-Instruct` | ~18GB | RTX 4090 | Yes |
| `google/gemma-7b-it` | ~16GB | RTX 4090 | Yes |

For gated models (Llama, Gemma), you need to:
1. Accept the license on HuggingFace
2. Get a token from https://huggingface.co/settings/tokens
3. Set `HF_TOKEN` environment variable

---

## Supported GPU Providers

| Provider | Status | GPU Types |
|----------|--------|-----------|
| **Mock** | ✅ Ready | Testing without real GPUs |
| **RunPod** | ✅ Ready | RTX 4090, RTX 4080, A100, H100, L40S |
| **Vast.ai** | 🚧 Stub | Interface ready, implementation pending |
| **Lambda** | ⏳ Planned | — |

---

## GPU Pricing (RunPod estimates)

| GPU | VRAM | On-Demand | Spot |
|-----|------|-----------|------|
| RTX 4090 | 24GB | ~$0.44/hr | ~$0.22/hr |
| RTX 4080 | 16GB | ~$0.34/hr | ~$0.17/hr |
| A100 40GB | 40GB | ~$0.79/hr | ~$0.39/hr |
| A100 80GB | 80GB | ~$1.19/hr | ~$0.59/hr |
| H100 | 80GB | ~$2.49/hr | ~$1.24/hr |
| L40S | 48GB | ~$0.99/hr | ~$0.49/hr |

---

## Docker Deployment

### Option 1: Docker Compose (Local Development)

```bash
make docker-up
```

This starts:
- Gateway on port 8080
- Frontend on port 3000

### Option 2: Docker Compose (Production)

```bash
docker compose -f docker-compose.prod.yml up -d --build
```

This starts:
- Gateway (internal)
- Frontend (internal)
- Caddy on `80/443` (public)

### Option 3: Build & Push Worker Image

```bash
# Build the worker image
make docker-build-worker

# Push to Docker Hub
make docker-push DOCKER_USERNAME=your_username

# Use with RunPod
make run-gateway-runpod \
  INFERA_WORKER_SHARED_TOKEN=xxx \
  RUNPOD_API_KEY=xxx \
  INFERA_WORKER_IMAGE=your_username/infera-worker:latest
```

### Option 4: Install from GitHub (No Docker Push Required)

Push your code to GitHub and set `INFERA_GITHUB_REPO`:

```bash
make run-gateway-runpod \
  INFERA_WORKER_SHARED_TOKEN=xxx \
  RUNPOD_API_KEY=xxx \
  INFERA_GITHUB_REPO=https://github.com/your_username/infera.git
```

RunPod will use a base image and install the worker from GitHub at runtime.

---

## Troubleshooting

### Gateway not starting

```bash
# Check if port is in use
lsof -i :8080

# Use different port
INFERA_WORKER_SHARED_TOKEN=xxx make run-gateway GATEWAY_PORT=8081
```

### Frontend can't connect to gateway

Make sure gateway is running on port 8080. The frontend proxies API requests to `http://localhost:8080`.

### RunPod instance not connecting to gateway

1. Ensure ngrok is running: `make ngrok`
2. Set `INFERA_GATEWAY_ADDRESS` to your ngrok URL (without `https://`)
3. Check RunPod pod logs in their dashboard for errors

### Model download fails

For gated models (Llama, Gemma):
1. Accept the model license on HuggingFace
2. Create a token at https://huggingface.co/settings/tokens
3. Set `HF_TOKEN` when starting the gateway:
   ```bash
make run-gateway-runpod INFERA_WORKER_SHARED_TOKEN=xxx RUNPOD_API_KEY=xxx HF_TOKEN=hf_xxx
   ```

### Worker not registering

Check that:
1. `INFERA_GATEWAY_ADDRESS` points to your public domain (`https://...`)
2. Worker has non-empty `INFERA_ROUTER_ADDRESS`
3. Gateway and worker share the same `INFERA_WORKER_SHARED_TOKEN`
4. Worker can reach `/api/workers/register` and `/health`
5. You reprovisioned workers after env changes

---

## Contributing

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/my-feature`
3. Make your changes
4. Run tests: `make test`
5. Commit: `git commit -am 'Add my feature'`
6. Push: `git push origin feature/my-feature`
7. Submit a pull request

---

## License

MIT
