package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/infera/infera/go/internal/audit"
	"github.com/infera/infera/go/internal/auth"
)

func TestHandleGetAuditUsage_Success(t *testing.T) {
	store, err := audit.NewStore(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("audit.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	now := time.Now().UTC()
	if err := store.AppendInference(audit.InferenceAuditRecord{
		Timestamp:   now.Add(-30 * time.Minute),
		RequestID:   "req-1",
		KeyID:       "inf_key_a",
		WorkspaceID: "ws_alpha",
		Model:       "m1",
		Status:      "success",
		TokenCount:  120,
		TokenSource: audit.TokenSourceExact,
		Cost: audit.CostAttribution{
			Provider: "runpod", InstanceID: "i-1", PriceSnapshotVersion: "provider-instance-hourly-v1",
			PriceAmountNano: 1_000_000_000, PriceCurrency: "USD", PriceTimeUnit: "hour",
			PriceCapturedAt: now.Add(-time.Hour), CostNano: 1_000_000, CostAccuracy: audit.CostAccuracyExact,
			CostAttributionMethod: "provider_request_charge_v1", ObservedActiveConcurrency: 1,
		},
	}); err != nil {
		t.Fatalf("AppendInference req-1: %v", err)
	}
	if err := store.AppendInference(audit.InferenceAuditRecord{
		Timestamp:   now.Add(-20 * time.Minute),
		RequestID:   "req-2",
		KeyID:       "inf_key_a",
		WorkspaceID: "ws_alpha",
		Model:       "m1",
		Status:      "inference_error",
		TokenCount:  0,
	}); err != nil {
		t.Fatalf("AppendInference req-2: %v", err)
	}

	g := New(DefaultConfig(), nil, nil)
	g.SetAuditStore(store)

	start := now.Add(-2 * time.Hour).Format(time.RFC3339)
	end := now.Add(time.Hour).Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodGet, "/api/audit/usage?bucket=day&start="+start+"&end="+end, nil)
	req = req.WithContext(auth.ContextWithKey(req.Context(), &auth.KeyRecord{
		Role:          auth.RoleBilling,
		PrincipalType: auth.PrincipalHuman,
		Status:        "active",
		WorkspaceID:   "ws_alpha",
	}))
	rec := httptest.NewRecorder()
	g.handleGetAuditUsage(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Rows []struct {
			WorkspaceID       string      `json:"workspace_id"`
			KeyID             string      `json:"key_id"`
			Attempts          int64       `json:"attempts"`
			Requests          int64       `json:"requests"`
			Tokens            int64       `json:"tokens"`
			ExactRequests     int64       `json:"exact_requests"`
			EstimatedRequests int64       `json:"estimated_requests"`
			ExactTokens       int64       `json:"exact_tokens"`
			EstimatedTokens   int64       `json:"estimated_tokens"`
			Successes         int64       `json:"successes"`
			Errors            int64       `json:"errors"`
			Cost              costMetrics `json:"cost"`
		} `json:"rows"`
		Reconciliation usageReconciliation `json:"reconciliation"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(payload.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(payload.Rows))
	}

	row := payload.Rows[0]
	if row.WorkspaceID != "ws_alpha" {
		t.Fatalf("expected workspace ws_alpha, got %q", row.WorkspaceID)
	}
	if row.KeyID != "inf_key_a" {
		t.Fatalf("expected key inf_key_a, got %q", row.KeyID)
	}
	if row.Attempts != 2 || row.Requests != 1 || row.Tokens != 120 || row.Successes != 1 || row.Errors != 1 {
		t.Fatalf("unexpected row values: %+v", row)
	}
	if row.ExactRequests != 1 || row.EstimatedRequests != 0 || row.ExactTokens != 120 || row.EstimatedTokens != 0 {
		t.Fatalf("unexpected accuracy values: %+v", row)
	}
	if row.Cost.Currency != "USD" || row.Cost.CostUSD != 0.001 || row.Cost.CostPerRequestUSD != 0.001 || row.Cost.CostedTokens != 120 || row.Cost.ExactRequests != 1 || row.Cost.UnavailableRequests != 1 {
		t.Fatalf("unexpected cost metrics: %+v", row.Cost)
	}
	if payload.Reconciliation.Status != "ok" || len(payload.Reconciliation.Discrepancies) != 0 {
		t.Fatalf("unexpected reconciliation: %+v", payload.Reconciliation)
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
	req = req.WithContext(auth.ContextWithKey(req.Context(), &auth.KeyRecord{
		Role:          auth.RoleBilling,
		PrincipalType: auth.PrincipalHuman,
		Status:        "active",
		WorkspaceID:   auth.DefaultWorkspaceID,
	}))
	rec := httptest.NewRecorder()
	g.handleGetAuditUsage(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
