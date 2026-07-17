#!/usr/bin/env bash
# Terminate only the exact release-owned RunPod workers before a coordinated transition.

set -euo pipefail

MANIFEST="${1:?usage: runpod-stop-workers.sh <release.manifest>}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=recovery-adapter-common.sh
source "${SCRIPT_DIR}/recovery-adapter-common.sh"

RELEASE_ID="$(recovery_manifest_value "${MANIFEST}" INFERA_RELEASE_ID)"
RUNPOD_KEY="$(recovery_env_value RUNPOD_API_KEY)"
RUNPOD_CONFIG="$(recovery_bearer_config "${RUNPOD_KEY}")"
trap 'rm -f "${RUNPOD_CONFIG}"' EXIT

TERMINATED="$(recovery_runpod_remove_named_pods "infera-release-${RELEASE_ID}" "${RUNPOD_CONFIG}")"
echo "RunPod workers terminated for release ${RELEASE_ID}: ${TERMINATED}"
