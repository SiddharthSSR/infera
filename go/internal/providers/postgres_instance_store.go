package providers

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/infera/infera/go/internal/secretbox"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	controlStateSchemaVersion  = "2"
	controlStateWriterProtocol = "1"
	controlStateMigrationLock  = int64(4242424701)
)

var (
	ErrControlStateUnavailable   = errors.New("control state unavailable")
	ErrWorkerIdentityConflict    = errors.New("worker identity conflict")
	ErrWorkerCredentialIntegrity = errors.New("worker credential integrity failure")
)

func controlStateUnavailable(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %v", ErrControlStateUnavailable, err)
}

func credentialIntegrityError(err error) error {
	return fmt.Errorf("%w: %v", ErrWorkerCredentialIntegrity, err)
}

// ControlStateMetadata identifies the durable control-plane store shared by
// gateway replicas. It contains no credentials or customer data.
type ControlStateMetadata struct {
	ClusterID      string
	SchemaVersion  string
	WriterProtocol string
}

// PostgresInstanceStore persists managed provider instances and their
// deployment-bound credentials for cross-replica authentication and routing.
type PostgresInstanceStore struct {
	db           *sql.DB
	box          *secretbox.Box
	updateLoaded func() // test-only transaction interleaving hook
}

// NewPostgresInstanceStore opens and migrates a shared managed-instance store.
// encodedKey uses the same AES-256-GCM key format as provider credentials.
func NewPostgresInstanceStore(dsn, encodedKey string) (*PostgresInstanceStore, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("postgres control-state DSN is required")
	}
	box, err := secretbox.New(encodedKey, "worker credential")
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	store := &PostgresInstanceStore{db: db, box: box}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.validateStoredCredentials(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *PostgresInstanceStore) Close() error { return s.db.Close() }

func (s *PostgresInstanceStore) migrate() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock($1)`, controlStateMigrationLock); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS control_state_metadata (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`); err != nil {
		return err
	}
	var protocol string
	err = tx.QueryRow(`SELECT value FROM control_state_metadata WHERE key = 'writer_protocol'`).Scan(&protocol)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		protocol = controlStateWriterProtocol
	case err != nil:
		return err
	case protocol != controlStateWriterProtocol:
		return fmt.Errorf("control-state writer protocol %q is incompatible with this gateway", protocol)
	}
	var schemaVersion string
	err = tx.QueryRow(`SELECT value FROM control_state_metadata WHERE key = 'schema_version'`).Scan(&schemaVersion)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		schemaVersion = controlStateSchemaVersion
	case err != nil:
		return err
	case schemaVersion != controlStateSchemaVersion:
		return fmt.Errorf("control-state schema version %q is incompatible with this gateway", schemaVersion)
	}
	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS managed_instances (
			id TEXT PRIMARY KEY,
			provider_id TEXT NOT NULL,
			provider TEXT NOT NULL,
			workspace_id TEXT NOT NULL,
			state_json JSONB NOT NULL,
			worker_id TEXT,
			worker_credential_hash BYTEA NOT NULL,
			worker_credential_ciphertext TEXT NOT NULL,
			lifecycle_version BIGINT NOT NULL DEFAULT 1 CHECK (lifecycle_version > 0),
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (provider, provider_id)
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_managed_instances_worker_id
			ON managed_instances(worker_id) WHERE worker_id IS NOT NULL AND worker_id <> '';
		CREATE UNIQUE INDEX IF NOT EXISTS idx_managed_instances_worker_credential_hash
			ON managed_instances(worker_credential_hash);
		CREATE INDEX IF NOT EXISTS idx_managed_instances_workspace
			ON managed_instances(workspace_id);
	`); err != nil {
		return err
	}
	metadata := map[string]string{
		"schema_version":  controlStateSchemaVersion,
		"writer_protocol": controlStateWriterProtocol,
	}
	for key, value := range metadata {
		if _, err := tx.Exec(`
			INSERT INTO control_state_metadata (key, value) VALUES ($1, $2)
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, key, value); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`
		INSERT INTO control_state_metadata (key, value) VALUES ('control_state_cluster_id', $1)
		ON CONFLICT (key) DO NOTHING`, uuid.NewString()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PostgresInstanceStore) ControlStateMetadata() (ControlStateMetadata, error) {
	rows, err := s.db.Query(`
		SELECT key, value FROM control_state_metadata
		WHERE key IN ('control_state_cluster_id', 'schema_version', 'writer_protocol')`)
	if err != nil {
		return ControlStateMetadata{}, err
	}
	defer rows.Close()
	values := make(map[string]string, 3)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return ControlStateMetadata{}, err
		}
		values[key] = value
	}
	if err := rows.Err(); err != nil {
		return ControlStateMetadata{}, err
	}
	metadata := ControlStateMetadata{
		ClusterID:      values["control_state_cluster_id"],
		SchemaVersion:  values["schema_version"],
		WriterProtocol: values["writer_protocol"],
	}
	if metadata.ClusterID == "" || metadata.SchemaVersion == "" || metadata.WriterProtocol == "" {
		return ControlStateMetadata{}, errors.New("control-state metadata is incomplete")
	}
	return metadata, nil
}

func workerCredentialAAD(instanceID, workspaceID string, provider ProviderType) []byte {
	return []byte("worker-credential\x00" + workspaceID + "\x00" + instanceID + "\x00" + string(provider))
}

func (s *PostgresInstanceStore) put(instance *Instance) error {
	if instance == nil || strings.TrimSpace(instance.ID) == "" {
		return errors.New("managed instance ID is required")
	}
	credential := strings.TrimSpace(instance.WorkerCredential)
	if credential == "" {
		return errors.New("managed instance worker credential is required")
	}
	hash := sha256.Sum256([]byte(credential))
	if instance.WorkerCredentialHash != ([sha256.Size]byte{}) && subtle.ConstantTimeCompare(hash[:], instance.WorkerCredentialHash[:]) != 1 {
		return errors.New("managed instance worker credential hash does not match credential")
	}
	instance.WorkerCredentialHash = hash
	ciphertext, err := s.box.Encrypt(credential, workerCredentialAAD(instance.ID, instance.WorkspaceID, instance.Provider))
	if err != nil {
		return err
	}
	payload, err := json.Marshal(instance)
	if err != nil {
		return fmt.Errorf("encode managed instance: %w", err)
	}
	workerID := any(nil)
	if strings.TrimSpace(instance.WorkerID) != "" {
		workerID = strings.TrimSpace(instance.WorkerID)
	}
	createdAt := instance.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	_, err = s.db.Exec(`
		INSERT INTO managed_instances
			(id, provider_id, provider, workspace_id, state_json, worker_id,
			 worker_credential_hash, worker_credential_ciphertext, lifecycle_version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1, $9, CURRENT_TIMESTAMP)
		ON CONFLICT (id) DO UPDATE SET
			provider_id = EXCLUDED.provider_id,
			provider = EXCLUDED.provider,
			workspace_id = EXCLUDED.workspace_id,
			state_json = EXCLUDED.state_json,
			worker_id = EXCLUDED.worker_id,
			worker_credential_hash = EXCLUDED.worker_credential_hash,
			worker_credential_ciphertext = EXCLUDED.worker_credential_ciphertext,
			updated_at = CURRENT_TIMESTAMP`,
		instance.ID, instance.ProviderID, string(instance.Provider), instance.WorkspaceID,
		payload, workerID, hash[:], ciphertext, createdAt,
	)
	return controlStateUnavailable(err)
}

const instanceSelectColumns = `id, provider_id, provider, workspace_id, state_json, worker_id,
	worker_credential_hash, worker_credential_ciphertext, lifecycle_version`

type rowScanner interface{ Scan(...any) error }

func (s *PostgresInstanceStore) scanInstance(row rowScanner) (*Instance, error) {
	var id, providerID, provider, workspaceID string
	var payload, hash []byte
	var workerID sql.NullString
	var ciphertext string
	var lifecycleVersion int64
	if err := row.Scan(&id, &providerID, &provider, &workspaceID, &payload, &workerID, &hash, &ciphertext, &lifecycleVersion); err != nil {
		return nil, err
	}
	if len(hash) != sha256.Size {
		return nil, credentialIntegrityError(fmt.Errorf("managed instance %s has invalid worker credential hash", id))
	}
	var instance Instance
	if err := json.Unmarshal(payload, &instance); err != nil {
		return nil, credentialIntegrityError(fmt.Errorf("decode managed instance %s: %w", id, err))
	}
	instance.ID = id
	instance.ProviderID = providerID
	instance.Provider = ProviderType(provider)
	instance.WorkspaceID = workspaceID
	instance.WorkerID = workerID.String
	instance.LifecycleVersion = lifecycleVersion
	copy(instance.WorkerCredentialHash[:], hash)
	credential, err := s.box.Decrypt(ciphertext, workerCredentialAAD(id, workspaceID, instance.Provider))
	if err != nil {
		return nil, credentialIntegrityError(fmt.Errorf("decrypt managed instance %s worker credential: %w", id, err))
	}
	computed := sha256.Sum256([]byte(credential))
	if subtle.ConstantTimeCompare(computed[:], hash) != 1 {
		return nil, credentialIntegrityError(fmt.Errorf("managed instance %s worker credential hash mismatch", id))
	}
	instance.WorkerCredential = credential
	return &instance, nil
}

func (s *PostgresInstanceStore) get(instanceID string) (*Instance, bool, error) {
	instance, err := s.scanInstance(s.db.QueryRow(`SELECT `+instanceSelectColumns+` FROM managed_instances WHERE id = $1`, instanceID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		if errors.Is(err, ErrWorkerCredentialIntegrity) {
			return nil, false, err
		}
		return nil, false, controlStateUnavailable(err)
	}
	return instance, true, nil
}

func (s *PostgresInstanceStore) listQuery(query string, args ...any) ([]*Instance, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var instances []*Instance
	for rows.Next() {
		instance, err := s.scanInstance(rows)
		if err != nil {
			return nil, err
		}
		instances = append(instances, instance)
	}
	return instances, rows.Err()
}

func (s *PostgresInstanceStore) list() ([]*Instance, error) {
	instances, err := s.listQuery(`SELECT ` + instanceSelectColumns + ` FROM managed_instances`)
	return instances, classifyStoreReadError(err)
}

func (s *PostgresInstanceStore) listByProvider(providerType ProviderType) ([]*Instance, error) {
	instances, err := s.listQuery(`SELECT `+instanceSelectColumns+` FROM managed_instances WHERE provider = $1`, string(providerType))
	return instances, classifyStoreReadError(err)
}

func (s *PostgresInstanceStore) listByWorkspace(workspaceID string) ([]*Instance, error) {
	instances, err := s.listQuery(`SELECT `+instanceSelectColumns+` FROM managed_instances WHERE workspace_id = $1`, workspaceID)
	return instances, classifyStoreReadError(err)
}

func (s *PostgresInstanceStore) findByWorker(workerID string) (*Instance, bool, error) {
	instance, err := s.scanInstance(s.db.QueryRow(`SELECT `+instanceSelectColumns+` FROM managed_instances WHERE worker_id = $1`, workerID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, classifyStoreReadError(err)
	}
	return instance, true, nil
}

func (s *PostgresInstanceStore) findByProviderRef(providerType ProviderType, providerID string) (*Instance, bool, error) {
	instance, err := s.scanInstance(s.db.QueryRow(`SELECT `+instanceSelectColumns+` FROM managed_instances WHERE provider = $1 AND provider_id = $2`, string(providerType), providerID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, classifyStoreReadError(err)
	}
	return instance, true, nil
}

func (s *PostgresInstanceStore) findReusableStopped(providerType ProviderType, req *ProvisionRequest) (*Instance, error) {
	instances, err := s.listByProvider(providerType)
	if err != nil {
		return nil, err
	}
	for _, instance := range instances {
		if instance.WorkspaceID == req.WorkspaceID && instance.Status == InstanceStatusStopped &&
			instance.GPUType == req.GPUType && instance.GPUCount == req.GPUCount &&
			instance.Engine.OrDefault() == req.Engine.OrDefault() && sameModels(instance.Models, req.Models) {
			return instance, nil
		}
	}
	return nil, nil
}

func (s *PostgresInstanceStore) update(instanceID string, apply func(*Instance)) (bool, error) {
	return s.updateVersioned(instanceID, 0, false, apply)
}

func (s *PostgresInstanceStore) updateLifecycle(instanceID string, apply func(*Instance)) (bool, error) {
	return s.updateVersioned(instanceID, 0, true, apply)
}

func (s *PostgresInstanceStore) updateIfLifecycleVersion(instanceID string, expectedVersion int64, apply func(*Instance)) (bool, error) {
	return s.updateVersioned(instanceID, expectedVersion, true, apply)
}

func (s *PostgresInstanceStore) updateVersioned(instanceID string, expectedVersion int64, incrementLifecycle bool, apply func(*Instance)) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, controlStateUnavailable(err)
	}
	defer func() { _ = tx.Rollback() }()
	instance, err := s.scanInstance(tx.QueryRow(`SELECT `+instanceSelectColumns+` FROM managed_instances WHERE id = $1 FOR UPDATE`, instanceID))
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, classifyStoreReadError(err)
	}
	if expectedVersion > 0 && instance.LifecycleVersion != expectedVersion {
		return false, nil
	}
	if s.updateLoaded != nil {
		s.updateLoaded()
	}
	apply(instance)
	payload, err := json.Marshal(instance)
	if err != nil {
		return false, err
	}
	workerID := any(nil)
	if strings.TrimSpace(instance.WorkerID) != "" {
		workerID = strings.TrimSpace(instance.WorkerID)
	}
	query := `UPDATE managed_instances SET state_json = $1, worker_id = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $3`
	if incrementLifecycle {
		query = `UPDATE managed_instances SET state_json = $1, worker_id = $2, lifecycle_version = lifecycle_version + 1, updated_at = CURRENT_TIMESTAMP WHERE id = $3`
	}
	if _, err := tx.Exec(query, payload, workerID, instanceID); err != nil {
		return false, controlStateUnavailable(err)
	}
	if err := tx.Commit(); err != nil {
		return false, controlStateUnavailable(err)
	}
	return true, nil
}

func classifyStoreReadError(err error) error {
	if err == nil || errors.Is(err, ErrWorkerCredentialIntegrity) {
		return err
	}
	return controlStateUnavailable(err)
}

func (s *PostgresInstanceStore) authenticateWorkerTokenHash(hash [sha256.Size]byte) (*Instance, bool, error) {
	instance, err := s.scanInstance(s.db.QueryRow(`SELECT `+instanceSelectColumns+` FROM managed_instances WHERE worker_credential_hash = $1`, hash[:]))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, classifyStoreReadError(err)
	}
	if !workerCredentialActive(instance.Status) {
		return nil, false, nil
	}
	return instance, true, nil
}

func workerCredentialActive(status InstanceStatus) bool {
	switch status {
	case InstanceStatusPending, InstanceStatusProvisioning, InstanceStatusRunning:
		return true
	default:
		return false
	}
}

func (s *PostgresInstanceStore) authorizeWorkerBinding(instanceID, workerID string) error {
	instanceID = strings.TrimSpace(instanceID)
	workerID = strings.TrimSpace(workerID)
	if instanceID == "" || workerID == "" {
		return fmt.Errorf("%w: instance ID and worker ID are required", ErrWorkerIdentityConflict)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return controlStateUnavailable(err)
	}
	defer func() { _ = tx.Rollback() }()
	var existing sql.NullString
	if err := tx.QueryRow(`SELECT worker_id FROM managed_instances WHERE id = $1 FOR UPDATE`, instanceID).Scan(&existing); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("instance %s not found", instanceID)
		}
		return controlStateUnavailable(err)
	}
	if existing.Valid && existing.String != "" && existing.String != workerID {
		return fmt.Errorf("%w: instance is already bound to worker %s", ErrWorkerIdentityConflict, existing.String)
	}
	var owner string
	err = tx.QueryRow(`SELECT id FROM managed_instances WHERE worker_id = $1 AND id <> $2`, workerID, instanceID).Scan(&owner)
	if err == nil {
		return fmt.Errorf("%w: worker %s is already bound to another instance", ErrWorkerIdentityConflict, workerID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return controlStateUnavailable(err)
	}
	return controlStateUnavailable(tx.Commit())
}

func (s *PostgresInstanceStore) linkWorker(instanceID, workerID string, now time.Time) error {
	instanceID = strings.TrimSpace(instanceID)
	workerID = strings.TrimSpace(workerID)
	if instanceID == "" || workerID == "" {
		return fmt.Errorf("%w: instance ID and worker ID are required", ErrWorkerIdentityConflict)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return controlStateUnavailable(err)
	}
	defer func() { _ = tx.Rollback() }()
	instance, err := s.scanInstance(tx.QueryRow(`SELECT `+instanceSelectColumns+` FROM managed_instances WHERE id = $1 FOR UPDATE`, instanceID))
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("instance %s not found", instanceID)
	}
	if err != nil {
		return classifyStoreReadError(err)
	}
	if !workerCredentialActive(instance.Status) {
		return fmt.Errorf("%w: instance is not active", ErrWorkerIdentityConflict)
	}
	if instance.WorkerID != "" && instance.WorkerID != workerID {
		return fmt.Errorf("%w: instance is already bound to worker %s", ErrWorkerIdentityConflict, instance.WorkerID)
	}
	instance.WorkerID = workerID
	if instance.WorkerRegisteredAt == nil {
		instance.WorkerRegisteredAt = &now
	}
	instance.WorkerLastHeartbeatAt = &now
	instance.LastWorkerRegistrationCheckAt = &now
	instance.WorkerRegistrationStatus = WorkerRegistrationReady
	instance.LastWorkerRegistrationError = ""
	payload, err := json.Marshal(instance)
	if err != nil {
		return controlStateUnavailable(err)
	}
	result, err := tx.Exec(`UPDATE managed_instances SET worker_id = $1, state_json = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $3`, workerID, payload, instanceID)
	if err != nil {
		return controlStateUnavailable(err)
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		if err != nil {
			return controlStateUnavailable(err)
		}
		return fmt.Errorf("instance %s not found", instanceID)
	}
	return controlStateUnavailable(tx.Commit())
}

func (s *PostgresInstanceStore) workerCredentialForWorker(workerID string) (string, bool, error) {
	instance, err := s.scanInstance(s.db.QueryRow(`SELECT `+instanceSelectColumns+` FROM managed_instances WHERE worker_id = $1`, workerID))
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, classifyStoreReadError(err)
	}
	if !workerCredentialActive(instance.Status) {
		return "", false, nil
	}
	credential := strings.TrimSpace(instance.WorkerCredential)
	return credential, credential != "", nil
}

func (s *PostgresInstanceStore) validateStoredCredentials() error {
	_, err := s.listQuery(`SELECT ` + instanceSelectColumns + ` FROM managed_instances`)
	return classifyStoreReadError(err)
}
