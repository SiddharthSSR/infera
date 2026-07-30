package providers

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"
)

const millisecondsPerHour int64 = 60 * 60 * 1000

var ErrCostHistoryIncomplete = errors.New("shared cost history does not cover the requested window")

// InfrastructureCostSession is one immutable provider-billing interval. The
// start timestamp is part of its identity so retries are idempotent while a
// later restart of the same instance creates a distinct interval.
type InfrastructureCostSession struct {
	WorkspaceID          string
	InstanceID           string
	Provider             string
	GPUType              string
	PriceSnapshotVersion string
	PriceAmountNano      int64
	PriceCurrency        string
	PriceTimeUnit        string
	StartedAt            time.Time
	StoppedAt            *time.Time
}

type InfrastructureCostLedger interface {
	InfrastructureCostCoverageStart() (time.Time, error)
	EnsureInfrastructureCostSession(InfrastructureCostSession) error
	CloseInfrastructureCostSession(workspaceID, instanceID string, startedAt, stoppedAt time.Time) error
	ListInfrastructureCostSessions(workspaceID string, start, end time.Time) ([]InfrastructureCostSession, error)
}

func (m *Manager) SetInfrastructureCostLedger(ledger InfrastructureCostLedger) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.infrastructureCosts = ledger
}

func (m *Manager) infrastructureCostLedger() InfrastructureCostLedger {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.infrastructureCosts
}

// GetSharedCostSummary reconciles durable instance state with the shared cost
// session ledger, then computes UTC half-open day and month totals.
func (m *Manager) GetSharedCostSummary(workspaceID string, now time.Time) (*CostSummary, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, errors.New("workspace is required for cost summary")
	}
	ledger := m.infrastructureCostLedger()
	if ledger == nil {
		return nil, errors.New("shared infrastructure cost ledger is not configured")
	}
	coverageStart, err := ledger.InfrastructureCostCoverageStart()
	if err != nil {
		return nil, err
	}
	now = now.UTC()
	if now.IsZero() {
		return nil, errors.New("cost summary end time is required")
	}
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if monthStart.Before(coverageStart.UTC()) {
		return nil, ErrCostHistoryIncomplete
	}

	instances, err := m.costSummaryInstances(workspaceID)
	if err != nil {
		return nil, err
	}
	for _, instance := range instances {
		if err := reconcileInfrastructureCostInstance(ledger, instance, coverageStart, now); err != nil {
			return nil, err
		}
	}

	sessions, err := ledger.ListInfrastructureCostSessions(workspaceID, monthStart, now)
	if err != nil {
		return nil, err
	}
	summary := &CostSummary{ByProvider: map[string]float64{}, ByGPU: map[string]float64{}}
	for _, instance := range instances {
		if instance == nil || !instanceAccruesHourlyCost(instance.Status) {
			continue
		}
		if !validHourlyPrice(instance.CostPerHour) {
			return nil, fmt.Errorf("instance %q has invalid hourly cost", instance.ID)
		}
		nextHourly := summary.CurrentHourly + instance.CostPerHour
		if math.IsInf(nextHourly, 0) || math.IsNaN(nextHourly) {
			return nil, errors.New("aggregate hourly cost is invalid")
		}
		summary.CurrentHourly = nextHourly
		summary.ByProvider[string(instance.Provider)] += instance.CostPerHour
		summary.ByGPU[string(instance.GPUType)] += instance.CostPerHour
		if math.IsInf(summary.ByProvider[string(instance.Provider)], 0) ||
			math.IsNaN(summary.ByProvider[string(instance.Provider)]) ||
			math.IsInf(summary.ByGPU[string(instance.GPUType)], 0) ||
			math.IsNaN(summary.ByGPU[string(instance.GPUType)]) {
			return nil, errors.New("aggregate hourly cost breakdown is invalid")
		}
	}
	var monthCostNano, todayCostNano int64
	for _, session := range sessions {
		monthCost, err := infrastructureSessionCostNano(session, monthStart, now)
		if err != nil {
			return nil, err
		}
		if monthCost < 0 || monthCostNano > math.MaxInt64-monthCost {
			return nil, errors.New("monthly cost overflow")
		}
		monthCostNano += monthCost
		todayCost, err := infrastructureSessionCostNano(session, todayStart, now)
		if err != nil {
			return nil, err
		}
		if todayCost < 0 || todayCostNano > math.MaxInt64-todayCost {
			return nil, errors.New("daily cost overflow")
		}
		todayCostNano += todayCost
	}
	summary.MonthTotal = float64(monthCostNano) / 1_000_000_000
	summary.TodayTotal = float64(todayCostNano) / 1_000_000_000
	daysInMonth := float64(time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day())
	summary.ProjectedMonth = (summary.MonthTotal / float64(now.Day())) * daysInMonth
	return summary, nil
}

func (m *Manager) costSummaryInstances(workspaceID string) ([]*Instance, error) {
	return m.instances.listByWorkspace(strings.TrimSpace(workspaceID))
}

func (m *Manager) syncInfrastructureCostByID(instanceID string) error {
	ledger := m.infrastructureCostLedger()
	if ledger == nil {
		return nil
	}
	instance, found, err := m.instances.get(instanceID)
	if err != nil || !found {
		return err
	}
	return m.syncInfrastructureCostInstance(instance)
}

func (m *Manager) syncInfrastructureCostInstance(instance *Instance) error {
	ledger := m.infrastructureCostLedger()
	if ledger == nil {
		return nil
	}
	coverageStart, err := ledger.InfrastructureCostCoverageStart()
	if err != nil {
		return err
	}
	return reconcileInfrastructureCostInstance(ledger, instance, coverageStart, m.now().UTC())
}

func reconcileInfrastructureCostInstance(ledger InfrastructureCostLedger, instance *Instance, coverageStart, now time.Time) error {
	if instance == nil {
		return nil
	}
	startedAt := instance.CreatedAt.UTC()
	if instance.StartedAt != nil {
		startedAt = instance.StartedAt.UTC()
	}
	if startedAt.Before(coverageStart) {
		startedAt = coverageStart.UTC()
	}
	var stoppedAt *time.Time
	if !instanceAccruesHourlyCost(instance.Status) {
		if instance.StoppedAt == nil || !instance.StoppedAt.After(startedAt) {
			return nil
		}
		stopped := instance.StoppedAt.UTC()
		stoppedAt = &stopped
	}
	if !startedAt.Before(now) || (stoppedAt != nil && !startedAt.Before(*stoppedAt)) {
		return nil
	}
	if !validHourlyPrice(instance.CostPerHour) {
		return fmt.Errorf("instance %q has invalid hourly cost", instance.ID)
	}
	session := InfrastructureCostSession{
		WorkspaceID: instance.WorkspaceID, InstanceID: instance.ID,
		Provider: string(instance.Provider), GPUType: string(instance.GPUType),
		PriceSnapshotVersion: PriceSnapshotVersionV1,
		PriceAmountNano:      int64(math.Round(instance.CostPerHour * 1_000_000_000)),
		PriceCurrency:        PriceCurrencyUSD, PriceTimeUnit: PriceTimeUnitHour,
		StartedAt: startedAt,
	}
	if err := ledger.EnsureInfrastructureCostSession(session); err != nil {
		return err
	}
	if stoppedAt != nil {
		return ledger.CloseInfrastructureCostSession(instance.WorkspaceID, instance.ID, startedAt, *stoppedAt)
	}
	return nil
}

func infrastructureSessionCostNano(session InfrastructureCostSession, start, end time.Time) (int64, error) {
	if session.PriceSnapshotVersion != PriceSnapshotVersionV1 ||
		session.PriceCurrency != PriceCurrencyUSD ||
		session.PriceTimeUnit != PriceTimeUnitHour ||
		session.PriceAmountNano <= 0 ||
		session.StartedAt.IsZero() ||
		(session.StoppedAt != nil && session.StoppedAt.Before(session.StartedAt)) {
		return 0, errors.New("invalid infrastructure cost session")
	}
	overlapStart := session.StartedAt.UTC()
	if overlapStart.Before(start.UTC()) {
		overlapStart = start.UTC()
	}
	overlapEnd := end.UTC()
	if session.StoppedAt != nil && session.StoppedAt.UTC().Before(overlapEnd) {
		overlapEnd = session.StoppedAt.UTC()
	}
	if !overlapStart.Before(overlapEnd) {
		return 0, nil
	}
	durationMS := overlapEnd.UnixMilli() - overlapStart.UnixMilli()
	numerator := new(big.Int).Mul(big.NewInt(session.PriceAmountNano), big.NewInt(durationMS))
	denominator := big.NewInt(millisecondsPerHour)
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, denominator, remainder)
	if new(big.Int).Lsh(remainder, 1).Cmp(denominator) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() {
		return 0, errors.New("infrastructure cost overflow")
	}
	return quotient.Int64(), nil
}
