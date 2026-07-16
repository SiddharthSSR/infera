package agents

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/infera/infera/go/internal/auth"
	"github.com/infera/infera/go/internal/migrate"
	_ "github.com/mattn/go-sqlite3"
)

var agentMigrations = []migrate.Migration{
	{
		Version:     1,
		Description: "create agent runs and steps tables",
		SQL: `
		CREATE TABLE IF NOT EXISTS agent_runs (
			id                TEXT PRIMARY KEY,
			workspace_id      TEXT NOT NULL,
			created_by_key_id TEXT NOT NULL DEFAULT '',
			agent_id          TEXT NOT NULL,
			model             TEXT NOT NULL,
			input_text        TEXT NOT NULL,
			status            TEXT NOT NULL,
			max_steps         INTEGER NOT NULL,
			current_step      INTEGER NOT NULL DEFAULT 0,
			final_output      TEXT NOT NULL DEFAULT '',
			failure_reason    TEXT NOT NULL DEFAULT '',
			created_at        TEXT NOT NULL,
			updated_at        TEXT NOT NULL,
			started_at        TEXT NOT NULL DEFAULT '',
			finished_at       TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS idx_agent_runs_workspace_updated
			ON agent_runs(workspace_id, updated_at DESC);
		CREATE INDEX IF NOT EXISTS idx_agent_runs_workspace_status
			ON agent_runs(workspace_id, status);
		CREATE TABLE IF NOT EXISTS agent_run_steps (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id        TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
			workspace_id  TEXT NOT NULL,
			step_index    INTEGER NOT NULL,
			step_type     TEXT NOT NULL,
			tool_name     TEXT NOT NULL DEFAULT '',
			payload_json  TEXT NOT NULL,
			created_at    TEXT NOT NULL,
			UNIQUE(run_id, step_index)
		);
		CREATE INDEX IF NOT EXISTS idx_agent_run_steps_run
			ON agent_run_steps(run_id, step_index ASC);`,
	},
	{
		Version:     2,
		Description: "add run metadata and attachments",
		SQL: `
		ALTER TABLE agent_runs ADD COLUMN run_mode TEXT NOT NULL DEFAULT 'operations';
		ALTER TABLE agent_runs ADD COLUMN analysis_depth TEXT NOT NULL DEFAULT 'standard';
		CREATE TABLE IF NOT EXISTS agent_attachments (
			id                TEXT PRIMARY KEY,
			workspace_id      TEXT NOT NULL,
			created_by_key_id TEXT NOT NULL DEFAULT '',
			run_id            TEXT NOT NULL DEFAULT '',
			file_name         TEXT NOT NULL,
			mime_type         TEXT NOT NULL,
			size_bytes        INTEGER NOT NULL,
			width             INTEGER NOT NULL DEFAULT 0,
			height            INTEGER NOT NULL DEFAULT 0,
			sha256            TEXT NOT NULL,
			storage_path      TEXT NOT NULL,
			created_at        TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_agent_attachments_workspace_created
			ON agent_attachments(workspace_id, created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_agent_attachments_run
			ON agent_attachments(run_id, created_at ASC);`,
	},
	{
		Version:     3,
		Description: "create agent custom definitions table",
		SQL: `
		CREATE TABLE IF NOT EXISTS agent_custom_definitions (
			id              TEXT PRIMARY KEY,
			workspace_id    TEXT NOT NULL,
			name            TEXT NOT NULL,
			description     TEXT NOT NULL DEFAULT '',
			system_prompt   TEXT NOT NULL,
			tools           TEXT NOT NULL DEFAULT '[]',
			max_steps       INTEGER NOT NULL DEFAULT 8,
			timeout_seconds INTEGER NOT NULL DEFAULT 45,
			model           TEXT NOT NULL DEFAULT '',
			created_at      TEXT NOT NULL,
			updated_at      TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_agent_custom_defs_workspace
			ON agent_custom_definitions(workspace_id);`,
	},
	{
		Version:     4,
		Description: "create agent webhook configs table",
		SQL: `
		CREATE TABLE IF NOT EXISTS agent_webhook_configs (
			id           TEXT PRIMARY KEY,
			workspace_id TEXT NOT NULL,
			url          TEXT NOT NULL,
			secret       TEXT NOT NULL DEFAULT '',
			events       TEXT NOT NULL DEFAULT '["agent.run.completed"]',
			active       INTEGER NOT NULL DEFAULT 1,
			created_at   TEXT NOT NULL,
			updated_at   TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_agent_webhook_configs_workspace ON agent_webhook_configs(workspace_id);`,
	},
}

type Store struct {
	db             *sql.DB
	attachmentRoot string
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
	if err := migrate.Run(db, agentMigrations); err != nil {
		_ = db.Close()
		return nil, err
	}

	attachmentRoot := filepath.Join(filepath.Dir(dbPath), "agent_attachments")
	if err := os.MkdirAll(attachmentRoot, 0o755); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Store{
		db:             db,
		attachmentRoot: attachmentRoot,
	}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) AttachmentRoot() string {
	return s.attachmentRoot
}

func normalizeWorkspaceID(workspaceID string) string {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return auth.DefaultWorkspaceID
	}
	return workspaceID
}

func normalizeListLimit(limit int) int {
	if limit <= 0 {
		return 25
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func normalizeRunMode(mode RunMode) RunMode {
	switch RunMode(strings.TrimSpace(string(mode))) {
	case RunModeResearch:
		return RunModeResearch
	case RunModeMultimodal:
		return RunModeMultimodal
	default:
		return RunModeOperations
	}
}

func normalizeAnalysisDepth(depth AnalysisDepth) AnalysisDepth {
	switch AnalysisDepth(strings.TrimSpace(string(depth))) {
	case AnalysisDepthDeep:
		return AnalysisDepthDeep
	default:
		return AnalysisDepthStandard
	}
}

func parseOptionalTimestamp(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	ts, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil, err
	}
	return &ts, nil
}

func parseRequiredTimestamp(raw string) (time.Time, error) {
	ts, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, err
	}
	return ts, nil
}

func runFromRow(
	id string,
	workspaceID string,
	createdByKeyID string,
	agentID string,
	mode string,
	analysisDepth string,
	model string,
	input string,
	status string,
	maxSteps int,
	currentStep int,
	finalOutput string,
	failureReason string,
	createdAtRaw string,
	updatedAtRaw string,
	startedAtRaw string,
	finishedAtRaw string,
) (*Run, error) {
	createdAt, err := parseRequiredTimestamp(createdAtRaw)
	if err != nil {
		return nil, err
	}
	updatedAt, err := parseRequiredTimestamp(updatedAtRaw)
	if err != nil {
		return nil, err
	}
	startedAt, err := parseOptionalTimestamp(startedAtRaw)
	if err != nil {
		return nil, err
	}
	finishedAt, err := parseOptionalTimestamp(finishedAtRaw)
	if err != nil {
		return nil, err
	}

	return &Run{
		ID:             id,
		WorkspaceID:    workspaceID,
		CreatedByKeyID: createdByKeyID,
		AgentID:        agentID,
		Mode:           normalizeRunMode(RunMode(mode)),
		AnalysisDepth:  normalizeAnalysisDepth(AnalysisDepth(analysisDepth)),
		Model:          model,
		Input:          input,
		Status:         Status(status),
		MaxSteps:       maxSteps,
		CurrentStep:    currentStep,
		FinalOutput:    finalOutput,
		FailureReason:  failureReason,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
		StartedAt:      startedAt,
		FinishedAt:     finishedAt,
	}, nil
}

func attachmentFromRow(
	id string,
	workspaceID string,
	createdByKeyID string,
	runID string,
	fileName string,
	mimeType string,
	sizeBytes int64,
	width int,
	height int,
	sha256 string,
	storagePath string,
	createdAtRaw string,
) (*Attachment, error) {
	createdAt, err := parseRequiredTimestamp(createdAtRaw)
	if err != nil {
		return nil, err
	}

	return &Attachment{
		ID:             id,
		WorkspaceID:    workspaceID,
		CreatedByKeyID: createdByKeyID,
		RunID:          runID,
		FileName:       fileName,
		MIMEType:       mimeType,
		SizeBytes:      sizeBytes,
		Width:          width,
		Height:         height,
		SHA256:         sha256,
		CreatedAt:      createdAt,
		StoragePath:    storagePath,
	}, nil
}

func (s *Store) CreateRun(
	workspaceID,
	createdByKeyID,
	agentID string,
	mode RunMode,
	analysisDepth AnalysisDepth,
	model,
	input string,
	maxSteps int,
	now time.Time,
) (*Run, error) {
	workspaceID = normalizeWorkspaceID(workspaceID)
	createdByKeyID = strings.TrimSpace(createdByKeyID)
	agentID = strings.TrimSpace(agentID)
	model = strings.TrimSpace(model)
	input = strings.TrimSpace(input)
	mode = normalizeRunMode(mode)
	analysisDepth = normalizeAnalysisDepth(analysisDepth)
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required")
	}
	if model == "" {
		return nil, fmt.Errorf("model is required")
	}
	if input == "" {
		return nil, fmt.Errorf("input is required")
	}
	if maxSteps <= 0 {
		return nil, fmt.Errorf("max_steps must be positive")
	}

	run := &Run{
		ID:             uuid.New().String(),
		WorkspaceID:    workspaceID,
		CreatedByKeyID: createdByKeyID,
		AgentID:        agentID,
		Mode:           mode,
		AnalysisDepth:  analysisDepth,
		Model:          model,
		Input:          input,
		Status:         StatusQueued,
		MaxSteps:       maxSteps,
		CurrentStep:    0,
		CreatedAt:      now.UTC(),
		UpdatedAt:      now.UTC(),
	}

	_, err := s.db.Exec(
		`INSERT INTO agent_runs (
			id, workspace_id, created_by_key_id, agent_id, run_mode, analysis_depth, model, input_text, status, max_steps, current_step, final_output, failure_reason, created_at, updated_at, started_at, finished_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', '')`,
		run.ID,
		run.WorkspaceID,
		run.CreatedByKeyID,
		run.AgentID,
		run.Mode,
		run.AnalysisDepth,
		run.Model,
		run.Input,
		run.Status,
		run.MaxSteps,
		run.CurrentStep,
		run.FinalOutput,
		run.FailureReason,
		run.CreatedAt.Format(time.RFC3339Nano),
		run.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, err
	}
	return run, nil
}

func (s *Store) ListRuns(workspaceID string, limit int) ([]*Run, error) {
	rows, err := s.db.Query(
		`SELECT id, workspace_id, created_by_key_id, agent_id, run_mode, analysis_depth, model, input_text, status, max_steps, current_step, final_output, failure_reason, created_at, updated_at, started_at, finished_at
		FROM agent_runs
		WHERE workspace_id = ?
		ORDER BY updated_at DESC
		LIMIT ?`,
		normalizeWorkspaceID(workspaceID),
		normalizeListLimit(limit),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := make([]*Run, 0)
	for rows.Next() {
		var (
			id             string
			wsID           string
			createdByKeyID string
			agentID        string
			mode           string
			analysisDepth  string
			model          string
			input          string
			status         string
			maxSteps       int
			currentStep    int
			finalOutput    string
			failureReason  string
			createdAtRaw   string
			updatedAtRaw   string
			startedAtRaw   string
			finishedAtRaw  string
		)
		if err := rows.Scan(
			&id,
			&wsID,
			&createdByKeyID,
			&agentID,
			&mode,
			&analysisDepth,
			&model,
			&input,
			&status,
			&maxSteps,
			&currentStep,
			&finalOutput,
			&failureReason,
			&createdAtRaw,
			&updatedAtRaw,
			&startedAtRaw,
			&finishedAtRaw,
		); err != nil {
			return nil, err
		}
		run, err := runFromRow(id, wsID, createdByKeyID, agentID, mode, analysisDepth, model, input, status, maxSteps, currentStep, finalOutput, failureReason, createdAtRaw, updatedAtRaw, startedAtRaw, finishedAtRaw)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (s *Store) GetRun(workspaceID, runID string) (*Run, error) {
	row := s.db.QueryRow(
		`SELECT id, workspace_id, created_by_key_id, agent_id, run_mode, analysis_depth, model, input_text, status, max_steps, current_step, final_output, failure_reason, created_at, updated_at, started_at, finished_at
		FROM agent_runs
		WHERE workspace_id = ? AND id = ?`,
		normalizeWorkspaceID(workspaceID),
		strings.TrimSpace(runID),
	)

	var (
		id             string
		wsID           string
		createdByKeyID string
		agentID        string
		mode           string
		analysisDepth  string
		model          string
		input          string
		status         string
		maxSteps       int
		currentStep    int
		finalOutput    string
		failureReason  string
		createdAtRaw   string
		updatedAtRaw   string
		startedAtRaw   string
		finishedAtRaw  string
	)
	if err := row.Scan(
		&id,
		&wsID,
		&createdByKeyID,
		&agentID,
		&mode,
		&analysisDepth,
		&model,
		&input,
		&status,
		&maxSteps,
		&currentStep,
		&finalOutput,
		&failureReason,
		&createdAtRaw,
		&updatedAtRaw,
		&startedAtRaw,
		&finishedAtRaw,
	); err != nil {
		return nil, err
	}

	return runFromRow(id, wsID, createdByKeyID, agentID, mode, analysisDepth, model, input, status, maxSteps, currentStep, finalOutput, failureReason, createdAtRaw, updatedAtRaw, startedAtRaw, finishedAtRaw)
}

func (s *Store) ListRunSteps(workspaceID, runID string) ([]*RunStep, error) {
	rows, err := s.db.Query(
		`SELECT id, run_id, step_index, step_type, tool_name, payload_json, created_at
		FROM agent_run_steps
		WHERE workspace_id = ? AND run_id = ?
		ORDER BY step_index ASC`,
		normalizeWorkspaceID(workspaceID),
		strings.TrimSpace(runID),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	steps := make([]*RunStep, 0)
	for rows.Next() {
		var (
			id           int64
			stepRunID    string
			index        int
			stepType     string
			toolName     string
			payloadRaw   string
			createdAtRaw string
		)
		if err := rows.Scan(&id, &stepRunID, &index, &stepType, &toolName, &payloadRaw, &createdAtRaw); err != nil {
			return nil, err
		}
		createdAt, err := parseRequiredTimestamp(createdAtRaw)
		if err != nil {
			return nil, err
		}
		steps = append(steps, &RunStep{
			ID:        id,
			RunID:     stepRunID,
			Index:     index,
			Type:      StepType(stepType),
			ToolName:  toolName,
			Payload:   json.RawMessage(payloadRaw),
			CreatedAt: createdAt,
		})
	}
	return steps, rows.Err()
}

func (s *Store) ListStepsAfter(workspaceID, runID string, afterIndex int) ([]*RunStep, error) {
	rows, err := s.db.Query(
		`SELECT id, run_id, step_index, step_type, tool_name, payload_json, created_at
		FROM agent_run_steps
		WHERE workspace_id = ? AND run_id = ? AND step_index > ?
		ORDER BY step_index ASC`,
		normalizeWorkspaceID(workspaceID),
		strings.TrimSpace(runID),
		afterIndex,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	steps := make([]*RunStep, 0)
	for rows.Next() {
		var (
			id           int64
			stepRunID    string
			index        int
			stepType     string
			toolName     string
			payloadRaw   string
			createdAtRaw string
		)
		if err := rows.Scan(&id, &stepRunID, &index, &stepType, &toolName, &payloadRaw, &createdAtRaw); err != nil {
			return nil, err
		}
		createdAt, err := parseRequiredTimestamp(createdAtRaw)
		if err != nil {
			return nil, err
		}
		steps = append(steps, &RunStep{
			ID:        id,
			RunID:     stepRunID,
			Index:     index,
			Type:      StepType(stepType),
			ToolName:  toolName,
			Payload:   json.RawMessage(payloadRaw),
			CreatedAt: createdAt,
		})
	}
	return steps, rows.Err()
}

func (s *Store) GetRunDetail(workspaceID, runID string) (*RunDetail, error) {
	run, err := s.GetRun(workspaceID, runID)
	if err != nil {
		return nil, err
	}
	steps, err := s.ListRunSteps(workspaceID, runID)
	if err != nil {
		return nil, err
	}
	attachments, err := s.ListAttachmentsForRun(workspaceID, runID)
	if err != nil {
		return nil, err
	}
	return &RunDetail{Run: run, Steps: steps, Attachments: attachments}, nil
}

func (s *Store) MarkRunRunning(workspaceID, runID string, now time.Time) error {
	result, err := s.db.Exec(
		`UPDATE agent_runs
		SET status = ?, started_at = ?, updated_at = ?
		WHERE workspace_id = ? AND id = ? AND status = ?`,
		StatusRunning,
		now.UTC().Format(time.RFC3339Nano),
		now.UTC().Format(time.RFC3339Nano),
		normalizeWorkspaceID(workspaceID),
		strings.TrimSpace(runID),
		StatusQueued,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) AppendStep(workspaceID, runID string, stepType StepType, toolName string, payload any, now time.Time) (*RunStep, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	var currentStep int
	row := tx.QueryRow(
		`SELECT current_step FROM agent_runs WHERE workspace_id = ? AND id = ?`,
		normalizeWorkspaceID(workspaceID),
		strings.TrimSpace(runID),
	)
	if scanErr := row.Scan(&currentStep); scanErr != nil {
		err = scanErr
		return nil, err
	}

	nextStep := currentStep + 1
	result, execErr := tx.Exec(
		`INSERT INTO agent_run_steps (run_id, workspace_id, step_index, step_type, tool_name, payload_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		strings.TrimSpace(runID),
		normalizeWorkspaceID(workspaceID),
		nextStep,
		stepType,
		strings.TrimSpace(toolName),
		string(payloadBytes),
		now.UTC().Format(time.RFC3339Nano),
	)
	if execErr != nil {
		err = execErr
		return nil, err
	}

	if _, execErr = tx.Exec(
		`UPDATE agent_runs SET current_step = ?, updated_at = ? WHERE workspace_id = ? AND id = ?`,
		nextStep,
		now.UTC().Format(time.RFC3339Nano),
		normalizeWorkspaceID(workspaceID),
		strings.TrimSpace(runID),
	); execErr != nil {
		err = execErr
		return nil, err
	}

	stepID, execErr := result.LastInsertId()
	if execErr != nil {
		err = execErr
		return nil, err
	}

	if execErr = tx.Commit(); execErr != nil {
		err = execErr
		return nil, err
	}

	return &RunStep{
		ID:        stepID,
		RunID:     strings.TrimSpace(runID),
		Index:     nextStep,
		Type:      stepType,
		ToolName:  strings.TrimSpace(toolName),
		Payload:   payloadBytes,
		CreatedAt: now.UTC(),
	}, nil
}

func (s *Store) CompleteRun(workspaceID, runID string, status Status, finalOutput, failureReason string, now time.Time) error {
	result, err := s.db.Exec(
		`UPDATE agent_runs
		SET status = ?, final_output = ?, failure_reason = ?, updated_at = ?, finished_at = ?
		WHERE workspace_id = ? AND id = ? AND status IN (?, ?)`,
		status,
		strings.TrimSpace(finalOutput),
		strings.TrimSpace(failureReason),
		now.UTC().Format(time.RFC3339Nano),
		now.UTC().Format(time.RFC3339Nano),
		normalizeWorkspaceID(workspaceID),
		strings.TrimSpace(runID),
		StatusQueued,
		StatusRunning,
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) MarkCanceled(workspaceID, runID, reason string, now time.Time) error {
	return s.CompleteRun(workspaceID, runID, StatusCanceled, "", reason, now)
}

func (s *Store) MarkInterruptedRuns(now time.Time, reason string) (int64, error) {
	result, err := s.db.Exec(
		`UPDATE agent_runs
		SET status = ?, failure_reason = ?, updated_at = ?, finished_at = ?
		WHERE status IN (?, ?)`,
		StatusFailed,
		strings.TrimSpace(reason),
		now.UTC().Format(time.RFC3339Nano),
		now.UTC().Format(time.RFC3339Nano),
		StatusQueued,
		StatusRunning,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) CreateAttachment(
	workspaceID,
	createdByKeyID,
	fileName,
	mimeType string,
	sizeBytes int64,
	width,
	height int,
	sha256,
	storagePath string,
	now time.Time,
) (*Attachment, error) {
	workspaceID = normalizeWorkspaceID(workspaceID)
	createdByKeyID = strings.TrimSpace(createdByKeyID)
	fileName = strings.TrimSpace(fileName)
	mimeType = strings.TrimSpace(mimeType)
	sha256 = strings.TrimSpace(sha256)
	storagePath = strings.TrimSpace(storagePath)
	if fileName == "" {
		return nil, fmt.Errorf("file_name is required")
	}
	if mimeType == "" {
		return nil, fmt.Errorf("mime_type is required")
	}
	if sizeBytes <= 0 {
		return nil, fmt.Errorf("size_bytes must be positive")
	}
	if sha256 == "" {
		return nil, fmt.Errorf("sha256 is required")
	}
	if storagePath == "" {
		return nil, fmt.Errorf("storage_path is required")
	}

	attachment := &Attachment{
		ID:             uuid.New().String(),
		WorkspaceID:    workspaceID,
		CreatedByKeyID: createdByKeyID,
		FileName:       fileName,
		MIMEType:       mimeType,
		SizeBytes:      sizeBytes,
		Width:          width,
		Height:         height,
		SHA256:         sha256,
		CreatedAt:      now.UTC(),
		StoragePath:    storagePath,
	}

	_, err := s.db.Exec(
		`INSERT INTO agent_attachments (
			id, workspace_id, created_by_key_id, run_id, file_name, mime_type, size_bytes, width, height, sha256, storage_path, created_at
		) VALUES (?, ?, ?, '', ?, ?, ?, ?, ?, ?, ?, ?)`,
		attachment.ID,
		attachment.WorkspaceID,
		attachment.CreatedByKeyID,
		attachment.FileName,
		attachment.MIMEType,
		attachment.SizeBytes,
		attachment.Width,
		attachment.Height,
		attachment.SHA256,
		attachment.StoragePath,
		attachment.CreatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, err
	}
	return attachment, nil
}

func (s *Store) GetAttachment(workspaceID, attachmentID string) (*Attachment, error) {
	row := s.db.QueryRow(
		`SELECT id, workspace_id, created_by_key_id, run_id, file_name, mime_type, size_bytes, width, height, sha256, storage_path, created_at
		FROM agent_attachments
		WHERE workspace_id = ? AND id = ?`,
		normalizeWorkspaceID(workspaceID),
		strings.TrimSpace(attachmentID),
	)

	var (
		id             string
		wsID           string
		createdByKeyID string
		runID          string
		fileName       string
		mimeType       string
		sizeBytes      int64
		width          int
		height         int
		sha256         string
		storagePath    string
		createdAtRaw   string
	)
	if err := row.Scan(
		&id,
		&wsID,
		&createdByKeyID,
		&runID,
		&fileName,
		&mimeType,
		&sizeBytes,
		&width,
		&height,
		&sha256,
		&storagePath,
		&createdAtRaw,
	); err != nil {
		return nil, err
	}

	return attachmentFromRow(id, wsID, createdByKeyID, runID, fileName, mimeType, sizeBytes, width, height, sha256, storagePath, createdAtRaw)
}

func (s *Store) ListAttachmentsForRun(workspaceID, runID string) ([]*Attachment, error) {
	rows, err := s.db.Query(
		`SELECT id, workspace_id, created_by_key_id, run_id, file_name, mime_type, size_bytes, width, height, sha256, storage_path, created_at
		FROM agent_attachments
		WHERE workspace_id = ? AND run_id = ?
		ORDER BY created_at ASC`,
		normalizeWorkspaceID(workspaceID),
		strings.TrimSpace(runID),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	attachments := make([]*Attachment, 0)
	for rows.Next() {
		var (
			id             string
			wsID           string
			createdByKeyID string
			currentRunID   string
			fileName       string
			mimeType       string
			sizeBytes      int64
			width          int
			height         int
			sha256         string
			storagePath    string
			createdAtRaw   string
		)
		if err := rows.Scan(
			&id,
			&wsID,
			&createdByKeyID,
			&currentRunID,
			&fileName,
			&mimeType,
			&sizeBytes,
			&width,
			&height,
			&sha256,
			&storagePath,
			&createdAtRaw,
		); err != nil {
			return nil, err
		}
		attachment, err := attachmentFromRow(id, wsID, createdByKeyID, currentRunID, fileName, mimeType, sizeBytes, width, height, sha256, storagePath, createdAtRaw)
		if err != nil {
			return nil, err
		}
		attachments = append(attachments, attachment)
	}
	return attachments, rows.Err()
}

func (s *Store) ListAttachmentsByID(workspaceID string, attachmentIDs []string) ([]*Attachment, error) {
	if len(attachmentIDs) == 0 {
		return nil, nil
	}
	attachments := make([]*Attachment, 0, len(attachmentIDs))
	for _, attachmentID := range attachmentIDs {
		attachment, err := s.GetAttachment(workspaceID, attachmentID)
		if err != nil {
			return nil, err
		}
		attachments = append(attachments, attachment)
	}
	return attachments, nil
}

// webhookFromRow converts raw DB columns into a WebhookConfig, parsing
// timestamps and the JSON-encoded events list.
func webhookFromRow(
	id string,
	workspaceID string,
	url string,
	secret string,
	eventsJSON string,
	active int,
	createdAtRaw string,
	updatedAtRaw string,
) (*WebhookConfig, error) {
	createdAt, err := parseRequiredTimestamp(createdAtRaw)
	if err != nil {
		return nil, err
	}
	updatedAt, err := parseRequiredTimestamp(updatedAtRaw)
	if err != nil {
		return nil, err
	}
	var events []string
	if err := json.Unmarshal([]byte(eventsJSON), &events); err != nil {
		events = []string{webhookEventRunComplete}
	}
	return &WebhookConfig{
		ID:          id,
		WorkspaceID: workspaceID,
		URL:         url,
		Secret:      secret,
		Events:      events,
		Active:      active != 0,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}, nil
}

func (s *Store) CreateWebhookConfig(workspaceID, url, secret string, events []string) (*WebhookConfig, error) {
	workspaceID = normalizeWorkspaceID(workspaceID)
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, fmt.Errorf("url is required")
	}
	if len(events) == 0 {
		events = []string{webhookEventRunComplete}
	}
	eventsJSON, err := json.Marshal(events)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	wh := &WebhookConfig{
		ID:          uuid.New().String(),
		WorkspaceID: workspaceID,
		URL:         url,
		Secret:      secret,
		Events:      events,
		Active:      true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err = s.db.Exec(
		`INSERT INTO agent_webhook_configs (id, workspace_id, url, secret, events, active, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 1, ?, ?)`,
		wh.ID,
		wh.WorkspaceID,
		wh.URL,
		wh.Secret,
		string(eventsJSON),
		wh.CreatedAt.Format(time.RFC3339Nano),
		wh.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, err
	}
	return wh, nil
}

func (s *Store) ListWebhookConfigs(workspaceID string) ([]*WebhookConfig, error) {
	rows, err := s.db.Query(
		`SELECT id, workspace_id, url, secret, events, active, created_at, updated_at
		FROM agent_webhook_configs
		WHERE workspace_id = ?
		ORDER BY created_at ASC`,
		normalizeWorkspaceID(workspaceID),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	webhooks := make([]*WebhookConfig, 0)
	for rows.Next() {
		var (
			id           string
			wsID         string
			url          string
			secret       string
			eventsJSON   string
			active       int
			createdAtRaw string
			updatedAtRaw string
		)
		if err := rows.Scan(&id, &wsID, &url, &secret, &eventsJSON, &active, &createdAtRaw, &updatedAtRaw); err != nil {
			return nil, err
		}
		wh, err := webhookFromRow(id, wsID, url, secret, eventsJSON, active, createdAtRaw, updatedAtRaw)
		if err != nil {
			return nil, err
		}
		webhooks = append(webhooks, wh)
	}
	return webhooks, rows.Err()
}

func (s *Store) DeleteWebhookConfig(workspaceID, webhookID string) error {
	result, err := s.db.Exec(
		`DELETE FROM agent_webhook_configs WHERE workspace_id = ? AND id = ?`,
		normalizeWorkspaceID(workspaceID),
		strings.TrimSpace(webhookID),
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetActiveWebhooksForEvent returns all active webhook configs for the given
// workspace that subscribe to the provided event string (e.g. "succeeded").
func (s *Store) GetActiveWebhooksForEvent(workspaceID string, event string) ([]*WebhookConfig, error) {
	// SQLite's json_each lets us check membership in the stored JSON array
	// without pulling every row into Go.
	rows, err := s.db.Query(
		`SELECT w.id, w.workspace_id, w.url, w.secret, w.events, w.active, w.created_at, w.updated_at
		FROM agent_webhook_configs w
		WHERE w.workspace_id = ?
		  AND w.active = 1
		  AND EXISTS (
		        SELECT 1
		        FROM json_each(w.events)
		        WHERE json_each.value = ?
		  )`,
		normalizeWorkspaceID(workspaceID),
		strings.TrimSpace(event),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	webhooks := make([]*WebhookConfig, 0)
	for rows.Next() {
		var (
			id           string
			wsID         string
			url          string
			secret       string
			eventsJSON   string
			active       int
			createdAtRaw string
			updatedAtRaw string
		)
		if err := rows.Scan(&id, &wsID, &url, &secret, &eventsJSON, &active, &createdAtRaw, &updatedAtRaw); err != nil {
			return nil, err
		}
		wh, err := webhookFromRow(id, wsID, url, secret, eventsJSON, active, createdAtRaw, updatedAtRaw)
		if err != nil {
			return nil, err
		}
		webhooks = append(webhooks, wh)
	}
	return webhooks, rows.Err()
}

func (s *Store) AttachAttachmentsToRun(workspaceID, runID string, attachmentIDs []string) error {
	if len(attachmentIDs) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	normalizedWorkspaceID := normalizeWorkspaceID(workspaceID)
	normalizedRunID := strings.TrimSpace(runID)
	for _, attachmentID := range attachmentIDs {
		result, execErr := tx.Exec(
			`UPDATE agent_attachments
			SET run_id = ?
			WHERE workspace_id = ? AND id = ? AND (run_id = '' OR run_id = ?)`,
			normalizedRunID,
			normalizedWorkspaceID,
			strings.TrimSpace(attachmentID),
			normalizedRunID,
		)
		if execErr != nil {
			err = execErr
			return err
		}
		affected, execErr := result.RowsAffected()
		if execErr != nil {
			err = execErr
			return err
		}
		if affected == 0 {
			err = fmt.Errorf("attachment %q is unavailable for this run", attachmentID)
			return err
		}
	}

	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

func customDefinitionFromRow(
	id string,
	workspaceID string,
	name string,
	description string,
	systemPrompt string,
	toolsJSON string,
	maxSteps int,
	timeoutSeconds int,
	model string,
	createdAtRaw string,
	updatedAtRaw string,
) (*CustomDefinition, error) {
	createdAt, err := parseRequiredTimestamp(createdAtRaw)
	if err != nil {
		return nil, err
	}
	updatedAt, err := parseRequiredTimestamp(updatedAtRaw)
	if err != nil {
		return nil, err
	}
	var tools []string
	if err := json.Unmarshal([]byte(toolsJSON), &tools); err != nil {
		tools = []string{}
	}
	return &CustomDefinition{
		ID:             id,
		WorkspaceID:    workspaceID,
		Name:           name,
		Description:    description,
		SystemPrompt:   systemPrompt,
		Tools:          tools,
		MaxSteps:       maxSteps,
		TimeoutSeconds: timeoutSeconds,
		Model:          model,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}, nil
}

func (s *Store) CreateCustomDefinition(
	workspaceID,
	name,
	description,
	systemPrompt string,
	tools []string,
	maxSteps,
	timeoutSeconds int,
	model string,
	now time.Time,
) (*CustomDefinition, error) {
	workspaceID = normalizeWorkspaceID(workspaceID)
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	systemPrompt = strings.TrimSpace(systemPrompt)
	model = strings.TrimSpace(model)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if systemPrompt == "" {
		return nil, fmt.Errorf("system_prompt is required")
	}
	if maxSteps <= 0 {
		maxSteps = 8
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = 45
	}
	if tools == nil {
		tools = []string{}
	}
	toolsJSON, err := json.Marshal(tools)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize tools: %w", err)
	}

	def := &CustomDefinition{
		ID:             uuid.New().String(),
		WorkspaceID:    workspaceID,
		Name:           name,
		Description:    description,
		SystemPrompt:   systemPrompt,
		Tools:          tools,
		MaxSteps:       maxSteps,
		TimeoutSeconds: timeoutSeconds,
		Model:          model,
		CreatedAt:      now.UTC(),
		UpdatedAt:      now.UTC(),
	}

	_, err = s.db.Exec(
		`INSERT INTO agent_custom_definitions (
			id, workspace_id, name, description, system_prompt, tools, max_steps, timeout_seconds, model, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		def.ID,
		def.WorkspaceID,
		def.Name,
		def.Description,
		def.SystemPrompt,
		string(toolsJSON),
		def.MaxSteps,
		def.TimeoutSeconds,
		def.Model,
		def.CreatedAt.Format(time.RFC3339Nano),
		def.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, err
	}
	return def, nil
}

func (s *Store) ListCustomDefinitions(workspaceID string) ([]*CustomDefinition, error) {
	rows, err := s.db.Query(
		`SELECT id, workspace_id, name, description, system_prompt, tools, max_steps, timeout_seconds, model, created_at, updated_at
		FROM agent_custom_definitions
		WHERE workspace_id = ?
		ORDER BY created_at ASC`,
		normalizeWorkspaceID(workspaceID),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	defs := make([]*CustomDefinition, 0)
	for rows.Next() {
		var (
			id             string
			wsID           string
			name           string
			description    string
			systemPrompt   string
			toolsJSON      string
			maxSteps       int
			timeoutSeconds int
			model          string
			createdAtRaw   string
			updatedAtRaw   string
		)
		if err := rows.Scan(
			&id, &wsID, &name, &description, &systemPrompt,
			&toolsJSON, &maxSteps, &timeoutSeconds, &model,
			&createdAtRaw, &updatedAtRaw,
		); err != nil {
			return nil, err
		}
		def, err := customDefinitionFromRow(id, wsID, name, description, systemPrompt, toolsJSON, maxSteps, timeoutSeconds, model, createdAtRaw, updatedAtRaw)
		if err != nil {
			return nil, err
		}
		defs = append(defs, def)
	}
	return defs, rows.Err()
}

func (s *Store) GetCustomDefinition(workspaceID, defID string) (*CustomDefinition, error) {
	row := s.db.QueryRow(
		`SELECT id, workspace_id, name, description, system_prompt, tools, max_steps, timeout_seconds, model, created_at, updated_at
		FROM agent_custom_definitions
		WHERE workspace_id = ? AND id = ?`,
		normalizeWorkspaceID(workspaceID),
		strings.TrimSpace(defID),
	)
	var (
		id             string
		wsID           string
		name           string
		description    string
		systemPrompt   string
		toolsJSON      string
		maxSteps       int
		timeoutSeconds int
		model          string
		createdAtRaw   string
		updatedAtRaw   string
	)
	if err := row.Scan(
		&id, &wsID, &name, &description, &systemPrompt,
		&toolsJSON, &maxSteps, &timeoutSeconds, &model,
		&createdAtRaw, &updatedAtRaw,
	); err != nil {
		return nil, err
	}
	return customDefinitionFromRow(id, wsID, name, description, systemPrompt, toolsJSON, maxSteps, timeoutSeconds, model, createdAtRaw, updatedAtRaw)
}

func (s *Store) DeleteCustomDefinition(workspaceID, defID string) error {
	result, err := s.db.Exec(
		`DELETE FROM agent_custom_definitions WHERE workspace_id = ? AND id = ?`,
		normalizeWorkspaceID(workspaceID),
		strings.TrimSpace(defID),
	)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}
