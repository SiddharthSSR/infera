# RunPod Worker Registration Reliability

Date: 2026-07-03

Linear issue: INF-35

## Problem Statement

A RunPod instance can reach provider state `running` while no inference worker registers with the gateway. In that state, the gateway remains reachable but degraded with zero workers. Inference fails, production benchmarks cannot run, and safe route metadata verification cannot prove successful route decisions.

The operator needs a clear way to tell whether the failure is in provider provisioning, network/proxy exposure, worker boot, model loading, gateway registration, or heartbeat health.

## Observed Incident

During INF-32 production verification on 2026-07-03, one RunPod worker was provisioned for `Qwen/Qwen2.5-7B-Instruct`.

| Field | Value |
| --- | --- |
| Provider instance ID | `2ticqy47pk6zn3` |
| GPU | `A100_80GB` |
| Cost | `$1.19/hr` |
| Provider state | `running` |
| Gateway worker state | `workers=0`, `healthy_workers=0` |
| Missing provider fields | public IP, HTTP port, SSH host, SSH port |
| Worker proxy health | no response |
| Final action | stopped after one failed restart attempt |
| Final production health | `status=degraded`, `workers=0`, `healthy_workers=0` |

The instance was stopped, started once more, and observed again. It still did not expose connectivity details or register with the gateway, so it was stopped to avoid leaving paid capacity running.

## Worker Lifecycle Chain

The expected RunPod worker lifecycle is:

1. Provider creates instance.
2. Provider exposes network or proxy details.
3. Worker container starts.
4. Worker health endpoint becomes reachable.
5. Model loading begins.
6. Model loading completes.
7. Worker registers with the gateway.
8. Gateway accepts registration.
9. Worker sends heartbeats.
10. `/v1/models` lists the served model.
11. Inference succeeds.

Every step should be observable enough that an operator can identify where the chain stopped.

## Failure States To Represent

The P0 implementation exposes `worker_registration_status` on managed instances. The instance API, dashboard, and operator runbooks should distinguish these states:

| State | Meaning |
| --- | --- |
| `pending` | Provider instance is not yet running or is still within the registration window. |
| `provider_running_no_network` | Provider reports the instance running, but public/proxy/SSH metadata is missing. |
| `provider_running_worker_unregistered` | Provider reports the instance running and network metadata is present, but no gateway worker registered before the deadline. |
| `worker_unreachable` | Network metadata exists, but the worker endpoint does not respond. |
| `worker_health_unavailable` | Worker responds but `/health` is missing, failing, or malformed. |
| `model_loading` | Worker is reachable and actively loading the configured model. |
| `model_load_failed` | Worker reached the model-load step and failed. |
| `registration_failed` | Worker loaded far enough to register but gateway rejected or did not accept registration. |
| `heartbeat_missing` | Gateway registered the worker but no current heartbeat is present. |
| `registered_unhealthy` | Gateway registry has the worker but marks it unhealthy. |
| `ready` | Provider, worker health, model load, registration, heartbeat, and model listing are all valid. |

P0 currently computes `pending`, `provider_running_no_network`, `provider_running_worker_unregistered`, `registration_failed`, `heartbeat_missing`, and `ready` from the provider status, network metadata, gateway worker link, and heartbeat timestamps. The remaining states are reserved for P1 worker-health and model-load probes.

## Implemented P0 Behavior

Managed instances now include these safe lifecycle fields in `/api/instances` and `/api/instances/{id}`:

| Field | Purpose |
| --- | --- |
| `worker_registration_status` | Current provider-to-gateway registration state. |
| `worker_registration_deadline` | Time by which a running provider instance should have registered a gateway worker. |
| `last_worker_registration_error` | Operator-facing non-secret explanation for the current failure state. |
| `last_worker_registration_check_at` | Last time the gateway reconciled registration state. |
| `worker_registered_at` | First observed time the gateway linked a worker to this instance. |
| `worker_last_heartbeat_at` | Last heartbeat observed for the linked worker. |
| `worker_health_url` | Safe health URL derived from public/proxy metadata when available. |
| `provider_network_ready` | Whether provider network metadata is sufficient for worker reachability. |
| `provider_network_error` | Operator-facing non-secret explanation when provider network metadata is missing. |

Registration timeout is controlled by `INFERA_WORKER_REGISTRATION_TIMEOUT` on the gateway. The default is `10m`. If an instance reaches provider `running` with usable network metadata but no gateway worker registers before this deadline, the instance surfaces `provider_running_worker_unregistered`.

If an instance reaches provider `running` without a public/proxy endpoint, the gateway surfaces `provider_running_no_network` immediately. This matches the observed `2ticqy47pk6zn3` failure mode where RunPod reported `running` but did not provide enough endpoint metadata for a worker health check or registration diagnosis.

The dashboard uses the same lifecycle fields in its instance readiness badge. It shows prominent error states such as `NO NETWORK`, `WORKER NOT REGISTERED`, `REGISTRATION FAILED`, and `HEARTBEAT MISSING` while preserving the existing serving-ready path for healthy registered workers.

The API does not expose provider credentials, worker registration tokens, authorization headers, API keys, or raw provider metadata.

## Diagnostic Commands And Checks

These checks must not print API keys, worker shared tokens, provider credentials, or authorization headers.

1. Check public gateway health:

   ```bash
   curl -fsS https://inferai.co.in/health
   ```

2. Check managed instances with an authorized operator key:

   ```bash
   curl -fsS \
     -H "Authorization: Bearer ${INFERA_ADMIN_KEY}" \
     https://inferai.co.in/api/instances
   ```

   Inspect only non-secret fields such as instance ID, provider ID, status, GPU type, cost, public IP, HTTP port, SSH host, and SSH port.

3. Check provider status:

   ```bash
   curl -fsS \
     -H "Authorization: Bearer ${INFERA_ADMIN_KEY}" \
     https://inferai.co.in/api/providers
   ```

   Compare provider active instance counts with gateway worker counts. Provider counts may include stopped RunPod pods, so gateway health remains the source of truth for serving readiness.

4. Check provider instance metadata:

   ```bash
   curl -fsS \
     -H "Authorization: Bearer ${INFERA_ADMIN_KEY}" \
     https://inferai.co.in/api/instances/<instance_id>
   ```

   Confirm whether public/proxy/SSH details are present without printing credentials.

5. Check worker proxy health if a provider ID is available:

   ```bash
   curl -kfsS --max-time 10 \
     https://<provider_id>-8081.proxy.runpod.net/health
   ```

6. Check gateway worker registry through trusted internal access:

   ```bash
   docker compose -f docker-compose.prod.yml exec -T gateway \
     wget -qO- http://127.0.0.1:8080/internal/prometheus/worker-targets
   ```

7. Inspect worker logs if accessible through the provider or SSH. Focus on startup, model load, registration, and heartbeat messages. Do not print tokens or environment values.

8. Verify registration inputs are present without printing values:

   - worker shared registration token is configured;
   - gateway URL points at production;
   - model ID matches the provision request;
   - worker image is pinned and expected for the model runtime.

9. Verify authenticated model listing only after a worker is expected to be ready:

   ```bash
   curl -fsS \
     -H "Authorization: Bearer ${INFERA_SMOKE_API_KEY}" \
     https://inferai.co.in/v1/models
   ```

## Implementation Roadmap

### P0

- Surface provider-running-but-unregistered state in the instance API and dashboard. Implemented with `worker_registration_status`.
- Add a timeout for worker registration after provider state becomes running. Implemented with `INFERA_WORKER_REGISTRATION_TIMEOUT`, default `10m`.
- Store and expose last worker startup error/status. Implemented with `last_worker_registration_error` and `last_worker_registration_check_at`.
- Improve provider instance reconciliation when network/proxy details are missing. Implemented with `provider_network_ready`, `provider_network_error`, and `worker_health_url`.
- Add a release/smoke verification check that fails clearly if provider instances are running but gateway workers are zero.

### P1

- Add a worker startup event timeline.
- Add dashboard lifecycle visualization.
- Add a retry/recreate policy when RunPod proxy details never appear.
- Add an alert for provider running but no gateway worker after a configured duration.
- Probe worker health and model-load state so `worker_unreachable`, `worker_health_unavailable`, `model_loading`, and `model_load_failed` can be populated from direct worker observations.

### P2

- Add automatic recovery or reprovision-one-worker flow.
- Add cost-aware cleanup for stuck provider instances.
- Add a historical provider reliability report.

## Relationship To INF-32

INF-32 safe route metadata is merged and deployed, and the no-worker failure-path header behavior was verified. INF-32 cannot be marked Done until a healthy worker can be reliably started and successful route metadata can be verified for non-streaming, streaming, and `infera-bench --capture-route-decision`.

This issue should unblock the final INF-32 production verification retry.

## Success Condition

A future operator can diagnose whether a RunPod worker startup failure is caused by:

- provider provisioning;
- missing network or proxy metadata;
- worker container boot;
- model loading;
- gateway registration;
- heartbeat health;
- a genuinely ready worker path.

The system should expose enough state that a provider-running instance with zero gateway workers is an explicit, actionable failure mode instead of an ambiguous degraded state.
