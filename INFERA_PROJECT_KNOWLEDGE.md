# Infera: AI Inference Platform

## Complete Project Documentation & Knowledge Base

**Created:** February 2026  
**Last Updated:** March 3, 2026  
**Author:** Siddharth  
**Platform:** Distributed AI Inference System

---

## Table of Contents

1. [Project Overview](#project-overview)
2. [Origin Story](#origin-story)
3. [Architecture Design](#architecture-design)
4. [Technology Stack](#technology-stack)
5. [Component Details](#component-details)
6. [Implementation Timeline](#implementation-timeline)
7. [Key Learnings & Decisions](#key-learnings--decisions)
8. [Future Roadmap](#future-roadmap)
9. [Current State & Known Issues](#current-state--known-issues)

---

## Project Overview

### What is Infera?

**Infera** (inference + era) is a cost-efficient AI inference platform designed as a distributed system for serving large language models. The name was chosen to clearly convey the platform's purpose while being unique and brandable.

### Core Mission

Build the **most cost-efficient platform for LLM inference** by:
- Maximizing GPU utilization through smart batching and routing
- Minimizing infrastructure overhead with lightweight Go services
- Leveraging cutting-edge Python ML optimizations (vLLM, PagedAttention)
- Supporting elastic scaling on cloud GPU providers (RunPod, Vast.ai)

### Key Features

- **OpenAI-Compatible API**: Drop-in replacement for existing applications
- **Multi-Model Routing**: Serve multiple models with intelligent worker selection
- **Cloud GPU Integration**: Provision and manage GPU instances on RunPod/Vast.ai
- **Real-Time Dashboard**: Monitor workers, metrics, and inference requests
- **Auto-Registration**: Workers automatically register with the gateway
- **Streaming Support**: SSE-based token streaming for real-time responses

---

## Origin Story

### The Beginning (February 4, 2026)

The project started as a system design exercise, exploring high-level and low-level design patterns. From several options including:
- Distributed Build System
- Kubernetes Autoscaler  
- Multi-Agent Orchestration System
- Feature Flag Service
- Log Aggregation Pipeline

**AI Inference Platform** was chosen due to its relevance to ongoing MLX-LM explorations and the opportunity to build a substantial distributed system.

### Naming Session

After brainstorming names across categories:
- **Speed-focused**: Flux, Impulse, Quicksilver, Bolt
- **Neural-themed**: Synapse, Cortex, Axon, Lumen
- **Infrastructure-themed**: Forge, Foundry, Conduit, Nexus
- **Abstract/Unique**: Infera, Servion, Tensora

**"Infera"** was selected - a portmanteau of "inference" and "era," conveying both the technical purpose and a forward-looking vision.

---

## Architecture Design

### High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              INFERA PLATFORM                                │
│                                                                             │
│  ┌──────────────────────── Go (Infrastructure) ──────────────────────┐     │
│  │                                                                   │     │
│  │   ┌─────────┐     ┌─────────┐     ┌─────────┐                    │     │
│  │   │ Gateway │ ──► │ Router  │ ──► │  Vault  │                    │     │
│  │   │         │     │         │     │         │                    │     │
│  │   │ • Auth  │     │ • Smart │     │ • Model │                    │     │
│  │   │ • Rate  │     │   batch │     │   store │                    │     │
│  │   │   limit │     │   form  │     │ • Cache │                    │     │
│  │   └─────────┘     └────┬────┘     └─────────┘                    │     │
│  │                        │                                         │     │
│  └────────────────────────┼─────────────────────────────────────────┘     │
│                           │                                               │
│                           │ gRPC/HTTP                                     │
│                           ▼                                               │
│  ┌──────────────────────── Python (Inference) ──────────────────────┐     │
│  │                                                                   │     │
│  │   ┌───────────────────────────────────────────────────────────┐  │     │
│  │   │                     Worker Pool                           │  │     │
│  │   │                                                           │  │     │
│  │   │  ┌──────────┐  ┌──────────┐  ┌──────────┐                │  │     │
│  │   │  │ Worker 1 │  │ Worker 2 │  │ Worker N │                │  │     │
│  │   │  │ vLLM     │  │ vLLM     │  │ vLLM     │                │  │     │
│  │   │  │ GPU 0    │  │ GPU 1    │  │ GPU N    │                │  │     │
│  │   │  └──────────┘  └──────────┘  └──────────┘                │  │     │
│  │   └───────────────────────────────────────────────────────────┘  │     │
│  └───────────────────────────────────────────────────────────────────┘     │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Design Philosophy

**Polyglot Architecture (Go + Python)**:
- **Go** for infrastructure: Gateway, Router, Vault
  - Excellent concurrency with goroutines
  - Tiny container images (~10MB)
  - Native Kubernetes client
  - Sub-millisecond routing decisions
  
- **Python** for inference: Worker services
  - Native access to PyTorch, vLLM, CUDA
  - Continuous batching with PagedAttention
  - Quantization support (AWQ, GPTQ, INT8)
  - Direct HuggingFace integration

### Class Design Summary (77 Classes Designed)

The complete design includes:

| Category | Classes |
|----------|---------|
| **Core Request/Response** | InferenceRequest, InferenceResponse, Message, TokenChunk, UsageStats |
| **Router & Scheduling** | Router, WorkerRegistry, RoutingStrategy, BatchManager, Dispatcher |
| **Strategies** | LeastLoaded, LatencyBased, RoundRobin, Affinity, Experiment |
| **Worker** | Worker, ModelManager, InferenceEngine, KVCacheManager, Sampler |
| **Vault** | Vault, ModelRegistry, ArtifactStore, VersionManager, AccessControl |
| **Batching** | BatchScheduler, BatchQueue, BatchConfig, BatchContext |
| **Resources** | ResourcePool, Device, DeviceStats, MemoryBlock |

---

## Technology Stack

### Backend - Go Services

| Component | Technology |
|-----------|------------|
| HTTP Server | net/http + Chi router |
| gRPC | google.golang.org/grpc |
| Configuration | Environment variables |
| Logging | Standard library log |
| UUID Generation | github.com/google/uuid |

### Backend - Python Services

| Component | Technology |
|-----------|------------|
| Inference Engine | vLLM |
| HTTP Server | aiohttp |
| gRPC | grpcio |
| Configuration | pydantic-settings |
| Model Loading | HuggingFace Transformers |

### Frontend

| Component | Technology |
|-----------|------------|
| Framework | React 18 |
| Build Tool | Vite |
| Styling | Tailwind CSS v4 |
| UI Components | shadcn/ui (tweakcn theme) |
| Fonts | Inter, JetBrains Mono |
| State Management | React hooks + Context |

### Infrastructure

| Component | Technology |
|-----------|------------|
| Containerization | Docker |
| GPU Providers | RunPod, Vast.ai |
| Base Images | vllm/vllm-openai |

---

## Component Details

### Gateway (Go)

The API Gateway provides:
- OpenAI-compatible REST endpoints (`/v1/chat/completions`)
- Request validation and authentication
- Rate limiting per API key
- Streaming response handling (SSE)
- Worker health aggregation
- Instance provisioning API

**Key Endpoints:**
```
POST /v1/chat/completions     - Inference requests
GET  /api/workers             - List workers
GET  /api/stats               - Platform statistics
POST /api/instances/provision - Provision GPU instances
GET  /api/instances           - List instances
```

### Router

The Router handles:
- Worker registration and health tracking
- Intelligent request routing strategies
- Batch formation for efficiency
- Load balancing across workers

**Routing Strategies:**
1. **Least Loaded**: Route to worker with lowest queue depth
2. **Round Robin**: Even distribution across workers
3. **Latency Based**: Prefer workers with lowest response times
4. **Affinity**: Sticky routing for session consistency

### Worker (Python)

The Python Worker provides:
- vLLM engine integration for inference
- Model loading from HuggingFace
- Auto-registration with Gateway
- Heartbeat reporting
- HTTP health checks for RunPod
- gRPC for internal communication

**Configuration:**
```python
INFERA_WORKER_ID          # Unique worker identifier
INFERA_WORKER_ADDRESS     # Public address for callbacks
INFERA_ROUTER_ADDRESS     # Gateway registration endpoint
INFERA_PRELOAD_MODELS     # Models to load on startup
```

### Frontend Dashboard

**Features:**
- Real-time worker monitoring
- GPU provisioning interface with toggle selection
- Chat playground with persistent state
- Model selector with live availability
- Dark/Light mode support
- Responsive sidebar navigation

**Pages:**
- Dashboard: Overview stats, quick actions
- Playground: Interactive chat interface
- Workers: Worker management
- Models: Model catalog
- Instances: GPU instance management
- Settings: Platform configuration
- Logs: Real-time log viewer

---

## Implementation Timeline

### Phase 1: Core Design (Feb 4-22, 2026) ✅

- Initial system design and architecture
- Named the platform "Infera"
- Designed 77 classes across all components
- Decided on Go + Python polyglot architecture
- Created proto definitions for gRPC

### Phase 2: Vertical Slice (Feb 23-26, 2026) ✅

- Implemented Go Gateway with OpenAI-compatible API
- Built Python Worker with vLLM integration
- Created React dashboard with real-time monitoring
- Docker Compose setup for local development
- Worker auto-registration system

### Phase 3: GPU Provider Integration (Feb 26, 2026) ✅

- RunPod API integration
- Vast.ai API integration
- Mock provider for testing
- Instance provisioning UI
- Cost tracking implementation

### Phase 4: RunPod Deployment (Feb 27 - Mar 1, 2026) ✅

- Docker image creation for workers
- Worker deployment on RunPod GPUs
- Fixed HTTPS/HTTP protocol mismatches
- Resolved worker registration issues
- Fixed inference nil pointer bugs
- Corrected vLLM chat template issues

### Phase 5: Frontend Redesign (Mar 2, 2026) ✅

- Vercel-inspired UI overhaul
- Tailwind v4 + shadcn/ui integration
- GPU selection with toggle behavior
- Chat state persistence across navigation
- Light/dark mode improvements
- Stats display fixes

---

## Key Learnings & Decisions

### Technical Discoveries

1. **Pydantic-settings Parsing**: Configuration parsing issues can occur before field validators run. Solution: Direct environment variable access to bypass automatic JSON parsing for complex fields.

2. **RunPod Health Checks**: RunPod expects HTTP health endpoints, but core inference uses gRPC. Solution: Run both servers concurrently - HTTP on port 8000 for health, gRPC on port 50051 for inference.

3. **Worker Registration**: Workers need proper `INFERA_ROUTER_ADDRESS` configuration. The gateway URL should NOT include protocol prefix for internal registration.

4. **vLLM Chat Templates**: Duplicate token generation can occur with improper chat template handling. Solution: Use `add_generation_prompt=True` and avoid double-encoding.

5. **RunPod Public Address**: Use `RUNPOD_POD_ID` environment variable to construct the public proxy URL: `https://{pod_id}-8000.proxy.runpod.net`

### Architecture Decisions

1. **Why Polyglot (Go + Python)?**
   - Go: 10K+ concurrent connections, tiny binaries, K8s native
   - Python: Native ML libraries, vLLM/CUDA support
   - Clean separation: infra vs inference

2. **Why gRPC + HTTP?**
   - gRPC: Efficient internal communication
   - HTTP: External API compatibility (OpenAI format)

3. **Why vLLM?**
   - Continuous batching
   - PagedAttention (24x memory efficiency)
   - Production-proven
   - Easy HuggingFace integration

### UI/UX Decisions

1. **Persistent Chat State**: Implemented to avoid losing conversation context during navigation
2. **Toggle Selection**: GPU cards now toggle on re-click for intuitive UX
3. **Dark Mode Default**: Developer-friendly, reduces eye strain
4. **Minimal Formatting**: Clean interface without visual clutter

---

## Future Roadmap

### Phase 6: Auto-Scaling & Cost Optimization (Planned)

- [ ] Scale-to-zero when idle
- [ ] Spot instance support
- [ ] Request-based scaling triggers
- [ ] Cost analytics dashboard

### Phase 7: Vault - Model Registry (Planned)

- [ ] Model versioning
- [ ] Artifact storage (S3/GCS)
- [ ] Model metadata management
- [ ] Access control per model

### Phase 8: Authentication & Multi-Tenancy (Planned)

- [ ] API key management
- [ ] Per-user rate limiting
- [ ] Usage tracking and billing
- [ ] Quotas and permissions

### Phase 9: Observability (Planned)

- [ ] Prometheus metrics
- [ ] Distributed tracing
- [ ] Log aggregation
- [ ] Alerting rules

### Phase 10: Advanced Features (Planned)

- [ ] A/B testing for models
- [ ] Speculative decoding
- [ ] Prefix caching
- [ ] Multi-GPU tensor parallelism

---

## Current State & Known Issues

### Working Features ✅

- Gateway with OpenAI-compatible API
- Worker registration and health reporting
- RunPod GPU provisioning
- Model loading and inference
- Streaming token responses
- React dashboard with all pages
- Light/dark mode theming
- GPU instance management UI

### Known Issues

1. **Latency Display**: Shows `0ms` when no requests processed (expected behavior, not a bug)
2. **Worker Cold Start**: Model loading takes 30-60 seconds on first request
3. **Memory**: Large models require careful GPU memory management

### File Structure

```
infera/
├── gateway/                 # Go Gateway service
│   ├── cmd/
│   │   └── gateway/
│   │       └── main.go
│   ├── internal/
│   │   ├── handlers/
│   │   ├── registry/
│   │   └── providers/
│   └── go.mod
├── worker/                  # Python Worker service
│   ├── src/
│   │   └── infera_worker/
│   │       ├── __init__.py
│   │       ├── worker.py
│   │       ├── config.py
│   │       ├── engine.py
│   │       └── grpc_server.py
│   ├── Dockerfile
│   └── pyproject.toml
├── frontend/                # React Dashboard
│   ├── src/
│   │   ├── components/
│   │   ├── pages/
│   │   ├── lib/
│   │   └── App.tsx
│   ├── index.html
│   └── package.json
├── proto/                   # gRPC definitions
│   └── inference.proto
├── docker-compose.yml
├── Makefile
└── README.md
```

---

## Appendix: Original Design Goals

### Functional Requirements (From Day 1)

- Serve multiple models (different sizes, architectures)
- Handle both streaming and batch inference requests
- Support model versioning and A/B testing
- Provide usage tracking and rate limiting per user/API key
- Enable model hot-swapping without downtime

### Non-Functional Requirements (From Day 1)

- Low latency (P99 < 500ms for small models)
- High availability (99.9%+)
- Efficient GPU utilization (batching, queuing)
- Horizontal scalability
- Graceful degradation under load

### Cost Efficiency Metrics to Track

| Metric | Target | Why |
|--------|--------|-----|
| GPU Utilization | >80% | Paying for idle GPU is waste |
| Tokens/second/GPU | Model-dependent | Throughput efficiency |
| Batch size (avg) | >4 | Higher = better GPU efficiency |
| Queue wait time (P50) | <100ms | Too high = over-batching |
| Request rejection rate | <1% | Wasted client retries |
| Cold start time | <30s | Autoscaling responsiveness |
| Cost per 1M tokens | Track trend | The ultimate metric |

---

## Summary

Infera started as a system design exercise and evolved into a functional AI inference platform with:

- **77 designed classes** covering all aspects of distributed inference
- **Go + Python polyglot architecture** for optimal performance
- **Working vertical slice** with OpenAI-compatible API
- **Cloud GPU integration** with RunPod
- **Modern React dashboard** with Vercel-inspired design

The platform embodies the principle of using the right tool for each job: Go for lightweight, high-concurrency infrastructure, and Python for the ML-heavy inference workload. This split maximizes cost efficiency while maintaining developer velocity.

---

*This document serves as the single source of truth for the Infera project, capturing all design decisions, implementation details, and lessons learned throughout development.*
