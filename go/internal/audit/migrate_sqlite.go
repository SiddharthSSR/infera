package audit

import (
	"context"
	"fmt"
	"os"
	"time"
)

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
	source, err := NewStore(sqlitePath)
	if err != nil {
		return 0, fmt.Errorf("open sqlite source: %w", err)
	}
	defer source.Close()

	rows, err := source.db.QueryContext(ctx, `
		SELECT ts_unix_ms, request_id, client_request_id, key_id, workspace_id,
		       model, worker_id, stream, message_count, prompt_tokens,
		       completion_tokens, token_count, token_source, billable,
		       prompt_hash, status, error_code, latency_ms
		FROM inference_audit ORDER BY id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var copied int64
	for rows.Next() {
		var rec InferenceAuditRecord
		var timestampMS int64
		var stream, billable int
		if err := rows.Scan(
			&timestampMS, &rec.RequestID, &rec.ClientRequestID, &rec.KeyID,
			&rec.WorkspaceID, &rec.Model, &rec.WorkerID, &stream,
			&rec.MessageCount, &rec.PromptTokens, &rec.CompletionTokens,
			&rec.TokenCount, &rec.TokenSource, &billable, &rec.PromptHash,
			&rec.Status, &rec.ErrorCode, &rec.LatencyMS,
		); err != nil {
			return copied, err
		}
		rec.Timestamp = timeFromUnixMilli(timestampMS)
		rec.Stream = stream == 1
		rec.Billable = billable == 1
		if err := s.AppendInference(rec); err != nil {
			return copied, fmt.Errorf("copy execution %q: %w", rec.RequestID, err)
		}
		copied++
	}
	return copied, rows.Err()
}

func timeFromUnixMilli(value int64) time.Time {
	return time.UnixMilli(value).UTC()
}
