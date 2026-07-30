#!/usr/bin/env bash
# Shared fail-closed access to the single root-owned production environment source.

PRODUCTION_ENV_SOURCE_SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly PRODUCTION_ENV_SOURCE_SCRIPT_DIR

production_env_validate() {
  python3 "${PRODUCTION_ENV_SOURCE_SCRIPT_DIR}/production-env-source.py" validate
}

production_env_value() {
  local name="$1"
  python3 "${PRODUCTION_ENV_SOURCE_SCRIPT_DIR}/production-env-source.py" value "${name}"
}

production_compose() {
  local parser_args=()
  while [[ "${1:-}" == "--override" ]]; do
    [[ "$#" -ge 2 ]] || return 2
    parser_args+=("$1" "$2")
    shift 2
  done
  python3 "${PRODUCTION_ENV_SOURCE_SCRIPT_DIR}/production-env-source.py" \
    compose "${parser_args[@]}" -- "$@"
}
