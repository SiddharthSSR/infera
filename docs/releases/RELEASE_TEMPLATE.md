# Release Template

Use this as the GitHub release body when promoting `roadmap` to `main`.

## Title

`vX.Y.Z`

## Summary

Short release summary:

- what changed for users
- what changed operationally
- why this release matters

## Highlights

- Item 1
- Item 2
- Item 3

## Included Work

### Reliability and Deployment
- Deployment/runtime changes
- CI/smoke-test changes
- Auth/bootstrap changes

### Observability
- Dashboard/Prometheus/Alertmanager changes
- Worker discovery changes
- Metrics/alerts changes

### API and Gateway
- OpenAI-compatible API changes
- Streaming fixes
- Contract/regression coverage

### Frontend and UX
- Dashboard/login/playground changes
- Session/auth changes

## Verification

- `go test ./...`
- `pytest -q`
- `npm run test:run`
- `npm run build`
- `docker compose -f docker-compose.prod.yml config`
- production compose smoke checks
- post-deploy `./scripts/release-verify.sh`

## Deploy Notes

- Required env vars confirmed
- Grafana/Alertmanager credentials configured
- Dashboard DNS/TLS confirmed
- Worker discovery endpoint checked

## Known Follow-ups

- Follow-up 1
- Follow-up 2

## Tag

```bash
git tag -a vX.Y.Z -m "vX.Y.Z release"
git push origin vX.Y.Z
```
