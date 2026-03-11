#!/usr/bin/env bash
# backup-sqlite.sh — Safe online backup of Infera SQLite databases.
#
# Uses SQLite's .backup command which creates a consistent snapshot
# without blocking writers. Safe to run while the gateway is serving.
# Without sqlite3, the script falls back to a best-effort live copy.
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
        if sqlite3 "${src}" <<EOF
.backup "${dst}"
EOF
        then
            echo "  OK:   ${db} → ${dst}"
        else
            echo "  FAIL: ${db} — sqlite3 .backup failed"
            FAILED=1
        fi
    else
        # Fallback: best-effort live copy. This does not checkpoint WAL and may be unsafe.
        cp "${src}" "${dst}"
        # Also copy WAL/SHM if they exist
        [ -f "${src}-wal" ] && cp "${src}-wal" "${dst}-wal"
        [ -f "${src}-shm" ] && cp "${src}-shm" "${dst}-shm"
        echo "  WARN: ${db} → ${dst} (live-copy fallback without sqlite3; consistency is not guaranteed)"
    fi
done

# Verify backups are valid SQLite files
for db in "${DBS[@]}"; do
    dst="${BACKUP_SUBDIR}/${db}"
    if [ -f "${dst}" ]; then
        if command -v sqlite3 &> /dev/null; then
            result="$(sqlite3 "${dst}" "PRAGMA integrity_check;" 2>&1 | tr -d '\r' | xargs)"
            if [ "${result}" != "ok" ]; then
                echo "  WARN: ${dst} failed integrity check: ${result}"
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
            for artifact in "${dst}" "${dst}-wal" "${dst}-shm"; do
                if [ -f "${artifact}" ]; then
                    artifact_name="$(basename "${artifact}")"
                    s3_path="s3://${BACKUP_S3_BUCKET}/${S3_PREFIX}/${TIMESTAMP}/${artifact_name}"
                    if aws s3 cp "${artifact}" "${s3_path}" --quiet; then
                        echo "  S3:   ${artifact_name} → ${s3_path}"
                    else
                        echo "  FAIL: S3 upload failed for ${artifact_name}"
                        FAILED=1
                    fi
                fi
            done
        done
    else
        echo "  WARN: aws CLI not found, skipping S3 upload"
    fi
fi

# Prune old local backups (keep last 48 = ~2 days of hourly backups)
KEEP_COUNT="${BACKUP_KEEP_COUNT:-48}"
mapfile -t BACKUP_DIRS < <(
    find "${BACKUP_DIR}" -mindepth 1 -maxdepth 1 -type d -print \
      | grep -E '/20[0-9]{2}(0[1-9]|1[0-2])(0[1-9]|[12][0-9]|3[01])_([01][0-9]|2[0-3])([0-5][0-9])([0-5][0-9])$' \
      | sort
)
BACKUP_COUNT="${#BACKUP_DIRS[@]}"
if [ "${BACKUP_COUNT}" -gt "${KEEP_COUNT}" ]; then
    PRUNE_COUNT=$((BACKUP_COUNT - KEEP_COUNT))
    printf '%s\n' "${BACKUP_DIRS[@]}" | head -n "${PRUNE_COUNT}" | while read -r old; do
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
