# Deployment rollback and failure recovery

This runbook is the production contract for coordinated gateway/worker rollout, rollback, and
audit-ledger recovery. The incident commander owns the decision to deploy or roll back. The
platform operator runs the commands. The on-call application engineer diagnoses gateway/worker
failures, and the database owner approves ledger restore or point-in-time recovery (PITR).

## Safety invariants

- A release set is one immutable manifest containing pinned gateway and worker images, one release
  ID, one worker protocol, and one audit-ledger writer protocol. Never mix fields from two manifests.
- Stop/drain old workers before changing the gateway. Workers register only when their release and
  control-plane protocol match the gateway; a mismatch is not an acceptable rolling state.
- The active ledger writer protocol must match both candidate and rollback manifests before any
  mutation. Protocol migration is a separately reviewed, stop-the-world operation.
- A candidate is last-known-good only after automated verification passes. Authentication, tenant
  isolation, quota enforcement, and the shared ledger must never be bypassed to make a rollout pass.
- If candidate verification and rollback verification both fail, keep traffic drained and escalate.
- Ingress drain is a required state transition, not a log message: no release mutation may start
  until the drain adapter succeeds, and traffic returns only after release verification succeeds.

## Targets

| Data or service | RPO | RTO | Mechanism |
| --- | --- | --- | --- |
| Gateway/worker release | 0 configuration revisions | 15 minutes | Immutable manifests and coordinated rollback |
| PostgreSQL audit/quota ledger | Managed backup/PITR window, maximum 5 minutes | 30 minutes | PITR or custom-format dump restored to a new database |
| Required configuration/secrets | 0 approved revisions | 15 minutes | Versioned non-secret manifest plus secret-manager version rollback |

If the configured database or secret-manager service cannot meet these targets, external pilot
readiness is blocked. Measure actual RPO/RTO during every drill and record a follow-up when a target
is missed.

## Prepare a release set

Copy `deploy/releases/release.manifest.example` to an incident/release workspace outside the Git
checkout. Use exact image tags or digests built from the same reviewed commit. The manifest must not
contain DSNs, tokens, API keys, tenant identifiers, or credentials.

Keep the current proven manifest at `.infera-recovery/last-known-good.manifest`. Confirm both files:

```bash
diff -u .infera-recovery/last-known-good.manifest /secure/release/candidate.manifest
./scripts/validate-prod-env.sh
```

Back up required configuration as a release bundle before rollout: the candidate and last-known-good
manifests, the production `.env` template containing secret *names* only, and the selected immutable
secret-manager version IDs. Store the bundle in the restricted operations vault and record its
SHA-256 checksum. Do not export secret values into the bundle. To drill configuration restore, copy
the bundle to a clean operator host, verify its checksum, select the recorded secret versions, run
`./scripts/validate-prod-env.sh`, and render `docker compose -f docker-compose.prod.yml config
--quiet`. The drill passes only when both commands succeed without substituting defaults or printing
secret values.

Provider-specific executables receive one manifest path and must be idempotent:

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
export INFERA_STOP_WORKERS_EXECUTABLE=/opt/infera/bin/stop-release-workers
export INFERA_DEPLOY_WORKERS_EXECUTABLE=/opt/infera/bin/deploy-release-workers
export INFERA_DRAIN_TRAFFIC_EXECUTABLE=/opt/infera/bin/drain-release-traffic
export INFERA_RESTORE_TRAFFIC_EXECUTABLE=/opt/infera/bin/restore-release-traffic
export INFERA_ACTIVE_AUDIT_LEDGER_WRITER_PROTOCOL=2
export INFERA_SMOKE_API_KEY="$(secret-tool lookup service infera-smoke)"
export INFERA_SMOKE_MODEL=Qwen/Qwen2.5-7B-Instruct
./scripts/release-recovery.sh deploy \
  /secure/release/candidate.manifest \
  .infera-recovery/last-known-good.manifest
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
bash -n ./scripts/release-recovery.sh ./scripts/compose-release-driver.sh \
  ./scripts/verify-release-manifest.sh ./scripts/audit-ledger-recovery-drill.sh
```

For a production drill, attach the sanitized recovery and ledger evidence logs, start/end times,
release IDs, image digests, actual RPO/RTO, incident owner, and final decision. Review every file for
secrets and tenant data before attaching it to a pilot-readiness review.

The checked-in Compose driver intentionally delegates provider worker lifecycle and ingress control
to environment-owned executable adapters. INF-46 remains **In Progress** until a real production
drill exercises those exact adapters and demonstrates the stated RPO/RTO; deterministic CI coverage
alone is not pilot-readiness evidence.
