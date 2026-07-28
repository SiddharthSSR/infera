# Reproducible gateway and vLLM worker release builds

Gateway and vLLM worker release images are built from one explicit lowercase,
full 40-character Git commit. The reviewed builder archives only a
component-specific allowlist from that commit, passes the same commit as
`SOURCE_REVISION`, and rejects the image unless
`org.opencontainers.image.revision` matches exactly.

This makes the image a deterministic function of reviewed tracked source plus
the immutable inputs below. It does not claim byte-for-byte image identity:
Docker layer metadata and remote service availability can vary. The contract is
source reproducibility, content-addressed bases, exact dependency inputs, and
verified source attestation.

## Reviewed base provenance

All recorded digests are linux/amd64 manifests, not mutable tags:

| Component | Human-readable tag | Reviewed digest | Evidence |
| --- | --- | --- | --- |
| Gateway builder | `golang:1.22-alpine3.19` (resolved version `1.22.10-alpine3.19`) | `sha256:364133ae013cea7fbeb56f93ab38e01085d1bf87cf1b85d56c8598976c043530` | Docker Official Image; index at review was `sha256:6f73a1b8b608dad4866b9f746ac6888ffdb112f75ef59ed97c43b5f734368718` |
| Gateway runtime | `alpine:3.19` (resolved version `3.19.9`) | `sha256:b58899f069c47216f6002a6850143dc6fae0d35eb8b0df9300bbe6327b9c2171` | Docker Official Image; index at review was `sha256:6baf43584bcb78f2e5847d1de515f23499913ac9f12bdf834811a3145eb11ca1` |
| vLLM runtime | `docker.io/codingtensor/worker-vllm:engine-phase2` | `sha256:e3f6253d4c767d815fd6809b99d1f22fe24469472c53d3945ba8ec7f71d52331` | Published last-known-good RunPod worker, linux/amd64, created `2026-03-27T08:55:52Z` |

The vLLM runtime base was built on RunPod
`pytorch:2.4.0-py3.11-cuda12.4.1-devel-ubuntu22.04`. The current RunPod tag
resolves to `sha256:61a4aafb0094cd773f11eefa378929d5a687bd775febeb78eac62fc824141fb5`;
that lookup is provenance evidence only. The worker Dockerfile pins the
published runtime digest containing the actually exercised vLLM 0.18.0,
PyTorch 2.10.0, and CUDA dependency set instead of reconstructing it from a
mutable package service.

The published runtime did not have a source-revision label. It is accepted only
as a reviewed dependency input. Every new Infera worker image removes the old
`/app` source, installs the requested archived source non-editably without
dependency resolution, and carries the requested revision label.

## Dependency and package contract

The gateway keeps CGO enabled for SQLite and retains the PostgreSQL driver
through the reviewed Go module graph. Go modules are version-locked by
`go.mod`/`go.sum`; the build uses `-trimpath`, disables VCS probing, and removes
the Go build ID. Alpine packages are exact-version pins:

- builder: `gcc=13.2.1_git20231014-r0`,
  `musl-dev=1.2.4_git20230717-r6`
- runtime: `ca-certificates=20250911-r0`, `wget=1.21.4-r0`

Alpine 3.19 does not provide a first-party permanent dated repository snapshot.
Package versions therefore fail closed if the upstream repository stops
serving them, but upstream availability is a residual limitation. Do not
silently change the repository or loosen a pin to make a release build pass.

The worker performs no `apt`, pip upgrade, or remote dependency installation.
`python/requirements/worker-vllm.lock` is the exact active transitive closure
recovered from the immutable runtime base; its checksum is tracked separately
and verified in the image build. The Dockerfile then verifies every installed
distribution version before installing the reviewed local package with
`--no-deps --no-build-isolation --force-reinstall`. The existing vLLM import,
`AsyncEngineArgs`, and worker-adapter compatibility checks remain image gates.

This digest-plus-inventory approach is intentional. A hash-checked PyPI wheel
lock was not used because the approved build environment did not authorize
sending the private dependency set to an external resolver. The immutable base
digest content-addresses the installed artifacts without doing production-time
package resolution. A future move to a wheelhouse must review every wheel hash
and the CUDA/Python platform selection before replacing this contract.

## Build and verify

The revision must already exist as a commit object in the local repository:

```bash
revision="$(git rev-parse --verify 'HEAD^{commit}')"

./scripts/build-reviewed-release-image.sh \
  --component gateway \
  --revision "$revision" \
  --tag registry.example/infera-gateway:candidate

./scripts/build-reviewed-release-image.sh \
  --component worker-vllm \
  --revision "$revision" \
  --tag registry.example/infera-worker-vllm:candidate
```

Only `linux/amd64` is supported. Branches, tags, abbreviated or uppercase
object IDs, extra build arguments, unsafe tags, missing tracked paths, mutable
base defaults, and revision-label mismatches fail before a release can proceed.
The allowlists exclude `.git`, secret files, the mutable worktree, untracked
files, caches, tests not required at runtime, and local build output.

`scripts/build-docker.sh` also routes gateway and vLLM targets through this
builder and requires `RELEASE_REVISION`:

```bash
RELEASE_REVISION="$revision" VERSION=candidate \
  ./scripts/build-docker.sh --gateway --worker-vllm
```

After pushing under an operator-approved release workflow, record the registry
repo digest, not merely the writable tag, in the release manifest. Production
Compose is image-only for gateway and frontend; never add a production `build:`
fallback or use `compose up --build`.

## Reviewed refresh procedure

1. Start from a clean branch at the explicitly approved full commit.
2. Resolve a candidate tag with `docker buildx imagetools inspect`; record both
   the tag and linux/amd64 child digest. Never copy a digest from an unrelated
   architecture.
3. Inspect upstream release notes and security changes. For gateway bases,
   query exact package versions inside the candidate digest and update pins
   together. Do not add `apk upgrade`.
4. For the worker, identify a GPU-smoke-proven immutable runtime digest. Derive
   the active dependency closure from that exact image using
   `importlib.metadata`, update `worker-vllm.in`, deterministically sort
   `worker-vllm.lock`, and refresh `worker-vllm.lock.sha256`.
5. Run the static contract tests, Python vLLM compatibility tests, Go tests,
   shell syntax checks, Compose validation, and a real gateway build/inspect.
6. Build the worker on a linux/amd64 GPU-capable runner, verify the dependency
   inventory and vLLM API gates, then run one non-streaming and one streaming
   inference smoke before accepting the refreshed base. Record the image
   digest and evidence. Never refresh a base only because its tag moved.

Rollback selects the previously recorded gateway and worker repo digests. It
does not rebuild an old checkout.
