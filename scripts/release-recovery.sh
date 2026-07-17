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
  INFERA_RECOVERY_STATE_DIR  Absolute state/lock path in production.
  INFERA_RECOVERY_CONTROLLER_SCOPE
                             shared-filesystem or designated-single-controller

Optional environment:
  INFERA_RECOVERY_MODE         Default: production; test/development are explicit
  INFERA_RECOVERY_STATE_DIR    Default outside production: .infera-recovery
  INFERA_RECOVERY_EVIDENCE_DIR Default: recovery-evidence
  INFERA_RECOVERY_TIMEOUT_SECONDS          Default/max: 900
  INFERA_RECOVERY_ROLLBACK_RESERVE_SECONDS Default: 300
  INFERA_RECOVERY_AMBIGUOUS_CLEANUP_SECONDS Default: 60
  INFERA_RECOVERY_CANDIDATE_RESTORE_SECONDS Default: 30
  INFERA_RECOVERY_MIN_ROLLBACK_STAGE_SECONDS Default: 30

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
RECOVERY_MODE="${INFERA_RECOVERY_MODE:-production}"
STATE_DIR="${INFERA_RECOVERY_STATE_DIR:-.infera-recovery}"
CONTROLLER_SCOPE="${INFERA_RECOVERY_CONTROLLER_SCOPE:-}"
EVIDENCE_DIR="${INFERA_RECOVERY_EVIDENCE_DIR:-recovery-evidence}"
RECOVERY_TIMEOUT_SECONDS="${INFERA_RECOVERY_TIMEOUT_SECONDS:-900}"
ROLLBACK_RESERVE_SECONDS="${INFERA_RECOVERY_ROLLBACK_RESERVE_SECONDS:-300}"
AMBIGUOUS_CLEANUP_SECONDS="${INFERA_RECOVERY_AMBIGUOUS_CLEANUP_SECONDS:-60}"
CANDIDATE_RESTORE_SECONDS="${INFERA_RECOVERY_CANDIDATE_RESTORE_SECONDS:-30}"
MIN_ROLLBACK_STAGE_SECONDS="${INFERA_RECOVERY_MIN_ROLLBACK_STAGE_SECONDS:-30}"

[[ -x "${DRIVER}" ]] || { echo "ERROR: INFERA_RECOVERY_DRIVER must be executable" >&2; exit 2; }
[[ -x "${VERIFIER}" ]] || { echo "ERROR: INFERA_RECOVERY_VERIFIER must be executable" >&2; exit 2; }
[[ -n "${ACTIVE_LEDGER_PROTOCOL}" ]] || { echo "ERROR: active audit-ledger writer protocol is required" >&2; exit 2; }
[[ "${RECOVERY_MODE}" == "production" || "${RECOVERY_MODE}" == "test" || "${RECOVERY_MODE}" == "development" ]] || {
  echo "ERROR: INFERA_RECOVERY_MODE must be production, test, or development" >&2
  exit 2
}
if [[ "${RECOVERY_MODE}" == "production" ]]; then
  [[ -n "${INFERA_RECOVERY_STATE_DIR:-}" && "${STATE_DIR}" == /* ]] || {
    echo "ERROR: production recovery requires an explicitly configured absolute INFERA_RECOVERY_STATE_DIR" >&2
    exit 2
  }
  [[ "${CONTROLLER_SCOPE}" == "shared-filesystem" || "${CONTROLLER_SCOPE}" == "designated-single-controller" ]] || {
    echo "ERROR: production recovery requires INFERA_RECOVERY_CONTROLLER_SCOPE=shared-filesystem or designated-single-controller" >&2
    exit 2
  }
fi
[[ "${RECOVERY_TIMEOUT_SECONDS}" =~ ^[1-9][0-9]*$ && "${RECOVERY_TIMEOUT_SECONDS}" -le 900 ]] || {
  echo "ERROR: INFERA_RECOVERY_TIMEOUT_SECONDS must be between 1 and 900" >&2
  exit 2
}
[[ "${ROLLBACK_RESERVE_SECONDS}" =~ ^[1-9][0-9]*$ && "${ROLLBACK_RESERVE_SECONDS}" -lt "${RECOVERY_TIMEOUT_SECONDS}" ]] || {
  echo "ERROR: rollback reserve must be positive and smaller than the recovery timeout" >&2
  exit 2
}
[[ "${AMBIGUOUS_CLEANUP_SECONDS}" =~ ^[1-9][0-9]*$ && "${AMBIGUOUS_CLEANUP_SECONDS}" -lt "${ROLLBACK_RESERVE_SECONDS}" ]] || {
  echo "ERROR: ambiguous cleanup allowance must be positive and smaller than the rollback reserve" >&2
  exit 2
}
[[ "${CANDIDATE_RESTORE_SECONDS}" =~ ^[1-9][0-9]*$ && \
   "${CANDIDATE_RESTORE_SECONDS}" -lt "$((RECOVERY_TIMEOUT_SECONDS - ROLLBACK_RESERVE_SECONDS))" ]] || {
  echo "ERROR: candidate restore budget must fit before the rollback reserve" >&2
  exit 2
}
[[ "${MIN_ROLLBACK_STAGE_SECONDS}" =~ ^[1-9][0-9]*$ && "${MIN_ROLLBACK_STAGE_SECONDS}" -ge 3 && \
   $((MIN_ROLLBACK_STAGE_SECONDS * 5)) -le "${ROLLBACK_RESERVE_SECONDS}" ]] || {
  echo "ERROR: rollback reserve must retain at least five rollback-stage slices of 3 seconds or more" >&2
  exit 2
}

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
LOCK_DIR="${STATE_DIR}/recovery.lock"
if ! mkdir "${LOCK_DIR}" 2>/dev/null; then
  echo "ERROR: another recovery controller holds ${LOCK_DIR}; never steal or auto-expire this lock" >&2
  exit 1
fi
LOCK_HELD=1
ACTIVE_RUNNER_PID=""
PENDING_CONTROLLER_SIGNAL=""
PENDING_CONTROLLER_STATUS=""
release_lock() {
  if [[ "${LOCK_HELD:-0}" == "1" ]]; then
    rm -f "${LOCK_DIR}/owner" >/dev/null 2>&1 || true
    rmdir "${LOCK_DIR}" >/dev/null 2>&1 || true
    LOCK_HELD=0
  fi
}
trap release_lock EXIT
handle_controller_signal() {
  local signal_name="$1" exit_status="$2"
  trap '' HUP INT TERM
  if [[ -n "${ACTIVE_RUNNER_PID}" ]]; then
    kill -s "${signal_name}" "${ACTIVE_RUNNER_PID}" >/dev/null 2>&1 || true
    wait "${ACTIVE_RUNNER_PID}" >/dev/null 2>&1 || true
    ACTIVE_RUNNER_PID=""
  fi
  exit "${exit_status}"
}
queue_controller_signal() {
  PENDING_CONTROLLER_SIGNAL="$1"
  PENDING_CONTROLLER_STATUS="$2"
}
trap 'handle_controller_signal HUP 129' HUP
trap 'handle_controller_signal INT 130' INT
trap 'handle_controller_signal TERM 143' TERM
printf 'controller_pid=%s started_at=%s\n' "$$" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >"${LOCK_DIR}/owner" || exit 1
chmod 0600 "${LOCK_DIR}/owner" || exit 1
STARTED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
STARTED_EPOCH="$(date +%s)"
RECOVERY_DEADLINE_EPOCH="$((STARTED_EPOCH + RECOVERY_TIMEOUT_SECONDS))"
CANDIDATE_DEADLINE_EPOCH="$((RECOVERY_DEADLINE_EPOCH - ROLLBACK_RESERVE_SECONDS))"
EVIDENCE_FILE="${EVIDENCE_DIR}/recovery-${CANDIDATE_RELEASE}-$(date -u +%Y%m%dT%H%M%SZ)-$$.log"
FAILED_STEP=""

if ! python3 "$(dirname "$0")/create-private-evidence.py" "${EVIDENCE_FILE}"; then
  echo "ERROR: unable to create root-only recovery evidence" >&2
  exit 1
fi

record() {
  printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$1" | tee -a "${EVIDENCE_FILE}"
}

run_step() {
  local label="$1"
  local release deadline_limit command_deadline runner_status now
  shift
  release="${CANDIDATE_RELEASE}"
  [[ "${label}" == rollback.* ]] && release="${LAST_GOOD_RELEASE}"
  deadline_limit="${RECOVERY_DEADLINE_EPOCH}"
  case "${label}" in
    candidate.*) deadline_limit="${CANDIDATE_DEADLINE_EPOCH}" ;;
    rollback.stop-candidate-workers) deadline_limit="$((RECOVERY_DEADLINE_EPOCH - (MIN_ROLLBACK_STAGE_SECONDS * 4)))" ;;
    rollback.deploy-gateway) deadline_limit="$((RECOVERY_DEADLINE_EPOCH - (MIN_ROLLBACK_STAGE_SECONDS * 3)))" ;;
    rollback.deploy-workers) deadline_limit="$((RECOVERY_DEADLINE_EPOCH - (MIN_ROLLBACK_STAGE_SECONDS * 2)))" ;;
    rollback.verify-last-known-good) deadline_limit="$((RECOVERY_DEADLINE_EPOCH - MIN_ROLLBACK_STAGE_SECONDS))" ;;
  esac
  command_deadline="${deadline_limit}"
  # The worker adapter stops ordinary work at the candidate deadline itself,
  # but may use a bounded part of the reserve to reconcile an ambiguous paid resource.
  [[ "${label}" == "candidate.deploy-workers" ]] && command_deadline="$((deadline_limit + AMBIGUOUS_CLEANUP_SECONDS))"
  record "START ${label}"
  if (( $(date +%s) >= deadline_limit )); then
    record "FAIL ${label} reason=deadline_exhausted"
    FAILED_STEP="${label}"
    return 1
  fi
  now="$(date +%s)"
  if [[ "${label}" == "candidate.restore-traffic" ]]; then
    command_deadline="$((now + CANDIDATE_RESTORE_SECONDS))"
    (( command_deadline > CANDIDATE_DEADLINE_EPOCH )) && command_deadline="${CANDIDATE_DEADLINE_EPOCH}"
  fi
  # Defer a signal that lands between spawning the wrapper and recording its
  # PID. Caught dispositions are not inherited across exec, and the pending
  # signal is delivered after the wrapper PID becomes authoritative.
  PENDING_CONTROLLER_SIGNAL=""
  PENDING_CONTROLLER_STATUS=""
  trap 'queue_controller_signal HUP 129' HUP
  trap 'queue_controller_signal INT 130' INT
  trap 'queue_controller_signal TERM 143' TERM
  INFERA_RECOVERY_DEADLINE_EPOCH="${RECOVERY_DEADLINE_EPOCH}" \
    INFERA_RECOVERY_ROLLBACK_RESERVE_SECONDS="${ROLLBACK_RESERVE_SECONDS}" \
    INFERA_RECOVERY_EVIDENCE_FILE="${EVIDENCE_FILE}" \
    INFERA_RECOVERY_RELEASE_ID="${release}" \
    INFERA_RECOVERY_STEP="${label}" \
  python3 "$(dirname "$0")/run-with-deadline.py" "${command_deadline}" "$@" &
  ACTIVE_RUNNER_PID=$!
  trap 'handle_controller_signal HUP 129' HUP
  trap 'handle_controller_signal INT 130' INT
  trap 'handle_controller_signal TERM 143' TERM
  if [[ -n "${PENDING_CONTROLLER_SIGNAL}" ]]; then
    handle_controller_signal "${PENDING_CONTROLLER_SIGNAL}" "${PENDING_CONTROLLER_STATUS}"
  fi
  wait "${ACTIVE_RUNNER_PID}"
  runner_status=$?
  ACTIVE_RUNNER_PID=""
  if [[ "${runner_status}" -eq 0 ]]; then
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

record "DRILL candidate=${CANDIDATE_RELEASE} last_known_good=${LAST_GOOD_RELEASE} ledger_protocol=${ACTIVE_LEDGER_PROTOCOL} timeout_seconds=${RECOVERY_TIMEOUT_SECONDS} rollback_reserve_seconds=${ROLLBACK_RESERVE_SECONDS}"

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

if (( CANDIDATE_DEADLINE_EPOCH - $(date +%s) < CANDIDATE_RESTORE_SECONDS )); then
  FAILED_STEP="candidate.restore-budget"
  record "FAIL ${FAILED_STEP} reason=insufficient_pre_promotion_restore_budget"
  rollback
  exit 1
fi

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
