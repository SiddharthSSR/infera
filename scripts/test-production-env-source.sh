#!/usr/bin/env bash
# shellcheck disable=SC2016

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
TEST_ROOT="$(mktemp -d "${REPO_ROOT}/.production-env-test.XXXXXX")"
trap 'rm -rf "${TEST_ROOT}"' EXIT

EXPECTED_UID="$(id -u)"
VALIDATOR=(python3 "${SCRIPT_DIR}/production-env-source.py")
SECRET_SENTINEL="SUPER_SECRET_MUST_NOT_LEAK"

fail_test() {
  echo "FAIL: $*" >&2
  exit 1
}

write_valid_source() {
  local path="$1"
  python3 - "${SCRIPT_DIR}/production-env-source.py" "${path}" "${SECRET_SENTINEL}" <<'PY'
import importlib.util
import sys

module_path, output_path, sentinel = sys.argv[1:]
spec = importlib.util.spec_from_file_location("production_env_source", module_path)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
values = {name: f"test-{name.lower()}" for name in module.REQUIRED_NAMES}
values.update(
    {
        "INFERA_ADMIN_KEY": sentinel,
        "INFERA_GATEWAY_IMAGE": "ghcr.io/example/gateway@sha256:" + "a" * 64,
        "INFERA_FRONTEND_IMAGE": "ghcr.io/example/frontend@sha256:" + "b" * 64,
        "INFERA_WORKER_IMAGE": "ghcr.io/example/worker@sha256:" + "c" * 64,
        "INFERA_GATEWAY_REPLICAS": "2",
        "INFERA_AUDIT_LEDGER_BACKEND": "postgres",
        "INFERA_AUDIT_LEDGER_DSN": "postgresql://approved.invalid/infera",
        "INFERA_RECOVERY_WORKER_MAX_COST_HOUR": "1.00",
    }
)
with open(output_path, "w", encoding="utf-8") as output:
    for name, value in values.items():
        output.write(f"{name}={value}\n")
PY
  chmod 0600 "${path}"
}

run_validator() {
  INFERA_PRODUCTION_ENV_FILE="$1" \
    "${VALIDATOR[@]}" validate --expected-uid "${EXPECTED_UID}"
}

assert_rejected() {
  local name="$1"
  shift
  local output="${TEST_ROOT}/${name}.output"
  if "$@" >"${output}" 2>&1; then
    fail_test "${name} unexpectedly passed"
  fi
  if grep -Fq "${SECRET_SENTINEL}" "${output}"; then
    fail_test "${name} leaked a secret value"
  fi
}

SOURCE="${TEST_ROOT}/production.env"
write_valid_source "${SOURCE}"
run_validator "${SOURCE}" | grep -Fq 'values hidden'

assert_rejected missing-reference \
  env -u INFERA_PRODUCTION_ENV_FILE \
  "${VALIDATOR[@]}" validate --expected-uid "${EXPECTED_UID}"

assert_rejected ambiguous-input \
  env INFERA_PRODUCTION_ENV_FILE="${SOURCE}" ENV_FILE="${SOURCE}" \
  "${VALIDATOR[@]}" validate --expected-uid "${EXPECTED_UID}"

(
  cd "${TEST_ROOT}"
  assert_rejected relative-path \
    env INFERA_PRODUCTION_ENV_FILE=production.env \
    "${VALIDATOR[@]}" validate --expected-uid "${EXPECTED_UID}"
)

ln -s "${SOURCE}" "${TEST_ROOT}/source-link"
assert_rejected symlink-source run_validator "${TEST_ROOT}/source-link"
assert_rejected non-regular-source run_validator "${TEST_ROOT}"

mkdir "${TEST_ROOT}/real-parent"
chmod 0700 "${TEST_ROOT}/real-parent"
write_valid_source "${TEST_ROOT}/real-parent/production.env"
ln -s "${TEST_ROOT}/real-parent" "${TEST_ROOT}/linked-parent"
assert_rejected symlink-parent run_validator \
  "${TEST_ROOT}/linked-parent/production.env"

mkdir "${TEST_ROOT}/unsafe-parent"
chmod 0750 "${TEST_ROOT}/unsafe-parent"
write_valid_source "${TEST_ROOT}/unsafe-parent/production.env"
assert_rejected unsafe-parent-mode run_validator \
  "${TEST_ROOT}/unsafe-parent/production.env"

UNSAFE_MODE="${TEST_ROOT}/unsafe-mode.env"
cp "${SOURCE}" "${UNSAFE_MODE}"
chmod 0640 "${UNSAFE_MODE}"
assert_rejected unsafe-mode run_validator "${UNSAFE_MODE}"

assert_rejected wrong-owner \
  env INFERA_PRODUCTION_ENV_FILE="${SOURCE}" \
  "${VALIDATOR[@]}" validate --expected-uid "$((EXPECTED_UID + 1))"

MISSING_NAME="${TEST_ROOT}/missing-name.env"
grep -v '^RUNPOD_API_KEY=' "${SOURCE}" >"${MISSING_NAME}"
chmod 0600 "${MISSING_NAME}"
assert_rejected missing-required-name run_validator "${MISSING_NAME}"

DUPLICATE_NAME="${TEST_ROOT}/duplicate-name.env"
cp "${SOURCE}" "${DUPLICATE_NAME}"
printf '%s\n' 'RUNPOD_API_KEY=duplicate-value' >>"${DUPLICATE_NAME}"
chmod 0600 "${DUPLICATE_NAME}"
assert_rejected duplicate-name run_validator "${DUPLICATE_NAME}"

CONFLICTING_NAME="${TEST_ROOT}/conflicting-name.env"
cp "${SOURCE}" "${CONFLICTING_NAME}"
printf '%s\n' 'INFERA_ADMIN_KEY=SUPER_SECRET_CONFLICTING_VALUE' >>"${CONFLICTING_NAME}"
chmod 0600 "${CONFLICTING_NAME}"
assert_rejected conflicting-name run_validator "${CONFLICTING_NAME}"
if grep -Fq 'SUPER_SECRET_CONFLICTING_VALUE' "${TEST_ROOT}/conflicting-name.output"; then
  fail_test "conflicting-name leaked the conflicting value"
fi

INVALID_RECOVERY_PROTOCOL="${TEST_ROOT}/invalid-recovery-protocol.env"
sed 's/^INFERA_RECOVERY_API_PROTOCOL_VERSION=.*/INFERA_RECOVERY_API_PROTOCOL_VERSION=invalid protocol/' \
  "${SOURCE}" >"${INVALID_RECOVERY_PROTOCOL}"
chmod 0600 "${INVALID_RECOVERY_PROTOCOL}"
assert_rejected invalid-recovery-protocol run_validator \
  "${INVALID_RECOVERY_PROTOCOL}"

OVER_POLICY_COST="${TEST_ROOT}/over-policy-cost.env"
sed 's/^INFERA_RECOVERY_WORKER_MAX_COST_HOUR=.*/INFERA_RECOVERY_WORKER_MAX_COST_HOUR=1.01/' \
  "${SOURCE}" >"${OVER_POLICY_COST}"
chmod 0600 "${OVER_POLICY_COST}"
assert_rejected over-policy-cost run_validator "${OVER_POLICY_COST}"

SPECIAL_VALUES="${TEST_ROOT}/special-values.env"
cp "${SOURCE}" "${SPECIAL_VALUES}"
printf '%s\n' \
  'TEST_UNQUOTED=safe-._~:/?@!%+=,' \
  'TEST_SINGLE='\''literal # $ ${NOT_EXPANDED} with spaces'\''' \
  'TEST_DOUBLE="double # value with spaces and \"quotes\" and \\slash"' \
  >>"${SPECIAL_VALUES}"
chmod 0600 "${SPECIAL_VALUES}"
run_validator "${SPECIAL_VALUES}" >/dev/null
[[ "$(
  INFERA_PRODUCTION_ENV_FILE="${SPECIAL_VALUES}" \
    "${VALIDATOR[@]}" value TEST_UNQUOTED --expected-uid "${EXPECTED_UID}"
)" == 'safe-._~:/?@!%+=,' ]]
[[ "$(
  INFERA_PRODUCTION_ENV_FILE="${SPECIAL_VALUES}" \
    "${VALIDATOR[@]}" value TEST_SINGLE --expected-uid "${EXPECTED_UID}"
)" == 'literal # $ ${NOT_EXPANDED} with spaces' ]]
[[ "$(
  INFERA_PRODUCTION_ENV_FILE="${SPECIAL_VALUES}" \
    "${VALIDATOR[@]}" value TEST_DOUBLE --expected-uid "${EXPECTED_UID}"
)" == 'double # value with spaces and "quotes" and \slash' ]]

UNQUOTED_INTERPOLATION="${TEST_ROOT}/unquoted-interpolation.env"
cp "${SOURCE}" "${UNQUOTED_INTERPOLATION}"
printf '%s\n' 'TEST_EXPANSION=${SUPER_SECRET_MUST_NOT_LEAK}' \
  >>"${UNQUOTED_INTERPOLATION}"
chmod 0600 "${UNQUOTED_INTERPOLATION}"
assert_rejected unquoted-interpolation run_validator "${UNQUOTED_INTERPOLATION}"

DOUBLE_INTERPOLATION="${TEST_ROOT}/double-interpolation.env"
cp "${SOURCE}" "${DOUBLE_INTERPOLATION}"
printf '%s\n' 'TEST_EXPANSION="${SUPER_SECRET_MUST_NOT_LEAK}"' \
  >>"${DOUBLE_INTERPOLATION}"
chmod 0600 "${DOUBLE_INTERPOLATION}"
assert_rejected double-interpolation run_validator "${DOUBLE_INTERPOLATION}"

AMBIGUOUS_COMMENT="${TEST_ROOT}/ambiguous-comment.env"
cp "${SOURCE}" "${AMBIGUOUS_COMMENT}"
printf '%s\n' 'TEST_COMMENT=SUPER_SECRET_MUST_NOT_LEAK # comment' \
  >>"${AMBIGUOUS_COMMENT}"
chmod 0600 "${AMBIGUOUS_COMMENT}"
assert_rejected ambiguous-inline-comment run_validator "${AMBIGUOUS_COMMENT}"

mkdir "${TEST_ROOT}/bin"
cat >"${TEST_ROOT}/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ -z "${INFERA_ADMIN_KEY:-}" ]]
[[ -z "${RUNPOD_API_KEY:-}" ]]
[[ "${INFERA_GATEWAY_IMAGE}" == "manifest-gateway-image" ]]
printf '%s\n' "$*" >"${COMPOSE_CAPTURE}"
EOF
chmod +x "${TEST_ROOT}/bin/docker"
COMPOSE_CAPTURE="${TEST_ROOT}/compose.args" \
PATH="${TEST_ROOT}/bin:${PATH}" \
INFERA_PRODUCTION_ENV_FILE="${SOURCE}" \
INFERA_ADMIN_KEY="${SECRET_SENTINEL}" \
RUNPOD_API_KEY="${SECRET_SENTINEL}" \
INFERA_GATEWAY_IMAGE=manifest-gateway-image \
  "${VALIDATOR[@]}" compose --expected-uid "${EXPECTED_UID}" \
    --override INFERA_GATEWAY_IMAGE -- -f docker-compose.prod.yml config --quiet
grep -Fq "compose --env-file ${SOURCE} -f docker-compose.prod.yml config --quiet" \
  "${TEST_ROOT}/compose.args"
if grep -Fq "${SECRET_SENTINEL}" "${TEST_ROOT}/compose.args"; then
  fail_test "Compose invocation leaked a secret value"
fi

echo "Production environment source tests passed."
