.PHONY: all build test clean dev deps help
.PHONY: go-build go-test go-lint run-gateway
.PHONY: python-build python-test python-lint run-worker
.PHONY: frontend-build frontend-test frontend-lint run-frontend
.PHONY: docker-build docker-push docker-up docker-down
.PHONY: ngrok smoke-prod

# ============================================================================
# Configuration (override with environment variables)
# ============================================================================
RUNPOD_API_KEY ?= 
HF_TOKEN ?= 
INFERA_GATEWAY_ADDRESS ?= 
INFERA_WORKER_IMAGE ?= 
INFERA_GITHUB_REPO ?= 
DOCKER_USERNAME ?= 
INFERA_BASE_URL ?= https://inferai.co.in
PYTHON_CMD ?= $(if $(wildcard $(CURDIR)/python/venv/bin/python),$(CURDIR)/python/venv/bin/python,python3)
GOLANGCI_LINT_VERSION ?= v1.64.8
GOLANGCI_LINT_BIN ?= $(shell command -v golangci-lint 2>/dev/null)
GOLANGCI_LINT_CMD ?= $(if $(GOLANGCI_LINT_BIN),$(GOLANGCI_LINT_BIN),go run github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION))

# Ports
GATEWAY_PORT ?= 8080
FRONTEND_PORT ?= 5173
WORKER_PORT ?= 8081

# ============================================================================
# Default target
# ============================================================================
all: deps build

build: go-build frontend-build

# ============================================================================
# Dependencies
# ============================================================================
deps: go-deps python-deps frontend-deps

go-deps:
	cd go && go mod tidy

python-deps:
	cd python && $(PYTHON_CMD) -m pip install -e ".[dev]"

frontend-deps:
	cd frontend && npm install

# ============================================================================
# Go Gateway
# ============================================================================
go-build:
	cd go && go build -o ../bin/gateway ./cmd/gateway

go-test:
	cd go && go test -v ./...

go-test-cover:
	cd go && go test -v -cover ./...

go-lint:
	cd go && $(GOLANGCI_LINT_CMD) run

# Run gateway (basic - mock only)
run-gateway:
	cd go && INFERA_DEV_MODE=1 go run ./cmd/gateway -port $(GATEWAY_PORT)

# Run gateway with RunPod
run-gateway-runpod:
	@if [ -z "$(RUNPOD_API_KEY)" ]; then \
		echo "Error: RUNPOD_API_KEY is required"; \
		echo "Usage: make run-gateway-runpod RUNPOD_API_KEY=your_key"; \
		exit 1; \
	fi
	cd go && INFERA_DEV_MODE=1 \
		RUNPOD_API_KEY=$(RUNPOD_API_KEY) \
		INFERA_GATEWAY_ADDRESS=$(INFERA_GATEWAY_ADDRESS) \
		HF_TOKEN=$(HF_TOKEN) \
		INFERA_WORKER_IMAGE=$(INFERA_WORKER_IMAGE) \
		INFERA_GITHUB_REPO=$(INFERA_GITHUB_REPO) \
		go run ./cmd/gateway -port $(GATEWAY_PORT)

# ============================================================================
# Python Worker
# ============================================================================
python-build:
	cd python && $(PYTHON_CMD) -m pip install -e .

python-test:
	cd python && $(PYTHON_CMD) -m pytest -v

python-test-cover:
	cd python && $(PYTHON_CMD) -m pytest -v --cov=infera_worker --cov-report=html

python-lint:
	cd python && $(PYTHON_CMD) -m ruff check .

python-format:
	cd python && $(PYTHON_CMD) -m ruff format .

# Run worker with mock engine (no GPU required)
run-worker:
	cd python && INFERA_ENGINE=mock \
		INFERA_HTTP_PORT=$(WORKER_PORT) \
		$(PYTHON_CMD) -m infera_worker.cli

# Run worker with mock engine and connect to gateway
run-worker-connected:
	cd python && INFERA_ENGINE=mock \
		INFERA_HTTP_PORT=$(WORKER_PORT) \
		INFERA_ROUTER_ADDRESS=localhost:$(GATEWAY_PORT) \
		$(PYTHON_CMD) -m infera_worker.cli

# Run worker with vLLM (requires GPU)
run-worker-vllm:
	cd python && INFERA_ENGINE=vllm \
		INFERA_HTTP_PORT=$(WORKER_PORT) \
		INFERA_ROUTER_ADDRESS=localhost:$(GATEWAY_PORT) \
		$(PYTHON_CMD) -m infera_worker.cli

# ============================================================================
# Frontend
# ============================================================================
frontend-build:
	cd frontend && npm run build

frontend-test:
	cd frontend && npm test

frontend-test-run:
	cd frontend && npm run test:run

frontend-lint:
	cd frontend && npm run lint

run-frontend:
	cd frontend && npm run dev

# ============================================================================
# Combined Targets
# ============================================================================
test: go-test python-test frontend-test-run

lint: go-lint python-lint frontend-lint

# ============================================================================
# Development - Run All Services
# ============================================================================

# Run gateway + frontend (most common for development)
dev:
	@echo "Starting Infera development environment..."
	@echo ""
	@echo "Run these commands in separate terminals:"
	@echo ""
	@echo "  Terminal 1 (Gateway):"
	@echo "    make run-gateway"
	@echo ""
	@echo "  Terminal 2 (Frontend):"
	@echo "    make run-frontend"
	@echo ""
	@echo "  Terminal 3 (Worker - optional):"
	@echo "    make run-worker-connected"
	@echo ""
	@echo "Then open: http://localhost:$(FRONTEND_PORT)"

# Quick start with gateway only
dev-gateway:
	@echo "Starting gateway on port $(GATEWAY_PORT)..."
	@make run-gateway

# Quick start with RunPod
dev-runpod:
	@if [ -z "$(RUNPOD_API_KEY)" ]; then \
		echo ""; \
		echo "Usage: make dev-runpod RUNPOD_API_KEY=your_key"; \
		echo ""; \
		echo "Optional:"; \
		echo "  INFERA_GATEWAY_ADDRESS=abc123.ngrok-free.app  (for worker registration)"; \
		echo "  HF_TOKEN=hf_xxx                                (for gated models)"; \
		echo ""; \
		exit 1; \
	fi
	@make run-gateway-runpod

# ============================================================================
# Docker
# ============================================================================
docker-build:
	docker-compose build

docker-build-worker:
	docker build -t infera-worker:latest -f python/Dockerfile python/

docker-build-worker-light:
	docker build -t infera-worker:light -f python/Dockerfile.light python/

docker-build-gateway:
	docker build -t infera-gateway:latest -f deploy/docker/Dockerfile.gateway .

# Push to Docker Hub (requires DOCKER_USERNAME)
docker-push:
	@if [ -z "$(DOCKER_USERNAME)" ]; then \
		echo "Error: DOCKER_USERNAME is required"; \
		echo "Usage: make docker-push DOCKER_USERNAME=your_username"; \
		exit 1; \
	fi
	docker tag infera-worker:latest $(DOCKER_USERNAME)/infera-worker:latest
	docker push $(DOCKER_USERNAME)/infera-worker:latest
	@echo ""
	@echo "Pushed: $(DOCKER_USERNAME)/infera-worker:latest"
	@echo ""
	@echo "To use with RunPod, set:"
	@echo "  INFERA_WORKER_IMAGE=$(DOCKER_USERNAME)/infera-worker:latest"

docker-up:
	docker-compose up

docker-up-detached:
	docker-compose up -d

docker-down:
	docker-compose down

docker-logs:
	docker-compose logs -f

# ============================================================================
# Utilities
# ============================================================================

# Start ngrok tunnel for external access
ngrok:
	@echo "Starting ngrok tunnel to port $(GATEWAY_PORT)..."
	@echo "Use the HTTPS URL for INFERA_GATEWAY_ADDRESS"
	@echo ""
	ngrok http $(GATEWAY_PORT)

# Check if services are running
status:
	@echo "Checking services..."
	@echo ""
	@echo "Gateway (port $(GATEWAY_PORT)):"
	@curl -s http://localhost:$(GATEWAY_PORT)/health 2>/dev/null && echo " ✓ Running" || echo " ✗ Not running"
	@echo ""
	@echo "Worker (port $(WORKER_PORT)):"
	@curl -s http://localhost:$(WORKER_PORT)/health 2>/dev/null && echo " ✓ Running" || echo " ✗ Not running"
	@echo ""
	@echo "Frontend (port $(FRONTEND_PORT)):"
	@curl -s http://localhost:$(FRONTEND_PORT) 2>/dev/null > /dev/null && echo " ✓ Running" || echo " ✗ Not running"

# List RunPod providers and offerings
check-providers:
	@curl -s http://localhost:$(GATEWAY_PORT)/api/providers | python3 -m json.tool 2>/dev/null || echo "Gateway not running"

check-offerings:
	@curl -s http://localhost:$(GATEWAY_PORT)/api/offerings | python3 -m json.tool 2>/dev/null || echo "Gateway not running"

# Production smoke test (health + authenticated models list)
smoke-prod:
	@if [ -z "$(INFERA_SMOKE_API_KEY)" ] && [ -z "$(INFERA_ADMIN_KEY)" ]; then \
		echo "Error: INFERA_SMOKE_API_KEY (or INFERA_ADMIN_KEY) is required"; \
		echo "Usage: make smoke-prod INFERA_SMOKE_API_KEY=inf_xxx [INFERA_BASE_URL=https://inferai.co.in]"; \
		exit 1; \
	fi
	@./scripts/smoke-test.sh "$(INFERA_BASE_URL)"

# ============================================================================
# Clean
# ============================================================================
clean:
	rm -rf bin/
	rm -rf go/pkg/proto/
	rm -rf python/src/infera_proto/
	rm -rf python/.pytest_cache/
	rm -rf python/htmlcov/
	rm -rf frontend/dist/
	find . -type d -name __pycache__ -exec rm -rf {} + 2>/dev/null || true
	find . -type d -name .pytest_cache -exec rm -rf {} + 2>/dev/null || true

clean-all: clean
	rm -rf frontend/node_modules/
	rm -rf python/venv/

# ============================================================================
# Help
# ============================================================================
help:
	@echo "╔══════════════════════════════════════════════════════════════════════╗"
	@echo "║                         Infera Makefile                              ║"
	@echo "╚══════════════════════════════════════════════════════════════════════╝"
	@echo ""
	@echo "QUICK START"
	@echo "───────────"
	@echo "  make deps                    Install all dependencies"
	@echo "  make run-gateway             Start gateway (mock provider only)"
	@echo "  make run-frontend            Start frontend dev server"
	@echo "  make dev                     Show development instructions"
	@echo ""
	@echo "WITH RUNPOD"
	@echo "───────────"
	@echo "  make run-gateway-runpod RUNPOD_API_KEY=xxx"
	@echo "  make run-gateway-runpod RUNPOD_API_KEY=xxx INFERA_GATEWAY_ADDRESS=abc.ngrok.io"
	@echo ""
	@echo "BUILD"
	@echo "─────"
	@echo "  make build                   Build gateway and frontend"
	@echo "  make go-build                Build Go gateway binary"
	@echo "  make frontend-build          Build frontend for production"
	@echo ""
	@echo "TEST"
	@echo "────"
	@echo "  make test                    Run all tests"
	@echo "  make go-test                 Run Go tests"
	@echo "  make python-test             Run Python tests"
	@echo "  make frontend-test-run       Run frontend tests"
	@echo ""
	@echo "DOCKER"
	@echo "──────"
	@echo "  make docker-build-worker     Build worker Docker image"
	@echo "  make docker-push DOCKER_USERNAME=xxx   Push to Docker Hub"
	@echo "  make docker-up               Start all services with Docker Compose"
	@echo ""
	@echo "UTILITIES"
	@echo "─────────"
	@echo "  make ngrok                   Start ngrok tunnel (for RunPod workers)"
	@echo "  make status                  Check if services are running"
	@echo "  make check-providers         List available providers"
	@echo "  make check-offerings         List GPU offerings"
	@echo "  make clean                   Clean build artifacts"
	@echo ""
	@echo "ENVIRONMENT VARIABLES"
	@echo "─────────────────────"
	@echo "  RUNPOD_API_KEY               RunPod API key"
	@echo "  HF_TOKEN                     HuggingFace token (for gated models)"
	@echo "  INFERA_GATEWAY_ADDRESS       Public gateway URL (for worker registration)"
	@echo "  INFERA_WORKER_IMAGE          Custom worker Docker image"
	@echo "  DOCKER_USERNAME              Docker Hub username"
