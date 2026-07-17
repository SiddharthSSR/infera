package audit

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestPostgresCrossProcessQuotaAdmission(t *testing.T) {
	dsn := os.Getenv("INFERA_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("INFERA_TEST_POSTGRES_DSN is not configured")
	}
	if os.Getenv("INFERA_QUOTA_HELPER") == "1" {
		runQuotaHelper(dsn)
		return
	}

	testSuffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	workspaceID := "ws_cross_process_" + testSuffix
	applicationName := "quota_cross_process_" + testSuffix
	gateLockID := time.Now().UnixNano() & 0x3fffffffffffffff
	testDir := t.TempDir()
	startFile := filepath.Join(testDir, "start")
	readyFiles := make([]string, 2)
	commands := make([]*exec.Cmd, 2)
	stderr := make([]bytes.Buffer, 2)
	observer, err := NewPostgresStore(dsn)
	if err != nil {
		t.Fatalf("open serialization observer: %v", err)
	}
	defer observer.Close()
	gateConn, err := observer.db.Conn(context.Background())
	if err != nil {
		t.Fatalf("open admission gate connection: %v", err)
	}
	defer gateConn.Close()
	if _, err := gateConn.ExecContext(context.Background(), `SELECT pg_advisory_lock($1)`, gateLockID); err != nil {
		t.Fatalf("hold admission gate: %v", err)
	}
	gateHeld := true
	defer func() {
		if gateHeld {
			_, _ = gateConn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, gateLockID)
		}
	}()
	for i := range commands {
		readyFiles[i] = filepath.Join(testDir, fmt.Sprintf("ready_%d", i))
		commands[i] = exec.Command(os.Args[0], "-test.run=^TestPostgresCrossProcessQuotaAdmission$")
		commands[i].Stderr = &stderr[i]
		commands[i].Env = append(os.Environ(),
			"INFERA_QUOTA_HELPER=1",
			"INFERA_QUOTA_WORKSPACE="+workspaceID,
			"INFERA_QUOTA_APPLICATION_NAME="+applicationName,
			fmt.Sprintf("INFERA_QUOTA_EXECUTION=exec_%s_%d", testSuffix, i),
			"INFERA_QUOTA_START_FILE="+startFile,
			"INFERA_QUOTA_READY_FILE="+readyFiles[i],
			"INFERA_QUOTA_GATE_LOCK_ID="+strconv.FormatInt(gateLockID, 10),
		)
		if err := commands[i].Start(); err != nil {
			t.Fatalf("start helper %d: %v", i, err)
		}
	}
	if err := waitForFiles(5*time.Second, readyFiles...); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(startFile, []byte("start"), 0600); err != nil {
		t.Fatalf("release helpers: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		var waitingAdvisoryLocks int
		if err := observer.db.QueryRow(`
			SELECT COUNT(*)
			FROM pg_locks AS locks
			JOIN pg_stat_activity AS activity ON activity.pid = locks.pid
			WHERE activity.application_name = $1
			  AND locks.locktype = 'advisory'
			  AND NOT locks.granted`, applicationName).Scan(&waitingAdvisoryLocks); err != nil {
			t.Fatalf("observe period serialization: %v", err)
		}
		if waitingAdvisoryLocks == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("helpers did not reach the admission boundary: waiting=%d", waitingAdvisoryLocks)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := gateConn.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, gateLockID); err != nil {
		t.Fatalf("release admission gate: %v", err)
	}
	gateHeld = false

	var admitted, rejected int
	for i, command := range commands {
		err := command.Wait()
		switch {
		case err == nil:
			admitted++
		case exitCode(err) == 42:
			rejected++
		default:
			t.Fatalf("helper %d failed unexpectedly: %v: %s", i, err, stderr[i].String())
		}
	}
	if admitted != 1 || rejected != 1 {
		t.Fatalf("expected exactly one admission, got admitted=%d rejected=%d", admitted, rejected)
	}
}

func TestPostgresQuotaSnapshotCountsReservationDuringAuditTransition(t *testing.T) {
	dsn := os.Getenv("INFERA_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("INFERA_TEST_POSTGRES_DSN is not configured")
	}
	reserveStore, err := NewPostgresStore(dsn)
	if err != nil {
		t.Fatalf("NewPostgresStore reserve: %v", err)
	}
	defer reserveStore.Close()
	appendStore, err := NewPostgresStore(dsn)
	if err != nil {
		t.Fatalf("NewPostgresStore append: %v", err)
	}
	defer appendStore.Close()

	workspaceID := "ws_transition_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	periodStart := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	limit := int64(1)
	reservation := QuotaReservation{
		ExecutionID: "exec_transition", WorkspaceID: workspaceID,
		PeriodStart: periodStart, PeriodEnd: periodStart.AddDate(0, 1, 0),
		ReservedRequests: 1, MonthlyRequestLimit: &limit,
		ExpiresAt: time.Now().UTC().Add(time.Minute),
	}
	if err := appendStore.ReserveQuota(reservation); err != nil {
		t.Fatalf("seed transition reservation: %v", err)
	}

	lockBase := time.Now().UnixNano() & 0x3fffffffffffffff
	barrier := &quotaSnapshotBarrier{reachedLockID: lockBase, releaseLockID: lockBase + 1}
	reserveStore.quotaSnapshotBarrier = barrier
	ctx := context.Background()
	gateConn, err := appendStore.db.Conn(ctx)
	if err != nil {
		t.Fatalf("open gate connection: %v", err)
	}
	defer gateConn.Close()
	if _, err := gateConn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, barrier.releaseLockID); err != nil {
		t.Fatalf("hold snapshot gate: %v", err)
	}
	gateHeld := true
	defer func() {
		if gateHeld {
			_, _ = gateConn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, barrier.releaseLockID)
		}
	}()

	result := make(chan error, 1)
	go func() {
		candidate := reservation
		candidate.ExecutionID = "exec_candidate"
		result <- reserveStore.ReserveQuota(candidate)
	}()

	observerConn, err := appendStore.db.Conn(ctx)
	if err != nil {
		t.Fatalf("open observer connection: %v", err)
	}
	defer observerConn.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var acquired bool
		if err := observerConn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, barrier.reachedLockID).Scan(&acquired); err != nil {
			t.Fatalf("observe snapshot barrier: %v", err)
		}
		if !acquired {
			break
		}
		if _, err := observerConn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, barrier.reachedLockID); err != nil {
			t.Fatalf("release observer probe: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("quota snapshot did not reach transition barrier")
		}
		time.Sleep(5 * time.Millisecond)
	}

	if err := appendStore.AppendInference(InferenceAuditRecord{
		Timestamp: periodStart.Add(time.Hour), RequestID: reservation.ExecutionID,
		WorkspaceID: workspaceID, Model: "m1", Status: "success", TokenCount: 1,
	}); err != nil {
		t.Fatalf("commit reservation-to-audit transition: %v", err)
	}
	if _, err := gateConn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, barrier.releaseLockID); err != nil {
		t.Fatalf("release snapshot gate: %v", err)
	}
	gateHeld = false

	select {
	case err := <-result:
		if !errors.Is(err, ErrQuotaExceeded) {
			t.Fatalf("transition was counted as neither pending nor committed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("quota reservation remained blocked after releasing transition gate")
	}
}

func TestPostgresQuotaReservationsAreWorkspaceScoped(t *testing.T) {
	dsn := os.Getenv("INFERA_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("INFERA_TEST_POSTGRES_DSN is not configured")
	}
	store, err := NewPostgresStore(dsn)
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	defer store.Close()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	workspaceA := "ws_scope_a_" + suffix
	workspaceB := "ws_scope_b_" + suffix
	periodStart := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	for _, workspaceID := range []string{workspaceA, workspaceB} {
		if err := store.ReserveQuota(QuotaReservation{
			ExecutionID: "exec_shared", WorkspaceID: workspaceID,
			PeriodStart: periodStart, PeriodEnd: periodStart.AddDate(0, 1, 0),
			ReservedRequests: 1, ExpiresAt: time.Now().Add(time.Minute),
		}); err != nil {
			t.Fatalf("reserve %s: %v", workspaceID, err)
		}
	}
	if err := store.AppendInference(InferenceAuditRecord{
		Timestamp: periodStart.Add(time.Hour), RequestID: "exec_shared",
		WorkspaceID: workspaceA, Model: "m1", Status: "failed",
	}); err != nil {
		t.Fatalf("finalize workspace A: %v", err)
	}
	var count int
	if err := store.db.QueryRow(`
		SELECT COUNT(*) FROM quota_reservations
		WHERE workspace_id = $1 AND execution_id = $2`, workspaceB, "exec_shared").Scan(&count); err != nil {
		t.Fatalf("query workspace B reservation: %v", err)
	}
	if count != 1 {
		t.Fatal("finalizing workspace A deleted workspace B reservation")
	}
}

func TestPostgresMigratesGlobalReservationKeyToWorkspaceScope(t *testing.T) {
	dsn := os.Getenv("INFERA_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("INFERA_TEST_POSTGRES_DSN is not configured")
	}
	admin, err := NewPostgresStore(dsn)
	if err != nil {
		t.Fatalf("open admin store: %v", err)
	}
	defer admin.Close()
	schema := "quota_v4_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if _, err := admin.db.Exec(`CREATE SCHEMA "` + schema + `"`); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	defer func() { _, _ = admin.db.Exec(`DROP SCHEMA "` + schema + `" CASCADE`) }()

	scopedDSN, err := postgresDSNWithParameter(dsn, "search_path", schema)
	if err != nil {
		t.Fatalf("scope PostgreSQL DSN: %v", err)
	}
	legacy, err := sql.Open("pgx", scopedDSN)
	if err != nil {
		t.Fatalf("open legacy schema: %v", err)
	}
	if _, err := legacy.Exec(`
		CREATE TABLE audit_ledger_metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL);
		INSERT INTO audit_ledger_metadata VALUES ('schema_version', '4'), ('writer_protocol', '1');
		CREATE TABLE quota_reservations (
			execution_id TEXT PRIMARY KEY,
			workspace_id TEXT NOT NULL,
			period_start_ms BIGINT NOT NULL,
			period_end_ms BIGINT NOT NULL,
			reserved_requests BIGINT NOT NULL,
			reserved_tokens BIGINT NOT NULL,
			expires_at_ms BIGINT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		_ = legacy.Close()
		t.Fatalf("seed legacy schema: %v", err)
	}
	_ = legacy.Close()

	migrated, err := NewPostgresStore(scopedDSN)
	if err != nil {
		t.Fatalf("migrate legacy schema: %v", err)
	}
	defer migrated.Close()
	var primaryKeyColumns, schemaVersion, writerProtocol string
	if err := migrated.db.QueryRow(`
		SELECT string_agg(attribute.attname, ',' ORDER BY key_column.ordinality)
		FROM pg_constraint constraint_row
		CROSS JOIN LATERAL unnest(constraint_row.conkey) WITH ORDINALITY AS key_column(attnum, ordinality)
		JOIN pg_attribute attribute
		  ON attribute.attrelid = constraint_row.conrelid AND attribute.attnum = key_column.attnum
		WHERE constraint_row.conrelid = 'quota_reservations'::regclass AND constraint_row.contype = 'p'`).Scan(&primaryKeyColumns); err != nil {
		t.Fatalf("query migrated primary key: %v", err)
	}
	if primaryKeyColumns != "workspace_id,execution_id" {
		t.Fatalf("unexpected migrated primary key: %s", primaryKeyColumns)
	}
	if err := migrated.db.QueryRow(`SELECT value FROM audit_ledger_metadata WHERE key = 'schema_version'`).Scan(&schemaVersion); err != nil {
		t.Fatalf("query schema version: %v", err)
	}
	if err := migrated.db.QueryRow(`SELECT value FROM audit_ledger_metadata WHERE key = 'writer_protocol'`).Scan(&writerProtocol); err != nil {
		t.Fatalf("query writer protocol: %v", err)
	}
	if schemaVersion != postgresSchemaVersion || writerProtocol != postgresWriterProtocol {
		t.Fatalf("unexpected migrated metadata: schema=%s protocol=%s", schemaVersion, writerProtocol)
	}
	if _, err := migrated.db.Exec(`
		INSERT INTO quota_reservations
		(workspace_id, execution_id, period_start_ms, period_end_ms, reserved_requests, reserved_tokens, expires_at_ms)
		VALUES ('ws_a', 'shared', 1, 2, 1, 0, 3), ('ws_b', 'shared', 1, 2, 1, 0, 3)`); err != nil {
		t.Fatalf("workspace-scoped identities conflict after migration: %v", err)
	}
}

func runQuotaHelper(dsn string) {
	var err error
	dsn, err = postgresDSNWithParameter(dsn, "application_name", os.Getenv("INFERA_QUOTA_APPLICATION_NAME"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	store, err := NewPostgresStore(dsn)
	if err != nil {
		os.Exit(2)
	}
	if err := os.WriteFile(os.Getenv("INFERA_QUOTA_READY_FILE"), []byte("ready"), 0600); err != nil {
		os.Exit(5)
	}
	gateLockID, err := strconv.ParseInt(os.Getenv("INFERA_QUOTA_GATE_LOCK_ID"), 10, 64)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	store.quotaAdmissionBarrier = func(tx *sql.Tx) error {
		_, err := tx.Exec(`SELECT pg_advisory_xact_lock_shared($1)`, gateLockID)
		return err
	}
	periodStart := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	if err := waitForFiles(5*time.Second, os.Getenv("INFERA_QUOTA_START_FILE")); err != nil {
		os.Exit(4)
	}
	limit := int64(1)
	err = store.ReserveQuota(QuotaReservation{
		ExecutionID: os.Getenv("INFERA_QUOTA_EXECUTION"), WorkspaceID: os.Getenv("INFERA_QUOTA_WORKSPACE"),
		PeriodStart: periodStart, PeriodEnd: periodStart.AddDate(0, 1, 0), ReservedRequests: 1,
		MonthlyRequestLimit: &limit, ExpiresAt: time.Now().UTC().Add(time.Minute),
	})
	_ = store.Close()
	if errors.Is(err, ErrQuotaExceeded) {
		os.Exit(42)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(3)
	}
}

func postgresDSNWithParameter(dsn, key, value string) (string, error) {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Set(key, value)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func waitForFiles(timeout time.Duration, paths ...string) error {
	deadline := time.Now().Add(timeout)
	for {
		missing := false
		for _, path := range paths {
			if _, err := os.Stat(path); err != nil {
				missing = true
				break
			}
		}
		if !missing {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for files %v", paths)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func exitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func TestPostgresFirstWriteAndSQLiteMigration(t *testing.T) {
	dsn := os.Getenv("INFERA_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("INFERA_TEST_POSTGRES_DSN is not configured")
	}
	target, err := NewPostgresStore(dsn)
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	defer target.Close()

	workspaceID := "ws_migration_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	sourcePath := filepath.Join(t.TempDir(), "audit.db")
	source, err := NewStore(sourcePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	record := InferenceAuditRecord{
		Timestamp: time.Now().UTC().Truncate(time.Millisecond), RequestID: "exec_1",
		WorkspaceID: workspaceID, Model: "m1", Status: "success", TokenCount: 11,
	}
	if err := source.AppendInference(record); err != nil {
		t.Fatalf("seed SQLite: %v", err)
	}
	if _, err := source.db.Exec(`
		UPDATE inference_audit
		SET prompt_tokens = 7, completion_tokens = 3, token_count = 0,
		    token_source = 'exact', billable = 0, status = 'success', error_code = 'preserved'
		WHERE workspace_id = ? AND request_id = ?`, workspaceID, record.RequestID); err != nil {
		t.Fatalf("seed non-canonical persisted fields: %v", err)
	}
	_ = source.Close()
	if copied, err := target.MigrateSQLiteHistory(context.Background(), sourcePath); err != nil || copied != 1 {
		t.Fatalf("MigrateSQLiteHistory copied=%d err=%v", copied, err)
	}
	if copied, err := target.MigrateSQLiteHistory(context.Background(), sourcePath); err != nil || copied != 1 {
		t.Fatalf("idempotent MigrateSQLiteHistory copied=%d err=%v", copied, err)
	}
	conflict := record
	conflict.TokenCount = 99
	if err := target.AppendInference(conflict); err == nil {
		t.Fatal("expected conflicting second write to fail")
	}
	var promptTokens, completionTokens, tokenCount, billable int64
	var status, errorCode string
	if err := target.db.QueryRow(`
		SELECT prompt_tokens, completion_tokens, token_count, billable, status, error_code
		FROM inference_audit WHERE workspace_id = $1 AND request_id = $2`, workspaceID, record.RequestID,
	).Scan(&promptTokens, &completionTokens, &tokenCount, &billable, &status, &errorCode); err != nil {
		t.Fatalf("query migrated exact row: %v", err)
	}
	if promptTokens != 7 || completionTokens != 3 || tokenCount != 0 || billable != 0 || status != "success" || errorCode != "preserved" {
		t.Fatalf("migration normalized persisted fields: prompt=%d completion=%d total=%d billable=%d status=%q error=%q",
			promptTokens, completionTokens, tokenCount, billable, status, errorCode)
	}
}

func TestPostgresRejectsIncompatibleWriterProtocol(t *testing.T) {
	dsn := os.Getenv("INFERA_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("INFERA_TEST_POSTGRES_DSN is not configured")
	}
	store, err := NewPostgresStore(dsn)
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	defer store.Close()
	if _, err := store.db.Exec(`UPDATE audit_ledger_metadata SET value = '999' WHERE key = 'writer_protocol'`); err != nil {
		t.Fatalf("set incompatible protocol: %v", err)
	}
	defer func() {
		_, _ = store.db.Exec(`UPDATE audit_ledger_metadata SET value = $1 WHERE key = 'writer_protocol'`, postgresWriterProtocol)
	}()
	if incompatible, err := NewPostgresStore(dsn); err == nil {
		_ = incompatible.Close()
		t.Fatal("expected incompatible writer protocol to fail startup")
	}
}
