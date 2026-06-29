#!/usr/bin/env bash
# validate-worker-image-pin.sh - fail fast if the production worker image is not pinned.

set -euo pipefail

worker_image="${1:-${INFERA_WORKER_IMAGE:-}}"

if [[ -z "${worker_image}" ]]; then
  echo "ERROR: INFERA_WORKER_IMAGE is required." >&2
  exit 1
fi

if [[ "${worker_image}" == *@sha256:* ]]; then
  exit 0
fi

image_name="${worker_image##*/}"
if [[ "${image_name}" != *:* ]]; then
  echo "ERROR: INFERA_WORKER_IMAGE must include an explicit tag or digest." >&2
  exit 1
fi

tag="${image_name##*:}"
if [[ -z "${tag}" || "${tag}" == "latest" ]]; then
  echo "ERROR: INFERA_WORKER_IMAGE must be pinned to a non-latest tag or digest." >&2
  exit 1
fi
