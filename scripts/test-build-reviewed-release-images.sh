#!/usr/bin/env bash

set -euo pipefail

fail() {
    echo "FAIL: $*" >&2
    exit 1
}

assert_content() {
    local path=$1 expected=$2
    [[ -f "$path" ]] || fail "expected file: $path"
    [[ "$(<"$path")" == "$expected" ]] || fail "unexpected content in $path"
}

assert_absent() {
    [[ ! -e "$1" ]] || fail "unexpected reviewed-context path: $1"
}

test_root=$(mktemp -d "${TMPDIR:-/tmp}/infera-release-build-test.XXXXXX")
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
    "$fixture/deploy/docker" \
    "$fixture/go/cmd/gateway" \
    "$fixture/go/internal/example" \
    "$fixture/go/pkg/example" \
    "$fixture/python/requirements" \
    "$fixture/python/src/infera_worker/engines" \
    "$fake_bin" "$capture" "$contexts"

cp scripts/build-reviewed-release-image.sh "$fixture/scripts/"
cp deploy/docker/Dockerfile.gateway deploy/docker/Dockerfile.worker.vllm "$fixture/deploy/docker/"
cp python/requirements/worker-vllm.lock python/requirements/worker-vllm.lock.sha256 \
    "$fixture/python/requirements/"
printf '%s\n' 'module example.invalid/infera' > "$fixture/go/go.mod"
printf '%s\n' 'sum' > "$fixture/go/go.sum"
printf '%s\n' CLEAN_GATEWAY > "$fixture/go/cmd/gateway/main.go"
printf '%s\n' INTERNAL > "$fixture/go/internal/example/value.go"
printf '%s\n' PKG > "$fixture/go/pkg/example/value.go"
printf '%s\n' '[build-system]' > "$fixture/python/pyproject.toml"
printf '%s\n' README > "$fixture/python/README.md"
printf '%s\n' CLEAN_WORKER > "$fixture/python/src/infera_worker/__init__.py"
printf '%s\n' ENGINE > "$fixture/python/src/infera_worker/engines/vllm_engine.py"

git -C "$fixture" init -q
git -C "$fixture" config user.email inf78-test@example.invalid
git -C "$fixture" config user.name "INF-78 test"
git -C "$fixture" add .
git -C "$fixture" commit -qm "reviewed source"
reviewed_revision=$(git -C "$fixture" rev-parse HEAD)

printf '%s\n' DIRTY_GATEWAY > "$fixture/go/cmd/gateway/main.go"
printf '%s\n' DIRTY_WORKER > "$fixture/python/src/infera_worker/__init__.py"
printf '%s\n' TAMPERED_LOCK >> "$fixture/python/requirements/worker-vllm.lock"
printf '%s\n' SECRET > "$fixture/.env.production"
printf '%s\n' SECRET > "$fixture/python/.env"
printf '%s\n' OUTPUT > "$fixture/go/gateway"
mkdir -p "$fixture/python/tests" "$fixture/python/.cache" "$fixture/.git-shadow"
printf '%s\n' TEST > "$fixture/python/tests/untracked.py"
printf '%s\n' CACHE > "$fixture/python/.cache/state"
printf '%s\n' VCS > "$fixture/.git-shadow/config"

cat > "$fake_bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

printf '%s\n' invoked > "$CAPTURE_ROOT/docker-invoked"
if [[ "$1" == build ]]; then
    shift
    context=${!#}
    iid_file=
    source_revision=
    component=
    printf '%s\n' "$@" > "$CAPTURE_ROOT/docker-build-arguments"
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --iidfile)
                iid_file=$2
                shift 2
                ;;
            --build-arg)
                source_revision=${2#SOURCE_REVISION=}
                shift 2
                ;;
            --file)
                [[ "$2" == *Dockerfile.gateway ]] && component=gateway
                [[ "$2" == *Dockerfile.worker.vllm ]] && component=worker-vllm
                shift 2
                ;;
            *)
                shift
                ;;
        esac
    done
    rm -rf "$CAPTURE_ROOT/context-$component"
    cp -R "$context" "$CAPTURE_ROOT/context-$component"
    printf '%s\n' "$source_revision" > "$CAPTURE_ROOT/source-revision"
    printf '%s\n' 'sha256:fake-reviewed-image' > "$iid_file"
    exit 0
fi
if [[ "$1" == image && "$2" == inspect ]]; then
    if [[ "${LABEL_MISMATCH:-0}" == 1 ]]; then
        printf '%040d\n' 0
    else
        cat "$CAPTURE_ROOT/source-revision"
    fi
    exit 0
fi
echo "unexpected fake docker invocation: $*" >&2
exit 1
EOF
chmod +x "$fake_bin/docker" "$fixture/scripts/build-reviewed-release-image.sh"

run_reviewed() {
    PATH="$fake_bin:$PATH" CAPTURE_ROOT="$capture" TMPDIR="$contexts" \
        "$fixture/scripts/build-reviewed-release-image.sh" \
        --component "$1" \
        --revision "$reviewed_revision" \
        --tag "example.invalid/infera-$1:test"
}

run_reviewed gateway > "$test_root/gateway.out"
assert_content "$capture/context-gateway/go/cmd/gateway/main.go" CLEAN_GATEWAY
assert_absent "$capture/context-gateway/.env.production"
assert_absent "$capture/context-gateway/python"
assert_absent "$capture/context-gateway/.git"
assert_absent "$capture/context-gateway/go/gateway"

run_reviewed worker-vllm > "$test_root/worker.out"
assert_content "$capture/context-worker-vllm/python/src/infera_worker/__init__.py" CLEAN_WORKER
grep -qx '# Exact active dependency closure recovered from:' \
    "$capture/context-worker-vllm/python/requirements/worker-vllm.lock" ||
    fail "worker context did not use the committed lock"
assert_absent "$capture/context-worker-vllm/python/.env"
assert_absent "$capture/context-worker-vllm/python/tests"
assert_absent "$capture/context-worker-vllm/python/.cache"
assert_absent "$capture/context-worker-vllm/go"

grep -qx -- '--pull' "$capture/docker-build-arguments" || fail "missing --pull"
grep -qx -- '--no-cache' "$capture/docker-build-arguments" || fail "missing --no-cache"
grep -qx -- "SOURCE_REVISION=$reviewed_revision" "$capture/docker-build-arguments" ||
    fail "missing exact SOURCE_REVISION"
grep -qx "IMAGE_SOURCE_REVISION=$reviewed_revision" "$test_root/worker.out" ||
    fail "worker label was not verified"

assert_rejected() {
    local label=$1
    shift
    rm -f "$capture/docker-invoked"
    if PATH="$fake_bin:$PATH" CAPTURE_ROOT="$capture" TMPDIR="$contexts" \
        "$fixture/scripts/build-reviewed-release-image.sh" "$@" \
        > /dev/null 2>"$test_root/$label.err"; then
        fail "$label unexpectedly succeeded"
    fi
    assert_absent "$capture/docker-invoked"
}

assert_rejected symbolic-revision \
    --component gateway --revision HEAD --tag example.invalid/gateway:test
uppercase_revision=$(printf '%s' "$reviewed_revision" | tr '[:lower:]' '[:upper:]')
assert_rejected uppercase-revision \
    --component gateway --revision "$uppercase_revision" --tag example.invalid/gateway:test
assert_rejected unsupported-platform \
    --component gateway --revision "$reviewed_revision" --tag example.invalid/gateway:test \
    --platform linux/arm64
assert_rejected unsafe-tag \
    --component gateway --revision "$reviewed_revision" --tag -bad
assert_rejected unsafe-build-arg \
    --component gateway --revision "$reviewed_revision" --tag example.invalid/gateway:test \
    --build-arg SECRET=value
assert_rejected unsupported-component \
    --component worker --revision "$reviewed_revision" --tag example.invalid/worker:test

git -C "$fixture" rm -q python/requirements/worker-vllm.lock.sha256
git -C "$fixture" commit -qm "remove required lock checksum"
missing_path_revision=$(git -C "$fixture" rev-parse HEAD)
assert_rejected missing-path \
    --component worker-vllm --revision "$missing_path_revision" \
    --tag example.invalid/worker-vllm:test

if LABEL_MISMATCH=1 PATH="$fake_bin:$PATH" CAPTURE_ROOT="$capture" TMPDIR="$contexts" \
    "$fixture/scripts/build-reviewed-release-image.sh" \
    --component gateway --revision "$reviewed_revision" --tag example.invalid/gateway:test \
    > /dev/null 2>"$test_root/label-mismatch.err"; then
    fail "revision-label mismatch unexpectedly succeeded"
fi
grep -q 'built image revision label mismatch' "$test_root/label-mismatch.err" ||
    fail "revision-label mismatch was not reported"

if find "$contexts" -maxdepth 1 \
    \( -name 'infera-gateway-context.*' -o -name 'infera-worker-vllm-context.*' \
       -o -name 'infera-gateway-iid.*' -o -name 'infera-worker-vllm-iid.*' \) \
    -print -quit | grep -q .; then
    fail "temporary release build inputs were not cleaned"
fi

for dockerfile in deploy/docker/Dockerfile.gateway deploy/docker/Dockerfile.worker.vllm; do
    awk '/^FROM / && $0 !~ /@sha256:[0-9a-f]{64}([[:space:]]|$)/ { exit 1 }' "$dockerfile" ||
        fail "mutable base default in $dockerfile"
    grep -q 'org.opencontainers.image.revision="${SOURCE_REVISION}"' "$dockerfile" ||
        fail "missing OCI revision label in $dockerfile"
done

if grep -Eq 'apt-get|pip install --upgrade|vllm>=|pip install .* -e([[:space:]]|$)' \
    deploy/docker/Dockerfile.worker.vllm; then
    fail "worker Dockerfile contains mutable production dependency resolution"
fi
grep -q 'sha256sum -c worker-vllm.lock.sha256' deploy/docker/Dockerfile.worker.vllm ||
    fail "worker Dockerfile does not verify lock integrity"
grep -q 'build_reviewed_image "gateway" "gateway"' scripts/build-docker.sh ||
    fail "build-docker gateway path bypasses the reviewed builder"
grep -q 'build_reviewed_image "worker-vllm" "worker-vllm"' scripts/build-docker.sh ||
    fail "build-docker vLLM path bypasses the reviewed builder"
grep -q 'RELEASE_REVISION: \${{ github.sha }}' .github/workflows/build-worker-image.yml ||
    fail "worker image workflow does not bind the reviewed commit"
if awk '
    /^  gateway:/ { in_gateway=1; next }
    in_gateway && /^  [A-Za-z0-9_-]+:/ { in_gateway=0 }
    in_gateway && /^[[:space:]]+build:/ { found=1 }
    END { exit found ? 0 : 1 }
' docker-compose.prod.yml; then
    fail "production Compose contains a mutable gateway build path"
fi
(
    cd python/requirements
    shasum -a 256 -c worker-vllm.lock.sha256
) >/dev/null || fail "worker dependency lock checksum mismatch"

python3 - python/requirements/worker-vllm.lock <<'PY'
from pathlib import Path
import re
import sys

lines = [
    line for line in Path(sys.argv[1]).read_text(encoding="utf-8").splitlines()
    if line and not line.startswith("#")
]
if lines != sorted(lines, key=lambda line: line.split("==", 1)[0].casefold()):
    raise SystemExit("dependency lock is not deterministically sorted")
if len(lines) != len({line.split("==", 1)[0].lower().replace("_", "-") for line in lines}):
    raise SystemExit("dependency lock contains duplicate normalized names")
for line in lines:
    if not re.fullmatch(r"[A-Za-z0-9_.-]+==[^ \t]+", line):
        raise SystemExit(f"invalid exact dependency pin: {line}")
required = {"vllm==0.18.0", "torch==2.10.0", "transformers==4.57.6"}
if not required.issubset(lines):
    raise SystemExit("dependency lock is missing the reviewed vLLM runtime versions")
PY

echo "PASS: reviewed gateway/worker contexts exclude dirty, untracked, and out-of-allowlist files"
echo "PASS: exact commits, platform, tags, arguments, paths, and source labels fail closed"
echo "PASS: base digests and the immutable worker dependency inventory are enforced"
