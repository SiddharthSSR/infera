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