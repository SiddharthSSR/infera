package audit

import (
	"context"
	"fmt"
	"os"
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
