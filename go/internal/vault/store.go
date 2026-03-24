// Package vault provides a SQLite-backed model registry for Infera.
package vault

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/infera/infera/go/internal/migrate"
	_ "github.com/mattn/go-sqlite3"
)

// vaultMigrations defines the versioned schema for the vault database.
var vaultMigrations = []migrate.Migration{
	{
		Version:     1,
		Description: "create models table",
		SQL: `
		CREATE TABLE IF NOT EXISTS models (
			id            TEXT PRIMARY KEY,
			name          TEXT NOT NULL,
			source        TEXT NOT NULL DEFAULT 'huggingface',
			source_uri    TEXT NOT NULL,
			parameters    TEXT NOT NULL DEFAULT '',
			quantization  TEXT NOT NULL DEFAULT 'none',
			vram_required INTEGER NOT NULL DEFAULT 0,
			max_context   INTEGER NOT NULL DEFAULT 4096,
			family        TEXT NOT NULL DEFAULT '',
			tags          TEXT NOT NULL DEFAULT '[]',
			metadata      TEXT NOT NULL DEFAULT '{}',
			status        TEXT NOT NULL DEFAULT 'available',
			created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_models_family ON models(family);
		CREATE INDEX IF NOT EXISTS idx_models_status ON models(status);
		CREATE INDEX IF NOT EXISTS idx_models_name ON models(name);`,
	},
}

// Model represents a model in the registry.
type Model struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Source       string            `json:"source"`
	SourceURI    string            `json:"source_uri"`
	Parameters   string            `json:"parameters"`
	Quantization string            `json:"quantization"`
	VRAMRequired int               `json:"vram_required"`
	MaxContext   int               `json:"max_context"`
	Family       string            `json:"family"`
	Tags         []string          `json:"tags"`
	Metadata     map[string]string `json:"metadata"`
	Status       string            `json:"status"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

// ModelFilter supports query filtering on models.
type ModelFilter struct {
	Family       string
	Status       string
	Quantization string
	MinVRAM      int
	MaxVRAM      int
	Tag          string
	Search       string
}

// Store wraps a SQLite database for model storage.
type Store struct {
	db *sql.DB
}

// ErrModelNotFound indicates the requested model does not exist in the registry.
var ErrModelNotFound = errors.New("model not found")

// NewStore opens a SQLite database and runs migrations.
func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return s, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	return migrate.Run(s.db, vaultMigrations)
}

// Create inserts a new model into the registry.
func (s *Store) Create(m *Model) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	if m.Source == "" {
		m.Source = "huggingface"
	}
	if m.Status == "" {
		m.Status = "available"
	}
	if m.Quantization == "" {
		m.Quantization = "none"
	}
	if m.MaxContext == 0 {
		m.MaxContext = 4096
	}
	if m.Tags == nil {
		m.Tags = []string{}
	}
	if m.Metadata == nil {
		m.Metadata = map[string]string{}
	}

	now := time.Now()
	m.CreatedAt = now
	m.UpdatedAt = now

	tagsJSON, err := json.Marshal(m.Tags)
	if err != nil {
		return fmt.Errorf("failed to marshal tags: %w", err)
	}
	metaJSON, err := json.Marshal(m.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	_, err = s.db.Exec(`
		INSERT INTO models (id, name, source, source_uri, parameters, quantization, vram_required, max_context, family, tags, metadata, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.Name, m.Source, m.SourceURI, m.Parameters, m.Quantization,
		m.VRAMRequired, m.MaxContext, m.Family, string(tagsJSON), string(metaJSON),
		m.Status, m.CreatedAt, m.UpdatedAt,
	)
	return err
}

// Get retrieves a model by ID.
func (s *Store) Get(id string) (*Model, error) {
	row := s.db.QueryRow(`SELECT id, name, source, source_uri, parameters, quantization, vram_required, max_context, family, tags, metadata, status, created_at, updated_at FROM models WHERE id = ?`, id)
	return scanModel(row)
}

// GetBySourceURI retrieves a model by its source URI.
func (s *Store) GetBySourceURI(sourceURI string) (*Model, error) {
	row := s.db.QueryRow(`SELECT id, name, source, source_uri, parameters, quantization, vram_required, max_context, family, tags, metadata, status, created_at, updated_at FROM models WHERE source_uri = ? LIMIT 1`, sourceURI)
	return scanModel(row)
}

// Update updates an existing model.
func (s *Store) Update(m *Model) error {
	m.UpdatedAt = time.Now()

	tagsJSON, err := json.Marshal(m.Tags)
	if err != nil {
		return fmt.Errorf("failed to marshal tags: %w", err)
	}
	metaJSON, err := json.Marshal(m.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	result, err := s.db.Exec(`
		UPDATE models SET name=?, source=?, source_uri=?, parameters=?, quantization=?, vram_required=?, max_context=?, family=?, tags=?, metadata=?, status=?, updated_at=?
		WHERE id=?`,
		m.Name, m.Source, m.SourceURI, m.Parameters, m.Quantization,
		m.VRAMRequired, m.MaxContext, m.Family, string(tagsJSON), string(metaJSON),
		m.Status, m.UpdatedAt, m.ID,
	)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("model %s not found", m.ID)
	}
	return nil
}

// Delete removes a model by ID.
func (s *Store) Delete(id string) error {
	result, err := s.db.Exec("DELETE FROM models WHERE id = ?", id)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("model %s not found", id)
	}
	return nil
}

// List returns models matching the given filter.
func (s *Store) List(f *ModelFilter) ([]*Model, error) {
	query := "SELECT id, name, source, source_uri, parameters, quantization, vram_required, max_context, family, tags, metadata, status, created_at, updated_at FROM models"
	var conditions []string
	var args []interface{}

	if f != nil {
		if f.Family != "" {
			conditions = append(conditions, "family = ?")
			args = append(args, f.Family)
		}
		if f.Status != "" {
			conditions = append(conditions, "status = ?")
			args = append(args, f.Status)
		}
		if f.Quantization != "" {
			conditions = append(conditions, "quantization = ?")
			args = append(args, f.Quantization)
		}
		if f.MinVRAM > 0 {
			conditions = append(conditions, "vram_required >= ?")
			args = append(args, f.MinVRAM)
		}
		if f.MaxVRAM > 0 {
			conditions = append(conditions, "vram_required <= ?")
			args = append(args, f.MaxVRAM)
		}
		if f.Tag != "" {
			conditions = append(conditions, "tags LIKE ?")
			args = append(args, "%\""+f.Tag+"\"%")
		}
		if f.Search != "" {
			conditions = append(conditions, "(name LIKE ? OR source_uri LIKE ?)")
			search := "%" + f.Search + "%"
			args = append(args, search, search)
		}
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY name ASC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var models []*Model
	for rows.Next() {
		m, err := scanModelRows(rows)
		if err != nil {
			return nil, err
		}
		models = append(models, m)
	}
	if models == nil {
		models = []*Model{}
	}
	return models, rows.Err()
}

// ListFamilies returns distinct model families.
func (s *Store) ListFamilies() ([]string, error) {
	rows, err := s.db.Query("SELECT DISTINCT family FROM models WHERE family != '' ORDER BY family ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var families []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, err
		}
		families = append(families, f)
	}
	if families == nil {
		families = []string{}
	}
	return families, rows.Err()
}

// Stats returns aggregate registry statistics.
type Stats struct {
	TotalModels      int `json:"total_models"`
	AvailableModels  int `json:"available_models"`
	DeprecatedModels int `json:"deprecated_models"`
	ModelFamilies    int `json:"model_families"`
}

func (s *Store) Stats() (*Stats, error) {
	st := &Stats{}

	err := s.db.QueryRow("SELECT COUNT(*) FROM models").Scan(&st.TotalModels)
	if err != nil {
		return nil, err
	}
	err = s.db.QueryRow("SELECT COUNT(*) FROM models WHERE status = 'available'").Scan(&st.AvailableModels)
	if err != nil {
		return nil, err
	}
	err = s.db.QueryRow("SELECT COUNT(*) FROM models WHERE status = 'deprecated'").Scan(&st.DeprecatedModels)
	if err != nil {
		return nil, err
	}
	err = s.db.QueryRow("SELECT COUNT(DISTINCT family) FROM models WHERE family != ''").Scan(&st.ModelFamilies)
	if err != nil {
		return nil, err
	}

	return st, nil
}

// Count returns total model count (used by seed to check if seeding is needed).
func (s *Store) Count() (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM models").Scan(&count)
	return count, err
}

// scanner interface for *sql.Row and *sql.Rows
type scanner interface {
	Scan(dest ...interface{}) error
}

func scanModel(row *sql.Row) (*Model, error) {
	m := &Model{}
	var tagsJSON, metaJSON string

	err := row.Scan(
		&m.ID, &m.Name, &m.Source, &m.SourceURI, &m.Parameters, &m.Quantization,
		&m.VRAMRequired, &m.MaxContext, &m.Family, &tagsJSON, &metaJSON,
		&m.Status, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrModelNotFound
		}
		return nil, err
	}

	if err := json.Unmarshal([]byte(tagsJSON), &m.Tags); err != nil {
		m.Tags = []string{}
	}
	if err := json.Unmarshal([]byte(metaJSON), &m.Metadata); err != nil {
		m.Metadata = map[string]string{}
	}

	return m, nil
}

func scanModelRows(rows *sql.Rows) (*Model, error) {
	m := &Model{}
	var tagsJSON, metaJSON string

	err := rows.Scan(
		&m.ID, &m.Name, &m.Source, &m.SourceURI, &m.Parameters, &m.Quantization,
		&m.VRAMRequired, &m.MaxContext, &m.Family, &tagsJSON, &metaJSON,
		&m.Status, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(tagsJSON), &m.Tags); err != nil {
		m.Tags = []string{}
	}
	if err := json.Unmarshal([]byte(metaJSON), &m.Metadata); err != nil {
		m.Metadata = map[string]string{}
	}

	return m, nil
}
