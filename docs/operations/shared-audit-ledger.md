# Shared audit and quota ledger operations

PostgreSQL is the production multi-replica source of truth for inference audit events and quota
reservations. SQLite is supported only when `INFERA_GATEWAY_REPLICAS=1`; a shared filesystem does
not make SQLite safe for active-active gateways.

## Invariants

- Every replica uses the same PostgreSQL database and schema.
- A quota decision locks the execution ID and workspace billing period in one transaction, then
  evaluates committed audit usage plus non-expired reservations before inserting a reservation.
- `(workspace_id, request_id)` preserves the first terminal audit event. Replays must match it.
- Writing a terminal audit event and releasing its reservation happen in one transaction.
- PostgreSQL unavailability fails quota-controlled requests closed and production startup fails if
  the required ledger cannot be opened.

## SQLite to PostgreSQL cutover

This is a stop-the-world data cutover. Mixed SQLite/PostgreSQL writers are intentionally not
supported because they have no common quota serialization point.

1. Provision PostgreSQL with encrypted connections, automated backups, point-in-time recovery,
   storage alerts, and enough connections for `INFERA_AUDIT_LEDGER_MAX_OPEN_CONNS × gateway replicas`
   plus operational headroom. Tune max-open, max-idle, and connection lifetime through the
   corresponding `INFERA_AUDIT_LEDGER_*` settings rather than rebuilding the gateway.
2. Back up `data/audit.db` and its `-wal`/`-shm` files after stopping every gateway. Keep the
   original files read-only until the rollback window closes.
3. Run the idempotent migration from the repository root:

   ```bash
   cd go
   INFERA_AUDIT_LEDGER_DSN='postgres://...?...sslmode=require' \
     go run ./cmd/audit-ledger-migrate -sqlite ../data/audit.db
   ```

   The tool migrates immutable audit history and verifies conflicting first writes. It does not
   copy transient reservations, so all gateways must remain drained while it runs.
4. Set `INFERA_AUDIT_LEDGER_BACKEND=postgres`, the same secret
   `INFERA_AUDIT_LEDGER_DSN` on every replica, and the intended `INFERA_GATEWAY_REPLICAS` value.
   Run `./scripts/validate-prod-env.sh` before starting any gateway.
5. Start one gateway, verify health and usage queries, then start the remaining replicas. Exercise
   a one-slot test workspace and confirm exactly one concurrent request is admitted.

## Backup and restore

Use managed snapshots/PITR or `pg_dump --format=custom` against a transactionally consistent
snapshot. Restore into a new database, run the gateway once against it to apply compatible schema
migrations, validate per-workspace counts and token sums, then atomically rotate every replica to
the restored DSN. Never restore only `quota_reservations` or only `inference_audit`; they form one
accounting state.

Use `scripts/audit-ledger-recovery-drill.sh` and the RPO/RTO and sanitized-evidence procedure in
`docs/operations/deployment-recovery.md` for the reproducible restore drill.

## Rollback

Application rollback is safe only to a release that supports PostgreSQL writer protocol `2`.
Rollback all replicas together and leave them on the same PostgreSQL ledger. Returning to SQLite
requires a full outage and a separately reviewed PostgreSQL-to-SQLite export; this release does not
provide that lossy reverse migration. Do not re-enable the pre-cutover SQLite copy after PostgreSQL
has accepted writes.

## Mixed-version behavior

The ledger records writer protocol `2` and rejects a gateway if the database advertises a different
protocol. Protocol `2` scopes reservation identities by workspace; protocol `1` gateways must be
fully drained before the schema upgrade because they assume globally unique execution IDs.
Pre-INF-42 gateways never connect to this ledger and therefore must not overlap: drain and stop them
before migration. Schema changes that alter reservation or idempotency semantics must introduce a
new writer protocol and use a coordinated, non-mixed rollout.
