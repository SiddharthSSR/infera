package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/infera/infera/go/internal/audit"
)

func TestHandleGetAuditUsage_Success(t *testing.T) {
	store, err := audit.NewStore(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("audit.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().UTC()
	if err := store.AppendInference(audit.InferenceAuditRecord{
		Timestamp:  now.Add(-30 * time.Minute),
		RequestID:  "req-1",
		KeyID:      "inf_key_a",
		Model:      "m1",
		Status:     "success",
		TokenCount: 120,
	}); err != nil {
		t.Fatalf("AppendInference req-1: %v", err)
	}
	if err := store.AppendInference(audit.InferenceAuditRecord{
		Timestamp:  now.Add(-20 * time.Minute),
		RequestID:  "req-2",
		KeyID:      "inf_key_a",
		Model:      "m1",
		Status:     "inference_error",
		TokenCount: 0,
	}); err != nil {
		t.Fatalf("AppendInference req-2: %v", err)
	}

	g := New(DefaultConfig(), nil, nil)
	g.SetAuditStore(store)

	start := now.Add(-2 * time.Hour).Format(time.RFC3339)
	end := now.Add(time.Hour).Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodGet, "/api/audit/usage?bucket=day&start="+start+"&end="+end, nil)
	rec := httptest.NewRecorder()
	g.handleGetAuditUsage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Rows []struct {
			KeyID     string `json:"key_id"`
			Requests  int64  `json:"requests"`
			Tokens    int64  `json:"tokens"`
			Successes int64  `json:"successes"`
			Errors    int64  `json:"errors"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(payload.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(payload.Rows))
	}

	row := payload.Rows[0]
	if row.KeyID != "inf_key_a" {
		t.Fatalf("expected key inf_key_a, got %q", row.KeyID)
	}
	if row.Requests != 2 || row.Tokens != 120 || row.Successes != 1 || row.Errors != 1 {
		t.Fatalf("unexpected row values: %+v", row)
	}
}

func TestHandleGetAuditUsage_InvalidBucket(t *testing.T) {
	store, err := audit.NewStore(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("audit.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	g := New(DefaultConfig(), nil, nil)
	g.SetAuditStore(store)

	req := httptest.NewRequest(http.MethodGet, "/api/audit/usage?bucket=week", nil)
	rec := httptest.NewRecorder()
	g.handleGetAuditUsage(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
