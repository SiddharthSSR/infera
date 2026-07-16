# RunPod Worker Registration Production Verification

## Summary

| Field | Value |
| --- | --- |
| Linear issue | `INF-35` |
| Verification date | `2026-07-15` |
| Result | Blocked before provider instance creation |
| Deployed main commit | `436015587cb35d85b0b451f47c13d136432b1f7d` |
| Gateway image ID | `sha256:ca81699802852e6fe8d4976327996a90838f9e99604a7273169f01c989693476` |
| Frontend image ID | `sha256:ed05d60db434d94bc9fe47abceaf58280e2ac1ad25353f0b7f3f5395f8f5ae5a` |
| Frontend build asset | `index-Cv_EBqnc.js` |
| Requested provider | RunPod |
| Requested GPU | `A100_80GB` (`NVIDIA A100 80GB PCIe`) |
| Expected hourly cost | `$1.19/hour` |
| Requested model | `Qwen/Qwen2.5-7B-Instruct` |
| Provisioning request time | `2026-07-15T10:30:55Z` |
| Provider instance ID | Not created |
| Final worker state | No worker created; no worker to stop |

Production was successfully redeployed from the merged `main` commit. The single permitted RunPod provision request was rejected because the provider account balance was too low. The request produced no managed instance or provider instance ID, and RunPod reported zero active instances afterward. Because no worker existed, the new lifecycle diagnostics could not be observed in production and `INF-35` must remain In Progress.

## Deployment And Endpoint Verification

The production checkout fast-forwarded from `7a5094e01e527c64a2cf75bdbbcdd4f3026a7622` to `436015587cb35d85b0b451f47c13d136432b1f7d`. The repository's production environment validation passed with values hidden, Compose configuration rendered successfully, and the stack was rebuilt with `docker compose -f docker-compose.prod.yml up -d --build --force-recreate`.

Post-deploy checks:

- gateway container: running and healthy;
- frontend container: running and healthy;
- public `/health`: responded with `status=degraded`, `workers=0`, and `healthy_workers=0`;
- dashboard `/api/health`: responded successfully;
- authenticated `/api/instances`: responded successfully;
- internal gateway `/metrics`: responded with nine `infera_` metric families;
- deployed dashboard entry page loaded and showed `Gateway online` with `0 workers connected`.

## Zero-Worker Baseline

Before provisioning, authenticated `/api/instances` returned `total=0`. No instance was running, provisioning, or stopping. Public health correctly reported a degraded deployment with zero workers.

The response was recursively checked for key names matching API keys, tokens, authorization data, credentials, or secrets. No such keys were present.

## Provision Attempt

Exactly one provision request was sent with the following non-secret configuration:

- provider: RunPod;
- GPU: `A100_80GB`;
- provider GPU type: `NVIDIA A100 80GB PCIe`;
- GPU count: one;
- model: `Qwen/Qwen2.5-7B-Instruct`;
- expected cost: `$1.19/hour`.

The gateway returned HTTP 503. Sanitized deployment history recorded outcome `request_failed` and the provider explanation that the account balance was too low to rent a pod. The request was not retried.

After the rejection:

- authenticated `/api/instances` still returned `total=0`;
- no running, provisioning, or stopping managed instance existed;
- RunPod provider status reported `connected=true` and `active_instances=0`;
- no provider instance ID or worker ID was created;
- no paid worker was left running.

## Lifecycle Timeline And API States

| Time (UTC) | Observed state |
| --- | --- |
| Before `2026-07-15T10:30:55Z` | Production deployed; zero instances; health degraded with zero workers |
| `2026-07-15T10:30:55Z` | One RunPod A100 80GB provision request submitted |
| `2026-07-15T10:30:56Z` | Request failed before instance creation because the provider account balance was insufficient |
| After failure | Managed instances remained zero; provider active instances remained zero |

No provider provisioning, provider running, network readiness, registration pending, registered, heartbeat, ready, or registration-timeout state could be observed because RunPod rejected the request before creating an instance.

## Dashboard And Diagnostic Behavior

The production frontend built successfully and the deployed entry page loaded with the correct zero-worker summary. An authenticated lifecycle warning or error card could not be exercised because no instance existed. Therefore this run does not establish whether the production dashboard correctly renders `provider_running_no_network`, `provider_running_worker_unregistered`, `registration_failed`, `heartbeat_missing`, or `ready`.

The production API deployment was confirmed, but the newly added per-instance lifecycle fields were not observable on a live instance for the same reason. The required acceptance condition—observing the new lifecycle diagnostics in production—was not met.

## Safe-Field And Security Validation

The zero-instance `/api/instances` response was checked for secret-like field names and did not expose API keys, RunPod credentials, worker registration tokens, authorization headers, or private provider credentials. No secrets, tokens, raw authorization headers, provider credentials, or sensitive raw logs are included in this report.

Because no instance was created, populated lifecycle fields such as provider status, registration status, network readiness, registration error, worker ID, heartbeat, model, GPU, and hourly cost could not be validated together on a production instance.

## Final State

- Worker stop action: not applicable; no worker or provider instance was created.
- Managed active instances: zero.
- RunPod active instances: zero.
- Public production health: degraded with zero workers.
- `INF-35`: keep In Progress.
- `INF-32`: keep blocked; worker registration and safe route metadata verification must not be retried yet.

## Limitations And Follow-Up

1. Add sufficient RunPod account funds without changing the deployed code.
2. Repeat the production verification with exactly one A100 80GB worker.
3. Observe either a successful registered/heartbeat/ready lifecycle or a correctly surfaced network/registration failure through both the authenticated API and dashboard.
4. Stop the worker and confirm zero active managed/provider instances.
5. Mark `INF-35` Done only after those lifecycle diagnostics are observed in production.
6. Retry `INF-32` only if worker registration succeeds.
