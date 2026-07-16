package audit

import (
	"database/sql"
	"path/filepath"
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := s.AppendInference(tt.record); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestAppendInferenceIsIdempotentPerWorkspaceRequest(t *testing.T) {
	s := newTestStore(t)
	base := time.Date(2026, 7, 16, 5, 0, 0, 0, time.UTC)

	first := InferenceAuditRecord{
		Timestamp:        base,
		RequestID:        "req_duplicate",
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
	duplicate := first
	duplicate.TokenCount = 999
	if err := s.AppendInference(duplicate); err != nil {
		t.Fatalf("AppendInference duplicate: %v", err)
	}

	summary, err := s.UsageSummary(UsageSummaryQuery{
		Start:       base.Add(-time.Hour),
		End:         base.Add(time.Hour),
		WorkspaceID: "ws_alpha",
	})
	if err != nil {
		t.Fatalf("UsageSummary: %v", err)
	}
	if summary.AttemptCount != 1 || summary.RequestCount != 1 || summary.TokenCount != 15 {
		t.Fatalf("duplicate inflated usage: %+v", summary)
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
