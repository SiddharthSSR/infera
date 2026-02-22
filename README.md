# Infera

> AI Inference Platform — Cost-efficient LLM serving at scale

## Architecture
```
Gateway (Go) → Router (Go) → Workers (Python) 
                    ↓
               Vault (Go)
```

## Quick Start
```bash
# Generate proto files
make proto

# Build Go services
make go-build

# Install Python worker
make python-build

# Run tests
make go-test
make python-test
```

## Components

| Component | Language | Purpose |
|-----------|----------|---------|
| Gateway | Go | API endpoint, auth, rate limiting |
| Router | Go | Request scheduling, batching |
| Vault | Go | Model registry, artifacts |
| Worker | Python | Model loading, inference |

infera/
├── README.md
├── Makefile
├── proto/
│   ├── buf.yaml
│   ├── buf.gen.yaml
│   ├── common.proto
│   ├── inference.proto
│   ├── router.proto
│   ├── vault.proto
│   ├── worker.proto
│   └── gateway.proto
├── go/
│   ├── go.mod
│   ├── cmd/
│   │   ├── gateway/
│   │   ├── router/
│   │   └── vault/
│   ├── internal/
│   │   └── router/
│   │       ├── router.go
│   │       ├── router_test.go
│   │       ├── registry/
│   │       │   └── registry.go
│   │       ├── strategy/
│   │       │   ├── strategy.go
│   │       │   ├── engine.go
│   │       │   ├── least_loaded.go
│   │       │   ├── round_robin.go
│   │       │   ├── latency_based.go
│   │       │   └── affinity.go
│   │       └── batcher/
│   │           └── batcher.go
│   └── pkg/
│       └── types/
│           ├── types.go
│           ├── worker.go
│           └── routing.go
├── python/
│   ├── pyproject.toml
│   ├── src/
│   │   ├── infera_worker/
│   │   │   ├── __init__.py
│   │   │   ├── config.py
│   │   │   ├── types.py
│   │   │   ├── engine.py
│   │   │   ├── worker.py
│   │   │   ├── server.py
│   │   │   ├── cli.py
│   │   │   └── engines/
│   │   │       ├── __init__.py
│   │   │       └── vllm_engine.py
│   │   └── infera_proto/
│   │       └── __init__.py
│   └── tests/
│       └── test_worker.py
└── deploy/
    ├── docker/
    └── k8s/