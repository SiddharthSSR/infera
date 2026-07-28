#!/usr/bin/env bash

set -euo pipefail

usage() {
    cat <<'EOF'
Usage: ./scripts/build-reviewed-release-image.sh \
  --component <gateway|worker-vllm> \
  --revision <full-commit-sha> \
  --tag <image-tag> \
  [--platform linux/amd64]

Build a gateway or vLLM worker release image exclusively from tracked objects
in an explicit full Git commit. No additional build arguments are accepted.
EOF
}

component=
revision=
image_tag=
platform=linux/amd64

while [[ $# -gt 0 ]]; do
    case "$1" in
        --component)
            [[ $# -ge 2 ]] || { echo "error: --component requires a value" >&2; exit 2; }
            component=$2
            shift 2
            ;;
        --revision)
            [[ $# -ge 2 ]] || { echo "error: --revision requires a value" >&2; exit 2; }
            revision=$2
            shift 2
            ;;
        --tag)
            [[ $# -ge 2 ]] || { echo "error: --tag requires a value" >&2; exit 2; }
            image_tag=$2
            shift 2
            ;;
        --platform)
            [[ $# -ge 2 ]] || { echo "error: --platform requires a value" >&2; exit 2; }
            platform=$2
            shift 2
            ;;
        --help)
            usage
            exit 0
            ;;
        *)
            echo "error: unknown or unsafe argument: $1" >&2
            usage >&2
            exit 2
            ;;
    esac
done

case "$component" in
    gateway)
        dockerfile=deploy/docker/Dockerfile.gateway
        required_paths=(
            "$dockerfile"
            go/go.mod
            go/go.sum
            go/cmd/gateway
            go/internal
            go/pkg
        )
        ;;
    worker-vllm)
        dockerfile=deploy/docker/Dockerfile.worker.vllm
        required_paths=(
            "$dockerfile"
            python/pyproject.toml
            python/README.md
            python/requirements/worker-vllm.lock
            python/requirements/worker-vllm.lock.sha256
            python/src/infera_worker
        )
        ;;
    *)
        echo "error: --component must be gateway or worker-vllm" >&2
        exit 2
        ;;
esac

[[ "$platform" == "linux/amd64" ]] || {
    echo "error: reviewed release builds require --platform linux/amd64" >&2
    exit 2
}
[[ "$revision" =~ ^[0-9a-f]{40}$ ]] || {
    echo "error: --revision must be a lowercase full 40-character commit ID" >&2
    exit 2
}
[[ "$image_tag" =~ ^[A-Za-z0-9][A-Za-z0-9._/:@-]*$ ]] || {
    echo "error: --tag contains unsafe characters" >&2
    exit 2
}
[[ "$image_tag" != *@sha256:* ]] || {
    echo "error: --tag must name a writable image tag, not a digest reference" >&2
    exit 2
}

command -v git >/dev/null 2>&1 || { echo "error: git is required" >&2; exit 1; }
command -v docker >/dev/null 2>&1 || { echo "error: docker is required" >&2; exit 1; }

script_dir=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repository_root=$(git -C "$script_dir/.." rev-parse --show-toplevel)

resolved_revision=$(git -C "$repository_root" rev-parse \
    --verify --end-of-options "${revision}^{commit}" 2>/dev/null) || {
    echo "error: revision does not name a commit object: $revision" >&2
    exit 2
}
[[ "$resolved_revision" == "$revision" ]] || {
    echo "error: revision did not resolve to the exact requested commit" >&2
    exit 2
}

for path in "${required_paths[@]}"; do
    git -C "$repository_root" cat-file -e "${revision}:${path}" 2>/dev/null || {
        echo "error: reviewed revision is missing required tracked path: $path" >&2
        exit 2
    }
done

context_parent=${TMPDIR:-/tmp}
mkdir -p "$context_parent"
build_context=$(mktemp -d "${context_parent%/}/infera-${component}-context.XXXXXX")
cleanup() {
    rm -rf -- "$build_context"
}
trap cleanup EXIT HUP INT TERM

git -C "$repository_root" archive --format=tar "$revision" -- "${required_paths[@]}" |
    tar -xf - -C "$build_context"

iid_file=$(mktemp "${context_parent%/}/infera-${component}-iid.XXXXXX")
rm -f "$iid_file"
cleanup_with_iid() {
    rm -rf -- "$build_context"
    rm -f -- "$iid_file"
}
trap cleanup_with_iid EXIT HUP INT TERM

context_file_count=$(find "$build_context" -type f | wc -l | tr -d '[:space:]')
context_size_kib=$(du -sk "$build_context" | awk '{print $1}')
echo "COMPONENT=$component"
echo "SOURCE_REVISION=$revision"
echo "CONTEXT_FILES=$context_file_count"
echo "CONTEXT_SIZE_KIB=$context_size_kib"

docker build \
    --pull \
    --no-cache \
    --platform "$platform" \
    --iidfile "$iid_file" \
    --build-arg "SOURCE_REVISION=$revision" \
    --file "$build_context/$dockerfile" \
    --tag "$image_tag" \
    "$build_context"

image_id=$(<"$iid_file")
image_revision=$(docker image inspect \
    --format '{{ index .Config.Labels "org.opencontainers.image.revision" }}' \
    "$image_tag")
[[ "$image_revision" == "$revision" ]] || {
    echo "error: built image revision label mismatch: expected $revision, got $image_revision" >&2
    exit 1
}

echo "IMAGE_TAG=$image_tag"
echo "IMAGE_ID=$image_id"
echo "IMAGE_SOURCE_REVISION=$image_revision"
