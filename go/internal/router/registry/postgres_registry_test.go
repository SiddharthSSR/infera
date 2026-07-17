package registry

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/infera/infera/go/pkg/types"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresRegistryCrossReplicaRegistrationAndHeartbeat(t *testing.T) {
	dsn := isolatedRegistryDSN(t)
	seedManagedWorker(t, dsn, "instance-1", "worker-1", "workspace-1", "running")
	first := newTestPostgresRegistry(t, dsn, PostgresRegistryConfig{})
	second := newTestPostgresRegistry(t, dsn, PostgresRegistryConfig{})

	worker := testManagedWorker("instance-1", "worker-1", "workspace-1")
	if err := first.Register(context.Background(), worker); err != nil {
		t.Fatalf("register: %v", err)
	}
	if worker.RegistrationID == "" {
		t.Fatal("register did not issue an opaque identity")
	}
	initialRegistrationID := worker.RegistrationID

	snapshot, err := second.Snapshot(context.Background())
	if err != nil || len(snapshot) != 1 {
		t.Fatalf("cross-replica snapshot: workers=%+v err=%v", snapshot, err)
	}
	if snapshot[0].InstanceID != "instance-1" || snapshot[0].WorkspaceID != "workspace-1" {
		t.Fatalf("snapshot lost durable ownership: %+v", snapshot[0])
	}

	replacement := testManagedWorker("instance-1", "worker-1", "workspace-1")
	replacement.Address = "http://worker-1.internal:9090"
	if err := second.Register(context.Background(), replacement); err != nil {
		t.Fatalf("replace registration: %v", err)
	}
	if replacement.RegistrationID == "" || replacement.RegistrationID == initialRegistrationID {
		t.Fatalf("replacement reused registration identity: old=%q new=%q", initialRegistrationID, replacement.RegistrationID)
	}

	stats := types.WorkerStats{QueueDepth: 7, GPUUtilization: 0.5}
	heartbeat, err := first.Heartbeat(context.Background(), "instance-1", "worker-1", stats, []types.LoadedModel{}, true)
	if err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	if heartbeat.Stats.QueueDepth != 7 || len(heartbeat.LoadedModels) != 0 {
		t.Fatalf("heartbeat was not atomic or did not clear models: %+v", heartbeat)
	}
	if heartbeat.RegistrationID != replacement.RegistrationID {
		t.Fatalf("heartbeat changed registration identity: got=%q want=%q", heartbeat.RegistrationID, replacement.RegistrationID)
	}
}

func TestPostgresRegistryRejectsOwnershipConflictAndInactiveInstance(t *testing.T) {
	dsn := isolatedRegistryDSN(t)
	seedManagedWorker(t, dsn, "instance-1", "worker-1", "workspace-1", "running")
	seedManagedWorker(t, dsn, "instance-2", "worker-2", "workspace-2", "running")
	registry := newTestPostgresRegistry(t, dsn, PostgresRegistryConfig{})

	worker := testManagedWorker("instance-1", "worker-1", "workspace-1")
	if err := registry.Register(context.Background(), worker); err != nil {
		t.Fatal(err)
	}
	conflict := testManagedWorker("instance-2", "worker-1", "workspace-2")
	if err := registry.Register(context.Background(), conflict); !errors.Is(err, ErrWorkerRegistrationConflict) {
		t.Fatalf("cross-instance replacement was not rejected: %v", err)
	}
	if _, err := registry.Heartbeat(context.Background(), "instance-2", "worker-1", types.WorkerStats{}, nil, false); !errors.Is(err, ErrWorkerNotFound) {
		t.Fatalf("cross-instance heartbeat was not rejected: %v", err)
	}

	setManagedStatus(t, registry.db, "instance-1", "stopped")
	snapshot, err := registry.Snapshot(context.Background())
	if err != nil || len(snapshot) != 0 {
		t.Fatalf("inactive instance remained routable: workers=%+v err=%v", snapshot, err)
	}
	if _, err := registry.Heartbeat(context.Background(), "instance-1", "worker-1", types.WorkerStats{}, nil, false); !errors.Is(err, ErrWorkerNotFound) {
		t.Fatalf("inactive instance heartbeat did not fail closed: %v", err)
	}
	if err := registry.Reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile inactive registration: %v", err)
	}
	var registrations int
	if err := registry.db.QueryRow(`SELECT COUNT(*) FROM worker_registrations`).Scan(&registrations); err != nil || registrations != 0 {
		t.Fatalf("inactive registration not removed: count=%d err=%v", registrations, err)
	}
}

func TestPostgresRegistryRetiresPriorWorkerAfterManagedRebinding(t *testing.T) {
	dsn := isolatedRegistryDSN(t)
	seedManagedWorker(t, dsn, "instance-1", "worker-old", "workspace-1", "running")
	registry := newTestPostgresRegistry(t, dsn, PostgresRegistryConfig{})
	if err := registry.Register(context.Background(), testManagedWorker("instance-1", "worker-old", "workspace-1")); err != nil {
		t.Fatalf("register old worker: %v", err)
	}
	if _, err := registry.db.Exec(`UPDATE managed_instances SET worker_id = 'worker-new' WHERE id = 'instance-1'`); err != nil {
		t.Fatalf("rebind managed worker: %v", err)
	}
	if err := registry.Register(context.Background(), testManagedWorker("instance-1", "worker-new", "workspace-1")); err != nil {
		t.Fatalf("register rebound worker: %v", err)
	}

	workers, err := registry.Snapshot(context.Background())
	if err != nil || len(workers) != 1 || workers[0].WorkerID != "worker-new" {
		t.Fatalf("old registration was not retired: workers=%+v err=%v", workers, err)
	}
}

func TestPostgresRegistryConditionalHealthTransitionsAcrossReplicas(t *testing.T) {
	dsn := isolatedRegistryDSN(t)
	seedManagedWorker(t, dsn, "instance-unhealthy", "worker-unhealthy", "workspace-1", "running")
	seedManagedWorker(t, dsn, "instance-removed", "worker-removed", "workspace-1", "running")
	config := PostgresRegistryConfig{RegistryConfig: RegistryConfig{
		HealthCheckInterval: time.Hour,
		UnhealthyThreshold:  time.Minute,
		RemovalThreshold:    2 * time.Minute,
	}}
	first := newTestPostgresRegistry(t, dsn, config)
	second := newTestPostgresRegistry(t, dsn, config)
	for _, worker := range []*types.WorkerInfo{
		testManagedWorker("instance-unhealthy", "worker-unhealthy", "workspace-1"),
		testManagedWorker("instance-removed", "worker-removed", "workspace-1"),
	} {
		if err := first.Register(context.Background(), worker); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := first.db.Exec(`
		UPDATE worker_registrations
		SET last_health_check = CASE worker_id
			WHEN 'worker-unhealthy' THEN CURRENT_TIMESTAMP - INTERVAL '90 seconds'
			ELSE CURRENT_TIMESTAMP - INTERVAL '3 minutes'
		END`); err != nil {
		t.Fatal(err)
	}

	var transitionM sync.Mutex
	var transitions []HealthTransition
	callback := func(transition HealthTransition) {
		transitionM.Lock()
		transitions = append(transitions, transition)
		transitionM.Unlock()
	}
	first.OnHealthTransition(callback)
	second.OnHealthTransition(callback)
	var wg sync.WaitGroup
	for _, candidate := range []*PostgresRegistry{first, second} {
		wg.Add(1)
		go func(registry *PostgresRegistry) {
			defer wg.Done()
			if err := registry.Reconcile(context.Background()); err != nil {
				t.Errorf("reconcile: %v", err)
			}
		}(candidate)
	}
	wg.Wait()
	transitionM.Lock()
	defer transitionM.Unlock()
	if len(transitions) != 2 {
		t.Fatalf("expected exactly one transition per worker across replicas, got %+v", transitions)
	}
	events := map[string]HealthTransitionEvent{}
	for _, transition := range transitions {
		events[transition.WorkerID] = transition.Event
	}
	if events["worker-unhealthy"] != HealthTransitionMarkedUnhealthy || events["worker-removed"] != HealthTransitionRemoved {
		t.Fatalf("unexpected transition receipts: %+v", transitions)
	}
	snapshot, err := first.Snapshot(context.Background())
	if err != nil || len(snapshot) != 1 || snapshot[0].WorkerID != "worker-unhealthy" || snapshot[0].Status != types.WorkerStatusUnhealthy {
		t.Fatalf("unexpected reconciled snapshot: workers=%+v err=%v", snapshot, err)
	}
}

func TestPostgresRegistryMigrationIsConcurrentAndIdempotent(t *testing.T) {
	dsn := isolatedRegistryDSN(t)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	registries := make(chan *PostgresRegistry, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			registry, err := NewPostgresRegistry(dsn, PostgresRegistryConfig{})
			if registry != nil {
				registries <- registry
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	close(registries)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent migration: %v", err)
		}
	}
	for registry := range registries {
		if err := registry.Close(); err != nil {
			t.Errorf("close registry: %v", err)
		}
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version string
	if err := db.QueryRow(`SELECT value FROM control_state_metadata WHERE key = 'worker_registry_schema'`).Scan(&version); err != nil || version != workerRegistrySchemaVersion {
		t.Fatalf("registry capability marker: version=%q err=%v", version, err)
	}
}

func TestPostgresRegistryUsesCallerDerivedBoundedContext(t *testing.T) {
	dsn := isolatedRegistryDSN(t)
	registry := newTestPostgresRegistry(t, dsn, PostgresRegistryConfig{QueryTimeout: 2 * time.Second})
	blocker, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	tx, err := blocker.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`LOCK TABLE worker_registrations IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = registry.Snapshot(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("snapshot did not preserve caller deadline: %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("snapshot exceeded caller deadline: %v", elapsed)
	}
}

func newTestPostgresRegistry(t *testing.T, dsn string, config PostgresRegistryConfig) *PostgresRegistry {
	t.Helper()
	registry, err := NewPostgresRegistry(dsn, config)
	if err != nil {
		t.Fatalf("new postgres registry: %v", err)
	}
	t.Cleanup(func() {
		if err := registry.Close(); err != nil {
			t.Errorf("close postgres registry: %v", err)
		}
	})
	return registry
}

func testManagedWorker(instanceID, workerID, workspaceID string) *types.WorkerInfo {
	return &types.WorkerInfo{
		InstanceID: instanceID, WorkerID: workerID, WorkspaceID: workspaceID,
		Address: "http://" + workerID + ".internal:8081", Status: types.WorkerStatusHealthy,
		LoadedModels: []types.LoadedModel{{ModelID: "model-1"}}, Tags: map[string]string{"test": "true"},
	}
}

func seedManagedWorker(t *testing.T, dsn, instanceID, workerID, workspaceID, status string) {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		INSERT INTO managed_instances (id, worker_id, workspace_id, state_json)
		VALUES ($1, $2, $3, jsonb_build_object('status', $4::text))`, instanceID, workerID, workspaceID, status); err != nil {
		t.Fatalf("seed managed worker: %v", err)
	}
}

func setManagedStatus(t *testing.T, db *sql.DB, instanceID, status string) {
	t.Helper()
	if _, err := db.Exec(`UPDATE managed_instances SET state_json = jsonb_set(state_json, '{status}', to_jsonb($2::text)) WHERE id = $1`, instanceID, status); err != nil {
		t.Fatalf("set managed status: %v", err)
	}
}

func isolatedRegistryDSN(t *testing.T) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("INFERA_TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("INFERA_TEST_POSTGRES_DSN is not configured")
	}
	schema := "worker_registry_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(`CREATE SCHEMA "` + schema + `"`); err != nil {
		_ = admin.Close()
		t.Fatalf("create registry test schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), `DROP SCHEMA "`+schema+`" CASCADE`)
		_ = admin.Close()
	})
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	isolated := parsed.String()
	db, err := sql.Open("pgx", isolated)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		CREATE TABLE control_state_metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL);
		CREATE TABLE managed_instances (
			id TEXT PRIMARY KEY,
			worker_id TEXT,
			workspace_id TEXT NOT NULL,
			state_json JSONB NOT NULL
		)`); err != nil {
		t.Fatalf("create registry test control state: %v", err)
	}
	return isolated
}
