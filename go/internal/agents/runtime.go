package agents

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/infera/infera/go/internal/auth"
	"github.com/infera/infera/go/pkg/types"
)

const defaultTimeout = 45 * time.Second

type Runtime struct {
	store  *Store
	runner ModelRunner

	mu          sync.RWMutex
	definitions map[string]Definition
	tools       map[string]ToolDefinition

	cancelMu sync.Mutex
	cancels  map[string]context.CancelFunc
}

func NewRuntime(store *Store, runner ModelRunner) *Runtime {
	return &Runtime{
		store:       store,
		runner:      runner,
		definitions: make(map[string]Definition),
		tools:       make(map[string]ToolDefinition),
		cancels:     make(map[string]context.CancelFunc),
	}
}

func (r *Runtime) RecoverInterruptedRuns() (int64, error) {
	return r.store.MarkInterruptedRuns(time.Now().UTC(), "agent run interrupted during gateway startup")
}

func (r *Runtime) RegisterDefinition(def Definition) error {
	def.ID = strings.TrimSpace(def.ID)
	def.Name = strings.TrimSpace(def.Name)
	def.Description = strings.TrimSpace(def.Description)
	if def.ID == "" {
		return fmt.Errorf("agent definition id is required")
	}
	if def.Name == "" {
		return fmt.Errorf("agent definition name is required")
	}
	if def.DefaultMaxSteps <= 0 {
		return fmt.Errorf("agent definition default_max_steps must be positive")
	}
	if def.Timeout <= 0 {
		def.Timeout = defaultTimeout
	}
	if def.ModelParameters.MaxTokens <= 0 {
		def.ModelParameters = types.DefaultInferenceParameters()
		def.ModelParameters.MaxTokens = 512
		def.ModelParameters.Temperature = 0.1
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.definitions[def.ID] = def
	return nil
}

func (r *Runtime) RegisterTool(tool ToolDefinition) error {
	tool.Name = strings.TrimSpace(tool.Name)
	tool.Description = strings.TrimSpace(tool.Description)
	if tool.Name == "" {
		return fmt.Errorf("tool name is required")
	}
	if tool.Description == "" {
		return fmt.Errorf("tool description is required")
	}
	if tool.Handler == nil {
		return fmt.Errorf("tool handler is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name] = tool
	return nil
}

func (r *Runtime) ListDefinitions(actor *auth.KeyRecord) []AgentDescriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]AgentDescriptor, 0, len(r.definitions))
	for _, def := range r.definitions {
		out = append(out, AgentDescriptor{
			ID:              def.ID,
			Name:            def.Name,
			Description:     def.Description,
			DefaultMaxSteps: def.DefaultMaxSteps,
			Tools:           r.toolDescriptorsForDefinition(actor, def),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func (r *Runtime) getDefinition(agentID string) (Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.definitions[strings.TrimSpace(agentID)]
	return def, ok
}

func (r *Runtime) toolDescriptorsForDefinition(actor *auth.KeyRecord, def Definition) []ToolDescriptor {
	descriptors := make([]ToolDescriptor, 0, len(def.Tools))
	for _, name := range def.Tools {
		tool, ok := r.tools[name]
		if !ok {
			continue
		}
		if tool.Permission != "" && !auth.HasPermission(actor, tool.Permission) {
			continue
		}
		descriptors = append(descriptors, ToolDescriptor{
			Name:        tool.Name,
			Description: tool.Description,
		})
	}
	sort.Slice(descriptors, func(i, j int) bool {
		return descriptors[i].Name < descriptors[j].Name
	})
	return descriptors
}

func (r *Runtime) availableTools(actor *auth.KeyRecord, def Definition) map[string]ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make(map[string]ToolDefinition, len(def.Tools))
	for _, name := range def.Tools {
		tool, ok := r.tools[name]
		if !ok {
			continue
		}
		if tool.Permission != "" && !auth.HasPermission(actor, tool.Permission) {
			continue
		}
		out[name] = tool
	}
	return out
}

func (r *Runtime) ListRuns(workspaceID string, limit int) ([]*Run, error) {
	return r.store.ListRuns(workspaceID, limit)
}

func (r *Runtime) GetRunDetail(workspaceID, runID string) (*RunDetail, error) {
	return r.store.GetRunDetail(workspaceID, runID)
}

func (r *Runtime) CreateRun(ctx context.Context, actor *auth.KeyRecord, session *auth.SessionRecord, req CreateRunRequest) (*Run, error) {
	agentID := strings.TrimSpace(req.AgentID)
	if agentID == "" {
		agentID = "hermes"
	}
	def, ok := r.getDefinition(agentID)
	if !ok {
		return nil, fmt.Errorf("unknown agent_id %q", agentID)
	}

	maxSteps := req.MaxSteps
	if maxSteps <= 0 {
		maxSteps = def.DefaultMaxSteps
	}

	workspaceID := auth.DefaultWorkspaceID
	createdByKeyID := ""
	if actor != nil {
		if strings.TrimSpace(actor.WorkspaceID) != "" {
			workspaceID = actor.WorkspaceID
		}
		createdByKeyID = actor.ID
	}

	run, err := r.store.CreateRun(workspaceID, createdByKeyID, agentID, req.Model, req.Input, maxSteps, time.Now().UTC())
	if err != nil {
		return nil, err
	}

	go r.executeRun(actor, session, run, def)
	return run, nil
}

func (r *Runtime) CancelRun(workspaceID, runID string) (*Run, error) {
	run, err := r.store.GetRun(workspaceID, runID)
	if err != nil {
		return nil, err
	}
	if run.Status == StatusSucceeded || run.Status == StatusFailed || run.Status == StatusCanceled {
		return run, nil
	}

	now := time.Now().UTC()
	cancelReason := "canceled by user"

	r.cancelMu.Lock()
	cancel := r.cancels[run.ID]
	r.cancelMu.Unlock()
	if cancel != nil {
		cancel()
	}

	if err := r.store.MarkCanceled(workspaceID, runID, cancelReason, now); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	return r.store.GetRun(workspaceID, runID)
}

func (r *Runtime) executeRun(actor *auth.KeyRecord, session *auth.SessionRecord, run *Run, def Definition) {
	now := time.Now().UTC()
	if err := r.store.MarkRunRunning(run.WorkspaceID, run.ID, now); err != nil {
		return
	}
	run.Status = StatusRunning
	run.StartedAt = &now

	timeout := def.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	r.cancelMu.Lock()
	r.cancels[run.ID] = cancel
	r.cancelMu.Unlock()
	defer func() {
		r.cancelMu.Lock()
		delete(r.cancels, run.ID)
		r.cancelMu.Unlock()
	}()

	availableTools := r.availableTools(actor, def)
	toolDescriptors := r.toolDescriptorsForDefinition(actor, def)
	systemPrompt := def.BuildSystemPrompt(toolDescriptors)

	conversation := []types.Message{
		{Role: types.RoleSystem, Content: systemPrompt},
		{Role: types.RoleUser, Content: run.Input},
	}

	for stepIndex := 0; stepIndex < run.MaxSteps; stepIndex++ {
		resp, err := r.runner.Run(ctx, ModelRunRequest{
			Actor:      actor,
			Session:    session,
			Run:        run,
			Messages:   conversation,
			Parameters: def.ModelParameters,
		})
		if err != nil {
			r.handleRunError(run, ctx, fmt.Sprintf("model execution failed: %v", err), err)
			return
		}
		if len(resp.Choices) == 0 {
			r.failRun(run, "model returned no choices")
			return
		}

		assistantMessage := strings.TrimSpace(resp.Choices[0].Message.Content)
		envelope, err := ParseActionEnvelope(assistantMessage)
		if err != nil {
			_, _ = r.store.AppendStep(run.WorkspaceID, run.ID, StepTypeError, "", map[string]any{
				"error":        "invalid_json_action",
				"message":      err.Error(),
				"raw_response": assistantMessage,
			}, time.Now().UTC())
			r.failRun(run, "invalid JSON action from model")
			return
		}

		switch envelope.Type {
		case "tool_call":
			r.handleToolCall(ctx, actor, run, availableTools, assistantMessage, envelope, &conversation)
		case "final":
			if strings.TrimSpace(envelope.Message) == "" {
				_, _ = r.store.AppendStep(run.WorkspaceID, run.ID, StepTypeError, "", map[string]any{
					"error":   "invalid_final_action",
					"message": "final action requires message",
				}, time.Now().UTC())
				r.failRun(run, "final action missing message")
				return
			}
			_, _ = r.store.AppendStep(run.WorkspaceID, run.ID, StepTypeFinal, "", map[string]any{
				"message": envelope.Message,
			}, time.Now().UTC())
			_ = r.store.CompleteRun(run.WorkspaceID, run.ID, StatusSucceeded, envelope.Message, "", time.Now().UTC())
			return
		default:
			_, _ = r.store.AppendStep(run.WorkspaceID, run.ID, StepTypeError, "", map[string]any{
				"error":   "unsupported_action",
				"message": fmt.Sprintf("unsupported action type %q", envelope.Type),
			}, time.Now().UTC())
			r.failRun(run, fmt.Sprintf("unsupported action type %q", envelope.Type))
			return
		}
	}

	_, _ = r.store.AppendStep(run.WorkspaceID, run.ID, StepTypeError, "", map[string]any{
		"error":   "max_steps_exceeded",
		"message": fmt.Sprintf("agent exhausted max_steps=%d without returning a final answer", run.MaxSteps),
	}, time.Now().UTC())
	r.failRun(run, "agent exhausted max steps without a final answer")
}

func (r *Runtime) handleToolCall(ctx context.Context, actor *auth.KeyRecord, run *Run, availableTools map[string]ToolDefinition, assistantMessage string, envelope ToolCallEnvelope, conversation *[]types.Message) {
	now := time.Now().UTC()
	argsPayload := any(map[string]any{})
	if len(envelope.Arguments) > 0 {
		var argsMap map[string]any
		if err := json.Unmarshal(envelope.Arguments, &argsMap); err != nil {
			_, _ = r.store.AppendStep(run.WorkspaceID, run.ID, StepTypeError, envelope.ToolName, map[string]any{
				"error":   "invalid_tool_arguments",
				"message": err.Error(),
			}, now)
			r.failRun(run, "tool call arguments must be a JSON object")
			return
		}
		argsPayload = argsMap
	}
	_, _ = r.store.AppendStep(run.WorkspaceID, run.ID, StepTypeToolCall, envelope.ToolName, map[string]any{
		"arguments": argsPayload,
	}, now)

	tool, ok := availableTools[envelope.ToolName]
	resultPayload := map[string]any{"ok": false}
	if !ok {
		resultPayload["error"] = fmt.Sprintf("tool %q is unavailable for this run", envelope.ToolName)
		_, _ = r.store.AppendStep(run.WorkspaceID, run.ID, StepTypeToolResult, envelope.ToolName, resultPayload, time.Now().UTC())
		*conversation = append(*conversation,
			types.Message{Role: types.RoleAssistant, Content: assistantMessage},
			types.Message{Role: types.RoleUser, Content: formatToolResult(envelope.ToolName, resultPayload)},
		)
		return
	}

	result, err := tool.Handler(ctx, ToolCallContext{Run: run, Actor: actor}, envelope.Arguments)
	if err != nil {
		resultPayload["error"] = err.Error()
	} else {
		resultPayload["ok"] = true
		resultPayload["result"] = result
	}

	_, _ = r.store.AppendStep(run.WorkspaceID, run.ID, StepTypeToolResult, envelope.ToolName, resultPayload, time.Now().UTC())
	*conversation = append(*conversation,
		types.Message{Role: types.RoleAssistant, Content: assistantMessage},
		types.Message{Role: types.RoleUser, Content: formatToolResult(envelope.ToolName, resultPayload)},
	)
}

func (r *Runtime) handleRunError(run *Run, ctx context.Context, message string, err error) {
	now := time.Now().UTC()
	if ctxErr := ctx.Err(); ctxErr != nil {
		if ctxErr == context.Canceled {
			_ = r.store.MarkCanceled(run.WorkspaceID, run.ID, "canceled by user", now)
			return
		}
		_, _ = r.store.AppendStep(run.WorkspaceID, run.ID, StepTypeError, "", map[string]any{
			"error":   "timeout",
			"message": ctxErr.Error(),
		}, now)
		r.failRun(run, "agent timed out")
		return
	}
	if inferaErr, ok := err.(*types.InferaError); ok {
		_, _ = r.store.AppendStep(run.WorkspaceID, run.ID, StepTypeError, "", map[string]any{
			"error":   string(inferaErr.Code),
			"message": inferaErr.Message,
		}, now)
		r.failRun(run, inferaErr.Message)
		return
	}
	_, _ = r.store.AppendStep(run.WorkspaceID, run.ID, StepTypeError, "", map[string]any{
		"error":   "model_error",
		"message": message,
	}, now)
	r.failRun(run, err.Error())
}

func (r *Runtime) failRun(run *Run, reason string) {
	_ = r.store.CompleteRun(run.WorkspaceID, run.ID, StatusFailed, "", reason, time.Now().UTC())
}

func ParseActionEnvelope(raw string) (ToolCallEnvelope, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ToolCallEnvelope{}, fmt.Errorf("empty response")
	}
	var envelope ToolCallEnvelope
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return ToolCallEnvelope{}, err
	}
	if decoder.More() {
		return ToolCallEnvelope{}, fmt.Errorf("response must contain a single JSON object")
	}
	envelope.Type = strings.TrimSpace(envelope.Type)
	envelope.ToolName = strings.TrimSpace(envelope.ToolName)
	envelope.Message = strings.TrimSpace(envelope.Message)
	if envelope.Type == "" {
		return ToolCallEnvelope{}, fmt.Errorf("type is required")
	}
	switch envelope.Type {
	case "tool_call":
		if envelope.ToolName == "" {
			return ToolCallEnvelope{}, fmt.Errorf("tool_name is required for tool_call")
		}
		if len(envelope.Arguments) == 0 {
			envelope.Arguments = json.RawMessage(`{}`)
		}
	case "final":
		if envelope.Message == "" {
			return ToolCallEnvelope{}, fmt.Errorf("message is required for final")
		}
	default:
		return ToolCallEnvelope{}, fmt.Errorf("unsupported action type %q", envelope.Type)
	}
	return envelope, nil
}

func formatToolResult(toolName string, payload map[string]any) string {
	body, err := json.Marshal(payload)
	if err != nil {
		body = []byte(`{"ok":false,"error":"failed to encode tool result"}`)
	}
	return fmt.Sprintf("Tool result for %s: %s\nRespond with a JSON object only.", toolName, string(body))
}
