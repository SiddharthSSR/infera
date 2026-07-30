#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
TEST_ROOT="$(mktemp -d)"
trap 'rm -rf "${TEST_ROOT}"' EXIT

readonly CONFIG_SHA="10b5521ca8a8f0c9c8113ed70ce6d6cf863a07619d3a4efa6f1a4072841d4b53"
readonly ALERTS_SHA="053732934fa41f2b59cfe0b73a1a1c7fb839f46527fa4cb15d6df14c3575419f"
readonly SLO_SHA="cb2464e859dfd672e998fe3270d0008a52fd39c1657dbd1de4980d593ec390a2"
readonly ROLLBACK_SHA="f5b7774f0611bfb89f9d9ad56eabf6b67fc645cda52a4d4c12690a11b89e644b"
readonly IMAGE_ID="sha256:2659f4c2ebb718e7695cb9b25ffa7d6be64db013daba13e05c875451cf51b0d3"

fail_test() {
  echo "FAIL: $*" >&2
  exit 1
}

portable_mode() {
  if stat -f '%Lp' "$1" >/dev/null 2>&1; then
    stat -f '%Lp' "$1"
  else
    stat -c '%a' "$1"
  fi
}

portable_sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  else
    shasum -a 256 "$1" | awk '{print $1}'
  fi
}

write_state() {
  printf '%s\n' "$1" >"${MOCK_STATE_DIR}/state"
}

read_state() {
  tr -d '\n' <"${MOCK_STATE_DIR}/state"
}

increment_reload_count() {
  local count
  count="$(tr -d '\n' <"${MOCK_STATE_DIR}/reload-count")"
  count=$((count + 1))
  printf '%s\n' "${count}" >"${MOCK_STATE_DIR}/reload-count"
  printf '%s\n' "${count}"
}

mock_rules_json() {
  local state="$1"
  python3 - "${state}" <<'PY'
import json
import sys

state = sys.argv[1]
counts = {
    "baseline": {
        "infera-gateway-alerts": 9,
        "infera-worker-alerts": 1,
    },
    "rollback": {
        "infera-gateway-alerts": 9,
        "infera-worker-alerts": 1,
    },
    "forward": {
        "infera-gateway-alerts": 7,
        "infera-worker-alerts": 1,
        "infera-slo-v1-recording": 32,
        "infera-slo-v1-alerts": 4,
    },
    "semantic_bad": {
        "infera-gateway-alerts": 7,
        "infera-worker-alerts": 1,
        "infera-slo-v1-recording": 31,
        "infera-slo-v1-alerts": 4,
    },
}[state]
groups = [
    {
        "name": name,
        "rules": [{"health": "ok"} for _ in range(count)],
    }
    for name, count in counts.items()
]
print(json.dumps({"status": "success", "data": {"groups": groups}}))
PY
}

mock_targets_json() {
  local health="up"
  [[ "${MOCK_SCENARIO}" == "target_loss" ]] && health="down"
  HEALTH="${health}" python3 - <<'PY'
import json
import os

health = os.environ["HEALTH"]
print(json.dumps({
    "status": "success",
    "data": {
        "activeTargets": [
            {"health": health, "labels": {"job": "infera_gateway"}},
            {"health": "up", "labels": {"job": "prometheus"}},
        ],
        "droppedTargets": [],
    },
}))
PY
}

docker() {
  local command="${1:-}"
  shift || true
  case "${command}" in
    compose)
      local service=""
      while [[ "$#" -gt 0 ]]; do
        service="$1"
        shift
      done
      case "${service}" in
        prometheus)
          case "${MOCK_SCENARIO}" in
            no_container) return 0 ;;
            multiple_containers) printf 'prometheus-one\nprometheus-two\n' ;;
            *) printf 'prometheus-one\n' ;;
          esac
          ;;
        gateway)
          printf 'gateway-one\ngateway-two\n'
          ;;
        *)
          return 1
          ;;
      esac
      ;;
    inspect)
      local format="" container=""
      while [[ "$#" -gt 0 ]]; do
        case "$1" in
          --format)
            format="$2"
            shift 2
            ;;
          *)
            container="$1"
            shift
            ;;
        esac
      done
      [[ "${container}" == "prometheus-one" ]] || return 1
      case "${format}" in
        '{{.RestartCount}}') printf '0\n' ;;
        '{{.State.Running}}') printf 'true\n' ;;
        '{{.State.Restarting}}') printf 'false\n' ;;
        '{{.Config.Image}}') printf 'prom/prometheus:v2.55.1\n' ;;
        '{{.Image}}') printf '%s\n' "${IMAGE_ID}" ;;
        '{{json .Args}}') printf '["--web.enable-lifecycle"]\n' ;;
        *) return 1 ;;
      esac
      ;;
    exec)
      local container="$1"
      shift
      if [[ "$1" == "sha256sum" ]]; then
        case "$2" in
          /etc/prometheus/prometheus.yml) printf '%s  %s\n' "${CONFIG_SHA}" "$2" ;;
          /etc/prometheus/rules/infera-alerts.yml)
            if [[ "${MOCK_SCENARIO}" == "semantic_failure" &&
              "$(tr -d '\n' <"${MOCK_STATE_DIR}/reload-count")" -ge 2 ]]; then
              printf '%s  %s\n' "${ROLLBACK_SHA}" "$2"
            else
              printf '%s  %s\n' "${ALERTS_SHA}" "$2"
            fi
            ;;
          /etc/prometheus/rules/infera-slo-v1.yml) printf '%s  %s\n' "${SLO_SHA}" "$2" ;;
          /etc/prometheus/rules/rollback/infera-alerts.pre-slo-v1.yml)
            printf '%s  %s\n' "${ROLLBACK_SHA}" "$2"
            ;;
          *) return 1 ;;
        esac
        return 0
      fi
      if [[ "$1" == "promtool" ]]; then
        [[ "${MOCK_SCENARIO}" != "promtool_failure" ]]
        return
      fi
      [[ "$1" == "wget" ]] || return 1
      local is_post=0 url=""
      while [[ "$#" -gt 0 ]]; do
        case "$1" in
          --post-data=*) is_post=1 ;;
          http://*) url="$1" ;;
        esac
        shift
      done
      if [[ "${container}" == gateway-* ]]; then
        printf '{"workers":0,"healthy_workers":0}\n'
        return 0
      fi
      [[ "${container}" == "prometheus-one" ]] || return 1
      case "${url}" in
        */-/healthy|*/-/ready)
          return 0
          ;;
        */api/v1/status/flags)
          printf '{"status":"success","data":{"web.enable-lifecycle":"true"}}\n'
          ;;
        */api/v1/status/runtimeinfo)
          case "$(read_state)" in
            baseline) printf '{"data":{"lastConfigTime":"2026-07-30T00:00:00Z"}}\n' ;;
            forward|semantic_bad) printf '{"data":{"lastConfigTime":"2026-07-30T00:00:01Z"}}\n' ;;
            rollback) printf '{"data":{"lastConfigTime":"2026-07-30T00:00:02Z"}}\n' ;;
            *) return 1 ;;
          esac
          ;;
        */api/v1/rules)
          mock_rules_json "$(read_state)"
          ;;
        */api/v1/targets)
          mock_targets_json
          ;;
        */api/v1/alerts)
          printf '{"status":"success","data":{"alerts":[{"state":"firing","labels":{"alertname":"InferaZeroHealthyWorkers"}}]}}\n'
          ;;
        */-/reload)
          [[ "${is_post}" == "1" ]] || return 1
          local count
          count="$(increment_reload_count)"
          case "${MOCK_SCENARIO}" in
            rejected_reload)
              return 1
              ;;
            semantic_failure)
              if [[ "${count}" == "1" ]]; then
                write_state semantic_bad
              else
                write_state rollback
              fi
              ;;
            *)
              write_state forward
              ;;
          esac
          ;;
        *)
          return 1
          ;;
      esac
      ;;
    *)
      return 1
      ;;
  esac
}

curl() {
  printf '503'
}

sleep() {
  :
}

export -f docker curl sleep read_state write_state increment_reload_count
export -f mock_rules_json mock_targets_json
export CONFIG_SHA ALERTS_SHA SLO_SHA ROLLBACK_SHA IMAGE_ID

new_fixture() {
  local name="$1"
  FIXTURE="${TEST_ROOT}/${name}"
  mkdir -p "${FIXTURE}/rules/rollback" "${FIXTURE}/private" "${FIXTURE}/evidence" "${FIXTURE}/state"
  cp "${REPO_ROOT}/deploy/observability/prometheus/prometheus.yml" "${FIXTURE}/prometheus.yml"
  cp "${REPO_ROOT}/deploy/observability/prometheus/rules/infera-alerts.yml" "${FIXTURE}/rules/"
  cp "${REPO_ROOT}/deploy/observability/prometheus/rules/infera-slo-v1.yml" "${FIXTURE}/rules/"
  cp "${REPO_ROOT}/deploy/observability/prometheus/rules/rollback/infera-alerts.pre-slo-v1.yml" \
    "${FIXTURE}/rules/rollback/"
  cp "${REPO_ROOT}/docker-compose.prod.yml" "${FIXTURE}/docker-compose.prod.yml"
  printf 'baseline\n' >"${FIXTURE}/state/state"
  printf '0\n' >"${FIXTURE}/state/reload-count"
  printf 'evidence/\nprivate/\nstate/\nstderr\nstdout\n' >"${FIXTURE}/.gitignore"
  git -C "${FIXTURE}" init -q
  git -C "${FIXTURE}" add .gitignore prometheus.yml rules docker-compose.prod.yml
  git -C "${FIXTURE}" -c user.name=test -c user.email=test@example.invalid commit -qm fixture
}

run_wrapper() {
  local scenario="$1"
  local source_revision
  source_revision="$(git -C "${FIXTURE}" rev-parse HEAD)"
  MOCK_SCENARIO="${scenario}" \
  MOCK_STATE_DIR="${FIXTURE}/state" \
  COMPOSE_FILE="${FIXTURE}/docker-compose.prod.yml" \
  INFERA_PROMETHEUS_SOURCE_ROOT="${FIXTURE}" \
  INFERA_PROMETHEUS_SOURCE_REVISION="${source_revision}" \
  INFERA_PROMETHEUS_CONFIG_FILE="${FIXTURE}/prometheus.yml" \
  INFERA_PROMETHEUS_RULES_DIR="${FIXTURE}/rules" \
  INFERA_PROMETHEUS_PRIVATE_ROOT="${FIXTURE}/private" \
  INFERA_PROMETHEUS_EVIDENCE_DIR="${FIXTURE}/evidence" \
  INFERA_PROMETHEUS_VERIFY_ATTEMPTS=1 \
  INFERA_PROMETHEUS_VERIFY_INTERVAL_SECONDS=0 \
  INFERA_BASE_URL=https://example.invalid \
    bash "${SCRIPT_DIR}/prometheus-safe-reload.sh" >"${FIXTURE}/stdout" 2>"${FIXTURE}/stderr"
}

assert_clean_residue() {
  local active_residue
  active_residue="$(find "${FIXTURE}/rules" -maxdepth 1 \
    \( -name '.*.forward-*' -o -name '.*.rollback-*' \) -print -quit)"
  [[ -z "${active_residue}" ]] || fail_test "active rule residue remained for ${FIXTURE}"
  [[ -z "$(find "${FIXTURE}/private" -mindepth 1 -print -quit)" ]] ||
    fail_test "private stage or lock residue remained for ${FIXTURE}"
}

assert_evidence_private() {
  local evidence_files=()
  local evidence_file
  while IFS= read -r evidence_file; do
    [[ -n "${evidence_file}" ]] && evidence_files[${#evidence_files[@]}]="${evidence_file}"
  done < <(find "${FIXTURE}/evidence" -mindepth 1 -maxdepth 1 -type f -print)
  [[ "${#evidence_files[@]}" -eq 1 ]] || fail_test "expected one regular evidence file"
  [[ "$(portable_mode "${FIXTURE}/evidence")" == "700" ]] ||
    fail_test "evidence directory mode is not 0700"
  [[ "$(portable_mode "${evidence_files[0]}")" == "600" ]] ||
    fail_test "evidence file mode is not 0600"
  if grep -Ev '^[0-9TZ:-]+ PROMETHEUS_RELOAD event=[a-z_]+ result=(start|pass|fail) reason=[a-z_]+ source_revision=[0-9a-f]{40}$' \
    "${evidence_files[0]}" | grep -q .; then
    fail_test "evidence contains an unexpected field"
  fi
}

assert_forward_files() {
  [[ "$(portable_sha256 "${FIXTURE}/rules/infera-alerts.yml")" == "${ALERTS_SHA}" ]] ||
    fail_test "forward alert rules were not restored"
  [[ "$(portable_sha256 "${FIXTURE}/rules/infera-slo-v1.yml")" == "${SLO_SHA}" ]] ||
    fail_test "forward SLO rules were not restored"
}

assert_retained_rollback_files() {
  [[ "$(portable_sha256 "${FIXTURE}/rules/infera-alerts.yml")" == "${ROLLBACK_SHA}" ]] ||
    fail_test "reviewed prior alert rules were not retained"
  [[ ! -e "${FIXTURE}/rules/infera-slo-v1.yml" ]] ||
    fail_test "candidate SLO rules remained in the active glob"
  local retained="${FIXTURE}/rules/rollback/infera-slo-v1.candidate.disabled"
  [[ "$(portable_sha256 "${retained}")" == "${SLO_SHA}" ]] ||
    fail_test "candidate SLO rules were not retained exactly outside the active glob"
  [[ "$(portable_mode "${retained}")" == "600" ]] ||
    fail_test "retained candidate SLO rules are not mode 0600"
}

new_fixture success
run_wrapper success || {
  sed -n '1,200p' "${FIXTURE}/stderr" >&2
  fail_test "success scenario failed"
}
[[ "$(tr -d '\n' <"${FIXTURE}/state/reload-count")" == "1" ]] ||
  fail_test "success did not issue exactly one reload"
[[ "$(tr -d '\n' <"${FIXTURE}/state/state")" == "forward" ]] ||
  fail_test "success did not load forward rules"
assert_forward_files
assert_clean_residue
assert_evidence_private

new_fixture rejected
if run_wrapper rejected_reload; then
  fail_test "rejected reload unexpectedly succeeded"
fi
[[ "$(tr -d '\n' <"${FIXTURE}/state/reload-count")" == "1" ]] ||
  fail_test "rejected reload issued an unexpected number of reloads"
[[ "$(tr -d '\n' <"${FIXTURE}/state/state")" == "baseline" ]] ||
  fail_test "rejected reload changed runtime state"
assert_forward_files
assert_clean_residue
assert_evidence_private

new_fixture semantic
if run_wrapper semantic_failure; then
  fail_test "semantic failure unexpectedly succeeded"
fi
[[ "$(tr -d '\n' <"${FIXTURE}/state/reload-count")" == "2" ]] ||
  fail_test "semantic failure did not issue candidate and rollback reloads"
[[ "$(tr -d '\n' <"${FIXTURE}/state/state")" == "rollback" ]] ||
  fail_test "semantic failure did not restore baseline runtime"
assert_retained_rollback_files
assert_clean_residue
assert_evidence_private
grep -q 'event=rollback result=pass reason=none' "${FIXTURE}"/evidence/* ||
  fail_test "semantic rollback evidence is missing"

for container_scenario in no_container multiple_containers; do
  new_fixture "${container_scenario}"
  if run_wrapper "${container_scenario}"; then
    fail_test "${container_scenario} unexpectedly succeeded"
  fi
  [[ "$(tr -d '\n' <"${FIXTURE}/state/reload-count")" == "0" ]] ||
    fail_test "${container_scenario} attempted a reload"
  assert_clean_residue
  assert_evidence_private
done

new_fixture hash_drift
printf '\n# drift\n' >>"${FIXTURE}/prometheus.yml"
git -C "${FIXTURE}" add prometheus.yml
git -C "${FIXTURE}" -c user.name=test -c user.email=test@example.invalid commit -qm drift
if run_wrapper success; then
  fail_test "hash drift unexpectedly succeeded"
fi
[[ "$(tr -d '\n' <"${FIXTURE}/state/reload-count")" == "0" ]] ||
  fail_test "hash drift attempted a reload"
assert_clean_residue
assert_evidence_private

new_fixture target_loss
if run_wrapper target_loss; then
  fail_test "target loss unexpectedly succeeded"
fi
[[ "$(tr -d '\n' <"${FIXTURE}/state/reload-count")" == "0" ]] ||
  fail_test "target loss attempted a reload"
assert_forward_files
assert_clean_residue
assert_evidence_private

echo "Prometheus safe reload tests passed."
