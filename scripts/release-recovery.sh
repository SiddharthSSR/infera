#!/usr/bin/env bash
# Coordinate a gateway/worker release and return to a proven last-known-good set on failure.

set -uo pipefail

usage() {
  cat >&2 <<'EOF'
Usage: release-recovery.sh deploy <candidate.manifest> <last-known-good.manifest>

Required environment:
  INFERA_RECOVERY_DRIVER       Executable implementing: preflight, stop-workers,
                               deploy-gateway, deploy-workers, drain-traffic,
                               and restore-traffic.
  INFERA_RECOVERY_VERIFIER     Executable accepting one release manifest.
  INFERA_ACTIVE_AUDIT_LEDGER_WRITER_PROTOCOL

Optional environment:
  INFERA_RECOVERY_STATE_DIR    Default: .infera-recovery
  INFERA_RECOVERY_EVIDENCE_DIR Default: recovery-evidence

Release manifests contain exactly these non-secret KEY=value fields:
  INFERA_RELEASE_ID
  INFERA_GATEWAY_IMAGE
  INFERA_WORKER_IMAGE
  INFERA_WORKER_PROTOCOL_VERSION
  INFERA_AUDIT_LEDGER_WRITER_PROTOCOL
EOF
  exit 2
}

[[ "${1:-}" == "deploy" && $# -eq 3 ]] || usage

CANDIDATE_MANIFEST="$2"
LAST_GOOD_MANIFEST="$3"
DRIVER="${INFERA_RECOVERY_DRIVER:-}"
VERIFIER="${INFERA_RECOVERY_VERIFIER:-}"
ACTIVE_LEDGER_PROTOCOL="${INFERA_ACTIVE_AUDIT_LEDGER_WRITER_PROTOCOL:-}"
STATE_DIR="${INFERA_RECOVERY_STATE_DIR:-.infera-recovery}"
EVIDENCE_DIR="${INFERA_RECOVERY_EVIDENCE_DIR:-recovery-evidence}"

[[ -x "${DRIVER}" ]] || { echo "ERROR: INFERA_RECOVERY_DRIVER must be executable" >&2; exit 2; }
[[ -x "${VERIFIER}" ]] || { echo "ERROR: INFERA_RECOVERY_VERIFIER must be executable" >&2; exit 2; }
[[ -n "${ACTIVE_LEDGER_PROTOCOL}" ]] || { echo "ERROR: active audit-ledger writer protocol is required" >&2; exit 2; }

manifest_value() {
  local manifest="$1"
  local key="$2"
  awk -F= -v wanted="${key}" '$1 == wanted { count++; value=substr($0, index($0, "=") + 1) } END { if (count != 1) exit 1; print value }' "${manifest}"
}

validate_manifest() {
  local manifest="$1"
  local key
  local value
  local allowed='^(INFERA_RELEASE_ID|INFERA_GATEWAY_IMAGE|INFERA_WORKER_IMAGE|INFERA_WORKER_PROTOCOL_VERSION|INFERA_AUDIT_LEDGER_WRITER_PROTOCOL)='

  [[ -f "${manifest}" ]] || { echo "ERROR: release manifest not found: ${manifest}" >&2; return 1; }
  if grep -Ev "${allowed}|^[[:space:]]*$|^#" "${manifest}" | grep -q .; then
    echo "ERROR: ${manifest} contains unsupported fields" >&2
    return 1
  fi
  for key in INFERA_RELEASE_ID INFERA_GATEWAY_IMAGE INFERA_WORKER_IMAGE INFERA_WORKER_PROTOCOL_VERSION INFERA_AUDIT_LEDGER_WRITER_PROTOCOL; do
    if ! value="$(manifest_value "${manifest}" "${key}")" || [[ ! "${value}" =~ ^[A-Za-z0-9._:/@+-]+$ ]]; then
      echo "ERROR: ${manifest} must contain one safe, non-empty ${key}" >&2
      return 1
    fi
  done
}

validate_manifest "${CANDIDATE_MANIFEST}" || exit 2
validate_manifest "${LAST_GOOD_MANIFEST}" || exit 2

CANDIDATE_RELEASE="$(manifest_value "${CANDIDATE_MANIFEST}" INFERA_RELEASE_ID)"
LAST_GOOD_RELEASE="$(manifest_value "${LAST_GOOD_MANIFEST}" INFERA_RELEASE_ID)"
CANDIDATE_LEDGER_PROTOCOL="$(manifest_value "${CANDIDATE_MANIFEST}" INFERA_AUDIT_LEDGER_WRITER_PROTOCOL)"
LAST_GOOD_LEDGER_PROTOCOL="$(manifest_value "${LAST_GOOD_MANIFEST}" INFERA_AUDIT_LEDGER_WRITER_PROTOCOL)"

if [[ "${CANDIDATE_LEDGER_PROTOCOL}" != "${ACTIVE_LEDGER_PROTOCOL}" ]]; then
  echo "ERROR: candidate ${CANDIDATE_RELEASE} requires audit-ledger writer protocol ${CANDIDATE_LEDGER_PROTOCOL}, active ledger is ${ACTIVE_LEDGER_PROTOCOL}" >&2
  exit 2
fi
if [[ "${LAST_GOOD_LEDGER_PROTOCOL}" != "${ACTIVE_LEDGER_PROTOCOL}" ]]; then
  echo "ERROR: rollback target ${LAST_GOOD_RELEASE} requires audit-ledger writer protocol ${LAST_GOOD_LEDGER_PROTOCOL}, active ledger is ${ACTIVE_LEDGER_PROTOCOL}" >&2
  exit 2
fi

mkdir -p "${STATE_DIR}" "${EVIDENCE_DIR}" || exit 1
STARTED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
EVIDENCE_FILE="${EVIDENCE_DIR}/recovery-${CANDIDATE_RELEASE}-$(date -u +%Y%m%dT%H%M%SZ)-$$.log"
FAILED_STEP=""

record() {
  printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$1" | tee -a "${EVIDENCE_FILE}"
}

run_step() {
  local label="$1"
  shift
  record "START ${label}"
  if "$@"; then
    record "PASS ${label}"
    return 0
  fi
  record "FAIL ${label}"
  FAILED_STEP="${label}"
  return 1
}

rollback() {
  record "ROLLBACK from=${CANDIDATE_RELEASE} to=${LAST_GOOD_RELEASE} trigger=${FAILED_STEP}"
  run_step "rollback.stop-candidate-workers" "${DRIVER}" stop-workers "${CANDIDATE_MANIFEST}" || true
  if ! run_step "rollback.deploy-gateway" "${DRIVER}" deploy-gateway "${LAST_GOOD_MANIFEST}"; then
    record "FAIL_CLOSED release=${LAST_GOOD_RELEASE} action=keep-traffic-drained-and-escalate"
    return 1
  fi
  if ! run_step "rollback.deploy-workers" "${DRIVER}" deploy-workers "${LAST_GOOD_MANIFEST}"; then
    record "FAIL_CLOSED release=${LAST_GOOD_RELEASE} action=keep-traffic-drained-and-escalate"
    return 1
  fi
  if ! run_step "rollback.verify-last-known-good" "${VERIFIER}" "${LAST_GOOD_MANIFEST}"; then
    record "FAIL_CLOSED release=${LAST_GOOD_RELEASE} action=keep-traffic-drained-and-escalate"
    return 1
  fi
  if ! run_step "rollback.restore-traffic" "${DRIVER}" restore-traffic "${LAST_GOOD_MANIFEST}"; then
    record "FAIL_CLOSED release=${LAST_GOOD_RELEASE} action=keep-traffic-drained-and-escalate"
    return 1
  fi
  record "RECOVERED release=${LAST_GOOD_RELEASE} started_at=${STARTED_AT}"
  return 0
}

record "DRILL candidate=${CANDIDATE_RELEASE} last_known_good=${LAST_GOOD_RELEASE} ledger_protocol=${ACTIVE_LEDGER_PROTOCOL}"

if ! run_step "candidate.preflight" "${DRIVER}" preflight "${CANDIDATE_MANIFEST}"; then
  record "REJECTED release=${CANDIDATE_RELEASE} action=leave-last-known-good-untouched"
  exit 1
fi
if ! run_step "candidate.drain-traffic" "${DRIVER}" drain-traffic "${CANDIDATE_MANIFEST}"; then
  record "FAIL_CLOSED release=${LAST_GOOD_RELEASE} action=verify-ingress-is-drained-and-escalate"
  exit 1
fi
if ! run_step "candidate.stop-last-known-good-workers" "${DRIVER}" stop-workers "${LAST_GOOD_MANIFEST}"; then rollback; exit 1; fi
if ! run_step "candidate.deploy-gateway" "${DRIVER}" deploy-gateway "${CANDIDATE_MANIFEST}"; then rollback; exit 1; fi
if ! run_step "candidate.deploy-workers" "${DRIVER}" deploy-workers "${CANDIDATE_MANIFEST}"; then rollback; exit 1; fi
if ! run_step "candidate.verify" "${VERIFIER}" "${CANDIDATE_MANIFEST}"; then rollback; exit 1; fi

PROMOTION_TMP="${STATE_DIR}/.last-known-good.manifest.$$"
if ! cp "${CANDIDATE_MANIFEST}" "${PROMOTION_TMP}" || \
   ! mv "${PROMOTION_TMP}" "${STATE_DIR}/last-known-good.manifest"; then
  rm -f "${PROMOTION_TMP}" >/dev/null 2>&1 || true
  FAILED_STEP="candidate.promote-state"
  record "FAIL ${FAILED_STEP}"
  rollback
  exit 1
fi
record "PROMOTED release=${CANDIDATE_RELEASE} state=${STATE_DIR}/last-known-good.manifest"
if ! run_step "candidate.restore-traffic" "${DRIVER}" restore-traffic "${CANDIDATE_MANIFEST}"; then
  record "FAIL_CLOSED release=${CANDIDATE_RELEASE} action=keep-traffic-drained-and-escalate"
  exit 1
fi
