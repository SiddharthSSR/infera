# Safe Route Metadata Production Verification

Date: 2026-07-03

Status: blocked after partial verification.

Linear issue: INF-32

## Purpose

Verify that safe route decision metadata for benchmarks is deployed to production and behaves as an authenticated, opt-in response header without exposing prompts, credentials, provider secrets, or raw response bodies.

## Deployment

| Field | Value |
| --- | --- |
| Merged branch | `task/safe-route-decision-metadata` |
| Merged commit | `7a5094e01e527c64a2cf75bdbbcdd4f3026a7622` |
| Deployed main commit | `7a5094e01e527c64a2cf75bdbbcdd4f3026a7622` |
| Gateway image | `infera-gateway:latest` |
| Gateway image ID | `sha256:cbaeac2e1cf5c1c9f2991cd34dbf5f06d84445c453eb2d3b82ad8f8a72469d0d` |

Production gateway was redeployed from latest `main`. Public `/health`, dashboard `/api/health`, and internal gateway `/metrics` responded after redeploy.

Internal gateway `/metrics` exposed the expected route metric families after redeploy:

- `infera_route_decisions_total`
- `infera_route_candidates_evaluated`

## Worker Attempt

Exactly one RunPod worker instance was provisioned for this verification.

| Field | Value |
| --- | --- |
| Provider | RunPod |
| Provider instance ID | `2ticqy47pk6zn3` |
| GPU | `A100_80GB` |
| Hourly cost | `$1.19/hr` |
| Model | `Qwen/Qwen2.5-7B-Instruct` |

The instance reached provider status `running`, but it did not register with the gateway. The gateway remained `status=degraded`, `workers=0`, and `healthy_workers=0`. The managed instance record did not expose public IP, HTTP port, SSH host, or SSH port while it was running. The RunPod proxy health endpoint did not respond during the verification window.

The same instance was stopped, started once more, and observed again. It still did not register with the gateway, so it was stopped to avoid leaving paid capacity running.

Final instance status after cleanup:

| Field | Value |
| --- | --- |
| Provider instance ID | `2ticqy47pk6zn3` |
| Status | `stopped` |

## Header Verification

Because no healthy worker registered, successful route metadata could not be verified. The deployed gateway failure path was verified safely with authenticated no-worker chat requests.

| Check | Result |
| --- | --- |
| Non-streaming request without `X-Infera-Debug-Route` | `X-Infera-Route-Decision` absent |
| Non-streaming request with `X-Infera-Debug-Route: true` | `X-Infera-Route-Decision` present |
| Decoded failure metadata fields | `request_id`, `model`, `reason`, `candidates_evaluated` |
| Decoded model | `Qwen/Qwen2.5-7B-Instruct` |
| Decoded candidates evaluated | `0` |
| Forbidden content found in decoded metadata | no |

The decoded failure metadata was not recorded in full because it includes request-level internal identifiers.

## Metrics Evidence

After the no-worker header checks, internal gateway metrics showed failure route decisions and zero successful candidate observations:

| Metric | Value |
| --- | ---: |
| `infera_route_decisions_total{result="failure",strategy="unknown"}` | `2` |
| `infera_route_candidates_evaluated_count` | `0` |
| `infera_route_candidates_evaluated_sum` | `0` |

## Security Exclusions Verified

The decoded route metadata did not contain:

- prompt text
- `messages`
- API key material
- authorization header text
- worker token text
- provider credential text
- raw response body
- raw logs

This report does not include API keys, authorization headers, raw prompts, raw decoded metadata, raw JSON responses, provider credentials, or secrets.

## Infera-Bench Capture

`infera-bench --capture-route-decision` was not run in production because the worker never registered and production had zero healthy workers. Running the benchmark in that state would only measure the known no-worker failure mode, not successful route metadata capture.

No raw benchmark result files from this verification were committed.

## Final Production State

After stopping the failed verification worker, production health was:

| Field | Value |
| --- | --- |
| `status` | `degraded` |
| `workers` | `0` |
| `healthy_workers` | `0` |

The provider API continued to report historical active instance counts that include stopped pods, consistent with the existing provider/gateway instance-count mismatch.

## Limitations

- Successful route metadata fields such as `strategy`, `selected_worker`, `selected_provider`, `worker_queue_depth`, `worker_active_requests`, `worker_load`, and `decision_timestamp` were not verified in production because no worker registered.
- Streaming route metadata was not verified in production because streaming inference requires a healthy worker.
- `infera-bench --capture-route-decision` was not verified in production for the same reason.

## Next Steps

1. Investigate why RunPod instance `2ticqy47pk6zn3` reached `running` but did not expose worker connectivity or register with the gateway.
2. Add a focused worker startup failure issue if this failure recurs.
3. Re-run INF-32 production verification with exactly one healthy worker.
4. Only after successful verification, move INF-32 to Done.
