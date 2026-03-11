package migrate

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", "file::memory:?cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

func TestRunCreatesTable(t *testing.T) {
	db := openTestDB(t)
	if err := Run(db, nil); err != nil {
		t.Fatal(err)
	}

	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	if err != nil {
		t.Fatal("schema_migrations table should exist:", err)
	}
	if count != 0 {
		t.Errorf("expected 0 rows, got %d", count)
	}
}

func TestRunAppliesMigrations(t *testing.T) {
	db := openTestDB(t)

	migrations := []Migration{
		{Version: 1, Description: "create users", SQL: "CREATE TABLE users (id TEXT PRIMARY KEY, name TEXT NOT NULL)"},
		{Version: 2, Description: "add email", SQL: "ALTER TABLE users ADD COLUMN email TEXT DEFAULT ''"},
	}

	if err := Run(db, migrations); err != nil {
		t.Fatal(err)
	}

	// Verify table and column exist
	_, err := db.Exec("INSERT INTO users (id, name, email) VALUES ('1', 'test', 'test@example.com')")
	if err != nil {
		t.Fatal("migration should have created users table with email column:", err)
	}

	// Verify version tracking
	v, err := currentVersion(db)
	if err != nil {
		t.Fatal(err)
	}
	if v != 2 {
		t.Errorf("expected version 2, got %d", v)
	}
}

func TestRunSkipsApplied(t *testing.T) {
	db := openTestDB(t)

	m1 := []Migration{
		{Version: 1, Description: "create items", SQL: "CREATE TABLE items (id TEXT PRIMARY KEY)"},
	}
	if err := Run(db, m1); err != nil {
		t.Fatal(err)
	}

	// Run again with m1 + m2 — should only apply m2
	m2 := append(m1, Migration{
		Version: 2, Description: "add price", SQL: "ALTER TABLE items ADD COLUMN price REAL DEFAULT 0",
	})
	if err := Run(db, m2); err != nil {
		t.Fatal(err)
	}

	v, err := currentVersion(db)
	if err != nil {
		t.Fatal(err)
	}
	if v != 2 {
		t.Errorf("expected version 2, got %d", v)
	}
}

func TestRunFailsOnBadSQL(t *testing.T) {
	db := openTestDB(t)

	migrations := []Migration{
		{Version: 1, Description: "bad sql", SQL: "THIS IS NOT SQL"},
	}

	err := Run(db, migrations)
	if err == nil {
		t.Fatal("expected error for bad SQL")
	}

	// Version should still be 0 (rolled back)
	v, _ := currentVersion(db)
	if v != 0 {
		t.Errorf("expected version 0 after rollback, got %d", v)
	}
}

func TestRunIdempotent(t *testing.T) {
	db := openTestDB(t)

	migrations := []Migration{
		{Version: 1, Description: "create t", SQL: "CREATE TABLE t (id INTEGER PRIMARY KEY)"},
	}

	// Run twice — second run should be a no-op
	if err := Run(db, migrations); err != nil {
		t.Fatal(err)
	}
	if err := Run(db, migrations); err != nil {
		t.Fatal("second run should succeed (no-op):", err)
	}
}

func TestRunRejectsNonIncreasingUnappliedVersions(t *testing.T) {
	db := openTestDB(t)

	migrations := []Migration{
		{Version: 2, Description: "create t2", SQL: "CREATE TABLE t2 (id INTEGER PRIMARY KEY)"},
		{Version: 2, Description: "duplicate t2", SQL: "CREATE TABLE t2_dup (id INTEGER PRIMARY KEY)"},
	}

	err := Run(db, migrations)
	if err == nil {
		t.Fatal("expected error for duplicate unapplied versions")
	}
}

func TestRunRejectsOutOfOrderUnappliedVersions(t *testing.T) {
	db := openTestDB(t)

	migrations := []Migration{
		{Version: 3, Description: "create t3", SQL: "CREATE TABLE t3 (id INTEGER PRIMARY KEY)"},
		{Version: 1, Description: "create t1", SQL: "CREATE TABLE t1 (id INTEGER PRIMARY KEY)"},
	}

	err := Run(db, migrations)
	if err == nil {
		t.Fatal("expected error for out-of-order unapplied versions")
	}
}
