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

	CostAccuracyExact       = "exact"
	CostAccuracyEstimated   = "estimated"
	CostAccuracyUnavailable = "unavailable"

	CostMethodActiveInstanceTimeShareV1 = "active_instance_time_share_v1"
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
	{
		Version:     5,
		Description: "scope quota reservation identities by workspace",
		SQL: `
		CREATE TABLE quota_reservations_v5 (
			workspace_id      TEXT NOT NULL,
			execution_id      TEXT NOT NULL,
			period_start_ms    INTEGER NOT NULL,
			period_end_ms      INTEGER NOT NULL,
			reserved_requests INTEGER NOT NULL,
			reserved_tokens   INTEGER NOT NULL,
			expires_at_ms      INTEGER NOT NULL,
			created_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (workspace_id, execution_id)
		);
		INSERT INTO quota_reservations_v5
			(workspace_id, execution_id, period_start_ms, period_end_ms, reserved_requests, reserved_tokens, expires_at_ms, created_at)
		SELECT workspace_id, execution_id, period_start_ms, period_end_ms, reserved_requests, reserved_tokens, expires_at_ms, created_at
		FROM quota_reservations;
		DROP TABLE quota_reservations;
		ALTER TABLE quota_reservations_v5 RENAME TO quota_reservations;
		CREATE INDEX idx_quota_reservations_workspace_period
			ON quota_reservations(workspace_id, period_start_ms, period_end_ms);
		CREATE INDEX idx_quota_reservations_expiry
			ON quota_reservations(expires_at_ms);`,
	},
	{
		Version:     6,
		Description: "add immutable provider price snapshots and request cost attribution",
		SQL: `
		ALTER TABLE inference_audit ADD COLUMN cost_provider TEXT NOT NULL DEFAULT '';
		ALTER TABLE inference_audit ADD COLUMN cost_instance_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE inference_audit ADD COLUMN price_snapshot_version TEXT NOT NULL DEFAULT '';
		ALTER TABLE inference_audit ADD COLUMN price_amount_nano INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE inference_audit ADD COLUMN price_currency TEXT NOT NULL DEFAULT '';
		ALTER TABLE inference_audit ADD COLUMN price_time_unit TEXT NOT NULL DEFAULT '';
		ALTER TABLE inference_audit ADD COLUMN price_captured_at_ms INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE inference_audit ADD COLUMN cost_nano INTEGER NOT NULL DEFAULT 0;
		ALTER TABLE inference_audit ADD COLUMN cost_accuracy TEXT NOT NULL DEFAULT 'unavailable';
		ALTER TABLE inference_audit ADD COLUMN cost_attribution_method TEXT NOT NULL DEFAULT '';
		ALTER TABLE inference_audit ADD COLUMN cost_observed_concurrency INTEGER NOT NULL DEFAULT 0;`,
	},
}

var ErrQuotaExceeded = errors.New("workspace quota exceeded")

type Store struct {
	db                    *sql.DB
	dialect               ledgerDialect
	quotaSnapshotBarrier  *quotaSnapshotBarrier
	quotaAdmissionBarrier func(*sql.Tx) error
}

// quotaSnapshotBarrier is test-only synchronization for proving that both
// aggregates share one PostgreSQL statement snapshot during reconciliation.
type quotaSnapshotBarrier struct {
	reachedLockID int64
	releaseLockID int64
}

type ledgerDialect string

const (
	dialectSQLite   ledgerDialect = "sqlite"
	dialectPostgres ledgerDialect = "postgres"
)

const (
	postgresSchemaVersion  = "6"
	postgresWriterProtocol = "2"
)

type PostgresConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

func DefaultPostgresConfig() PostgresConfig {
	return PostgresConfig{MaxOpenConns: 20, MaxIdleConns: 5, ConnMaxLifetime: 30 * time.Minute}
}

func (c PostgresConfig) Validate() error {
	if c.MaxOpenConns < 1 {
		return errors.New("postgres max open connections must be positive")
	}
	if c.MaxIdleConns < 0 || c.MaxIdleConns > c.MaxOpenConns {
		return errors.New("postgres max idle connections must be between zero and max open connections")
	}
	if c.ConnMaxLifetime <= 0 {
		return errors.New("postgres connection max lifetime must be positive")
	}
	return nil
}

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
	Cost             CostAttribution
}

// CostAttribution is persisted as part of the immutable inference identity.
// PriceAmountNano is nanocurrency per PriceTimeUnit; CostNano is nanocurrency.
type CostAttribution struct {
	Provider                  string
	InstanceID                string
	PriceSnapshotVersion      string
	PriceAmountNano           int64
	PriceCurrency             string
	PriceTimeUnit             string
	PriceCapturedAt           time.Time
	CostNano                  int64
	CostAccuracy              string
	CostAttributionMethod     string
	ObservedActiveConcurrency int64
}

func UnavailableCostAttribution() CostAttribution {
	return CostAttribution{CostAccuracy: CostAccuracyUnavailable}
}

type inferenceAuditRow struct {
	TimestampMS             int64
	RequestID               string
	ClientRequestID         string
	KeyID                   string
	WorkspaceID             string
	Model                   string
	WorkerID                string
	Stream                  int64
	MessageCount            int64
	PromptTokens            int64
	CompletionTokens        int64
	TokenCount              int64
	TokenSource             string
	Billable                int64
	PromptHash              string
	Status                  string
	ErrorCode               string
	LatencyMS               int64
	CostProvider            string
	CostInstanceID          string
	PriceSnapshotVersion    string
	PriceAmountNano         int64
	PriceCurrency           string
	PriceTimeUnit           string
	PriceCapturedAtMS       int64
	CostNano                int64
	CostAccuracy            string
	CostAttributionMethod   string
	CostObservedConcurrency int64
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
	CostNano              int64
	CostedTokenCount      int64
	ExactCostCount        int64
	EstimatedCostCount    int64
	UnavailableCostCount  int64
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
	CostNano              int64 `json:"cost_nano"`
	CostedTokenCount      int64 `json:"costed_token_count"`
	ExactCostCount        int64 `json:"exact_cost_count"`
	EstimatedCostCount    int64 `json:"estimated_cost_count"`
	UnavailableCostCount  int64 `json:"unavailable_cost_count"`
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
	return NewPostgresStoreWithConfig(dsn, DefaultPostgresConfig())
}

func NewPostgresStoreWithConfig(dsn string, config PostgresConfig) (*Store, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("postgres audit ledger DSN is required")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(config.MaxOpenConns)
	db.SetMaxIdleConns(config.MaxIdleConns)
	db.SetConnMaxLifetime(config.ConnMaxLifetime)
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
	return NewLedgerWithPostgresConfig(backend, sqlitePath, postgresDSN, DefaultPostgresConfig())
}

func NewLedgerWithPostgresConfig(backend, sqlitePath, postgresDSN string, postgresConfig PostgresConfig) (Ledger, error) {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "", "sqlite":
		return NewStore(sqlitePath)
	case "postgres", "postgresql":
		return NewPostgresStoreWithConfig(postgresDSN, postgresConfig)
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
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS audit_ledger_metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		return err
	}
	var protocol string
	err = tx.QueryRow(`SELECT value FROM audit_ledger_metadata WHERE key = 'writer_protocol'`).Scan(&protocol)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		protocol = postgresWriterProtocol
	case err != nil:
		return err
	case protocol != "1" && protocol != postgresWriterProtocol:
		return fmt.Errorf("audit ledger writer protocol %q is incompatible with this gateway", protocol)
	}
	ddl := `
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
			cost_provider TEXT NOT NULL DEFAULT '',
			cost_instance_id TEXT NOT NULL DEFAULT '',
			price_snapshot_version TEXT NOT NULL DEFAULT '',
			price_amount_nano BIGINT NOT NULL DEFAULT 0,
			price_currency TEXT NOT NULL DEFAULT '',
			price_time_unit TEXT NOT NULL DEFAULT '',
			price_captured_at_ms BIGINT NOT NULL DEFAULT 0,
			cost_nano BIGINT NOT NULL DEFAULT 0,
			cost_accuracy TEXT NOT NULL DEFAULT 'unavailable',
			cost_attribution_method TEXT NOT NULL DEFAULT '',
			cost_observed_concurrency BIGINT NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE (workspace_id, request_id)
		);
		CREATE INDEX IF NOT EXISTS idx_inference_audit_ts ON inference_audit(ts_unix_ms);
		CREATE INDEX IF NOT EXISTS idx_inference_audit_key_ts ON inference_audit(key_id, ts_unix_ms);
		CREATE INDEX IF NOT EXISTS idx_inference_audit_model_ts ON inference_audit(model, ts_unix_ms);
		CREATE INDEX IF NOT EXISTS idx_inference_audit_workspace_ts ON inference_audit(workspace_id, ts_unix_ms);
		CREATE INDEX IF NOT EXISTS idx_inference_audit_workspace_client_request ON inference_audit(workspace_id, client_request_id);
		ALTER TABLE inference_audit ADD COLUMN IF NOT EXISTS cost_provider TEXT NOT NULL DEFAULT '';
		ALTER TABLE inference_audit ADD COLUMN IF NOT EXISTS cost_instance_id TEXT NOT NULL DEFAULT '';
		ALTER TABLE inference_audit ADD COLUMN IF NOT EXISTS price_snapshot_version TEXT NOT NULL DEFAULT '';
		ALTER TABLE inference_audit ADD COLUMN IF NOT EXISTS price_amount_nano BIGINT NOT NULL DEFAULT 0;
		ALTER TABLE inference_audit ADD COLUMN IF NOT EXISTS price_currency TEXT NOT NULL DEFAULT '';
		ALTER TABLE inference_audit ADD COLUMN IF NOT EXISTS price_time_unit TEXT NOT NULL DEFAULT '';
		ALTER TABLE inference_audit ADD COLUMN IF NOT EXISTS price_captured_at_ms BIGINT NOT NULL DEFAULT 0;
		ALTER TABLE inference_audit ADD COLUMN IF NOT EXISTS cost_nano BIGINT NOT NULL DEFAULT 0;
		ALTER TABLE inference_audit ADD COLUMN IF NOT EXISTS cost_accuracy TEXT NOT NULL DEFAULT 'unavailable';
		ALTER TABLE inference_audit ADD COLUMN IF NOT EXISTS cost_attribution_method TEXT NOT NULL DEFAULT '';
		ALTER TABLE inference_audit ADD COLUMN IF NOT EXISTS cost_observed_concurrency BIGINT NOT NULL DEFAULT 0;
		CREATE TABLE IF NOT EXISTS quota_reservations (
			workspace_id TEXT NOT NULL,
			execution_id TEXT NOT NULL,
			period_start_ms BIGINT NOT NULL,
			period_end_ms BIGINT NOT NULL,
			reserved_requests BIGINT NOT NULL,
			reserved_tokens BIGINT NOT NULL,
			expires_at_ms BIGINT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (workspace_id, execution_id)
		);
		CREATE INDEX IF NOT EXISTS idx_quota_reservations_workspace_period ON quota_reservations(workspace_id, period_start_ms, period_end_ms);
		CREATE INDEX IF NOT EXISTS idx_quota_reservations_expiry ON quota_reservations(expires_at_ms);
	`
	for _, statement := range strings.Split(ddl, ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	var primaryKeyName string
	if err := tx.QueryRow(`
		SELECT conname FROM pg_constraint
		WHERE conrelid = 'quota_reservations'::regclass AND contype = 'p'`).Scan(&primaryKeyName); err != nil {
		return err
	}
	var matchingColumns, totalColumns int
	if err := tx.QueryRow(`
		SELECT
			COUNT(*) FILTER (WHERE (key_column.ordinality = 1 AND attribute.attname = 'workspace_id')
			                      OR (key_column.ordinality = 2 AND attribute.attname = 'execution_id')),
			COUNT(*)
		FROM pg_constraint constraint_row
		CROSS JOIN LATERAL unnest(constraint_row.conkey) WITH ORDINALITY AS key_column(attnum, ordinality)
		JOIN pg_attribute attribute
		  ON attribute.attrelid = constraint_row.conrelid AND attribute.attnum = key_column.attnum
		WHERE constraint_row.conrelid = 'quota_reservations'::regclass AND constraint_row.contype = 'p'`).Scan(&matchingColumns, &totalColumns); err != nil {
		return err
	}
	if matchingColumns != 2 || totalColumns != 2 {
		quotedConstraint := `"` + strings.ReplaceAll(primaryKeyName, `"`, `""`) + `"`
		if _, err := tx.Exec(`ALTER TABLE quota_reservations DROP CONSTRAINT ` + quotedConstraint); err != nil {
			return err
		}
		if _, err := tx.Exec(`ALTER TABLE quota_reservations ADD PRIMARY KEY (workspace_id, execution_id)`); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`
		INSERT INTO audit_ledger_metadata (key, value) VALUES ('schema_version', $1)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, postgresSchemaVersion); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO audit_ledger_metadata (key, value) VALUES ('writer_protocol', $1)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, postgresWriterProtocol); err != nil {
		return err
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

func (s *Store) lockExecution(tx *sql.Tx, workspaceID, executionID string) error {
	if s.dialect != dialectPostgres {
		return nil
	}
	_, err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended($2, hashtextextended($1, 0)))`, workspaceID, executionID)
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
	cost := rec.Cost
	costAccuracy := strings.ToLower(strings.TrimSpace(cost.CostAccuracy))
	if costAccuracy == "" {
		costAccuracy = CostAccuracyUnavailable
	}
	switch costAccuracy {
	case CostAccuracyExact, CostAccuracyEstimated, CostAccuracyUnavailable:
	default:
		return fmt.Errorf("invalid cost_accuracy %q", cost.CostAccuracy)
	}
	if cost.PriceAmountNano < 0 || cost.CostNano < 0 || cost.ObservedActiveConcurrency < 0 {
		return fmt.Errorf("cost and price values cannot be negative")
	}
	if costAccuracy == CostAccuracyUnavailable {
		cost = UnavailableCostAttribution()
	} else if strings.TrimSpace(cost.PriceCurrency) == "" || strings.TrimSpace(cost.PriceTimeUnit) == "" ||
		strings.TrimSpace(cost.PriceSnapshotVersion) == "" || cost.PriceAmountNano <= 0 || cost.ObservedActiveConcurrency <= 0 {
		return fmt.Errorf("available cost attribution requires a versioned price with explicit units and positive concurrency")
	}
	billable := 0
	if rec.Billable || rec.Status == "success" {
		billable = 1
	}

	row := inferenceAuditRow{
		TimestampMS: ts.UnixMilli(), RequestID: rec.RequestID,
		ClientRequestID: strings.TrimSpace(rec.ClientRequestID), KeyID: keyID,
		WorkspaceID: workspaceID, Model: rec.Model, WorkerID: rec.WorkerID,
		Stream: int64(stream), MessageCount: int64(rec.MessageCount),
		PromptTokens: int64(rec.PromptTokens), CompletionTokens: int64(rec.CompletionTokens),
		TokenCount: int64(rec.TokenCount), TokenSource: tokenSource, Billable: int64(billable),
		PromptHash: rec.PromptHash, Status: rec.Status, ErrorCode: rec.ErrorCode, LatencyMS: rec.LatencyMS,
		CostProvider: strings.TrimSpace(cost.Provider), CostInstanceID: strings.TrimSpace(cost.InstanceID),
		PriceSnapshotVersion: strings.TrimSpace(cost.PriceSnapshotVersion), PriceAmountNano: cost.PriceAmountNano,
		PriceCurrency: strings.TrimSpace(cost.PriceCurrency), PriceTimeUnit: strings.TrimSpace(cost.PriceTimeUnit),
		PriceCapturedAtMS: cost.PriceCapturedAt.UTC().UnixMilli(), CostNano: cost.CostNano,
		CostAccuracy: costAccuracy, CostAttributionMethod: strings.TrimSpace(cost.CostAttributionMethod),
		CostObservedConcurrency: cost.ObservedActiveConcurrency,
	}
	if cost.PriceCapturedAt.IsZero() {
		row.PriceCapturedAtMS = 0
	}
	return s.appendInferenceRow(row, true)
}

func (s *Store) appendInferenceRow(row inferenceAuditRow, finalizeReservation bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.lockExecution(tx, row.WorkspaceID, row.RequestID); err != nil {
		return err
	}
	result, err := tx.Exec(
		s.bind(`INSERT INTO inference_audit
		 (ts_unix_ms, request_id, client_request_id, key_id, workspace_id, model, worker_id, stream, message_count, prompt_tokens, completion_tokens, token_count, token_source, billable, prompt_hash, status, error_code, latency_ms,
		  cost_provider, cost_instance_id, price_snapshot_version, price_amount_nano, price_currency, price_time_unit, price_captured_at_ms, cost_nano, cost_accuracy, cost_attribution_method, cost_observed_concurrency)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(workspace_id, request_id) DO NOTHING`),
		row.TimestampMS, row.RequestID, row.ClientRequestID, row.KeyID, row.WorkspaceID,
		row.Model, row.WorkerID, row.Stream, row.MessageCount, row.PromptTokens,
		row.CompletionTokens, row.TokenCount, row.TokenSource, row.Billable,
		row.PromptHash, row.Status, row.ErrorCode, row.LatencyMS,
		row.CostProvider, row.CostInstanceID, row.PriceSnapshotVersion, row.PriceAmountNano,
		row.PriceCurrency, row.PriceTimeUnit, row.PriceCapturedAtMS, row.CostNano,
		row.CostAccuracy, row.CostAttributionMethod, row.CostObservedConcurrency,
	)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		var existing inferenceAuditRow
		if err := tx.QueryRow(s.bind(`
			SELECT ts_unix_ms, client_request_id, key_id, model, worker_id, stream,
			       message_count, prompt_tokens, completion_tokens, token_count,
			       token_source, billable, prompt_hash, status, error_code, latency_ms,
			       cost_provider, cost_instance_id, price_snapshot_version, price_amount_nano,
			       price_currency, price_time_unit, price_captured_at_ms, cost_nano,
			       cost_accuracy, cost_attribution_method, cost_observed_concurrency
			FROM inference_audit WHERE workspace_id = ? AND request_id = ?`),
			row.WorkspaceID, row.RequestID,
		).Scan(
			&existing.TimestampMS, &existing.ClientRequestID, &existing.KeyID,
			&existing.Model, &existing.WorkerID, &existing.Stream,
			&existing.MessageCount, &existing.PromptTokens, &existing.CompletionTokens,
			&existing.TokenCount, &existing.TokenSource, &existing.Billable,
			&existing.PromptHash, &existing.Status, &existing.ErrorCode, &existing.LatencyMS,
			&existing.CostProvider, &existing.CostInstanceID, &existing.PriceSnapshotVersion,
			&existing.PriceAmountNano, &existing.PriceCurrency, &existing.PriceTimeUnit,
			&existing.PriceCapturedAtMS, &existing.CostNano, &existing.CostAccuracy,
			&existing.CostAttributionMethod, &existing.CostObservedConcurrency,
		); err != nil {
			return err
		}
		existing.RequestID = row.RequestID
		existing.WorkspaceID = row.WorkspaceID
		if existing != row {
			return fmt.Errorf("execution identity %q already records a different inference event in workspace %q", row.RequestID, row.WorkspaceID)
		}
	}
	if finalizeReservation {
		if _, err := tx.Exec(s.bind(`DELETE FROM quota_reservations WHERE workspace_id = ? AND execution_id = ?`), row.WorkspaceID, row.RequestID); err != nil {
			return err
		}
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
	if err := s.lockExecution(tx, workspaceID, executionID); err != nil {
		return err
	}
	if err := s.lockQuotaPeriod(tx, workspaceID, start.UnixMilli(), end.UnixMilli()); err != nil {
		return err
	}
	nowMS := time.Now().UTC().UnixMilli()
	if _, err := tx.Exec(s.bind(`
		DELETE FROM quota_reservations
		WHERE workspace_id = ? AND period_start_ms = ? AND period_end_ms = ? AND expires_at_ms <= ?`),
		workspaceID, start.UnixMilli(), end.UnixMilli(), nowMS); err != nil {
		return err
	}

	var existingStart, existingEnd, existingRequests, existingTokens, existingExpiry int64
	err = tx.QueryRow(s.bind(`
		SELECT period_start_ms, period_end_ms, reserved_requests, reserved_tokens, expires_at_ms
		FROM quota_reservations WHERE workspace_id = ? AND execution_id = ?`), workspaceID, executionID,
	).Scan(&existingStart, &existingEnd, &existingRequests, &existingTokens, &existingExpiry)
	if err == nil && existingExpiry <= nowMS {
		if _, deleteErr := tx.Exec(s.bind(`DELETE FROM quota_reservations WHERE workspace_id = ? AND execution_id = ?`), workspaceID, executionID); deleteErr != nil {
			return deleteErr
		}
		err = sql.ErrNoRows
	}
	switch {
	case err == nil:
		if existingStart == start.UnixMilli() && existingEnd == end.UnixMilli() && existingRequests == res.ReservedRequests &&
			existingTokens == res.ReservedTokens {
			if expiresAt.UnixMilli() > existingExpiry {
				if _, err := tx.Exec(s.bind(`UPDATE quota_reservations SET expires_at_ms = ? WHERE workspace_id = ? AND execution_id = ?`), expiresAt.UnixMilli(), workspaceID, executionID); err != nil {
					return err
				}
			}
			return tx.Commit()
		}
		return fmt.Errorf("execution identity %q already has a different quota reservation", executionID)
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}

	committedRequests, committedTokens, pendingRequests, pendingTokens, err := s.quotaUsageSnapshot(
		tx, workspaceID, start.UnixMilli(), end.UnixMilli(), nowMS,
	)
	if err != nil {
		return err
	}
	if quotaWouldExceed(res.MonthlyRequestLimit, committedRequests, pendingRequests, res.ReservedRequests) {
		return ErrQuotaExceeded
	}
	if quotaWouldExceed(res.MonthlyTokenLimit, committedTokens, pendingTokens, res.ReservedTokens) {
		return ErrQuotaExceeded
	}
	if s.quotaAdmissionBarrier != nil {
		if err := s.quotaAdmissionBarrier(tx); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(s.bind(`
		INSERT INTO quota_reservations
		(workspace_id, execution_id, period_start_ms, period_end_ms, reserved_requests, reserved_tokens, expires_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?)`),
		workspaceID, executionID, start.UnixMilli(), end.UnixMilli(),
		res.ReservedRequests, res.ReservedTokens, expiresAt.UnixMilli(),
	); err != nil {
		return err
	}
	return tx.Commit()
}

// quotaUsageSnapshot reads both sides of the reservation-to-audit transition
// in one statement. Under PostgreSQL READ COMMITTED, a statement has one MVCC
// snapshot, so an atomic AppendInference commit cannot disappear between the
// committed and pending aggregates.
func (s *Store) quotaUsageSnapshot(tx *sql.Tx, workspaceID string, startMS, endMS, nowMS int64) (int64, int64, int64, int64, error) {
	query := `
		WITH committed AS (
			SELECT
				COALESCE(SUM(CASE WHEN billable = 1 THEN 1 ELSE 0 END), 0) AS requests,
				COALESCE(SUM(CASE WHEN billable = 1 THEN token_count ELSE 0 END), 0) AS tokens
			FROM inference_audit
			WHERE workspace_id = ? AND ts_unix_ms >= ? AND ts_unix_ms < ?
		), pending AS (
			SELECT
				COALESCE(SUM(reserved_requests), 0) AS requests,
				COALESCE(SUM(reserved_tokens), 0) AS tokens
			FROM quota_reservations
			WHERE workspace_id = ? AND period_start_ms = ? AND period_end_ms = ? AND expires_at_ms > ?
		)
		SELECT committed.requests, committed.tokens, pending.requests, pending.tokens
		FROM committed, pending`
	args := []any{
		workspaceID, startMS, endMS,
		workspaceID, startMS, endMS, nowMS,
	}
	if s.dialect == dialectPostgres && s.quotaSnapshotBarrier != nil {
		query = `
			WITH committed AS MATERIALIZED (
				SELECT
					COALESCE(SUM(CASE WHEN billable = 1 THEN 1 ELSE 0 END), 0) AS requests,
					COALESCE(SUM(CASE WHEN billable = 1 THEN token_count ELSE 0 END), 0) AS tokens
				FROM inference_audit
				WHERE workspace_id = ? AND ts_unix_ms >= ? AND ts_unix_ms < ?
			), barrier AS MATERIALIZED (
				SELECT pg_advisory_xact_lock(?), pg_advisory_xact_lock(?) FROM committed
			), pending AS MATERIALIZED (
				SELECT
					COALESCE(SUM(reserved_requests), 0) AS requests,
					COALESCE(SUM(reserved_tokens), 0) AS tokens
				FROM quota_reservations, barrier
				WHERE workspace_id = ? AND period_start_ms = ? AND period_end_ms = ? AND expires_at_ms > ?
			)
			SELECT committed.requests, committed.tokens, pending.requests, pending.tokens
			FROM committed, pending`
		args = []any{
			workspaceID, startMS, endMS,
			s.quotaSnapshotBarrier.reachedLockID, s.quotaSnapshotBarrier.releaseLockID,
			workspaceID, startMS, endMS, nowMS,
		}
	}
	var committedRequests, committedTokens, pendingRequests, pendingTokens int64
	err := tx.QueryRow(s.bind(query), args...).Scan(
		&committedRequests, &committedTokens, &pendingRequests, &pendingTokens,
	)
	return committedRequests, committedTokens, pendingRequests, pendingTokens, err
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
		COALESCE(SUM(CASE WHEN status <> 'success' THEN 1 ELSE 0 END), 0) AS error_count,
		COALESCE(SUM(cost_nano), 0) AS cost_nano,
		COALESCE(SUM(CASE WHEN cost_accuracy <> 'unavailable' THEN token_count ELSE 0 END), 0) AS costed_token_count,
		COALESCE(SUM(CASE WHEN cost_accuracy = 'exact' THEN 1 ELSE 0 END), 0) AS exact_cost_count,
		COALESCE(SUM(CASE WHEN cost_accuracy = 'estimated' THEN 1 ELSE 0 END), 0) AS estimated_cost_count,
		COALESCE(SUM(CASE WHEN cost_accuracy = 'unavailable' THEN 1 ELSE 0 END), 0) AS unavailable_cost_count
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
			&row.CostNano,
			&row.CostedTokenCount,
			&row.ExactCostCount,
			&row.EstimatedCostCount,
			&row.UnavailableCostCount,
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
		COALESCE(SUM(CASE WHEN status <> 'success' THEN 1 ELSE 0 END), 0) AS error_count,
		COALESCE(SUM(cost_nano), 0) AS cost_nano,
		COALESCE(SUM(CASE WHEN cost_accuracy <> 'unavailable' THEN token_count ELSE 0 END), 0) AS costed_token_count,
		COALESCE(SUM(CASE WHEN cost_accuracy = 'exact' THEN 1 ELSE 0 END), 0) AS exact_cost_count,
		COALESCE(SUM(CASE WHEN cost_accuracy = 'estimated' THEN 1 ELSE 0 END), 0) AS estimated_cost_count,
		COALESCE(SUM(CASE WHEN cost_accuracy = 'unavailable' THEN 1 ELSE 0 END), 0) AS unavailable_cost_count
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
		&summary.CostNano,
		&summary.CostedTokenCount,
		&summary.ExactCostCount,
		&summary.EstimatedCostCount,
		&summary.UnavailableCostCount,
	); err != nil {
		return nil, err
	}
	return summary, nil
}
