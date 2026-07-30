package audit

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/infera/infera/go/internal/providers"
)

func TestSQLiteInfrastructureCostConcurrentConflictsFailClosed(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "audit.db")
	storeA, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore A: %v", err)
	}
	t.Cleanup(func() { _ = storeA.Close() })
	storeB, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore B: %v", err)
	}
	t.Cleanup(func() { _ = storeB.Close() })

	coverageStart, err := storeA.InfrastructureCostCoverageStart()
	if err != nil {
		t.Fatalf("coverage start: %v", err)
	}
	baseStart := coverageStart.Add(time.Hour).Truncate(time.Millisecond)

	t.Run("conflicting starts", func(t *testing.T) {
		for iteration := 0; iteration < 20; iteration++ {
			sessionA := testInfrastructureCostSession(
				fmt.Sprintf("ws_concurrent_start_%d", iteration), "instance",
				baseStart.Add(time.Duration(iteration)*time.Hour),
			)
			sessionB := sessionA
			sessionB.PriceAmountNano++

			errA, errB := runConcurrentCostMutations(
				func() error { return storeA.EnsureInfrastructureCostSession(sessionA) },
				func() error { return storeB.EnsureInfrastructureCostSession(sessionB) },
			)
			assertExactlyOneConcurrentCostWinner(t, errA, errB)

			sessions, err := storeA.ListInfrastructureCostSessions(
				sessionA.WorkspaceID, sessionA.StartedAt, sessionA.StartedAt.Add(time.Hour),
			)
			if err != nil {
				t.Fatalf("iteration %d list durable start winner: %v", iteration, err)
			}
			if len(sessions) != 1 {
				t.Fatalf("iteration %d durable start winners=%d", iteration, len(sessions))
			}
			expectedPrice := sessionA.PriceAmountNano
			if errB == nil {
				expectedPrice = sessionB.PriceAmountNano
			}
			if sessions[0].PriceAmountNano != expectedPrice {
				t.Fatalf(
					"iteration %d durable start does not match successful writer: got=%d want=%d",
					iteration, sessions[0].PriceAmountNano, expectedPrice,
				)
			}
		}
	})

	t.Run("conflicting stops", func(t *testing.T) {
		for iteration := 0; iteration < 20; iteration++ {
			session := testInfrastructureCostSession(
				fmt.Sprintf("ws_concurrent_stop_%d", iteration), "instance",
				baseStart.Add(time.Duration(iteration)*time.Hour),
			)
			if err := storeA.EnsureInfrastructureCostSession(session); err != nil {
				t.Fatalf("iteration %d seed session: %v", iteration, err)
			}
			stopA := session.StartedAt.Add(30 * time.Minute)
			stopB := stopA.Add(time.Millisecond)
			errA, errB := runConcurrentCostMutations(
				func() error {
					return storeA.CloseInfrastructureCostSession(
						session.WorkspaceID, session.InstanceID, session.StartedAt, stopA,
					)
				},
				func() error {
					return storeB.CloseInfrastructureCostSession(
						session.WorkspaceID, session.InstanceID, session.StartedAt, stopB,
					)
				},
			)
			assertExactlyOneConcurrentCostWinner(t, errA, errB)

			sessions, err := storeA.ListInfrastructureCostSessions(
				session.WorkspaceID, session.StartedAt, stopB.Add(time.Hour),
			)
			if err != nil {
				t.Fatalf("iteration %d list durable stop winner: %v", iteration, err)
			}
			if len(sessions) != 1 || sessions[0].StoppedAt == nil {
				t.Fatalf("iteration %d unexpected durable stop winners: %+v", iteration, sessions)
			}
			expectedStop := stopA
			if errB == nil {
				expectedStop = stopB
			}
			if !sessions[0].StoppedAt.Equal(expectedStop) {
				t.Fatalf(
					"iteration %d durable stop does not match successful writer: got=%s want=%s",
					iteration, sessions[0].StoppedAt, expectedStop,
				)
			}
		}
	})
}

func runConcurrentCostMutations(mutationA, mutationB func() error) (error, error) {
	start := make(chan struct{})
	var ready sync.WaitGroup
	ready.Add(2)
	results := make(chan struct {
		writer int
		err    error
	}, 2)
	run := func(writer int, mutation func() error) {
		ready.Done()
		<-start
		results <- struct {
			writer int
			err    error
		}{writer: writer, err: mutation()}
	}
	go run(0, mutationA)
	go run(1, mutationB)
	ready.Wait()
	close(start)

	var errs [2]error
	for range 2 {
		result := <-results
		errs[result.writer] = result.err
	}
	return errs[0], errs[1]
}

func assertExactlyOneConcurrentCostWinner(t *testing.T, errA, errB error) {
	t.Helper()
	if errA == nil && errB == nil {
		t.Fatal("both conflicting concurrent writes were accepted")
	}
	if errA != nil && errB != nil {
		t.Fatalf("no concurrent write committed: A=%v B=%v", errA, errB)
	}
}

func TestInfrastructureCostSessionFirstWriteWinsAndTenantWindows(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	coverageStart, err := store.InfrastructureCostCoverageStart()
	if err != nil {
		t.Fatalf("coverage start: %v", err)
	}
	startedAt := coverageStart.Add(time.Hour).Truncate(time.Millisecond)
	stoppedAt := startedAt.Add(30 * time.Minute)
	session := testInfrastructureCostSession("ws_alpha", "instance-a", startedAt)

	if err := store.EnsureInfrastructureCostSession(session); err != nil {
		t.Fatalf("ensure session: %v", err)
	}
	if err := store.EnsureInfrastructureCostSession(session); err != nil {
		t.Fatalf("identical ensure retry: %v", err)
	}
	conflict := session
	conflict.PriceAmountNano++
	if err := store.EnsureInfrastructureCostSession(conflict); err == nil {
		t.Fatal("conflicting price retry succeeded")
	}
	if err := store.CloseInfrastructureCostSession(
		session.WorkspaceID, session.InstanceID, session.StartedAt, stoppedAt,
	); err != nil {
		t.Fatalf("close session: %v", err)
	}
	if err := store.CloseInfrastructureCostSession(
		session.WorkspaceID, session.InstanceID, session.StartedAt, stoppedAt,
	); err != nil {
		t.Fatalf("identical close retry: %v", err)
	}
	if err := store.CloseInfrastructureCostSession(
		session.WorkspaceID, session.InstanceID, session.StartedAt, stoppedAt.Add(time.Millisecond),
	); err == nil {
		t.Fatal("conflicting stop retry succeeded")
	}
	if err := store.CloseInfrastructureCostSession(
		session.WorkspaceID, "missing", session.StartedAt, stoppedAt,
	); err == nil {
		t.Fatal("closing a missing session succeeded")
	}

	otherTenant := testInfrastructureCostSession("ws_beta", "instance-b", startedAt)
	if err := store.EnsureInfrastructureCostSession(otherTenant); err != nil {
		t.Fatalf("ensure other-tenant session: %v", err)
	}
	if err := store.CloseInfrastructureCostSession(
		otherTenant.WorkspaceID, otherTenant.InstanceID, otherTenant.StartedAt, stoppedAt,
	); err != nil {
		t.Fatalf("close other-tenant session: %v", err)
	}

	sessions, err := store.ListInfrastructureCostSessions("ws_alpha", startedAt, stoppedAt)
	if err != nil {
		t.Fatalf("list workspace sessions: %v", err)
	}
	if len(sessions) != 1 || sessions[0].WorkspaceID != "ws_alpha" ||
		sessions[0].StoppedAt == nil || !sessions[0].StoppedAt.Equal(stoppedAt) {
		t.Fatalf("unexpected workspace sessions: %+v", sessions)
	}
	atEnd, err := store.ListInfrastructureCostSessions("ws_alpha", stoppedAt, stoppedAt.Add(time.Hour))
	if err != nil {
		t.Fatalf("list half-open end: %v", err)
	}
	if len(atEnd) != 0 {
		t.Fatalf("session ending at window start was included: %+v", atEnd)
	}
	if _, err := store.ListInfrastructureCostSessions("", startedAt, stoppedAt); err == nil {
		t.Fatal("unscoped cost-session query succeeded")
	}
}

func TestInfrastructureCostSessionRejectsInvalidDurableState(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	coverageStart, err := store.InfrastructureCostCoverageStart()
	if err != nil {
		t.Fatalf("coverage start: %v", err)
	}
	session := testInfrastructureCostSession("ws_alpha", "instance-a", coverageStart.Add(time.Hour))
	stoppedAt := session.StartedAt.Add(time.Hour)
	session.StoppedAt = &stoppedAt
	if err := store.EnsureInfrastructureCostSession(session); err == nil {
		t.Fatal("ensure accepted a caller-supplied stop time")
	}

	session.StoppedAt = nil
	if _, err := store.db.Exec(`
		INSERT INTO infrastructure_cost_sessions
			(workspace_id, instance_id, started_at_ms, stopped_at_ms, provider, gpu_type,
			 price_snapshot_version, price_amount_nano, price_currency, price_time_unit)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		session.WorkspaceID, session.InstanceID, session.StartedAt.UnixMilli(),
		session.StartedAt.Add(-time.Millisecond).UnixMilli(), session.Provider, session.GPUType,
		session.PriceSnapshotVersion, session.PriceAmountNano, session.PriceCurrency, session.PriceTimeUnit,
	); err != nil {
		t.Fatalf("seed corrupt session: %v", err)
	}
	_, err = store.ListInfrastructureCostSessions(
		session.WorkspaceID, session.StartedAt.Add(-time.Hour), session.StartedAt.Add(time.Hour),
	)
	if err == nil || !strings.Contains(err.Error(), "invalid durable infrastructure cost session") {
		t.Fatalf("expected invalid durable state error, got %v", err)
	}
}

func testInfrastructureCostSession(
	workspaceID, instanceID string, startedAt time.Time,
) providers.InfrastructureCostSession {
	return providers.InfrastructureCostSession{
		WorkspaceID: workspaceID, InstanceID: instanceID,
		Provider: string(providers.ProviderRunPod), GPUType: string(providers.GPUH100),
		PriceSnapshotVersion: providers.PriceSnapshotVersionV1,
		PriceAmountNano:      1_500_000_000,
		PriceCurrency:        providers.PriceCurrencyUSD,
		PriceTimeUnit:        providers.PriceTimeUnitHour,
		StartedAt:            startedAt,
	}
}
