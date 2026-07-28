#!/usr/bin/env bash

set -euo pipefail

usage() {
    cat <<'EOF'
Usage: ./scripts/build-reviewed-frontend.sh --revision <git-revision> --tag <image-tag> [options]

Build the frontend from tracked files in an explicit Git commit. The mutable
working tree is never used as the Docker build context.

Options:
  --revision <revision>  Required commit, tag, or other revision resolving to a commit
  --tag <image-tag>      Required local image tag
  --platform <platform>  Docker target platform; must be linux/amd64
  --build-arg <value>    Additional Docker build argument; may be repeated
  --help                 Show this help
EOF
}

revision=
image_tag=
platform=linux/amd64
build_args=()
build_arg_count=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --revision)
            [[ $# -ge 2 ]] || {
                echo "error: --revision requires a value" >&2
                exit 2
            }
            revision=$2
            shift 2
            ;;
        --tag)
            [[ $# -ge 2 ]] || {
                echo "error: --tag requires a value" >&2
                exit 2
            }
            image_tag=$2
            shift 2
            ;;
        --platform)
            [[ $# -ge 2 ]] || {
                echo "error: --platform requires a value" >&2
                exit 2
            }
            [[ "$2" == "linux/amd64" ]] || {
                echo "error: reviewed frontend builds require --platform linux/amd64" >&2
                exit 2
            }
            platform=$2
            shift 2
            ;;
        --build-arg)
            [[ $# -ge 2 ]] || {
                echo "error: --build-arg requires a value" >&2
                exit 2
            }
            build_args+=(--build-arg "$2")
            build_arg_count=$((build_arg_count + 1))
            shift 2
            ;;
        --help)
            usage
            exit 0
            ;;
        *)
            echo "error: unknown argument: $1" >&2
            usage >&2
            exit 2
            ;;
    esac
done

[[ -n "$revision" ]] || {
    echo "error: --revision is required" >&2
    exit 2
}
[[ -n "$image_tag" ]] || {
    echo "error: --tag is required" >&2
    exit 2
}
[[ -n "$platform" ]] || {
    echo "error: --platform must not be empty" >&2
    exit 2
}

command -v git >/dev/null 2>&1 || {
    echo "error: git is required" >&2
    exit 1
}
command -v docker >/dev/null 2>&1 || {
    echo "error: docker is required" >&2
    exit 1
}

script_dir=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repository_root=$(git -C "$script_dir/.." rev-parse --show-toplevel)

if ! source_revision=$(git -C "$repository_root" rev-parse \
    --verify --end-of-options "${revision}^{commit}" 2>/dev/null); then
    echo "error: revision does not resolve to a commit: $revision" >&2
    exit 2
fi

if [[ ! "$source_revision" =~ ^[0-9a-f]{40}$ ]]; then
    echo "error: resolved revision is not a full SHA-1 commit ID: $source_revision" >&2
    exit 2
fi

required_paths=(
    .dockerignore
    frontend
    deploy/docker/Dockerfile.frontend
    deploy/docker/nginx.conf
)
for path in "${required_paths[@]}"; do
    if ! git -C "$repository_root" cat-file -e "${source_revision}:${path}" 2>/dev/null; then
        echo "error: reviewed revision is missing required path: $path" >&2
        exit 2
    fi
done

context_parent=${TMPDIR:-/tmp}
mkdir -p "$context_parent"
build_context=$(mktemp -d "${context_parent%/}/infera-frontend-context.XXXXXX")
cleanup() {
    rm -rf -- "$build_context"
}
trap cleanup EXIT HUP INT TERM

git -C "$repository_root" archive --format=tar "$source_revision" -- "${required_paths[@]}" |
    tar -xf - -C "$build_context"

context_file_count=$(find "$build_context" -type f | wc -l | tr -d '[:space:]')
context_size_kib=$(du -sk "$build_context" | awk '{print $1}')
iid_file="$build_context/.frontend-image-id"

echo "SOURCE_REVISION=$source_revision"
echo "CONTEXT_FILES=$context_file_count"
echo "CONTEXT_SIZE_KIB=$context_size_kib"

docker_command=(
    docker build
    --pull
    --no-cache
    --platform "$platform"
    --iidfile "$iid_file"
    --build-arg "SOURCE_REVISION=$source_revision"
)
if [[ $build_arg_count -gt 0 ]]; then
    docker_command+=("${build_args[@]}")
fi
docker_command+=(
    --file "$build_context/deploy/docker/Dockerfile.frontend"
    --tag "$image_tag"
    "$build_context"
)
"${docker_command[@]}"

image_id=$(<"$iid_file")
image_revision=$(docker image inspect \
    --format '{{ index .Config.Labels "org.opencontainers.image.revision" }}' \
    "$image_tag")

if [[ "$image_revision" != "$source_revision" ]]; then
    echo "error: built image revision label mismatch: expected $source_revision, got $image_revision" >&2
    exit 1
fi

echo "IMAGE_TAG=$image_tag"
echo "IMAGE_ID=$image_id"
echo "IMAGE_SOURCE_REVISION=$image_revision"
