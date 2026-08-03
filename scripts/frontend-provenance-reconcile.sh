#!/usr/bin/env bash
# Reconcile only the frontend Compose provenance label without changing its image.

set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage: frontend-provenance-reconcile.sh \
  --expected-head <full-commit-sha> \
  --source-revision <full-commit-sha> \
  --image <repository@sha256:digest> \
  --production-env-file <absolute-path> \
  [--base-url <https-url>] [--health-attempts <count>]

This is a dedicated, frontend-only provenance repair. It validates the direct
root-only production environment source, proves the checked-in Compose bytes
equal the frontend image's exact source revision, stages a second healthy
frontend without recreating the original, and removes the original only after
an isolated public-route cutover succeeds. Public root bytes must remain exact;
public health JSON may differ only in its non-negative integer uptime_seconds.
EOF
  exit 2
}

expected_head=
source_revision=
frontend_image=
production_env_file=
base_url="${INFERA_BASE_URL:-https://inferai.co.in}"
health_attempts=30
while [[ $# -gt 0 ]]; do
  case "$1" in
    --expected-head)
      [[ $# -ge 2 ]] || usage
      expected_head="$2"
      shift 2
      ;;
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
    --production-env-file)
      [[ $# -ge 2 ]] || usage
      production_env_file="$2"
      shift 2
      ;;
    --base-url)
      [[ $# -ge 2 ]] || usage
      base_url="${2%/}"
      shift 2
      ;;
    --health-attempts)
      [[ $# -ge 2 ]] || usage
      health_attempts="$2"
      shift 2
      ;;
    *) usage ;;
  esac
done

[[ "${expected_head}" =~ ^[0-9a-f]{40}$ ]] || usage
[[ "${source_revision}" =~ ^[0-9a-f]{40}$ ]] || usage
[[ "${frontend_image}" =~ ^[^@[:space:]]+@sha256:[0-9a-f]{64}$ ]] || usage
[[ "${production_env_file}" == /* && "${production_env_file}" != *$'\n'* ]] || usage
[[ "${base_url}" =~ ^https://[A-Za-z0-9.-]+(:[0-9]+)?$ ]] || usage
[[ "${health_attempts}" =~ ^[1-9][0-9]*$ ]] || usage
[[ "${EUID}" -eq 0 ]] || {
  echo "ERROR: frontend provenance reconciliation must run as root" >&2
  exit 2
}

for executable in git docker curl python3 cmp mktemp date; do
  command -v "${executable}" >/dev/null 2>&1 || {
    echo "ERROR: a required reconciliation executable is unavailable" >&2
    exit 1
  }
done

script_dir="$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repository_root="$(git -C "${script_dir}/.." rev-parse --show-toplevel)"
compose_file="${repository_root}/docker-compose.prod.yml"
recovery_state_dir="${repository_root}/.infera-recovery"
recovery_lock_dir="${recovery_state_dir}/recovery.lock"
recovery_manifest="${recovery_state_dir}/last-known-good.manifest"
recovery_policy="${repository_root}/deploy/production/infera-production-recovery-policy"
# shellcheck source=production-env-source.sh
# shellcheck disable=SC1091
source "${script_dir}/production-env-source.sh"

[[ -z "$(git -C "${repository_root}" status --porcelain=v1 --untracked-files=normal)" ]] || {
  echo "ERROR: repository is dirty" >&2
  exit 2
}
head_revision="$(git -C "${repository_root}" rev-parse HEAD)"
main_revision="$(git -C "${repository_root}" rev-parse origin/main)"
[[ "${head_revision}" == "${expected_head}" && "${main_revision}" == "${expected_head}" ]] || {
  echo "ERROR: checkout is not the approved exact main revision" >&2
  exit 2
}
[[ -f "${compose_file}" && ! -L "${compose_file}" ]] || {
  echo "ERROR: checked-in production Compose path is unsafe" >&2
  exit 2
}
[[ "$(python3 - "${compose_file}" <<'PY'
import os, stat, sys
path = sys.argv[1]
info = os.lstat(path)
print(str(
    stat.S_ISREG(info.st_mode)
    and info.st_uid == 0
    and info.st_nlink == 1
    and not stat.S_IMODE(info.st_mode) & 0o022
    and os.path.realpath(path) == path
).lower())
PY
)" == true ]] || {
  echo "ERROR: checked-in production Compose metadata is unsafe" >&2
  exit 2
}
resolved_source="$(git -C "${repository_root}" rev-parse --verify --end-of-options "${source_revision}^{commit}" 2>/dev/null || true)"
[[ "${resolved_source}" == "${source_revision}" ]] || {
  echo "ERROR: frontend source revision is unavailable" >&2
  exit 2
}
if ! git -C "${repository_root}" cat-file blob \
  "${source_revision}:docker-compose.prod.yml" | cmp -s - "${compose_file}"; then
  echo "ERROR: checked-in Compose bytes differ from the frontend source revision" >&2
  exit 2
fi

export INFERA_PRODUCTION_ENV_FILE="${production_env_file}"
production_env_validate >/dev/null || {
  echo "ERROR: direct production environment source validation failed" >&2
  exit 2
}
[[ "$(production_env_value INFERA_FRONTEND_IMAGE)" == "${frontend_image}" ]] || {
  echo "ERROR: production environment frontend identity differs" >&2
  exit 2
}
[[ "$(python3 - "${recovery_state_dir}" <<'PY'
import os, stat, sys
path = sys.argv[1]
try:
    info = os.lstat(path)
except OSError:
    print("false")
else:
    print(str(
        stat.S_ISDIR(info.st_mode)
        and info.st_uid == 0
        and not stat.S_IMODE(info.st_mode) & 0o022
        and os.path.realpath(path) == path
    ).lower())
PY
)" == true ]] || {
  echo "ERROR: production recovery state directory is unsafe" >&2
  exit 2
}
if ! python3 "${script_dir}/production-recovery-verifier.py" verify-environment \
  --manifest "${recovery_manifest}" \
  --policy "${recovery_policy}" \
  --production-env "${production_env_file}" >/dev/null; then
  echo "ERROR: production environment semantic contract differs" >&2
  exit 2
fi

private_dir="$(mktemp -d "${TMPDIR:-/tmp}/infera-frontend-provenance.XXXXXX")"
chmod 0700 "${private_dir}"
compose_log="${private_dir}/compose.log"
baseline_root="${private_dir}/baseline-root"
baseline_health="${private_dir}/baseline-health"
: >"${compose_log}"
: >"${baseline_root}"
: >"${baseline_health}"
chmod 0600 "${compose_log}" "${baseline_root}" "${baseline_health}"

original_id=
replacement_id=
replacement_ids=()
committed=false
replacement_ownership_ambiguous=false
lock_held=false
mutation_started=false

quiet_docker() {
  docker "$@" >>"${compose_log}" 2>&1
}

container_value() {
  local format="$1" container_id="$2"
  docker inspect --format "${format}" "${container_id}" 2>>"${compose_log}"
}

frontend_ids() {
  production_compose --override INFERA_FRONTEND_IMAGE \
    --project-directory "${repository_root}" -f "${compose_file}" \
    ps -aq frontend 2>>"${compose_log}"
}

read_frontend_ids() {
  local container_id output
  FRONTEND_IDS=()
  output="$(frontend_ids)" || return 1
  while IFS= read -r container_id; do
    [[ -n "${container_id}" ]] && FRONTEND_IDS+=("${container_id}")
  done <<<"${output}"
  return 0
}

wait_healthy() {
  local container_id="$1" attempt status
  for ((attempt = 1; attempt <= health_attempts; attempt++)); do
    status="$(container_value '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "${container_id}" || true)"
    [[ "${status}" == healthy ]] && return 0
    [[ "${status}" == exited || "${status}" == unhealthy ]] && return 1
    sleep 2
  done
  return 1
}

probe() {
  local suffix="$1" destination="$2"
  curl --fail --silent --show-error --location --max-redirs 2 \
    --proto '=https' --proto-redir '=https' --max-time 10 \
    --output "${destination}" "${base_url}${suffix}" 2>>"${compose_log}"
}

health_matches_baseline() {
  local baseline="$1" candidate="$2"
  python3 - "${baseline}" "${candidate}" <<'PY'
import json
import sys


def stable_health(path):
    def unique_object(pairs):
        payload = {}
        for key, value in pairs:
            if key in payload:
                raise ValueError("duplicate health key")
            payload[key] = value
        return payload

    def reject_nonstandard_constant(_value):
        raise ValueError("nonstandard JSON constant")

    try:
        with open(path, encoding="utf-8") as source:
            payload = json.load(
                source,
                object_pairs_hook=unique_object,
                parse_constant=reject_nonstandard_constant,
            )
    except (OSError, UnicodeError, ValueError):
        raise SystemExit(1)
    if not isinstance(payload, dict):
        raise SystemExit(1)
    uptime = payload.pop("uptime_seconds", None)
    if type(uptime) is not int or uptime < 0:
        raise SystemExit(1)
    return payload


raise SystemExit(0 if stable_health(sys.argv[1]) == stable_health(sys.argv[2]) else 1)
PY
}

routes_match_baseline() {
  local candidate_root="${private_dir}/candidate-root"
  local candidate_health="${private_dir}/candidate-health"
  : >"${candidate_root}"
  : >"${candidate_health}"
  chmod 0600 "${candidate_root}" "${candidate_health}"
  probe / "${candidate_root}" && probe /health "${candidate_health}" &&
    cmp -s "${baseline_root}" "${candidate_root}" &&
    health_matches_baseline "${baseline_health}" "${candidate_health}"
}

verify_identity() {
  local container_id="$1"
  [[ "$(container_value '{{.Config.Image}}' "${container_id}")" == "${frontend_image}" ]] &&
    [[ "$(container_value '{{.Image}}' "${container_id}")" == "${expected_image_id}" ]] &&
    [[ "$(container_value '{{.RestartCount}}' "${container_id}")" == 0 ]] &&
    [[ "$(container_value '{{index .Config.Labels "com.docker.compose.project.config_files"}}' "${container_id}")" == "${compose_file}" ]] &&
    [[ "$(container_value '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "${container_id}")" == "${repository_root}" ]] &&
    [[ "$(container_value '{{index .Config.Labels "com.docker.compose.service"}}' "${container_id}")" == frontend ]] &&
    wait_healthy "${container_id}"
}

discover_owned_replacement() {
  local candidate_id
  read_frontend_ids || return 1
  replacement_ids=()
  for candidate_id in "${FRONTEND_IDS[@]}"; do
    [[ "${candidate_id}" == "${original_id}" ]] || replacement_ids+=("${candidate_id}")
  done
  if [[ "${#replacement_ids[@]}" -gt 1 ]]; then
    replacement_ownership_ambiguous=true
    replacement_ids=()
    return 1
  fi
}

release_lock() {
  [[ "${lock_held}" == true ]] || return 0
  rm -f -- "${recovery_lock_dir}/owner" >/dev/null 2>&1 || return 1
  rmdir -- "${recovery_lock_dir}" >/dev/null 2>&1 || return 1
  lock_held=false
}

rollback_uncommitted() {
  local restored=false candidate_id
  [[ -n "${original_id}" ]] || return 0
  if wait_healthy "${original_id}"; then
    restored=true
  elif quiet_docker start "${original_id}" && wait_healthy "${original_id}"; then
    restored=true
  fi
  [[ "${restored}" == true ]] || return 1
  if [[ "${replacement_ownership_ambiguous}" == true ]]; then
    return 1
  fi
  if [[ "${#replacement_ids[@]}" -eq 0 ]]; then
    discover_owned_replacement || return 1
  fi
  for candidate_id in "${replacement_ids[@]}"; do
    [[ -n "${candidate_id}" ]] && quiet_docker rm -f "${candidate_id}" || return 1
  done
  routes_match_baseline
}

cleanup() {
  local status=$? cleanup_failed=false
  trap - EXIT HUP INT TERM
  if [[ "${status}" -ne 0 && "${committed}" == false && "${mutation_started}" == true ]]; then
    if ! rollback_uncommitted; then
      echo "ERROR: automatic frontend rollback could not be proven complete" >&2
      cleanup_failed=true
    fi
  fi
  if ! rm -rf -- "${private_dir}"; then
    echo "ERROR: private reconciliation cleanup failed" >&2
    cleanup_failed=true
  fi
  if ! release_lock; then
    echo "ERROR: recovery lock cleanup failed" >&2
    cleanup_failed=true
  fi
  if [[ "${cleanup_failed}" == true && "${status}" -eq 0 ]]; then
    status=1
  fi
  exit "${status}"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

if ! mkdir -m 0700 -- "${recovery_lock_dir}" 2>/dev/null; then
  echo "ERROR: another production recovery action holds the shared lock" >&2
  exit 1
fi
lock_held=true
printf 'controller_pid=%s started_at=%s\n' "$$" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  >"${recovery_lock_dir}/owner"
chmod 0600 "${recovery_lock_dir}/owner"

export INFERA_FRONTEND_IMAGE="${frontend_image}"
if ! production_compose --override INFERA_FRONTEND_IMAGE \
  --project-directory "${repository_root}" -f "${compose_file}" \
  config --format json 2>>"${compose_log}" | python3 -c '
import json
import sys

expected_image = sys.argv[1]
try:
    services = json.load(sys.stdin).get("services")
except (AttributeError, json.JSONDecodeError):
    raise SystemExit(1)
frontend = services.get("frontend") if isinstance(services, dict) else None
if not isinstance(frontend, dict) or "build" in frontend or frontend.get("image") != expected_image:
    raise SystemExit(1)
if any(frontend.get(name) for name in ("container_name", "ports", "volumes", "configs", "secrets")):
    raise SystemExit(1)
' "${frontend_image}" 2>>"${compose_log}"; then
  echo "ERROR: exact-main production Compose validation failed" >&2
  exit 1
fi

expected_image_id="$(docker image inspect --format '{{.Id}}' "${frontend_image}" 2>>"${compose_log}" || true)"
[[ "${expected_image_id}" =~ ^sha256:[0-9a-f]{64}$ ]] || {
  echo "ERROR: immutable frontend image is unavailable" >&2
  exit 1
}
image_revision="$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "${expected_image_id}" 2>>"${compose_log}" || true)"
[[ "${image_revision}" == "${source_revision}" ]] || {
  echo "ERROR: frontend image revision differs from the approved source" >&2
  exit 1
}

read_frontend_ids
initial_ids=("${FRONTEND_IDS[@]}")
[[ "${#initial_ids[@]}" -eq 1 && -n "${initial_ids[0]}" ]] || {
  echo "ERROR: frontend cardinality is not exactly one" >&2
  exit 1
}
original_id="${initial_ids[0]}"
original_image="$(container_value '{{.Config.Image}}' "${original_id}")"
original_image_id="$(container_value '{{.Image}}' "${original_id}")"
original_restart="$(container_value '{{.RestartCount}}' "${original_id}")"
original_started="$(container_value '{{.State.StartedAt}}' "${original_id}")"
[[ "${original_image}" == "${frontend_image}" && "${original_image_id}" == "${expected_image_id}" && "${original_restart}" == 0 ]] || {
  echo "ERROR: current frontend identity or restart baseline differs" >&2
  exit 1
}
wait_healthy "${original_id}" || {
  echo "ERROR: current frontend is not healthy" >&2
  exit 1
}
if ! probe / "${baseline_root}" || ! probe /health "${baseline_health}" ||
  ! health_matches_baseline "${baseline_health}" "${baseline_health}"; then
  echo "ERROR: public route baseline failed" >&2
  exit 1
fi

current_label="$(container_value '{{index .Config.Labels "com.docker.compose.project.config_files"}}' "${original_id}")"
if [[ "${current_label}" == "${compose_file}" ]]; then
  if ! verify_identity "${original_id}" || ! routes_match_baseline; then
    echo "ERROR: existing exact-provenance frontend failed verification" >&2
    exit 1
  fi
  echo "Frontend provenance already reconciled; verification passed."
  exit 0
fi

mutation_started=true
if ! production_compose --override INFERA_FRONTEND_IMAGE \
  --project-directory "${repository_root}" -f "${compose_file}" up \
  -d --no-build --no-deps --no-recreate --scale frontend=2 frontend \
  >>"${compose_log}" 2>&1; then
  discover_owned_replacement || true
  echo "ERROR: replacement frontend staging failed" >&2
  exit 1
fi
read_frontend_ids || {
  echo "ERROR: replacement frontend discovery failed" >&2
  exit 1
}
staged_ids=("${FRONTEND_IDS[@]}")
new_ids=()
for candidate_id in "${staged_ids[@]}"; do
  [[ "${candidate_id}" == "${original_id}" ]] || new_ids+=("${candidate_id}")
done
if [[ "${#staged_ids[@]}" -ne 2 || "${#new_ids[@]}" -ne 1 ]]; then
  if [[ "${#new_ids[@]}" -gt 1 ]]; then
    replacement_ownership_ambiguous=true
  else
    replacement_ids=("${new_ids[@]}")
  fi
  echo "ERROR: replacement staging cardinality is invalid" >&2
  exit 1
fi
replacement_ids=("${new_ids[0]}")
replacement_id="${replacement_ids[0]}"
[[ -n "${replacement_id}" ]] || {
  echo "ERROR: replacement frontend identity is ambiguous" >&2
  exit 1
}
if [[ "$(container_value '{{.RestartCount}}' "${original_id}")" != "${original_restart}" ]] ||
  [[ "$(container_value '{{.State.StartedAt}}' "${original_id}")" != "${original_started}" ]] ||
  ! wait_healthy "${original_id}"; then
    echo "ERROR: original frontend changed during staging" >&2
    exit 1
fi
if ! verify_identity "${replacement_id}" || ! routes_match_baseline; then
  echo "ERROR: staged frontend verification failed" >&2
  exit 1
fi

if ! quiet_docker stop "${original_id}"; then
  echo "ERROR: unable to isolate the original frontend" >&2
  exit 1
fi
if ! verify_identity "${replacement_id}" || ! routes_match_baseline; then
  echo "ERROR: replacement-only public cutover failed" >&2
  exit 1
fi

quiet_docker rm "${original_id}" || {
  echo "ERROR: unable to retire the original frontend" >&2
  exit 1
}
committed=true

read_frontend_ids || {
  echo "ERROR: final frontend discovery failed" >&2
  exit 1
}
final_ids=("${FRONTEND_IDS[@]}")
if [[ "${#final_ids[@]}" -ne 1 || "${final_ids[0]}" != "${replacement_id}" ]] ||
  ! verify_identity "${replacement_id}" || ! routes_match_baseline; then
    echo "ERROR: final frontend provenance verification failed" >&2
    exit 1
fi

echo "Frontend provenance reconciliation passed with one healthy immutable frontend."
