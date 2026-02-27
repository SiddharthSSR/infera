# Infera Worker - vLLM Inference Engine
# Base image with CUDA support for vLLM
# Use --platform=linux/amd64 when building on ARM Macs
FROM --platform=linux/amd64 runpod/pytorch:2.1.0-py3.10-cuda11.8.0-devel-ubuntu22.04

WORKDIR /app

# Install system dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    curl \
    git \
    && rm -rf /var/lib/apt/lists/*

# Upgrade pip
RUN pip install --upgrade pip

# Install vLLM (this takes a while)
RUN pip install vllm==0.3.3

# Copy worker source code
COPY pyproject.toml .
COPY README.md .
COPY src/ ./src/

# Install the worker package
RUN pip install --no-cache-dir .

# Environment variables
ENV INFERA_ENGINE=vllm
ENV INFERA_HTTP_PORT=8081
ENV INFERA_GRPC_PORT=50051
ENV INFERA_LOG_LEVEL=INFO
ENV INFERA_LOG_FORMAT=json

# Default model to load (can be overridden)
ENV INFERA_PRELOAD_MODELS=""

# Gateway address (set at runtime)
ENV INFERA_ROUTER_ADDRESS=""

# Expose ports
EXPOSE 8081 50051

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
    CMD curl -f http://localhost:8081/health || exit 1

# Run the worker
CMD ["python", "-m", "infera_worker.cli"]