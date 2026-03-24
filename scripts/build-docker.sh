#!/bin/bash
# Build and push Infera Docker images
set -e

# Configuration
REGISTRY="${DOCKER_REGISTRY:-docker.io}"
NAMESPACE="${DOCKER_NAMESPACE:-infera}"
VERSION="${VERSION:-latest}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${GREEN}Infera Docker Build Script${NC}"
echo "Registry: $REGISTRY"
echo "Namespace: $NAMESPACE"
echo "Version: $VERSION"
echo ""

# Check if docker is available
if ! command -v docker &> /dev/null; then
    echo -e "${RED}Error: docker is not installed${NC}"
    exit 1
fi

# Build function
build_image() {
    local name=$1
    local dockerfile=$2
    local context=$3
    shift 3
    local tag="$REGISTRY/$NAMESPACE/$name:$VERSION"
    
    echo -e "${YELLOW}Building $name...${NC}"
    docker build "$@" -t "$tag" -f "$dockerfile" "$context"
    echo -e "${GREEN}✓ Built $tag${NC}"
    echo ""
}

# Push function
push_image() {
    local name=$1
    local tag="$REGISTRY/$NAMESPACE/$name:$VERSION"
    
    echo -e "${YELLOW}Pushing $name...${NC}"
    docker push "$tag"
    echo -e "${GREEN}✓ Pushed $tag${NC}"
    echo ""
}

# Parse arguments
BUILD_GATEWAY=false
BUILD_WORKER=false
BUILD_WORKER_VLLM=false
BUILD_WORKER_SGLANG=false
BUILD_WORKER_TENSORRT_LLM=false
BUILD_FRONTEND=false
PUSH=false

WORKER_SGLANG_PACKAGE="${WORKER_SGLANG_PACKAGE:-sglang}"
WORKER_TENSORRT_LLM_PACKAGE="${WORKER_TENSORRT_LLM_PACKAGE:-tensorrt_llm}"
WORKER_TENSORRT_LLM_PYPI_EXTRA_INDEX_URL="${WORKER_TENSORRT_LLM_PYPI_EXTRA_INDEX_URL:-https://pypi.nvidia.com}"

while [[ $# -gt 0 ]]; do
    case $1 in
        --gateway)
            BUILD_GATEWAY=true
            shift
            ;;
        --worker)
            BUILD_WORKER=true
            shift
            ;;
        --worker-vllm)
            BUILD_WORKER_VLLM=true
            shift
            ;;
        --worker-sglang)
            BUILD_WORKER_SGLANG=true
            shift
            ;;
        --worker-tensorrt-llm)
            BUILD_WORKER_TENSORRT_LLM=true
            shift
            ;;
        --worker-all-engines)
            BUILD_WORKER_VLLM=true
            BUILD_WORKER_SGLANG=true
            BUILD_WORKER_TENSORRT_LLM=true
            shift
            ;;
        --frontend)
            BUILD_FRONTEND=true
            shift
            ;;
        --all)
            BUILD_GATEWAY=true
            BUILD_WORKER=true
            BUILD_WORKER_VLLM=true
            BUILD_WORKER_SGLANG=true
            BUILD_WORKER_TENSORRT_LLM=true
            BUILD_FRONTEND=true
            shift
            ;;
        --push)
            PUSH=true
            shift
            ;;
        --help)
            echo "Usage: $0 [options]"
            echo ""
            echo "Options:"
            echo "  --gateway       Build gateway image"
            echo "  --worker        Build worker image (mock/light)"
            echo "  --worker-vllm   Build worker image with vLLM"
            echo "  --worker-sglang Build worker image with SGLang"
            echo "  --worker-tensorrt-llm Build worker image with TensorRT-LLM"
            echo "  --worker-all-engines Build all engine-specific worker images"
            echo "  --frontend      Build frontend image"
            echo "  --all           Build all images"
            echo "  --push          Push images after building"
            echo ""
            echo "Environment variables:"
            echo "  DOCKER_REGISTRY   Registry (default: docker.io)"
            echo "  DOCKER_NAMESPACE  Namespace (default: infera)"
            echo "  VERSION           Image version tag (default: latest)"
            echo "  WORKER_SGLANG_PACKAGE  SGLang package spec (default: sglang)"
            echo "  WORKER_TENSORRT_LLM_PACKAGE  TensorRT-LLM package spec (default: tensorrt_llm)"
            echo "  WORKER_TENSORRT_LLM_PYPI_EXTRA_INDEX_URL  Extra index for TensorRT-LLM (default: https://pypi.nvidia.com)"
            exit 0
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            exit 1
            ;;
    esac
done

# Default to building all if nothing specified
if ! $BUILD_GATEWAY && ! $BUILD_WORKER && ! $BUILD_WORKER_VLLM && ! $BUILD_WORKER_SGLANG && ! $BUILD_WORKER_TENSORRT_LLM && ! $BUILD_FRONTEND; then
    echo "No images specified, building all..."
    BUILD_GATEWAY=true
    BUILD_WORKER=true
    BUILD_FRONTEND=true
fi

# Navigate to project root
cd "$(dirname "$0")/.."

# Build images
if $BUILD_GATEWAY; then
    build_image "gateway" "deploy/docker/Dockerfile.gateway" "."
fi

if $BUILD_WORKER; then
    build_image "worker" "python/Dockerfile.light" "python"
fi

if $BUILD_WORKER_VLLM; then
    build_image "worker-vllm" "deploy/docker/Dockerfile.worker.vllm" "."
fi

if $BUILD_WORKER_SGLANG; then
    build_image \
        "worker-sglang" \
        "deploy/docker/Dockerfile.worker.sglang" \
        "." \
        --build-arg "SGLANG_PACKAGE=$WORKER_SGLANG_PACKAGE"
fi

if $BUILD_WORKER_TENSORRT_LLM; then
    build_image \
        "worker-tensorrt-llm" \
        "deploy/docker/Dockerfile.worker.tensorrt_llm" \
        "." \
        --build-arg "TENSORRT_LLM_PACKAGE=$WORKER_TENSORRT_LLM_PACKAGE" \
        --build-arg "TENSORRT_LLM_PYPI_EXTRA_INDEX_URL=$WORKER_TENSORRT_LLM_PYPI_EXTRA_INDEX_URL"
fi

if $BUILD_FRONTEND; then
    build_image "frontend" "deploy/docker/Dockerfile.frontend" "."
fi

# Push images if requested
if $PUSH; then
    echo -e "${YELLOW}Pushing images...${NC}"
    
    if $BUILD_GATEWAY; then
        push_image "gateway"
    fi
    
    if $BUILD_WORKER; then
        push_image "worker"
    fi
    
    if $BUILD_WORKER_VLLM; then
        push_image "worker-vllm"
    fi

    if $BUILD_WORKER_SGLANG; then
        push_image "worker-sglang"
    fi

    if $BUILD_WORKER_TENSORRT_LLM; then
        push_image "worker-tensorrt-llm"
    fi
    
    if $BUILD_FRONTEND; then
        push_image "frontend"
    fi
fi

echo -e "${GREEN}Done!${NC}"

# Show image names
echo ""
echo "Built images:"
if $BUILD_GATEWAY; then
    echo "  - $REGISTRY/$NAMESPACE/gateway:$VERSION"
fi
if $BUILD_WORKER; then
    echo "  - $REGISTRY/$NAMESPACE/worker:$VERSION"
fi
if $BUILD_WORKER_VLLM; then
    echo "  - $REGISTRY/$NAMESPACE/worker-vllm:$VERSION"
fi
if $BUILD_WORKER_SGLANG; then
    echo "  - $REGISTRY/$NAMESPACE/worker-sglang:$VERSION"
fi
if $BUILD_WORKER_TENSORRT_LLM; then
    echo "  - $REGISTRY/$NAMESPACE/worker-tensorrt-llm:$VERSION"
fi
if $BUILD_FRONTEND; then
    echo "  - $REGISTRY/$NAMESPACE/frontend:$VERSION"
fi
