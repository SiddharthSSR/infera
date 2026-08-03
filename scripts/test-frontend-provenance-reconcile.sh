#!/usr/bin/env bash
# shellcheck disable=SC1090

set -euo pipefail

if [[ "$(id -u)" -ne 0 ]]; then
  exec sudo -n bash "$0"
fi

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

root_test_base="$(python3 -c 'import os, pwd; print(os.path.realpath(pwd.getpwuid(0).pw_dir))')"
test_root="$(mktemp -d "${root_test_base}/.infera-frontend-provenance-test.XXXXXX")"
cleanup() {
  rm -rf -- "${test_root}"
}
trap cleanup EXIT HUP INT TERM

fixture="${test_root}/repository"
fake_bin="${test_root}/bin"
state="${test_root}/state"
calls="${test_root}/calls"
private_tmp="${test_root}/tmp"
original=
replacement=
intruder=
mkdir -p "${fixture}/scripts" "${fixture}/deploy/production" "${fake_bin}" "${private_tmp}"
cp scripts/frontend-provenance-reconcile.sh scripts/production-env-source.py \
  scripts/production-env-source.sh scripts/production-recovery-verifier.py \
  "${fixture}/scripts/"
chmod +x "${fixture}/scripts/"*
cp deploy/production/infera-production-recovery-policy "${fixture}/deploy/production/"

image="ghcr.io/example/frontend@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
image_id="sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
python3 scripts/write-production-env-test-fixture.py "${test_root}/production.env"

git -C "${fixture}" init -q
git -C "${fixture}" config user.email provenance-test@example.invalid
git -C "${fixture}" config user.name "Frontend provenance test"
cat >"${fixture}/docker-compose.prod.yml" <<'EOF'
services:
  frontend:
    image: ${INFERA_FRONTEND_IMAGE:?INFERA_FRONTEND_IMAGE is required}
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://127.0.0.1:3000/"]
EOF
printf '%s\n' '__pycache__/' >"${fixture}/.gitignore"
git -C "${fixture}" add .
git -C "${fixture}" commit -qm "exact frontend compose"
source_revision="$(git -C "${fixture}" rev-parse HEAD)"
git -C "${fixture}" update-ref refs/remotes/origin/main "${source_revision}"
printf '%s\n' '.infera-recovery/' >>"${fixture}/.git/info/exclude"
mkdir -m 0700 "${fixture}/.infera-recovery"
cat >"${fixture}/.infera-recovery/last-known-good.manifest" <<'EOF'
INFERA_RELEASE_ID=synthetic
INFERA_GATEWAY_IMAGE=ghcr.io/example/gateway@sha256:aaa
INFERA_WORKER_IMAGE=ghcr.io/example/worker@sha256:bbb
INFERA_WORKER_PROTOCOL_VERSION=synthetic-worker
INFERA_RECOVERY_API_PROTOCOL_VERSION=test-infera_recovery_api_protocol_version
INFERA_AUDIT_LEDGER_WRITER_PROTOCOL=synthetic-ledger
EOF
chmod 0600 "${fixture}/.infera-recovery/last-known-good.manifest"

cat >"${fake_bin}/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

printf '%s\n' "$*" >>"${TEST_CALLS}"
read_state() {
  # shellcheck disable=SC1090
  source "${TEST_STATE}"
}
write_state() {
  local staged="${TEST_STATE}.$$"
  printf 'original=%q\nreplacement=%q\nintruder=%q\n' \
    "${original}" "${replacement}" "${intruder}" >"${staged}" || {
      rm -f -- "${staged}"
      return 1
    }
  mv -f -- "${staged}" "${TEST_STATE}" || {
    rm -f -- "${staged}"
    return 1
  }
}
read_state

if [[ "${1:-} ${2:-}" == "image inspect" ]]; then
  if [[ "$*" == *"{{.Id}}"* ]]; then
    printf '%s\n' "${TEST_IMAGE_ID}"
  else
    printf '%s\n' "${TEST_IMAGE_REVISION}"
  fi
  exit 0
fi

if [[ "${1:-}" == inspect ]]; then
  format=
  container=
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --format) format="$2"; shift 2 ;;
      inspect) shift ;;
      *) container="$1"; shift ;;
    esac
  done
  current=
  [[ "${container}" == original-container ]] && current="${original}"
  [[ "${container}" == replacement-container ]] && current="${replacement}"
  [[ "${container}" == intruder-container ]] && current="${intruder}"
  [[ -n "${current}" && "${current}" != absent ]] || exit 1
  case "${format}" in
    *State.Health*)
      if [[ "${container}" == replacement-container && "${TEST_REPLACEMENT_HEALTH:-healthy}" != healthy ]]; then
        printf '%s\n' "${TEST_REPLACEMENT_HEALTH}"
      elif [[ "${current}" == stopped ]]; then
        printf '%s\n' exited
      else
        printf '%s\n' healthy
      fi
      ;;
    *State.Status*)
      [[ "${current}" == stopped ]] && printf '%s\n' exited || printf '%s\n' running
      ;;
    *Config.Image*) printf '%s\n' "${TEST_IMAGE}" ;;
    *'.Image}}'*) printf '%s\n' "${TEST_IMAGE_ID}" ;;
    *RestartCount*) printf '%s\n' 0 ;;
    *StartedAt*) printf '%s\n' 2026-08-01T00:00:00Z ;;
    *project.config_files*)
      if [[ "${container}" == original-container ]]; then
        printf '%s\n' /private/stale/compose.yml
      else
        printf '%s\n' "${TEST_COMPOSE_FILE}"
      fi
      ;;
    *project.working_dir*) printf '%s\n' "${TEST_REPOSITORY}" ;;
    *compose.service*) printf '%s\n' frontend ;;
    *) exit 1 ;;
  esac
  exit 0
fi

case "${1:-}" in
  stop)
    [[ "$2" == original-container ]]
    if [[ "${TEST_FAIL_STOP:-0}" == 1 ]]; then
      if [[ "${TEST_STOPPED_DESPITE_FAILURE:-0}" == 1 ]]; then
        original=stopped
        write_state
      fi
      exit 1
    fi
    original=stopped
    write_state
    exit 0
    ;;
  start)
    [[ "$2" == original-container ]]
    [[ "${TEST_FAIL_START:-0}" != 1 ]] || exit 1
    original=running
    write_state
    exit 0
    ;;
  rm)
    target="${!#}"
    case "${target}" in
      original-container) original=absent ;;
      replacement-container) replacement=absent ;;
      intruder-container) intruder=absent ;;
      *) exit 1 ;;
    esac
    write_state
    exit 0
    ;;
esac

[[ "${1:-}" == compose ]] || exit 1
shift
action=
while [[ $# -gt 0 ]]; do
  case "$1" in
    config|up|ps) action="$1"; shift; break ;;
    *) shift ;;
  esac
done
case "${action}" in
  config)
    printf '%s\n' '{"services":{"frontend":{"image":"'"${TEST_IMAGE}"'"}}}'
    ;;
  ps)
    [[ "${original}" != absent ]] && printf '%s\n' original-container
    [[ "${replacement}" != absent ]] && printf '%s\n' replacement-container
    [[ "${intruder}" != absent ]] && printf '%s\n' intruder-container
    true
    ;;
  up)
    if [[ "${TEST_NO_REPLACEMENT:-0}" == 1 ]]; then
      write_state
      exit 0
    fi
    replacement=running
    write_state
    [[ "${TEST_FAIL_UP:-0}" != 1 ]] || exit 1
    [[ "${TEST_STAGE_RACE:-0}" != 1 ]] || intruder=running
    write_state
    ;;
  *) exit 1 ;;
esac
EOF

cat >"${fake_bin}/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
destination=
url=
while [[ $# -gt 0 ]]; do
  case "$1" in
    --output) destination="$2"; shift 2 ;;
    http*) url="$1"; shift ;;
    *) shift ;;
  esac
done
# shellcheck disable=SC1090
source "${TEST_STATE}"
if [[ -z "${destination}" || -z "${url}" ]]; then
  echo "fake curl: destination and URL are required" >&2
  exit 64
fi
printf 'curl %s\n' "${url}" >>"${TEST_CALLS}"
if [[ "${TEST_CUTOVER_ROUTE_FAIL:-0}" == 1 && "${original}" == stopped ]]; then
  exit 22
fi
if [[ "${TEST_STAGE_ROUTE_FAIL:-0}" == 1 && "${replacement}" != absent ]]; then
  exit 22
fi
case "${url}" in
  */health)
    health_status=safe
    if [[ "${TEST_HEALTH_STATUS_DRIFT:-0}" == 1 && "${replacement}" != absent ]]; then
      health_status=changed
    fi
    uptime_seconds_json=1
    if [[ "${TEST_INVALID_HEALTH_UPTIME:-0}" == 1 ]]; then
      uptime_seconds_json='"unknown"'
    elif [[ "${TEST_DYNAMIC_HEALTH_UPTIME:-0}" == 1 ]]; then
      uptime_seconds_json="$(grep -c 'curl .*/health$' "${TEST_CALLS}" || true)"
    fi
    printf '{"status":"%s","uptime_seconds":%s}' \
      "${health_status}" "${uptime_seconds_json}" >"${destination}"
    ;;
  *) printf '%s' '<html>safe frontend</html>' >"${destination}" ;;
esac
EOF
chmod +x "${fake_bin}/docker" "${fake_bin}/curl"

reset_state() {
  printf '%s\n' 'original=running' 'replacement=absent' 'intruder=absent' >"${state}"
  : >"${calls}"
  find "${private_tmp:?}" -mindepth 1 -delete
}

replace_env_value() {
  python3 - "$1" "$2" "$3" <<'PY'
import sys
from pathlib import Path

path, name, value = Path(sys.argv[1]), sys.argv[2], sys.argv[3]
lines = path.read_text(encoding="utf-8").splitlines()
matches = [index for index, line in enumerate(lines) if line.startswith(name + "=")]
if len(matches) != 1:
    raise SystemExit(1)
lines[matches[0]] = f"{name}={value}"
path.write_text("\n".join(lines) + "\n", encoding="utf-8")
path.chmod(0o600)
PY
}

run_reconcile() {
  local reconcile_args=(
    --expected-head "${source_revision}"
    --source-revision "${source_revision}"
    --image "${image}"
    --production-env-file "${test_root}/production.env"
    --health-attempts "${TEST_HEALTH_ATTEMPTS:-1}"
  )
  if [[ "${TEST_OMIT_BASE_URL:-0}" != 1 ]]; then
    reconcile_args+=(--base-url https://example.invalid)
  fi
  if [[ "${TEST_EXEC_RECONCILE:-0}" == 1 ]]; then
    exec env PATH="${fake_bin}:${PATH}" TMPDIR="${private_tmp}" \
      TEST_STATE="${state}" TEST_CALLS="${calls}" TEST_IMAGE="${image}" \
      TEST_IMAGE_ID="${image_id}" TEST_IMAGE_REVISION="${TEST_IMAGE_REVISION:-${source_revision}}" \
      TEST_COMPOSE_FILE="${fixture}/docker-compose.prod.yml" TEST_REPOSITORY="${fixture}" \
      TEST_REPLACEMENT_HEALTH="${TEST_REPLACEMENT_HEALTH:-healthy}" \
      "${fixture}/scripts/frontend-provenance-reconcile.sh" "${reconcile_args[@]}"
  fi
  PATH="${fake_bin}:${PATH}" TMPDIR="${private_tmp}" \
    TEST_STATE="${state}" TEST_CALLS="${calls}" TEST_IMAGE="${image}" \
    TEST_IMAGE_ID="${image_id}" TEST_IMAGE_REVISION="${TEST_IMAGE_REVISION:-${source_revision}}" \
    TEST_COMPOSE_FILE="${fixture}/docker-compose.prod.yml" TEST_REPOSITORY="${fixture}" \
    TEST_REPLACEMENT_HEALTH="${TEST_REPLACEMENT_HEALTH:-healthy}" \
    "${fixture}/scripts/frontend-provenance-reconcile.sh" "${reconcile_args[@]}"
}

reset_state
: >"${private_tmp}/.hidden-residue"
reset_state
[[ -z "$(find "${private_tmp}" -mindepth 1 -print -quit)" ]] ||
  fail "state reset left hidden temporary residue"
if PATH="${fake_bin}:${PATH}" TEST_STATE="${state}" TEST_CALLS="${calls}" \
  TEST_IMAGE="${image}" curl https://example.invalid >/dev/null 2>&1; then
  fail "fake curl accepted an invocation without --output"
fi

run_reconcile >/dev/null
# shellcheck disable=SC1090
source "${state}"
[[ "${original}" == absent && "${replacement}" == running && "${intruder}" == absent ]] ||
  fail "successful reconciliation did not leave exactly the replacement"
grep -Fq -- "-f ${fixture}/docker-compose.prod.yml up -d --no-build --no-deps --no-recreate --scale frontend=2 frontend" "${calls}" ||
  fail "reconciliation did not stage from the stable exact Compose path"
[[ -z "$(find "${private_tmp}" -mindepth 1 -print -quit)" ]] || fail "success left temporary residue"
[[ ! -e "${fixture}/.infera-recovery/recovery.lock" ]] || fail "success left the shared recovery lock"

reset_state
TEST_DYNAMIC_HEALTH_UPTIME=1 run_reconcile >/dev/null
source "${state}"
[[ "${original}" == absent && "${replacement}" == running && "${intruder}" == absent ]] ||
  fail "dynamic health uptime prevented safe reconciliation"

reset_state
if TEST_HEALTH_STATUS_DRIFT=1 run_reconcile >/dev/null 2>&1; then
  fail "non-uptime health drift unexpectedly reconciled"
fi
source "${state}"
[[ "${original}" == running && "${replacement}" == absent ]] ||
  fail "non-uptime health drift did not preserve the original"

reset_state
if TEST_INVALID_HEALTH_UPTIME=1 run_reconcile >/dev/null 2>&1; then
  fail "invalid health uptime unexpectedly reconciled"
fi
if grep -q ' up ' "${calls}"; then
  fail "invalid health uptime reached frontend staging"
fi

run_reconcile >/dev/null
up_count="$(grep -c ' up ' "${calls}" || true)"
[[ "${up_count}" -eq 1 ]] || fail "idempotent verification created another frontend"

reset_state
# Intentional unsafe mode: prove the workflow rejects a writable state directory.
chmod 0777 "${fixture}/.infera-recovery"
chmod 0644 "${fixture}/.infera-recovery/last-known-good.manifest"
unsafe_state_output="${test_root}/unsafe-state-output"
if run_reconcile >"${unsafe_state_output}" 2>&1; then
  fail "writable recovery state directory unexpectedly passed"
fi
grep -Fq 'production recovery state directory is unsafe' "${unsafe_state_output}" ||
  fail "recovery manifest was read before its unsafe parent was rejected"
chmod 0600 "${fixture}/.infera-recovery/last-known-good.manifest"
chmod 0700 "${fixture}/.infera-recovery"
if grep -q ' up ' "${calls}"; then
  fail "unsafe recovery state reached frontend staging"
fi

reset_state
# Intentional unsafe mode: prove the workflow rejects a writable Compose file.
chmod 0666 "${fixture}/docker-compose.prod.yml"
if run_reconcile >/dev/null 2>&1; then
  fail "writable checked-in Compose file unexpectedly passed"
fi
chmod 0644 "${fixture}/docker-compose.prod.yml"
if grep -q ' up ' "${calls}"; then
  fail "unsafe Compose metadata reached frontend staging"
fi

reset_state
INFERA_BASE_URL=https://environment.example.invalid TEST_OMIT_BASE_URL=1 \
  run_reconcile >/dev/null
grep -Fq 'curl https://environment.example.invalid/' "${calls}" ||
  fail "environment base URL was not used as the default"

reset_state
if INFERA_BASE_URL=http://unsafe.example.invalid TEST_OMIT_BASE_URL=1 \
  run_reconcile >/dev/null 2>&1; then
  fail "unsafe environment base URL unexpectedly passed validation"
fi
if grep -q ' up ' "${calls}"; then
  fail "unsafe environment base URL reached frontend staging"
fi

reset_state
zero_replacement_output="${test_root}/zero-replacement-output"
if TEST_NO_REPLACEMENT=1 run_reconcile >"${zero_replacement_output}" 2>&1; then
  fail "zero-replacement staging unexpectedly reconciled"
fi
source "${state}"
[[ "${original}" == running && "${replacement}" == absent ]] ||
  fail "zero-replacement rollback changed clean frontend state"
if grep -Fq 'automatic frontend rollback could not be proven complete' \
  "${zero_replacement_output}"; then
  fail "zero-replacement staging was falsely classified as ambiguous"
fi

reset_state
if TEST_FAIL_UP=1 run_reconcile >/dev/null 2>&1; then
  fail "partially failed Compose staging unexpectedly reconciled"
fi
source "${state}"
[[ "${original}" == running && "${replacement}" == absent ]] ||
  fail "partial staging failure did not remove its single replacement"

reset_state
if TEST_REPLACEMENT_HEALTH=unhealthy run_reconcile >/dev/null 2>&1; then
  fail "unhealthy replacement unexpectedly reconciled"
fi
source "${state}"
[[ "${original}" == running && "${replacement}" == absent ]] ||
  fail "staging failure did not preserve the untouched original"
if awk '$1 == "stop" { for (field_index = 2; field_index <= NF; field_index++) if ($field_index == "original-container") found = 1 } END { exit !found }' "${calls}"; then
  fail "staging failure stopped the original"
fi

reset_state
if TEST_CUTOVER_ROUTE_FAIL=1 run_reconcile >/dev/null 2>&1; then
  fail "failed replacement-only public route unexpectedly reconciled"
fi
source "${state}"
[[ "${original}" == running && "${replacement}" == absent ]] ||
  fail "cutover failure did not restore the original"

reset_state
if TEST_STAGE_RACE=1 run_reconcile >/dev/null 2>&1; then
  fail "racing third frontend unexpectedly reconciled"
fi
source "${state}"
[[ "${original}" == running && "${replacement}" == running && "${intruder}" == running ]] ||
  fail "ambiguous race cleanup deleted a frontend it could not prove it owned"
if grep -q '^rm .*replacement-container\|^rm .*intruder-container' "${calls}"; then
  fail "ambiguous race cleanup removed an unowned frontend"
fi

reset_state
mkdir -m 0700 "${fixture}/.infera-recovery/recovery.lock"
if run_reconcile >/dev/null 2>&1; then
  fail "concurrent recovery lock unexpectedly passed"
fi
source "${state}"
[[ "${original}" == running && "${replacement}" == absent ]] ||
  fail "concurrent lock failure changed frontend state"
rmdir "${fixture}/.infera-recovery/recovery.lock"

reset_state
if TEST_CUTOVER_ROUTE_FAIL=1 TEST_FAIL_START=1 run_reconcile >/dev/null 2>&1; then
  fail "unrecoverable rollback unexpectedly reconciled"
fi
source "${state}"
[[ "${original}" == stopped && "${replacement}" == running ]] ||
  fail "failed original restoration did not retain the healthy replacement"

reset_state
if TEST_FAIL_STOP=1 TEST_STOPPED_DESPITE_FAILURE=1 run_reconcile >/dev/null 2>&1; then
  fail "ambiguous failed stop unexpectedly reconciled"
fi
source "${state}"
[[ "${original}" == running && "${replacement}" == absent ]] ||
  fail "failed stop did not restore the original and clean its replacement"

reset_state
TEST_EXEC_RECONCILE=1 TEST_REPLACEMENT_HEALTH=starting TEST_HEALTH_ATTEMPTS=30 \
  run_reconcile >/dev/null 2>&1 &
signal_pid=$!
for _ in $(seq 1 1500); do
  grep -q '^replacement=running$' "${state}" && break
  sleep 0.01
done
grep -q '^replacement=running$' "${state}" || fail "signal test did not stage a replacement"
kill -TERM "${signal_pid}"
if wait "${signal_pid}"; then
  fail "signalled reconciliation unexpectedly succeeded"
fi
source "${state}"
[[ "${original}" == running && "${replacement}" == absent ]] ||
  fail "signal rollback did not preserve the original and clean the replacement"
[[ ! -e "${fixture}/.infera-recovery/recovery.lock" ]] || fail "signal rollback left the recovery lock"
[[ -z "$(find "${private_tmp}" -mindepth 1 -print -quit)" ]] || fail "signal rollback left temporary residue"

semantic_output="${test_root}/semantic-output"
replace_env_value "${test_root}/production.env" \
  INFERA_RECOVERY_API_PROTOCOL_VERSION valid-but-mismatched-protocol-sentinel
if run_reconcile >"${semantic_output}" 2>&1; then
  fail "mismatched recovery protocol unexpectedly passed"
fi
if grep -Fq valid-but-mismatched-protocol-sentinel "${semantic_output}"; then
  fail "semantic protocol failure leaked its value"
fi
replace_env_value "${test_root}/production.env" \
  INFERA_RECOVERY_API_PROTOCOL_VERSION test-infera_recovery_api_protocol_version
replace_env_value "${test_root}/production.env" INFERA_RECOVERY_WORKER_MAX_COST_HOUR 0.50
if run_reconcile >"${semantic_output}" 2>&1; then
  fail "mismatched recovery cost unexpectedly passed"
fi
if grep -Eq '0\.50|1\.00' "${semantic_output}"; then
  fail "semantic cost failure leaked a policy value"
fi
replace_env_value "${test_root}/production.env" INFERA_RECOVERY_WORKER_MAX_COST_HOUR 1.00

reset_state
sentinel=never-print-this-production-value
printf 'INFERA_ADMIN_KEY=%s\n' "${sentinel}" >>"${test_root}/production.env"
output="${test_root}/output"
if run_reconcile >"${output}" 2>&1; then
  fail "duplicate secret-bearing fixture unexpectedly passed"
fi
if grep -Fq "${sentinel}" "${output}"; then
  fail "reconciliation leaked a production value"
fi

echo "Frontend provenance reconciliation tests passed."
