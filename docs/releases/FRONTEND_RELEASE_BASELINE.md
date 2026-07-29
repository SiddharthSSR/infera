# Frontend release baseline

Date: 2026-07-19

This note records the source and serving path observed for `https://inferai.co.in`. It is a
release-planning artifact, not evidence that a deployment was performed.

## Observed production state

- Caddy serves `inferai.co.in` and proxies the catch-all route to the Compose `frontend:3000`
  service. The frontend image is built by `deploy/docker/Dockerfile.frontend` from `frontend/` and
  nginx serves the generated `dist/` directory.
- The live root response includes `Via: 1.1 Caddy`, the security headers configured by
  `deploy/docker/nginx.conf`, and these Vite assets:
  - `/assets/index-Cv_EBqnc.js`
  - `/assets/index-BCSeRdCB.css`
- A clean build of commit `436015587cb35d85b0b451f47c13d136432b1f7d` produces those exact
  asset names. That commit is therefore the identified frontend source revision. Its commit date is
  2026-07-04; the live root document reports `Last-Modified: Wed, 15 Jul 2026 10:26:50 GMT`, which
  identifies the later image build/installation time rather than a later source revision.
- At the same observation time, `/health` reported release ID `main-2d2a021-inf49`. A clean build of
  repository commit `2d2a021` instead produces `/assets/index-CG3b0oGD.js` and
  `/assets/index-BMIt_SzI.css`.
- No `vercel.json`, `.vercel/` metadata, or deployment workflow for Vercel exists in this
  repository. The authoritative checked-in production path is DigitalOcean, Docker Compose,
  Caddy, and nginx.

These facts show that the gateway was advanced while the frontend container retained the build
from `4360155`. They do not reveal which operator command performed that gateway update.

## Why the release identities diverged

The coordinated recovery contract currently versions gateway and worker images only:

- `deploy/releases/release.manifest.example` has gateway and worker image fields but no frontend
  image or source revision.
- `scripts/compose-release-driver.sh deploy-gateway` recreates only the `gateway` service.
- `scripts/release-verify.sh` checks that the site root responds, but does not check a frontend
  revision or asset fingerprint.

Consequently, a gateway/worker recovery rollout can pass while leaving an older healthy frontend
container in place. Frontend rollback was also not immutable at the time of the audit:
`docker-compose.prod.yml` built the frontend locally and did not name a pinned frontend image.
Adding a frontend image/revision to the manifest, coordinated promotion/rollback, and automated
identity verification should be reviewed as a release-contract change before it is used in
production.

## Baseline for follow-up frontend work

Use reviewed `origin/main`, not the live `4360155` tree, as the implementation base. INF-74 started
from exact `origin/main` commit `7a270653bdef98966fbee1cd48531630449b73cf`. Rebase or merge the
latest reviewed `origin/main` immediately before opening later implementation PRs, then record the
resulting full commit SHA in the preview evidence.

Do not treat the production landing page as the design source of truth. It is useful only as the
known-old comparison build until an intentional frontend promotion occurs.

## Reviewed-source build invariant

The release identity and release inputs must come from the same Git commit. Do not run `docker
build .` or `docker compose build frontend` for a release: those commands read the mutable working
directory. `.dockerignore` remains defense in depth for generic repository builds, not a provenance
control.

`scripts/build-reviewed-frontend.sh` resolves an explicit revision to a full commit ID, creates a
temporary archive containing only the reviewed `.dockerignore`, `frontend/`,
`deploy/docker/Dockerfile.frontend`, and `deploy/docker/nginx.conf`, and builds that context with
`--pull`, `--no-cache`, and `--platform linux/amd64`. The context is removed on success, failure, or
interruption. Dirty tracked files, staged changes, arbitrary untracked public assets, local
credentials, recovery evidence, `node_modules`, `dist`, caches, and host metadata are not read from
the working directory and cannot enter the reviewed build.

The script reports the resolved source revision, context file count and size, local image ID, and
the verified `org.opencontainers.image.revision` label. Missing or invalid revisions fail before
Docker runs.

Production Compose defines the frontend as image-only and requires `INFERA_FRONTEND_IMAGE`.
It intentionally has no frontend build configuration, so Compose cannot rebuild or retag the
frontend from a mutable production checkout.

## Safe preview procedure

Approve a full commit ID, run the normal source checks, then build the production-shaped image from
that exact Git object:

```bash
REVIEWED_REVISION="$(git rev-parse --verify origin/main^{commit})"
test "${#REVIEWED_REVISION}" -eq 40
git diff --check "${REVIEWED_REVISION}^" "${REVIEWED_REVISION}"
git show --no-patch --format=fuller "${REVIEWED_REVISION}"

PREVIEW_SHA="${REVIEWED_REVISION:0:12}"
PREVIEW_IMAGE="infera-frontend-preview:${PREVIEW_SHA}"
mkdir -p recovery-evidence
./scripts/build-reviewed-frontend.sh \
  --revision "${REVIEWED_REVISION}" \
  --tag "${PREVIEW_IMAGE}" |
  tee "recovery-evidence/frontend-build-${PREVIEW_SHA}.txt"
docker image inspect \
  --format 'id={{.Id}} revision={{index .Config.Labels "org.opencontainers.image.revision"}}' \
  "${PREVIEW_IMAGE}"
docker run --rm \
  --name infera-frontend-reviewed-preview \
  --add-host gateway:127.0.0.1 \
  -p 127.0.0.1:3001:3000 \
  "${PREVIEW_IMAGE}"
```

The nginx configuration resolves its `gateway:8080` upstream when it starts. The loopback host
mapping satisfies name resolution without joining a Compose network or connecting to any gateway.
It does not create an API listener.

In another terminal, verify nginx, static routes, the exact asset fingerprint, and the expected
failure of proxied routes:

```bash
docker exec infera-frontend-reviewed-preview nginx -t
for route in / /login /security /trust; do
  curl --fail --silent --show-error "http://127.0.0.1:3001${route}" >/dev/null
done
curl --fail --silent --show-error http://127.0.0.1:3001/ |
  grep -oE '/assets/index-[A-Za-z0-9_-]+\.(js|css)' |
  sort -u
for route in /api/ /v1/models /health; do
  status="$(curl --silent --output /dev/null --write-out '%{http_code}' \
    "http://127.0.0.1:3001${route}")"
  test "${status}" = 502
done
```

The asset-fingerprint command comes from the useful verification improvement in PR #113; this
procedure does not depend on or merge that stale branch. Record the full commit, build context
count/size, image ID, revision label, and sorted JS/CSS names. API-backed authenticated flows need
an isolated preview stack with non-production credentials; never point an unreviewed preview at
the production gateway.

## Safe release procedure under the current contract

Until frontend identity is added to the immutable release manifest, frontend promotion is an
explicit operator step and must not be inferred from a successful gateway recovery rollout.

1. Approve a specific full commit SHA and retain the successful tests, reviewed-build output,
   source label, context count/size, and generated JS/CSS asset names from the preview.
2. Before changing a production-like canary, capture the running frontend image ID, immutable repo
   digest, and full source revision label. All three are required for rollback:

   ```bash
   FRONTEND_CONTAINER_ID="$(docker compose -f docker-compose.prod.yml ps -q frontend)"
   PREVIOUS_FRONTEND_IMAGE_ID="$(docker inspect --format '{{.Image}}' "${FRONTEND_CONTAINER_ID}")"
   PREVIOUS_FRONTEND_IMAGE_REF="$(docker inspect --format '{{.Config.Image}}' "${FRONTEND_CONTAINER_ID}")"
   PREVIOUS_FRONTEND_SOURCE_REVISION="$(docker image inspect --format \
     '{{index .Config.Labels "org.opencontainers.image.revision"}}' \
     "${PREVIOUS_FRONTEND_IMAGE_ID}")"
   ./scripts/validate-worker-image-pin.sh \
     "${PREVIOUS_FRONTEND_IMAGE_REF}" PREVIOUS_FRONTEND_IMAGE_REF --require-digest
   [[ "${PREVIOUS_FRONTEND_SOURCE_REVISION}" =~ ^[0-9a-f]{40}$ ]]
   docker image inspect "${PREVIOUS_FRONTEND_IMAGE_ID}" "${PREVIOUS_FRONTEND_IMAGE_REF}" >/dev/null
   ```

3. Build once from the approved Git revision, push that image under an immutable release tag, and
   record the registry digest printed by `docker push`. A local image ID is useful evidence but is
   not a portable registry digest:

   ```bash
   REVIEWED_REVISION="<approved-full-commit-sha>"
   FRONTEND_IMAGE="docker.io/<namespace>/infera-frontend:${REVIEWED_REVISION}"
   mkdir -p recovery-evidence
   ./scripts/build-reviewed-frontend.sh \
     --revision "${REVIEWED_REVISION}" \
     --tag "${FRONTEND_IMAGE}" |
     tee "recovery-evidence/frontend-build-${REVIEWED_REVISION}.txt"
   docker push "${FRONTEND_IMAGE}" |
     tee "recovery-evidence/frontend-push-${REVIEWED_REVISION}.txt"
   FRONTEND_REPO_DIGEST="$(docker image inspect \
     --format '{{index .RepoDigests 0}}' "${FRONTEND_IMAGE}")"
   test -n "${FRONTEND_REPO_DIGEST}"
   printf 'source=%s\nimage=%s\n' "${REVIEWED_REVISION}" "${FRONTEND_REPO_DIGEST}"
   ```

4. Run the checked-in exact-source guard with the same approved full revision and immutable digest.
   The guard reads `docker-compose.prod.yml` from that Git object rather than the working tree,
   privately renders it with the repository project directory and `ENV_FILE` (default `.env`),
   rejects a dirty or unavailable source, a mutable image, a frontend `build` field, or an image
   mismatch, pulls the exact digest, proves its OCI revision label equals the requested source, and
   recreates only `frontend` with `--no-build --no-deps`. It reports success only after the
   recreated container's configured digest, actual image ID, and OCI revision match those reviewed
   inputs:

   ```bash
   ./scripts/frontend-compose-action.sh candidate \
     --source-revision "${REVIEWED_REVISION}" \
     --image "${FRONTEND_REPO_DIGEST}"
   INFERA_FRONTEND_IMAGE="${FRONTEND_REPO_DIGEST}" \
     docker compose -f docker-compose.prod.yml ps frontend
   ```

   Never use an on-host `docker-compose.prod.yml`—including
   `/opt/infera/docker-compose.prod.yml`—as release evidence or an unverified action source. A clean
   checkout may still be stale. The explicit source revision is the contract.

5. Fetch the canary root document, confirm that its JS/CSS asset names exactly match the approved
   preview, exercise public/login routes, and run normal release verification with the required
   smoke credentials.
6. Promote the same recorded image digest during the approved production window; do not rebuild.
   Confirm the live source label and root asset names before declaring success.
7. Keep the previous frontend image ID and immutable reference until the watch window closes.
   Record both plus the prior source SHA in release evidence; rebuilding an unrecorded branch is not
   an adequate rollback plan.

Do not change DNS or Caddy routing for a frontend-only release. If the candidate asset fingerprint,
container health, login/public-route checks, or release verification differs from the approved
canary, stop and restore the recorded previous frontend image while leaving gateway/worker release
state unchanged:

```bash
docker image inspect "${PREVIOUS_FRONTEND_IMAGE_ID}" "${PREVIOUS_FRONTEND_IMAGE_REF}" >/dev/null
./scripts/frontend-compose-action.sh rollback \
  --source-revision "${PREVIOUS_FRONTEND_SOURCE_REVISION}" \
  --image "${PREVIOUS_FRONTEND_IMAGE_REF}"
```

Run the same root fingerprint and health checks after rollback. Stop and escalate if the recorded
image ID is missing or the Compose image reference cannot be established; do not rebuild a guessed
revision during an incident. Configuration or host recovery uses the identical guard with the
`restore` action, the recorded full source revision, and recorded digest. After the service is
stable, reconcile the operator checkout to the reviewed revision through the normal reviewed
checkout/update procedure; checkout reconciliation is follow-up hygiene and never a substitute for
the exact-source guard.

## Pinned base images

The multi-stage Dockerfile pins reviewed Docker Official Image indexes while the release builder
targets `linux/amd64`:

- builder: `node:20.19.0-alpine@sha256:8bda036ddd59ea51a23bc1a1035d3b5c614e72c01366d989f4120e8adca196d4`
  (amd64 child `sha256:37a5a350292926f98d48de9af160b0a3f7fcb141566117ee452742739500a5bd`,
  upstream revision `e028becede0527249b105c22a3881412641b6d45`)
- runtime: `nginx:1.31.3-alpine@sha256:4a73073bd557c65b759505da037898b61f1be6cbcc3c2c3aeac22d2a470c1752`
  (amd64 child `sha256:1d40e3eb3bf4f138de1d67193f2aa5309fcaf343eb5ffadbf5e9439de1eb1ebb`,
  upstream revision `ccdab6c99ae2e2fc53a144dc68d6b8f44163adf2`)

Refresh either pin only in a reviewed PR. Resolve the intended Docker Official Image tag with
`docker buildx imagetools inspect <tag>`, review its upstream source/revision annotations and
`linux/amd64` child, update the human-readable tag and immutable index digest together, then rerun
the reviewed-context regression, clean/no-cache image build, source-label check, `nginx -t`, static
and proxied route checks, and exact asset fingerprint comparison. Never replace a digest with a
tag-only `FROM`.

## Evidence commands used for this audit

```bash
curl -sSIL https://inferai.co.in/
curl -sS https://inferai.co.in/
curl -sS https://inferai.co.in/health
git archive --format=tar --output=/tmp/infera-frontend-436015.tar \
  436015587cb35d85b0b451f47c13d136432b1f7d frontend deploy/docker
mkdir /tmp/infera-frontend-436015
tar -xf /tmp/infera-frontend-436015.tar -C /tmp/infera-frontend-436015
(cd /tmp/infera-frontend-436015/frontend && npm ci && npm run build)
```

The historical build used the repository lockfile, whose SHA-256 is identical at `4360155` and
`2d2a021`. No production resources, DNS records, or deployment configuration were mutated.
