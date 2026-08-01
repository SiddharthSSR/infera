#!/usr/bin/env bash
# Regression tests for check-production-env-source.sh.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
TEST_ROOT="$(mktemp -d "${REPO_ROOT}/.check-env-test.XXXXXX")"
trap 'rm -rf "${TEST_ROOT}"' EXIT

CHECK="${SCRIPT_DIR}/check-production-env-source.sh"
EXPECTED_UID="$(id -u)"

fail_test() { echo "FAIL: $*" >&2; exit 1; }

write_valid_source() {
  python3 - "${SCRIPT_DIR}/production-env-source.py" "$1" <<'PY'
import importlib.util
import sys

module_path, output_path = sys.argv[1:]
spec = importlib.util.spec_from_file_location("production_env_source", module_path)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
values = {name: f"test-{name.lower()}" for name in module.REQUIRED_NAMES}
values.update(
    {
        "INFERA_ADMIN_KEY": "test-admin-key",
        "INFERA_GATEWAY_IMAGE": "ghcr.io/example/gateway@sha256:" + "a" * 64,
        "INFERA_FRONTEND_IMAGE": "ghcr.io/example/frontend@sha256:" + "b" * 64,
        "INFERA_WORKER_IMAGE": "ghcr.io/example/worker@sha256:" + "c" * 64,
        "INFERA_GATEWAY_REPLICAS": "1",
        "INFERA_AUDIT_LEDGER_BACKEND": "postgres",
        "INFERA_AUDIT_LEDGER_DSN": "postgresql://approved.invalid/infera",
        "INFERA_RECOVERY_WORKER_MAX_COST_HOUR": "1.00",
    }
)
with open(output_path, "w", encoding="utf-8") as output:
    for name, value in values.items():
        output.write(f"{name}={value}\n")
PY
  chmod 0600 "$1"
}

# 1) Absent env source -> exit 3 with materialization remediation.
missing="${TEST_ROOT}/does-not-exist.env"
out="${TEST_ROOT}/missing.out"
set +e
INFERA_PRODUCTION_ENV_FILE="${missing}" "${CHECK}" --expected-uid "${EXPECTED_UID}" >"${out}" 2>&1
status=$?
set -e
[[ "${status}" -eq 3 ]] || fail_test "expected exit 3 for missing env source, got ${status}"
grep -Fq "materialization has not been run" "${out}" || fail_test "missing case lacked remediation guidance"

# 2) Present and contract-complete -> exit 0.
valid="${TEST_ROOT}/production.env"
write_valid_source "${valid}"
INFERA_PRODUCTION_ENV_FILE="${valid}" "${CHECK}" --expected-uid "${EXPECTED_UID}" \
  | grep -Fq 'values hidden' || fail_test "valid env source did not pass"

# 3) Present but missing a required name -> non-zero, no secret leak.
incomplete="${TEST_ROOT}/incomplete.env"
grep -v '^RUNPOD_API_KEY=' "${valid}" >"${incomplete}"
chmod 0600 "${incomplete}"
out2="${TEST_ROOT}/incomplete.out"
if INFERA_PRODUCTION_ENV_FILE="${incomplete}" "${CHECK}" --expected-uid "${EXPECTED_UID}" >"${out2}" 2>&1; then
  fail_test "incomplete env source unexpectedly passed"
fi
grep -Fq "test-admin-key" "${out2}" && fail_test "incomplete case leaked a value"

echo "check-production-env-source tests passed."
