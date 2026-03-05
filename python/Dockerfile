# Infera Worker - vLLM Inference Engine
#
# Build strategy:
# - Use RunPod's CUDA 12.4 base for Python 3.11 + system deps
# - Remove pre-installed PyTorch to avoid version conflicts
# - pip install vllm (pulls its own PyTorch + bundled CUDA runtime)
# - vLLM wheels bundle CUDA runtime, so host only needs driver >= 525
# - No GPU needed at build time — only at runtime
#
FROM --platform=linux/amd64 runpod/pytorch:2.4.0-py3.11-cuda12.4.1-devel-ubuntu22.04

WORKDIR /app

# Install system dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    curl \
    git \
    && rm -rf /var/lib/apt/lists/*

# Remove pre-installed PyTorch from base image to avoid version conflicts.
# vLLM will pull its own compatible PyTorch version.
RUN pip uninstall -y torch torchvision torchaudio 2>/dev/null; true

# Upgrade pip
RUN pip install --upgrade pip

# Install vLLM — this is a pre-built wheel from PyPI that bundles its own
# CUDA runtime. It pulls PyTorch as a dependency automatically.
# Using plain pip (not uv) because uv's --torch-backend=auto needs a GPU
# at install time which isn't available during Docker build.
RUN pip install --no-cache-dir \
    "vllm>=0.16.0" \
    "transformers>=4.43.0" \
    "accelerate>=0.30.0" \
    "tokenizers>=0.19.0" \
    "aiohttp>=3.9.0"

# Copy worker source code
COPY pyproject.toml .
COPY README.md .
COPY src/ ./src/

# Install the worker package and verify ALL critical imports succeed.
# If any import fails here, the Docker build fails — no silent breakage.
RUN pip install --no-cache-dir -e . && \
    echo "=== Verifying installation ===" && \
    python -c "import torch; print(f'PyTorch {torch.__version__}, CUDA: {torch.version.cuda}')" && \
    python -c "import transformers; print(f'Transformers {transformers.__version__}')" && \
    python -c "import vllm; print(f'vLLM {vllm.__version__}')" && \
    python -c "from vllm import AsyncLLMEngine, AsyncEngineArgs, SamplingParams; print('vLLM imports OK')" && \
    python -c "from infera_worker.engines.vllm_engine import VLLMEngine; print('VLLMEngine OK')" && \
    python -c "from infera_worker.engine import create_engine; print('create_engine OK')"

# Environment variables
ENV INFERA_ENGINE=vllm
ENV INFERA_HTTP_PORT=8081
ENV INFERA_GRPC_PORT=50051
ENV INFERA_LOG_LEVEL=INFO
ENV INFERA_LOG_FORMAT=json

# vLLM V1 uses multiprocessing — spawn avoids CUDA fork issues in containers
ENV VLLM_WORKER_MULTIPROC_METHOD=spawn

# HuggingFace cache directory
ENV HF_HOME=/app/.cache/huggingface

# Default model to load (can be overridden at runtime)
ENV INFERA_PRELOAD_MODELS=""

# Gateway address (set at runtime)
ENV INFERA_ROUTER_ADDRESS=""

# Expose ports
EXPOSE 8081 50051

# Health check - long start period for model download + loading
HEALTHCHECK --interval=30s --timeout=10s --start-period=300s --retries=3 \
    CMD curl -f http://localhost:8081/health || exit 1

# Run the worker
CMD ["python", "-m", "infera_worker.cli"]
