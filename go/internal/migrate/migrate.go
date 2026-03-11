// Package migrate provides a minimal versioned migration runner for SQLite.
//
// Each database keeps a schema_migrations table tracking which version has been
// applied. Migrations are plain SQL strings executed in order. Migrations that
// have already been applied (version <= current) are skipped.
package migrate

import (
	"database/sql"
	"fmt"
	"log/slog"
)

// Migration is a single schema change.
type Migration struct {
	Version     int
	Description string
	SQL         string
}

// Run applies any unapplied migrations to db in order.
// It creates the schema_migrations table if it doesn't exist.
func Run(db *sql.DB, migrations []Migration) error {
	if err := ensureMigrationsTable(db); err != nil {
		return fmt.Errorf("migrate: create tracking table: %w", err)
	}

	current, err := currentVersion(db)
	if err != nil {
		return fmt.Errorf("migrate: read current version: %w", err)
	}

	unapplied := make([]Migration, 0, len(migrations))
	for _, m := range migrations {
		if m.Version > current {
			unapplied = append(unapplied, m)
		}
	}
	for i := 1; i < len(unapplied); i++ {
		prev := unapplied[i-1]
		curr := unapplied[i]
		if curr.Version <= prev.Version {
			return fmt.Errorf(
				"migrate: unapplied migrations must be strictly increasing: v%d (%s) before v%d (%s)",
				prev.Version,
				prev.Description,
				curr.Version,
				curr.Description,
			)
		}
	}

	for _, m := range unapplied {

		slog.Info("migrate: applying",
			slog.Int("version", m.Version),
			slog.String("description", m.Description),
		)

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("migrate v%d: begin tx: %w", m.Version, err)
		}

		if _, err := tx.Exec(m.SQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migrate v%d (%s): %w", m.Version, m.Description, err)
		}

		if _, err := tx.Exec(
			"INSERT INTO schema_migrations (version, description) VALUES (?, ?)",
			m.Version, m.Description,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migrate v%d: record version: %w", m.Version, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migrate v%d: commit: %w", m.Version, err)
		}
	}

	return nil
}

func ensureMigrationsTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version     INTEGER PRIMARY KEY,
			description TEXT NOT NULL DEFAULT '',
			applied_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`)
	return err
}

func currentVersion(db *sql.DB) (int, error) {
	var v int
	err := db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&v)
	return v, err
}
