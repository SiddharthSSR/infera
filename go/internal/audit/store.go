package audit

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/infera/infera/go/internal/migrate"
	_ "github.com/jackc/pgx/v5/stdlib"
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
	{
		Version:     4,
		Description: "separate client correlation and add durable quota reservations",
		SQL: `
		ALTER TABLE inference_audit ADD COLUMN client_request_id TEXT NOT NULL DEFAULT '';
		CREATE INDEX IF NOT EXISTS idx_inference_audit_workspace_client_request
			ON inference_audit(workspace_id, client_request_id);
		CREATE TABLE IF NOT EXISTS quota_reservations (
			execution_id     TEXT PRIMARY KEY,
			workspace_id     TEXT NOT NULL,
			period_start_ms   INTEGER NOT NULL,
			period_end_ms     INTEGER NOT NULL,
			reserved_requests INTEGER NOT NULL,
			reserved_tokens   INTEGER NOT NULL,
			expires_at_ms     INTEGER NOT NULL,
			created_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_quota_reservations_workspace_period
			ON quota_reservations(workspace_id, period_start_ms, period_end_ms);
		CREATE INDEX IF NOT EXISTS idx_quota_reservations_expiry
			ON quota_reservations(expires_at_ms);`,
	},
}

var ErrQuotaExceeded = errors.New("workspace quota exceeded")

type Store struct {
	db      *sql.DB
	dialect ledgerDialect
}

type ledgerDialect string

const (
	dialectSQLite   ledgerDialect = "sqlite"
	dialectPostgres ledgerDialect = "postgres"
)

// Ledger is the durable audit and quota contract used by the gateway.
type Ledger interface {
	AppendInference(InferenceAuditRecord) error
	ReserveQuota(QuotaReservation) error
	UsageSummary(UsageSummaryQuery) (*UsageSummary, error)
	UsageByKey(UsageQuery) ([]UsageRow, error)
	Close() error
}

type InferenceAuditRecord struct {
	Timestamp        time.Time
	RequestID        string
	ClientRequestID  string
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

type QuotaReservation struct {
	ExecutionID         string
	WorkspaceID         string
	PeriodStart         time.Time
	PeriodEnd           time.Time
	ReservedRequests    int64
	ReservedTokens      int64
	MonthlyRequestLimit *int64
	MonthlyTokenLimit   *int64
	ExpiresAt           time.Time
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
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_txlock=immediate")
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
	return &Store{db: db, dialect: dialectSQLite}, nil
}

// NewPostgresStore opens the shared, multi-replica audit and quota ledger.
func NewPostgresStore(dsn string) (*Store, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("postgres audit ledger DSN is required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := migratePostgres(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db, dialect: dialectPostgres}, nil
}

// NewLedger selects the configured ledger without treating a filesystem path
// as a shared multi-replica database.
func NewLedger(backend, sqlitePath, postgresDSN string) (Ledger, error) {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "", "sqlite":
		return NewStore(sqlitePath)
	case "postgres", "postgresql":
		return NewPostgresStore(postgresDSN)
	default:
		return nil, fmt.Errorf("unsupported audit ledger backend %q", backend)
	}
}

func migratePostgres(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(4242424201)`); err != nil {
		return err
	}
	ddl := `
		CREATE TABLE IF NOT EXISTS audit_ledger_metadata (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS inference_audit (
			id BIGSERIAL PRIMARY KEY,
			ts_unix_ms BIGINT NOT NULL,
			request_id TEXT NOT NULL,
			client_request_id TEXT NOT NULL DEFAULT '',
			key_id TEXT NOT NULL,
			workspace_id TEXT NOT NULL,
			model TEXT NOT NULL,
			worker_id TEXT NOT NULL DEFAULT '',
			stream BIGINT NOT NULL DEFAULT 0,
			message_count BIGINT NOT NULL DEFAULT 0,
			prompt_tokens BIGINT NOT NULL DEFAULT 0,
			completion_tokens BIGINT NOT NULL DEFAULT 0,
			token_count BIGINT NOT NULL DEFAULT 0,
			token_source TEXT NOT NULL DEFAULT 'unknown',
			billable BIGINT NOT NULL DEFAULT 0,
			prompt_hash TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			error_code TEXT NOT NULL DEFAULT '',
			latency_ms BIGINT NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (workspace_id, request_id)
		);
		CREATE INDEX IF NOT EXISTS idx_inference_audit_ts ON inference_audit(ts_unix_ms);
		CREATE INDEX IF NOT EXISTS idx_inference_audit_key_ts ON inference_audit(key_id, ts_unix_ms);
		CREATE INDEX IF NOT EXISTS idx_inference_audit_model_ts ON inference_audit(model, ts_unix_ms);
		CREATE INDEX IF NOT EXISTS idx_inference_audit_workspace_ts ON inference_audit(workspace_id, ts_unix_ms);
		CREATE INDEX IF NOT EXISTS idx_inference_audit_workspace_client_request ON inference_audit(workspace_id, client_request_id);
		CREATE TABLE IF NOT EXISTS quota_reservations (
			execution_id TEXT PRIMARY KEY,
			workspace_id TEXT NOT NULL,
			period_start_ms BIGINT NOT NULL,
			period_end_ms BIGINT NOT NULL,
			reserved_requests BIGINT NOT NULL,
			reserved_tokens BIGINT NOT NULL,
			expires_at_ms BIGINT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_quota_reservations_workspace_period ON quota_reservations(workspace_id, period_start_ms, period_end_ms);
		CREATE INDEX IF NOT EXISTS idx_quota_reservations_expiry ON quota_reservations(expires_at_ms);
		INSERT INTO audit_ledger_metadata (key, value) VALUES ('schema_version', '4')
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
		INSERT INTO audit_ledger_metadata (key, value) VALUES ('writer_protocol', '1')
		ON CONFLICT (key) DO NOTHING;
	`
	for _, statement := range strings.Split(ddl, ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	var protocol string
	if err := tx.QueryRow(`SELECT value FROM audit_ledger_metadata WHERE key = 'writer_protocol'`).Scan(&protocol); err != nil {
		return err
	}
	if protocol != "1" {
		return fmt.Errorf("audit ledger writer protocol %q is incompatible with this gateway", protocol)
	}
	return tx.Commit()
}

var placeholderPattern = regexp.MustCompile(`\?`)

func (s *Store) bind(query string) string {
	if s.dialect != dialectPostgres {
		return query
	}
	n := 0
	return placeholderPattern.ReplaceAllStringFunc(query, func(string) string {
		n++
		return fmt.Sprintf("$%d", n)
	})
}

func (s *Store) lockExecution(tx *sql.Tx, executionID string) error {
	if s.dialect != dialectPostgres {
		return nil
	}
	_, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "execution:"+executionID)
	return err
}

func (s *Store) lockQuotaPeriod(tx *sql.Tx, workspaceID string, startMS, endMS int64) error {
	if s.dialect != dialectPostgres {
		return nil
	}
	key := fmt.Sprintf("quota:%s:%d:%d", workspaceID, startMS, endMS)
	_, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, key)
	return err
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

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.lockExecution(tx, rec.RequestID); err != nil {
		return err
	}

	result, err := tx.Exec(
		s.bind(`INSERT INTO inference_audit
		 (ts_unix_ms, request_id, client_request_id, key_id, workspace_id, model, worker_id, stream, message_count, prompt_tokens, completion_tokens, token_count, token_source, billable, prompt_hash, status, error_code, latency_ms)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(workspace_id, request_id) DO NOTHING`),
		ts.UnixMilli(),
		rec.RequestID,
		strings.TrimSpace(rec.ClientRequestID),
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
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		var existing InferenceAuditRecord
		var existingTimestampMS int64
		var existingStream, existingBillable int
		if err := tx.QueryRow(s.bind(`
			SELECT ts_unix_ms, client_request_id, key_id, model, worker_id, stream,
			       message_count, prompt_tokens, completion_tokens, token_count,
			       token_source, billable, prompt_hash, status, error_code, latency_ms
			FROM inference_audit WHERE workspace_id = ? AND request_id = ?`),
			workspaceID, rec.RequestID,
		).Scan(
			&existingTimestampMS, &existing.ClientRequestID, &existing.KeyID,
			&existing.Model, &existing.WorkerID, &existingStream,
			&existing.MessageCount, &existing.PromptTokens, &existing.CompletionTokens,
			&existing.TokenCount, &existing.TokenSource, &existingBillable,
			&existing.PromptHash, &existing.Status, &existing.ErrorCode, &existing.LatencyMS,
		); err != nil {
			return err
		}
		existing.Timestamp = time.UnixMilli(existingTimestampMS).UTC()
		existing.RequestID = rec.RequestID
		existing.WorkspaceID = workspaceID
		existing.Stream = existingStream == 1
		existing.Billable = existingBillable == 1
		candidate := rec
		candidate.Timestamp = ts.UTC().Truncate(time.Millisecond)
		candidate.ClientRequestID = strings.TrimSpace(candidate.ClientRequestID)
		candidate.KeyID = keyID
		candidate.WorkspaceID = workspaceID
		candidate.TokenCount = rec.TokenCount
		candidate.TokenSource = tokenSource
		candidate.Billable = billable == 1
		if existing != candidate {
			return fmt.Errorf("execution identity %q already records a different inference event", rec.RequestID)
		}
	}
	if _, err := tx.Exec(s.bind(`DELETE FROM quota_reservations WHERE execution_id = ?`), rec.RequestID); err != nil {
		return err
	}
	return tx.Commit()
}

// ReserveQuota atomically accounts for committed and in-flight usage before dispatch.
// Repeating the exact same execution identity is idempotent; conflicting reuse fails.
func (s *Store) ReserveQuota(res QuotaReservation) error {
	executionID := strings.TrimSpace(res.ExecutionID)
	workspaceID := strings.TrimSpace(res.WorkspaceID)
	if executionID == "" || workspaceID == "" {
		return fmt.Errorf("execution_id and workspace_id are required")
	}
	start := res.PeriodStart.UTC()
	end := res.PeriodEnd.UTC()
	if start.IsZero() || !start.Before(end) {
		return fmt.Errorf("invalid quota period")
	}
	if res.ReservedRequests < 0 || res.ReservedTokens < 0 {
		return fmt.Errorf("quota reservation values cannot be negative")
	}
	expiresAt := res.ExpiresAt.UTC()
	if expiresAt.IsZero() {
		return fmt.Errorf("expires_at is required")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.lockExecution(tx, executionID); err != nil {
		return err
	}
	if err := s.lockQuotaPeriod(tx, workspaceID, start.UnixMilli(), end.UnixMilli()); err != nil {
		return err
	}
	nowMS := time.Now().UTC().UnixMilli()
	if _, err := tx.Exec(s.bind(`DELETE FROM quota_reservations WHERE expires_at_ms <= ?`), nowMS); err != nil {
		return err
	}

	var existingWorkspace string
	var existingStart, existingEnd, existingRequests, existingTokens, existingExpiry int64
	err = tx.QueryRow(s.bind(`
		SELECT workspace_id, period_start_ms, period_end_ms, reserved_requests, reserved_tokens, expires_at_ms
		FROM quota_reservations WHERE execution_id = ?`), executionID,
	).Scan(&existingWorkspace, &existingStart, &existingEnd, &existingRequests, &existingTokens, &existingExpiry)
	switch {
	case err == nil:
		if existingWorkspace == workspaceID && existingStart == start.UnixMilli() &&
			existingEnd == end.UnixMilli() && existingRequests == res.ReservedRequests &&
			existingTokens == res.ReservedTokens {
			if expiresAt.UnixMilli() > existingExpiry {
				if _, err := tx.Exec(s.bind(`UPDATE quota_reservations SET expires_at_ms = ? WHERE execution_id = ?`), expiresAt.UnixMilli(), executionID); err != nil {
					return err
				}
			}
			return tx.Commit()
		}
		return fmt.Errorf("execution identity %q already has a different quota reservation", executionID)
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}

	var committedRequests, committedTokens int64
	if err := tx.QueryRow(s.bind(`
		SELECT
			COALESCE(SUM(CASE WHEN billable = 1 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN billable = 1 THEN token_count ELSE 0 END), 0)
		FROM inference_audit
		WHERE workspace_id = ? AND ts_unix_ms >= ? AND ts_unix_ms < ?`),
		workspaceID, start.UnixMilli(), end.UnixMilli(),
	).Scan(&committedRequests, &committedTokens); err != nil {
		return err
	}
	var pendingRequests, pendingTokens int64
	if err := tx.QueryRow(s.bind(`
		SELECT COALESCE(SUM(reserved_requests), 0), COALESCE(SUM(reserved_tokens), 0)
		FROM quota_reservations
		WHERE workspace_id = ? AND period_start_ms = ? AND period_end_ms = ? AND expires_at_ms > ?`),
		workspaceID, start.UnixMilli(), end.UnixMilli(), nowMS,
	).Scan(&pendingRequests, &pendingTokens); err != nil {
		return err
	}
	if quotaWouldExceed(res.MonthlyRequestLimit, committedRequests, pendingRequests, res.ReservedRequests) {
		return ErrQuotaExceeded
	}
	if quotaWouldExceed(res.MonthlyTokenLimit, committedTokens, pendingTokens, res.ReservedTokens) {
		return ErrQuotaExceeded
	}
	if _, err := tx.Exec(s.bind(`
		INSERT INTO quota_reservations
		(execution_id, workspace_id, period_start_ms, period_end_ms, reserved_requests, reserved_tokens, expires_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?)`),
		executionID, workspaceID, start.UnixMilli(), end.UnixMilli(),
		res.ReservedRequests, res.ReservedTokens, expiresAt.UnixMilli(),
	); err != nil {
		return err
	}
	return tx.Commit()
}

func quotaWouldExceed(limit *int64, committed, pending, requested int64) bool {
	if limit == nil {
		return false
	}
	if *limit < 0 || committed < 0 || pending < 0 || requested < 0 || committed > *limit {
		return true
	}
	remaining := *limit - committed
	if pending > remaining {
		return true
	}
	return requested > remaining-pending
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

	rows, err := s.db.Query(s.bind(sqlQuery), args...)
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
	if err := s.db.QueryRow(s.bind(query), args...).Scan(
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
