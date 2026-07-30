#!/usr/bin/env bash
# Reload the reviewed Prometheus SLO rules with fail-closed semantic rollback.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
# shellcheck disable=SC1091
source "${SCRIPT_DIR}/recovery-adapter-common.sh"

COMPOSE_FILE="${COMPOSE_FILE:-${REPO_ROOT}/docker-compose.prod.yml}"
SOURCE_ROOT="${INFERA_PROMETHEUS_SOURCE_ROOT:-${REPO_ROOT}}"
SOURCE_REVISION="${INFERA_PROMETHEUS_SOURCE_REVISION:?INFERA_PROMETHEUS_SOURCE_REVISION is required}"
CONFIG_FILE="${INFERA_PROMETHEUS_CONFIG_FILE:-${REPO_ROOT}/deploy/observability/prometheus/prometheus.yml}"
RULES_DIR="${INFERA_PROMETHEUS_RULES_DIR:-${REPO_ROOT}/deploy/observability/prometheus/rules}"
ALERTS_FILE="${RULES_DIR}/infera-alerts.yml"
SLO_FILE="${RULES_DIR}/infera-slo-v1.yml"
ROLLBACK_FILE="${RULES_DIR}/rollback/infera-alerts.pre-slo-v1.yml"
RETAINED_SLO_FILE="${RULES_DIR}/rollback/infera-slo-v1.candidate.disabled"
PRIVATE_ROOT="${INFERA_PROMETHEUS_PRIVATE_ROOT:-${REPO_ROOT}/.infera-recovery/prometheus-reload}"
EVIDENCE_DIR="${INFERA_PROMETHEUS_EVIDENCE_DIR:?INFERA_PROMETHEUS_EVIDENCE_DIR is required}"
BASE_URL="${INFERA_BASE_URL:?INFERA_BASE_URL is required}"
VERIFY_ATTEMPTS="${INFERA_PROMETHEUS_VERIFY_ATTEMPTS:-6}"
VERIFY_INTERVAL="${INFERA_PROMETHEUS_VERIFY_INTERVAL_SECONDS:-5}"

readonly EXPECTED_IMAGE_REF="prom/prometheus:v2.55.1"
readonly EXPECTED_IMAGE_ID="sha256:2659f4c2ebb718e7695cb9b25ffa7d6be64db013daba13e05c875451cf51b0d3"
readonly EXPECTED_CONFIG_SHA256="10b5521ca8a8f0c9c8113ed70ce6d6cf863a07619d3a4efa6f1a4072841d4b53"
readonly EXPECTED_ALERTS_SHA256="053732934fa41f2b59cfe0b73a1a1c7fb839f46527fa4cb15d6df14c3575419f"
readonly EXPECTED_SLO_SHA256="cb2464e859dfd672e998fe3270d0008a52fd39c1657dbd1de4980d593ec390a2"
readonly EXPECTED_ROLLBACK_SHA256="f5b7774f0611bfb89f9d9ad56eabf6b67fc645cda52a4d4c12690a11b89e644b"
readonly CONTAINER_CONFIG="/etc/prometheus/prometheus.yml"
readonly CONTAINER_ALERTS="/etc/prometheus/rules/infera-alerts.yml"
readonly CONTAINER_SLO="/etc/prometheus/rules/infera-slo-v1.yml"
readonly CONTAINER_ROLLBACK="/etc/prometheus/rules/rollback/infera-alerts.pre-slo-v1.yml"
readonly EXPECTED_FIRING_ALERTS="InferaZeroHealthyWorkers"

[[ "${VERIFY_ATTEMPTS}" =~ ^[1-9][0-9]*$ && "${VERIFY_ATTEMPTS}" -le 30 ]] || {
  echo "ERROR: INFERA_PROMETHEUS_VERIFY_ATTEMPTS must be 1 through 30" >&2
  exit 2
}
[[ "${VERIFY_INTERVAL}" =~ ^[0-9]+$ && "${VERIFY_INTERVAL}" -le 30 ]] || {
  echo "ERROR: INFERA_PROMETHEUS_VERIFY_INTERVAL_SECONDS must be 0 through 30" >&2
  exit 2
}
recovery_https_host "${BASE_URL}" >/dev/null || {
  echo "ERROR: INFERA_BASE_URL must be a canonical HTTPS origin" >&2
  exit 2
}
[[ "${SOURCE_REVISION}" =~ ^[0-9a-f]{40}$ ]] || {
  echo "ERROR: INFERA_PROMETHEUS_SOURCE_REVISION must be a full lowercase Git SHA" >&2
  exit 2
}
[[ -d "${SOURCE_ROOT}" && ! -L "${SOURCE_ROOT}" ]] || {
  echo "ERROR: Prometheus source root must be a real directory" >&2
  exit 2
}
if [[ "$(git -C "${SOURCE_ROOT}" rev-parse HEAD)" != "${SOURCE_REVISION}" ||
  -n "$(git -C "${SOURCE_ROOT}" status --porcelain --untracked-files=all)" ]]; then
  echo "ERROR: Prometheus source must match the requested clean revision" >&2
  exit 1
fi

umask 077
STAGE_DIR=""
LOCK_DIR=""
EVIDENCE_FILE=""
PROMETHEUS_ID=""
BASE_RESTART_COUNT=""
BASE_CONFIG_TIME=""
RULES_TOUCHED=0
DISABLED_SLO=""
EXIT_RECORDED=0

record_evidence() {
  local event="$1" result="$2" reason="$3"
  [[ -n "${EVIDENCE_FILE}" ]] || return 0
  python3 - "${EVIDENCE_FILE}" "${event}" "${result}" "${reason}" "${SOURCE_REVISION}" <<'PY'
import datetime
import os
import re
import stat
import sys

path, event, result, reason, source_revision = sys.argv[1:]
if event not in {"preflight", "stage", "reload", "verify", "rollback", "cleanup"}:
    raise SystemExit(1)
if result not in {"start", "pass", "fail"}:
    raise SystemExit(1)
if reason not in {
    "none", "container_count", "baseline", "hash_drift", "image_drift",
    "lifecycle_disabled", "promtool", "target_loss", "ingress_open",
    "workers_present", "reload_rejected", "semantic_failure",
    "rollback_reload", "rollback_verify", "restore_files", "residue",
}:
    raise SystemExit(1)
if not re.fullmatch(r"[a-z_]+", event + result + reason):
    raise SystemExit(1)
if not re.fullmatch(r"[0-9a-f]{40}", source_revision):
    raise SystemExit(1)
flags = os.O_WRONLY | os.O_APPEND
if not hasattr(os, "O_NOFOLLOW"):
    raise SystemExit(1)
fd = os.open(path, flags | os.O_NOFOLLOW)
try:
    info = os.fstat(fd)
    if not stat.S_ISREG(info.st_mode) or stat.S_IMODE(info.st_mode) != 0o600:
        raise SystemExit(1)
    timestamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    line = (
        f"{timestamp} PROMETHEUS_RELOAD event={event} result={result} "
        f"reason={reason} source_revision={source_revision}\n"
    )
    os.write(fd, line.encode("ascii"))
finally:
    os.close(fd)
PY
}

fail() {
  local event="$1" reason="$2" message="$3"
  record_evidence "${event}" fail "${reason}" || true
  EXIT_RECORDED=1
  echo "ERROR: ${message}" >&2
  return 1
}

sha256_file() {
  sha256sum "$1" | awk '{print $1}'
}

container_sha256() {
  docker exec "${PROMETHEUS_ID}" sha256sum "$1" | awk '{print $1}'
}

restore_forward_files() {
  local status=0 temp_alerts=""
  [[ "${RULES_TOUCHED}" == "1" ]] || return 0
  temp_alerts="${RULES_DIR}/.infera-alerts.forward-$$"
  if ! install -m 0644 "${STAGE_DIR}/forward/infera-alerts.yml" "${temp_alerts}" ||
    ! mv -f "${temp_alerts}" "${ALERTS_FILE}"; then
    status=1
  fi
  if [[ -n "${DISABLED_SLO}" && -e "${DISABLED_SLO}" ]]; then
    if ! mv -f "${DISABLED_SLO}" "${SLO_FILE}"; then
      status=1
    fi
  elif [[ ! -e "${SLO_FILE}" ]]; then
    if ! install -m 0644 "${STAGE_DIR}/forward/infera-slo-v1.yml" "${SLO_FILE}"; then
      status=1
    fi
  fi
  rm -f "${temp_alerts}"
  RULES_TOUCHED=0
  [[ "${status}" == "0" ]]
}

cleanup() {
  local status="$?"
  trap - EXIT
  if ! restore_forward_files; then
    record_evidence cleanup fail restore_files || true
    status=1
  fi
  if [[ -n "${STAGE_DIR}" && -d "${STAGE_DIR}" ]]; then
    rm -rf "${STAGE_DIR}"
  fi
  if [[ -n "${LOCK_DIR}" && -d "${LOCK_DIR}" ]]; then
    rmdir "${LOCK_DIR}" 2>/dev/null || status=1
  fi
  if [[ "${status}" == "0" ]]; then
    record_evidence cleanup pass none || status=1
  elif [[ "${EXIT_RECORDED}" == "0" ]]; then
    record_evidence cleanup fail residue || true
  fi
  exit "${status}"
}
trap cleanup EXIT

prometheus_get() {
  docker exec "${PROMETHEUS_ID}" wget -qO- "http://127.0.0.1:9090$1"
}

prometheus_reload() {
  docker exec "${PROMETHEUS_ID}" wget --post-data='' -qO- \
    http://127.0.0.1:9090/-/reload >/dev/null
}

config_time() {
  prometheus_get /api/v1/status/runtimeinfo | python3 -c \
    'import json,sys; print(json.load(sys.stdin)["data"]["lastConfigTime"])'
}

time_advanced() {
  python3 - "$1" "$2" <<'PY'
import datetime
import sys

def parse(value):
    return datetime.datetime.fromisoformat(value.replace("Z", "+00:00"))

raise SystemExit(0 if parse(sys.argv[2]) > parse(sys.argv[1]) else 1)
PY
}

container_unchanged() {
  [[ "$(docker inspect --format '{{.RestartCount}}' "${PROMETHEUS_ID}")" == "${BASE_RESTART_COUNT}" ]] &&
    [[ "$(production_compose -f "${COMPOSE_FILE}" ps -aq prometheus)" == "${PROMETHEUS_ID}" ]]
}

verify_gateway_zero_workers() {
  local ids_output gateway_id body count=0
  local gateway_ids=()
  ids_output="$(production_compose -f "${COMPOSE_FILE}" ps -q gateway)" || return 1
  while IFS= read -r gateway_id; do
    [[ -n "${gateway_id}" ]] && gateway_ids[${#gateway_ids[@]}]="${gateway_id}"
  done <<<"${ids_output}"
  [[ "${#gateway_ids[@]}" -eq 2 ]] || return 1
  for gateway_id in "${gateway_ids[@]}"; do
    body="$(docker exec "${gateway_id}" wget -qO- http://127.0.0.1:8080/health)" || return 1
    HEALTH_BODY="${body}" python3 - <<'PY' || return 1
import json
import os

payload = json.loads(os.environ["HEALTH_BODY"])
if payload.get("workers") != 0 or payload.get("healthy_workers") != 0:
    raise SystemExit(1)
PY
    count=$((count + 1))
  done
  [[ "${count}" -eq 2 ]]
}

verify_snapshot() {
  local mode="$1" rules targets alerts
  rules="$(prometheus_get /api/v1/rules)" || return 1
  targets="$(prometheus_get /api/v1/targets)" || return 1
  alerts="$(prometheus_get /api/v1/alerts)" || return 1
  MODE="${mode}" RULES="${rules}" TARGETS="${targets}" ALERTS="${alerts}" \
    EXPECTED_FIRING="${EXPECTED_FIRING_ALERTS}" python3 - <<'PY'
import json
import os

mode = os.environ["MODE"]
rules = json.loads(os.environ["RULES"])["data"]["groups"]
targets = json.loads(os.environ["TARGETS"])["data"]
alerts = json.loads(os.environ["ALERTS"])["data"]["alerts"]

expected_groups = {
    "baseline": {
        "infera-gateway-alerts": 9,
        "infera-worker-alerts": 1,
    },
    "forward": {
        "infera-gateway-alerts": 7,
        "infera-worker-alerts": 1,
        "infera-slo-v1-recording": 32,
        "infera-slo-v1-alerts": 4,
    },
}[mode]
actual_groups = {group.get("name"): len(group.get("rules", [])) for group in rules}
if actual_groups != expected_groups:
    raise SystemExit(1)
all_rules = [rule for group in rules for rule in group.get("rules", [])]
if any(rule.get("health") != "ok" for rule in all_rules):
    raise SystemExit(1)
expected_total = 10 if mode == "baseline" else 44
if len(all_rules) != expected_total:
    raise SystemExit(1)

active = targets.get("activeTargets", [])
if len(active) != 2 or targets.get("droppedTargets", []):
    raise SystemExit(1)
if any(target.get("health") != "up" for target in active):
    raise SystemExit(1)
jobs = sorted(target.get("labels", {}).get("job") for target in active)
if jobs != ["infera_gateway", "prometheus"]:
    raise SystemExit(1)

firing = sorted(
    alert.get("labels", {}).get("alertname")
    for alert in alerts
    if alert.get("state") == "firing"
)
expected_firing = sorted(filter(None, os.environ["EXPECTED_FIRING"].split(",")))
if firing != expected_firing:
    raise SystemExit(1)
PY
}

verify_common_runtime() {
  docker exec "${PROMETHEUS_ID}" wget -qO- http://127.0.0.1:9090/-/healthy >/dev/null &&
    docker exec "${PROMETHEUS_ID}" wget -qO- http://127.0.0.1:9090/-/ready >/dev/null &&
    container_unchanged
}

verify_with_retries() {
  local mode="$1" attempt
  for ((attempt = 1; attempt <= VERIFY_ATTEMPTS; attempt++)); do
    if verify_common_runtime && verify_snapshot "${mode}"; then
      return 0
    fi
    if (( attempt < VERIFY_ATTEMPTS && VERIFY_INTERVAL > 0 )); then
      sleep "${VERIFY_INTERVAL}"
    fi
  done
  return 1
}

perform_semantic_rollback() {
  local reason="$1" rollback_temp="" rollback_base_time="" rollback_config_time=""
  record_evidence rollback start "${reason}"
  rollback_temp="${RULES_DIR}/.infera-alerts.rollback-$$"
  DISABLED_SLO="${RETAINED_SLO_FILE}"
  RULES_TOUCHED=1
  if ! install -m 0644 "${STAGE_DIR}/rollback/infera-alerts.yml" "${rollback_temp}" ||
    ! mv -f "${SLO_FILE}" "${DISABLED_SLO}" ||
    ! mv -f "${rollback_temp}" "${ALERTS_FILE}"; then
    rm -f "${rollback_temp}"
    fail rollback restore_files "unable to stage the reviewed rollback rules" || true
    return 1
  fi
  chmod 0600 "${DISABLED_SLO}"
  RULES_TOUCHED=0
  rollback_base_time="$(config_time)" || {
    fail rollback rollback_reload "rollback config time could not be read; ingress remains drained" || true
    return 1
  }
  if ! prometheus_reload; then
    fail rollback rollback_reload "rollback reload was rejected; ingress remains drained" || true
    return 1
  fi
  rollback_config_time="$(config_time)" || true
  if [[ -z "${rollback_config_time}" ]] ||
    ! time_advanced "${rollback_base_time}" "${rollback_config_time}" ||
    ! verify_with_retries baseline; then
    fail rollback rollback_verify "rollback verification failed; ingress remains drained" || true
    return 1
  fi
  if [[ "$(sha256_file "${ALERTS_FILE}")" != "${EXPECTED_ROLLBACK_SHA256}" ||
    -e "${SLO_FILE}" ||
    "$(sha256_file "${RETAINED_SLO_FILE}")" != "${EXPECTED_SLO_SHA256}" ||
    "$(container_sha256 "${CONTAINER_ALERTS}")" != "${EXPECTED_ROLLBACK_SHA256}" ]]; then
    fail rollback restore_files "reviewed rollback files were not retained exactly" || true
    return 1
  fi
  python3 - "${RETAINED_SLO_FILE}" <<'PY' || {
import os
import stat
import sys

info = os.lstat(sys.argv[1])
if not stat.S_ISREG(info.st_mode) or stat.S_IMODE(info.st_mode) != 0o600:
    raise SystemExit(1)
PY
    fail rollback restore_files "retained candidate SLO file is not private" || true
    return 1
  }
  DISABLED_SLO=""
  record_evidence rollback pass none
  return 0
}

[[ ! -L "${EVIDENCE_DIR}" && ! -L "${PRIVATE_ROOT}" ]] || {
  echo "ERROR: private directory paths cannot be symlinks" >&2
  exit 1
}
mkdir -p "${EVIDENCE_DIR}" "${PRIVATE_ROOT}"
chmod 0700 "${EVIDENCE_DIR}" "${PRIVATE_ROOT}"
[[ -d "${EVIDENCE_DIR}" && ! -L "${EVIDENCE_DIR}" && -d "${PRIVATE_ROOT}" && ! -L "${PRIVATE_ROOT}" ]] || {
  echo "ERROR: private directories must be real directories" >&2
  exit 1
}
if [[ -n "$(find "${EVIDENCE_DIR}" -mindepth 1 -print -quit)" ||
  -n "$(find "${PRIVATE_ROOT}" -mindepth 1 -print -quit)" ]]; then
  echo "ERROR: private directories must be empty for a new reload attempt" >&2
  exit 1
fi
python3 - "${CONFIG_FILE}" "${ALERTS_FILE}" "${SLO_FILE}" "${ROLLBACK_FILE}" <<'PY' || {
import os
import stat
import sys

for path in sys.argv[1:]:
    info = os.lstat(path)
    if not stat.S_ISREG(info.st_mode):
        raise SystemExit(1)
PY
  echo "ERROR: reviewed Prometheus inputs must be regular files" >&2
  exit 1
}
[[ ! -e "${RETAINED_SLO_FILE}" && ! -L "${RETAINED_SLO_FILE}" ]] || {
  echo "ERROR: a retained semantic rollback already exists" >&2
  exit 1
}
EVIDENCE_FILE="${EVIDENCE_DIR}/prometheus-reload-$(date -u +%Y%m%dT%H%M%SZ)-$$.log"
python3 "${SCRIPT_DIR}/create-private-evidence.py" "${EVIDENCE_FILE}"
record_evidence preflight start none

LOCK_DIR="${PRIVATE_ROOT}/reload.lock"
if ! mkdir "${LOCK_DIR}"; then
  fail preflight baseline "another Prometheus reload is already active"
  exit 1
fi
chmod 0700 "${LOCK_DIR}"

STAGE_DIR="$(mktemp -d "${PRIVATE_ROOT}/stage.XXXXXX")"
chmod 0700 "${STAGE_DIR}"
mkdir -m 0700 "${STAGE_DIR}/forward" "${STAGE_DIR}/rollback"
python3 - "${RULES_DIR}" "${STAGE_DIR}" <<'PY'
import os
import sys

active = os.path.realpath(sys.argv[1])
stage = os.path.realpath(sys.argv[2])
if os.path.commonpath([active, stage]) == active:
    raise SystemExit("rollback stage must be outside the active rule directory")
PY

PROMETHEUS_IDS=()
while IFS= read -r prometheus_id; do
  [[ -n "${prometheus_id}" ]] && PROMETHEUS_IDS[${#PROMETHEUS_IDS[@]}]="${prometheus_id}"
done < <(production_compose -f "${COMPOSE_FILE}" ps -aq prometheus)
if [[ "${#PROMETHEUS_IDS[@]}" -ne 1 || -z "${PROMETHEUS_IDS[0]}" ]]; then
  fail preflight container_count "exactly one Prometheus Compose container is required"
  exit 1
fi
PROMETHEUS_ID="${PROMETHEUS_IDS[0]}"
BASE_RESTART_COUNT="$(docker inspect --format '{{.RestartCount}}' "${PROMETHEUS_ID}")"
[[ "$(docker inspect --format '{{.State.Running}}' "${PROMETHEUS_ID}")" == "true" &&
  "$(docker inspect --format '{{.State.Restarting}}' "${PROMETHEUS_ID}")" == "false" ]] || {
  fail preflight baseline "Prometheus must be running and stable"
  exit 1
}
[[ "$(docker inspect --format '{{.Config.Image}}' "${PROMETHEUS_ID}")" == "${EXPECTED_IMAGE_REF}" &&
  "$(docker inspect --format '{{.Image}}' "${PROMETHEUS_ID}")" == "${EXPECTED_IMAGE_ID}" ]] || {
  fail preflight image_drift "Prometheus image identity does not match the reviewed immutable image"
  exit 1
}
docker inspect --format '{{json .Args}}' "${PROMETHEUS_ID}" |
  python3 -c 'import json,sys; raise SystemExit(0 if "--web.enable-lifecycle" in json.load(sys.stdin) else 1)' || {
  fail preflight lifecycle_disabled "Prometheus lifecycle reload is not enabled"
  exit 1
}
FLAGS="$(prometheus_get /api/v1/status/flags)" || {
  fail preflight lifecycle_disabled "Prometheus flags endpoint is unavailable"
  exit 1
}
if ! FLAGS="${FLAGS}" python3 - <<'PY'
import json
import os

payload = json.loads(os.environ["FLAGS"])
if payload.get("data", {}).get("web.enable-lifecycle") != "true":
    raise SystemExit(1)
PY
then
  fail preflight lifecycle_disabled "Prometheus lifecycle reload is disabled at runtime"
  exit 1
fi

if [[ "$(sha256_file "${CONFIG_FILE}")" != "${EXPECTED_CONFIG_SHA256}" ||
  "$(sha256_file "${ALERTS_FILE}")" != "${EXPECTED_ALERTS_SHA256}" ||
  "$(sha256_file "${SLO_FILE}")" != "${EXPECTED_SLO_SHA256}" ||
  "$(sha256_file "${ROLLBACK_FILE}")" != "${EXPECTED_ROLLBACK_SHA256}" ||
  "$(container_sha256 "${CONTAINER_CONFIG}")" != "${EXPECTED_CONFIG_SHA256}" ||
  "$(container_sha256 "${CONTAINER_ALERTS}")" != "${EXPECTED_ALERTS_SHA256}" ||
  "$(container_sha256 "${CONTAINER_SLO}")" != "${EXPECTED_SLO_SHA256}" ||
  "$(container_sha256 "${CONTAINER_ROLLBACK}")" != "${EXPECTED_ROLLBACK_SHA256}" ]]; then
  fail preflight hash_drift "mounted Prometheus files do not match the reviewed hashes"
  exit 1
fi

if ! docker exec "${PROMETHEUS_ID}" promtool check config "${CONTAINER_CONFIG}" >/dev/null ||
  ! docker exec "${PROMETHEUS_ID}" promtool check rules \
    "${CONTAINER_ALERTS}" "${CONTAINER_SLO}" "${CONTAINER_ROLLBACK}" >/dev/null; then
  fail preflight promtool "Prometheus config or rule validation failed"
  exit 1
fi
record_evidence stage start none
install -m 0600 "${ALERTS_FILE}" "${STAGE_DIR}/forward/infera-alerts.yml"
install -m 0600 "${SLO_FILE}" "${STAGE_DIR}/forward/infera-slo-v1.yml"
install -m 0600 "${ROLLBACK_FILE}" "${STAGE_DIR}/rollback/infera-alerts.yml"
[[ "$(sha256_file "${STAGE_DIR}/forward/infera-alerts.yml")" == "${EXPECTED_ALERTS_SHA256}" &&
  "$(sha256_file "${STAGE_DIR}/forward/infera-slo-v1.yml")" == "${EXPECTED_SLO_SHA256}" &&
  "$(sha256_file "${STAGE_DIR}/rollback/infera-alerts.yml")" == "${EXPECTED_ROLLBACK_SHA256}" ]] || {
  fail stage hash_drift "private rollback staging changed reviewed content"
  exit 1
}
record_evidence stage pass none

if ! verify_common_runtime || ! verify_snapshot baseline; then
  fail preflight target_loss "Prometheus baseline health, targets, rules, or alert continuity failed"
  exit 1
fi
if ! verify_gateway_zero_workers; then
  fail preflight workers_present "both gateway replicas must report zero workers"
  exit 1
fi
PUBLIC_STATUS="$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' \
  --max-time 15 "${BASE_URL}/health")"
if [[ "${PUBLIC_STATUS}" != "503" ]]; then
  fail preflight ingress_open "public ingress must remain drained"
  exit 1
fi
BASE_CONFIG_TIME="$(config_time)"
record_evidence preflight pass none

record_evidence reload start none
if ! prometheus_reload; then
  if verify_common_runtime && verify_snapshot baseline && [[ "$(config_time)" == "${BASE_CONFIG_TIME}" ]]; then
    fail reload reload_rejected "Prometheus rejected the reload and retained its last good rules"
    exit 1
  fi
  perform_semantic_rollback semantic_failure || true
  fail reload semantic_failure "reload outcome was ambiguous and the candidate was rejected"
  exit 1
fi
record_evidence reload pass none

POST_CONFIG_TIME="$(config_time)"
if ! time_advanced "${BASE_CONFIG_TIME}" "${POST_CONFIG_TIME}" ||
  ! verify_with_retries forward; then
  perform_semantic_rollback semantic_failure || true
  fail verify semantic_failure "candidate rules failed semantic verification and were rolled back"
  exit 1
fi
record_evidence verify pass none
EXIT_RECORDED=1
echo "Prometheus SLO reload verified."
