.PHONY: all proto go-build python-build frontend-build test go-test python-test clean dev docker-build

# Default target
all: go-build python-build frontend-build

# Protocol buffers (for future gRPC)
proto:
	cd proto && buf generate

# Go services
go-build:
	cd go && go build -o ../bin/gateway ./cmd/gateway

go-test:
	cd go && go test -v ./...

go-lint:
	cd go && golangci-lint run

# Python worker
python-build:
	cd python && pip install -e .

python-test:
	cd python && pytest -v

python-lint:
	cd python && ruff check .

# Frontend
frontend-build:
	cd frontend && npm install && npm run build

frontend-dev:
	cd frontend && npm run dev

# Combined targets
test: go-test python-test

lint: go-lint python-lint

# Development
dev:
	./scripts/dev.sh

# Docker
docker-build:
	docker-compose build

docker-up:
	docker-compose up

docker-down:
	docker-compose down

# Run individual services
run-gateway:
	cd go && go run ./cmd/gateway -port 8080

run-worker:
	cd python && INFERA_ENGINE=mock python -m infera_worker.cli

run-frontend:
	cd frontend && npm run dev

# Clean
clean:
	rm -rf bin/
	rm -rf go/pkg/proto/
	rm -rf python/src/infera_proto/
	rm -rf frontend/dist/
	rm -rf frontend/node_modules/

# Install dependencies
deps:
	cd go && go mod tidy
	cd python && pip install -e ".[dev]"
	cd frontend && npm install

# Help
help:
	@echo "Infera Makefile"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  all            Build all components"
	@echo "  go-build       Build Go gateway"
	@echo "  python-build   Install Python worker"
	@echo "  frontend-build Build React frontend"
	@echo "  test           Run all tests"
	@echo "  dev            Run all services locally"
	@echo "  docker-up      Start with Docker Compose"
	@echo "  clean          Clean build artifacts"
	@echo "  deps           Install dependencies"