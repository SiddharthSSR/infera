#!/usr/bin/env bash
# Prove the accepted dotenv subset agrees with real Compose rendering and runtime.
# shellcheck disable=SC1091,SC2016

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
EXPECTED_UID="$(id -u)"
if [[ "${EXPECTED_UID}" -eq 0 ]]; then
  TEST_BASE="$(python3 - <<'PY'
import os
import pwd

print(os.path.realpath(pwd.getpwuid(0).pw_dir))
PY
)"
else
  TEST_BASE="${REPO_ROOT}"
fi
TEST_ROOT="$(mktemp -d "${TEST_BASE}/.production-env-compose-test.XXXXXX")"
export INFERA_PRODUCTION_ENV_FILE="${TEST_ROOT}/production.env"

# shellcheck source=production-env-source.sh
source "${REPO_ROOT}/scripts/production-env-source.sh"

test_env_value() {
  if [[ "${EXPECTED_UID}" -eq 0 ]]; then
    production_env_value "$1"
  else
    python3 "${REPO_ROOT}/scripts/production-env-source.py" \
      value "$1" --expected-uid "${EXPECTED_UID}"
  fi
}

test_compose() {
  if [[ "${EXPECTED_UID}" -eq 0 ]]; then
    production_compose "$@"
  else
    python3 "${REPO_ROOT}/scripts/production-env-source.py" \
      compose --expected-uid "${EXPECTED_UID}" -- "$@"
  fi
}

cleanup() {
  test_compose -f "${TEST_ROOT}/compose.yml" down --remove-orphans \
    >/dev/null 2>&1 || true
  rm -rf "${TEST_ROOT}"
}
trap cleanup EXIT

python3 "${REPO_ROOT}/scripts/write-production-env-test-fixture.py" \
  "${INFERA_PRODUCTION_ENV_FILE}"
printf '%s\n' \
  'TEST_UNQUOTED=safe-._~:/?@!%+=,' \
  'TEST_SINGLE='\''literal # $ ${NOT_EXPANDED} with spaces'\''' \
  'TEST_DOUBLE="double # value with spaces and \"quotes\" and \\slash"' \
  >>"${INFERA_PRODUCTION_ENV_FILE}"
chmod 0600 "${INFERA_PRODUCTION_ENV_FILE}"

cat >"${TEST_ROOT}/compose.yml" <<'EOF'
services:
  values:
    image: alpine:3.19
    environment:
      TEST_UNQUOTED: "${TEST_UNQUOTED:?}"
      TEST_SINGLE: "${TEST_SINGLE:?}"
      TEST_DOUBLE: "${TEST_DOUBLE:?}"
EOF

expected_unquoted='safe-._~:/?@!%+=,'
expected_single='literal # $ ${NOT_EXPANDED} with spaces'
expected_double='double # value with spaces and "quotes" and \slash'

[[ "$(test_env_value TEST_UNQUOTED)" == "${expected_unquoted}" ]]
[[ "$(test_env_value TEST_SINGLE)" == "${expected_single}" ]]
[[ "$(test_env_value TEST_DOUBLE)" == "${expected_double}" ]]

test_compose -f "${TEST_ROOT}/compose.yml" config --format json \
  >"${TEST_ROOT}/rendered.json"
python3 - "${TEST_ROOT}/rendered.json" \
  "${expected_unquoted}" "${expected_single}" "${expected_double}" <<'PY'
import json
import sys

path, unquoted, single, double = sys.argv[1:]
with open(path, encoding="utf-8") as rendered_file:
    environment = json.load(rendered_file)["services"]["values"]["environment"]
expected = {
    "TEST_UNQUOTED": unquoted,
    "TEST_SINGLE": single,
    "TEST_DOUBLE": double,
}
runtime_environment = {
    name: value.replace("$$", "$") for name, value in environment.items()
}
if runtime_environment != expected:
    mismatches = sorted(
        name for name, value in expected.items() if runtime_environment.get(name) != value
    )
    raise SystemExit(
        "Compose render disagrees with validated dotenv names: " + ", ".join(mismatches)
    )
PY

test_compose -f "${TEST_ROOT}/compose.yml" run --rm --no-deps values \
  /bin/sh -c \
  'printf "%s\0%s\0%s\0" "$TEST_UNQUOTED" "$TEST_SINGLE" "$TEST_DOUBLE"' \
  >"${TEST_ROOT}/container-values"
python3 - "${TEST_ROOT}/container-values" \
  "${expected_unquoted}" "${expected_single}" "${expected_double}" <<'PY'
import sys

path, unquoted, single, double = sys.argv[1:]
with open(path, "rb") as output:
    values = output.read().split(b"\0")
expected = [unquoted.encode(), single.encode(), double.encode(), b""]
if values != expected:
    raise SystemExit("Compose container disagrees with validated dotenv values")
PY

echo "Production environment Compose value agreement tests passed."
