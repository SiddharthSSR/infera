package types

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Role represents a participant in a conversation.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ToolCall represents a function call made by the model.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall contains the function name and arguments.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolDefinition describes a tool available to the model.
type ToolDefinition struct {
	Type     string         `json:"type"`
	Function FunctionSchema `json:"function"`
}

// FunctionSchema describes a function's signature.
type FunctionSchema struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  interface{} `json:"parameters,omitempty"`
}

// Message represents a single message in a conversation.
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// InferenceParameters controls generation behavior.
type InferenceParameters struct {
	MaxTokens        int      `json:"max_tokens"`
	Temperature      float64  `json:"temperature"`
	TopP             float64  `json:"top_p"`
	TopK             *int     `json:"top_k,omitempty"`
	StopSequences    []string `json:"stop_sequences,omitempty"`
	PresencePenalty  float64  `json:"presence_penalty"`
	FrequencyPenalty float64  `json:"frequency_penalty"`
	Seed             *int64   `json:"seed,omitempty"`
}

// DefaultInferenceParameters returns sensible defaults.
func DefaultInferenceParameters() InferenceParameters {
	return InferenceParameters{
		MaxTokens:   256,
		Temperature: 1.0,
		TopP:        1.0,
	}
}

// Priority represents request priority level.
type Priority int

const (
	PriorityLow    Priority = 1
	PriorityNormal Priority = 2
	PriorityHigh   Priority = 3
)

const (
	MetadataAffinityKey      = "affinity_key"
	MetadataAffinitySource   = "affinity_source"
	MetadataPromptPrefixHash = "prompt_prefix_hash"
	MetadataExplicitAffinity = "explicit"
	MetadataSessionAffinity  = "session_prefix"
	MetadataAPIKeyAffinity   = "api_key_prefix"
	MetadataAgentID          = "agent_id"
	MetadataAgentRunID       = "agent_run_id"
)

// InferenceRequest represents a request for model inference.
type InferenceRequest struct {
	// RequestID is the server-generated identity for one model execution.
	RequestID       string              `json:"request_id"`
	ClientRequestID string              `json:"-"`
	ModelID         string              `json:"model_id"`
	Messages        []Message           `json:"messages"`
	Parameters      InferenceParameters `json:"parameters"`
	Stream          bool                `json:"stream"`
	Priority        Priority            `json:"priority"`
	Metadata        map[string]string   `json:"metadata,omitempty"`
	CreatedAt       time.Time           `json:"created_at"`
	APIKeyID        string              `json:"api_key_id,omitempty"`
	WorkspaceID     string              `json:"-"`
	Tools           []ToolDefinition    `json:"tools,omitempty"`
	ToolChoice      json.RawMessage     `json:"tool_choice,omitempty"`
}

// NewInferenceRequest creates a new request with generated ID.
func NewInferenceRequest(modelID string, messages []Message) *InferenceRequest {
	return &InferenceRequest{
		RequestID:  uuid.New().String(),
		ModelID:    modelID,
		Messages:   messages,
		Parameters: DefaultInferenceParameters(),
		Priority:   PriorityNormal,
		CreatedAt:  time.Now(),
	}
}

// TokenEstimate provides a rough estimate of input tokens.
func (r *InferenceRequest) TokenEstimate() int {
	total := 0
	for _, msg := range r.Messages {
		total += len(msg.Content)
	}
	return total / 4
}

// FinishReason indicates why generation stopped.
type FinishReason string

const (
	FinishReasonStop      FinishReason = "stop"
	FinishReasonLength    FinishReason = "length"
	FinishReasonError     FinishReason = "error"
	FinishReasonToolCalls FinishReason = "tool_calls"
)

// UsageStats tracks token usage.
type UsageStats struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// LatencyStats tracks timing information.
type LatencyStats struct {
	QueueMS            int64 `json:"queue_ms"`
	InferenceMS        int64 `json:"inference_ms"`
	TotalMS            int64 `json:"total_ms"`
	TimeToFirstTokenMS int64 `json:"time_to_first_token_ms"`
}

// Choice represents a single generation output.
type Choice struct {
	Index        int          `json:"index"`
	Message      Message      `json:"message"`
	FinishReason FinishReason `json:"finish_reason"`
}

// InferenceResponse represents the complete response.
type InferenceResponse struct {
	RequestID string       `json:"request_id"`
	ModelID   string       `json:"model_id"`
	Choices   []Choice     `json:"choices"`
	Usage     UsageStats   `json:"usage"`
	Latency   LatencyStats `json:"latency"`
	CreatedAt time.Time    `json:"created_at"`
}

// ToolCallChunkDelta represents an incremental tool call update in a streaming chunk.
type ToolCallChunkDelta struct {
	Index    int           `json:"index"`
	ID       string        `json:"id,omitempty"`
	Type     string        `json:"type,omitempty"`
	Function FunctionDelta `json:"function,omitempty"`
}

// FunctionDelta is the incremental function call content within a streaming chunk.
type FunctionDelta struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// TokenChunk represents a streaming token.
type TokenChunk struct {
	RequestID    string               `json:"request_id"`
	Index        int                  `json:"index"`
	Delta        string               `json:"delta"`
	FinishReason *FinishReason        `json:"finish_reason,omitempty"`
	Usage        *UsageStats          `json:"usage,omitempty"`
	CreatedAt    time.Time            `json:"created_at"`
	ToolCalls    []ToolCallChunkDelta `json:"tool_calls,omitempty"`
}

// IsFinal returns true if this is the last chunk.
func (c *TokenChunk) IsFinal() bool {
	return c.FinishReason != nil
}

// ErrorCode represents error types.
type ErrorCode string

const (
	ErrorCodeInvalidRequest            ErrorCode = "invalid_request"
	ErrorCodeModelNotFound             ErrorCode = "model_not_found"
	ErrorCodeNoWorkersAvailable        ErrorCode = "no_workers_available"
	ErrorCodeRateLimited               ErrorCode = "rate_limited"
	ErrorCodeModelOverloaded           ErrorCode = "model_overloaded"
	ErrorCodeWorkerRegistryUnavailable ErrorCode = "worker_registry_unavailable"
	ErrorCodeInternalError             ErrorCode = "internal_error"
	ErrorCodeTimeout                   ErrorCode = "timeout"
)

// InferaError represents an API error.
type InferaError struct {
	Code              ErrorCode         `json:"code"`
	Message           string            `json:"message"`
	RequestID         string            `json:"request_id,omitempty"`
	RetryAfterSeconds *int              `json:"retry_after_seconds,omitempty"`
	Details           map[string]string `json:"details,omitempty"`
}

func (e *InferaError) Error() string {
	return e.Message
}

// NewInferaError creates a new error.
func NewInferaError(code ErrorCode, message string) *InferaError {
	return &InferaError{Code: code, Message: message}
}

// WithRequestID adds request ID to error.
func (e *InferaError) WithRequestID(requestID string) *InferaError {
	e.RequestID = requestID
	return e
}
