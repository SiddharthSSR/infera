package audit

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/infera/infera/go/internal/migrate"
	_ "github.com/mattn/go-sqlite3"
)

const (
	TokenSourceExact     = "exact"
	TokenSourceEstimated = "estimated"
	TokenSourceMixed     = "mixed"
	TokenSourceUnknown   = "unknown"
)

var auditMigrations = []migrate.Migration{
	{
		Version:     1,
		Description: "create inference_audit table",
		SQL: `
		CREATE TABLE IF NOT EXISTS inference_audit (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			ts_unix_ms    INTEGER NOT NULL,
			request_id    TEXT NOT NULL,
			key_id        TEXT NOT NULL,
			model         TEXT NOT NULL,
			worker_id     TEXT NOT NULL DEFAULT '',
			stream        INTEGER NOT NULL DEFAULT 0,
			message_count INTEGER NOT NULL DEFAULT 0,
			token_count   INTEGER NOT NULL DEFAULT 0,
			prompt_hash   TEXT NOT NULL DEFAULT '',
			status        TEXT NOT NULL,
			error_code    TEXT NOT NULL DEFAULT '',
			latency_ms    INTEGER NOT NULL DEFAULT 0,
			created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_inference_audit_ts ON inference_audit(ts_unix_ms);
		CREATE INDEX IF NOT EXISTS idx_inference_audit_key_ts ON inference_audit(key_id, ts_unix_ms);
		CREATE INDEX IF NOT EXISTS idx_inference_audit_model_ts ON inference_audit(model, ts_unix_ms);`,
	},
	{
		Version:     2,
		Description: "add workspace scope to inference_audit",
		SQL: `
		ALTER TABLE inference_audit ADD COLUMN workspace_id TEXT NOT NULL DEFAULT 'ws_default';
		UPDATE inference_audit SET workspace_id = 'ws_default' WHERE workspace_id IS NULL OR workspace_id = '';
		CREATE INDEX IF NOT EXISTS idx_inference_audit_workspace_ts ON inference_audit(workspace_id, ts_unix_ms);`,
	},
	{
		Version:     3,
		Description: "add trustworthy usage attribution and idempotency",
		SQL: `
		ALTER TABLE inference_audit ADD COLUMN prompt_tokens INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE inference_audit ADD COLUMN completion_tokens INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE inference_audit ADD COLUMN token_source TEXT NOT NULL DEFAULT 'unknown';
		ALTER TABLE inference_audit ADD COLUMN billable INTEGER NOT NULL DEFAULT 0;
		UPDATE inference_audit SET billable = CASE WHEN status = 'success' THEN 1 ELSE 0 END;
		-- v2 assigned every legacy row to ws_default. Preserve colliding legacy
		-- events by giving later rows a stable synthetic request ID.
		UPDATE inference_audit
		SET request_id = request_id || '#legacy-row-' || id
		WHERE workspace_id = 'ws_default'
		  AND id NOT IN (
			SELECT MIN(id) FROM inference_audit
			WHERE workspace_id = 'ws_default'
			GROUP BY workspace_id, request_id
		  );
		DELETE FROM inference_audit
		WHERE id NOT IN (
			SELECT MIN(id) FROM inference_audit GROUP BY workspace_id, request_id
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_inference_audit_workspace_request
			ON inference_audit(workspace_id, request_id);`,
	},
}

type Store struct {
	db *sql.DB
}

type InferenceAuditRecord struct {
	Timestamp        time.Time
	RequestID        string
	KeyID            string
	WorkspaceID      string
	Model            string
	WorkerID         string
	Stream           bool
	MessageCount     int
	PromptTokens     int
	CompletionTokens int
	TokenCount       int
	TokenSource      string
	Billable         bool
	PromptHash       string
	Status           string
	ErrorCode        string
	LatencyMS        int64
}

type UsageQuery struct {
	Start       time.Time
	End         time.Time
	Bucket      string // "hour" or "day"
	KeyID       string
	WorkspaceID string
	Model       string
}

type UsageRow struct {
	BucketStartMS         int64
	WorkspaceID           string
	KeyID                 string
	AttemptCount          int64
	RequestCount          int64
	TokenCount            int64
	ExactRequestCount     int64
	EstimatedRequestCount int64
	ExactTokenCount       int64
	EstimatedTokenCount   int64
	SuccessCount          int64
	ErrorCount            int64
}

type UsageSummaryQuery struct {
	Start       time.Time
	End         time.Time
	WorkspaceID string
}

type UsageSummary struct {
	AttemptCount          int64 `json:"attempt_count"`
	RequestCount          int64 `json:"request_count"`
	TokenCount            int64 `json:"token_count"`
	ExactRequestCount     int64 `json:"exact_request_count"`
	EstimatedRequestCount int64 `json:"estimated_request_count"`
	ExactTokenCount       int64 `json:"exact_token_count"`
	EstimatedTokenCount   int64 `json:"estimated_token_count"`
	SuccessCount          int64 `json:"success_count"`
	ErrorCount            int64 `json:"error_count"`
}

func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := migrate.Run(db, auditMigrations); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// AppendInference persists one immutable inference audit event.
func (s *Store) AppendInference(rec InferenceAuditRecord) error {
	if strings.TrimSpace(rec.RequestID) == "" {
		return fmt.Errorf("request_id is required")
	}
	if strings.TrimSpace(rec.Model) == "" {
		return fmt.Errorf("model is required")
	}
	if strings.TrimSpace(rec.Status) == "" {
		return fmt.Errorf("status is required")
	}

	keyID := strings.TrimSpace(rec.KeyID)
	if keyID == "" {
		keyID = "anonymous"
	}
	workspaceID := strings.TrimSpace(rec.WorkspaceID)
	if workspaceID == "" {
		workspaceID = "ws_default"
	}

	ts := rec.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	stream := 0
	if rec.Stream {
		stream = 1
	}
	if rec.PromptTokens < 0 || rec.CompletionTokens < 0 || rec.TokenCount < 0 {
		return fmt.Errorf("token counts cannot be negative")
	}
	if rec.TokenCount == 0 {
		rec.TokenCount = rec.PromptTokens + rec.CompletionTokens
	}
	tokenSource := strings.ToLower(strings.TrimSpace(rec.TokenSource))
	if tokenSource == "" {
		tokenSource = "unknown"
	}
	switch tokenSource {
	case TokenSourceExact, TokenSourceEstimated, TokenSourceMixed, TokenSourceUnknown:
	default:
		return fmt.Errorf("invalid token_source %q", rec.TokenSource)
	}
	billable := 0
	if rec.Billable || rec.Status == "success" {
		billable = 1
	}

	_, err := s.db.Exec(
		`INSERT INTO inference_audit
		 (ts_unix_ms, request_id, key_id, workspace_id, model, worker_id, stream, message_count, prompt_tokens, completion_tokens, token_count, token_source, billable, prompt_hash, status, error_code, latency_ms)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(workspace_id, request_id) DO NOTHING`,
		ts.UnixMilli(),
		rec.RequestID,
		keyID,
		workspaceID,
		rec.Model,
		rec.WorkerID,
		stream,
		rec.MessageCount,
		rec.PromptTokens,
		rec.CompletionTokens,
		rec.TokenCount,
		tokenSource,
		billable,
		rec.PromptHash,
		rec.Status,
		rec.ErrorCode,
		rec.LatencyMS,
	)
	return err
}

func (s *Store) UsageByKey(q UsageQuery) ([]UsageRow, error) {
	bucket := strings.ToLower(strings.TrimSpace(q.Bucket))
	if bucket == "" {
		bucket = "day"
	}

	var bucketMS int64
	switch bucket {
	case "hour":
		bucketMS = int64(time.Hour / time.Millisecond)
	case "day":
		bucketMS = int64(24 * time.Hour / time.Millisecond)
	default:
		return nil, fmt.Errorf("invalid bucket %q", q.Bucket)
	}

	start := q.Start.UTC()
	if start.IsZero() {
		start = time.Now().UTC().Add(-24 * time.Hour)
	}
	end := q.End.UTC()
	if end.IsZero() {
		end = time.Now().UTC()
	}
	if !start.Before(end) {
		return nil, fmt.Errorf("start must be before end")
	}

	sqlQuery := `
	SELECT
		(ts_unix_ms / ?) * ? AS bucket_start_ms,
		workspace_id,
		key_id,
		COUNT(*) AS attempt_count,
		COALESCE(SUM(CASE WHEN billable = 1 THEN 1 ELSE 0 END), 0) AS request_count,
		COALESCE(SUM(CASE WHEN billable = 1 THEN token_count ELSE 0 END), 0) AS token_count,
		COALESCE(SUM(CASE WHEN billable = 1 AND token_source = 'exact' THEN 1 ELSE 0 END), 0) AS exact_request_count,
		COALESCE(SUM(CASE WHEN billable = 1 AND token_source <> 'exact' THEN 1 ELSE 0 END), 0) AS estimated_request_count,
		COALESCE(SUM(CASE WHEN billable = 1 AND token_source = 'exact' THEN token_count ELSE 0 END), 0) AS exact_token_count,
		COALESCE(SUM(CASE WHEN billable = 1 AND token_source <> 'exact' THEN token_count ELSE 0 END), 0) AS estimated_token_count,
		COALESCE(SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END), 0) AS success_count,
		COALESCE(SUM(CASE WHEN status <> 'success' THEN 1 ELSE 0 END), 0) AS error_count
	FROM inference_audit
	WHERE ts_unix_ms >= ? AND ts_unix_ms < ?`

	args := []any{bucketMS, bucketMS, start.UnixMilli(), end.UnixMilli()}
	if strings.TrimSpace(q.KeyID) != "" {
		sqlQuery += " AND key_id = ?"
		args = append(args, strings.TrimSpace(q.KeyID))
	}
	if strings.TrimSpace(q.WorkspaceID) != "" {
		sqlQuery += " AND workspace_id = ?"
		args = append(args, strings.TrimSpace(q.WorkspaceID))
	}
	if strings.TrimSpace(q.Model) != "" {
		sqlQuery += " AND model = ?"
		args = append(args, strings.TrimSpace(q.Model))
	}

	sqlQuery += " GROUP BY bucket_start_ms, workspace_id, key_id ORDER BY bucket_start_ms ASC, workspace_id ASC, key_id ASC"

	rows, err := s.db.Query(sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]UsageRow, 0)
	for rows.Next() {
		var row UsageRow
		if err := rows.Scan(
			&row.BucketStartMS,
			&row.WorkspaceID,
			&row.KeyID,
			&row.AttemptCount,
			&row.RequestCount,
			&row.TokenCount,
			&row.ExactRequestCount,
			&row.EstimatedRequestCount,
			&row.ExactTokenCount,
			&row.EstimatedTokenCount,
			&row.SuccessCount,
			&row.ErrorCount,
		); err != nil {
			return nil, err
		}
		result = append(result, row)
	}

	return result, rows.Err()
}

func (s *Store) UsageSummary(q UsageSummaryQuery) (*UsageSummary, error) {
	start := q.Start.UTC()
	if start.IsZero() {
		start = time.Now().UTC().Add(-24 * time.Hour)
	}
	end := q.End.UTC()
	if end.IsZero() {
		end = time.Now().UTC()
	}
	if !start.Before(end) {
		return nil, fmt.Errorf("start must be before end")
	}

	query := `
	SELECT
		COUNT(*) AS attempt_count,
		COALESCE(SUM(CASE WHEN billable = 1 THEN 1 ELSE 0 END), 0) AS request_count,
		COALESCE(SUM(CASE WHEN billable = 1 THEN token_count ELSE 0 END), 0) AS token_count,
		COALESCE(SUM(CASE WHEN billable = 1 AND token_source = 'exact' THEN 1 ELSE 0 END), 0) AS exact_request_count,
		COALESCE(SUM(CASE WHEN billable = 1 AND token_source <> 'exact' THEN 1 ELSE 0 END), 0) AS estimated_request_count,
		COALESCE(SUM(CASE WHEN billable = 1 AND token_source = 'exact' THEN token_count ELSE 0 END), 0) AS exact_token_count,
		COALESCE(SUM(CASE WHEN billable = 1 AND token_source <> 'exact' THEN token_count ELSE 0 END), 0) AS estimated_token_count,
		COALESCE(SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END), 0) AS success_count,
		COALESCE(SUM(CASE WHEN status <> 'success' THEN 1 ELSE 0 END), 0) AS error_count
	FROM inference_audit
	WHERE ts_unix_ms >= ? AND ts_unix_ms < ?`
	args := []any{start.UnixMilli(), end.UnixMilli()}
	if strings.TrimSpace(q.WorkspaceID) != "" {
		query += " AND workspace_id = ?"
		args = append(args, strings.TrimSpace(q.WorkspaceID))
	}

	summary := &UsageSummary{}
	if err := s.db.QueryRow(query, args...).Scan(
		&summary.AttemptCount,
		&summary.RequestCount,
		&summary.TokenCount,
		&summary.ExactRequestCount,
		&summary.EstimatedRequestCount,
		&summary.ExactTokenCount,
		&summary.EstimatedTokenCount,
		&summary.SuccessCount,
		&summary.ErrorCount,
	); err != nil {
		return nil, err
	}
	return summary, nil
}
