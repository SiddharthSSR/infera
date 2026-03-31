package agents

import (
	"context"
	"encoding/json"
	"time"

	"github.com/infera/infera/go/internal/auth"
	"github.com/infera/infera/go/pkg/types"
)

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
)

type StepType string

const (
	StepTypeToolCall   StepType = "tool_call"
	StepTypeToolResult StepType = "tool_result"
	StepTypeFinal      StepType = "final"
	StepTypeError      StepType = "error"
)

type Run struct {
	ID             string     `json:"id"`
	WorkspaceID    string     `json:"workspace_id"`
	CreatedByKeyID string     `json:"created_by_key_id,omitempty"`
	AgentID        string     `json:"agent_id"`
	Model          string     `json:"model"`
	Input          string     `json:"input"`
	Status         Status     `json:"status"`
	MaxSteps       int        `json:"max_steps"`
	CurrentStep    int        `json:"current_step"`
	FinalOutput    string     `json:"final_output,omitempty"`
	FailureReason  string     `json:"failure_reason,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}

type RunStep struct {
	ID        int64           `json:"id"`
	RunID     string          `json:"run_id"`
	Index     int             `json:"index"`
	Type      StepType        `json:"type"`
	ToolName  string          `json:"tool_name,omitempty"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

type RunDetail struct {
	Run   *Run       `json:"run"`
	Steps []*RunStep `json:"steps"`
}

type Definition struct {
	ID                string
	Name              string
	Description       string
	DefaultMaxSteps   int
	Timeout           time.Duration
	ModelParameters   types.InferenceParameters
	Tools             []string
	BuildSystemPrompt func([]ToolDescriptor) string
}

type ToolDefinition struct {
	Name        string
	Description string
	Permission  string
	Handler     ToolHandler
}

type ToolDescriptor struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type AgentDescriptor struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Description     string           `json:"description"`
	DefaultMaxSteps int              `json:"default_max_steps"`
	Tools           []ToolDescriptor `json:"tools"`
}

type ToolCallContext struct {
	Run   *Run
	Actor *auth.KeyRecord
}

type ToolHandler func(ctx context.Context, call ToolCallContext, arguments json.RawMessage) (any, error)

type ToolCallEnvelope struct {
	Type      string          `json:"type"`
	ToolName  string          `json:"tool_name,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Message   string          `json:"message,omitempty"`
}

type CreateRunRequest struct {
	AgentID  string
	Model    string
	Input    string
	MaxSteps int
}

type ModelRunRequest struct {
	Actor      *auth.KeyRecord
	Session    *auth.SessionRecord
	Run        *Run
	Messages   []types.Message
	Parameters types.InferenceParameters
}

type ModelRunner interface {
	Run(ctx context.Context, req ModelRunRequest) (*types.InferenceResponse, error)
}
