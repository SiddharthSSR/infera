package audit

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/infera/infera/go/internal/migrate"
	_ "github.com/mattn/go-sqlite3"
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
}

type Store struct {
	db *sql.DB
}

type InferenceAuditRecord struct {
	Timestamp    time.Time
	RequestID    string
	KeyID        string
	Model        string
	WorkerID     string
	Stream       bool
	MessageCount int
	TokenCount   int
	PromptHash   string
	Status       string
	ErrorCode    string
	LatencyMS    int64
}

type UsageQuery struct {
	Start  time.Time
	End    time.Time
	Bucket string // "hour" or "day"
	KeyID  string
	Model  string
}

type UsageRow struct {
	BucketStartMS int64
	KeyID         string
	RequestCount  int64
	TokenCount    int64
	SuccessCount  int64
	ErrorCount    int64
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

	ts := rec.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	stream := 0
	if rec.Stream {
		stream = 1
	}

	_, err := s.db.Exec(
		`INSERT INTO inference_audit
		 (ts_unix_ms, request_id, key_id, model, worker_id, stream, message_count, token_count, prompt_hash, status, error_code, latency_ms)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ts.UnixMilli(),
		rec.RequestID,
		keyID,
		rec.Model,
		rec.WorkerID,
		stream,
		rec.MessageCount,
		rec.TokenCount,
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
		key_id,
		COUNT(*) AS request_count,
		COALESCE(SUM(token_count), 0) AS token_count,
		COALESCE(SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END), 0) AS success_count,
		COALESCE(SUM(CASE WHEN status <> 'success' THEN 1 ELSE 0 END), 0) AS error_count
	FROM inference_audit
	WHERE ts_unix_ms >= ? AND ts_unix_ms < ?`

	args := []any{bucketMS, bucketMS, start.UnixMilli(), end.UnixMilli()}
	if strings.TrimSpace(q.KeyID) != "" {
		sqlQuery += " AND key_id = ?"
		args = append(args, strings.TrimSpace(q.KeyID))
	}
	if strings.TrimSpace(q.Model) != "" {
		sqlQuery += " AND model = ?"
		args = append(args, strings.TrimSpace(q.Model))
	}

	sqlQuery += " GROUP BY bucket_start_ms, key_id ORDER BY bucket_start_ms ASC, key_id ASC"

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
			&row.KeyID,
			&row.RequestCount,
			&row.TokenCount,
			&row.SuccessCount,
			&row.ErrorCount,
		); err != nil {
			return nil, err
		}
		result = append(result, row)
	}

	return result, rows.Err()
}
