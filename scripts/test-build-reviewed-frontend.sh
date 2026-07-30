#!/usr/bin/env bash

set -euo pipefail

fail() {
    echo "FAIL: $*" >&2
    exit 1
}

assert_file_content() {
    local path=$1
    local expected=$2
    [[ -f "$path" ]] || fail "expected file: $path"
    [[ "$(<"$path")" == "$expected" ]] ||
        fail "unexpected content in $path"
}

assert_absent() {
    local path=$1
    [[ ! -e "$path" ]] || fail "unexpected path in reviewed context: $path"
}

test_root=$(mktemp -d "${TMPDIR:-/tmp}/infera-frontend-build-test.XXXXXX")
cleanup() {
    rm -rf -- "$test_root"
}
trap cleanup EXIT HUP INT TERM

fixture="$test_root/repository"
fake_bin="$test_root/bin"
capture="$test_root/capture"
contexts="$test_root/contexts"
mkdir -p \
    "$fixture/scripts" \
    "$fixture/frontend/public" \
    "$fixture/frontend/src" \
    "$fixture/deploy/docker" \
    "$fake_bin" \
    "$capture" \
    "$contexts"

cp scripts/build-reviewed-frontend.sh "$fixture/scripts/"
cp .dockerignore "$fixture/"
cp deploy/docker/Dockerfile.frontend deploy/docker/nginx.conf "$fixture/deploy/docker/"
printf '%s\n' CLEAN_TRACKED > "$fixture/frontend/index.html"
printf '%s\n' CLEAN_STAGED > "$fixture/frontend/src/staged.txt"
printf '%s\n' '#!/usr/bin/env sh' 'exit 0' > "$fixture/frontend/build-helper.sh"
chmod +x "$fixture/frontend/build-helper.sh"
printf '%s\n' '{"scripts":{"build":"true"}}' > "$fixture/frontend/package.json"
printf '%s\n' '{"lockfileVersion":3}' > "$fixture/frontend/package-lock.json"

git -C "$fixture" init -q
git -C "$fixture" config user.email inf74-test@example.invalid
git -C "$fixture" config user.name "INF-74 test"
git -C "$fixture" add .
git -C "$fixture" commit -qm "reviewed source"
reviewed_revision=$(git -C "$fixture" rev-parse HEAD)

printf '%s\n' DIRTY_TRACKED > "$fixture/frontend/index.html"
printf '%s\n' DIRTY_STAGED > "$fixture/frontend/src/staged.txt"
git -C "$fixture" add frontend/src/staged.txt
printf '%s\n' UNTRACKED_PUBLIC > "$fixture/frontend/public/untracked.txt"
printf '%s\n' UNTRACKED_CREDENTIAL > "$fixture/frontend/.env.production"
mkdir -p \
    "$fixture/frontend/node_modules/host-package" \
    "$fixture/frontend/dist" \
    "$fixture/frontend/recovery-evidence" \
    "$fixture/frontend/.cache"
printf '%s\n' HOST_DEPENDENCY > "$fixture/frontend/node_modules/host-package/index.js"
printf '%s\n' HOST_DIST > "$fixture/frontend/dist/index.html"
printf '%s\n' RECOVERY > "$fixture/frontend/recovery-evidence/operator.txt"
printf '%s\n' HOST_CACHE > "$fixture/frontend/.cache/state"
printf '%s\n' HOST_ARTIFACT > "$fixture/frontend/.DS_Store"

cat > "$fake_bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

printf '%s\n' invoked > "$CAPTURE_ROOT/docker-invoked"

if [[ "$1" == build ]]; then
    shift
    context=${!#}
    iid_file=
    source_revision=
    printf '%s\n' "$@" > "$CAPTURE_ROOT/docker-build-arguments"
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --iidfile)
                iid_file=$2
                shift 2
                ;;
            --build-arg)
                if [[ "$2" == SOURCE_REVISION=* ]]; then
                    source_revision=${2#SOURCE_REVISION=}
                fi
                shift 2
                ;;
            *)
                shift
                ;;
        esac
    done
    find "$context/deploy/docker/nginx.conf" -perm -004 -print -quit | grep -q . ||
        {
            echo "tracked 0644 file is not world-readable in build context" >&2
            exit 43
        }
    find "$context/frontend/build-helper.sh" -perm -111 -print -quit | grep -q . ||
        {
            echo "tracked executable file is not executable in build context" >&2
            exit 44
        }
    cp -R "$context" "$CAPTURE_ROOT/context"
    printf '%s\n' "$source_revision" > "$CAPTURE_ROOT/source-revision"
    if [[ "${FAIL_DOCKER_BUILD:-0}" == 1 ]]; then
        exit 42
    fi
    printf '%s\n' 'sha256:fake-reviewed-image' > "$iid_file"
    exit 0
fi

if [[ "$1" == image && "$2" == inspect ]]; then
    cat "$CAPTURE_ROOT/source-revision"
    exit 0
fi

echo "unexpected fake docker invocation: $*" >&2
exit 1
EOF
chmod +x "$fake_bin/docker" "$fixture/scripts/build-reviewed-frontend.sh"

(
    umask 0077
    PATH="$fake_bin:$PATH" \
    CAPTURE_ROOT="$capture" \
    TMPDIR="$contexts" \
        "$fixture/scripts/build-reviewed-frontend.sh" \
        --revision "$reviewed_revision" \
        --tag infera-frontend:test
) > "$test_root/build-output"

assert_file_content "$capture/context/frontend/index.html" CLEAN_TRACKED
assert_file_content "$capture/context/frontend/src/staged.txt" CLEAN_STAGED
assert_absent "$capture/context/frontend/public/untracked.txt"
assert_absent "$capture/context/frontend/.env.production"
assert_absent "$capture/context/frontend/node_modules"
assert_absent "$capture/context/frontend/dist"
assert_absent "$capture/context/frontend/recovery-evidence"
assert_absent "$capture/context/frontend/.cache"
assert_absent "$capture/context/frontend/.DS_Store"

grep -qx -- '--pull' "$capture/docker-build-arguments" ||
    fail "reviewed build did not pull pinned bases"
grep -qx -- '--no-cache' "$capture/docker-build-arguments" ||
    fail "reviewed build did not disable the cache"
grep -qx -- 'linux/amd64' "$capture/docker-build-arguments" ||
    fail "reviewed build did not target linux/amd64"
grep -qx -- "SOURCE_REVISION=$reviewed_revision" "$capture/docker-build-arguments" ||
    fail "reviewed build did not pass the resolved source revision"
grep -q "^IMAGE_SOURCE_REVISION=$reviewed_revision$" "$test_root/build-output" ||
    fail "reviewed build did not report the verified revision label"

if find "$contexts" -maxdepth 1 -name 'infera-frontend-context.*' -print -quit | grep -q .; then
    fail "temporary build context was not cleaned up"
fi

assert_rejected_platform() {
    local rejected_platform=$1
    local label=$2
    local error_output="$test_root/platform-${label}.err"
    local exit_code

    rm -f "$capture/docker-invoked"
    set +e
    PATH="$fake_bin:$PATH" CAPTURE_ROOT="$capture" TMPDIR="$contexts" \
        "$fixture/scripts/build-reviewed-frontend.sh" \
        --revision "$reviewed_revision" \
        --tag infera-frontend:test \
        --platform "$rejected_platform" \
        > /dev/null 2>"$error_output"
    exit_code=$?
    set -e

    [[ $exit_code -eq 2 ]] ||
        fail "platform ${label} exited ${exit_code}, expected 2"
    grep -q "reviewed frontend builds require --platform linux/amd64" "$error_output" ||
        fail "platform ${label} did not report the amd64 requirement"
    assert_absent "$capture/docker-invoked"
    if find "$contexts" -maxdepth 1 -name 'infera-frontend-context.*' -print -quit | grep -q .; then
        fail "platform ${label} left a temporary context behind"
    fi
}

assert_rejected_platform "linux/arm64" arm64
assert_rejected_platform "" empty
assert_rejected_platform "windows/amd64" other

if PATH="$fake_bin:$PATH" CAPTURE_ROOT="$capture" TMPDIR="$contexts" \
    "$fixture/scripts/build-reviewed-frontend.sh" \
    --tag infera-frontend:test > /dev/null 2>&1; then
    fail "missing revision unexpectedly succeeded"
fi

if PATH="$fake_bin:$PATH" CAPTURE_ROOT="$capture" TMPDIR="$contexts" \
    "$fixture/scripts/build-reviewed-frontend.sh" \
    --revision does-not-exist \
    --tag infera-frontend:test > /dev/null 2>&1; then
    fail "invalid revision unexpectedly succeeded"
fi

if PATH="$fake_bin:$PATH" CAPTURE_ROOT="$capture" TMPDIR="$contexts" FAIL_DOCKER_BUILD=1 \
    "$fixture/scripts/build-reviewed-frontend.sh" \
    --revision "$reviewed_revision" \
    --tag infera-frontend:test > /dev/null 2>&1; then
    fail "simulated Docker failure unexpectedly succeeded"
fi

if find "$contexts" -maxdepth 1 -name 'infera-frontend-context.*' -print -quit | grep -q .; then
    fail "failed builds left a temporary context behind"
fi

echo "PASS: reviewed frontend context ignores dirty, staged, and untracked workspace inputs"
echo "PASS: reviewed frontend context preserves Git file modes under a restrictive umask"
echo "PASS: reviewed frontend builds reject every non-linux/amd64 platform before Docker"
echo "PASS: invalid/missing revisions fail and temporary contexts are cleaned"
