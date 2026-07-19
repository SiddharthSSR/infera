# Deployment rollback and failure recovery

This runbook is the production contract for coordinated gateway/worker rollout, rollback, and
audit-ledger recovery. The incident commander owns the decision to deploy or roll back. The
platform operator runs the commands. The on-call application engineer diagnoses gateway/worker
failures, and the database owner approves ledger restore or point-in-time recovery (PITR).

## Safety invariants

- A release set is one immutable manifest containing pinned gateway and worker images, one release
  ID, one worker protocol, and one audit-ledger writer protocol. Never mix fields from two manifests.
- The current immutable release set does not include the Compose frontend image. A coordinated
  gateway/worker rollout therefore does not promote or roll back frontend source. For a release
  containing frontend changes, use the separately recorded frontend canary and promotion procedure
  in `docs/releases/FRONTEND_RELEASE_BASELINE.md`; do not infer frontend identity from the gateway
  release ID.
- Stop/drain old workers before changing the gateway. Workers register only when their release and
  control-plane protocol match the gateway; a mismatch is not an acceptable rolling state.
- The active ledger writer protocol must match both candidate and rollback manifests before any
  mutation. Protocol migration is a separately reviewed, stop-the-world operation.
- Every production gateway replica must use the same PostgreSQL control-state database and provider
  credential encryption key. An application rollback is allowed only when the target gateway is
  compatible with the active control-state schema.
- A candidate is last-known-good only after automated verification passes. Authentication, tenant
  isolation, quota enforcement, and the shared ledger must never be bypassed to make a rollout pass.
- If candidate verification and rollback verification both fail, keep traffic drained and escalate.
- Ingress drain is a required state transition, not a log message: no release mutation may start
  until the drain adapter succeeds, and traffic returns only after release verification succeeds.
- While ingress is drained, the only public gateway exception is worker registration and heartbeat
  on their two exact paths. Those requests must carry a worker-token header and still pass the
  gateway's credential authentication; health, inference, and every other customer route remain 503.
- Run exactly one recovery controller for a production stack. Production mode requires an explicit
  absolute `INFERA_RECOVERY_STATE_DIR` and a controller scope. Use `shared-filesystem` only when
  every operator host mounts the same state path; otherwise use `designated-single-controller` and
  permit recovery only on that host. The controller holds an atomic state-directory lock for the
  complete rollout and rollback. It never steals or auto-expires an existing lock; after a host
  crash, an incident commander must verify the old process is gone and determine the actual
  ingress/provider state before manually removing the stale lock directory.

## Targets

| Data or service | RPO | RTO | Mechanism |
| --- | --- | --- | --- |
| Gateway/worker release | 0 configuration revisions | 15 minutes | Immutable manifests and coordinated rollback |
| PostgreSQL audit/quota ledger | Managed backup/PITR window, maximum 5 minutes | 30 minutes | PITR or custom-format dump restored to a new database |
| PostgreSQL provider/worker control state | Managed backup/PITR window, maximum 5 minutes | 30 minutes | PITR or custom-format dump restored to a new database |
| Required configuration/secrets | 0 approved revisions | 15 minutes | Versioned non-secret manifest plus secret-manager version rollback |

If the configured database or secret-manager service cannot meet these targets, external pilot
readiness is blocked. Measure actual RPO/RTO during every drill and record a follow-up when a target
is missed.

## Prepare a release set

Copy `deploy/releases/release.manifest.example` to an incident/release workspace outside the Git
checkout. Use exact image tags or digests built from the same reviewed commit. The manifest must not
contain DSNs, tokens, API keys, tenant identifiers, or credentials.

`INFERA_RECOVERY_API_PROTOCOL_VERSION` is a required compatibility boundary between the recovery
adapters and the gateway binary. Before making a release eligible as last-known-good, verify that
every gateway replica reports the same value from `/health`. Older manifests without this field are
intentionally rejected; bootstrap them by deploying and verifying a protocol-bearing gateway with
the previously reviewed orchestration, then record that release as the new last-known-good target.

Keep the current proven manifest at `.infera-recovery/last-known-good.manifest`. Confirm both files:

```bash
diff -u .infera-recovery/last-known-good.manifest /secure/release/candidate.manifest
INFERA_ACTIVE_AUDIT_LEDGER_WRITER_PROTOCOL=2 \
  ./scripts/check-last-known-good.sh .infera-recovery/last-known-good.manifest
./scripts/validate-prod-env.sh
```

Run the last-known-good check after every production release and before starting a recovery drill.
It compares the recorded release and protocol identity with every live gateway replica, verifies the
running immutable gateway image, and verifies the selected immutable worker image configured in each
gateway. A mismatch is release drift: do not copy the candidate over the manifest manually. Promote
the candidate only through the coordinated recovery command below after its full verification gate
passes.

Back up required configuration as a release bundle before rollout: the candidate and last-known-good
manifests, the production `.env` template containing secret *names* only, and the selected immutable
secret-manager version IDs. Store the bundle in the restricted operations vault and record its
SHA-256 checksum. Do not export secret values into the bundle. To drill configuration restore, copy
the bundle to a clean operator host, verify its checksum, select the recorded secret versions, run
`./scripts/validate-prod-env.sh`, and render `docker compose -f docker-compose.prod.yml config
--quiet`. The drill passes only when both commands succeed without substituting defaults or printing
secret values.

Provider-specific executables receive one manifest path and must be idempotent. The checked-in
RunPod and Caddy adapters are suitable for the production Compose topology:

- `INFERA_STOP_WORKERS_EXECUTABLE`: drain requests, then stop every worker in that release set.
- `INFERA_DEPLOY_WORKERS_EXECUTABLE`: create/reprovision workers with the manifest's pinned image,
  release ID, worker protocol, gateway address, and existing shared-token secret reference. It must
  return nonzero until expected workers register and become healthy.
- `INFERA_DRAIN_TRAFFIC_EXECUTABLE`: remove public ingress from the gateway and verify that new
  requests receive the approved maintenance response before returning success.
- `INFERA_RESTORE_TRAFFIC_EXECUTABLE`: restore ingress to the manifest's verified gateway and prove
  public health reaches that release before returning success.

These executables must log resource IDs and safe lifecycle states only. They must never print the
worker token, provider credentials, authorization headers, DSNs, or raw environment dumps.

## Deploy with automatic operational rollback

Export the existing production secrets without printing them, then run:

```bash
export INFERA_RECOVERY_DRIVER="$PWD/scripts/compose-release-driver.sh"
export INFERA_RECOVERY_VERIFIER="$PWD/scripts/verify-release-manifest.sh"
export INFERA_STOP_WORKERS_EXECUTABLE="$PWD/scripts/runpod-stop-workers.sh"
export INFERA_DEPLOY_WORKERS_EXECUTABLE="$PWD/scripts/runpod-deploy-workers.sh"
export INFERA_DRAIN_TRAFFIC_EXECUTABLE="$PWD/scripts/caddy-drain-traffic.sh"
export INFERA_RESTORE_TRAFFIC_EXECUTABLE="$PWD/scripts/caddy-restore-traffic.sh"
export INFERA_ACTIVE_AUDIT_LEDGER_WRITER_PROTOCOL=2
export INFERA_EXPECT_TRAFFIC_DRAINED=1
export INFERA_BASE_URL=https://inferai.co.in
export INFERA_DASHBOARD_URL=https://dashboard.inferai.co.in
export INFERA_SMOKE_API_KEY="$(secret-tool lookup service infera-smoke)"
export INFERA_SMOKE_MODEL=Qwen/Qwen2.5-7B-Instruct
export INFERA_RECOVERY_WORKER_MODEL=Qwen/Qwen2.5-7B-Instruct
export INFERA_RECOVERY_WORKER_GPU_TYPES=RTX_4090,A100_80GB,H100
export INFERA_RECOVERY_REGISTRATION_ATTEMPT_SECONDS=180
export INFERA_RECOVERY_POST_201_CLEANUP_SECONDS=60
export INFERA_RECOVERY_STATE_DIR=/opt/infera/.infera-recovery
export INFERA_RECOVERY_CONTROLLER_SCOPE=designated-single-controller
./scripts/release-recovery.sh deploy \
  /secure/release/candidate.manifest \
  /opt/infera/.infera-recovery/last-known-good.manifest
```

The command performs these gates in order:

1. Validate the candidate and current production configuration without displaying values.
2. Drain public ingress and verify the maintenance response.
3. Stop/drain last-known-good workers.
4. Deploy the candidate gateway and require its container health check to pass.
5. Deploy candidate workers; the provider adapter requires successful registration and health.
6. Run release verification. `/health` must match release/protocol, worker discovery must contain at
   least one target, and authenticated non-streaming/streaming smoke requests must pass. A
   `quota_unavailable` response fails this gate rather than weakening quota enforcement.
7. Atomically replace `.infera-recovery/last-known-good.manifest`, then restore public ingress. If
   either state promotion or ingress restore fails, the command fails with traffic still drained;
   promotion failure also restores the verified prior release set.

Release verification defaults to `INFERA_RELEASE_WORKER_MODE=serving` and rejects a reachable
gateway when `/health` reports `healthy_workers=0`. A deliberately scaled-to-zero release must set
`INFERA_RELEASE_WORKER_MODE=cost-saving`; that mode requires exactly zero healthy workers, skips
worker discovery and chat inference, but still verifies rollout identity, dashboard health,
authentication, and the model-list response contract. Any other mode or malformed worker count
fails closed. Never use cost-saving mode to bypass a failed worker rollout.

The RunPod deployment adapter requires an explicit reviewed `INFERA_RECOVERY_WORKER_MODEL`; it does
not select a model implicitly. It defaults to one `RTX_4090` vLLM worker. Set the ordered,
comma-separated `INFERA_RECOVERY_WORKER_GPU_TYPES` to at most five reviewed values from
`RTX_4090`, `RTX_4080`, `A100_40GB`, `A100_80GB`, `H100`, and `L40S`. The legacy singleton
`INFERA_RECOVERY_WORKER_GPU_TYPE` remains supported when the ordered variable is unset; setting both
is an error. `INFERA_RECOVERY_WORKER_ENGINE` selects the reviewed engine. The recovery driver pins
the selected engine-specific gateway image variable to the manifest's worker image for both rollout
and rollback, preventing stale `.env` values from mixing release sets. It reads the admin and RunPod
keys from the environment or `INFERA_ENV_FILE`, places bearer headers only in mode-0600 temporary
curl configuration files, and waits for the gateway-managed worker to register. Before provisioning
or stopping, it reconciles only pods whose name exactly matches `infera-release-<release ID>`; an
orphan from an interrupted attempt is terminated before a replacement is created. A non-final GPU
that has not attached a RunPod runtime within the
reviewed registration slice may fall back only after the gateway instance is deleted, the exact-name
pod is removed, and a second query proves zero matching pods. An attached runtime that fails gateway
registration remains terminal because it indicates a model, credential, or network failure rather
than placement capacity. The final GPU receives the remaining registration budget while preserving
the configured post-create cleanup slice. While Caddy
returns the maintenance 503, the verifier enumerates every configured container-private gateway
address and runs health, worker discovery, and authenticated inference checks against each replica.
The restore adapter then proves public `/health` reaches the expected release, worker protocol, and
recovery API protocol before it returns success. If that public validation fails, it immediately
reloads and verifies the maintenance configuration.

GPU fallback is deliberately narrow. Before every provisioning POST, the adapter proves that the
exact release-owned RunPod name has zero pods. It advances to the next reviewed GPU only when the
gateway returns HTTP 503 with `provider=runpod`, `provider_error_code=capacity_unavailable`, and
`retryable=true`, then removes and re-confirms zero exact-name pods. A transport error, malformed or
unknown response, any other status/code, or any HTTP 201 is terminal for that adapter invocation;
after a 201 it never sends another provisioning POST. Ambiguous outcomes trigger exact-name cleanup
within a bounded slice of the rollback reserve and then fail into coordinated rollback.

The coordinator uses one absolute deadline, defaulting to and capped at 900 seconds. It reserves 300
seconds for rollback by default, stops candidate work at the soft deadline, and terminates hung
driver/verifier process groups with the checked-in portable deadline wrapper. Worker provisioning
POSTs default to 45 seconds and a new GPU attempt is refused without the configured minimum
attempt-and-cleanup budget. Every evidence line begins with a UTC timestamp and then uses one of two
fixed record families. Coordinator records use these exact event-specific fields: `DRILL candidate
last_known_good ledger_protocol timeout_seconds rollback_reserve_seconds`; `START step`; `PASS step`;
`FAIL step [reason]`; `ROLLBACK from to trigger`; `FAIL_CLOSED release action`; `RECOVERED release
started_at`; `REJECTED release action`; and `PROMOTED release`. Here, `step` is the single positional
token following `START`, `PASS`, or `FAIL`; every other listed field is emitted as `key=value`.
Worker-adapter records use exactly `WORKER_RECOVERY event result gpu attempt reason release step`,
all as `key=value`. Allowed events are `candidate_selected`, `provision_response`, `reconcile`, and
`registration`; allowed results are `start`, `pass`, `fail`, `fallback`, and `terminal`; allowed
reasons are `none`, `capacity_unavailable`, `created`, `registered`, `deadline_exhausted`,
`invalid_response`, `unknown_failure`, `transport_failure`, `state_not_empty`, `cleanup_failed`, and
`registration_timeout`, and `runtime_attachment_timeout`. Raw provider/gateway responses,
credentials, DSNs, configured filesystem paths, and arbitrary child output are never copied into
the evidence file.

The maintenance configuration permits only `/api/workers/register` and
`/api/workers/heartbeat` through to the gateway when the request presents `X-Worker-Token` or a
Bearer credential. The gateway remains authoritative for validating per-instance or shared worker
credentials. This narrow control-plane exception lets replacement workers register while customer
health and inference stay fail-closed at 503; it is not permission to expose other `/api/*` routes.

The recovery adapter deploys the configured `INFERA_GATEWAY_REPLICAS` count and requires every
container to become healthy. Multiple replicas are safe only when all replicas use the same
`INFERA_CONTROL_STATE_DSN`, provider credential encryption key, and PostgreSQL audit ledger. Treat
gateway and worker images as a coordinated release: rollback both together, and do not roll back to
a gateway that cannot read the active control-state schema.

Any failure after orchestration starts stops candidate workers, redeploys the old gateway, deploys
old workers, and verifies the old release. A successful recovery still returns a nonzero command
status because the candidate was rejected. Inspect the newest sanitized file in
`recovery-evidence/` and open an incident.

## Decision points

- **Candidate preflight or gateway startup fails:** allow automatic rollback. Diagnose config,
  image startup, and ledger connectivity offline.
- **Workers do not register:** allow automatic rollback. Check provider network readiness, release
  ID, worker protocol, shared-token secret version, model load, then heartbeat state.
- **Zero healthy workers or smoke verification fails:** allow automatic rollback. Do not declare a
  gateway-only deployment healthy.
- **`quota_unavailable` or ledger startup failure:** allow automatic rollback only when the rollback
  manifest advertises the active writer protocol. Do not switch to local SQLite or disable quotas.
- **Rollback verification fails:** keep traffic drained, page the incident commander and database
  owner, and restore the ledger only if database evidence indicates corruption/unavailability.
- **Protocol mismatch is reported before preflight:** stop. Select a compatible manifest or execute
  a separately approved protocol migration; never override the guard.

## Audit/quota backup and restore drill

Restore into a new, empty database. Never overwrite the production database during a drill.

```bash
export INFERA_AUDIT_LEDGER_SOURCE_DSN="$(secret-tool lookup service infera-ledger-primary)"
export INFERA_AUDIT_LEDGER_RESTORE_DSN="$(secret-tool lookup service infera-ledger-drill)"
export INFERA_AUDIT_LEDGER_WRITER_PROTOCOL=2
./scripts/audit-ledger-recovery-drill.sh
```

Before any dump or restore, the script resolves each connection to PostgreSQL's physical system
identifier plus database OID and refuses equivalent targets. The recovery database role therefore
requires permission to execute `pg_control_system()`; inability to prove identity fails closed. The
target identity is checked again immediately before restore. The script then holds `SHARE` locks on all
accounting tables and exports one PostgreSQL snapshot. Both `pg_dump` and the source digest consume
that exact snapshot while the helper transaction remains live, blocking accounting writes. After
restoring the complete schema, it compares the writer protocol and deterministic MD5 content
digests of every JSONB-normalized metadata, audit, and reservation row in primary-key order. Raw
rows, tenant IDs, request IDs, and DSNs never enter evidence. After the drill, the database owner drops the isolated
restore database through the managed database console.

For an incident restore, prefer managed PITR to a new database at the latest safe point within the
five-minute RPO. Run the drill validation against it, start exactly one gateway using the restored
DSN, verify health and a one-slot quota test, then atomically rotate all gateways to that DSN. Do not
restore only one table, reuse a pre-cutover SQLite file, or overlap different writer protocols.

## Pilot-readiness evidence

Run the deterministic failure-injection suite on the reviewed commit:

```bash
bash ./scripts/test-release-recovery.sh
bash ./scripts/test-production-recovery-adapters.sh
bash -n ./scripts/release-recovery.sh ./scripts/compose-release-driver.sh \
  ./scripts/verify-release-manifest.sh ./scripts/audit-ledger-recovery-drill.sh \
  ./scripts/runpod-stop-workers.sh ./scripts/runpod-deploy-workers.sh \
  ./scripts/caddy-drain-traffic.sh ./scripts/caddy-restore-traffic.sh
```

For a production drill, attach the sanitized recovery and ledger evidence logs, start/end times,
release IDs, image digests, actual RPO/RTO, incident owner, and final decision. Review every file for
secrets and tenant data before attaching it to a pilot-readiness review.

INF-46 remains **In Progress** until a real production drill exercises the checked-in RunPod and
Caddy adapters and demonstrates the stated RPO/RTO; deterministic CI coverage alone is not
pilot-readiness evidence.
