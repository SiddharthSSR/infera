#!/usr/bin/env bash
# check-production-env-source.sh - proactively verify the production runtime
# environment source exists and satisfies the required-name contract.
#
# The 2026-07-31 outage was caused by /etc/infera/production.env never being
# materialized (the one-time materialization step was skipped) while the
# recovery tooling that depends on it had already shipped. The failure only
# surfaced during an emergency recovery, when it was most costly. Run this from
# monitoring/CI so a missing or drifted production env source is caught early.
#
# Exit codes:
#   0  present and contract-complete
#   2  no env source configured (missing INFERA_PRODUCTION_ENV_FILE and pointer)
#   3  configured env source file is absent (materialization not run / removed)
#   >0 present but failed contract validation (propagated from the validator)
#
# Any extra flags (e.g. --expected-uid) are forwarded to the validator so the
# same check runs under test as a non-root user and in production as root.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
POINTER="${REPO_ROOT}/deploy/production/infera-production-env-source"

env_file="${INFERA_PRODUCTION_ENV_FILE:-}"
if [[ -z "${env_file}" && -f "${POINTER}" ]]; then
  env_file="$(sed -n 's/^INFERA_PRODUCTION_ENV_FILE=//p' "${POINTER}" | head -n1)"
fi

if [[ -z "${env_file}" ]]; then
  echo "ERROR: no production env source configured (set INFERA_PRODUCTION_ENV_FILE or add ${POINTER})." >&2
  exit 2
fi

if [[ ! -e "${env_file}" ]]; then
  cat >&2 <<EOF
ERROR: production env source is missing: ${env_file}
The one-time production environment materialization has not been run (or the
file was removed). Emergency recovery fails closed until it exists.
Remediation: follow "One-time production environment materialization" in
docs/operations/deployment-recovery.md to install a contract-complete
${env_file} from the approved secret export.
EOF
  exit 3
fi

# Delegate the full contract validation (ownership, mode, required names,
# values hidden) to the canonical validator; forward any flags to it.
INFERA_PRODUCTION_ENV_FILE="${env_file}" \
  python3 "${SCRIPT_DIR}/production-env-source.py" validate "$@"
