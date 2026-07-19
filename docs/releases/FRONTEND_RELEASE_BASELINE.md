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
container in place. Frontend rollback is also not immutable: `docker-compose.prod.yml` builds the
frontend locally and does not name a pinned frontend image. Adding a frontend image/revision to the
manifest, coordinated promotion/rollback, and automated identity verification should be reviewed
as a release-contract change before it is used in production.

## Baseline for follow-up frontend work

Use reviewed `origin/main`, not the live `4360155` tree, as the implementation base. At the time of
this audit that base is `2d2a021`. Rebase or merge the latest reviewed `origin/main` immediately
before opening the implementation PR, then record the resulting full commit SHA in the preview
evidence.

Do not treat the production landing page as the design source of truth. It is useful only as the
known-old comparison build until an intentional frontend promotion occurs.

## Safe preview procedure

Run the source build first:

```bash
git rev-parse HEAD
cd frontend
npm ci
npm run test:run
npm run build
sed -n '/assets\/index-/p' dist/index.html
cd ..
```

For a production-shaped, local static preview, build and run the same Dockerfile on loopback:

```bash
PREVIEW_SHA="$(git rev-parse --short=12 HEAD)"
docker build -f deploy/docker/Dockerfile.frontend -t "infera-frontend-preview:${PREVIEW_SHA}" .
docker run --rm -p 127.0.0.1:3001:3000 "infera-frontend-preview:${PREVIEW_SHA}"
```

Open `http://127.0.0.1:3001` and record the commit SHA plus the JS/CSS asset names from the returned
HTML. Static login and public routes can be reviewed this way. API-backed authenticated flows need
an isolated preview stack with non-production credentials; do not point an unreviewed preview at
the production gateway.

## Safe release procedure under the current contract

Until frontend identity is added to the immutable release manifest, frontend promotion is an
explicit operator step and must not be inferred from a successful gateway recovery rollout.

1. Approve a specific full commit SHA and retain the successful test/build evidence and generated
   JS/CSS asset names from the preview.
2. Before changing a production-like canary, capture both the running frontend image ID and the
   Compose image reference. Retagging that immutable ID is the rollback path for the current
   unpinned Compose contract:

   ```bash
   FRONTEND_CONTAINER_ID="$(docker compose -f docker-compose.prod.yml ps -q frontend)"
   PREVIOUS_FRONTEND_IMAGE_ID="$(docker inspect --format '{{.Image}}' "${FRONTEND_CONTAINER_ID}")"
   FRONTEND_IMAGE_REF="$(docker inspect --format '{{.Config.Image}}' "${FRONTEND_CONTAINER_ID}")"
   docker image inspect "${PREVIOUS_FRONTEND_IMAGE_ID}" "${FRONTEND_IMAGE_REF}" >/dev/null
   ```

3. Check out the exact approved SHA, render the real Compose configuration, explicitly build the
   frontend service, and recreate only that service:

   ```bash
   git rev-parse HEAD
   docker compose -f docker-compose.prod.yml config --quiet
   docker compose -f docker-compose.prod.yml build --no-cache frontend
   docker compose -f docker-compose.prod.yml up -d --no-deps --force-recreate frontend
   docker compose -f docker-compose.prod.yml ps frontend
   ```

4. Fetch the canary root document, confirm that its JS/CSS asset names exactly match the approved
   preview, exercise the public/login routes, and run the normal release verification with the
   required smoke credentials.
5. Promote the same reviewed SHA through the same explicit build/recreate sequence during the
   approved production window. Confirm the live root asset names before declaring success.
6. Keep the previous frontend image ID until the watch window closes. Because the current Compose
   contract does not pin a frontend image, record that image ID and the prior source SHA in the
   release evidence; rebuilding an unrecorded branch is not an adequate rollback plan.

Do not change DNS or Caddy routing for a frontend-only release. If the candidate asset fingerprint,
container health, login/public-route checks, or release verification differs from the approved
canary, stop and restore the recorded previous frontend image while leaving gateway/worker release
state unchanged:

```bash
docker image tag "${PREVIOUS_FRONTEND_IMAGE_ID}" "${FRONTEND_IMAGE_REF}"
docker compose -f docker-compose.prod.yml up -d --no-deps --force-recreate frontend
```

Run the same root fingerprint and health checks after rollback. Stop and escalate if the recorded
image ID is missing or the Compose image reference cannot be established; do not rebuild a guessed
revision during an incident.

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
