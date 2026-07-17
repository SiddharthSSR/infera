package audit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/infera/infera/go/internal/migrate"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()

	s, err := NewStore(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestAppendInference(t *testing.T) {
	s := newTestStore(t)

	err := s.AppendInference(InferenceAuditRecord{
		Timestamp:        time.Now().UTC(),
		RequestID:        "req_1",
		KeyID:            "inf_abcd1234",
		WorkspaceID:      "ws_default",
		Model:            "meta-llama/Llama-3.1-8B-Instruct",
		WorkerID:         "w1",
		Stream:           false,
		MessageCount:     2,
		PromptTokens:     96,
		CompletionTokens: 32,
		TokenCount:       128,
		TokenSource:      "exact",
		PromptHash:       "aabbccddeeff0011",
		Status:           "success",
		LatencyMS:        320,
	})
	if err != nil {
		t.Fatalf("AppendInference: %v", err)
	}

	var promptTokens, completionTokens, totalTokens, billable int
	var tokenSource string
	if err := s.db.QueryRow(
		`SELECT prompt_tokens, completion_tokens, token_count, token_source, billable
		 FROM inference_audit WHERE workspace_id = ? AND request_id = ?`,
		"ws_default", "req_1",
	).Scan(&promptTokens, &completionTokens, &totalTokens, &tokenSource, &billable); err != nil {
		t.Fatalf("query persisted usage: %v", err)
	}
	if promptTokens != 96 || completionTokens != 32 || totalTokens != 128 || tokenSource != "exact" || billable != 1 {
		t.Fatalf("unexpected persisted usage detail: prompt=%d completion=%d total=%d source=%q billable=%d", promptTokens, completionTokens, totalTokens, tokenSource, billable)
	}
}

func TestAppendInferenceRejectsInvalidTokenData(t *testing.T) {
	s := newTestStore(t)
	tests := []struct {
		name   string
		record InferenceAuditRecord
	}{
		{name: "negative prompt tokens", record: InferenceAuditRecord{RequestID: "req_negative", Model: "m1", Status: "success", PromptTokens: -1}},
		{name: "unknown token source", record: InferenceAuditRecord{RequestID: "req_source", Model: "m1", Status: "success", TokenSource: "guessed"}},
		{name: "unknown cost accuracy", record: InferenceAuditRecord{RequestID: "req_cost_accuracy", Model: "m1", Status: "success", Cost: CostAttribution{CostAccuracy: "guessed"}}},
		{name: "implicit price units", record: InferenceAuditRecord{RequestID: "req_cost_units", Model: "m1", Status: "success", Cost: CostAttribution{CostAccuracy: CostAccuracyEstimated, PriceAmountNano: 1, ObservedActiveConcurrency: 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := s.AppendInference(tt.record); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestAppendInferenceSeparatesClientCorrelationFromExecutionIdentity(t *testing.T) {
	s := newTestStore(t)
	base := time.Date(2026, 7, 16, 5, 0, 0, 0, time.UTC)

	first := InferenceAuditRecord{
		Timestamp:        base,
		RequestID:        "exec_1",
		ClientRequestID:  "req_duplicate",
		KeyID:            "inf_key_a",
		WorkspaceID:      "ws_alpha",
		Model:            "m1",
		Status:           "success",
		PromptTokens:     10,
		CompletionTokens: 5,
		TokenSource:      "exact",
	}
	if err := s.AppendInference(first); err != nil {
		t.Fatalf("AppendInference first: %v", err)
	}
	if err := s.AppendInference(first); err != nil {
		t.Fatalf("AppendInference idempotent retry: %v", err)
	}
	conflict := first
	conflict.TokenCount = 999
	if err := s.AppendInference(conflict); err == nil {
		t.Fatal("expected conflicting execution identity to be rejected")
	}
	second := first
	second.RequestID = "exec_2"
	second.TokenCount = 20
	if err := s.AppendInference(second); err != nil {
		t.Fatalf("AppendInference second execution: %v", err)
	}

	summary, err := s.UsageSummary(UsageSummaryQuery{
		Start:       base.Add(-time.Hour),
		End:         base.Add(time.Hour),
		WorkspaceID: "ws_alpha",
	})
	if err != nil {
		t.Fatalf("UsageSummary: %v", err)
	}
	if summary.AttemptCount != 2 || summary.RequestCount != 2 || summary.TokenCount != 35 {
		t.Fatalf("client correlation suppressed usage: %+v", summary)
	}

	otherWorkspace := first
	otherWorkspace.WorkspaceID = "ws_beta"
	if err := s.AppendInference(otherWorkspace); err != nil {
		t.Fatalf("AppendInference other workspace: %v", err)
	}
}

func TestTrustworthyUsageMigrationPreservesLegacyCollisionsAndFirstWrite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "audit.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if err := migrate.Run(db, auditMigrations[:2]); err != nil {
		_ = db.Close()
		t.Fatalf("migrate v1-v2: %v", err)
	}
	for _, row := range []struct {
		workspaceID string
		requestID   string
		tokenCount  int
	}{
		{workspaceID: "ws_default", requestID: "req_legacy", tokenCount: 10},
		{workspaceID: "ws_default", requestID: "req_legacy", tokenCount: 20},
		{workspaceID: "ws_alpha", requestID: "req_duplicate", tokenCount: 30},
		{workspaceID: "ws_alpha", requestID: "req_duplicate", tokenCount: 40},
	} {
		if _, err := db.Exec(`
			INSERT INTO inference_audit (
				ts_unix_ms, request_id, key_id, workspace_id, model, status, token_count
			) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			time.Now().UTC().UnixMilli(), row.requestID, "key_1", row.workspaceID, "model-1", "success", row.tokenCount,
		); err != nil {
			_ = db.Close()
			t.Fatalf("insert pre-v3 row: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close pre-v3 database: %v", err)
	}

	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	rows, err := store.db.Query(`
		SELECT request_id, token_count
		FROM inference_audit
		WHERE workspace_id = 'ws_default'
		ORDER BY id`)
	if err != nil {
		t.Fatalf("query legacy rows: %v", err)
	}
	defer rows.Close()
	var legacyRequests []string
	var legacyTokens []int
	for rows.Next() {
		var requestID string
		var tokenCount int
		if err := rows.Scan(&requestID, &tokenCount); err != nil {
			t.Fatalf("scan legacy row: %v", err)
		}
		legacyRequests = append(legacyRequests, requestID)
		legacyTokens = append(legacyTokens, tokenCount)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate legacy rows: %v", err)
	}
	if len(legacyRequests) != 2 || legacyRequests[0] != "req_legacy" || legacyRequests[1] == "req_legacy" {
		t.Fatalf("legacy collisions were not preserved safely: requests=%v", legacyRequests)
	}
	if legacyTokens[0] != 10 || legacyTokens[1] != 20 {
		t.Fatalf("unexpected legacy token totals: %v", legacyTokens)
	}

	var tokenCount int
	if err := store.db.QueryRow(`
		SELECT token_count FROM inference_audit
		WHERE workspace_id = 'ws_alpha' AND request_id = 'req_duplicate'`,
	).Scan(&tokenCount); err != nil {
		t.Fatalf("query deduplicated row: %v", err)
	}
	if tokenCount != 30 {
		t.Fatalf("expected earliest row to survive, got token_count=%d", tokenCount)
	}

	var clientRequestID string
	if err := store.db.QueryRow(`SELECT client_request_id FROM inference_audit WHERE workspace_id = 'ws_alpha'`).Scan(&clientRequestID); err != nil {
		t.Fatalf("query migrated client request id: %v", err)
	}
	if clientRequestID != "" {
		t.Fatalf("expected safe empty correlation metadata for legacy row, got %q", clientRequestID)
	}
}

func TestReserveQuotaAtomicallyEnforcesConcurrentHardLimits(t *testing.T) {
	tests := []struct {
		name         string
		requestLimit *int64
		tokenLimit   *int64
		tokens       int64
	}{
		{name: "requests", requestLimit: int64Ptr(1), tokens: 1},
		{name: "tokens", tokenLimit: int64Ptr(10), tokens: 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "audit.db")
			stores := make([]*Store, 2)
			for i := range stores {
				store, err := NewStore(dbPath)
				if err != nil {
					t.Fatalf("NewStore %d: %v", i, err)
				}
				stores[i] = store
				t.Cleanup(func() { _ = store.Close() })
			}
			periodStart := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
			start := make(chan struct{})
			results := make(chan error, 2)
			var wg sync.WaitGroup
			for i := range stores {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					results <- stores[i].ReserveQuota(QuotaReservation{
						ExecutionID:         fmt.Sprintf("exec_%d", i),
						WorkspaceID:         "ws_alpha",
						PeriodStart:         periodStart,
						PeriodEnd:           periodStart.AddDate(0, 1, 0),
						ReservedRequests:    1,
						ReservedTokens:      tt.tokens,
						MonthlyRequestLimit: tt.requestLimit,
						MonthlyTokenLimit:   tt.tokenLimit,
						ExpiresAt:           time.Now().UTC().Add(time.Minute),
					})
				}(i)
			}
			close(start)
			wg.Wait()
			close(results)

			var admitted, rejected int
			for err := range results {
				switch {
				case err == nil:
					admitted++
				case errors.Is(err, ErrQuotaExceeded):
					rejected++
				default:
					t.Fatalf("unexpected reservation result: %v", err)
				}
			}
			if admitted != 1 || rejected != 1 {
				t.Fatalf("expected one admission and one rejection, got admitted=%d rejected=%d", admitted, rejected)
			}
		})
	}
}

func TestAppendInferenceAtomicallyReconcilesReservations(t *testing.T) {
	s := newTestStore(t)
	periodStart := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	limit := int64(1)
	reserve := func(executionID string) error {
		return s.ReserveQuota(QuotaReservation{
			ExecutionID: executionID, WorkspaceID: "ws_alpha",
			PeriodStart: periodStart, PeriodEnd: periodStart.AddDate(0, 1, 0),
			ReservedRequests: 1, ReservedTokens: 10, MonthlyRequestLimit: &limit,
			ExpiresAt: time.Now().UTC().Add(time.Minute),
		})
	}
	if err := reserve("exec_canceled"); err != nil {
		t.Fatalf("reserve canceled execution: %v", err)
	}
	if err := reserve("exec_canceled"); err != nil {
		t.Fatalf("retrying the same reservation should be idempotent: %v", err)
	}
	if err := s.AppendInference(InferenceAuditRecord{
		Timestamp: periodStart.Add(time.Hour), RequestID: "exec_canceled", ClientRequestID: "retry-id",
		WorkspaceID: "ws_alpha", Model: "m1", Status: "client_canceled",
	}); err != nil {
		t.Fatalf("record cancellation: %v", err)
	}
	if err := reserve("exec_success"); err != nil {
		t.Fatalf("reservation was not released after cancellation: %v", err)
	}
	if err := s.AppendInference(InferenceAuditRecord{
		Timestamp: periodStart.Add(2 * time.Hour), RequestID: "exec_success", ClientRequestID: "retry-id",
		WorkspaceID: "ws_alpha", Model: "m1", Status: "success", TokenCount: 9,
	}); err != nil {
		t.Fatalf("record success: %v", err)
	}
	if err := reserve("exec_over_limit"); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("expected committed usage to enforce the request limit, got %v", err)
	}
}

func TestQuotaReservationsAreWorkspaceScoped(t *testing.T) {
	s := newTestStore(t)
	periodStart := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	for _, workspaceID := range []string{"ws_alpha", "ws_beta"} {
		if err := s.ReserveQuota(QuotaReservation{
			ExecutionID: "exec_shared", WorkspaceID: workspaceID,
			PeriodStart: periodStart, PeriodEnd: periodStart.AddDate(0, 1, 0),
			ReservedRequests: 1, ExpiresAt: time.Now().UTC().Add(time.Minute),
		}); err != nil {
			t.Fatalf("reserve %s: %v", workspaceID, err)
		}
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM quota_reservations WHERE execution_id = ?`, "exec_shared").Scan(&count); err != nil {
		t.Fatalf("count shared execution reservations: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected two workspace-scoped reservations, got %d", count)
	}
	if err := s.AppendInference(InferenceAuditRecord{
		Timestamp: periodStart.Add(time.Hour), RequestID: "exec_shared",
		WorkspaceID: "ws_alpha", Model: "m1", Status: "failed",
	}); err != nil {
		t.Fatalf("finalize ws_alpha: %v", err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM quota_reservations WHERE workspace_id = ? AND execution_id = ?`, "ws_beta", "exec_shared").Scan(&count); err != nil {
		t.Fatalf("count ws_beta reservation: %v", err)
	}
	if count != 1 {
		t.Fatalf("finalizing ws_alpha deleted ws_beta reservation")
	}
}

func TestSQLiteQuotaReservationMigrationScopesExecutionIdentity(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "audit.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if err := migrate.Run(db, auditMigrations[:4]); err != nil {
		_ = db.Close()
		t.Fatalf("migrate v1-v4: %v", err)
	}
	periodStart := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	if _, err := db.Exec(`
		INSERT INTO quota_reservations
		(execution_id, workspace_id, period_start_ms, period_end_ms, reserved_requests, reserved_tokens, expires_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"exec_shared", "ws_alpha", periodStart.UnixMilli(), periodStart.AddDate(0, 1, 0).UnixMilli(), 1, 0, time.Now().Add(time.Minute).UnixMilli(),
	); err != nil {
		_ = db.Close()
		t.Fatalf("seed v4 reservation: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close v4 database: %v", err)
	}
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("migrate to v5: %v", err)
	}
	defer s.Close()
	if err := s.ReserveQuota(QuotaReservation{
		ExecutionID: "exec_shared", WorkspaceID: "ws_beta",
		PeriodStart: periodStart, PeriodEnd: periodStart.AddDate(0, 1, 0),
		ReservedRequests: 1, ExpiresAt: time.Now().Add(time.Minute),
	}); err != nil {
		t.Fatalf("reserve duplicate execution in second workspace: %v", err)
	}
}

func TestReserveQuotaReusesExpiredExecutionFromAnotherPeriod(t *testing.T) {
	s := newTestStore(t)
	previousStart := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if _, err := s.db.Exec(`
		INSERT INTO quota_reservations
		(execution_id, workspace_id, period_start_ms, period_end_ms, reserved_requests, reserved_tokens, expires_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"exec_reused", "ws_alpha", previousStart.UnixMilli(), previousStart.AddDate(0, 1, 0).UnixMilli(),
		1, 10, time.Now().UTC().Add(-time.Minute).UnixMilli(),
	); err != nil {
		t.Fatalf("seed expired reservation: %v", err)
	}
	currentStart := previousStart.AddDate(0, 1, 0)
	if err := s.ReserveQuota(QuotaReservation{
		ExecutionID: "exec_reused", WorkspaceID: "ws_alpha",
		PeriodStart: currentStart, PeriodEnd: currentStart.AddDate(0, 1, 0),
		ReservedRequests: 1, ExpiresAt: time.Now().UTC().Add(time.Minute),
	}); err != nil {
		t.Fatalf("reuse expired execution identity: %v", err)
	}
	var periodStartMS int64
	if err := s.db.QueryRow(`SELECT period_start_ms FROM quota_reservations WHERE workspace_id = ? AND execution_id = ?`, "ws_alpha", "exec_reused").Scan(&periodStartMS); err != nil {
		t.Fatalf("query replacement reservation: %v", err)
	}
	if periodStartMS != currentStart.UnixMilli() {
		t.Fatalf("expired reservation was not replaced: period_start_ms=%d", periodStartMS)
	}
}

func int64Ptr(value int64) *int64 { return &value }

func TestMigrateSQLiteHistoryRejectsMissingSource(t *testing.T) {
	target := &Store{dialect: dialectPostgres}
	if _, err := target.MigrateSQLiteHistory(context.Background(), filepath.Join(t.TempDir(), "missing.db")); err == nil {
		t.Fatal("expected a missing SQLite source to fail instead of creating an empty ledger")
	}
}

func TestUsageByKey(t *testing.T) {
	s := newTestStore(t)

	base := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	records := []InferenceAuditRecord{
		{
			Timestamp:   base.Add(-2 * time.Hour),
			RequestID:   "req_1",
			KeyID:       "inf_key_a",
			WorkspaceID: "ws_alpha",
			Model:       "m1",
			Status:      "success",
			TokenCount:  100,
		},
		{
			Timestamp:   base.Add(-1 * time.Hour),
			RequestID:   "req_2",
			KeyID:       "inf_key_a",
			WorkspaceID: "ws_alpha",
			Model:       "m1",
			Status:      "success",
			TokenCount:  50,
		},
		{
			Timestamp:   base.Add(-30 * time.Minute),
			RequestID:   "req_3",
			KeyID:       "inf_key_b",
			WorkspaceID: "ws_beta",
			Model:       "m1",
			Status:      "inference_error",
			TokenCount:  10,
		},
	}
	for _, rec := range records {
		if err := s.AppendInference(rec); err != nil {
			t.Fatalf("AppendInference: %v", err)
		}
	}

	rows, err := s.UsageByKey(UsageQuery{
		Start:  base.Add(-3 * time.Hour),
		End:    base.Add(time.Hour),
		Bucket: "day",
	})
	if err != nil {
		t.Fatalf("UsageByKey: %v", err)
	}

	if len(rows) != 2 {
		t.Fatalf("expected 2 usage rows (2 keys), got %d", len(rows))
	}

	for _, row := range rows {
		switch row.KeyID {
		case "inf_key_a":
			if row.WorkspaceID != "ws_alpha" {
				t.Fatalf("expected ws_alpha, got %q", row.WorkspaceID)
			}
			if row.RequestCount != 2 {
				t.Fatalf("expected 2 requests for key a, got %d", row.RequestCount)
			}
			if row.AttemptCount != 2 {
				t.Fatalf("expected 2 attempts for key a, got %d", row.AttemptCount)
			}
			if row.TokenCount != 150 {
				t.Fatalf("expected 150 tokens for key a, got %d", row.TokenCount)
			}
			if row.SuccessCount != 2 || row.ErrorCount != 0 {
				t.Fatalf("expected success=2 error=0 for key a, got success=%d error=%d", row.SuccessCount, row.ErrorCount)
			}
		case "inf_key_b":
			if row.WorkspaceID != "ws_beta" {
				t.Fatalf("expected ws_beta, got %q", row.WorkspaceID)
			}
			if row.AttemptCount != 1 || row.RequestCount != 0 {
				t.Fatalf("expected 1 attempt and 0 billable requests for key b, got attempts=%d requests=%d", row.AttemptCount, row.RequestCount)
			}
			if row.TokenCount != 0 {
				t.Fatalf("expected failed request to contribute 0 billable tokens, got %d", row.TokenCount)
			}
			if row.SuccessCount != 0 || row.ErrorCount != 1 {
				t.Fatalf("expected success=0 error=1 for key b, got success=%d error=%d", row.SuccessCount, row.ErrorCount)
			}
		default:
			t.Fatalf("unexpected key id %q", row.KeyID)
		}
	}
}

func TestUsageSummary(t *testing.T) {
	s := newTestStore(t)

	base := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	records := []InferenceAuditRecord{
		{
			Timestamp:   base.Add(-2 * time.Hour),
			RequestID:   "req_1",
			KeyID:       "inf_key_a",
			WorkspaceID: "ws_alpha",
			Model:       "m1",
			Status:      "success",
			TokenCount:  100,
		},
		{
			Timestamp:   base.Add(-1 * time.Hour),
			RequestID:   "req_2",
			KeyID:       "inf_key_a",
			WorkspaceID: "ws_alpha",
			Model:       "m1",
			Status:      "inference_error",
			TokenCount:  10,
		},
		{
			Timestamp:   base.Add(-30 * time.Minute),
			RequestID:   "req_3",
			KeyID:       "inf_key_b",
			WorkspaceID: "ws_beta",
			Model:       "m1",
			Status:      "success",
			TokenCount:  50,
		},
	}
	for _, rec := range records {
		if err := s.AppendInference(rec); err != nil {
			t.Fatalf("AppendInference: %v", err)
		}
	}

	summary, err := s.UsageSummary(UsageSummaryQuery{
		Start:       base.Add(-3 * time.Hour),
		End:         base.Add(time.Hour),
		WorkspaceID: "ws_alpha",
	})
	if err != nil {
		t.Fatalf("UsageSummary: %v", err)
	}
	if summary.AttemptCount != 2 || summary.RequestCount != 1 || summary.TokenCount != 100 || summary.SuccessCount != 1 || summary.ErrorCount != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestUsageSummarySeparatesExactAndEstimatedTokens(t *testing.T) {
	s := newTestStore(t)
	base := time.Date(2026, 7, 16, 5, 0, 0, 0, time.UTC)
	records := []InferenceAuditRecord{
		{Timestamp: base, RequestID: "req_exact", WorkspaceID: "ws_alpha", Model: "m1", Status: "success", TokenCount: 100, TokenSource: TokenSourceExact},
		{Timestamp: base.Add(time.Minute), RequestID: "req_estimated", WorkspaceID: "ws_alpha", Model: "m1", Status: "success", TokenCount: 40, TokenSource: TokenSourceEstimated},
		{Timestamp: base.Add(2 * time.Minute), RequestID: "req_mixed", WorkspaceID: "ws_alpha", Model: "m1", Status: "success", TokenCount: 60, TokenSource: TokenSourceMixed},
		{Timestamp: base.Add(3 * time.Minute), RequestID: "req_failed", WorkspaceID: "ws_alpha", Model: "m1", Status: "failed", TokenCount: 999, TokenSource: TokenSourceEstimated},
	}
	for _, rec := range records {
		if err := s.AppendInference(rec); err != nil {
			t.Fatalf("AppendInference %s: %v", rec.RequestID, err)
		}
	}

	summary, err := s.UsageSummary(UsageSummaryQuery{Start: base.Add(-time.Hour), End: base.Add(time.Hour), WorkspaceID: "ws_alpha"})
	if err != nil {
		t.Fatalf("UsageSummary: %v", err)
	}
	if summary.AttemptCount != 4 || summary.RequestCount != 3 || summary.TokenCount != 200 {
		t.Fatalf("unexpected usage totals: %+v", summary)
	}
	if summary.ExactRequestCount != 1 || summary.EstimatedRequestCount != 2 {
		t.Fatalf("unexpected request accuracy totals: %+v", summary)
	}
	if summary.ExactTokenCount != 100 || summary.EstimatedTokenCount != 100 {
		t.Fatalf("unexpected token accuracy totals: %+v", summary)
	}
}

func TestCostAttributionIsImmutableAndSummarizesAccuracy(t *testing.T) {
	s := newTestStore(t)
	base := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	estimated := CostAttribution{
		Provider: "runpod", InstanceID: "instance-1", PriceSnapshotVersion: "provider-instance-hourly-v1",
		PriceAmountNano: 1_800_000_000, PriceCurrency: "USD", PriceTimeUnit: "hour",
		PriceCapturedAt: base.Add(-time.Hour), CostNano: 500_000, CostAccuracy: CostAccuracyEstimated,
		CostAttributionMethod: CostMethodActiveInstanceTimeShareV1, ObservedActiveConcurrency: 2,
	}
	record := InferenceAuditRecord{
		Timestamp: base, RequestID: "costed-success", WorkspaceID: "ws_alpha", Model: "m1",
		Status: "success", PromptTokens: 80, CompletionTokens: 20, TokenSource: TokenSourceExact, Cost: estimated,
	}
	if err := s.AppendInference(record); err != nil {
		t.Fatalf("AppendInference: %v", err)
	}
	if err := s.AppendInference(record); err != nil {
		t.Fatalf("idempotent replay: %v", err)
	}
	conflict := record
	conflict.Cost.CostNano++
	if err := s.AppendInference(conflict); err == nil {
		t.Fatal("expected changed historical cost to conflict")
	}
	if err := s.AppendInference(InferenceAuditRecord{
		Timestamp: base.Add(time.Minute), RequestID: "costed-failure", WorkspaceID: "ws_alpha", Model: "m1",
		Status: "failed", TokenCount: 10, TokenSource: TokenSourceEstimated, Cost: estimated,
	}); err != nil {
		t.Fatalf("append failed attempt cost: %v", err)
	}
	if err := s.AppendInference(InferenceAuditRecord{
		Timestamp: base.Add(2 * time.Minute), RequestID: "missing-price", WorkspaceID: "ws_alpha", Model: "m1",
		Status: "success", TokenCount: 50, TokenSource: TokenSourceMixed,
	}); err != nil {
		t.Fatalf("append unavailable cost: %v", err)
	}

	summary, err := s.UsageSummary(UsageSummaryQuery{Start: base.Add(-time.Hour), End: base.Add(time.Hour), WorkspaceID: "ws_alpha"})
	if err != nil {
		t.Fatalf("UsageSummary: %v", err)
	}
	if summary.AttemptCount != 3 || summary.CostNano != 1_000_000 || summary.CostedTokenCount != 110 {
		t.Fatalf("unexpected cost totals: %+v", summary)
	}
	if summary.ExactCostCount != 0 || summary.EstimatedCostCount != 2 || summary.UnavailableCostCount != 1 {
		t.Fatalf("unexpected cost accuracy: %+v", summary)
	}
}
