#!/usr/bin/env bash

set -euo pipefail

if [[ "$(id -u)" -ne 0 ]]; then
  exec sudo -n bash "$0"
fi

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

test_root="$(mktemp -d "${TMPDIR:-/tmp}/infera-frontend-compose-test.XXXXXX")"
cleanup() {
  rm -rf -- "${test_root}"
}
trap cleanup EXIT HUP INT TERM

fixture="${test_root}/repository"
fake_bin="${test_root}/bin"
calls="${test_root}/calls"
mkdir -p "${fixture}/scripts" "${fake_bin}"
cp scripts/frontend-compose-action.sh scripts/production-env-source.py \
  scripts/production-env-source.sh "${fixture}/scripts/"
chmod +x "${fixture}/scripts/frontend-compose-action.sh" \
  "${fixture}/scripts/production-env-source.py" "${fixture}/scripts/production-env-source.sh"
python3 scripts/write-production-env-test-fixture.py "${test_root}/production.env"
export INFERA_PRODUCTION_ENV_FILE="${test_root}/production.env"

git -C "${fixture}" init -q
git -C "${fixture}" config user.email frontend-compose-test@example.invalid
git -C "${fixture}" config user.name "Frontend Compose test"

cat >"${fixture}/docker-compose.prod.yml" <<'EOF'
services:
  frontend:
    build:
      context: .
    image: infera-frontend
EOF
git -C "${fixture}" add .
git -C "${fixture}" commit -qm "stale build compose"
stale_revision="$(git -C "${fixture}" rev-parse HEAD)"

cat >"${fixture}/docker-compose.prod.yml" <<'EOF'
services:
  frontend:
    image: ${INFERA_FRONTEND_IMAGE:?INFERA_FRONTEND_IMAGE is required}
EOF
git -C "${fixture}" add docker-compose.prod.yml
git -C "${fixture}" commit -qm "image-only compose"
good_revision="$(git -C "${fixture}" rev-parse HEAD)"

cat >"${fixture}/docker-compose.prod.yml" <<'EOF'
services:
  frontend:
    image: example.invalid/infera-frontend@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
EOF
git -C "${fixture}" add docker-compose.prod.yml
git -C "${fixture}" commit -qm "mismatched image compose"
mismatch_revision="$(git -C "${fixture}" rev-parse HEAD)"

cat >"${fake_bin}/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

printf '%s\n' "$*" >>"${TEST_CALLS}"
if [[ "${1:-}" == pull ]]; then
  exit 0
fi
if [[ "${1:-} ${2:-}" == "image inspect" ]]; then
  if [[ "$*" == *"'{{.Id}}'"* || "$*" == *"{{.Id}}"* ]]; then
    printf '%s\n' "${TEST_EXPECTED_IMAGE_ID:-sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc}"
  elif [[ "${!#}" == sha256:* ]]; then
    printf '%s\n' "${TEST_RUNTIME_IMAGE_REVISION-${TEST_IMAGE_REVISION:-}}"
  else
    printf '%s\n' "${TEST_IMAGE_REVISION:-}"
  fi
  exit 0
fi
if [[ "${1:-}" == inspect ]]; then
  if [[ "$*" == *".Config.Image"* ]]; then
    printf '%s\n' "${TEST_RUNTIME_CONFIG_IMAGE:-${INFERA_FRONTEND_IMAGE}}"
  elif [[ "$*" == *".Image"* ]]; then
    printf '%s\n' "${TEST_RUNTIME_IMAGE_ID:-sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc}"
  else
    exit 1
  fi
  exit 0
fi
[[ "${1:-}" == compose ]] || exit 1
shift
compose_file=
while [[ $# -gt 0 ]]; do
  case "$1" in
    -f)
      compose_file="$2"
      shift 2
      ;;
    config)
      if grep -q 'build:' "${compose_file}"; then
        printf '%s\n' '{"services":{"frontend":{"build":{"context":"."},"image":"infera-frontend"}}}'
      elif grep -q 'example.invalid/infera-frontend@sha256:bbbb' "${compose_file}"; then
        printf '%s\n' '{"services":{"frontend":{"image":"example.invalid/infera-frontend@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}}'
      else
        python3 - "${INFERA_FRONTEND_IMAGE}" <<'PY'
import json
import sys
print(json.dumps({"services": {"frontend": {"image": sys.argv[1]}}}))
PY
      fi
      exit 0
      ;;
    up)
      exit 0
      ;;
    ps)
      case "${TEST_RUNTIME_CONTAINER_MODE:-one}" in
        one) printf '%s\n' frontend-container ;;
        none) ;;
        multiple) printf '%s\n' frontend-container frontend-container-2 ;;
        *) exit 1 ;;
      esac
      exit 0
      ;;
    *)
      shift
      ;;
  esac
done
exit 1
EOF
chmod +x "${fake_bin}/docker"
: >"${calls}"

image="registry.example.invalid/infera-frontend@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
run_action() {
  PATH="${fake_bin}:${PATH}" TEST_CALLS="${calls}" \
    TEST_IMAGE_REVISION="${TEST_IMAGE_REVISION:-${good_revision}}" \
    "${fixture}/scripts/frontend-compose-action.sh" "$@"
}

for action in candidate rollback restore; do
  run_action "${action}" --source-revision "${good_revision}" --image "${image}" >/dev/null
done

up_calls="$(grep -c -- ' up -d --no-build --no-deps --force-recreate frontend$' "${calls}")"
[[ "${up_calls}" -eq 3 ]] || fail "candidate, rollback, and restore did not share the guarded frontend command"
if grep ' up ' "${calls}" | grep -Ev -- ' up -d --no-build --no-deps --force-recreate frontend$'; then
  fail "frontend action escaped the fixed no-build/no-deps service scope"
fi

: >"${calls}"
if run_action candidate --source-revision "${stale_revision}" --image "${image}" >/dev/null 2>&1; then
  fail "stale Compose frontend build unexpectedly succeeded"
fi
if grep -q ' up ' "${calls}"; then
  fail "stale Compose reached a recreate"
fi

: >"${calls}"
if run_action candidate --source-revision "${mismatch_revision}" --image "${image}" >/dev/null 2>&1; then
  fail "rendered source/digest mismatch unexpectedly succeeded"
fi
if grep -q ' up ' "${calls}"; then
  fail "source/digest mismatch reached a recreate"
fi

: >"${calls}"
if TEST_IMAGE_REVISION="${stale_revision}" run_action candidate \
  --source-revision "${good_revision}" --image "${image}" >/dev/null 2>&1; then
  fail "image source revision/digest mismatch unexpectedly succeeded"
fi
if grep -q ' up ' "${calls}"; then
  fail "image source revision/digest mismatch reached a recreate"
fi

: >"${calls}"
if TEST_RUNTIME_CONFIG_IMAGE=infera-frontend run_action candidate \
  --source-revision "${good_revision}" --image "${image}" >/dev/null 2>&1; then
  fail "successful recreate with a mutable runtime image reference unexpectedly succeeded"
fi
grep -q -- ' up -d --no-build --no-deps --force-recreate frontend$' "${calls}" ||
  fail "mutable runtime identity case did not reach the recreate"

: >"${calls}"
if TEST_RUNTIME_IMAGE_REVISION='' run_action candidate \
  --source-revision "${good_revision}" --image "${image}" >/dev/null 2>&1; then
  fail "successful recreate with a missing runtime revision unexpectedly succeeded"
fi

: >"${calls}"
if TEST_RUNTIME_IMAGE_REVISION="${stale_revision}" run_action candidate \
  --source-revision "${good_revision}" --image "${image}" >/dev/null 2>&1; then
  fail "successful recreate with a wrong runtime revision unexpectedly succeeded"
fi

: >"${calls}"
if TEST_RUNTIME_IMAGE_ID=sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd \
  run_action candidate --source-revision "${good_revision}" --image "${image}" >/dev/null 2>&1; then
  fail "successful recreate with a mismatched runtime image ID unexpectedly succeeded"
fi

for container_mode in none multiple; do
  : >"${calls}"
  if TEST_RUNTIME_CONTAINER_MODE="${container_mode}" run_action candidate \
    --source-revision "${good_revision}" --image "${image}" >/dev/null 2>&1; then
    fail "successful recreate with ${container_mode} frontend containers unexpectedly succeeded"
  fi
done

if run_action candidate --source-revision "${good_revision}" \
  --image registry.example.invalid/infera-frontend:mutable >/dev/null 2>&1; then
  fail "mutable frontend tag unexpectedly succeeded"
fi
if run_action candidate --image "${image}" >/dev/null 2>&1; then
  fail "missing source revision unexpectedly succeeded"
fi
if run_action candidate --source-revision "${good_revision}" >/dev/null 2>&1; then
  fail "missing frontend digest unexpectedly succeeded"
fi
if run_action candidate --source-revision "${good_revision:0:12}" --image "${image}" >/dev/null 2>&1; then
  fail "short source revision unexpectedly succeeded"
fi
if run_action candidate --source-revision 0000000000000000000000000000000000000000 \
  --image "${image}" >/dev/null 2>&1; then
  fail "unavailable full source revision unexpectedly succeeded"
fi
if run_action gateway --source-revision "${good_revision}" --image "${image}" >/dev/null 2>&1; then
  fail "non-frontend action scope unexpectedly succeeded"
fi
if run_action candidate --source-revision "${good_revision}" --image "${image}" \
  gateway >/dev/null 2>&1; then
  fail "extra service scope unexpectedly succeeded"
fi

printf '%s\n' dirty >"${fixture}/untracked"
if run_action restore --source-revision "${good_revision}" --image "${image}" >/dev/null 2>&1; then
  fail "dirty repository unexpectedly succeeded"
fi

echo "Frontend exact-source Compose action tests passed."
