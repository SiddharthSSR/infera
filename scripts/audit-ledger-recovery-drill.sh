#!/usr/bin/env bash
# Prove a PostgreSQL audit/quota ledger backup can be restored without exposing DSNs.

set -euo pipefail

: "${INFERA_AUDIT_LEDGER_SOURCE_DSN:?source DSN is required}"
: "${INFERA_AUDIT_LEDGER_RESTORE_DSN:?restore DSN is required}"
EXPECTED_PROTOCOL="${INFERA_AUDIT_LEDGER_WRITER_PROTOCOL:-2}"
EVIDENCE_DIR="${INFERA_RECOVERY_EVIDENCE_DIR:-recovery-evidence}"
TMP_DIR="$(mktemp -d)"
LOCK_READY="${TMP_DIR}/source-lock-ready"
LOCK_RELEASE="${TMP_DIR}/source-lock-release"
LOCK_LOG="${TMP_DIR}/source-lock.log"
SNAPSHOT_FILE="${TMP_DIR}/source-snapshot"
LOCK_PID=""
mkdir -p "${EVIDENCE_DIR}"
EVIDENCE_FILE="${EVIDENCE_DIR}/ledger-restore-$(date -u +%Y%m%dT%H%M%SZ)-$$.log"
DUMP_FILE="${TMP_DIR}/audit-ledger.dump"

cleanup() {
  if [[ -n "${LOCK_PID}" ]]; then
    touch "${LOCK_RELEASE}"
    wait "${LOCK_PID}" >/dev/null 2>&1 || true
  fi
  rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

record() { printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$1" | tee -a "${EVIDENCE_FILE}"; }

database_identity() {
  local dsn="$1"
  psql "${dsn}" -X -A -t -v ON_ERROR_STOP=1 -c \
    "SELECT system_identifier::text || ':' || db.oid::text FROM pg_control_system(), pg_database db WHERE db.datname = current_database();"
}

digest_queries() {
  printf '%s\n' \
    "SELECT 'metadata:' || md5(COALESCE(string_agg(md5(to_jsonb(row_data)::text), '' ORDER BY key), '')) FROM audit_ledger_metadata row_data;" \
    "SELECT 'audit:' || md5(COALESCE(string_agg(md5(to_jsonb(row_data)::text), '' ORDER BY workspace_id, request_id), '')) FROM inference_audit row_data;" \
    "SELECT 'reservations:' || md5(COALESCE(string_agg(md5(to_jsonb(row_data)::text), '' ORDER BY workspace_id, execution_id), '')) FROM quota_reservations row_data;"
}

content_digest() {
  local dsn="$1"
  local snapshot="${2:-}"
  if [[ -n "${snapshot}" ]]; then
    {
      printf '%s\n' "BEGIN ISOLATION LEVEL REPEATABLE READ;" "SET TRANSACTION SNAPSHOT '${snapshot}';"
      digest_queries
      printf '%s\n' "COMMIT;"
    } | psql "${dsn}" -X -q -A -t -v ON_ERROR_STOP=1
  else
    digest_queries | psql "${dsn}" -X -q -A -t -v ON_ERROR_STOP=1
  fi
}

require_source_lock() {
  if [[ -z "${LOCK_PID}" ]] || ! kill -0 "${LOCK_PID}" >/dev/null 2>&1 || [[ ! -f "${LOCK_READY}" ]]; then
    record "FAIL source-quiesce-lost"
    exit 1
  fi
}

if ! SOURCE_IDENTITY="$(database_identity "${INFERA_AUDIT_LEDGER_SOURCE_DSN}")" ||
   ! RESTORE_IDENTITY="$(database_identity "${INFERA_AUDIT_LEDGER_RESTORE_DSN}")"; then
  record "FAIL database-identity-unresolved"
  exit 1
fi
[[ -n "${SOURCE_IDENTITY}" && -n "${RESTORE_IDENTITY}" ]] || { record "FAIL database-identity-unresolved"; exit 1; }
if [[ "${SOURCE_IDENTITY}" == "${RESTORE_IDENTITY}" ]]; then
  record "FAIL restore-target-matches-source"
  exit 1
fi
record "PASS distinct-database-identities"

psql "${INFERA_AUDIT_LEDGER_SOURCE_DSN}" -X -q -A -t -v ON_ERROR_STOP=1 >"${LOCK_LOG}" 2>&1 <<SQL &
BEGIN ISOLATION LEVEL REPEATABLE READ;
SET LOCAL lock_timeout = '30s';
LOCK TABLE audit_ledger_metadata, inference_audit, quota_reservations IN SHARE MODE;
\o '${SNAPSHOT_FILE}'
SELECT pg_export_snapshot();
\o
\! touch '${LOCK_READY}'
\! while [ ! -f '${LOCK_RELEASE}' ]; do sleep 1; done
COMMIT;
SQL
LOCK_PID="$!"
for _ in $(seq 1 30); do
  [[ -f "${LOCK_READY}" ]] && break
  kill -0 "${LOCK_PID}" >/dev/null 2>&1 || { record "FAIL source-quiesce-lock"; exit 1; }
  sleep 1
done
[[ -f "${LOCK_READY}" ]] || { record "FAIL source-quiesce-timeout"; exit 1; }
SOURCE_SNAPSHOT="$(tr -d '[:space:]' <"${SNAPSHOT_FILE}")"
[[ "${SOURCE_SNAPSHOT}" =~ ^[A-Za-z0-9:-]+$ ]] || { record "FAIL source-snapshot-export"; exit 1; }

record "START ledger-backup-restore expected_protocol=${EXPECTED_PROTOCOL} rpo_target=5m rto_target=30m"
pg_dump --format=custom --no-owner --no-privileges --snapshot="${SOURCE_SNAPSHOT}" --file="${DUMP_FILE}" "${INFERA_AUDIT_LEDGER_SOURCE_DSN}"
require_source_lock
SOURCE_DIGEST="$(content_digest "${INFERA_AUDIT_LEDGER_SOURCE_DSN}" "${SOURCE_SNAPSHOT}")"
require_source_lock
touch "${LOCK_RELEASE}"
if ! wait "${LOCK_PID}"; then
  LOCK_PID=""
  record "FAIL source-quiesce-release"
  exit 1
fi
LOCK_PID=""
record "PASS backup-created-from-quiesced-ledger"

if ! CURRENT_RESTORE_IDENTITY="$(database_identity "${INFERA_AUDIT_LEDGER_RESTORE_DSN}")" ||
   [[ "${CURRENT_RESTORE_IDENTITY}" != "${RESTORE_IDENTITY}" ]] ||
   [[ "${CURRENT_RESTORE_IDENTITY}" == "${SOURCE_IDENTITY}" ]]; then
  record "FAIL restore-target-identity-changed"
  exit 1
fi
pg_restore --clean --if-exists --no-owner --no-privileges --dbname="${INFERA_AUDIT_LEDGER_RESTORE_DSN}" "${DUMP_FILE}"
record "PASS restore-completed"
RESTORE_DIGEST="$(content_digest "${INFERA_AUDIT_LEDGER_RESTORE_DSN}")"
[[ "$(printf '%s\n' "${RESTORE_DIGEST}" | sed -n '1p')" == "metadata:"* ]] || { record "FAIL metadata-digest"; exit 1; }
RESTORE_PROTOCOL="$(psql "${INFERA_AUDIT_LEDGER_RESTORE_DSN}" -X -A -t -v ON_ERROR_STOP=1 -c "SELECT value FROM audit_ledger_metadata WHERE key = 'writer_protocol';")"
[[ "${RESTORE_PROTOCOL}" == "${EXPECTED_PROTOCOL}" ]] || { record "FAIL writer-protocol"; exit 1; }
[[ "${SOURCE_DIGEST}" == "${RESTORE_DIGEST}" ]] || { record "FAIL accounting-content-digest"; exit 1; }
record "PASS accounting-content-digest"
record "COMPLETE sanitized_evidence=${EVIDENCE_FILE}"
