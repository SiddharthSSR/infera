#!/usr/bin/env bash
set -euo pipefail

TARGETS_FILE="${1:-deploy/observability/prometheus/worker_targets.json}"

if [ ! -f "${TARGETS_FILE}" ]; then
  echo "worker target validation failed: file not found: ${TARGETS_FILE}" >&2
  exit 1
fi

python3 - "${TARGETS_FILE}" <<'PY'
import json
import sys
from pathlib import Path

path = Path(sys.argv[1])
try:
    data = json.loads(path.read_text())
except Exception as exc:
    print(f"worker target validation failed: invalid JSON in {path}: {exc}", file=sys.stderr)
    raise SystemExit(1)

if not isinstance(data, list) or not data:
    print(f"worker target validation failed: {path} must be a non-empty JSON array", file=sys.stderr)
    raise SystemExit(1)

for idx, entry in enumerate(data):
    if isinstance(entry, str) and entry.strip():
        continue
    if isinstance(entry, dict):
        targets = entry.get("targets")
        if isinstance(targets, list) and any(isinstance(item, str) and item.strip() for item in targets):
            continue
    print(
        f"worker target validation failed: entry {idx} in {path} must be a target string or object with a non-empty targets list",
        file=sys.stderr,
    )
    raise SystemExit(1)

print(f"worker target validation passed: {path}")
PY
