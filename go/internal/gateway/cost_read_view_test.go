package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/infera/infera/go/internal/audit"
	"github.com/infera/infera/go/internal/auth"
	"github.com/infera/infera/go/internal/providers"
	"github.com/infera/infera/go/internal/providers/mock"
)

func TestCostReadViewIsReplicaConsistentAcrossUTCWindowsAndRetries(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "shared-audit.db")
	storeA := newSharedCostTestStore(t, dbPath)
	storeB := newSharedCostTestStore(t, dbPath)
	coverageStart, err := storeA.InfrastructureCostCoverageStart()
	if err != nil {
		t.Fatalf("read coverage start: %v", err)
	}
	monthStart := time.Date(coverageStart.Year(), coverageStart.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	now := monthStart.Add(25 * time.Hour)

	monthOnly := infrastructureCostSession("ws_alpha", "inst-month", monthStart.Add(23*time.Hour), 1_000_000_000)
	today := infrastructureCostSession("ws_alpha", "inst-today", monthStart.Add(24*time.Hour), 2_000_000_000)
	previousMonth := infrastructureCostSession("ws_alpha", "inst-old", monthStart.Add(-time.Hour), 9_000_000_000)
	otherWorkspace := infrastructureCostSession("ws_beta", "inst-other", monthStart.Add(24*time.Hour), 8_000_000_000)
	atEnd := infrastructureCostSession("ws_alpha", "inst-end", now, 6_000_000_000)
	for _, session := range []providers.InfrastructureCostSession{monthOnly, today, previousMonth, otherWorkspace, atEnd} {
		if err := storeA.EnsureInfrastructureCostSession(session); err != nil {
			t.Fatalf("seed %s: %v", session.InstanceID, err)
		}
	}
	closeCostSession(t, storeA, monthOnly, monthStart.Add(24*time.Hour))
	closeCostSession(t, storeA, today, monthStart.Add(24*time.Hour+30*time.Minute))
	closeCostSession(t, storeA, previousMonth, monthStart)
	closeCostSession(t, storeA, otherWorkspace, monthStart.Add(24*time.Hour+30*time.Minute))

	if err := storeB.EnsureInfrastructureCostSession(today); err != nil {
		t.Fatalf("identical start retry must be idempotent: %v", err)
	}
	if err := storeB.CloseInfrastructureCostSession(today.WorkspaceID, today.InstanceID, today.StartedAt, monthStart.Add(24*time.Hour+30*time.Minute)); err != nil {
		t.Fatalf("identical stop retry must be idempotent: %v", err)
	}
	conflict := today
	conflict.PriceAmountNano++
	if err := storeB.EnsureInfrastructureCostSession(conflict); err == nil {
		t.Fatal("conflicting retry must preserve the first write")
	}

	replicaA := newCostReadViewReplica(t, storeA, now)
	replicaB := newCostReadViewReplica(t, storeB, now)
	reproduceLegacyReplicaDisagreement(t)

	gotA := getWorkspaceCostSummary(t, replicaA, "ws_alpha")
	gotB := getWorkspaceCostSummary(t, replicaB, "ws_alpha")
	if gotA != gotB {
		t.Fatalf("replicas disagree: A=%+v B=%+v", gotA, gotB)
	}
	if gotA.CurrentHourly != 0 || gotA.TodayTotal != 1 || gotA.MonthTotal != 2 || gotA.ProjectedMonth != 31 {
		t.Fatalf("unexpected half-open UTC totals: %+v", gotA)
	}
}

func TestCostReadViewEmptyStateAndDefaultWorkspaceIsolation(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "shared-audit.db")
	storeA := newSharedCostTestStore(t, dbPath)
	storeB := newSharedCostTestStore(t, dbPath)
	coverageStart, err := storeA.InfrastructureCostCoverageStart()
	if err != nil {
		t.Fatalf("read coverage start: %v", err)
	}
	monthStart := time.Date(coverageStart.Year(), coverageStart.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	now := monthStart.Add(time.Hour)

	emptyA := getWorkspaceCostSummary(t, newCostReadViewReplica(t, storeA, now), "ws_empty")
	emptyB := getWorkspaceCostSummary(t, newCostReadViewReplica(t, storeB, now), "ws_empty")
	if emptyA != emptyB || emptyA != (costSummaryResponse{}) {
		t.Fatalf("unexpected empty summaries: A=%+v B=%+v", emptyA, emptyB)
	}

	defaultSession := infrastructureCostSession(auth.DefaultWorkspaceID, "inst-default", monthStart, 1_000_000_000)
	otherSession := infrastructureCostSession("ws_other", "inst-other", monthStart, 9_000_000_000)
	for _, session := range []providers.InfrastructureCostSession{defaultSession, otherSession} {
		if err := storeA.EnsureInfrastructureCostSession(session); err != nil {
			t.Fatalf("seed %s: %v", session.InstanceID, err)
		}
		closeCostSession(t, storeA, session, now)
	}
	got := getWorkspaceCostSummary(t, newCostReadViewReplica(t, storeA, now), auth.DefaultWorkspaceID)
	if got.TodayTotal != 1 || got.MonthTotal != 1 {
		t.Fatalf("default workspace leaked another tenant's costs: %+v", got)
	}
}

func TestCostReadViewFailsClosedOnConflictAndIncompleteHistory(t *testing.T) {
	store := newSharedCostTestStore(t, filepath.Join(t.TempDir(), "shared-audit.db"))
	coverageStart, err := store.InfrastructureCostCoverageStart()
	if err != nil {
		t.Fatalf("read coverage start: %v", err)
	}

	incomplete := newCostReadViewReplica(t, store, coverageStart.Add(time.Hour))
	assertCostError(t, incomplete, "ws_alpha", "cost_history_incomplete")

	now := time.Date(coverageStart.Year(), coverageStart.Month()+1, 1, 1, 0, 0, 0, time.UTC)
	manager, err := providers.NewManager(providers.ManagerConfig{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("create conflict manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	manager.RegisterProvider(mock.New())
	if _, err := manager.Provision(context.Background(), &providers.ProvisionRequest{
		Name: "conflict", Provider: providers.ProviderMock,
		WorkspaceID: "ws_alpha", GPUType: providers.GPURTX4090,
	}); err != nil {
		t.Fatalf("provision test instance: %v", err)
	}
	manager.SetInfrastructureCostLedger(&errorCostLedger{
		coverageStart: monthStartFor(now),
		ensureErr:     errors.New("conflicting durable cost session"),
	})
	h := NewInstanceHandlers(manager)
	h.now = func() time.Time { return now }
	assertCostError(t, h, "ws_alpha", "cost_state_unavailable")
}

type costSummaryResponse struct {
	CurrentHourly  float64 `json:"current_hourly"`
	TodayTotal     float64 `json:"today_total"`
	MonthTotal     float64 `json:"month_total"`
	ProjectedMonth float64 `json:"projected_month"`
}

func newSharedCostTestStore(t *testing.T, path string) *audit.Store {
	t.Helper()
	store, err := audit.NewStore(path)
	if err != nil {
		t.Fatalf("create shared cost store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func newCostReadViewReplica(t *testing.T, store *audit.Store, now time.Time) *InstanceHandlers {
	t.Helper()
	manager, err := providers.NewManager(providers.ManagerConfig{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	h := NewInstanceHandlers(manager)
	h.SetAuditStore(store)
	h.now = func() time.Time { return now }
	return h
}

func getWorkspaceCostSummary(t *testing.T, h *InstanceHandlers, workspaceID string) costSummaryResponse {
	t.Helper()
	req := authedWorkspaceRequest(httptest.NewRequest(http.MethodGet, "/api/costs", nil), auth.RoleBilling, workspaceID)
	rec := httptest.NewRecorder()
	h.handleCosts(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var response costSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode cost response: %v", err)
	}
	return response
}

func assertCostError(t *testing.T, h *InstanceHandlers, workspaceID, errorType string) {
	t.Helper()
	req := authedWorkspaceRequest(httptest.NewRequest(http.MethodGet, "/api/costs", nil), auth.RoleBilling, workspaceID)
	rec := httptest.NewRecorder()
	h.handleCosts(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", rec.Code, rec.Body.String())
	}
	var response map[string]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if response["error"]["type"] != errorType {
		t.Fatalf("unexpected error: %#v", response)
	}
}

func infrastructureCostSession(workspaceID, instanceID string, startedAt time.Time, priceNano int64) providers.InfrastructureCostSession {
	return providers.InfrastructureCostSession{
		WorkspaceID: workspaceID, InstanceID: instanceID,
		Provider: string(providers.ProviderRunPod), GPUType: string(providers.GPUH100),
		PriceSnapshotVersion: providers.PriceSnapshotVersionV1, PriceAmountNano: priceNano,
		PriceCurrency: providers.PriceCurrencyUSD, PriceTimeUnit: providers.PriceTimeUnitHour,
		StartedAt: startedAt,
	}
}

func reproduceLegacyReplicaDisagreement(t *testing.T) {
	t.Helper()
	replicaA, err := providers.NewManager(providers.ManagerConfig{DefaultProvider: providers.ProviderMock})
	if err != nil {
		t.Fatalf("create legacy replica A: %v", err)
	}
	t.Cleanup(func() { _ = replicaA.Close() })
	replicaB, err := providers.NewManager(providers.ManagerConfig{DefaultProvider: providers.ProviderMock})
	if err != nil {
		t.Fatalf("create legacy replica B: %v", err)
	}
	t.Cleanup(func() { _ = replicaB.Close() })
	replicaA.RegisterProvider(mock.New())
	instance, err := replicaA.Provision(context.Background(), &providers.ProvisionRequest{
		Name: "replica-local-history", Provider: providers.ProviderMock,
		WorkspaceID: "ws_alpha", GPUType: providers.GPURTX4090,
	})
	if err != nil {
		t.Fatalf("seed replica-local lifecycle: %v", err)
	}
	time.Sleep(time.Millisecond)
	if err := replicaA.Terminate(context.Background(), instance.ID); err != nil {
		t.Fatalf("close replica-local lifecycle: %v", err)
	}
	if replicaA.GetCostSummaryForWorkspace("ws_alpha").TodayTotal <= 0 ||
		replicaB.GetCostSummaryForWorkspace("ws_alpha").TodayTotal != 0 {
		t.Fatal("test setup did not reproduce divergent process-local accumulated totals")
	}
}

func closeCostSession(t *testing.T, store *audit.Store, session providers.InfrastructureCostSession, stoppedAt time.Time) {
	t.Helper()
	if err := store.CloseInfrastructureCostSession(session.WorkspaceID, session.InstanceID, session.StartedAt, stoppedAt); err != nil {
		t.Fatalf("close %s: %v", session.InstanceID, err)
	}
}

type errorCostLedger struct {
	coverageStart time.Time
	ensureErr     error
}

func (l *errorCostLedger) InfrastructureCostCoverageStart() (time.Time, error) {
	return l.coverageStart, nil
}

func (l *errorCostLedger) EnsureInfrastructureCostSession(providers.InfrastructureCostSession) error {
	return l.ensureErr
}

func (l *errorCostLedger) CloseInfrastructureCostSession(string, string, time.Time, time.Time) error {
	return nil
}

func (l *errorCostLedger) ListInfrastructureCostSessions(string, time.Time, time.Time) ([]providers.InfrastructureCostSession, error) {
	return nil, nil
}

func monthStartFor(now time.Time) time.Time {
	now = now.UTC()
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
}
