package audit

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/infera/infera/go/internal/providers"
)

func (s *Store) InfrastructureCostCoverageStart() (time.Time, error) {
	var value string
	if err := s.db.QueryRow(`SELECT value FROM infrastructure_cost_metadata WHERE key = 'coverage_start_ms'`).Scan(&value); err != nil {
		return time.Time{}, err
	}
	milliseconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || milliseconds <= 0 {
		return time.Time{}, errors.New("infrastructure cost coverage metadata is invalid")
	}
	return time.UnixMilli(milliseconds).UTC(), nil
}

func (s *Store) EnsureInfrastructureCostSession(session providers.InfrastructureCostSession) error {
	if session.StoppedAt != nil {
		return errors.New("cost session must be closed with its first stop time")
	}
	session, err := normalizeInfrastructureCostSession(session)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.lockExecution(tx, session.WorkspaceID, "infrastructure-cost:"+session.InstanceID); err != nil {
		return err
	}

	var existing providers.InfrastructureCostSession
	var existingStartedAtMS int64
	err = tx.QueryRow(s.bind(`
		SELECT workspace_id, instance_id, provider, gpu_type, price_snapshot_version,
		       price_amount_nano, price_currency, price_time_unit, started_at_ms
		FROM infrastructure_cost_sessions
		WHERE workspace_id = ? AND instance_id = ? AND started_at_ms = ?`),
		session.WorkspaceID, session.InstanceID, session.StartedAt.UnixMilli(),
	).Scan(
		&existing.WorkspaceID, &existing.InstanceID, &existing.Provider, &existing.GPUType,
		&existing.PriceSnapshotVersion, &existing.PriceAmountNano, &existing.PriceCurrency,
		&existing.PriceTimeUnit, &existingStartedAtMS,
	)
	switch {
	case err == nil:
		existing.StartedAt = time.UnixMilli(existingStartedAtMS).UTC()
		if infrastructureCostSessionIdentity(existing) != infrastructureCostSessionIdentity(session) {
			return fmt.Errorf("cost session identity conflicts for instance %q in workspace %q", session.InstanceID, session.WorkspaceID)
		}
		return tx.Commit()
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}

	existing = providers.InfrastructureCostSession{}
	existingStartedAtMS = 0
	err = tx.QueryRow(s.bind(`
		SELECT workspace_id, instance_id, provider, gpu_type, price_snapshot_version,
		       price_amount_nano, price_currency, price_time_unit, started_at_ms
		FROM infrastructure_cost_sessions
		WHERE workspace_id = ? AND instance_id = ? AND stopped_at_ms IS NULL`),
		session.WorkspaceID, session.InstanceID,
	).Scan(
		&existing.WorkspaceID, &existing.InstanceID, &existing.Provider, &existing.GPUType,
		&existing.PriceSnapshotVersion, &existing.PriceAmountNano, &existing.PriceCurrency,
		&existing.PriceTimeUnit, &existingStartedAtMS,
	)
	switch {
	case err == nil:
		existing.StartedAt = time.UnixMilli(existingStartedAtMS).UTC()
		if infrastructureCostSessionIdentity(existing) != infrastructureCostSessionIdentity(session) {
			return fmt.Errorf("instance %q already has a conflicting open cost session in workspace %q", session.InstanceID, session.WorkspaceID)
		}
		return tx.Commit()
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}

	result, err := tx.Exec(s.bind(`
		INSERT INTO infrastructure_cost_sessions
			(workspace_id, instance_id, started_at_ms, provider, gpu_type,
			 price_snapshot_version, price_amount_nano, price_currency, price_time_unit)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(workspace_id, instance_id, started_at_ms) DO NOTHING`),
		session.WorkspaceID, session.InstanceID, session.StartedAt.UnixMilli(),
		session.Provider, session.GPUType, session.PriceSnapshotVersion,
		session.PriceAmountNano, session.PriceCurrency, session.PriceTimeUnit,
	)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		if err := s.verifyInfrastructureCostSessionIdentity(tx, session); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) CloseInfrastructureCostSession(workspaceID, instanceID string, startedAt, stoppedAt time.Time) error {
	workspaceID = strings.TrimSpace(workspaceID)
	instanceID = strings.TrimSpace(instanceID)
	startedAt = startedAt.UTC()
	stoppedAt = stoppedAt.UTC()
	if workspaceID == "" || instanceID == "" || startedAt.IsZero() || stoppedAt.IsZero() {
		return errors.New("workspace, instance, start time, and stop time are required")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.lockExecution(tx, workspaceID, "infrastructure-cost:"+instanceID); err != nil {
		return err
	}
	var startedAtMS int64
	err = tx.QueryRow(s.bind(`
		SELECT started_at_ms FROM infrastructure_cost_sessions
		WHERE workspace_id = ? AND instance_id = ? AND started_at_ms = ? AND stopped_at_ms IS NULL`),
		workspaceID, instanceID, startedAt.UnixMilli(),
	).Scan(&startedAtMS)
	if errors.Is(err, sql.ErrNoRows) {
		if err := s.verifyInfrastructureCostSessionStop(
			tx, workspaceID, instanceID, startedAt.UnixMilli(), stoppedAt.UnixMilli(),
		); err != nil {
			return err
		}
		return tx.Commit()
	}
	if err != nil {
		return err
	}
	if stoppedAt.UnixMilli() < startedAtMS {
		return errors.New("cost session stop time must not be before its start")
	}
	result, err := tx.Exec(s.bind(`
		UPDATE infrastructure_cost_sessions SET stopped_at_ms = ?
		WHERE workspace_id = ? AND instance_id = ? AND started_at_ms = ? AND stopped_at_ms IS NULL`),
		stoppedAt.UnixMilli(), workspaceID, instanceID, startedAt.UnixMilli(),
	)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		if err := s.verifyInfrastructureCostSessionStop(
			tx, workspaceID, instanceID, startedAt.UnixMilli(), stoppedAt.UnixMilli(),
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) verifyInfrastructureCostSessionIdentity(
	tx *sql.Tx, expected providers.InfrastructureCostSession,
) error {
	var durable providers.InfrastructureCostSession
	var startedAtMS int64
	err := tx.QueryRow(s.bind(`
		SELECT workspace_id, instance_id, provider, gpu_type, price_snapshot_version,
		       price_amount_nano, price_currency, price_time_unit, started_at_ms
		FROM infrastructure_cost_sessions
		WHERE workspace_id = ? AND instance_id = ? AND started_at_ms = ?`),
		expected.WorkspaceID, expected.InstanceID, expected.StartedAt.UnixMilli(),
	).Scan(
		&durable.WorkspaceID, &durable.InstanceID, &durable.Provider, &durable.GPUType,
		&durable.PriceSnapshotVersion, &durable.PriceAmountNano, &durable.PriceCurrency,
		&durable.PriceTimeUnit, &startedAtMS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf(
			"cost session insert lost a race without a durable winner for instance %q in workspace %q",
			expected.InstanceID, expected.WorkspaceID,
		)
	}
	if err != nil {
		return err
	}
	durable.StartedAt = time.UnixMilli(startedAtMS).UTC()
	if infrastructureCostSessionIdentity(durable) != infrastructureCostSessionIdentity(expected) {
		return fmt.Errorf(
			"cost session identity conflicts for instance %q in workspace %q",
			expected.InstanceID, expected.WorkspaceID,
		)
	}
	return nil
}

func (s *Store) verifyInfrastructureCostSessionStop(
	tx *sql.Tx, workspaceID, instanceID string, startedAtMS, expectedStopMS int64,
) error {
	var durableStop sql.NullInt64
	err := tx.QueryRow(s.bind(`
		SELECT stopped_at_ms FROM infrastructure_cost_sessions
		WHERE workspace_id = ? AND instance_id = ? AND started_at_ms = ?`),
		workspaceID, instanceID, startedAtMS,
	).Scan(&durableStop)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("cost session for instance %q in workspace %q does not exist", instanceID, workspaceID)
	}
	if err != nil {
		return err
	}
	if !durableStop.Valid {
		return fmt.Errorf(
			"cost session stop lost a race without a durable winner for instance %q in workspace %q",
			instanceID, workspaceID,
		)
	}
	if durableStop.Int64 != expectedStopMS {
		return fmt.Errorf(
			"instance %q already has a conflicting cost-session stop time in workspace %q",
			instanceID, workspaceID,
		)
	}
	return nil
}

func (s *Store) ListInfrastructureCostSessions(workspaceID string, start, end time.Time) ([]providers.InfrastructureCostSession, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	start = start.UTC()
	end = end.UTC()
	if workspaceID == "" || start.IsZero() || !start.Before(end) {
		return nil, errors.New("workspace and valid infrastructure cost window are required")
	}
	query := `
		SELECT workspace_id, instance_id, provider, gpu_type, price_snapshot_version,
		       price_amount_nano, price_currency, price_time_unit, started_at_ms, stopped_at_ms
		FROM infrastructure_cost_sessions
		WHERE started_at_ms < ? AND (stopped_at_ms IS NULL OR stopped_at_ms > ?)`
	query += " AND workspace_id = ?"
	args := []any{end.UnixMilli(), start.UnixMilli(), workspaceID}
	query += " ORDER BY workspace_id, instance_id, started_at_ms"
	rows, err := s.db.Query(s.bind(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []providers.InfrastructureCostSession
	for rows.Next() {
		var session providers.InfrastructureCostSession
		var startedAtMS int64
		var stoppedAtMS sql.NullInt64
		if err := rows.Scan(
			&session.WorkspaceID, &session.InstanceID, &session.Provider, &session.GPUType,
			&session.PriceSnapshotVersion, &session.PriceAmountNano, &session.PriceCurrency,
			&session.PriceTimeUnit, &startedAtMS, &stoppedAtMS,
		); err != nil {
			return nil, err
		}
		session.StartedAt = time.UnixMilli(startedAtMS).UTC()
		if stoppedAtMS.Valid {
			stoppedAt := time.UnixMilli(stoppedAtMS.Int64).UTC()
			session.StoppedAt = &stoppedAt
		}
		session, err = normalizeInfrastructureCostSession(session)
		if err != nil {
			return nil, fmt.Errorf("invalid durable infrastructure cost session: %w", err)
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

func normalizeInfrastructureCostSession(session providers.InfrastructureCostSession) (providers.InfrastructureCostSession, error) {
	session.WorkspaceID = strings.TrimSpace(session.WorkspaceID)
	session.InstanceID = strings.TrimSpace(session.InstanceID)
	session.Provider = strings.TrimSpace(session.Provider)
	session.GPUType = strings.TrimSpace(session.GPUType)
	session.PriceSnapshotVersion = strings.TrimSpace(session.PriceSnapshotVersion)
	session.PriceCurrency = strings.TrimSpace(session.PriceCurrency)
	session.PriceTimeUnit = strings.TrimSpace(session.PriceTimeUnit)
	session.StartedAt = session.StartedAt.UTC()
	if session.WorkspaceID == "" || session.InstanceID == "" || session.Provider == "" || session.GPUType == "" ||
		session.PriceSnapshotVersion != providers.PriceSnapshotVersionV1 ||
		session.PriceCurrency != providers.PriceCurrencyUSD || session.PriceTimeUnit != providers.PriceTimeUnitHour ||
		session.PriceAmountNano <= 0 || session.StartedAt.IsZero() {
		return providers.InfrastructureCostSession{}, errors.New("invalid infrastructure cost session")
	}
	if session.StoppedAt != nil {
		stoppedAt := session.StoppedAt.UTC()
		if stoppedAt.Before(session.StartedAt) {
			return providers.InfrastructureCostSession{}, errors.New("invalid infrastructure cost session stop time")
		}
		session.StoppedAt = &stoppedAt
	}
	return session, nil
}

func infrastructureCostSessionIdentity(session providers.InfrastructureCostSession) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%d\x00%s\x00%s\x00%d",
		session.WorkspaceID, session.InstanceID, session.Provider, session.GPUType,
		session.PriceSnapshotVersion, session.PriceAmountNano, session.PriceCurrency,
		session.PriceTimeUnit, session.StartedAt.UnixMilli())
}
