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

type RunMode string

const (
	RunModeOperations RunMode = "operations"
	RunModeResearch   RunMode = "research"
	RunModeMultimodal RunMode = "multimodal"
)

type AnalysisDepth string

const (
	AnalysisDepthStandard AnalysisDepth = "standard"
	AnalysisDepthDeep     AnalysisDepth = "deep"
)

type Run struct {
	ID             string        `json:"id"`
	WorkspaceID    string        `json:"workspace_id"`
	CreatedByKeyID string        `json:"created_by_key_id,omitempty"`
	AgentID        string        `json:"agent_id"`
	Mode           RunMode       `json:"mode"`
	AnalysisDepth  AnalysisDepth `json:"analysis_depth"`
	Model          string        `json:"model"`
	Input          string        `json:"input"`
	Status         Status        `json:"status"`
	MaxSteps       int           `json:"max_steps"`
	CurrentStep    int           `json:"current_step"`
	FinalOutput    string        `json:"final_output,omitempty"`
	FailureReason  string        `json:"failure_reason,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
	StartedAt      *time.Time    `json:"started_at,omitempty"`
	FinishedAt     *time.Time    `json:"finished_at,omitempty"`
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
	Run         *Run             `json:"run"`
	Steps       []*RunStep       `json:"steps"`
	Attachments []*Attachment    `json:"attachments,omitempty"`
	Sources     []ResearchSource `json:"sources,omitempty"`
}

type Attachment struct {
	ID             string    `json:"id"`
	WorkspaceID    string    `json:"workspace_id"`
	CreatedByKeyID string    `json:"created_by_key_id,omitempty"`
	RunID          string    `json:"run_id,omitempty"`
	FileName       string    `json:"file_name"`
	MIMEType       string    `json:"mime_type"`
	SizeBytes      int64     `json:"size_bytes"`
	Width          int       `json:"width,omitempty"`
	Height         int       `json:"height,omitempty"`
	SHA256         string    `json:"sha256"`
	CreatedAt      time.Time `json:"created_at"`
	StoragePath    string    `json:"-"`
}

type AttachmentDescriptor struct {
	ID        string `json:"id"`
	FileName  string `json:"file_name"`
	MIMEType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
}

type ResearchSource struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Domain  string `json:"domain"`
	Snippet string `json:"snippet,omitempty"`
}

type RunPromptContext struct {
	Tools         []ToolDescriptor
	Mode          RunMode
	AnalysisDepth AnalysisDepth
	Attachments   []AttachmentDescriptor
}

type Definition struct {
	ID                string
	Name              string
	Description       string
	DefaultMaxSteps   int
	Timeout           time.Duration
	ModelParameters   types.InferenceParameters
	Tools             []string
	BuildSystemPrompt func(RunPromptContext) string
}

type ToolDefinition struct {
	Name        string
	Description string
	Modes       []RunMode
	Permission  string
	Handler     ToolHandler
}

type ToolDescriptor struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Modes       []RunMode `json:"modes,omitempty"`
}

type AgentDescriptor struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Description     string           `json:"description"`
	DefaultMaxSteps int              `json:"default_max_steps"`
	Tools           []ToolDescriptor `json:"tools"`
}

type ToolCallContext struct {
	Run         *Run
	Actor       *auth.KeyRecord
	Attachments []*Attachment
}

type ToolHandler func(ctx context.Context, call ToolCallContext, arguments json.RawMessage) (any, error)

type ToolCallEnvelope struct {
	Type      string          `json:"type"`
	ToolName  string          `json:"tool_name,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Message   string          `json:"message,omitempty"`
}

type CustomDefinition struct {
	ID             string    `json:"id"`
	WorkspaceID    string    `json:"workspace_id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	SystemPrompt   string    `json:"system_prompt"`
	Tools          []string  `json:"tools"`
	MaxSteps       int       `json:"max_steps"`
	TimeoutSeconds int       `json:"timeout_seconds"`
	Model          string    `json:"model,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type CreateRunRequest struct {
	AgentID       string
	Mode          RunMode
	AnalysisDepth AnalysisDepth
	Model         string
	Input         string
	MaxSteps      int
	AttachmentIDs []string
}

type CreateCustomDefinitionRequest struct {
	Name           string
	Description    string
	SystemPrompt   string
	Tools          []string
	MaxSteps       int
	TimeoutSeconds int
	Model          string
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

// WebhookConfig stores a registered webhook endpoint for a workspace.
// The Secret field is intentionally excluded from JSON serialization so it
// is never returned in API responses.
type WebhookConfig struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	URL         string    `json:"url"`
	Secret      string    `json:"-"` // Never expose in API responses
	Events      []string  `json:"events"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
