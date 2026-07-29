#!/usr/bin/env bash
# Recreate only the frontend from Compose bytes stored in an exact reviewed commit.

set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage: frontend-compose-action.sh <candidate|rollback|restore> \
  --source-revision <full-commit-sha> \
  --image <repository@sha256:digest>

The repository must be clean. Compose is read from the exact source revision,
rendered with the repository project directory and ENV_FILE (default: .env),
and rejected unless frontend is image-only, resolves to the exact digest, and
the pulled image's OCI revision label equals the source revision.
EOF
  exit 2
}

[[ $# -ge 1 ]] || usage
action="$1"
shift

case "${action}" in
  candidate|rollback|restore) ;;
  *)
    echo "ERROR: action must be candidate, rollback, or restore" >&2
    exit 2
    ;;
esac

source_revision=
frontend_image=
while [[ $# -gt 0 ]]; do
  case "$1" in
    --source-revision)
      [[ $# -ge 2 ]] || usage
      source_revision="$2"
      shift 2
      ;;
    --image)
      [[ $# -ge 2 ]] || usage
      frontend_image="$2"
      shift 2
      ;;
    *)
      echo "ERROR: unsupported argument: $1" >&2
      exit 2
      ;;
  esac
done

[[ "${source_revision}" =~ ^[0-9a-f]{40}$ ]] || {
  echo "ERROR: --source-revision must be a full lowercase 40-character commit SHA" >&2
  exit 2
}
[[ -n "${frontend_image}" ]] || {
  echo "ERROR: --image is required" >&2
  exit 2
}
[[ "${frontend_image}" =~ ^[^@[:space:]]+@sha256:[0-9a-f]{64}$ ]] || {
  echo "ERROR: --image must be an immutable repository @sha256 digest" >&2
  exit 2
}

command -v git >/dev/null 2>&1 || {
  echo "ERROR: git is required" >&2
  exit 1
}
command -v docker >/dev/null 2>&1 || {
  echo "ERROR: docker is required" >&2
  exit 1
}
command -v python3 >/dev/null 2>&1 || {
  echo "ERROR: python3 is required" >&2
  exit 1
}

script_dir="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repository_root="$(git -C "${script_dir}/.." rev-parse --show-toplevel)"

if [[ -n "$(git -C "${repository_root}" status --porcelain=v1 --untracked-files=normal)" ]]; then
  echo "ERROR: repository is dirty; reconcile or clean the checkout before a frontend action" >&2
  exit 2
fi
if ! resolved_revision="$(git -C "${repository_root}" rev-parse \
  --verify --end-of-options "${source_revision}^{commit}" 2>/dev/null)" ||
  [[ "${resolved_revision}" != "${source_revision}" ]]; then
  echo "ERROR: source revision is unavailable as an exact commit" >&2
  exit 2
fi
if ! git -C "${repository_root}" cat-file -e \
  "${source_revision}:docker-compose.prod.yml" 2>/dev/null; then
  echo "ERROR: source revision is missing docker-compose.prod.yml" >&2
  exit 2
fi

temporary_parent="${TMPDIR:-/tmp}"
mkdir -p "${temporary_parent}"
temporary_dir="$(mktemp -d "${temporary_parent%/}/infera-frontend-compose.XXXXXX")"
chmod 0700 "${temporary_dir}"
cleanup() {
  rm -rf -- "${temporary_dir}"
}
trap cleanup EXIT HUP INT TERM

compose_file="${temporary_dir}/docker-compose.prod.yml"
rendered_file="${temporary_dir}/rendered.json"
git -C "${repository_root}" cat-file blob \
  "${source_revision}:docker-compose.prod.yml" >"${compose_file}"
chmod 0600 "${compose_file}"

compose_args=(
  --project-directory "${repository_root}"
  -f "${compose_file}"
)
if [[ -n "${ENV_FILE:-}" ]]; then
  env_file="${ENV_FILE}"
  if [[ "${env_file}" != /* ]]; then
    env_file="${repository_root}/${env_file}"
  fi
  compose_args=(--project-directory "${repository_root}" --env-file "${env_file}" -f "${compose_file}")
fi

export INFERA_FRONTEND_IMAGE="${frontend_image}"
if ! docker compose "${compose_args[@]}" config --format json >"${rendered_file}"; then
  echo "ERROR: exact-source production Compose did not render" >&2
  exit 1
fi
chmod 0600 "${rendered_file}"

python3 - "${rendered_file}" "${frontend_image}" <<'PY'
import json
import sys

path, expected_image = sys.argv[1:]
try:
    with open(path, encoding="utf-8") as handle:
        rendered = json.load(handle)
except (OSError, json.JSONDecodeError) as exc:
    raise SystemExit(f"ERROR: rendered Compose is not valid JSON: {exc}")

services = rendered.get("services")
if not isinstance(services, dict):
    raise SystemExit("ERROR: rendered Compose has no services map")
frontend = services.get("frontend")
if not isinstance(frontend, dict):
    raise SystemExit("ERROR: rendered Compose has no frontend service")
if "build" in frontend:
    raise SystemExit("ERROR: rendered frontend must not contain a build field")
if frontend.get("image") != expected_image:
    raise SystemExit("ERROR: rendered frontend image does not equal the requested digest")
PY

if ! docker pull "${frontend_image}" >/dev/null; then
  echo "ERROR: unable to pull the requested immutable frontend digest" >&2
  exit 1
fi
if ! image_revision="$(docker image inspect \
  --format '{{ index .Config.Labels "org.opencontainers.image.revision" }}' \
  "${frontend_image}")" ||
  [[ "${image_revision}" != "${source_revision}" ]]; then
  echo "ERROR: frontend image OCI revision does not equal the requested source revision" >&2
  exit 1
fi

docker compose "${compose_args[@]}" up \
  -d --no-build --no-deps --force-recreate frontend
printf 'Frontend %s completed from reviewed source %s with an immutable image digest.\n' \
  "${action}" "${source_revision}"
