#!/usr/bin/env bash
# Deterministic behavioral tests for deployment recovery orchestration.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP_DIR="$(mktemp -d)"
cleanup() {
  if [[ -f "${TMP_DIR}/grandchild-pid" ]]; then
    kill -KILL "$(cat "${TMP_DIR}/grandchild-pid")" >/dev/null 2>&1 || true
  fi
  rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

make_manifest() {
  local path="$1" release="$2" worker_protocol="$3" ledger_protocol="$4"
  printf '%s\n' \
    "INFERA_RELEASE_ID=${release}" \
    "INFERA_GATEWAY_IMAGE=ghcr.io/example/gateway:${release}" \
    "INFERA_WORKER_IMAGE=ghcr.io/example/worker:${release}" \
    "INFERA_WORKER_PROTOCOL_VERSION=${worker_protocol}" \
    "INFERA_RECOVERY_API_PROTOCOL_VERSION=1" \
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
printf 'deadline:%s:reserve:%s:step:%s\n' \
  "${INFERA_RECOVERY_DEADLINE_EPOCH:-missing}" \
  "${INFERA_RECOVERY_ROLLBACK_RESERVE_SECONDS:-missing}" \
  "${INFERA_RECOVERY_STEP:-missing}" >>"${TEST_CALLS}"
if [[ "${BLOCK_DRIVER_STEP:-}" == "$1:${release}" ]]; then
  [[ -z "${SIGNAL_CHILD_EXIT_MARKER:-}" ]] || trap 'touch "${SIGNAL_CHILD_EXIT_MARKER}"' EXIT
  if [[ -n "${SIGNAL_GRANDCHILD_PID_FILE:-}" ]]; then
    python3 -c 'import os, signal, sys; [signal.signal(s, signal.SIG_IGN) for s in (signal.SIGHUP, signal.SIGINT, signal.SIGTERM)]; open(sys.argv[1], "w").write(str(os.getpid())); signal.pause()' \
      "${SIGNAL_GRANDCHILD_PID_FILE}" &
  fi
  : >"${BLOCK_MARKER}"
  while [[ ! -e "${BLOCK_RELEASE}" ]]; do sleep 0.1; done
  exit 1
fi
if [[ "${HANG_DRIVER_STEP:-}" == "$1:${release}" ]]; then
  sleep 30
fi
[[ "${FAIL_DRIVER_STEP:-}" != "$1:${release}" ]]
EOF
cat >"${TMP_DIR}/verifier" <<'EOF'
#!/usr/bin/env bash
set -eu
release="$(awk -F= '$1 == "INFERA_RELEASE_ID" { print $2 }' "$1")"
printf 'verify:%s\n' "${release}" >>"${TEST_CALLS}"
if [[ "${SLEEP_VERIFY_RELEASE:-}" == "${release}" ]]; then
  sleep "${SLEEP_VERIFY_SECONDS:?}"
fi
if [[ "${FAIL_PROMOTION_RELEASE:-}" == "${release}" ]]; then
  chmod 0500 "${INFERA_RECOVERY_STATE_DIR}"
fi
[[ "${FAIL_VERIFY_RELEASE:-}" != "${release}" ]]
EOF
chmod +x "${TMP_DIR}/driver" "${TMP_DIR}/verifier"

run_recovery() {
  TEST_CALLS="${TMP_DIR}/calls" \
  INFERA_RECOVERY_MODE=test \
  INFERA_RECOVERY_DRIVER="${TMP_DIR}/driver" \
  INFERA_RECOVERY_VERIFIER="${TMP_DIR}/verifier" \
  INFERA_ACTIVE_AUDIT_LEDGER_WRITER_PROTOCOL=2 \
  INFERA_RECOVERY_STATE_DIR="${TMP_DIR}/state" \
  INFERA_RECOVERY_EVIDENCE_DIR="${TMP_DIR}/evidence" \
  "${REPO_ROOT}/scripts/release-recovery.sh" deploy "$1" "${TMP_DIR}/stable.manifest"
}

grep -v '^INFERA_RECOVERY_API_PROTOCOL_VERSION=' "${TMP_DIR}/candidate.manifest" >"${TMP_DIR}/legacy.manifest"
: >"${TMP_DIR}/calls"
if run_recovery "${TMP_DIR}/legacy.manifest"; then
  echo "legacy manifest without recovery API protocol must be rejected" >&2
  exit 1
fi
[[ ! -s "${TMP_DIR}/calls" ]]

if INFERA_RECOVERY_TIMEOUT_SECONDS=30 \
  INFERA_RECOVERY_ROLLBACK_RESERVE_SECONDS=16 \
  INFERA_RECOVERY_AMBIGUOUS_CLEANUP_SECONDS=1 \
  INFERA_RECOVERY_CANDIDATE_RESTORE_SECONDS=3 \
  INFERA_RECOVERY_MIN_ROLLBACK_STAGE_SECONDS=3 \
  run_recovery "${TMP_DIR}/candidate.manifest"; then
  echo "rollback reserve exhausted by cleanup plus stage slices must be rejected" >&2
  exit 1
fi

assert_contains() { grep -qF "$2" "$1" || { echo "expected '$2' in $1" >&2; exit 1; }; }
assert_mode_600() {
  python3 - "$1" <<'PY'
import os
import stat
import sys

status = os.lstat(sys.argv[1])
if not stat.S_ISREG(status.st_mode):
    raise SystemExit(f"expected regular evidence file: {sys.argv[1]}")
mode = stat.S_IMODE(status.st_mode)
if mode != 0o600:
    raise SystemExit(f"expected mode 0600 for {sys.argv[1]}, got {mode:04o}")
PY
}
assert_order() {
  local file="$1" first="$2" second="$3" first_line second_line
  first_line="$(grep -nF "${first}" "${file}" | head -1 | cut -d: -f1)"
  second_line="$(grep -nF "${second}" "${file}" | head -1 | cut -d: -f1)"
  [[ -n "${first_line}" && -n "${second_line}" && "${first_line}" -lt "${second_line}" ]] || {
    echo "expected '${first}' before '${second}' in ${file}" >&2
    exit 1
  }
}

PRIVATE_EVIDENCE_DIR="${TMP_DIR}/private-evidence"
mkdir -p "${PRIVATE_EVIDENCE_DIR}"
python3 "${REPO_ROOT}/scripts/create-private-evidence.py" "${PRIVATE_EVIDENCE_DIR}/new.log"
assert_mode_600 "${PRIVATE_EVIDENCE_DIR}/new.log"
printf 'existing-content\n' >"${PRIVATE_EVIDENCE_DIR}/existing.log"
if python3 "${REPO_ROOT}/scripts/create-private-evidence.py" "${PRIVATE_EVIDENCE_DIR}/existing.log"; then
  echo "expected existing evidence path to be rejected" >&2
  exit 1
fi
[[ "$(cat "${PRIVATE_EVIDENCE_DIR}/existing.log")" == "existing-content" ]]
printf 'target-content\n' >"${PRIVATE_EVIDENCE_DIR}/target.log"
ln -s "${PRIVATE_EVIDENCE_DIR}/target.log" "${PRIVATE_EVIDENCE_DIR}/symlink.log"
if python3 "${REPO_ROOT}/scripts/create-private-evidence.py" "${PRIVATE_EVIDENCE_DIR}/symlink.log"; then
  echo "expected evidence symlink to be rejected" >&2
  exit 1
fi
[[ "$(cat "${PRIVATE_EVIDENCE_DIR}/target.log")" == "target-content" ]]

rm -f "${TMP_DIR}/calls"
run_recovery "${TMP_DIR}/candidate.manifest"
for evidence_file in "${TMP_DIR}/evidence/"*.log; do assert_mode_600 "${evidence_file}"; done
assert_contains "${TMP_DIR}/calls" "verify:candidate"
assert_order "${TMP_DIR}/calls" "drain-traffic:candidate" "deploy-gateway:candidate"
assert_order "${TMP_DIR}/calls" "verify:candidate" "restore-traffic:candidate"
assert_contains "${TMP_DIR}/state/last-known-good.manifest" "INFERA_RELEASE_ID=candidate"
deadline_count="$(grep '^deadline:' "${TMP_DIR}/calls" | cut -d: -f2 | sort -u | wc -l | tr -d ' ')"
[[ "${deadline_count}" == "1" ]]
if grep -q 'deadline:missing\|reserve:missing\|step:missing' "${TMP_DIR}/calls"; then
  echo "shared recovery deadline context was not propagated" >&2
  exit 1
fi

# The state-directory lock is atomic and cannot be stolen by a second
# controller while the first controller is active.
rm -f "${TMP_DIR}/calls" "${TMP_DIR}/block-marker" "${TMP_DIR}/block-release"
BLOCK_DRIVER_STEP=preflight:candidate \
BLOCK_MARKER="${TMP_DIR}/block-marker" BLOCK_RELEASE="${TMP_DIR}/block-release" \
run_recovery "${TMP_DIR}/candidate.manifest" &
blocked_pid=$!
for _ in $(seq 1 50); do
  [[ -e "${TMP_DIR}/block-marker" ]] && break
  sleep 0.1
done
[[ -e "${TMP_DIR}/block-marker" ]]
if run_recovery "${TMP_DIR}/candidate.manifest"; then
  echo "concurrent recovery controller stole the active lock" >&2
  exit 1
fi
touch "${TMP_DIR}/block-release"
if wait "${blocked_pid}"; then
  echo "blocked controller should reject its injected preflight" >&2
  exit 1
fi
[[ ! -e "${TMP_DIR}/state/recovery.lock" ]]

# TERM and session HUP reap the mutating child before releasing the lock.
for controller_signal in TERM HUP; do
  rm -f "${TMP_DIR}/calls" "${TMP_DIR}/block-marker" "${TMP_DIR}/block-release" \
    "${TMP_DIR}/child-exit" "${TMP_DIR}/grandchild-pid" "${TMP_DIR}/lock-removed-first"
  env \
    TEST_CALLS="${TMP_DIR}/calls" \
    INFERA_RECOVERY_MODE=test \
    INFERA_RECOVERY_DRIVER="${TMP_DIR}/driver" \
    INFERA_RECOVERY_VERIFIER="${TMP_DIR}/verifier" \
    INFERA_ACTIVE_AUDIT_LEDGER_WRITER_PROTOCOL=2 \
    INFERA_RECOVERY_STATE_DIR="${TMP_DIR}/state" \
    INFERA_RECOVERY_EVIDENCE_DIR="${TMP_DIR}/evidence" \
    BLOCK_DRIVER_STEP=preflight:candidate \
    BLOCK_MARKER="${TMP_DIR}/block-marker" \
    BLOCK_RELEASE="${TMP_DIR}/block-release" \
    SIGNAL_CHILD_EXIT_MARKER="${TMP_DIR}/child-exit" \
    SIGNAL_GRANDCHILD_PID_FILE="${TMP_DIR}/grandchild-pid" \
    "${REPO_ROOT}/scripts/release-recovery.sh" deploy \
      "${TMP_DIR}/candidate.manifest" "${TMP_DIR}/stable.manifest" &
  interrupted_pid=$!
  for _ in $(seq 1 50); do
    [[ -e "${TMP_DIR}/block-marker" && -e "${TMP_DIR}/grandchild-pid" && -d "${TMP_DIR}/state/recovery.lock" ]] && break
    sleep 0.1
  done
  [[ -e "${TMP_DIR}/block-marker" && -e "${TMP_DIR}/grandchild-pid" && -d "${TMP_DIR}/state/recovery.lock" ]]
  grandchild_pid="$(cat "${TMP_DIR}/grandchild-pid")"
  (
    while [[ -d "${TMP_DIR}/state/recovery.lock" ]]; do sleep 0.01; done
    if [[ ! -e "${TMP_DIR}/child-exit" ]] || kill -0 "${grandchild_pid}" 2>/dev/null; then
      : >"${TMP_DIR}/lock-removed-first"
    fi
  ) &
  lock_watcher_pid=$!
  kill -s "${controller_signal}" "${interrupted_pid}"
  if wait "${interrupted_pid}"; then
    echo "${controller_signal}-interrupted controller must return failure" >&2
    exit 1
  fi
  wait "${lock_watcher_pid}"
  [[ -e "${TMP_DIR}/child-exit" && ! -e "${TMP_DIR}/lock-removed-first" ]]
  ! kill -0 "${grandchild_pid}" 2>/dev/null
done

# A hung candidate worker deployment is killed before the global deadline;
# rollback retains enough of the shared deadline to restore the stable set.
rm -f "${TMP_DIR}/calls"
started_epoch="$(date +%s)"
if HANG_DRIVER_STEP=deploy-workers:candidate \
  INFERA_RECOVERY_TIMEOUT_SECONDS=24 \
  INFERA_RECOVERY_ROLLBACK_RESERVE_SECONDS=17 \
  INFERA_RECOVERY_AMBIGUOUS_CLEANUP_SECONDS=1 \
  INFERA_RECOVERY_CANDIDATE_RESTORE_SECONDS=3 \
  INFERA_RECOVERY_MIN_ROLLBACK_STAGE_SECONDS=3 \
  run_recovery "${TMP_DIR}/candidate.manifest"; then
  echo "hung candidate worker deployment must reject the candidate" >&2
  exit 1
fi
elapsed="$(( $(date +%s) - started_epoch ))"
(( elapsed <= 24 ))
assert_contains "${TMP_DIR}/calls" "deploy-gateway:stable"
assert_contains "${TMP_DIR}/calls" "deploy-workers:stable"
assert_contains "${TMP_DIR}/calls" "restore-traffic:stable"

# A hung early rollback stage cannot consume the downstream restore slices.
rm -f "${TMP_DIR}/calls"
if FAIL_VERIFY_RELEASE=candidate HANG_DRIVER_STEP=stop-workers:candidate \
  INFERA_RECOVERY_TIMEOUT_SECONDS=24 \
  INFERA_RECOVERY_ROLLBACK_RESERVE_SECONDS=17 \
  INFERA_RECOVERY_AMBIGUOUS_CLEANUP_SECONDS=1 \
  INFERA_RECOVERY_CANDIDATE_RESTORE_SECONDS=3 \
  INFERA_RECOVERY_MIN_ROLLBACK_STAGE_SECONDS=3 \
  run_recovery "${TMP_DIR}/candidate.manifest"; then
  echo "hung rollback cleanup must fail closed" >&2
  exit 1
fi
assert_contains "${TMP_DIR}/calls" "deploy-gateway:stable"
assert_contains "${TMP_DIR}/calls" "deploy-workers:stable"
assert_contains "${TMP_DIR}/calls" "restore-traffic:stable"

# Promotion is refused when verification leaves too little candidate restore budget.
cp "${TMP_DIR}/stable.manifest" "${TMP_DIR}/state/last-known-good.manifest"
rm -f "${TMP_DIR}/calls"
if SLEEP_VERIFY_RELEASE=candidate SLEEP_VERIFY_SECONDS=5 \
  INFERA_RECOVERY_TIMEOUT_SECONDS=25 \
  INFERA_RECOVERY_ROLLBACK_RESERVE_SECONDS=18 \
  INFERA_RECOVERY_AMBIGUOUS_CLEANUP_SECONDS=2 \
  INFERA_RECOVERY_CANDIDATE_RESTORE_SECONDS=6 \
  INFERA_RECOVERY_MIN_ROLLBACK_STAGE_SECONDS=3 \
  run_recovery "${TMP_DIR}/candidate.manifest"; then
  echo "insufficient restore budget must refuse promotion" >&2
  exit 1
fi
assert_contains "${TMP_DIR}/state/last-known-good.manifest" "INFERA_RELEASE_ID=stable"
assert_contains "${TMP_DIR}/calls" "restore-traffic:stable"

# Production mode refuses an implicit/relative lock before any recovery mutation.
rm -f "${TMP_DIR}/calls"
if env -u INFERA_RECOVERY_STATE_DIR \
  TEST_CALLS="${TMP_DIR}/calls" \
  INFERA_RECOVERY_MODE=production \
  INFERA_RECOVERY_DRIVER="${TMP_DIR}/driver" \
  INFERA_RECOVERY_VERIFIER="${TMP_DIR}/verifier" \
  INFERA_ACTIVE_AUDIT_LEDGER_WRITER_PROTOCOL=2 \
  INFERA_RECOVERY_EVIDENCE_DIR="${TMP_DIR}/evidence" \
  "${REPO_ROOT}/scripts/release-recovery.sh" deploy \
    "${TMP_DIR}/candidate.manifest" "${TMP_DIR}/stable.manifest"; then
  echo "production recovery must require an explicit absolute shared state path" >&2
  exit 1
fi
[[ ! -e "${TMP_DIR}/calls" ]]

rm -f "${TMP_DIR}/calls"
if FAIL_DRIVER_STEP=drain-traffic:candidate run_recovery "${TMP_DIR}/candidate.manifest"; then
  echo "expected traffic drain failure to stop the rollout" >&2
  exit 1
fi
if grep -Eq 'stop-workers:|deploy-gateway:|deploy-workers:|restore-traffic:' "${TMP_DIR}/calls"; then
  echo "release mutation ran after traffic drain failure" >&2
  exit 1
fi

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
  assert_order "${TMP_DIR}/calls" "verify:stable" "restore-traffic:stable"
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
if grep -qF "restore-traffic:stable" "${TMP_DIR}/calls"; then
  echo "traffic must remain drained when rollback deployment fails" >&2
  exit 1
fi

rm -f "${TMP_DIR}/calls"
if FAIL_VERIFY_RELEASE=candidate FAIL_DRIVER_STEP=restore-traffic:stable run_recovery "${TMP_DIR}/candidate.manifest"; then
  echo "expected rollback traffic restore failure to fail closed" >&2
  exit 1
fi
assert_order "${TMP_DIR}/calls" "verify:stable" "restore-traffic:stable"
grep -q "FAIL_CLOSED" "${TMP_DIR}/evidence/"*.log

cp "${TMP_DIR}/stable.manifest" "${TMP_DIR}/state/last-known-good.manifest"
rm -f "${TMP_DIR}/calls"
if FAIL_PROMOTION_RELEASE=candidate run_recovery "${TMP_DIR}/candidate.manifest"; then
  echo "expected promotion-state failure to reject candidate" >&2
  exit 1
fi
chmod 0700 "${TMP_DIR}/state"
rm -rf "${TMP_DIR}/state/recovery.lock"
assert_contains "${TMP_DIR}/state/last-known-good.manifest" "INFERA_RELEASE_ID=stable"
assert_order "${TMP_DIR}/calls" "verify:candidate" "deploy-gateway:stable"
assert_order "${TMP_DIR}/calls" "verify:stable" "restore-traffic:stable"

rm -f "${TMP_DIR}/calls"
if FAIL_DRIVER_STEP=restore-traffic:candidate run_recovery "${TMP_DIR}/candidate.manifest"; then
  echo "expected traffic restore failure to fail closed" >&2
  exit 1
fi
grep -q "FAIL_CLOSED" "${TMP_DIR}/evidence/"*.log

python3 - "${TMP_DIR}/evidence" <<'PY'
import glob
import re
import sys

safe = re.compile(r"^[A-Za-z0-9._:+/@-]+$")
timestamp = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$")
schemas = {
    "DRILL": ["candidate", "last_known_good", "ledger_protocol", "timeout_seconds", "rollback_reserve_seconds"],
    "ROLLBACK": ["from", "to", "trigger"],
    "FAIL_CLOSED": ["release", "action"],
    "RECOVERED": ["release", "started_at"],
    "REJECTED": ["release", "action"],
    "PROMOTED": ["release"],
}
for path in glob.glob(f"{sys.argv[1]}/*.log"):
    for line in open(path, encoding="ascii"):
        tokens = line.rstrip("\n").split()
        if len(tokens) < 3 or not timestamp.fullmatch(tokens[0]):
            raise SystemExit(f"invalid coordinator evidence prefix: {line!r}")
        event = tokens[1]
        if event in {"START", "PASS"}:
            if len(tokens) != 3 or not safe.fullmatch(tokens[2]):
                raise SystemExit(f"invalid {event} evidence: {line!r}")
            continue
        if event == "FAIL":
            if len(tokens) not in {3, 4} or not safe.fullmatch(tokens[2]):
                raise SystemExit(f"invalid FAIL evidence: {line!r}")
            fields = tokens[3:]
            expected = ["reason"] if fields else []
        else:
            expected = schemas.get(event)
            if expected is None:
                raise SystemExit(f"unknown coordinator evidence event: {line!r}")
            fields = tokens[2:]
        if len(fields) != len(expected):
            raise SystemExit(f"wrong coordinator evidence field count: {line!r}")
        for token, key in zip(fields, expected):
            prefix = f"{key}="
            if not token.startswith(prefix) or not safe.fullmatch(token[len(prefix):]):
                raise SystemExit(f"invalid coordinator evidence field: {line!r}")
PY

cat >"${TMP_DIR}/adapter-spy" <<'EOF'
#!/usr/bin/env bash
printf 'adapter-called\n' >>"${TEST_CALLS}"
EOF
chmod +x "${TMP_DIR}/adapter-spy"
rm -f "${TMP_DIR}/calls"
if (
  unset INFERA_RESTORE_TRAFFIC_EXECUTABLE
  TEST_CALLS="${TMP_DIR}/calls" \
  INFERA_RECOVERY_DRIVER="${REPO_ROOT}/scripts/compose-release-driver.sh" \
  INFERA_RECOVERY_VERIFIER="${TMP_DIR}/verifier" \
  INFERA_ACTIVE_AUDIT_LEDGER_WRITER_PROTOCOL=2 \
  INFERA_RECOVERY_STATE_DIR="${TMP_DIR}/state" \
  INFERA_RECOVERY_EVIDENCE_DIR="${TMP_DIR}/evidence" \
  INFERA_STOP_WORKERS_EXECUTABLE="${TMP_DIR}/adapter-spy" \
  INFERA_DEPLOY_WORKERS_EXECUTABLE="${TMP_DIR}/adapter-spy" \
  INFERA_DRAIN_TRAFFIC_EXECUTABLE="${TMP_DIR}/adapter-spy" \
  "${REPO_ROOT}/scripts/release-recovery.sh" deploy \
    "${TMP_DIR}/candidate.manifest" "${TMP_DIR}/stable.manifest"
); then
  echo "expected missing restore adapter to fail preflight" >&2
  exit 1
fi
[[ ! -e "${TMP_DIR}/calls" ]] || { echo "adapter ran after incomplete preflight" >&2; exit 1; }

mkdir -p "${TMP_DIR}/bin" "${TMP_DIR}/ledger-evidence"
cat >"${TMP_DIR}/bin/pg_dump" <<'EOF'
#!/usr/bin/env bash
set -eu
printf 'pg_dump\n' >>"${TEST_LEDGER_CALLS}"
if [[ "${LOSE_SOURCE_LOCK:-}" == "1" ]]; then sleep 0.2; fi
for arg in "$@"; do
  case "${arg}" in --file=*) : >"${arg#--file=}" ;; esac
done
EOF
cat >"${TMP_DIR}/bin/pg_restore" <<'EOF'
#!/usr/bin/env bash
printf 'pg_restore\n' >>"${TEST_LEDGER_CALLS}"
exit 0
EOF
cat >"${TMP_DIR}/bin/psql" <<'EOF'
#!/usr/bin/env bash
set -eu
dsn="$1"
shift
input="$(cat)"
printf 'psql\n' >>"${TEST_LEDGER_CALLS}"
query="$* ${input}"
case "${query}" in
  *pg_control_system*)
    if [[ "${SAME_DATABASE_IDENTITY:-}" == "1" ]]; then
      printf '%s\n' cluster-1:42
    elif [[ "${dsn}" == *source-secret* ]]; then
      printf '%s\n' cluster-1:42
    else
      printf '%s\n' cluster-2:84
    fi
    ;;
  *"LOCK TABLE"*)
    ready="$(printf '%s\n' "${input}" | sed -n "s/.*touch '\([^']*\)'.*/\1/p")"
    release="$(printf '%s\n' "${input}" | sed -n "s/.*-f '\([^']*\)'.*/\1/p")"
    snapshot_file="$(printf '%s\n' "${input}" | sed -n "s/.*\\o '\([^']*\)'.*/\1/p" | head -1)"
    printf '%s\n' snapshot-1 >"${snapshot_file}"
    touch "${ready}"
    if [[ "${LOSE_SOURCE_LOCK:-}" == "1" ]]; then sleep 0.05; exit 0; fi
    while [[ ! -f "${release}" ]]; do sleep 0.05; done
    ;;
  *"SELECT value FROM audit_ledger_metadata"*) printf '%s\n' 2 ;;
  *"to_jsonb(row_data)"*)
    if [[ "${RESTORE_CORRUPT:-}" == "1" && "${dsn}" == *restore-secret* ]]; then
      printf '%s\n' metadata:aaa audit:corrupt reservations:ccc
    else
      printf '%s\n' metadata:aaa audit:bbb reservations:ccc
    fi
    ;;
  *) exit 1 ;;
esac
EOF
chmod +x "${TMP_DIR}/bin/pg_dump" "${TMP_DIR}/bin/pg_restore" "${TMP_DIR}/bin/psql"

export TEST_LEDGER_CALLS="${TMP_DIR}/ledger-calls"
rm -f "${TEST_LEDGER_CALLS}"
if PATH="${TMP_DIR}/bin:${PATH}" \
  SAME_DATABASE_IDENTITY=1 \
  INFERA_AUDIT_LEDGER_SOURCE_DSN='postgres://source-secret' \
  INFERA_AUDIT_LEDGER_RESTORE_DSN='postgres://restore-secret' \
  INFERA_RECOVERY_EVIDENCE_DIR="${TMP_DIR}/ledger-evidence" \
  "${REPO_ROOT}/scripts/audit-ledger-recovery-drill.sh"; then
  echo "expected identical database identity to fail before restore" >&2
  exit 1
fi
if grep -Eq '^pg_dump$|^pg_restore$' "${TEST_LEDGER_CALLS}"; then
  echo "destructive ledger commands ran against an identical restore target" >&2
  exit 1
fi

rm -f "${TEST_LEDGER_CALLS}"
PATH="${TMP_DIR}/bin:${PATH}" \
INFERA_AUDIT_LEDGER_SOURCE_DSN='postgres://source-secret' \
INFERA_AUDIT_LEDGER_RESTORE_DSN='postgres://restore-secret' \
INFERA_RECOVERY_EVIDENCE_DIR="${TMP_DIR}/ledger-evidence" \
"${REPO_ROOT}/scripts/audit-ledger-recovery-drill.sh"
grep -q "PASS accounting-content-digest" "${TMP_DIR}/ledger-evidence/"*.log
for evidence_file in "${TMP_DIR}/ledger-evidence/"*.log; do assert_mode_600 "${evidence_file}"; done
if grep -Eq 'source-secret|restore-secret' "${TMP_DIR}/ledger-evidence/"*.log; then
  echo "ledger evidence exposed a DSN" >&2
  exit 1
fi

rm -f "${TEST_LEDGER_CALLS}"
if PATH="${TMP_DIR}/bin:${PATH}" \
  LOSE_SOURCE_LOCK=1 \
  INFERA_AUDIT_LEDGER_SOURCE_DSN='postgres://source-secret' \
  INFERA_AUDIT_LEDGER_RESTORE_DSN='postgres://restore-secret' \
  INFERA_RECOVERY_EVIDENCE_DIR="${TMP_DIR}/ledger-evidence" \
  "${REPO_ROOT}/scripts/audit-ledger-recovery-drill.sh"; then
  echo "expected lost source quiescence to abort the drill" >&2
  exit 1
fi
if grep -q '^pg_restore$' "${TEST_LEDGER_CALLS}"; then
  echo "restore ran after source quiescence was lost" >&2
  exit 1
fi
grep -q "FAIL source-quiesce-lost" "${TMP_DIR}/ledger-evidence/"*.log

rm -f "${TEST_LEDGER_CALLS}"
if PATH="${TMP_DIR}/bin:${PATH}" \
  RESTORE_CORRUPT=1 \
  INFERA_AUDIT_LEDGER_SOURCE_DSN='postgres://source-secret' \
  INFERA_AUDIT_LEDGER_RESTORE_DSN='postgres://restore-secret' \
  INFERA_RECOVERY_EVIDENCE_DIR="${TMP_DIR}/ledger-evidence" \
  "${REPO_ROOT}/scripts/audit-ledger-recovery-drill.sh"; then
  echo "expected row-content corruption to fail digest verification" >&2
  exit 1
fi
grep -q "FAIL accounting-content-digest" "${TMP_DIR}/ledger-evidence/"*.log

echo "Release recovery behavioral tests passed."
