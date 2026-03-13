// Package auth provides API key authentication for Infera.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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

// Store is a SQLite-backed API key store.
type Store struct {
	db         *sql.DB
	lastUsedCh chan string
	lastSeenCh chan string
	shutdownCh chan struct{}
	wg         sync.WaitGroup
	closeOnce  sync.Once
}

var bootstrapKeyPattern = regexp.MustCompile(`^inf_[0-9a-fA-F]{48}$`)

const (
	DefaultWorkspaceID   = "ws_default"
	DefaultWorkspaceSlug = "default"
	DefaultWorkspaceName = "Default Workspace"
)

// NewStore opens a SQLite database and runs migrations.
func NewStore(dbPath string) (*Store, error) {
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
		db:         db,
		lastUsedCh: make(chan string, 2048),
		lastSeenCh: make(chan string, 2048),
		shutdownCh: make(chan struct{}),
	}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to run auth migrations: %w", err)
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
		INSERT INTO api_keys (id, workspace_id, key_hash, key_prefix, name, role, principal_type, created_at, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'active')`,
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
		SELECT k.id, k.workspace_id, w.slug, w.name, k.key_prefix, k.name, k.role, k.principal_type, k.created_at, k.last_used, k.status
		FROM api_keys k
		JOIN workspaces w ON w.id = k.workspace_id
		WHERE k.key_hash = ? AND k.status = 'active'`,
		hash,
	)

	record := &KeyRecord{}
	err := row.Scan(
		&record.ID, &record.WorkspaceID, &record.WorkspaceSlug, &record.WorkspaceName,
		&record.KeyPrefix, &record.Name, &record.Role, &record.PrincipalType,
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
		SELECT k.id, k.workspace_id, w.slug, w.name, k.key_prefix, k.name, k.role, k.principal_type, k.created_at, k.last_used, k.status
		FROM api_keys k
		JOIN workspaces w ON w.id = k.workspace_id`
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
		INSERT INTO api_keys (id, workspace_id, key_hash, key_prefix, name, role, principal_type, created_at, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'active')`,
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
		       k.id, k.workspace_id, w.slug, w.name, k.key_prefix, k.name, k.role, k.principal_type, k.created_at, k.last_used, k.status
		FROM sessions s
		JOIN api_keys k ON s.key_id = k.id
		JOIN workspaces w ON w.id = k.workspace_id
		WHERE s.token_hash = ? AND s.expires_at > ? AND k.status = 'active'`,
		tokenHash, time.Now(),
	)

	session := &SessionRecord{}
	key := &KeyRecord{}
	err := row.Scan(
		&session.ID, &session.KeyID, &session.CreatedAt, &session.ExpiresAt, &session.LastSeen,
		&key.ID, &key.WorkspaceID, &key.WorkspaceSlug, &key.WorkspaceName, &key.KeyPrefix, &key.Name, &key.Role, &key.PrincipalType, &key.CreatedAt, &key.LastUsed, &key.Status,
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
