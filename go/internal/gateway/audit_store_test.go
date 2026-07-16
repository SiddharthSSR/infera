package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/infera/infera/go/internal/audit"
	"github.com/infera/infera/go/internal/auth"
)

type stubAuditUsageStore struct {
	mu                sync.Mutex
	appended          []audit.InferenceAuditRecord
	lastUsageQuery    audit.UsageQuery
	lastSummaryQuery  audit.UsageSummaryQuery
	usageRows         []audit.UsageRow
	usageSummaryValue *audit.UsageSummary
	appendFailures    int
	appendCalls       int
}

func (s *stubAuditUsageStore) AppendInference(rec audit.InferenceAuditRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.appendCalls++
	if s.appendFailures > 0 {
		s.appendFailures--
		return errors.New("temporary audit write failure")
	}
	s.appended = append(s.appended, rec)
	return nil
}

func (s *stubAuditUsageStore) UsageSummary(q audit.UsageSummaryQuery) (*audit.UsageSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSummaryQuery = q
	return s.usageSummaryValue, nil
}

func (s *stubAuditUsageStore) UsageByKey(q audit.UsageQuery) ([]audit.UsageRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastUsageQuery = q
	return s.usageRows, nil
}

func TestReconcileUsageRowsDetectsMismatches(t *testing.T) {
	result := reconcileUsageRows([]audit.UsageRow{{
		AttemptCount:          3,
		SuccessCount:          1,
		ErrorCount:            1,
		RequestCount:          2,
		ExactRequestCount:     1,
		EstimatedRequestCount: 0,
		TokenCount:            100,
		ExactTokenCount:       80,
		EstimatedTokenCount:   10,
	}})
	if result.Status != "mismatch" {
		t.Fatalf("expected mismatch, got %+v", result)
	}
	want := []string{"attempt_status_mismatch", "request_accuracy_mismatch", "token_accuracy_mismatch"}
	if len(result.Discrepancies) != len(want) {
		t.Fatalf("unexpected discrepancies: %+v", result.Discrepancies)
	}
	for i := range want {
		if result.Discrepancies[i] != want[i] {
			t.Fatalf("unexpected discrepancies: %+v", result.Discrepancies)
		}
	}
}

func TestUsageSummaryPayloadReconcilesMonthlyTotals(t *testing.T) {
	store := &stubAuditUsageStore{
		usageSummaryValue: &audit.UsageSummary{
			AttemptCount:          2,
			SuccessCount:          2,
			RequestCount:          2,
			ExactRequestCount:     1,
			EstimatedRequestCount: 0,
			TokenCount:            20,
			ExactTokenCount:       20,
		},
		usageRows: []audit.UsageRow{{
			AttemptCount:      1,
			SuccessCount:      1,
			RequestCount:      1,
			ExactRequestCount: 1,
			TokenCount:        10,
			ExactTokenCount:   10,
		}},
	}
	g := New(DefaultConfig(), nil, nil)
	g.SetAuditStore(store)
	t.Cleanup(func() {
		close(g.auditCh)
		g.auditWg.Wait()
	})

	payload, err := g.usageSummaryPayload("ws_alpha", time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("usageSummaryPayload: %v", err)
	}
	reconciliation, ok := payload["reconciliation"].(usageReconciliation)
	if !ok {
		t.Fatalf("unexpected reconciliation payload: %#v", payload["reconciliation"])
	}
	if reconciliation.Status != "mismatch" || len(reconciliation.Discrepancies) != 1 || reconciliation.Discrepancies[0] != "request_accuracy_mismatch" {
		t.Fatalf("expected monthly mismatch, got %+v", reconciliation)
	}
}

func TestHandleGetAuditUsageUsesInjectedAuditStore(t *testing.T) {
	store := &stubAuditUsageStore{
		usageRows: []audit.UsageRow{{
			BucketStartMS: time.Date(2026, 4, 14, 0, 0, 0, 0, time.UTC).UnixMilli(),
			WorkspaceID:   "ws_alpha",
			KeyID:         "key_1",
			RequestCount:  3,
			TokenCount:    42,
			SuccessCount:  2,
			ErrorCount:    1,
		}},
	}

	g := New(DefaultConfig(), nil, nil)
	g.SetAuditStore(store)
	t.Cleanup(func() {
		close(g.auditCh)
		g.auditWg.Wait()
	})

	req := httptest.NewRequest(http.MethodGet, "/api/audit/usage?bucket=day", nil)
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

	store.mu.Lock()
	lastQuery := store.lastUsageQuery
	store.mu.Unlock()
	if lastQuery.WorkspaceID != "ws_alpha" {
		t.Fatalf("expected workspace ws_alpha, got %q", lastQuery.WorkspaceID)
	}
	if lastQuery.Bucket != "day" {
		t.Fatalf("expected day bucket, got %q", lastQuery.Bucket)
	}

	var payload struct {
		Rows []struct {
			WorkspaceID string `json:"workspace_id"`
			KeyID       string `json:"key_id"`
			Requests    int64  `json:"requests"`
			Tokens      int64  `json:"tokens"`
			Successes   int64  `json:"successes"`
			Errors      int64  `json:"errors"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(payload.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(payload.Rows))
	}
	if payload.Rows[0].Requests != 3 || payload.Rows[0].Tokens != 42 {
		t.Fatalf("unexpected row values: %+v", payload.Rows[0])
	}
}

func TestRunAuditWriterUsesInjectedAuditStore(t *testing.T) {
	store := &stubAuditUsageStore{}
	g := New(DefaultConfig(), nil, nil)
	g.SetAuditStore(store)

	rec := audit.InferenceAuditRecord{
		Timestamp:   time.Date(2026, 4, 14, 0, 0, 0, 0, time.UTC),
		RequestID:   "req-1",
		WorkspaceID: "ws_alpha",
		Model:       "model-1",
		Status:      "success",
		TokenCount:  9,
	}
	if err := g.enqueueAuditRecord(rec); err != nil {
		t.Fatalf("enqueueAuditRecord: %v", err)
	}
	close(g.auditCh)
	g.auditWg.Wait()

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.appended) != 1 {
		t.Fatalf("expected 1 appended audit record, got %d", len(store.appended))
	}
	if store.appended[0].RequestID != "req-1" {
		t.Fatalf("expected req-1, got %#v", store.appended[0])
	}
}

func TestAuditWriterRetriesBeforeAcknowledging(t *testing.T) {
	store := &stubAuditUsageStore{appendFailures: 2}
	g := New(DefaultConfig(), nil, nil)
	g.SetAuditStore(store)

	rec := audit.InferenceAuditRecord{RequestID: "req-retry", Model: "model-1", Status: "success"}
	if err := g.enqueueAuditRecord(rec); err != nil {
		t.Fatalf("enqueueAuditRecord: %v", err)
	}
	close(g.auditCh)
	g.auditWg.Wait()

	store.mu.Lock()
	defer store.mu.Unlock()
	if store.appendCalls != 3 || len(store.appended) != 1 {
		t.Fatalf("expected 3 attempts and one durable record, got calls=%d records=%d", store.appendCalls, len(store.appended))
	}
}

func TestAuditWriterSurfacesExhaustedRetries(t *testing.T) {
	store := &stubAuditUsageStore{appendFailures: 4}
	g := New(DefaultConfig(), nil, nil)
	g.SetAuditStore(store)

	rec := audit.InferenceAuditRecord{RequestID: "req-exhausted", Model: "model-1", Status: "success"}
	if err := g.enqueueAuditRecord(rec); err == nil {
		t.Fatal("expected exhausted retry error")
	}
	close(g.auditCh)
	g.auditWg.Wait()

	store.mu.Lock()
	defer store.mu.Unlock()
	if store.appendCalls != 3 || len(store.appended) != 0 {
		t.Fatalf("expected 3 attempts and no durable record, got calls=%d records=%d", store.appendCalls, len(store.appended))
	}
}

func TestEnqueueAuditRecordTimesOutWhenWriterUnavailable(t *testing.T) {
	store := &stubAuditUsageStore{}
	g := &Gateway{
		auditStore: store,
		auditCh:    make(chan auditWriteRequest),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := g.enqueueAuditRecordWithContext(ctx, audit.InferenceAuditRecord{RequestID: "req-timeout"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}

func TestEnqueueAuditRecordTimesOutWaitingForAcknowledgement(t *testing.T) {
	store := &stubAuditUsageStore{}
	g := &Gateway{
		auditStore: store,
		auditCh:    make(chan auditWriteRequest, 1),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := g.enqueueAuditRecordWithContext(ctx, audit.InferenceAuditRecord{RequestID: "req-timeout"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}
