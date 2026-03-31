#!/usr/bin/env bash
set -euo pipefail

http_port="${INFERA_HTTP_PORT:-8081}"

if [[ -z "${INFERA_WORKER_ADDRESS:-}" ]]; then
  candidate_host="${INFERA_E2E_INGRESS_HOST:-}"
  if [[ -z "${candidate_host}" ]]; then
    candidate_host="${PUBLIC_IP:-${E2E_PUBLIC_IP:-}}"
  fi
  if [[ -n "${candidate_host}" ]]; then
    export INFERA_WORKER_ADDRESS="${candidate_host}:${http_port}"
  fi
fi

enable_jupyter="${INFERA_E2E_ENABLE_JUPYTER:-1}"
if [[ "${enable_jupyter,,}" != "0" && "${enable_jupyter,,}" != "false" && "${enable_jupyter,,}" != "no" ]]; then
  jupyter_port="${JUPYTER_PORT:-8888}"
  python -m jupyterlab \
    --ip=0.0.0.0 \
    --port="${jupyter_port}" \
    --allow-root \
    --ServerApp.token='' \
    --ServerApp.password='' \
    --ServerApp.allow_origin='*' \
    --no-browser \
    >/tmp/jupyter.log 2>&1 &
fi

exec python -m infera_worker.cli
