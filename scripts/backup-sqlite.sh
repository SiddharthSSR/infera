#!/usr/bin/env bash
# backup-sqlite.sh — Safe online backup of Infera SQLite databases.
#
# Uses SQLite's .backup command which creates a consistent snapshot
# without blocking writers. Safe to run while the gateway is serving.
#
# Usage:
#   ./scripts/backup-sqlite.sh                    # backup to ./backups/
#   ./scripts/backup-sqlite.sh /path/to/backups   # backup to custom dir
#   BACKUP_S3_BUCKET=my-bucket ./scripts/backup-sqlite.sh  # also upload to S3
#
# Cron example (every hour):
#   0 * * * * cd /app && ./scripts/backup-sqlite.sh >> /var/log/infera-backup.log 2>&1

set -euo pipefail

DATA_DIR="${DATA_DIR:-./data}"
BACKUP_DIR="${1:-./backups}"
TIMESTAMP=$(date -u +"%Y%m%d_%H%M%S")
BACKUP_SUBDIR="${BACKUP_DIR}/${TIMESTAMP}"

# Databases to back up
DBS=("auth.db" "vault.db" "costs.db")

# Ensure backup directory exists
mkdir -p "${BACKUP_SUBDIR}"

echo "[$(date -u +%Y-%m-%dT%H:%M:%SZ)] Starting SQLite backup..."

FAILED=0
for db in "${DBS[@]}"; do
    src="${DATA_DIR}/${db}"
    dst="${BACKUP_SUBDIR}/${db}"

    if [ ! -f "${src}" ]; then
        echo "  SKIP: ${src} does not exist"
        continue
    fi

    # Use SQLite .backup for online-safe copy
    if command -v sqlite3 &> /dev/null; then
        if sqlite3 "${src}" ".backup '${dst}'"; then
            echo "  OK:   ${db} → ${dst}"
        else
            echo "  FAIL: ${db} — sqlite3 .backup failed"
            FAILED=1
        fi
    else
        # Fallback: cp with WAL checkpoint first
        # This is less safe but works without sqlite3 binary
        cp "${src}" "${dst}"
        # Also copy WAL/SHM if they exist
        [ -f "${src}-wal" ] && cp "${src}-wal" "${dst}-wal"
        [ -f "${src}-shm" ] && cp "${src}-shm" "${dst}-shm"
        echo "  OK:   ${db} → ${dst} (cp fallback, sqlite3 not found)"
    fi
done

# Verify backups are valid SQLite files
for db in "${DBS[@]}"; do
    dst="${BACKUP_SUBDIR}/${db}"
    if [ -f "${dst}" ]; then
        if command -v sqlite3 &> /dev/null; then
            if ! sqlite3 "${dst}" "PRAGMA integrity_check;" > /dev/null 2>&1; then
                echo "  WARN: ${dst} failed integrity check"
                FAILED=1
            fi
        fi
    fi
done

# Upload to S3 if bucket is configured
if [ -n "${BACKUP_S3_BUCKET:-}" ]; then
    S3_PREFIX="${BACKUP_S3_PREFIX:-infera/backups}"
    if command -v aws &> /dev/null; then
        for db in "${DBS[@]}"; do
            dst="${BACKUP_SUBDIR}/${db}"
            if [ -f "${dst}" ]; then
                s3_path="s3://${BACKUP_S3_BUCKET}/${S3_PREFIX}/${TIMESTAMP}/${db}"
                if aws s3 cp "${dst}" "${s3_path}" --quiet; then
                    echo "  S3:   ${db} → ${s3_path}"
                else
                    echo "  FAIL: S3 upload failed for ${db}"
                    FAILED=1
                fi
            fi
        done
    else
        echo "  WARN: aws CLI not found, skipping S3 upload"
    fi
fi

# Prune old local backups (keep last 48 = ~2 days of hourly backups)
KEEP_COUNT="${BACKUP_KEEP_COUNT:-48}"
BACKUP_COUNT=$(ls -1d "${BACKUP_DIR}"/20* 2>/dev/null | wc -l | tr -d ' ')
if [ "${BACKUP_COUNT}" -gt "${KEEP_COUNT}" ]; then
    PRUNE_COUNT=$((BACKUP_COUNT - KEEP_COUNT))
    ls -1d "${BACKUP_DIR}"/20* | head -n "${PRUNE_COUNT}" | while read -r old; do
        rm -rf "${old}"
        echo "  PRUNE: ${old}"
    done
fi

if [ "${FAILED}" -eq 0 ]; then
    echo "[$(date -u +%Y-%m-%dT%H:%M:%SZ)] Backup completed successfully → ${BACKUP_SUBDIR}"
else
    echo "[$(date -u +%Y-%m-%dT%H:%M:%SZ)] Backup completed with errors"
    exit 1
fi
