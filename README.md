# Infera

> AI Inference Platform — Cost-efficient LLM serving at scale

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

### Development (All Services)
```bash
# Run everything locally
./scripts/dev.sh

# Or manually:
# Terminal 1: Gateway
cd go && go run ./cmd/gateway -port 8080

# Terminal 2: Worker
cd python && pip install -e . && python -m infera_worker.cli

# Terminal 3: Frontend
cd frontend && npm install && npm run dev
```

### With Docker
```bash
docker-compose up
```

Access the dashboard at http://localhost:3000

### With GPU Providers
```bash
# RunPod
RUNPOD_API_KEY=your_key go run ./cmd/gateway

# Vast.ai (coming soon)
VASTAI_API_KEY=your_key go run ./cmd/gateway
```

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
| `/api/stats` | GET | Cluster stats |
| `/health` | GET | Health check |

## Project Structure
```
infera/
├── go/                          # Go backend
│   ├── cmd/gateway/             # Gateway entrypoint
│   ├── internal/
│   │   ├── gateway/             # HTTP API handlers
│   │   ├── router/              # Request routing & load balancing
│   │   └── providers/           # GPU provider integrations
│   │       ├── runpod/          # RunPod API client
│   │       ├── vastai/          # Vast.ai API client (stub)
│   │       └── mock/            # Mock provider for testing
│   └── pkg/types/               # Shared types
│
├── python/                      # Python worker
│   └── src/infera_worker/
│       ├── worker.py            # Core worker logic
│       ├── http_server.py       # HTTP API
│       ├── engine.py            # Engine abstraction
│       └── vllm_engine.py       # vLLM integration
│
├── frontend/                    # React dashboard
│   └── src/
│       ├── components/          # UI components
│       ├── hooks/               # React Query hooks
│       ├── lib/                 # API client
│       └── types/               # TypeScript types
│
├── proto/                       # Protocol Buffers (future gRPC)
├── deploy/                      # Docker & K8s configs
└── scripts/                     # Development scripts
```

## Components

| Component | Language | Purpose |
|-----------|----------|---------|
| Gateway | Go | API endpoint, routing, provider management |
| Router | Go | Load balancing, request batching |
| Providers | Go | GPU provider integrations (RunPod, Vast.ai) |
| Worker | Python | Model loading, inference (vLLM) |
| Frontend | React | Dashboard, instance management |

## Supported GPU Providers

| Provider | Status | Features |
|----------|--------|----------|
| Mock | ✅ Complete | Testing without real GPUs |
| RunPod | ✅ Complete | On-demand & spot instances |
| Vast.ai | 🚧 Stub | Interface ready |
| Lambda | ⏳ Planned | - |

## Configuration

### Gateway

| Env Variable | Default | Description |
|--------------|---------|-------------|
| `RUNPOD_API_KEY` | - | RunPod API key |
| `VASTAI_API_KEY` | - | Vast.ai API key |

### Worker

| Env Variable | Default | Description |
|--------------|---------|-------------|
| `INFERA_ENGINE` | mock | Engine type (mock, vllm) |
| `INFERA_HTTP_PORT` | 8081 | Worker HTTP port |
| `INFERA_ROUTER_ADDRESS` | localhost:8080 | Gateway address |

## Development
```bash
# Build Go services
make go-build

# Install Python worker
make python-build

# Run tests
make test

# Build everything
make all

# Clean build artifacts
make clean
```

## License

MIT