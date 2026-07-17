package audit

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenSQLiteMigrationSourceIsImmutable(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "audit.db")
	source, err := NewStore(sourcePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := source.AppendInference(InferenceAuditRecord{
		Timestamp:   time.Date(2026, time.July, 17, 10, 0, 0, 0, time.UTC),
		RequestID:   "req_immutable",
		WorkspaceID: "ws_immutable",
		Model:       "model-1",
		Status:      "success",
		TokenCount:  12,
	}); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	if err := source.Close(); err != nil {
		t.Fatalf("close source: %v", err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(sourcePath + suffix); !os.IsNotExist(err) {
			t.Fatalf("expected closed source to have no %s sidecar, err=%v", suffix, err)
		}
	}
	if err := os.Chmod(sourcePath, 0o444); err != nil {
		t.Fatalf("make source read-only: %v", err)
	}

	before, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read source before migration open: %v", err)
	}
	beforeInfo, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatalf("stat source before migration open: %v", err)
	}

	readOnly, err := openSQLiteMigrationSource(sourcePath)
	if err != nil {
		t.Fatalf("openSQLiteMigrationSource: %v", err)
	}
	var requestID string
	if err := readOnly.QueryRowContext(context.Background(), `SELECT request_id FROM inference_audit`).Scan(&requestID); err != nil {
		t.Fatalf("read source content: %v", err)
	}
	if requestID != "req_immutable" {
		t.Fatalf("unexpected source row %q", requestID)
	}
	var migrationCount int
	if err := readOnly.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&migrationCount); err != nil {
		t.Fatalf("read source migration metadata: %v", err)
	}
	if migrationCount != len(auditMigrations) {
		t.Fatalf("migration metadata changed or incomplete: got %d want %d", migrationCount, len(auditMigrations))
	}
	if _, err := readOnly.Exec(`UPDATE inference_audit SET status = 'mutated'`); err == nil {
		t.Fatal("read-only migration source accepted a write")
	}
	if err := readOnly.Close(); err != nil {
		t.Fatalf("close read-only source: %v", err)
	}

	after, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read source after migration open: %v", err)
	}
	afterInfo, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatalf("stat source after migration open: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("migration source content or SQLite metadata changed")
	}
	if beforeInfo.Mode() != afterInfo.Mode() || !beforeInfo.ModTime().Equal(afterInfo.ModTime()) || beforeInfo.Size() != afterInfo.Size() {
		t.Fatalf("migration source filesystem metadata changed: before=%v after=%v", beforeInfo, afterInfo)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(sourcePath + suffix); !os.IsNotExist(err) {
			t.Fatalf("read-only migration open created %s sidecar, err=%v", suffix, err)
		}
	}
}

func TestOpenSQLiteMigrationSourceAcceptsRelativePath(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "audit.db")
	source, err := NewStore(sourcePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := source.Close(); err != nil {
		t.Fatalf("close source: %v", err)
	}

	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	relativePath, err := filepath.Rel(workingDirectory, sourcePath)
	if err != nil {
		t.Fatalf("make source path relative: %v", err)
	}
	readOnly, err := openSQLiteMigrationSource(relativePath)
	if err != nil {
		t.Fatalf("open relative SQLite migration source: %v", err)
	}
	defer readOnly.Close()
	var auditTables int
	if err := readOnly.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'inference_audit'`).Scan(&auditTables); err != nil {
		t.Fatalf("inspect relative SQLite migration source: %v", err)
	}
	if auditTables != 1 {
		t.Fatalf("expected inference_audit table, got %d", auditTables)
	}
}

func TestOpenSQLiteMigrationSourceRejectsIncompatibleSchemaWithoutMigrating(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite3", sourcePath)
	if err != nil {
		t.Fatalf("open legacy source: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE inference_audit (id INTEGER PRIMARY KEY, request_id TEXT NOT NULL)`); err != nil {
		t.Fatalf("create legacy source: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy source: %v", err)
	}

	_, err = openSQLiteMigrationSource(sourcePath)
	if err == nil || !strings.Contains(err.Error(), "sqlite source schema incompatible") || !strings.Contains(err.Error(), "workspace_id") {
		t.Fatalf("expected clear incompatible-schema error, got %v", err)
	}

	verify, err := sql.Open("sqlite3", "file:"+sourcePath+"?mode=ro")
	if err != nil {
		t.Fatalf("reopen legacy source: %v", err)
	}
	defer verify.Close()
	var trackingTables int
	if err := verify.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'`).Scan(&trackingTables); err != nil {
		t.Fatalf("inspect legacy source metadata: %v", err)
	}
	if trackingTables != 0 {
		t.Fatal("migration source opener created schema_migrations")
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(sourcePath + suffix); !os.IsNotExist(err) {
			t.Fatalf("incompatible source check created %s sidecar, err=%v", suffix, err)
		}
	}
}

func TestOpenSQLiteMigrationSourceRejectsUncheckpointedSidecars(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "audit.db")
	source, err := NewStore(sourcePath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := source.Close(); err != nil {
		t.Fatalf("close source: %v", err)
	}
	if err := os.WriteFile(sourcePath+"-wal", []byte("uncheckpointed"), 0o600); err != nil {
		t.Fatalf("create WAL evidence: %v", err)
	}

	_, err = openSQLiteMigrationSource(sourcePath)
	if err == nil || !strings.Contains(err.Error(), "checkpointed immutable database without -wal") {
		t.Fatalf("expected uncheckpointed WAL to fail clearly, got %v", err)
	}
	contents, readErr := os.ReadFile(sourcePath + "-wal")
	if readErr != nil || string(contents) != "uncheckpointed" {
		t.Fatalf("migration opener mutated WAL evidence: contents=%q err=%v", contents, readErr)
	}
}
