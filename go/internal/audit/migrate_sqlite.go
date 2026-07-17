package audit

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var sqliteMigrationColumns = []string{
	"id", "ts_unix_ms", "request_id", "client_request_id", "key_id", "workspace_id",
	"model", "worker_id", "stream", "message_count", "prompt_tokens",
	"completion_tokens", "token_count", "token_source", "billable",
	"prompt_hash", "status", "error_code", "latency_ms", "cost_provider",
	"cost_instance_id", "price_snapshot_version", "price_amount_nano",
	"price_currency", "price_time_unit", "price_captured_at_ms", "cost_nano",
	"cost_accuracy", "cost_attribution_method", "cost_observed_concurrency",
}

// MigrateSQLiteHistory copies immutable audit history into a PostgreSQL
// ledger. It is intentionally idempotent and excludes in-flight reservations;
// operators must drain gateways before running it.
func (s *Store) MigrateSQLiteHistory(ctx context.Context, sqlitePath string) (int64, error) {
	if s.dialect != dialectPostgres {
		return 0, fmt.Errorf("migration target must be a postgres audit ledger")
	}
	info, err := os.Stat(sqlitePath)
	if err != nil {
		return 0, fmt.Errorf("stat sqlite source: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return 0, fmt.Errorf("sqlite source must be an existing non-empty regular file")
	}
	source, err := openSQLiteMigrationSource(sqlitePath)
	if err != nil {
		return 0, fmt.Errorf("open sqlite source: %w", err)
	}
	defer source.Close()

	rows, err := source.QueryContext(ctx, `
		SELECT ts_unix_ms, request_id, client_request_id, key_id, workspace_id,
		       model, worker_id, stream, message_count, prompt_tokens,
		       completion_tokens, token_count, token_source, billable,
		       prompt_hash, status, error_code, latency_ms,
		       cost_provider, cost_instance_id, price_snapshot_version, price_amount_nano,
		       price_currency, price_time_unit, price_captured_at_ms, cost_nano,
		       cost_accuracy, cost_attribution_method, cost_observed_concurrency
		FROM inference_audit ORDER BY id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var copied int64
	for rows.Next() {
		var row inferenceAuditRow
		if err := rows.Scan(
			&row.TimestampMS, &row.RequestID, &row.ClientRequestID, &row.KeyID,
			&row.WorkspaceID, &row.Model, &row.WorkerID, &row.Stream,
			&row.MessageCount, &row.PromptTokens, &row.CompletionTokens,
			&row.TokenCount, &row.TokenSource, &row.Billable, &row.PromptHash,
			&row.Status, &row.ErrorCode, &row.LatencyMS, &row.CostProvider,
			&row.CostInstanceID, &row.PriceSnapshotVersion, &row.PriceAmountNano,
			&row.PriceCurrency, &row.PriceTimeUnit, &row.PriceCapturedAtMS,
			&row.CostNano, &row.CostAccuracy, &row.CostAttributionMethod,
			&row.CostObservedConcurrency,
		); err != nil {
			return copied, err
		}
		if err := s.appendInferenceRow(row, false); err != nil {
			return copied, fmt.Errorf("copy execution %q: %w", row.RequestID, err)
		}
		copied++
	}
	return copied, rows.Err()
}

// openSQLiteMigrationSource deliberately does not use NewStore. Migration
// sources are immutable evidence: opening one must not enable WAL, apply
// runtime migrations, or create/update schema_migrations.
func openSQLiteMigrationSource(sqlitePath string) (*sql.DB, error) {
	absolutePath, err := filepath.Abs(sqlitePath)
	if err != nil {
		return nil, fmt.Errorf("resolve sqlite migration source: %w", err)
	}
	if err := requireCheckpointedSQLiteMigrationSource(absolutePath); err != nil {
		return nil, err
	}
	dsn := (&url.URL{
		Scheme:   "file",
		Path:     absolutePath,
		RawQuery: url.Values{"mode": {"ro"}, "immutable": {"1"}, "_query_only": {"1"}}.Encode(),
	}).String()
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := validateSQLiteMigrationSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := requireCheckpointedSQLiteMigrationSource(absolutePath); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func requireCheckpointedSQLiteMigrationSource(sqlitePath string) error {
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(sqlitePath + suffix); err == nil {
			return fmt.Errorf("sqlite migration source must be a checkpointed immutable database without %s", suffix)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect sqlite migration source %s: %w", suffix, err)
		}
	}
	return nil
}

func validateSQLiteMigrationSchema(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(inference_audit)`)
	if err != nil {
		return fmt.Errorf("sqlite source schema incompatible: inspect inference_audit: %w", err)
	}
	defer rows.Close()

	available := make(map[string]struct{})
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return fmt.Errorf("sqlite source schema incompatible: inspect inference_audit columns: %w", err)
		}
		available[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("sqlite source schema incompatible: inspect inference_audit columns: %w", err)
	}

	missing := make([]string, 0)
	for _, column := range sqliteMigrationColumns {
		if _, ok := available[column]; !ok {
			missing = append(missing, column)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("sqlite source schema incompatible: inference_audit missing required columns: %s", strings.Join(missing, ", "))
	}
	return nil
}
