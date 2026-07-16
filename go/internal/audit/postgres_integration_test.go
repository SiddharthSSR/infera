package audit

import (
	"context"
	"errors"
	"fmt"
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

	workspaceID := "ws_cross_process_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	startFile := filepath.Join(t.TempDir(), "start")
	commands := make([]*exec.Cmd, 2)
	for i := range commands {
		commands[i] = exec.Command(os.Args[0], "-test.run=^TestPostgresCrossProcessQuotaAdmission$")
		commands[i].Env = append(os.Environ(),
			"INFERA_QUOTA_HELPER=1",
			"INFERA_QUOTA_WORKSPACE="+workspaceID,
			fmt.Sprintf("INFERA_QUOTA_EXECUTION=exec_%d", i),
			"INFERA_QUOTA_START_FILE="+startFile,
		)
		if err := commands[i].Start(); err != nil {
			t.Fatalf("start helper %d: %v", i, err)
		}
	}
	if err := os.WriteFile(startFile, []byte("start"), 0600); err != nil {
		t.Fatalf("release helpers: %v", err)
	}

	var admitted, rejected int
	for i, command := range commands {
		err := command.Wait()
		switch {
		case err == nil:
			admitted++
		case exitCode(err) == 42:
			rejected++
		default:
			t.Fatalf("helper %d failed unexpectedly: %v", i, err)
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
	defer func() {
		_, _ = gateConn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, barrier.releaseLockID)
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

	select {
	case err := <-result:
		if !errors.Is(err, ErrQuotaExceeded) {
			t.Fatalf("transition was counted as neither pending nor committed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("quota reservation remained blocked after releasing transition gate")
	}
}

func runQuotaHelper(dsn string) {
	store, err := NewPostgresStore(dsn)
	if err != nil {
		os.Exit(2)
	}
	periodStart := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(os.Getenv("INFERA_QUOTA_START_FILE")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			os.Exit(4)
		}
		time.Sleep(5 * time.Millisecond)
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
		os.Exit(3)
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
	summary, err := target.UsageSummary(UsageSummaryQuery{
		Start: record.Timestamp.Add(-time.Hour), End: record.Timestamp.Add(time.Hour), WorkspaceID: workspaceID,
	})
	if err != nil {
		t.Fatalf("UsageSummary: %v", err)
	}
	if summary.RequestCount != 1 || summary.TokenCount != 11 {
		t.Fatalf("first write was not preserved: %+v", summary)
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
	if _, err := store.db.Exec(`UPDATE audit_ledger_metadata SET value = '2' WHERE key = 'writer_protocol'`); err != nil {
		t.Fatalf("set incompatible protocol: %v", err)
	}
	defer func() {
		_, _ = store.db.Exec(`UPDATE audit_ledger_metadata SET value = '1' WHERE key = 'writer_protocol'`)
	}()
	if incompatible, err := NewPostgresStore(dsn); err == nil {
		_ = incompatible.Close()
		t.Fatal("expected incompatible writer protocol to fail startup")
	}
}
