#!/usr/bin/env bash
# Prove a PostgreSQL audit/quota ledger backup can be restored without exposing DSNs.

set -euo pipefail

: "${INFERA_AUDIT_LEDGER_SOURCE_DSN:?source DSN is required}"
: "${INFERA_AUDIT_LEDGER_RESTORE_DSN:?restore DSN is required}"
EXPECTED_PROTOCOL="${INFERA_AUDIT_LEDGER_WRITER_PROTOCOL:-2}"
EVIDENCE_DIR="${INFERA_RECOVERY_EVIDENCE_DIR:-recovery-evidence}"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT
mkdir -p "${EVIDENCE_DIR}"
EVIDENCE_FILE="${EVIDENCE_DIR}/ledger-restore-$(date -u +%Y%m%dT%H%M%SZ).log"
DUMP_FILE="${TMP_DIR}/audit-ledger.dump"

record() { printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$1" | tee -a "${EVIDENCE_FILE}"; }
fingerprint() {
  local dsn="$1"
  psql "${dsn}" -X -A -t -v ON_ERROR_STOP=1 <<'SQL'
SELECT value FROM audit_ledger_metadata WHERE key = 'writer_protocol';
SELECT COUNT(*) || ':' || COALESCE(SUM(prompt_tokens),0) || ':' || COALESCE(SUM(completion_tokens),0) || ':' || COALESCE(SUM(token_count),0) FROM inference_audit;
SELECT COUNT(*) || ':' || COALESCE(SUM(reserved_requests),0) || ':' || COALESCE(SUM(reserved_tokens),0) FROM quota_reservations;
SQL
}

record "START ledger-backup-restore expected_protocol=${EXPECTED_PROTOCOL} rpo_target=5m rto_target=30m"
pg_dump --format=custom --no-owner --no-privileges --file="${DUMP_FILE}" "${INFERA_AUDIT_LEDGER_SOURCE_DSN}"
record "PASS backup-created"
pg_restore --clean --if-exists --no-owner --no-privileges --dbname="${INFERA_AUDIT_LEDGER_RESTORE_DSN}" "${DUMP_FILE}"
record "PASS restore-completed"
SOURCE_FINGERPRINT="$(fingerprint "${INFERA_AUDIT_LEDGER_SOURCE_DSN}")"
RESTORE_FINGERPRINT="$(fingerprint "${INFERA_AUDIT_LEDGER_RESTORE_DSN}")"
[[ "$(printf '%s\n' "${RESTORE_FINGERPRINT}" | sed -n '1p')" == "${EXPECTED_PROTOCOL}" ]] || { record "FAIL writer-protocol"; exit 1; }
[[ "${SOURCE_FINGERPRINT}" == "${RESTORE_FINGERPRINT}" ]] || { record "FAIL accounting-fingerprint"; exit 1; }
record "PASS accounting-fingerprint"
record "COMPLETE sanitized_evidence=${EVIDENCE_FILE}"
