// Package auth provides API key authentication for Infera.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/infera/infera/go/internal/migrate"
	_ "github.com/mattn/go-sqlite3"
)

var ErrWorkspaceProviderConfigNotFound = errors.New("workspace provider config not found")

// authMigrations defines the versioned schema for the auth database.
var authMigrations = []migrate.Migration{
	{
		Version:     1,
		Description: "create api_keys table",
		SQL: `
		CREATE TABLE IF NOT EXISTS api_keys (
			id         TEXT PRIMARY KEY,
			key_hash   TEXT NOT NULL UNIQUE,
			key_prefix TEXT NOT NULL,
			name       TEXT NOT NULL,
			role       TEXT NOT NULL DEFAULT 'user',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_used  DATETIME,
			status     TEXT NOT NULL DEFAULT 'active'
		);
		CREATE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys(key_hash);
		CREATE INDEX IF NOT EXISTS idx_api_keys_status ON api_keys(status);`,
	},
	{
		Version:     2,
		Description: "create sessions table",
		SQL: `
		CREATE TABLE IF NOT EXISTS sessions (
			id         TEXT PRIMARY KEY,
			key_id     TEXT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
			token_hash TEXT NOT NULL UNIQUE,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME NOT NULL,
			last_seen  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_sessions_token_hash ON sessions(token_hash);
		CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);`,
	},
	{
		Version:     3,
		Description: "create workspaces and scope api keys",
		SQL: fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS workspaces (
			id         TEXT PRIMARY KEY,
			slug       TEXT NOT NULL UNIQUE,
			name       TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			status     TEXT NOT NULL DEFAULT 'active'
		);
		INSERT OR IGNORE INTO workspaces (id, slug, name, status)
		VALUES ('%s', '%s', '%s', 'active');
		ALTER TABLE api_keys ADD COLUMN workspace_id TEXT NOT NULL DEFAULT '%s';
		UPDATE api_keys SET workspace_id = '%s' WHERE workspace_id IS NULL OR workspace_id = '';
		CREATE INDEX IF NOT EXISTS idx_api_keys_workspace ON api_keys(workspace_id);`,
			DefaultWorkspaceID,
			DefaultWorkspaceSlug,
			DefaultWorkspaceName,
			DefaultWorkspaceID,
			DefaultWorkspaceID,
		),
	},
	{
		Version:     4,
		Description: "add workspace quotas",
		SQL: `
		CREATE TABLE IF NOT EXISTS workspace_quotas (
			workspace_id            TEXT PRIMARY KEY,
			monthly_request_limit   INTEGER,
			monthly_token_limit     INTEGER,
			enforce_hard_limits     INTEGER NOT NULL DEFAULT 1,
			updated_at              DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		INSERT OR IGNORE INTO workspace_quotas (workspace_id, enforce_hard_limits)
		SELECT id, 1 FROM workspaces;`,
	},
	{
		Version:     5,
		Description: "add key principal type",
		SQL: `
		ALTER TABLE api_keys ADD COLUMN principal_type TEXT NOT NULL DEFAULT 'human';
		CREATE INDEX IF NOT EXISTS idx_api_keys_principal_type ON api_keys(principal_type);`,
	},
	{
		Version:     6,
		Description: "add workspace memberships and invitations",
		SQL: `
		CREATE TABLE IF NOT EXISTS workspace_memberships (
			id           TEXT PRIMARY KEY,
			workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
			email        TEXT NOT NULL,
			display_name TEXT NOT NULL,
			role         TEXT NOT NULL,
			status       TEXT NOT NULL DEFAULT 'active',
			created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(workspace_id, email)
		);
		CREATE INDEX IF NOT EXISTS idx_workspace_memberships_workspace ON workspace_memberships(workspace_id);
		CREATE INDEX IF NOT EXISTS idx_workspace_memberships_email ON workspace_memberships(email);
		CREATE TABLE IF NOT EXISTS workspace_invitations (
			id                TEXT PRIMARY KEY,
			workspace_id      TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
			email             TEXT NOT NULL,
			display_name      TEXT NOT NULL DEFAULT '',
			role              TEXT NOT NULL,
			invite_token_hash TEXT NOT NULL UNIQUE,
			invited_by_key_id TEXT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
			created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			expires_at        DATETIME NOT NULL,
			status            TEXT NOT NULL DEFAULT 'pending'
		);
		CREATE INDEX IF NOT EXISTS idx_workspace_invitations_workspace ON workspace_invitations(workspace_id);
		CREATE INDEX IF NOT EXISTS idx_workspace_invitations_status ON workspace_invitations(status);
		ALTER TABLE api_keys ADD COLUMN membership_id TEXT;
		CREATE INDEX IF NOT EXISTS idx_api_keys_membership ON api_keys(membership_id);`,
	},
	{
		Version:     7,
		Description: "add workspace provider configs",
		SQL: `
		CREATE TABLE IF NOT EXISTS workspace_provider_configs (
			workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
			provider     TEXT NOT NULL,
			api_key      TEXT NOT NULL DEFAULT '',
			api_secret   TEXT NOT NULL DEFAULT '',
			endpoint     TEXT NOT NULL DEFAULT '',
			created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (workspace_id, provider)
		);
		CREATE INDEX IF NOT EXISTS idx_workspace_provider_configs_workspace ON workspace_provider_configs(workspace_id);`,
	},
	{
		Version:     8,
		Description: "add provider config options json",
		SQL: `
		ALTER TABLE workspace_provider_configs ADD COLUMN options_json TEXT NOT NULL DEFAULT '{}';`,
	},
}

// KeyRecord represents a stored API key.
type KeyRecord struct {
	ID            string     `json:"id"`
	WorkspaceID   string     `json:"workspace_id"`
	WorkspaceSlug string     `json:"workspace_slug"`
	WorkspaceName string     `json:"workspace_name"`
	KeyPrefix     string     `json:"key_prefix"`
	Name          string     `json:"name"`
	Role          string     `json:"role"`
	PrincipalType string     `json:"principal_type"`
	MembershipID  *string    `json:"membership_id,omitempty"`
	MemberEmail   *string    `json:"member_email,omitempty"`
	MemberName    *string    `json:"member_name,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	LastUsed      *time.Time `json:"last_used,omitempty"`
	Status        string     `json:"status"`
}

type WorkspaceRecord struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Status    string    `json:"status"`
}

type WorkspaceQuotaRecord struct {
	WorkspaceID         string    `json:"workspace_id"`
	MonthlyRequestLimit *int64    `json:"monthly_request_limit,omitempty"`
	MonthlyTokenLimit   *int64    `json:"monthly_token_limit,omitempty"`
	EnforceHardLimits   bool      `json:"enforce_hard_limits"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type WorkspaceMembershipRecord struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Role        string    `json:"role"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

type WorkspaceInvitationRecord struct {
	ID             string    `json:"id"`
	WorkspaceID    string    `json:"workspace_id"`
	Email          string    `json:"email"`
	DisplayName    string    `json:"display_name"`
	Role           string    `json:"role"`
	InvitedByKeyID string    `json:"invited_by_key_id"`
	CreatedAt      time.Time `json:"created_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	Status         string    `json:"status"`
}

type WorkspaceInvitationPreview struct {
	WorkspaceID   string    `json:"workspace_id"`
	WorkspaceSlug string    `json:"workspace_slug"`
	WorkspaceName string    `json:"workspace_name"`
	Email         string    `json:"email"`
	DisplayName   string    `json:"display_name"`
	Role          string    `json:"role"`
	ExpiresAt     time.Time `json:"expires_at"`
	Status        string    `json:"status"`
}

type WorkspaceProviderConfigRecord struct {
	WorkspaceID string            `json:"workspace_id"`
	Provider    string            `json:"provider"`
	Configured  bool              `json:"configured"`
	Endpoint    string            `json:"endpoint,omitempty"`
	Options     map[string]string `json:"options,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// Store is a SQLite-backed API key store.
type Store struct {
	db                       *sql.DB
	providerCredentialCipher *providerCredentialCipher
	lastUsedCh               chan string
	lastSeenCh               chan string
	shutdownCh               chan struct{}
	wg                       sync.WaitGroup
	closeOnce                sync.Once
}

var bootstrapKeyPattern = regexp.MustCompile(`^inf_[0-9a-fA-F]{48}$`)

const (
	DefaultWorkspaceID   = "ws_default"
	DefaultWorkspaceSlug = "default"
	DefaultWorkspaceName = "Default Workspace"
)

// NewStore opens a SQLite database and runs migrations.
func NewStore(dbPath string) (*Store, error) {
	return newStore(dbPath, nil)
}

// NewStoreWithProviderCredentialEncryption opens the auth store with AES-256-GCM
// encryption for workspace provider API credentials. encodedKey must be the
// base64 representation of exactly 32 random bytes.
func NewStoreWithProviderCredentialEncryption(dbPath, encodedKey string) (*Store, error) {
	credentialCipher, err := newProviderCredentialCipher(encodedKey)
	if err != nil {
		return nil, err
	}
	return newStore(dbPath, credentialCipher)
}

func newStore(dbPath string, credentialCipher *providerCredentialCipher) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open auth database: %w", err)
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to ping auth database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	s := &Store{
		db:                       db,
		providerCredentialCipher: credentialCipher,
		lastUsedCh:               make(chan string, 2048),
		lastSeenCh:               make(chan string, 2048),
		shutdownCh:               make(chan struct{}),
	}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to run auth migrations: %w", err)
	}
	if credentialCipher != nil {
		if err := s.encryptLegacyProviderCredentials(); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("failed to secure provider credentials: %w", err)
		}
	}
	s.startLastUsedUpdater()
	s.startSessionLastSeenUpdater()
	s.startSessionPruner()

	return s, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	s.closeOnce.Do(func() {
		close(s.shutdownCh)
	})
	s.wg.Wait()
	return s.db.Close()
}

func (s *Store) migrate() error {
	return migrate.Run(s.db, authMigrations)
}

// CreateKey generates a new API key and stores its hash.
// Returns the full key (only shown once) and the record.
func (s *Store) CreateKey(name, role string) (string, *KeyRecord, error) {
	return s.CreateKeyWithPrincipalInWorkspace(DefaultWorkspaceID, name, role, PrincipalHuman)
}

// CreateKeyInWorkspace generates a new API key scoped to a workspace and stores its hash.
// Returns the full key (only shown once) and the record.
func (s *Store) CreateKeyInWorkspace(workspaceID, name, role string) (string, *KeyRecord, error) {
	return s.CreateKeyWithPrincipalInWorkspace(workspaceID, name, role, PrincipalHuman)
}

func (s *Store) CreateKeyWithPrincipal(name, role, principalType string) (string, *KeyRecord, error) {
	return s.CreateKeyWithPrincipalInWorkspace(DefaultWorkspaceID, name, role, principalType)
}

func (s *Store) CreateKeyWithPrincipalInWorkspace(workspaceID, name, role, principalType string) (string, *KeyRecord, error) {
	if name == "" {
		return "", nil, fmt.Errorf("key name is required")
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		workspaceID = DefaultWorkspaceID
	}
	if role == "" {
		role = RoleUser
	}
	if !IsValidRole(role) {
		return "", nil, fmt.Errorf("invalid role %q", role)
	}
	if principalType == "" {
		principalType = PrincipalHuman
	}
	if !IsValidPrincipalType(principalType) {
		return "", nil, fmt.Errorf("invalid principal_type %q", principalType)
	}
	workspace, err := s.getWorkspace(workspaceID)
	if err != nil {
		return "", nil, err
	}

	// Generate random key: inf_ + 48 hex chars
	rawBytes := make([]byte, 24)
	if _, err := rand.Read(rawBytes); err != nil {
		return "", nil, fmt.Errorf("failed to generate key: %w", err)
	}
	fullKey := "inf_" + hex.EncodeToString(rawBytes)
	prefix := fullKey[:12] + "..."

	// Hash the key for storage
	hash := hashKey(fullKey)

	id := uuid.New().String()
	now := time.Now()

	_, err = s.db.Exec(`
		INSERT INTO api_keys (id, workspace_id, key_hash, key_prefix, name, role, principal_type, membership_id, created_at, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?, 'active')`,
		id, workspace.ID, hash, prefix, name, role, principalType, now,
	)
	if err != nil {
		return "", nil, fmt.Errorf("failed to store key: %w", err)
	}

	record := &KeyRecord{
		ID:            id,
		WorkspaceID:   workspace.ID,
		WorkspaceSlug: workspace.Slug,
		WorkspaceName: workspace.Name,
		KeyPrefix:     prefix,
		Name:          name,
		Role:          role,
		PrincipalType: principalType,
		CreatedAt:     now,
		Status:        "active",
	}

	return fullKey, record, nil
}

// ValidateKey checks a raw key against stored hashes.
// Returns the key record if valid, updates last_used.
func (s *Store) ValidateKey(rawKey string) (*KeyRecord, error) {
	hash := hashKey(rawKey)

	row := s.db.QueryRow(`
		SELECT k.id, k.workspace_id, w.slug, w.name, k.key_prefix, k.name,
		       COALESCE(m.role, k.role), k.principal_type, k.membership_id, m.email, m.display_name,
		       k.created_at, k.last_used, k.status
		FROM api_keys k
		JOIN workspaces w ON w.id = k.workspace_id
		LEFT JOIN workspace_memberships m ON m.id = k.membership_id
		WHERE k.key_hash = ? AND k.status = 'active'`,
		hash,
	)

	record := &KeyRecord{}
	err := row.Scan(
		&record.ID, &record.WorkspaceID, &record.WorkspaceSlug, &record.WorkspaceName,
		&record.KeyPrefix, &record.Name, &record.Role, &record.PrincipalType, &record.MembershipID, &record.MemberEmail, &record.MemberName,
		&record.CreatedAt, &record.LastUsed, &record.Status,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("invalid or revoked API key")
		}
		return nil, fmt.Errorf("failed to validate key: %w", err)
	}

	// Queue last_used updates for a single background updater.
	select {
	case <-s.shutdownCh:
		// Store is shutting down; skip best-effort update.
	case s.lastUsedCh <- record.ID:
	default:
		// Drop update if channel is full; this should not block request path.
	}

	return record, nil
}

// ListKeys returns all keys (prefix only, never the full key).
func (s *Store) ListKeys() ([]*KeyRecord, error) {
	return s.listKeys("")
}

func (s *Store) ListKeysByWorkspace(workspaceID string) ([]*KeyRecord, error) {
	return s.listKeys(strings.TrimSpace(workspaceID))
}

func (s *Store) listKeys(workspaceID string) ([]*KeyRecord, error) {
	query := `
		SELECT k.id, k.workspace_id, w.slug, w.name, k.key_prefix, k.name,
		       COALESCE(m.role, k.role), k.principal_type, k.membership_id, m.email, m.display_name,
		       k.created_at, k.last_used, k.status
		FROM api_keys k
		JOIN workspaces w ON w.id = k.workspace_id
		LEFT JOIN workspace_memberships m ON m.id = k.membership_id`
	args := []any{}
	if workspaceID != "" {
		query += " WHERE k.workspace_id = ?"
		args = append(args, workspaceID)
	}
	query += " ORDER BY k.created_at DESC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []*KeyRecord
	for rows.Next() {
		k := &KeyRecord{}
		if err := rows.Scan(
			&k.ID,
			&k.WorkspaceID,
			&k.WorkspaceSlug,
			&k.WorkspaceName,
			&k.KeyPrefix,
			&k.Name,
			&k.Role,
			&k.PrincipalType,
			&k.MembershipID,
			&k.MemberEmail,
			&k.MemberName,
			&k.CreatedAt,
			&k.LastUsed,
			&k.Status,
		); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	if keys == nil {
		keys = []*KeyRecord{}
	}
	return keys, rows.Err()
}

// RevokeKey soft-deletes a key by setting status to 'revoked'.
func (s *Store) RevokeKey(id string) error {
	return s.revokeKey(id, "")
}

func (s *Store) RevokeKeyInWorkspace(id, workspaceID string) error {
	return s.revokeKey(id, strings.TrimSpace(workspaceID))
}

func (s *Store) revokeKey(id, workspaceID string) error {
	query := "UPDATE api_keys SET status = 'revoked' WHERE id = ? AND status = 'active'"
	args := []any{id}
	if workspaceID != "" {
		query += " AND workspace_id = ?"
		args = append(args, workspaceID)
	}
	result, err := s.db.Exec(query, args...)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("key %s not found or already revoked", id)
	}
	return nil
}

// DeleteKey permanently removes a key.
func (s *Store) DeleteKey(id string) error {
	result, err := s.db.Exec("DELETE FROM api_keys WHERE id = ?", id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("key %s not found", id)
	}
	return nil
}

// Count returns the number of active keys.
func (s *Store) Count() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM api_keys WHERE status = 'active'").Scan(&count)
	return count, err
}

// CreateKeyFromRaw stores a pre-generated key (used for bootstrap admin key).
func (s *Store) CreateKeyFromRaw(fullKey, name, role string) (*KeyRecord, error) {
	return s.CreateKeyFromRawWithPrincipalInWorkspace(DefaultWorkspaceID, fullKey, name, role, PrincipalHuman)
}

// CreateKeyFromRawInWorkspace stores a pre-generated key for the given workspace.
func (s *Store) CreateKeyFromRawInWorkspace(workspaceID, fullKey, name, role string) (*KeyRecord, error) {
	return s.CreateKeyFromRawWithPrincipalInWorkspace(workspaceID, fullKey, name, role, PrincipalHuman)
}

func (s *Store) CreateKeyFromRawWithPrincipalInWorkspace(workspaceID, fullKey, name, role, principalType string) (*KeyRecord, error) {
	if !bootstrapKeyPattern.MatchString(fullKey) {
		return nil, fmt.Errorf("key must match inf_ followed by exactly 48 hexadecimal characters")
	}
	if name == "" {
		return nil, fmt.Errorf("key name is required")
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		workspaceID = DefaultWorkspaceID
	}
	if role == "" {
		role = RoleUser
	}
	if !IsValidRole(role) {
		return nil, fmt.Errorf("invalid role %q", role)
	}
	if principalType == "" {
		principalType = PrincipalHuman
	}
	if !IsValidPrincipalType(principalType) {
		return nil, fmt.Errorf("invalid principal_type %q", principalType)
	}
	workspace, err := s.getWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}

	prefix := fullKey[:12] + "..."
	hash := hashKey(fullKey)

	id := uuid.New().String()
	now := time.Now()

	_, err = s.db.Exec(`
		INSERT INTO api_keys (id, workspace_id, key_hash, key_prefix, name, role, principal_type, membership_id, created_at, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?, 'active')`,
		id, workspace.ID, hash, prefix, name, role, principalType, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to store key: %w", err)
	}

	return &KeyRecord{
		ID:            id,
		WorkspaceID:   workspace.ID,
		WorkspaceSlug: workspace.Slug,
		WorkspaceName: workspace.Name,
		KeyPrefix:     prefix,
		Name:          name,
		Role:          role,
		PrincipalType: principalType,
		CreatedAt:     now,
		Status:        "active",
	}, nil
}

// SessionRecord represents a stored session.
type SessionRecord struct {
	ID        string    `json:"id"`
	KeyID     string    `json:"key_id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	LastSeen  time.Time `json:"last_seen"`
}

const sessionDuration = 24 * time.Hour

// CreateSession creates a new session for the given key ID.
// Returns the raw token (to be set as cookie) and the session record.
func (s *Store) CreateSession(keyID string) (string, *SessionRecord, error) {
	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		return "", nil, fmt.Errorf("failed to generate session token: %w", err)
	}
	rawToken := hex.EncodeToString(rawBytes)
	tokenHash := hashKey(rawToken)

	id := uuid.New().String()
	now := time.Now()
	expiresAt := now.Add(sessionDuration)

	_, err := s.db.Exec(`
		INSERT INTO sessions (id, key_id, token_hash, created_at, expires_at, last_seen)
		VALUES (?, ?, ?, ?, ?, ?)`,
		id, keyID, tokenHash, now, expiresAt, now,
	)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create session: %w", err)
	}

	return rawToken, &SessionRecord{
		ID:        id,
		KeyID:     keyID,
		CreatedAt: now,
		ExpiresAt: expiresAt,
		LastSeen:  now,
	}, nil
}

// ValidateSession checks a raw session token.
// Returns the session and associated key record if valid.
func (s *Store) ValidateSession(rawToken string) (*SessionRecord, *KeyRecord, error) {
	tokenHash := hashKey(rawToken)

	row := s.db.QueryRow(`
		SELECT s.id, s.key_id, s.created_at, s.expires_at, s.last_seen,
		       k.id, k.workspace_id, w.slug, w.name, k.key_prefix, k.name,
		       COALESCE(m.role, k.role), k.principal_type, k.membership_id, m.email, m.display_name,
		       k.created_at, k.last_used, k.status
		FROM sessions s
		JOIN api_keys k ON s.key_id = k.id
		JOIN workspaces w ON w.id = k.workspace_id
		LEFT JOIN workspace_memberships m ON m.id = k.membership_id
		WHERE s.token_hash = ? AND s.expires_at > ? AND k.status = 'active'`,
		tokenHash, time.Now(),
	)

	session := &SessionRecord{}
	key := &KeyRecord{}
	err := row.Scan(
		&session.ID, &session.KeyID, &session.CreatedAt, &session.ExpiresAt, &session.LastSeen,
		&key.ID, &key.WorkspaceID, &key.WorkspaceSlug, &key.WorkspaceName, &key.KeyPrefix, &key.Name, &key.Role, &key.PrincipalType, &key.MembershipID, &key.MemberEmail, &key.MemberName, &key.CreatedAt, &key.LastUsed, &key.Status,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil, fmt.Errorf("invalid or expired session")
		}
		return nil, nil, fmt.Errorf("failed to validate session: %w", err)
	}

	select {
	case <-s.shutdownCh:
	case s.lastSeenCh <- session.ID:
	default:
	}

	return session, key, nil
}

// DeleteSession removes a session by ID.
func (s *Store) DeleteSession(id string) error {
	_, err := s.db.Exec("DELETE FROM sessions WHERE id = ?", id)
	return err
}

// DeleteSessionByToken removes a session by raw token.
func (s *Store) DeleteSessionByToken(rawToken string) error {
	tokenHash := hashKey(rawToken)
	_, err := s.db.Exec("DELETE FROM sessions WHERE token_hash = ?", tokenHash)
	return err
}

func hashKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

func (s *Store) CreateWorkspace(name string) (*WorkspaceRecord, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("workspace name is required")
	}
	slug := normalizeWorkspaceSlug(name)
	id := "ws_" + uuid.New().String()
	now := time.Now()

	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to begin workspace create transaction: %w", err)
	}

	_, err = tx.Exec(`
		INSERT INTO workspaces (id, slug, name, created_at, status)
		VALUES (?, ?, ?, ?, 'active')`,
		id, slug, name, now,
	)
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("failed to create workspace: %w", err)
	}
	if _, err := tx.Exec(`
		INSERT OR IGNORE INTO workspace_quotas (workspace_id, enforce_hard_limits, updated_at)
		VALUES (?, 1, ?)`,
		id, now,
	); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("failed to create workspace quota: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit workspace create: %w", err)
	}

	return &WorkspaceRecord{
		ID:        id,
		Slug:      slug,
		Name:      name,
		CreatedAt: now,
		Status:    "active",
	}, nil
}

func (s *Store) ListWorkspaces() ([]*WorkspaceRecord, error) {
	rows, err := s.db.Query(`
		SELECT id, slug, name, created_at, status
		FROM workspaces
		ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	workspaces := make([]*WorkspaceRecord, 0)
	for rows.Next() {
		w := &WorkspaceRecord{}
		if err := rows.Scan(&w.ID, &w.Slug, &w.Name, &w.CreatedAt, &w.Status); err != nil {
			return nil, err
		}
		workspaces = append(workspaces, w)
	}
	if workspaces == nil {
		workspaces = []*WorkspaceRecord{}
	}
	return workspaces, rows.Err()
}

func (s *Store) ListAccessibleWorkspaces(record *KeyRecord) ([]*WorkspaceRecord, error) {
	if record == nil || record.Status != "active" {
		return nil, fmt.Errorf("active key record is required")
	}

	if record.PrincipalType == PrincipalHuman && record.MemberEmail != nil {
		email := strings.ToLower(strings.TrimSpace(*record.MemberEmail))
		if email != "" {
			rows, err := s.db.Query(`
				SELECT DISTINCT w.id, w.slug, w.name, w.created_at, w.status
				FROM workspace_memberships m
				JOIN workspaces w ON w.id = m.workspace_id
				JOIN api_keys k ON k.membership_id = m.id
				WHERE lower(m.email) = ?
				  AND m.status = 'active'
				  AND k.status = 'active'
				  AND k.principal_type = ?
				ORDER BY w.created_at ASC`,
				email, PrincipalHuman,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to list accessible workspaces: %w", err)
			}
			defer rows.Close()

			workspaces := make([]*WorkspaceRecord, 0)
			for rows.Next() {
				workspace := &WorkspaceRecord{}
				if err := rows.Scan(&workspace.ID, &workspace.Slug, &workspace.Name, &workspace.CreatedAt, &workspace.Status); err != nil {
					return nil, fmt.Errorf("failed to scan accessible workspace: %w", err)
				}
				workspaces = append(workspaces, workspace)
			}
			if err := rows.Err(); err != nil {
				return nil, fmt.Errorf("failed to iterate accessible workspaces: %w", err)
			}
			if len(workspaces) > 0 {
				return workspaces, nil
			}
		}
	}

	workspace, err := s.getWorkspace(record.WorkspaceID)
	if err != nil {
		return nil, err
	}
	return []*WorkspaceRecord{workspace}, nil
}

func (s *Store) GetWorkspaceQuota(workspaceID string) (*WorkspaceQuotaRecord, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("workspace id is required")
	}
	if _, err := s.getWorkspace(workspaceID); err != nil {
		return nil, err
	}

	row := s.db.QueryRow(`
		SELECT workspace_id, monthly_request_limit, monthly_token_limit, enforce_hard_limits, updated_at
		FROM workspace_quotas
		WHERE workspace_id = ?`,
		workspaceID,
	)
	return scanWorkspaceQuota(row)
}

func (s *Store) SwitchSessionWorkspace(rawToken, workspaceID string) (*SessionRecord, *KeyRecord, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, nil, fmt.Errorf("workspace id is required")
	}

	session, currentKey, err := s.ValidateSession(rawToken)
	if err != nil {
		return nil, nil, err
	}
	if currentKey.WorkspaceID == workspaceID {
		return session, currentKey, nil
	}
	if currentKey.PrincipalType != PrincipalHuman {
		return nil, nil, fmt.Errorf("workspace switching is only available for human sessions")
	}
	if currentKey.MemberEmail == nil || strings.TrimSpace(*currentKey.MemberEmail) == "" {
		return nil, nil, fmt.Errorf("workspace switching is unavailable for this session")
	}

	targetKeyID, err := s.findSwitchableWorkspaceKeyID(*currentKey.MemberEmail, workspaceID)
	if err != nil {
		return nil, nil, err
	}

	if _, err := s.db.Exec(`UPDATE sessions SET key_id = ?, last_seen = ? WHERE id = ?`, targetKeyID, time.Now(), session.ID); err != nil {
		return nil, nil, fmt.Errorf("failed to switch session workspace: %w", err)
	}

	return s.ValidateSession(rawToken)
}

func (s *Store) findSwitchableWorkspaceKeyID(email, workspaceID string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	workspaceID = strings.TrimSpace(workspaceID)
	if email == "" || workspaceID == "" {
		return "", fmt.Errorf("email and workspace id are required")
	}

	var keyID string
	err := s.db.QueryRow(`
		SELECT k.id
		FROM workspace_memberships m
		JOIN api_keys k ON k.membership_id = m.id
		WHERE m.workspace_id = ?
		  AND lower(m.email) = ?
		  AND m.status = 'active'
		  AND k.status = 'active'
		  AND k.principal_type = ?
		ORDER BY k.created_at ASC
		LIMIT 1`,
		workspaceID, email, PrincipalHuman,
	).Scan(&keyID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("no accessible session key for workspace")
		}
		return "", fmt.Errorf("failed to locate switchable workspace key: %w", err)
	}

	return keyID, nil
}

func (s *Store) UpsertWorkspaceQuota(workspaceID string, monthlyRequestLimit, monthlyTokenLimit *int64, enforceHardLimits bool) (*WorkspaceQuotaRecord, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("workspace id is required")
	}
	if _, err := s.getWorkspace(workspaceID); err != nil {
		return nil, err
	}

	now := time.Now()
	_, err := s.db.Exec(`
		INSERT INTO workspace_quotas (workspace_id, monthly_request_limit, monthly_token_limit, enforce_hard_limits, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(workspace_id) DO UPDATE SET
			monthly_request_limit = excluded.monthly_request_limit,
			monthly_token_limit = excluded.monthly_token_limit,
			enforce_hard_limits = excluded.enforce_hard_limits,
			updated_at = excluded.updated_at`,
		workspaceID,
		toNullableInt64(monthlyRequestLimit),
		toNullableInt64(monthlyTokenLimit),
		boolToInt(enforceHardLimits),
		now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update workspace quota: %w", err)
	}
	return s.GetWorkspaceQuota(workspaceID)
}

func (s *Store) ListWorkspaceProviderConfigs(workspaceID string) ([]*WorkspaceProviderConfigRecord, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("workspace id is required")
	}
	if _, err := s.getWorkspace(workspaceID); err != nil {
		return nil, err
	}

	rows, err := s.db.Query(`
		SELECT workspace_id, provider, api_key, endpoint, options_json, created_at, updated_at
		FROM workspace_provider_configs
		WHERE workspace_id = ?
		ORDER BY provider ASC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []*WorkspaceProviderConfigRecord
	for rows.Next() {
		rec, err := scanWorkspaceProviderConfig(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	if records == nil {
		records = []*WorkspaceProviderConfigRecord{}
	}
	return records, rows.Err()
}

func (s *Store) GetWorkspaceProviderConfig(workspaceID, provider string) (*WorkspaceProviderConfigRecord, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	provider = strings.TrimSpace(provider)
	if workspaceID == "" || provider == "" {
		return nil, fmt.Errorf("workspace id and provider are required")
	}
	if _, err := s.getWorkspace(workspaceID); err != nil {
		return nil, err
	}

	row := s.db.QueryRow(`
		SELECT workspace_id, provider, api_key, endpoint, options_json, created_at, updated_at
		FROM workspace_provider_configs
		WHERE workspace_id = ? AND provider = ?`,
		workspaceID, provider,
	)
	return scanWorkspaceProviderConfig(row)
}

func (s *Store) ResolveWorkspaceProviderConfig(workspaceID, provider string) (apiKey, apiSecret, endpoint string, options map[string]string, err error) {
	workspaceID = strings.TrimSpace(workspaceID)
	provider = strings.TrimSpace(provider)
	if workspaceID == "" || provider == "" {
		return "", "", "", nil, fmt.Errorf("workspace id and provider are required")
	}
	if _, err := s.getWorkspace(workspaceID); err != nil {
		return "", "", "", nil, err
	}
	row := s.db.QueryRow(`
		SELECT api_key, api_secret, endpoint, options_json
		FROM workspace_provider_configs
		WHERE workspace_id = ? AND provider = ?`,
		workspaceID, provider,
	)
	var optionsJSON string
	if err := row.Scan(&apiKey, &apiSecret, &endpoint, &optionsJSON); err != nil {
		if err == sql.ErrNoRows {
			return "", "", "", nil, fmt.Errorf("%w: provider %s is not configured for workspace %s", ErrWorkspaceProviderConfigNotFound, provider, workspaceID)
		}
		return "", "", "", nil, err
	}
	if strings.TrimSpace(apiKey) == "" && strings.TrimSpace(apiSecret) == "" {
		return "", "", "", nil, fmt.Errorf("%w: provider %s is not configured for workspace %s", ErrWorkspaceProviderConfigNotFound, provider, workspaceID)
	}
	if s.providerCredentialCipher == nil {
		return "", "", "", nil, fmt.Errorf("provider credential encryption is not configured")
	}
	apiKey, err = s.providerCredentialCipher.decrypt(apiKey, workspaceID, provider, "api_key")
	if err != nil {
		return "", "", "", nil, fmt.Errorf("failed to decrypt provider %s API key for workspace %s: %w", provider, workspaceID, err)
	}
	apiSecret, err = s.providerCredentialCipher.decrypt(apiSecret, workspaceID, provider, "api_secret")
	if err != nil {
		return "", "", "", nil, fmt.Errorf("failed to decrypt provider %s API secret for workspace %s: %w", provider, workspaceID, err)
	}
	options, err = parseWorkspaceProviderOptions(optionsJSON)
	if err != nil {
		return "", "", "", nil, fmt.Errorf("invalid provider %s options for workspace %s: %w", provider, workspaceID, err)
	}
	return apiKey, apiSecret, endpoint, options, nil
}

func (s *Store) UpsertWorkspaceProviderConfig(workspaceID, provider, apiKey, apiSecret, endpoint string, options map[string]string) (*WorkspaceProviderConfigRecord, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	provider = strings.TrimSpace(provider)
	if workspaceID == "" || provider == "" {
		return nil, fmt.Errorf("workspace id and provider are required")
	}
	if _, err := s.getWorkspace(workspaceID); err != nil {
		return nil, err
	}
	apiKey = strings.TrimSpace(apiKey)
	apiSecret = strings.TrimSpace(apiSecret)
	if (apiKey != "" || apiSecret != "") && s.providerCredentialCipher == nil {
		return nil, fmt.Errorf("provider credential encryption is not configured")
	}
	var err error
	if s.providerCredentialCipher != nil {
		apiKey, err = s.providerCredentialCipher.encrypt(apiKey, workspaceID, provider, "api_key")
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt provider API key: %w", err)
		}
		apiSecret, err = s.providerCredentialCipher.encrypt(apiSecret, workspaceID, provider, "api_secret")
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt provider API secret: %w", err)
		}
	}
	optionsJSON, err := marshalWorkspaceProviderOptions(options)
	if err != nil {
		return nil, fmt.Errorf("failed to encode workspace provider options: %w", err)
	}
	now := time.Now()
	_, err = s.db.Exec(`
		INSERT INTO workspace_provider_configs (workspace_id, provider, api_key, api_secret, endpoint, options_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(workspace_id, provider) DO UPDATE SET
			api_key = excluded.api_key,
			api_secret = excluded.api_secret,
			endpoint = excluded.endpoint,
			options_json = excluded.options_json,
			updated_at = excluded.updated_at`,
		workspaceID,
		provider,
		apiKey,
		apiSecret,
		strings.TrimSpace(endpoint),
		optionsJSON,
		now,
		now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update workspace provider config: %w", err)
	}
	return s.GetWorkspaceProviderConfig(workspaceID, provider)
}

func (s *Store) encryptLegacyProviderCredentials() error {
	type storedCredential struct {
		workspaceID string
		provider    string
		apiKey      string
		apiSecret   string
	}
	rows, err := s.db.Query(`
		SELECT workspace_id, provider, api_key, api_secret
		FROM workspace_provider_configs`)
	if err != nil {
		return err
	}
	var credentials []storedCredential
	for rows.Next() {
		var credential storedCredential
		if err := rows.Scan(&credential.workspaceID, &credential.provider, &credential.apiKey, &credential.apiSecret); err != nil {
			_ = rows.Close()
			return err
		}
		credentials = append(credentials, credential)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, credential := range credentials {
		apiKey, keyChanged, err := s.secureStoredProviderCredential(credential.apiKey, credential.workspaceID, credential.provider, "api_key")
		if err != nil {
			return err
		}
		apiSecret, secretChanged, err := s.secureStoredProviderCredential(credential.apiSecret, credential.workspaceID, credential.provider, "api_secret")
		if err != nil {
			return err
		}
		if !keyChanged && !secretChanged {
			continue
		}
		if _, err := tx.Exec(`
			UPDATE workspace_provider_configs
			SET api_key = ?, api_secret = ?
			WHERE workspace_id = ? AND provider = ?`,
			apiKey, apiSecret, credential.workspaceID, credential.provider,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) secureStoredProviderCredential(value, workspaceID, provider, field string) (string, bool, error) {
	if value == "" {
		return "", false, nil
	}
	if strings.HasPrefix(value, providerCredentialCiphertextPrefix) {
		if _, err := s.providerCredentialCipher.decrypt(value, workspaceID, provider, field); err != nil {
			return "", false, err
		}
		return value, false, nil
	}
	encrypted, err := s.providerCredentialCipher.encrypt(value, workspaceID, provider, field)
	return encrypted, true, err
}

func (s *Store) DeleteWorkspaceProviderConfig(workspaceID, provider string) error {
	result, err := s.db.Exec(`
		DELETE FROM workspace_provider_configs
		WHERE workspace_id = ? AND provider = ?`,
		strings.TrimSpace(workspaceID), strings.TrimSpace(provider),
	)
	if err != nil {
		return fmt.Errorf("failed to delete workspace provider config: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("workspace provider config not found")
	}
	return nil
}

func (s *Store) getWorkspace(id string) (*WorkspaceRecord, error) {
	row := s.db.QueryRow(`
		SELECT id, slug, name, created_at, status
		FROM workspaces
		WHERE id = ? AND status = 'active'`,
		id,
	)
	w := &WorkspaceRecord{}
	if err := row.Scan(&w.ID, &w.Slug, &w.Name, &w.CreatedAt, &w.Status); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("workspace %s not found", id)
		}
		return nil, fmt.Errorf("failed to load workspace: %w", err)
	}
	return w, nil
}

func (s *Store) ListWorkspaceMemberships(workspaceID string) ([]*WorkspaceMembershipRecord, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("workspace id is required")
	}
	rows, err := s.db.Query(`
		SELECT id, workspace_id, email, display_name, role, status, created_at
		FROM workspace_memberships
		WHERE workspace_id = ?
		ORDER BY created_at ASC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*WorkspaceMembershipRecord
	for rows.Next() {
		m := &WorkspaceMembershipRecord{}
		if err := rows.Scan(&m.ID, &m.WorkspaceID, &m.Email, &m.DisplayName, &m.Role, &m.Status, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if out == nil {
		out = []*WorkspaceMembershipRecord{}
	}
	return out, rows.Err()
}

func (s *Store) GetWorkspaceMembership(workspaceID, membershipID string) (*WorkspaceMembershipRecord, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	membershipID = strings.TrimSpace(membershipID)
	if workspaceID == "" || membershipID == "" {
		return nil, fmt.Errorf("workspace id and membership id are required")
	}

	row := s.db.QueryRow(`
		SELECT id, workspace_id, email, display_name, role, status, created_at
		FROM workspace_memberships
		WHERE workspace_id = ? AND id = ?`,
		workspaceID, membershipID,
	)

	m := &WorkspaceMembershipRecord{}
	if err := row.Scan(&m.ID, &m.WorkspaceID, &m.Email, &m.DisplayName, &m.Role, &m.Status, &m.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("membership not found")
		}
		return nil, fmt.Errorf("failed to load membership: %w", err)
	}
	return m, nil
}

func (s *Store) UpdateWorkspaceMembershipRole(workspaceID, membershipID, role string) (*WorkspaceMembershipRecord, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	membershipID = strings.TrimSpace(membershipID)
	role = strings.TrimSpace(role)
	if workspaceID == "" || membershipID == "" {
		return nil, fmt.Errorf("workspace id and membership id are required")
	}
	if !IsValidRole(role) || role == RoleUser {
		return nil, fmt.Errorf("invalid membership role %q", role)
	}

	current, err := s.GetWorkspaceMembership(workspaceID, membershipID)
	if err != nil {
		return nil, err
	}
	if current.Status != "active" {
		return nil, fmt.Errorf("membership is not active")
	}
	if current.Role == RoleOwner && role != RoleOwner {
		var ownerCount int
		if err := s.db.QueryRow(`
			SELECT COUNT(1)
			FROM workspace_memberships
			WHERE workspace_id = ? AND role = ? AND status = 'active'`,
			workspaceID, RoleOwner,
		).Scan(&ownerCount); err != nil {
			return nil, fmt.Errorf("failed to count workspace owners: %w", err)
		}
		if ownerCount <= 1 {
			return nil, fmt.Errorf("cannot remove the last owner from a workspace")
		}
	}

	result, err := s.db.Exec(`
		UPDATE workspace_memberships
		SET role = ?
		WHERE workspace_id = ? AND id = ? AND status = 'active'`,
		role, workspaceID, membershipID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update membership role: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rows == 0 {
		return nil, fmt.Errorf("membership not found")
	}
	return s.GetWorkspaceMembership(workspaceID, membershipID)
}

func (s *Store) RemoveWorkspaceMembership(workspaceID, membershipID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	membershipID = strings.TrimSpace(membershipID)
	if workspaceID == "" || membershipID == "" {
		return fmt.Errorf("workspace id and membership id are required")
	}

	current, err := s.GetWorkspaceMembership(workspaceID, membershipID)
	if err != nil {
		return err
	}
	if current.Status != "active" {
		return fmt.Errorf("membership is not active")
	}
	if current.Role == RoleOwner {
		var ownerCount int
		if err := s.db.QueryRow(`
			SELECT COUNT(1)
			FROM workspace_memberships
			WHERE workspace_id = ? AND role = ? AND status = 'active'`,
			workspaceID, RoleOwner,
		).Scan(&ownerCount); err != nil {
			return fmt.Errorf("failed to count workspace owners: %w", err)
		}
		if ownerCount <= 1 {
			return fmt.Errorf("cannot remove the last owner from a workspace")
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin membership removal transaction: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.Exec(`
		UPDATE api_keys
		SET status = 'revoked'
		WHERE workspace_id = ? AND membership_id = ? AND status = 'active'`,
		workspaceID, membershipID,
	); err != nil {
		return fmt.Errorf("failed to revoke membership keys: %w", err)
	}

	result, err := tx.Exec(`
		UPDATE workspace_memberships
		SET status = 'removed'
		WHERE workspace_id = ? AND id = ? AND status = 'active'`,
		workspaceID, membershipID,
	)
	if err != nil {
		return fmt.Errorf("failed to remove membership: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("membership not found")
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit membership removal: %w", err)
	}
	tx = nil
	return nil
}

func (s *Store) CreateWorkspaceInvitation(workspaceID, email, displayName, role, invitedByKeyID string, expiresAt time.Time) (string, *WorkspaceInvitationRecord, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	email = strings.ToLower(strings.TrimSpace(email))
	displayName = strings.TrimSpace(displayName)
	invitedByKeyID = strings.TrimSpace(invitedByKeyID)
	if workspaceID == "" || email == "" || invitedByKeyID == "" {
		return "", nil, fmt.Errorf("workspace_id, email, and invited_by_key_id are required")
	}
	if !IsValidRole(role) || role == RoleUser {
		return "", nil, fmt.Errorf("invalid invitation role %q", role)
	}
	if displayName == "" {
		displayName = email
	}
	if _, err := s.getWorkspace(workspaceID); err != nil {
		return "", nil, err
	}
	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(7 * 24 * time.Hour)
	}

	var existing int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM workspace_memberships WHERE workspace_id = ? AND email = ? AND status = 'active'`, workspaceID, email).Scan(&existing); err != nil {
		return "", nil, fmt.Errorf("failed to check membership: %w", err)
	}
	if existing > 0 {
		return "", nil, fmt.Errorf("membership for %s already exists", email)
	}

	rawBytes := make([]byte, 24)
	if _, err := rand.Read(rawBytes); err != nil {
		return "", nil, fmt.Errorf("failed to generate invite token: %w", err)
	}
	rawToken := "invite_" + hex.EncodeToString(rawBytes)
	tokenHash := hashKey(rawToken)
	id := "inv_" + uuid.New().String()
	now := time.Now()

	_, err := s.db.Exec(`
		INSERT INTO workspace_invitations (id, workspace_id, email, display_name, role, invite_token_hash, invited_by_key_id, created_at, expires_at, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending')`,
		id, workspaceID, email, displayName, role, tokenHash, invitedByKeyID, now, expiresAt,
	)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create workspace invitation: %w", err)
	}

	return rawToken, &WorkspaceInvitationRecord{
		ID:             id,
		WorkspaceID:    workspaceID,
		Email:          email,
		DisplayName:    displayName,
		Role:           role,
		InvitedByKeyID: invitedByKeyID,
		CreatedAt:      now,
		ExpiresAt:      expiresAt,
		Status:         "pending",
	}, nil
}

func (s *Store) ListWorkspaceInvitations(workspaceID string) ([]*WorkspaceInvitationRecord, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("workspace id is required")
	}
	rows, err := s.db.Query(`
		SELECT id, workspace_id, email, display_name, role, invited_by_key_id, created_at, expires_at, status
		FROM workspace_invitations
		WHERE workspace_id = ?
		ORDER BY created_at DESC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*WorkspaceInvitationRecord
	for rows.Next() {
		inv := &WorkspaceInvitationRecord{}
		if err := rows.Scan(&inv.ID, &inv.WorkspaceID, &inv.Email, &inv.DisplayName, &inv.Role, &inv.InvitedByKeyID, &inv.CreatedAt, &inv.ExpiresAt, &inv.Status); err != nil {
			return nil, err
		}
		if inv.Status == "pending" && time.Now().After(inv.ExpiresAt) {
			inv.Status = "expired"
		}
		out = append(out, inv)
	}
	if out == nil {
		out = []*WorkspaceInvitationRecord{}
	}
	return out, rows.Err()
}

func (s *Store) RevokeWorkspaceInvitation(workspaceID, invitationID string) error {
	result, err := s.db.Exec(`
		UPDATE workspace_invitations
		SET status = 'revoked'
		WHERE id = ? AND workspace_id = ? AND status = 'pending'`,
		strings.TrimSpace(invitationID), strings.TrimSpace(workspaceID),
	)
	if err != nil {
		return fmt.Errorf("failed to revoke invitation: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("invitation not found")
	}
	return nil
}

func (s *Store) GetWorkspaceInvitationPreview(rawToken string) (*WorkspaceInvitationPreview, error) {
	tokenHash := hashKey(strings.TrimSpace(rawToken))
	if tokenHash == hashKey("") {
		return nil, fmt.Errorf("invitation token is required")
	}

	var preview WorkspaceInvitationPreview
	err := s.db.QueryRow(`
		SELECT wi.workspace_id, w.slug, w.name, wi.email, wi.display_name, wi.role, wi.expires_at, wi.status
		FROM workspace_invitations wi
		JOIN workspaces w ON w.id = wi.workspace_id
		WHERE wi.invite_token_hash = ?`,
		tokenHash,
	).Scan(
		&preview.WorkspaceID,
		&preview.WorkspaceSlug,
		&preview.WorkspaceName,
		&preview.Email,
		&preview.DisplayName,
		&preview.Role,
		&preview.ExpiresAt,
		&preview.Status,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("invalid invitation")
		}
		return nil, fmt.Errorf("failed to load invitation: %w", err)
	}
	if preview.DisplayName == "" {
		preview.DisplayName = preview.Email
	}
	if preview.Status != "pending" {
		return nil, fmt.Errorf("invitation is no longer pending")
	}
	if time.Now().After(preview.ExpiresAt) {
		return nil, fmt.Errorf("invitation has expired")
	}
	return &preview, nil
}

func (s *Store) AcceptWorkspaceInvitation(rawToken, displayName string) (*WorkspaceMembershipRecord, string, *KeyRecord, error) {
	tokenHash := hashKey(strings.TrimSpace(rawToken))
	displayName = strings.TrimSpace(displayName)
	now := time.Now()

	type inviteRow struct {
		ID          string
		WorkspaceID string
		Email       string
		DisplayName string
		Role        string
		Status      string
		ExpiresAt   time.Time
	}
	var invite inviteRow
	err := s.db.QueryRow(`
		SELECT id, workspace_id, email, display_name, role, status, expires_at
		FROM workspace_invitations
		WHERE invite_token_hash = ?`,
		tokenHash,
	).Scan(&invite.ID, &invite.WorkspaceID, &invite.Email, &invite.DisplayName, &invite.Role, &invite.Status, &invite.ExpiresAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, "", nil, fmt.Errorf("invalid invitation")
		}
		return nil, "", nil, fmt.Errorf("failed to load invitation: %w", err)
	}
	if invite.Status != "pending" {
		return nil, "", nil, fmt.Errorf("invitation is no longer pending")
	}
	if now.After(invite.ExpiresAt) {
		return nil, "", nil, fmt.Errorf("invitation has expired")
	}
	if displayName == "" {
		displayName = invite.DisplayName
	}
	if displayName == "" {
		displayName = invite.Email
	}
	workspace, err := s.getWorkspace(invite.WorkspaceID)
	if err != nil {
		return nil, "", nil, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, "", nil, fmt.Errorf("failed to begin accept invitation transaction: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	var existing int
	if err := tx.QueryRow(`SELECT COUNT(1) FROM workspace_memberships WHERE workspace_id = ? AND email = ? AND status = 'active'`, invite.WorkspaceID, invite.Email).Scan(&existing); err != nil {
		return nil, "", nil, fmt.Errorf("failed to check existing membership: %w", err)
	}
	if existing > 0 {
		return nil, "", nil, fmt.Errorf("membership for %s already exists", invite.Email)
	}

	membershipID := "mbr_" + uuid.New().String()
	if _, err := tx.Exec(`
		INSERT INTO workspace_memberships (id, workspace_id, email, display_name, role, status, created_at)
		VALUES (?, ?, ?, ?, ?, 'active', ?)`,
		membershipID, invite.WorkspaceID, invite.Email, displayName, invite.Role, now,
	); err != nil {
		return nil, "", nil, fmt.Errorf("failed to create membership: %w", err)
	}

	rawKeyBytes := make([]byte, 24)
	if _, err := rand.Read(rawKeyBytes); err != nil {
		return nil, "", nil, fmt.Errorf("failed to generate key: %w", err)
	}
	fullKey := "inf_" + hex.EncodeToString(rawKeyBytes)
	prefix := fullKey[:12] + "..."
	keyHash := hashKey(fullKey)
	keyID := uuid.New().String()

	if _, err := tx.Exec(`
		INSERT INTO api_keys (id, workspace_id, key_hash, key_prefix, name, role, principal_type, membership_id, created_at, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'active')`,
		keyID, invite.WorkspaceID, keyHash, prefix, displayName, invite.Role, PrincipalHuman, membershipID, now,
	); err != nil {
		return nil, "", nil, fmt.Errorf("failed to create key for invited member: %w", err)
	}

	if _, err := tx.Exec(`UPDATE workspace_invitations SET status = 'accepted' WHERE id = ?`, invite.ID); err != nil {
		return nil, "", nil, fmt.Errorf("failed to update invitation status: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, "", nil, fmt.Errorf("failed to commit invitation acceptance: %w", err)
	}
	tx = nil

	membership := &WorkspaceMembershipRecord{
		ID:          membershipID,
		WorkspaceID: invite.WorkspaceID,
		Email:       invite.Email,
		DisplayName: displayName,
		Role:        invite.Role,
		Status:      "active",
		CreatedAt:   now,
	}
	keyRecord := &KeyRecord{
		ID:            keyID,
		WorkspaceID:   invite.WorkspaceID,
		WorkspaceSlug: workspace.Slug,
		WorkspaceName: workspace.Name,
		KeyPrefix:     prefix,
		Name:          displayName,
		Role:          invite.Role,
		PrincipalType: PrincipalHuman,
		MembershipID:  &membershipID,
		MemberEmail:   &membership.Email,
		MemberName:    &membership.DisplayName,
		CreatedAt:     now,
		Status:        "active",
	}
	return membership, fullKey, keyRecord, nil
}

func normalizeWorkspaceSlug(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	lastDash := false
	for _, r := range name {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlphaNum {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteRune('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "workspace"
	}
	return slug
}

func scanWorkspaceQuota(row interface {
	Scan(dest ...any) error
}) (*WorkspaceQuotaRecord, error) {
	var quota WorkspaceQuotaRecord
	var requestLimit sql.NullInt64
	var tokenLimit sql.NullInt64
	var enforce int
	if err := row.Scan(&quota.WorkspaceID, &requestLimit, &tokenLimit, &enforce, &quota.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("workspace quota not found")
		}
		return nil, err
	}
	quota.MonthlyRequestLimit = nullableInt64Ptr(requestLimit)
	quota.MonthlyTokenLimit = nullableInt64Ptr(tokenLimit)
	quota.EnforceHardLimits = enforce != 0
	return &quota, nil
}

func scanWorkspaceProviderConfig(row interface {
	Scan(dest ...any) error
}) (*WorkspaceProviderConfigRecord, error) {
	var rec WorkspaceProviderConfigRecord
	var apiKey string
	var optionsJSON string
	if err := row.Scan(&rec.WorkspaceID, &rec.Provider, &apiKey, &rec.Endpoint, &optionsJSON, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("workspace provider config not found")
		}
		return nil, err
	}
	options, err := parseWorkspaceProviderOptions(optionsJSON)
	if err != nil {
		return nil, fmt.Errorf("invalid workspace provider config options: %w", err)
	}
	rec.Configured = strings.TrimSpace(apiKey) != ""
	rec.Options = options
	return &rec, nil
}

func marshalWorkspaceProviderOptions(options map[string]string) (string, error) {
	normalized := normalizeWorkspaceProviderOptions(options)
	if len(normalized) == 0 {
		return "{}", nil
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func parseWorkspaceProviderOptions(raw string) (map[string]string, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]string{}, nil
	}
	var options map[string]string
	if err := json.Unmarshal([]byte(raw), &options); err != nil {
		return nil, err
	}
	return normalizeWorkspaceProviderOptions(options), nil
}

func normalizeWorkspaceProviderOptions(options map[string]string) map[string]string {
	if len(options) == 0 {
		return map[string]string{}
	}
	normalized := make(map[string]string, len(options))
	for key, value := range options {
		trimmedKey := strings.TrimSpace(key)
		trimmedValue := strings.TrimSpace(value)
		if trimmedKey == "" || trimmedValue == "" {
			continue
		}
		normalized[trimmedKey] = trimmedValue
	}
	return normalized
}

func nullableInt64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	value := v.Int64
	return &value
}

func toNullableInt64(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (s *Store) startSessionPruner() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				result, err := s.db.Exec("DELETE FROM sessions WHERE expires_at <= ?", time.Now())
				if err != nil {
					slog.Warn("failed to prune expired sessions", slog.String("error", err.Error()))
				} else if n, _ := result.RowsAffected(); n > 0 {
					slog.Info("pruned expired sessions", slog.Int64("count", n))
				}
			case <-s.shutdownCh:
				return
			}
		}
	}()
}

func (s *Store) startLastUsedUpdater() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		pending := make(map[string]time.Time)
		flush := func() {
			if len(pending) == 0 {
				return
			}
			for id, ts := range pending {
				_, _ = s.db.Exec("UPDATE api_keys SET last_used = ? WHERE id = ?", ts, id)
			}
			pending = make(map[string]time.Time)
		}

		for {
			select {
			case id := <-s.lastUsedCh:
				pending[id] = time.Now()
			case <-ticker.C:
				flush()
			case <-s.shutdownCh:
				flush()
				return
			}
		}
	}()
}

func (s *Store) startSessionLastSeenUpdater() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		pending := make(map[string]struct{})
		flush := func() {
			if len(pending) == 0 {
				return
			}

			tx, err := s.db.Begin()
			if err != nil {
				slog.Warn("failed to begin session last_seen update transaction", slog.String("error", err.Error()))
				pending = make(map[string]struct{})
				return
			}

			stmt, err := tx.Prepare("UPDATE sessions SET last_seen = CURRENT_TIMESTAMP WHERE id = ?")
			if err != nil {
				_ = tx.Rollback()
				slog.Warn("failed to prepare session last_seen update", slog.String("error", err.Error()))
				pending = make(map[string]struct{})
				return
			}

			for id := range pending {
				if _, err := stmt.Exec(id); err != nil {
					slog.Warn("failed to update session last_seen", slog.String("session_id", id), slog.String("error", err.Error()))
				}
			}

			_ = stmt.Close()
			if err := tx.Commit(); err != nil {
				slog.Warn("failed to commit session last_seen updates", slog.String("error", err.Error()))
			}
			pending = make(map[string]struct{})
		}

		for {
			select {
			case id := <-s.lastSeenCh:
				pending[id] = struct{}{}
				if len(pending) >= 128 {
					flush()
				}
			case <-ticker.C:
				flush()
			case <-s.shutdownCh:
				flush()
				return
			}
		}
	}()
}
