# Infera Deployment Checklist

Use this checklist for production deployments of `main` to the VM.

For the `roadmap -> main` release gate, use [docs/releases/ROADMAP_MAIN_RELEASE_CHECKLIST.md](/Users/siddharthsingh/codingtensor/infera/docs/releases/ROADMAP_MAIN_RELEASE_CHECKLIST.md) first, then use this file for the VM deploy itself.

## 1. Pre-Deploy

- [ ] SSH access works from your current IP.
- [ ] VM firewall allows inbound `22`, `80`, `443`.
- [ ] Repo is up to date:

```bash
git checkout main
git pull origin main
```

- [ ] `.env` is present and complete.
- [ ] Required env vars are set:
  - [ ] `INFERA_ADMIN_KEY`
  - [ ] `INFERA_ALLOWED_ORIGINS`
  - [ ] `INFERA_GATEWAY_ADDRESS`
  - [ ] `INFERA_WORKER_SHARED_TOKEN`
  - [ ] `INFERA_WORKER_IMAGE`
  - [ ] `GRAFANA_ADMIN_USER`
  - [ ] `GRAFANA_ADMIN_PASSWORD`
  - [ ] `ALERT_EMAIL_TO`
  - [ ] `ALERT_SMTP_FROM`
  - [ ] `ALERT_SMTP_SMARTHOST`
  - [ ] `ALERT_SMTP_USERNAME`
  - [ ] `ALERT_SMTP_PASSWORD`
- [ ] Worker image is pinned to a non-`latest` tag or digest:

```bash
./scripts/validate-worker-image-pin.sh
```

- [ ] Production compose renders with the current `.env`:

```bash
docker compose -f docker-compose.prod.yml config --quiet
```

- [ ] `data` directory exists for persistent DB files:

```bash
mkdir -p data
```

## 2. Deploy

- [ ] Stop old stack:

```bash
docker compose -f docker-compose.prod.yml down --remove-orphans
```

- [ ] Build and start:

```bash
docker compose -f docker-compose.prod.yml up -d --build --force-recreate
```

- [ ] Confirm services are up:

```bash
docker compose -f docker-compose.prod.yml ps
```

- [ ] Validate gateway-backed worker discovery before enabling observability alerts:

```bash
docker compose -f docker-compose.prod.yml exec -T gateway \
  wget -qO- http://127.0.0.1:8080/internal/prometheus/worker-targets
```

## 3. Post-Deploy Verification

- [ ] Site responds:

```bash
curl -I https://inferai.co.in
```

- [ ] Health endpoint responds:

```bash
curl -I https://inferai.co.in/health
```

- [ ] Protected API is protected (expect `401` without API key):

```bash
curl -i https://inferai.co.in/api/stats
```

- [ ] Gateway logs look healthy:

```bash
docker compose -f docker-compose.prod.yml logs gateway --tail=200
```

- [ ] Frontend logs have no `emerg` errors:

```bash
docker compose -f docker-compose.prod.yml logs frontend --tail=200
```

- [ ] Caddy logs have no persistent upstream `502` errors:

```bash
docker compose -f docker-compose.prod.yml logs caddy --tail=200
```

- [ ] Grafana endpoint responds:

```bash
curl -I https://dashboard.inferai.co.in
```

- [ ] Prometheus target discovery is healthy:

```bash
docker compose -f docker-compose.prod.yml logs prometheus --tail=200
```

- [ ] Grafana logs show datasource/dashboard provisioning success:

```bash
docker compose -f docker-compose.prod.yml logs grafana --tail=200
```

- [ ] Alertmanager config loaded and notifications pipeline is healthy:

```bash
docker compose -f docker-compose.prod.yml logs alertmanager --tail=200
```

- [ ] Run the consolidated release verification script:

```bash
INFERA_SMOKE_API_KEY=<admin-or-smoke-key> \
INFERA_SMOKE_MODEL=<model-id-if-you-want-inference-checks> \
INFERA_SMOKE_STREAM=1 \
./scripts/release-verify.sh https://inferai.co.in
```

## 4. Functional Smoke Test

- [ ] Open `https://inferai.co.in` in browser.
- [ ] Open `https://dashboard.inferai.co.in` and log in.
- [ ] Session-based login works (UI login sets and persists cookies/session).
- [ ] API-key auth works (verify token-based access), if still used by your clients.
- [ ] Dashboard loads.
- [ ] Instances page loads.
- [ ] Playground page loads and can issue a request.
- [ ] Trigger a test alert (or silence flow) and verify email delivery path.

## 5. Rollback Plan

If deployment fails:

1. [ ] Check which service is unhealthy:

```bash
docker compose -f docker-compose.prod.yml ps
```

2. [ ] Inspect failing service logs:

```bash
docker compose -f docker-compose.prod.yml logs <service> --tail=300
```

3. [ ] Roll back to last known good commit:

```bash
git log --oneline -n 10
git checkout <known-good-commit>
docker compose -f docker-compose.prod.yml down --remove-orphans
docker compose -f docker-compose.prod.yml up -d --build --force-recreate
```

## 6. Known Failure Patterns

### A) Gateway unhealthy / restart loop
- Symptom: vault/auth DB open errors.
- Fix: ensure `./data:/app/data` volume exists and `data/` is writable.

### B) Frontend 502 via Caddy
- Symptom: Caddy `lookup frontend ... status 502`.
- Fix: inspect frontend logs and fix nginx startup errors first.

### C) SSH timeout
- Symptom: `ssh ... port 22: Operation timed out`.
- Fix: add current public IP to VM firewall allowlist for `22/tcp`.

### D) CI compose smoke fails on bootstrap admin key
- Symptom: `failed to store bootstrap admin key`.
- Fix: ensure CI/test keys match `inf_` + 48 hexadecimal characters.

## 7. Release Hygiene

- [ ] Tag release after successful verification:

```bash
git tag -a v1.1.0 -m "v1.1.0 release"
git push origin v1.1.0
```

- [ ] Add/update GitHub release notes using `docs/releases/RELEASE_TEMPLATE.md`.
- [ ] Record deployment timestamp and commit hash.

---

## Deployment Record (fill each release)

- Date/Time:
- Deployer:
- Branch:
- Commit:
- Tag:
- Result: Success / Failed
- Notes:
