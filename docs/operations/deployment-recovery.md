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
- Every frontend candidate, rollback, or configuration restore must run
  `scripts/frontend-compose-action.sh` with the recorded full source commit and immutable frontend
  repo digest. The guard obtains Compose from that exact Git object, proves the rendered frontend
  has no `build` field and resolves to the requested digest, verifies the digest's OCI revision
  label equals the requested source, fixes the action scope to `frontend` with `--no-build
  --no-deps`, and verifies the recreated container still uses that digest, image ID, and revision
  before reporting success. Never trust an arbitrary on-host Compose file, including
  `/opt/infera/docker-compose.prod.yml`, as an action source, even when the checkout is clean.
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
- Prometheus SLO rules are not part of automatic release rollback. While ingress is drained and
  workers are zero, use only `scripts/prometheus-safe-reload.sh`; it proves the exact reviewed
  image, mounted hashes, lifecycle capability, targets, groups, rules, and alert continuity before
  and after a lifecycle reload. It never restarts Prometheus or restores ingress. A semantic
  failure retains the reviewed pre-SLO rules on disk and requires an exact-checkout reconciliation
  before retry. See `deploy/observability/README.md` and
  `deploy/observability/RUNBOOKS.md`.
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

Build gateway and vLLM worker images from the exact candidate commit through
`docs/releases/REPRODUCIBLE_RELEASE_BUILDS.md`. Confirm the builder-reported
`IMAGE_SOURCE_REVISION` equals that commit, push only through an approved
release workflow, and replace candidate tags with registry repo digests in the
manifest. Production Compose has no gateway build fallback; rollback pulls the
recorded last-known-good digests and must never rebuild a mutable checkout.

Copy `deploy/releases/release.manifest.example` to an incident/release workspace outside the Git
checkout. Use exact repo digests built from the same reviewed commit. The manifest must not
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
secret-manager version IDs. For a frontend-bearing release, also record its full source revision and
immutable frontend repo digest. Store the bundle in the restricted operations vault and record its
SHA-256 checksum. Do not export secret values into the bundle. To drill configuration restore, copy
the bundle to a clean operator host, verify its checksum, select the recorded secret versions, run
`./scripts/validate-prod-env.sh`, then run:

```bash
./scripts/frontend-compose-action.sh restore \
  --source-revision "${RECORDED_FRONTEND_SOURCE_REVISION}" \
  --image "${RECORDED_FRONTEND_IMAGE_DIGEST}"
```

The drill passes only when validation and the guarded exact-source restore succeed without
substituting defaults or printing secret values. After recovery is stable, reconcile the operator
checkout through the normal reviewed update procedure. Do not use checkout reconciliation in place
of the exact-source action.

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
export INFERA_RECOVERY_WORKER_MAX_COST_HOUR=<reviewed-positive-usd-per-hour-cap>
export INFERA_RECOVERY_REGISTRATION_ATTEMPT_SECONDS=180
export INFERA_RECOVERY_POST_201_CLEANUP_SECONDS=60
export INFERA_RECOVERY_SMOKE_TIMEOUT_SECONDS=60
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

Recovery verification gives each authenticated chat smoke request 60 seconds by default so a newly
registered worker can complete its first cold inference. Health and authenticated model-list
requests retain the ordinary 10-second smoke timeout. Override the chat timeout only with
`INFERA_RECOVERY_SMOKE_TIMEOUT_SECONDS`; values must be canonical decimal integers from 1 through
120, without signs, whitespace, or leading zeros. The coordinator's absolute deadline still bounds
the complete verifier process, so a slow or hung inference continues to fail into rollback rather
than extending the recovery window.

The RunPod deployment adapter requires an explicit reviewed `INFERA_RECOVERY_WORKER_MODEL`; it does
not select a model implicitly. It also requires
`INFERA_RECOVERY_WORKER_MAX_COST_HOUR` as a positive finite decimal below the gateway provider's
existing supported hourly-price bound. Missing, zero, signed, exponent-form, non-finite, malformed,
or out-of-range values fail recovery preflight, before traffic or provider mutation. The canonical
numeric value is serialized as `max_cost_hour` on every provisioning request, including every
capacity-only GPU fallback attempt. The adapter defaults to one `RTX_4090` vLLM worker. Set the ordered,
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
pod is removed, and a second query proves zero matching pods. An attached runtime is never eligible
for GPU fallback because it may indicate a model, credential, or network failure rather than
placement capacity. If it has not registered by the end of the first GPU's reviewed registration
slice, the adapter keeps waiting with the remaining candidate budget while preserving the configured
post-create cleanup slice. It becomes terminal only if that extended wait also expires. The final GPU
receives the remaining registration budget while preserving the same cleanup slice. While Caddy
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

## Provider-credential encryption-key recovery

PostgreSQL backup and PITR preserve encrypted provider and worker credentials, but they do not
preserve `INFERA_PROVIDER_CREDENTIAL_ENCRYPTION_KEY`. A restored database is unusable if the gateway
does not receive the matching key. The independent recovery copy is operator-managed in AWS Secrets
Manager; the application does not read AWS automatically.

The approved recovery secret must retain two distinct version IDs for the same approved key
revision. After the establishment drill, `AWSCURRENT` identifies the first version and
`AWSPREVIOUS` identifies the second. Both explicit-version reads must compare equal to the existing
production value. Do not infer a value from a label alone, and never replace the production runtime
key merely because an AWS request succeeded.

### Prerequisites and authorization

The incident commander must approve the database restore point, the recorded secret version, and
the production environment change. The database owner restores PITR to a new isolated database.
The platform operator works from the designated production host as root with AWS CLI v2, TLS
certificate validation enabled, and short-lived credentials issued in the configured AWS Region.
Set these resource identifiers from the restricted incident inventory, outside the Git checkout:

- `AWS_REGION`
- `INFERA_PROVIDER_KEY_SECRET_ID`
- `INFERA_PROVIDER_KEY_CURRENT_VERSION_ID`
- `INFERA_PROVIDER_KEY_PREVIOUS_VERSION_ID`
- `INFERA_RUNTIME_ENV_FILE`

Do not put resolved secret IDs, account identifiers, ARNs, principals, host addresses, version IDs,
or credentials in this repository, tickets, evidence, or terminal transcripts. Refuse an unset or
unexpected Region or resource identifier; do not discover a replacement by listing account-wide
secrets.

Use separate short-lived permissions:

- A creator, needed only when establishing the recovery secret, has
  `secretsmanager:CreateSecret` for the approved name pattern and Region.
- The exact-secret operator has only `secretsmanager:PutSecretValue`,
  `secretsmanager:DescribeSecret`, `secretsmanager:ListSecretVersionIds`,
  `secretsmanager:GetSecretValue`, and `secretsmanager:UpdateSecretVersionStage`.
- A separate auditor may have regional `cloudtrail:LookupEvents`.

Do not grant `SecretsManagerReadWrite`, administrator access, deletion, rotation, replication,
resource-policy, or account-wide listing permissions. Use the AWS-managed `aws/secretsmanager` KMS
key for same-account recovery. It needs no explicit `kms:*` permission. A customer-managed KMS key
is a separate recovery dependency and change requiring its own approval, key policy, runbook, and
drill.

### Root-only explicit-version retrieval

Start a fresh, non-recorded root shell on the production host. The following pattern fails closed,
keeps the value out of arguments and stdout, and emits only a boolean. Do not run it with shell or
AWS debug tracing, through a task runner that records commands, or from a developer machine.

```bash
set -euo pipefail
set +x
set +o history
HISTFILE=/dev/null
umask 077
export AWS_PAGER=''
export AWS_CLI_AUTO_PROMPT=off

: "${AWS_REGION:?approved AWS Region is required}"
: "${INFERA_PROVIDER_KEY_SECRET_ID:?approved secret ID is required}"
: "${INFERA_PROVIDER_KEY_CURRENT_VERSION_ID:?explicit version ID is required}"
: "${INFERA_RUNTIME_ENV_FILE:?root-only runtime environment path is required}"

test "${EUID}" -eq 0
test -f "${INFERA_RUNTIME_ENV_FILE}"
test ! -L "${INFERA_RUNTIME_ENV_FILE}"
test "$(stat -c '%u' "${INFERA_RUNTIME_ENV_FILE}")" = 0
test "$(stat -c '%a' "${INFERA_RUNTIME_ENV_FILE}")" = 600

recovered_key=
runtime_key=
cleanup_provider_key_memory() {
  unset recovered_key runtime_key
}
exit_provider_key_recovery() {
  cleanup_provider_key_memory
  exit 1
}
trap cleanup_provider_key_memory EXIT
trap exit_provider_key_recovery HUP INT TERM

recovered_key="$(
  aws secretsmanager get-secret-value \
    --region "${AWS_REGION}" \
    --secret-id "${INFERA_PROVIDER_KEY_SECRET_ID}" \
    --version-id "${INFERA_PROVIDER_KEY_CURRENT_VERSION_ID}" \
    --version-stage AWSCURRENT \
    --query SecretString \
    --output text 2>/dev/null
)" || {
  printf '%s\n' 'provider_key_retrieved=false'
  exit 1
}
test -n "${recovered_key}"

IFS= read -r -d '' runtime_key < <(
  set +x
  set +a
  . "${INFERA_RUNTIME_ENV_FILE}"
  printf '%s\0' "${INFERA_PROVIDER_CREDENTIAL_ENCRYPTION_KEY-}"
)

if [[ -n "${runtime_key}" && "${recovered_key}" == "${runtime_key}" ]]; then
  printf '%s\n' 'provider_key_matches_runtime=true'
else
  printf '%s\n' 'provider_key_matches_runtime=false'
  unset recovered_key runtime_key
  exit 1
fi
unset runtime_key
```

The value may exist only in root shell memory, an anonymous pipe, AWS CLI memory, TLS, Secrets
Manager, and the approved runtime secret-injection path. Never print, encode, hash, checksum, copy,
or paste it. Never put it in a command argument, history, log, evidence file, clipboard, temporary
file, repository, task output, or developer machine. Suppress raw AWS errors because they may expose
resource or principal metadata; record only the operation name and a success boolean.

Recovery is operator-mediated. After the equality gate passes, use the production platform's
separately approved root-only environment update mechanism to set the existing
`INFERA_PROVIDER_CREDENTIAL_ENCRYPTION_KEY` runtime variable from `recovered_key`. This runbook does
not authorize a new file, a new application AWS integration, or automatic synchronization from
Secrets Manager. Preserve the existing root-only runtime environment permissions and unset
`recovered_key` immediately after injection.

Only after the approved injection reports success in the same shell, explicitly clear both key
variables and remove the traps:

```bash
cleanup_provider_key_memory
trap - EXIT HUP INT TERM
```

### Safe PITR ordering and validation

1. Drain customer traffic and stop all gateways that could write to the control-state database.
2. Restore PostgreSQL PITR to a new database. Never overwrite the active database in place.
3. Select the explicit key version recorded for that restore point. Retrieve it on the production
   host and pass the boolean equality gate above before changing any runtime environment.
4. Inject the recovered key through the existing root-only runtime mechanism, then start exactly
   one isolated gateway against the restored DSN.
5. Require gateway startup, health, and an authorized provider-credential decryption smoke check to
   pass. Evidence may contain only booleans and safe timing/status fields; never provider
   credentials, tenant data, request payloads, DSNs, or raw errors.
6. Only after database and key validation passes, atomically rotate all gateways to the restored
   DSN and matching key, run release verification, and restore traffic.

If the explicit key does not validate the restored ciphertext, stop with traffic drained. Do not
try versions against the live database, rotate the encryption key, delete encrypted records,
disable provider authentication, or fall back to plaintext credentials. Page the incident
commander, database owner, and security owner.

### Version-label rollback

First retrieve both candidate versions with their explicit version IDs and expected stages, compare
each in memory to the approved runtime value, and emit only:

```text
current_equals_runtime=true|false
previous_equals_runtime=true|false
explicit_versions_equal=true|false
```

Stop unless all required comparisons are true. To roll `AWSCURRENT` back to the previously recorded
version, keep all identifiers in the non-recorded root shell and suppress command output:

```bash
: "${INFERA_PROVIDER_KEY_CURRENT_VERSION_ID:?current version ID is required}"
: "${INFERA_PROVIDER_KEY_PREVIOUS_VERSION_ID:?previous version ID is required}"
test "${INFERA_PROVIDER_KEY_CURRENT_VERSION_ID}" != \
  "${INFERA_PROVIDER_KEY_PREVIOUS_VERSION_ID}"

aws secretsmanager update-secret-version-stage \
  --region "${AWS_REGION}" \
  --secret-id "${INFERA_PROVIDER_KEY_SECRET_ID}" \
  --version-stage AWSCURRENT \
  --move-to-version-id "${INFERA_PROVIDER_KEY_PREVIOUS_VERSION_ID}" \
  --remove-from-version-id "${INFERA_PROVIDER_KEY_CURRENT_VERSION_ID}" \
  >/dev/null 2>&1 || {
    printf '%s\n' 'provider_key_label_rollback=false'
    exit 1
  }
```

Poll bounded explicit-version reads until the moved-to version resolves with `AWSCURRENT` and the
moved-from version resolves with `AWSPREVIOUS`. Compare both payloads in memory again and emit only
`rollback_labels_ok`, `current_equals_runtime`, and `previous_equals_runtime` booleans. Label
convergence is not proof of value equality. Do not alter the production runtime or database during a
label-only drill.

### Cleanup, audit, and escalation

Unset every value and credential variable, destroy root-only temporary AWS configuration, terminate
the root shell, and prove that no temporary IAM user, access key, role, or credential file remains.
Retain the recovery secret and its two labeled versions. Do not delete a version during incident
cleanup.

Verify sanitized CloudTrail evidence for `CreateSecret`, `PutSecretValue`, `GetSecretValue`, and
`UpdateSecretVersionStage`. CloudTrail Event History is regional, retains only 90 days, and is not a
durable incident archive. If the security or compliance retention requirement exceeds 90 days,
open a follow-up to create and validate a durable trail or event data store, including its
S3/IAM/KMS, retention, access-control, and cost boundaries.

The recovery passes only when explicit-version equality, label placement, isolated gateway startup,
provider-credential decryption, cleanup, and sanitized audit checks all pass. On any false or
unknown result, keep traffic drained, preserve the isolated database and secret versions, avoid raw
evidence export, and escalate to the incident commander, database owner, platform owner, and
security owner.

## Pilot-readiness evidence

Run the deterministic failure-injection suite on the reviewed commit:

```bash
bash ./scripts/test-release-recovery.sh
bash ./scripts/test-production-recovery-adapters.sh
bash ./scripts/test-frontend-compose-action.sh
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
