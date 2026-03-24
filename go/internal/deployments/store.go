package deployments

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/infera/infera/go/internal/auth"
	"github.com/infera/infera/go/internal/migrate"
	"github.com/infera/infera/go/internal/providers"
	_ "github.com/mattn/go-sqlite3"
)

var deploymentMigrations = []migrate.Migration{
	{
		Version:     1,
		Description: "create deployment attempts table",
		SQL: `
		CREATE TABLE IF NOT EXISTS deployment_attempts (
			id                              TEXT PRIMARY KEY,
			workspace_id                    TEXT NOT NULL,
			created_by_key_id               TEXT NOT NULL DEFAULT '',
			created_at                      TEXT NOT NULL,
			updated_at                      TEXT NOT NULL,
			outcome                         TEXT NOT NULL,
			request_json                    TEXT NOT NULL,
			selected_model_name             TEXT NOT NULL DEFAULT '',
			instance_id                     TEXT NOT NULL DEFAULT '',
			instance_name                   TEXT NOT NULL DEFAULT '',
			failure_reason                  TEXT NOT NULL DEFAULT '',
			auto_verification_requested_at  TEXT NOT NULL DEFAULT '',
			verification_status             TEXT NOT NULL DEFAULT '',
			verification_verified_at        TEXT NOT NULL DEFAULT '',
			verification_latency_ms         INTEGER NOT NULL DEFAULT 0,
			verification_model              TEXT NOT NULL DEFAULT '',
			verification_response_preview   TEXT NOT NULL DEFAULT '',
			verification_error              TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS idx_deployment_attempts_workspace_updated
			ON deployment_attempts(workspace_id, updated_at DESC);
		CREATE INDEX IF NOT EXISTS idx_deployment_attempts_workspace_instance
			ON deployment_attempts(workspace_id, instance_id);
		CREATE INDEX IF NOT EXISTS idx_deployment_attempts_workspace_key
			ON deployment_attempts(workspace_id, created_by_key_id);`,
	},
}

type Store struct {
	db *sql.DB
}

type InferenceVerification struct {
	Status          string `json:"status"`
	VerifiedAt      time.Time `json:"verified_at"`
	LatencyMS       *int64 `json:"latency_ms,omitempty"`
	Model           string `json:"model,omitempty"`
	ResponsePreview string `json:"response_preview,omitempty"`
	Error           string `json:"error,omitempty"`
}

type AttemptRecord struct {
	ID                          string                 `json:"id"`
	WorkspaceID                 string                 `json:"workspace_id"`
	CreatedByKeyID              string                 `json:"created_by_key_id,omitempty"`
	CreatedAt                   time.Time              `json:"created_at"`
	UpdatedAt                   time.Time              `json:"updated_at"`
	Outcome                     string                 `json:"outcome"`
	Request                     providers.ProvisionRequest `json:"request"`
	SelectedModelName           string                 `json:"selected_model_name,omitempty"`
	InstanceID                  string                 `json:"instance_id,omitempty"`
	InstanceName                string                 `json:"instance_name,omitempty"`
	FailureReason               string                 `json:"failure_reason,omitempty"`
	AutoVerificationRequestedAt *time.Time             `json:"auto_verification_requested_at,omitempty"`
	InferenceVerification       *InferenceVerification `json:"inference_verification,omitempty"`
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
	if err := migrate.Run(db, deploymentMigrations); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func normalizeWorkspaceID(workspaceID string) string {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return auth.DefaultWorkspaceID
	}
	return workspaceID
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return 25
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func marshalRequest(req providers.ProvisionRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func parseTimestamp(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, nil
	}
	ts, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return ts, nil
	}
	return time.Parse(time.RFC3339, value)
}

func attemptFromRow(
	id string,
	workspaceID string,
	createdByKeyID string,
	createdAtRaw string,
	updatedAtRaw string,
	outcome string,
	requestRaw string,
	selectedModelName string,
	instanceID string,
	instanceName string,
	failureReason string,
	autoVerificationRequestedAtRaw string,
	verificationStatus string,
	verificationVerifiedAtRaw string,
	verificationLatencyMS int64,
	verificationModel string,
	verificationResponsePreview string,
	verificationError string,
) (*AttemptRecord, error) {
	var request providers.ProvisionRequest
	if err := json.Unmarshal([]byte(requestRaw), &request); err != nil {
		return nil, err
	}

	createdAt, err := parseTimestamp(createdAtRaw)
	if err != nil {
		return nil, err
	}
	updatedAt, err := parseTimestamp(updatedAtRaw)
	if err != nil {
		return nil, err
	}

	var autoVerificationRequestedAt *time.Time
	if strings.TrimSpace(autoVerificationRequestedAtRaw) != "" {
		ts, err := parseTimestamp(autoVerificationRequestedAtRaw)
		if err != nil {
			return nil, err
		}
		autoVerificationRequestedAt = &ts
	}

	var verification *InferenceVerification
	if strings.TrimSpace(verificationStatus) != "" {
		ts, err := parseTimestamp(verificationVerifiedAtRaw)
		if err != nil {
			return nil, err
		}
		verification = &InferenceVerification{
			Status:          verificationStatus,
			VerifiedAt:      ts,
			Model:           verificationModel,
			ResponsePreview: verificationResponsePreview,
			Error:           verificationError,
		}
		if verificationLatencyMS > 0 {
			latency := verificationLatencyMS
			verification.LatencyMS = &latency
		}
	}

	return &AttemptRecord{
		ID:                          id,
		WorkspaceID:                 workspaceID,
		CreatedByKeyID:              createdByKeyID,
		CreatedAt:                   createdAt,
		UpdatedAt:                   updatedAt,
		Outcome:                     outcome,
		Request:                     request,
		SelectedModelName:           selectedModelName,
		InstanceID:                  instanceID,
		InstanceName:                instanceName,
		FailureReason:               failureReason,
		AutoVerificationRequestedAt: autoVerificationRequestedAt,
		InferenceVerification:       verification,
	}, nil
}

func (s *Store) getAttempt(workspaceID, attemptID string) (*AttemptRecord, error) {
	row := s.db.QueryRow(
		`SELECT
			id,
			workspace_id,
			created_by_key_id,
			created_at,
			updated_at,
			outcome,
			request_json,
			selected_model_name,
			instance_id,
			instance_name,
			failure_reason,
			auto_verification_requested_at,
			verification_status,
			verification_verified_at,
			verification_latency_ms,
			verification_model,
			verification_response_preview,
			verification_error
		FROM deployment_attempts
		WHERE workspace_id = ? AND id = ?`,
		normalizeWorkspaceID(workspaceID),
		strings.TrimSpace(attemptID),
	)

	var (
		id                          string
		wsID                        string
		createdByKeyID              string
		createdAtRaw                string
		updatedAtRaw                string
		outcome                     string
		requestRaw                  string
		selectedModelName           string
		instanceID                  string
		instanceName                string
		failureReason               string
		autoVerificationRequestedAt string
		verificationStatus          string
		verificationVerifiedAt      string
		verificationLatencyMS       int64
		verificationModel           string
		verificationResponsePreview string
		verificationError           string
	)
	if err := row.Scan(
		&id,
		&wsID,
		&createdByKeyID,
		&createdAtRaw,
		&updatedAtRaw,
		&outcome,
		&requestRaw,
		&selectedModelName,
		&instanceID,
		&instanceName,
		&failureReason,
		&autoVerificationRequestedAt,
		&verificationStatus,
		&verificationVerifiedAt,
		&verificationLatencyMS,
		&verificationModel,
		&verificationResponsePreview,
		&verificationError,
	); err != nil {
		return nil, err
	}

	return attemptFromRow(
		id,
		wsID,
		createdByKeyID,
		createdAtRaw,
		updatedAtRaw,
		outcome,
		requestRaw,
		selectedModelName,
		instanceID,
		instanceName,
		failureReason,
		autoVerificationRequestedAt,
		verificationStatus,
		verificationVerifiedAt,
		verificationLatencyMS,
		verificationModel,
		verificationResponsePreview,
		verificationError,
	)
}

func (s *Store) ListAttempts(workspaceID string, limit int) ([]*AttemptRecord, error) {
	rows, err := s.db.Query(
		`SELECT
			id,
			workspace_id,
			created_by_key_id,
			created_at,
			updated_at,
			outcome,
			request_json,
			selected_model_name,
			instance_id,
			instance_name,
			failure_reason,
			auto_verification_requested_at,
			verification_status,
			verification_verified_at,
			verification_latency_ms,
			verification_model,
			verification_response_preview,
			verification_error
		FROM deployment_attempts
		WHERE workspace_id = ?
		ORDER BY updated_at DESC
		LIMIT ?`,
		normalizeWorkspaceID(workspaceID),
		normalizeLimit(limit),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*AttemptRecord, 0)
	for rows.Next() {
		var (
			id                          string
			wsID                        string
			createdByKeyID              string
			createdAtRaw                string
			updatedAtRaw                string
			outcome                     string
			requestRaw                  string
			selectedModelName           string
			instanceID                  string
			instanceName                string
			failureReason               string
			autoVerificationRequestedAt string
			verificationStatus          string
			verificationVerifiedAt      string
			verificationLatencyMS       int64
			verificationModel           string
			verificationResponsePreview string
			verificationError           string
		)
		if err := rows.Scan(
			&id,
			&wsID,
			&createdByKeyID,
			&createdAtRaw,
			&updatedAtRaw,
			&outcome,
			&requestRaw,
			&selectedModelName,
			&instanceID,
			&instanceName,
			&failureReason,
			&autoVerificationRequestedAt,
			&verificationStatus,
			&verificationVerifiedAt,
			&verificationLatencyMS,
			&verificationModel,
			&verificationResponsePreview,
			&verificationError,
		); err != nil {
			return nil, err
		}

		attempt, err := attemptFromRow(
			id,
			wsID,
			createdByKeyID,
			createdAtRaw,
			updatedAtRaw,
			outcome,
			requestRaw,
			selectedModelName,
			instanceID,
			instanceName,
			failureReason,
			autoVerificationRequestedAt,
			verificationStatus,
			verificationVerifiedAt,
			verificationLatencyMS,
			verificationModel,
			verificationResponsePreview,
			verificationError,
		)
		if err != nil {
			return nil, err
		}
		out = append(out, attempt)
	}
	return out, rows.Err()
}

func (s *Store) RecordProvisionedAttempt(
	workspaceID string,
	createdByKeyID string,
	req providers.ProvisionRequest,
	selectedModelName string,
	instance *providers.Instance,
) (*AttemptRecord, error) {
	if instance == nil {
		return nil, errors.New("instance is required")
	}
	body, err := marshalRequest(req)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	id := uuid.NewString()
	workspaceID = normalizeWorkspaceID(workspaceID)

	_, err = s.db.Exec(
		`INSERT INTO deployment_attempts (
			id,
			workspace_id,
			created_by_key_id,
			created_at,
			updated_at,
			outcome,
			request_json,
			selected_model_name,
			instance_id,
			instance_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id,
		workspaceID,
		strings.TrimSpace(createdByKeyID),
		now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
		"provisioned",
		body,
		strings.TrimSpace(selectedModelName),
		instance.ID,
		instance.Name,
	)
	if err != nil {
		return nil, err
	}
	return s.getAttempt(workspaceID, id)
}

func (s *Store) RecordFailedAttempt(
	workspaceID string,
	createdByKeyID string,
	req providers.ProvisionRequest,
	selectedModelName string,
	failureReason string,
) (*AttemptRecord, error) {
	body, err := marshalRequest(req)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	id := uuid.NewString()
	workspaceID = normalizeWorkspaceID(workspaceID)

	_, err = s.db.Exec(
		`INSERT INTO deployment_attempts (
			id,
			workspace_id,
			created_by_key_id,
			created_at,
			updated_at,
			outcome,
			request_json,
			selected_model_name,
			failure_reason
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id,
		workspaceID,
		strings.TrimSpace(createdByKeyID),
		now.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
		"request_failed",
		body,
		strings.TrimSpace(selectedModelName),
		strings.TrimSpace(failureReason),
	)
	if err != nil {
		return nil, err
	}
	return s.getAttempt(workspaceID, id)
}

func (s *Store) UpdateVerification(workspaceID, attemptID string, verification InferenceVerification) (*AttemptRecord, error) {
	if strings.TrimSpace(verification.Status) == "" {
		return nil, errors.New("verification status is required")
	}
	if verification.VerifiedAt.IsZero() {
		verification.VerifiedAt = time.Now().UTC()
	}

	latency := int64(0)
	if verification.LatencyMS != nil {
		latency = *verification.LatencyMS
	}

	result, err := s.db.Exec(
		`UPDATE deployment_attempts
		SET
			updated_at = ?,
			verification_status = ?,
			verification_verified_at = ?,
			verification_latency_ms = ?,
			verification_model = ?,
			verification_response_preview = ?,
			verification_error = ?
		WHERE workspace_id = ? AND id = ?`,
		verification.VerifiedAt.UTC().Format(time.RFC3339Nano),
		strings.TrimSpace(verification.Status),
		verification.VerifiedAt.UTC().Format(time.RFC3339Nano),
		latency,
		strings.TrimSpace(verification.Model),
		strings.TrimSpace(verification.ResponsePreview),
		strings.TrimSpace(verification.Error),
		normalizeWorkspaceID(workspaceID),
		strings.TrimSpace(attemptID),
	)
	if err != nil {
		return nil, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rows == 0 {
		return nil, sql.ErrNoRows
	}
	return s.getAttempt(workspaceID, attemptID)
}

func (s *Store) MarkAutoVerificationRequested(workspaceID, attemptID string, requestedAt time.Time) (*AttemptRecord, error) {
	if requestedAt.IsZero() {
		requestedAt = time.Now().UTC()
	}
	result, err := s.db.Exec(
		`UPDATE deployment_attempts
		SET auto_verification_requested_at = CASE
				WHEN auto_verification_requested_at = '' THEN ?
				ELSE auto_verification_requested_at
			END
		WHERE workspace_id = ? AND id = ?`,
		requestedAt.UTC().Format(time.RFC3339Nano),
		normalizeWorkspaceID(workspaceID),
		strings.TrimSpace(attemptID),
	)
	if err != nil {
		return nil, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rows == 0 {
		return nil, sql.ErrNoRows
	}
	return s.getAttempt(workspaceID, attemptID)
}
