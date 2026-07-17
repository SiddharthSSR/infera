package registry

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/infera/infera/go/pkg/types"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	workerRegistrySchemaVersion       = "1"
	workerRegistryMigrationLock       = int64(4242424701)
	defaultRegistryQueryTimeout       = 5 * time.Second
	defaultRegistryMaxOpenConnections = 20
	defaultRegistryMaxIdleConnections = 5
	defaultRegistryConnMaxLifetime    = 30 * time.Minute
)

var (
	// ErrWorkerRegistryUnavailable distinguishes a durable backend failure from
	// a genuinely absent worker registration.
	ErrWorkerRegistryUnavailable = errors.New("worker registry unavailable")
	// ErrWorkerRegistrationConflict means the authenticated managed instance
	// cannot own or replace the requested worker registration.
	ErrWorkerRegistrationConflict = errors.New("worker registration conflict")
)

// PostgresRegistryConfig bounds durable registry work and configures health
// transitions. Query contexts always derive from the caller context.
type PostgresRegistryConfig struct {
	RegistryConfig
	QueryTimeout    time.Duration
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// PostgresRegistry stores worker registrations in the shared control-state
// PostgreSQL database so every gateway replica sees the same routing state.
type PostgresRegistry struct {
	db        *sql.DB
	config    PostgresRegistryConfig
	callbackM sync.RWMutex
	callback  func(HealthTransition)
}

// NewPostgresRegistry opens and migrates a durable worker registry.
// The managed_instances table must already have been migrated on this DSN.
func NewPostgresRegistry(dsn string, config PostgresRegistryConfig) (*PostgresRegistry, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("postgres worker registry DSN is required")
	}
	config = normalizePostgresRegistryConfig(config)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(config.MaxOpenConns)
	db.SetMaxIdleConns(config.MaxIdleConns)
	db.SetConnMaxLifetime(config.ConnMaxLifetime)
	registry := &PostgresRegistry{db: db, config: config}
	ctx, cancel := registry.operationContext(context.Background())
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := registry.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return registry, nil
}

func normalizePostgresRegistryConfig(config PostgresRegistryConfig) PostgresRegistryConfig {
	defaults := DefaultRegistryConfig()
	if config.HealthCheckInterval <= 0 {
		config.HealthCheckInterval = defaults.HealthCheckInterval
	}
	if config.UnhealthyThreshold <= 0 {
		config.UnhealthyThreshold = defaults.UnhealthyThreshold
	}
	if config.RemovalThreshold <= config.UnhealthyThreshold {
		config.RemovalThreshold = defaults.RemovalThreshold
		if config.RemovalThreshold <= config.UnhealthyThreshold {
			config.RemovalThreshold = config.UnhealthyThreshold * 3
		}
	}
	if config.QueryTimeout <= 0 {
		config.QueryTimeout = defaultRegistryQueryTimeout
	}
	if config.MaxOpenConns <= 0 {
		config.MaxOpenConns = defaultRegistryMaxOpenConnections
	}
	if config.MaxIdleConns < 0 {
		config.MaxIdleConns = 0
	} else if config.MaxIdleConns == 0 {
		config.MaxIdleConns = defaultRegistryMaxIdleConnections
	}
	if config.MaxIdleConns > config.MaxOpenConns {
		config.MaxIdleConns = config.MaxOpenConns
	}
	if config.ConnMaxLifetime <= 0 {
		config.ConnMaxLifetime = defaultRegistryConnMaxLifetime
	}
	return config
}

func (r *PostgresRegistry) operationContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, r.config.QueryTimeout)
}

func (r *PostgresRegistry) migrate(parent context.Context) error {
	ctx, cancel := r.operationContext(parent)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return registryUnavailable(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, workerRegistryMigrationLock); err != nil {
		return registryUnavailable(err)
	}
	var managedInstancesTable sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT to_regclass('managed_instances')::text`).Scan(&managedInstancesTable); err != nil {
		return registryUnavailable(err)
	}
	if !managedInstancesTable.Valid {
		return errors.New("managed_instances must be migrated before the worker registry")
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS worker_registrations (
			worker_id TEXT PRIMARY KEY,
			instance_id TEXT NOT NULL UNIQUE REFERENCES managed_instances(id) ON DELETE CASCADE,
			workspace_id TEXT NOT NULL,
			shared_pool BOOLEAN NOT NULL DEFAULT FALSE,
			address TEXT NOT NULL,
			status TEXT NOT NULL CHECK (status IN ('healthy', 'degraded', 'unhealthy', 'draining', 'offline')),
			loaded_models JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(loaded_models) = 'array'),
			stats JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(stats) = 'object'),
			tags JSONB NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(tags) = 'object'),
			registration_id TEXT NOT NULL UNIQUE,
			registration_generation BIGINT NOT NULL DEFAULT 1 CHECK (registration_generation > 0),
			last_health_check TIMESTAMPTZ NOT NULL,
			registered_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_worker_registrations_workspace
			ON worker_registrations(workspace_id);
		CREATE INDEX IF NOT EXISTS idx_worker_registrations_heartbeat
			ON worker_registrations(last_health_check);
	`); err != nil {
		return registryUnavailable(err)
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE control_state_metadata SET value = $1
		WHERE key = 'worker_registry_schema' AND value = $1`, workerRegistrySchemaVersion)
	if err != nil {
		return registryUnavailable(err)
	}
	if rows, err := result.RowsAffected(); err != nil {
		return registryUnavailable(err)
	} else if rows == 0 {
		var existing string
		err := tx.QueryRowContext(ctx, `SELECT value FROM control_state_metadata WHERE key = 'worker_registry_schema'`).Scan(&existing)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if _, err := tx.ExecContext(ctx, `INSERT INTO control_state_metadata (key, value) VALUES ('worker_registry_schema', $1)`, workerRegistrySchemaVersion); err != nil {
				return registryUnavailable(err)
			}
		case err != nil:
			return registryUnavailable(err)
		case existing != workerRegistrySchemaVersion:
			return fmt.Errorf("worker registry schema version %q is incompatible with this gateway", existing)
		}
	}
	if err := tx.Commit(); err != nil {
		return registryUnavailable(err)
	}
	return nil
}

// Close closes this registry's connection pool.
func (r *PostgresRegistry) Close() error { return r.db.Close() }

// OnHealthTransition sets the callback for transitions won by this replica.
func (r *PostgresRegistry) OnHealthTransition(callback func(HealthTransition)) {
	r.callbackM.Lock()
	r.callback = callback
	r.callbackM.Unlock()
}

// Register inserts or replaces a registration owned by the same active,
// already-bound managed instance. Every success gets a fresh opaque identity.
func (r *PostgresRegistry) Register(parent context.Context, worker *types.WorkerInfo) error {
	if worker == nil || strings.TrimSpace(worker.WorkerID) == "" || strings.TrimSpace(worker.InstanceID) == "" {
		return fmt.Errorf("%w: worker ID and instance ID are required", ErrWorkerRegistrationConflict)
	}
	if strings.TrimSpace(worker.Address) == "" {
		return errors.New("worker address is required")
	}
	if !validWorkerStatus(worker.Status) {
		return fmt.Errorf("invalid worker status %q", worker.Status)
	}
	models, stats, tags, err := encodeWorkerPayload(worker.LoadedModels, worker.Stats, worker.Tags)
	if err != nil {
		return err
	}
	registrationID := uuid.NewString()
	ctx, cancel := r.operationContext(parent)
	defer cancel()
	stored, err := r.scanWorker(r.db.QueryRowContext(ctx, `
		WITH owner AS MATERIALIZED (
			SELECT managed.id, managed.workspace_id
			FROM managed_instances AS managed
			WHERE managed.id = $2
			  AND managed.worker_id = $1
			  AND managed.workspace_id = $3
			  AND managed.state_json->>'status' IN ('pending', 'provisioning', 'running')
		), retired AS (
			DELETE FROM worker_registrations AS previous
			USING owner
			WHERE previous.instance_id = owner.id
			  AND previous.worker_id <> $1
		)
		INSERT INTO worker_registrations AS registration
			(worker_id, instance_id, workspace_id, shared_pool, address, status,
			 loaded_models, stats, tags, registration_id, registration_generation,
			 last_health_check, registered_at, updated_at)
		SELECT $1, owner.id, owner.workspace_id, $4, $5, $6,
			$7, $8, $9, $10, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
		FROM owner
		ON CONFLICT (worker_id) DO UPDATE SET
			workspace_id = EXCLUDED.workspace_id,
			shared_pool = EXCLUDED.shared_pool,
			address = EXCLUDED.address,
			status = EXCLUDED.status,
			loaded_models = EXCLUDED.loaded_models,
			stats = EXCLUDED.stats,
			tags = EXCLUDED.tags,
			registration_id = EXCLUDED.registration_id,
			registration_generation = registration.registration_generation + 1,
			last_health_check = CURRENT_TIMESTAMP,
			registered_at = CURRENT_TIMESTAMP,
			updated_at = CURRENT_TIMESTAMP
		WHERE registration.instance_id = EXCLUDED.instance_id
		RETURNING `+workerRegistrationColumns,
		worker.WorkerID, worker.InstanceID, worker.WorkspaceID, worker.SharedPool,
		worker.Address, string(worker.Status), models, stats, tags, registrationID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: managed instance is inactive, mismatched, or owned by another worker", ErrWorkerRegistrationConflict)
	}
	if err != nil {
		return registryUnavailable(err)
	}
	copyWorkerRegistration(worker, stored)
	return nil
}

// Deregister deletes a worker registration.
func (r *PostgresRegistry) Deregister(parent context.Context, workerID string) error {
	ctx, cancel := r.operationContext(parent)
	defer cancel()
	result, err := r.db.ExecContext(ctx, `DELETE FROM worker_registrations WHERE worker_id = $1`, strings.TrimSpace(workerID))
	if err != nil {
		return registryUnavailable(err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return registryUnavailable(err)
	}
	if rows == 0 {
		return fmt.Errorf("%w: %s", ErrWorkerNotFound, workerID)
	}
	return nil
}

// UpdateWorkerStats updates statistics without changing models.
func (r *PostgresRegistry) UpdateWorkerStats(parent context.Context, workerID string, stats types.WorkerStats) error {
	payload, err := json.Marshal(stats)
	if err != nil {
		return err
	}
	return r.updateActiveWorker(parent, workerID, "stats = $2", payload, true)
}

// UpdateWorkerModels replaces the complete model set without changing stats.
func (r *PostgresRegistry) UpdateWorkerModels(parent context.Context, workerID string, models []types.LoadedModel) error {
	if models == nil {
		models = []types.LoadedModel{}
	}
	payload, err := json.Marshal(models)
	if err != nil {
		return err
	}
	return r.updateActiveWorker(parent, workerID, "loaded_models = $2", payload, false)
}

func (r *PostgresRegistry) updateActiveWorker(parent context.Context, workerID, assignment string, payload []byte, touchHealth bool) error {
	ctx, cancel := r.operationContext(parent)
	defer cancel()
	healthAssignment := ""
	if touchHealth {
		healthAssignment = `,
			last_health_check = CURRENT_TIMESTAMP,
			status = CASE WHEN registration.status IN ('unhealthy', 'offline') THEN 'healthy' ELSE registration.status END`
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE worker_registrations AS registration SET `+assignment+healthAssignment+`, updated_at = CURRENT_TIMESTAMP
		FROM managed_instances AS managed
		WHERE registration.worker_id = $1
		  AND managed.id = registration.instance_id
		  AND managed.worker_id = registration.worker_id
		  AND managed.workspace_id = registration.workspace_id
		  AND managed.state_json->>'status' IN ('pending', 'provisioning', 'running')`, workerID, payload)
	if err != nil {
		return registryUnavailable(err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return registryUnavailable(err)
	}
	if rows == 0 {
		return fmt.Errorf("%w: %s", ErrWorkerNotFound, workerID)
	}
	return nil
}

// Heartbeat atomically records stats, the optional complete model set, health
// recovery, and the authoritative server timestamp for one managed instance.
func (r *PostgresRegistry) Heartbeat(parent context.Context, instanceID, workerID string, stats types.WorkerStats, models []types.LoadedModel, replaceModels bool) (*types.WorkerInfo, error) {
	statsPayload, err := json.Marshal(stats)
	if err != nil {
		return nil, err
	}
	if models == nil {
		models = []types.LoadedModel{}
	}
	modelsPayload, err := json.Marshal(models)
	if err != nil {
		return nil, err
	}
	ctx, cancel := r.operationContext(parent)
	defer cancel()
	worker, err := r.scanWorker(r.db.QueryRowContext(ctx, `
		UPDATE worker_registrations AS registration SET
			stats = $3,
			loaded_models = CASE WHEN $4 THEN $5 ELSE registration.loaded_models END,
			status = CASE
				WHEN registration.status IN ('unhealthy', 'offline') THEN 'healthy'
				ELSE registration.status
			END,
			last_health_check = CURRENT_TIMESTAMP,
			updated_at = CURRENT_TIMESTAMP
		FROM managed_instances AS managed
		WHERE registration.worker_id = $2
		  AND registration.instance_id = $1
		  AND managed.id = registration.instance_id
		  AND managed.worker_id = registration.worker_id
		  AND managed.workspace_id = registration.workspace_id
		  AND managed.state_json->>'status' IN ('pending', 'provisioning', 'running')
		RETURNING `+workerRegistrationColumns,
		strings.TrimSpace(instanceID), strings.TrimSpace(workerID), statsPayload, replaceModels, modelsPayload,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrWorkerNotFound, workerID)
	}
	if err != nil {
		return nil, registryUnavailable(err)
	}
	return worker, nil
}

// Snapshot returns a consistent view containing only active, correctly bound
// managed instances. Stopped or terminated instances fail closed immediately.
func (r *PostgresRegistry) Snapshot(parent context.Context) ([]*types.WorkerInfo, error) {
	ctx, cancel := r.operationContext(parent)
	defer cancel()
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+workerRegistrationColumns+`
		FROM worker_registrations AS registration
		JOIN managed_instances AS managed ON managed.id = registration.instance_id
		WHERE managed.worker_id = registration.worker_id
		  AND managed.workspace_id = registration.workspace_id
		  AND managed.state_json->>'status' IN ('pending', 'provisioning', 'running')
		ORDER BY registration.worker_id`)
	if err != nil {
		return nil, registryUnavailable(err)
	}
	defer rows.Close()
	workers := make([]*types.WorkerInfo, 0)
	for rows.Next() {
		worker, err := r.scanWorker(rows)
		if err != nil {
			return nil, registryUnavailable(err)
		}
		workers = append(workers, worker)
	}
	if err := rows.Err(); err != nil {
		return nil, registryUnavailable(err)
	}
	return workers, nil
}

// Reconcile removes registrations that are inactive, mismatched, or expired,
// and marks registrations unhealthy using conditional replica-safe writes.
func (r *PostgresRegistry) Reconcile(parent context.Context) error {
	return r.reconcile(parent)
}

// StartHealthChecker periodically performs replica-safe health reconciliation.
func (r *PostgresRegistry) StartHealthChecker(ctx context.Context) {
	ticker := time.NewTicker(r.config.HealthCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.reconcile(ctx); err != nil && !errors.Is(err, context.Canceled) {
				slog.Warn("worker registry health reconciliation failed", slog.String("error", err.Error()))
			}
		}
	}
}

func (r *PostgresRegistry) reconcile(parent context.Context) error {
	ctx, cancel := r.operationContext(parent)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return registryUnavailable(err)
	}
	defer func() { _ = tx.Rollback() }()
	var databaseNow time.Time
	if err := tx.QueryRowContext(ctx, `SELECT CURRENT_TIMESTAMP`).Scan(&databaseNow); err != nil {
		return registryUnavailable(err)
	}
	removalCutoff := databaseNow.Add(-r.config.RemovalThreshold)
	unhealthyCutoff := databaseNow.Add(-r.config.UnhealthyThreshold)

	removedRows, err := tx.QueryContext(ctx, `
		DELETE FROM worker_registrations AS registration
		WHERE registration.last_health_check < $1
		   OR NOT EXISTS (
			SELECT 1 FROM managed_instances AS managed
			WHERE managed.id = registration.instance_id
			  AND managed.worker_id = registration.worker_id
			  AND managed.workspace_id = registration.workspace_id
			  AND managed.state_json->>'status' IN ('pending', 'provisioning', 'running')
		   )
		RETURNING worker_id, status, last_health_check`, removalCutoff)
	if err != nil {
		return registryUnavailable(err)
	}
	removed, err := scanHealthTransitions(removedRows, HealthTransitionRemoved, types.WorkerStatusOffline, databaseNow)
	if err != nil {
		return registryUnavailable(err)
	}

	unhealthyRows, err := tx.QueryContext(ctx, `
		WITH candidates AS MATERIALIZED (
			SELECT worker_id, status, last_health_check
			FROM worker_registrations
			WHERE last_health_check < $1
			  AND last_health_check >= $2
			  AND status <> 'unhealthy'
			FOR UPDATE
		)
		UPDATE worker_registrations AS registration
		SET status = 'unhealthy', updated_at = CURRENT_TIMESTAMP
		FROM candidates
		WHERE registration.worker_id = candidates.worker_id
		RETURNING registration.worker_id, candidates.status, candidates.last_health_check`, unhealthyCutoff, removalCutoff)
	if err != nil {
		return registryUnavailable(err)
	}
	unhealthy, err := scanHealthTransitions(unhealthyRows, HealthTransitionMarkedUnhealthy, types.WorkerStatusUnhealthy, databaseNow)
	if err != nil {
		return registryUnavailable(err)
	}
	if err := tx.Commit(); err != nil {
		return registryUnavailable(err)
	}
	// Only the replica whose conditional write committed emits the transition.
	r.emitTransitions(append(removed, unhealthy...))
	return nil
}

func scanHealthTransitions(rows *sql.Rows, event HealthTransitionEvent, to types.WorkerStatus, now time.Time) ([]HealthTransition, error) {
	defer rows.Close()
	var transitions []HealthTransition
	for rows.Next() {
		var workerID string
		var from types.WorkerStatus
		var heartbeat time.Time
		if err := rows.Scan(&workerID, &from, &heartbeat); err != nil {
			return nil, err
		}
		transitions = append(transitions, HealthTransition{
			Event: event, WorkerID: workerID, FromStatus: from, ToStatus: to,
			SinceHeartbeat: now.Sub(heartbeat),
		})
	}
	return transitions, rows.Err()
}

func (r *PostgresRegistry) emitTransitions(transitions []HealthTransition) {
	r.callbackM.RLock()
	callback := r.callback
	r.callbackM.RUnlock()
	if callback == nil {
		return
	}
	for _, transition := range transitions {
		callback(transition)
	}
}

const workerRegistrationColumns = `registration.worker_id, registration.instance_id,
	registration.workspace_id, registration.shared_pool, registration.address, registration.status,
	registration.loaded_models, registration.stats, registration.tags, registration.registration_id,
	registration.last_health_check, registration.registered_at`

type workerRowScanner interface{ Scan(...any) error }

func (r *PostgresRegistry) scanWorker(row workerRowScanner) (*types.WorkerInfo, error) {
	var worker types.WorkerInfo
	var status string
	var models, stats, tags []byte
	if err := row.Scan(
		&worker.WorkerID, &worker.InstanceID, &worker.WorkspaceID, &worker.SharedPool,
		&worker.Address, &status, &models, &stats, &tags, &worker.RegistrationID,
		&worker.LastHealthCheck, &worker.RegisteredAt,
	); err != nil {
		return nil, err
	}
	worker.Status = types.WorkerStatus(status)
	if err := json.Unmarshal(models, &worker.LoadedModels); err != nil {
		return nil, fmt.Errorf("decode worker models: %w", err)
	}
	if err := json.Unmarshal(stats, &worker.Stats); err != nil {
		return nil, fmt.Errorf("decode worker stats: %w", err)
	}
	if err := json.Unmarshal(tags, &worker.Tags); err != nil {
		return nil, fmt.Errorf("decode worker tags: %w", err)
	}
	return &worker, nil
}

func encodeWorkerPayload(models []types.LoadedModel, stats types.WorkerStats, tags map[string]string) ([]byte, []byte, []byte, error) {
	if models == nil {
		models = []types.LoadedModel{}
	}
	if tags == nil {
		tags = map[string]string{}
	}
	modelPayload, err := json.Marshal(models)
	if err != nil {
		return nil, nil, nil, err
	}
	statsPayload, err := json.Marshal(stats)
	if err != nil {
		return nil, nil, nil, err
	}
	tagPayload, err := json.Marshal(tags)
	return modelPayload, statsPayload, tagPayload, err
}

func validWorkerStatus(status types.WorkerStatus) bool {
	switch status {
	case types.WorkerStatusHealthy, types.WorkerStatusDegraded, types.WorkerStatusUnhealthy,
		types.WorkerStatusDraining, types.WorkerStatusOffline:
		return true
	default:
		return false
	}
}

func copyWorkerRegistration(destination, source *types.WorkerInfo) {
	destination.InstanceID = source.InstanceID
	destination.WorkspaceID = source.WorkspaceID
	destination.SharedPool = source.SharedPool
	destination.Address = source.Address
	destination.Status = source.Status
	destination.LoadedModels = source.LoadedModels
	destination.Stats = source.Stats
	destination.Tags = source.Tags
	destination.RegistrationID = source.RegistrationID
	destination.LastHealthCheck = source.LastHealthCheck
	destination.RegisteredAt = source.RegisteredAt
}

func registryUnavailable(err error) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrWorkerRegistryUnavailable, err)
}
