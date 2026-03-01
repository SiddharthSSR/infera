# Infera Worker - vLLM Inference Engine
# Base image with CUDA support for vLLM
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

# Install vLLM with compatible versions for PyTorch 2.1
# Pin transformers to version compatible with PyTorch 2.1
# Install vLLM with compatible versions
RUN pip install \
    vllm==0.4.2 \
    transformers==4.41.2 \
    accelerate==0.27.0 \
    tokenizers>=0.19.0 \
    aiohttp

# Copy worker source code
COPY pyproject.toml .
# COPY setup.py .
COPY README.md .
COPY src/ ./src/

# Install the worker package
RUN pip install --no-cache-dir -e . && \
    echo "=== Verifying installation ===" && \
    python -c "import torch; print(f'✓ PyTorch {torch.__version__}')" && \
    python -c "import transformers; print(f'✓ Transformers {transformers.__version__}')" && \
    python -c "import infera_worker; print('✓ infera_worker')" && \
    python -c "from infera_worker.engines.vllm_engine import VLLMEngine; print('✓ VLLMEngine')" && \
    python -c "from infera_worker.engine import create_engine; print('✓ create_engine')"

# Environment variables
ENV INFERA_ENGINE=vllm
ENV INFERA_HTTP_PORT=8081
ENV INFERA_GRPC_PORT=50051
ENV INFERA_LOG_LEVEL=INFO
ENV INFERA_LOG_FORMAT=json

# HuggingFace cache directory (for model downloads)
ENV HF_HOME=/app/.cache/huggingface
ENV TRANSFORMERS_CACHE=/app/.cache/huggingface

# Default model to load (can be overridden)
ENV INFERA_PRELOAD_MODELS=""

# Gateway address (set at runtime)
ENV INFERA_ROUTER_ADDRESS=""

# Expose ports
EXPOSE 8081 50051

# Health check - increased start period for model loading
HEALTHCHECK --interval=30s --timeout=10s --start-period=300s --retries=3 \
    CMD curl -f http://localhost:8081/health || exit 1

# Run the worker
CMD ["python", "-m", "infera_worker.cli"]