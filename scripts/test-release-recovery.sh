#!/usr/bin/env bash
# Deterministic behavioral tests for deployment recovery orchestration.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

make_manifest() {
  local path="$1" release="$2" worker_protocol="$3" ledger_protocol="$4"
  printf '%s\n' \
    "INFERA_RELEASE_ID=${release}" \
    "INFERA_GATEWAY_IMAGE=ghcr.io/example/gateway:${release}" \
    "INFERA_WORKER_IMAGE=ghcr.io/example/worker:${release}" \
    "INFERA_WORKER_PROTOCOL_VERSION=${worker_protocol}" \
    "INFERA_AUDIT_LEDGER_WRITER_PROTOCOL=${ledger_protocol}" >"${path}"
}

make_manifest "${TMP_DIR}/candidate.manifest" candidate 2 2
make_manifest "${TMP_DIR}/stable.manifest" stable 1 2
make_manifest "${TMP_DIR}/bad-ledger.manifest" bad-ledger 2 999

cat >"${TMP_DIR}/driver" <<'EOF'
#!/usr/bin/env bash
set -eu
release="$(awk -F= '$1 == "INFERA_RELEASE_ID" { print $2 }' "$2")"
printf '%s:%s\n' "$1" "${release}" >>"${TEST_CALLS}"
[[ "${FAIL_DRIVER_STEP:-}" != "$1:${release}" ]]
EOF
cat >"${TMP_DIR}/verifier" <<'EOF'
#!/usr/bin/env bash
set -eu
release="$(awk -F= '$1 == "INFERA_RELEASE_ID" { print $2 }' "$1")"
printf 'verify:%s\n' "${release}" >>"${TEST_CALLS}"
[[ "${FAIL_VERIFY_RELEASE:-}" != "${release}" ]]
EOF
chmod +x "${TMP_DIR}/driver" "${TMP_DIR}/verifier"

run_recovery() {
  TEST_CALLS="${TMP_DIR}/calls" \
  INFERA_RECOVERY_DRIVER="${TMP_DIR}/driver" \
  INFERA_RECOVERY_VERIFIER="${TMP_DIR}/verifier" \
  INFERA_ACTIVE_AUDIT_LEDGER_WRITER_PROTOCOL=2 \
  INFERA_RECOVERY_STATE_DIR="${TMP_DIR}/state" \
  INFERA_RECOVERY_EVIDENCE_DIR="${TMP_DIR}/evidence" \
  "${REPO_ROOT}/scripts/release-recovery.sh" deploy "$1" "${TMP_DIR}/stable.manifest"
}

assert_contains() { grep -qF "$2" "$1" || { echo "expected '$2' in $1" >&2; exit 1; }; }

rm -f "${TMP_DIR}/calls"
run_recovery "${TMP_DIR}/candidate.manifest"
assert_contains "${TMP_DIR}/calls" "verify:candidate"
assert_contains "${TMP_DIR}/state/last-known-good.manifest" "INFERA_RELEASE_ID=candidate"

for scenario in \
  "gateway-startup|deploy-gateway:candidate|" \
  "worker-registration|deploy-workers:candidate|" \
  "zero-healthy-workers||candidate" \
  "ledger-unavailable||candidate"; do
  IFS='|' read -r name driver_failure verifier_failure <<EOF
${scenario}
EOF
  cp "${TMP_DIR}/stable.manifest" "${TMP_DIR}/state/last-known-good.manifest"
  rm -f "${TMP_DIR}/calls"
  if FAIL_DRIVER_STEP="${driver_failure}" FAIL_VERIFY_RELEASE="${verifier_failure}" run_recovery "${TMP_DIR}/candidate.manifest"; then
    echo "expected ${name} injection to reject candidate" >&2
    exit 1
  fi
  assert_contains "${TMP_DIR}/calls" "deploy-gateway:stable"
  assert_contains "${TMP_DIR}/calls" "deploy-workers:stable"
  assert_contains "${TMP_DIR}/calls" "verify:stable"
  assert_contains "${TMP_DIR}/state/last-known-good.manifest" "INFERA_RELEASE_ID=stable"
done

rm -f "${TMP_DIR}/calls"
if run_recovery "${TMP_DIR}/bad-ledger.manifest"; then
  echo "expected incompatible ledger protocol to be refused" >&2
  exit 1
fi
[[ ! -e "${TMP_DIR}/calls" ]] || { echo "protocol refusal must occur before deployment" >&2; exit 1; }

rm -f "${TMP_DIR}/calls"
if FAIL_VERIFY_RELEASE=candidate FAIL_DRIVER_STEP=deploy-gateway:stable run_recovery "${TMP_DIR}/candidate.manifest"; then
  echo "expected failed rollback to fail closed" >&2
  exit 1
fi
grep -q "FAIL_CLOSED" "${TMP_DIR}/evidence/"*.log

mkdir -p "${TMP_DIR}/bin" "${TMP_DIR}/ledger-evidence"
cat >"${TMP_DIR}/bin/pg_dump" <<'EOF'
#!/usr/bin/env bash
set -eu
for arg in "$@"; do
  case "${arg}" in --file=*) : >"${arg#--file=}" ;; esac
done
EOF
cat >"${TMP_DIR}/bin/pg_restore" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
cat >"${TMP_DIR}/bin/psql" <<'EOF'
#!/usr/bin/env bash
cat >/dev/null
printf '%s\n' 2 12:120:60:180 1:1:50
EOF
chmod +x "${TMP_DIR}/bin/pg_dump" "${TMP_DIR}/bin/pg_restore" "${TMP_DIR}/bin/psql"
PATH="${TMP_DIR}/bin:${PATH}" \
INFERA_AUDIT_LEDGER_SOURCE_DSN='postgres://source-secret' \
INFERA_AUDIT_LEDGER_RESTORE_DSN='postgres://restore-secret' \
INFERA_RECOVERY_EVIDENCE_DIR="${TMP_DIR}/ledger-evidence" \
"${REPO_ROOT}/scripts/audit-ledger-recovery-drill.sh"
grep -q "PASS accounting-fingerprint" "${TMP_DIR}/ledger-evidence/"*.log
if grep -Eq 'source-secret|restore-secret' "${TMP_DIR}/ledger-evidence/"*.log; then
  echo "ledger evidence exposed a DSN" >&2
  exit 1
fi

echo "Release recovery behavioral tests passed."
