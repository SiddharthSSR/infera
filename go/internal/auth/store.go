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
}

// KeyRecord represents a stored API key.
type KeyRecord struct {
	ID        string     `json:"id"`
	KeyPrefix string     `json:"key_prefix"`
	Name      string     `json:"name"`
	Role      string     `json:"role"`
	CreatedAt time.Time  `json:"created_at"`
	LastUsed  *time.Time `json:"last_used,omitempty"`
	Status    string     `json:"status"`
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
	if name == "" {
		return "", nil, fmt.Errorf("key name is required")
	}
	if role == "" {
		role = "user"
	}
	if role != "admin" && role != "user" {
		return "", nil, fmt.Errorf("role must be 'admin' or 'user'")
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

	_, err := s.db.Exec(`
		INSERT INTO api_keys (id, key_hash, key_prefix, name, role, created_at, status)
		VALUES (?, ?, ?, ?, ?, ?, 'active')`,
		id, hash, prefix, name, role, now,
	)
	if err != nil {
		return "", nil, fmt.Errorf("failed to store key: %w", err)
	}

	record := &KeyRecord{
		ID:        id,
		KeyPrefix: prefix,
		Name:      name,
		Role:      role,
		CreatedAt: now,
		Status:    "active",
	}

	return fullKey, record, nil
}

// ValidateKey checks a raw key against stored hashes.
// Returns the key record if valid, updates last_used.
func (s *Store) ValidateKey(rawKey string) (*KeyRecord, error) {
	hash := hashKey(rawKey)

	row := s.db.QueryRow(`
		SELECT id, key_prefix, name, role, created_at, last_used, status
		FROM api_keys WHERE key_hash = ? AND status = 'active'`,
		hash,
	)

	record := &KeyRecord{}
	err := row.Scan(
		&record.ID, &record.KeyPrefix, &record.Name, &record.Role,
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
	rows, err := s.db.Query(`
		SELECT id, key_prefix, name, role, created_at, last_used, status
		FROM api_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []*KeyRecord
	for rows.Next() {
		k := &KeyRecord{}
		if err := rows.Scan(&k.ID, &k.KeyPrefix, &k.Name, &k.Role, &k.CreatedAt, &k.LastUsed, &k.Status); err != nil {
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
	result, err := s.db.Exec("UPDATE api_keys SET status = 'revoked' WHERE id = ? AND status = 'active'", id)
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
	if !bootstrapKeyPattern.MatchString(fullKey) {
		return nil, fmt.Errorf("key must match inf_ followed by exactly 48 hexadecimal characters")
	}
	if name == "" {
		return nil, fmt.Errorf("key name is required")
	}
	if role == "" {
		role = "user"
	}
	if role != "admin" && role != "user" {
		return nil, fmt.Errorf("role must be 'admin' or 'user'")
	}

	prefix := fullKey[:12] + "..."
	hash := hashKey(fullKey)

	id := uuid.New().String()
	now := time.Now()

	_, err := s.db.Exec(`
		INSERT INTO api_keys (id, key_hash, key_prefix, name, role, created_at, status)
		VALUES (?, ?, ?, ?, ?, ?, 'active')`,
		id, hash, prefix, name, role, now,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to store key: %w", err)
	}

	return &KeyRecord{
		ID:        id,
		KeyPrefix: prefix,
		Name:      name,
		Role:      role,
		CreatedAt: now,
		Status:    "active",
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
		       k.id, k.key_prefix, k.name, k.role, k.created_at, k.last_used, k.status
		FROM sessions s
		JOIN api_keys k ON s.key_id = k.id
		WHERE s.token_hash = ? AND s.expires_at > ? AND k.status = 'active'`,
		tokenHash, time.Now(),
	)

	session := &SessionRecord{}
	key := &KeyRecord{}
	err := row.Scan(
		&session.ID, &session.KeyID, &session.CreatedAt, &session.ExpiresAt, &session.LastSeen,
		&key.ID, &key.KeyPrefix, &key.Name, &key.Role, &key.CreatedAt, &key.LastUsed, &key.Status,
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
