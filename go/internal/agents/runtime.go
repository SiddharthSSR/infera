package agents

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/infera/infera/go/internal/auth"
	"github.com/infera/infera/go/internal/egress"
	"github.com/infera/infera/go/pkg/types"
)

const (
	defaultTimeout          = 45 * time.Second
	webhookEventRunComplete = "agent.run.completed"
)

type Runtime struct {
	store         *Store
	runner        ModelRunner
	webhookClient *http.Client

	mu          sync.RWMutex
	definitions map[string]Definition
	tools       map[string]ToolDefinition

	cancelMu sync.Mutex
	cancels  map[string]context.CancelFunc
}

func NewRuntime(store *Store, runner ModelRunner) *Runtime {
	return &Runtime{
		store:  store,
		runner: runner,
		webhookClient: egress.NewPublicClient(egress.ClientOptions{
			Timeout:        10 * time.Second,
			AllowedSchemes: []string{"https"},
		}),
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
		toolsByName := make(map[string]ToolDescriptor)
		for _, mode := range []RunMode{RunModeOperations, RunModeResearch, RunModeMultimodal} {
			for _, tool := range r.toolDescriptorsForDefinition(actor, def, mode) {
				if existing, ok := toolsByName[tool.Name]; ok {
					existing.Modes = mergeDescriptorModes(existing.Modes, tool.Modes)
					toolsByName[tool.Name] = existing
					continue
				}
				toolsByName[tool.Name] = tool
			}
		}
		tools := make([]ToolDescriptor, 0, len(toolsByName))
		for _, tool := range toolsByName {
			tools = append(tools, tool)
		}
		sort.Slice(tools, func(i, j int) bool {
			return tools[i].Name < tools[j].Name
		})
		out = append(out, AgentDescriptor{
			ID:              def.ID,
			Name:            def.Name,
			Description:     def.Description,
			DefaultMaxSteps: def.DefaultMaxSteps,
			Tools:           tools,
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

func buildCustomDefinition(def *CustomDefinition) Definition {
	params := types.DefaultInferenceParameters()
	params.MaxTokens = 512
	params.Temperature = 0.1

	timeout := time.Duration(def.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	return Definition{
		ID:              def.ID,
		Name:            def.Name,
		Description:     def.Description,
		DefaultMaxSteps: def.MaxSteps,
		Timeout:         timeout,
		ModelParameters: params,
		Tools:           append([]string(nil), def.Tools...),
		BuildSystemPrompt: func(ctx RunPromptContext) string {
			lines := []string{
				strings.TrimSpace(def.SystemPrompt),
				"",
				"Runtime contract:",
				"Use only the tools explicitly listed below.",
				"Respond with exactly one JSON object and no prose before or after it.",
				`Valid actions: {"type":"tool_call","tool_name":"<tool>","arguments":{...}} or {"type":"final","message":"<answer>"}.`,
				`The outer response format must stay JSON-only, but final.message itself must be concise operator-facing prose or markdown, not serialized JSON.`,
				"Never reveal hidden reasoning or chain-of-thought. Surface findings, evidence, uncertainty, and next actions only.",
			}
			if len(ctx.Attachments) > 0 {
				lines = append(lines, "Available attachments:")
				for _, attachment := range ctx.Attachments {
					lines = append(lines, fmt.Sprintf("- %s: %s [%s, %d bytes]", attachment.ID, attachment.FileName, attachment.MIMEType, attachment.SizeBytes))
				}
			}
			if len(ctx.Tools) == 0 {
				lines = append(lines, "No tools are available for this run. Return a final answer without requesting tools.")
				return strings.Join(lines, "\n")
			}
			lines = append(lines, "Available tools:")
			for _, tool := range ctx.Tools {
				lines = append(lines, fmt.Sprintf("- %s: %s", tool.Name, tool.Description))
			}
			return strings.Join(lines, "\n")
		},
	}
}

func (r *Runtime) resolveDefinition(workspaceID, agentID string) (Definition, string, bool, error) {
	if def, ok := r.getDefinition(agentID); ok {
		return def, "", true, nil
	}

	custom, err := r.store.GetCustomDefinition(workspaceID, agentID)
	if err == nil {
		return buildCustomDefinition(custom), strings.TrimSpace(custom.Model), true, nil
	}
	if err == sql.ErrNoRows {
		return Definition{}, "", false, nil
	}
	return Definition{}, "", false, err
}

func toolSupportsMode(tool ToolDefinition, mode RunMode) bool {
	if len(tool.Modes) == 0 {
		return true
	}
	mode = normalizeRunMode(mode)
	for _, candidate := range tool.Modes {
		if normalizeRunMode(candidate) == mode {
			return true
		}
	}
	return false
}

func descriptorModesForTool(tool ToolDefinition) []RunMode {
	if len(tool.Modes) == 0 {
		return []RunMode{RunModeOperations, RunModeResearch, RunModeMultimodal}
	}

	seen := make(map[RunMode]struct{}, len(tool.Modes))
	out := make([]RunMode, 0, len(tool.Modes))
	for _, mode := range tool.Modes {
		normalized := normalizeRunMode(mode)
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i] < out[j]
	})
	return out
}

func mergeDescriptorModes(existing []RunMode, incoming []RunMode) []RunMode {
	seen := make(map[RunMode]struct{}, len(existing)+len(incoming))
	out := make([]RunMode, 0, len(existing)+len(incoming))
	for _, mode := range append(append([]RunMode{}, existing...), incoming...) {
		normalized := normalizeRunMode(mode)
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i] < out[j]
	})
	return out
}

func (r *Runtime) toolDescriptorsForDefinition(actor *auth.KeyRecord, def Definition, mode RunMode) []ToolDescriptor {
	descriptors := make([]ToolDescriptor, 0, len(def.Tools))
	for _, name := range def.Tools {
		tool, ok := r.tools[name]
		if !ok {
			continue
		}
		if !toolSupportsMode(tool, mode) {
			continue
		}
		if tool.Permission != "" && !auth.HasPermission(actor, tool.Permission) {
			continue
		}
		descriptors = append(descriptors, ToolDescriptor{
			Name:        tool.Name,
			Description: tool.Description,
			Modes:       descriptorModesForTool(tool),
		})
	}
	sort.Slice(descriptors, func(i, j int) bool {
		return descriptors[i].Name < descriptors[j].Name
	})
	return descriptors
}

func (r *Runtime) availableTools(actor *auth.KeyRecord, def Definition, mode RunMode) map[string]ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make(map[string]ToolDefinition, len(def.Tools))
	for _, name := range def.Tools {
		tool, ok := r.tools[name]
		if !ok {
			continue
		}
		if !toolSupportsMode(tool, mode) {
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

func (r *Runtime) CreateWebhookConfig(workspaceID, url, secret string, events []string) (*WebhookConfig, error) {
	normalizedURL, err := validateWebhookURL(url)
	if err != nil {
		return nil, err
	}
	normalizedEvents, err := normalizeWebhookEvents(events)
	if err != nil {
		return nil, err
	}
	return r.store.CreateWebhookConfig(workspaceID, normalizedURL, secret, normalizedEvents)
}

func (r *Runtime) ListWebhookConfigs(workspaceID string) ([]*WebhookConfig, error) {
	return r.store.ListWebhookConfigs(workspaceID)
}

func (r *Runtime) DeleteWebhookConfig(workspaceID, webhookID string) error {
	return r.store.DeleteWebhookConfig(workspaceID, webhookID)
}

func (r *Runtime) CreateCustomDefinition(workspaceID string, req CreateCustomDefinitionRequest) (*CustomDefinition, error) {
	r.mu.RLock()
	knownTools := r.tools
	r.mu.RUnlock()

	for _, toolName := range req.Tools {
		if _, ok := knownTools[strings.TrimSpace(toolName)]; !ok {
			return nil, fmt.Errorf("unknown tool %q", toolName)
		}
	}

	return r.store.CreateCustomDefinition(
		workspaceID,
		req.Name,
		req.Description,
		req.SystemPrompt,
		req.Tools,
		req.MaxSteps,
		req.TimeoutSeconds,
		req.Model,
		time.Now().UTC(),
	)
}

func (r *Runtime) ListCustomDefinitions(workspaceID string) ([]*CustomDefinition, error) {
	return r.store.ListCustomDefinitions(workspaceID)
}

func (r *Runtime) GetCustomDefinition(workspaceID, defID string) (*CustomDefinition, error) {
	return r.store.GetCustomDefinition(workspaceID, defID)
}

func (r *Runtime) DeleteCustomDefinition(workspaceID, defID string) error {
	return r.store.DeleteCustomDefinition(workspaceID, defID)
}

func (r *Runtime) AttachmentRoot() string {
	return r.store.AttachmentRoot()
}

func (r *Runtime) CreateAttachment(
	actor *auth.KeyRecord,
	fileName,
	mimeType string,
	sizeBytes int64,
	width,
	height int,
	sha256,
	storagePath string,
) (*Attachment, error) {
	workspaceID := auth.DefaultWorkspaceID
	createdByKeyID := ""
	if actor != nil {
		if strings.TrimSpace(actor.WorkspaceID) != "" {
			workspaceID = actor.WorkspaceID
		}
		createdByKeyID = actor.ID
	}
	return r.store.CreateAttachment(workspaceID, createdByKeyID, fileName, mimeType, sizeBytes, width, height, sha256, storagePath, time.Now().UTC())
}

func (r *Runtime) GetRun(workspaceID, runID string) (*Run, error) {
	return r.store.GetRun(workspaceID, runID)
}

func (r *Runtime) ListStepsAfter(workspaceID, runID string, afterIndex int) ([]*RunStep, error) {
	return r.store.ListStepsAfter(workspaceID, runID, afterIndex)
}

func (r *Runtime) GetRunDetail(workspaceID, runID string) (*RunDetail, error) {
	detail, err := r.store.GetRunDetail(workspaceID, runID)
	if err != nil {
		return nil, err
	}
	detail.Sources = collectResearchSources(detail.Steps)
	return detail, nil
}

func (r *Runtime) CreateRun(ctx context.Context, actor *auth.KeyRecord, session *auth.SessionRecord, req CreateRunRequest) (*Run, error) {
	workspaceID := auth.DefaultWorkspaceID
	createdByKeyID := ""
	if actor != nil {
		if strings.TrimSpace(actor.WorkspaceID) != "" {
			workspaceID = actor.WorkspaceID
		}
		createdByKeyID = actor.ID
	}

	agentID := strings.TrimSpace(req.AgentID)
	if agentID == "" {
		agentID = "hermes"
	}
	def, defaultModel, ok, err := r.resolveDefinition(workspaceID, agentID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("unknown agent_id %q", agentID)
	}

	mode := normalizeRunMode(req.Mode)
	analysisDepth := normalizeAnalysisDepth(req.AnalysisDepth)
	if mode != RunModeMultimodal && len(req.AttachmentIDs) > 0 {
		return nil, fmt.Errorf("attachments are only valid for multimodal runs")
	}

	maxSteps := req.MaxSteps
	if maxSteps <= 0 {
		maxSteps = def.DefaultMaxSteps
		if analysisDepth == AnalysisDepthDeep && maxSteps < 12 {
			maxSteps = 12
		}
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = defaultModel
	}
	if model == "" {
		return nil, fmt.Errorf("model is required")
	}

	attachments, err := r.store.ListAttachmentsByID(workspaceID, req.AttachmentIDs)
	if err != nil {
		return nil, err
	}
	for _, attachment := range attachments {
		if attachment.RunID != "" {
			return nil, fmt.Errorf("attachment %q is already attached to another run", attachment.ID)
		}
	}

	run, err := r.store.CreateRun(workspaceID, createdByKeyID, agentID, mode, analysisDepth, model, req.Input, maxSteps, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if err := r.store.AttachAttachmentsToRun(workspaceID, run.ID, req.AttachmentIDs); err != nil {
		_ = r.store.CompleteRun(workspaceID, run.ID, StatusFailed, "", err.Error(), time.Now().UTC())
		return nil, err
	}

	go r.executeRun(actor, session, run, adjustDefinitionForRun(def, run))
	return run, nil
}

func adjustDefinitionForRun(def Definition, run *Run) Definition {
	adjusted := def
	if adjusted.Timeout <= 0 {
		adjusted.Timeout = defaultTimeout
	}
	if adjusted.ModelParameters.MaxTokens <= 0 {
		adjusted.ModelParameters = types.DefaultInferenceParameters()
	}
	if run != nil && run.AnalysisDepth == AnalysisDepthDeep {
		if adjusted.Timeout < 90*time.Second {
			adjusted.Timeout = 90 * time.Second
		} else {
			adjusted.Timeout *= 2
		}
		if adjusted.ModelParameters.MaxTokens < 1024 {
			adjusted.ModelParameters.MaxTokens = 1024
		}
	}
	return adjusted
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

func attachmentDescriptors(attachments []*Attachment) []AttachmentDescriptor {
	descriptors := make([]AttachmentDescriptor, 0, len(attachments))
	for _, attachment := range attachments {
		descriptors = append(descriptors, AttachmentDescriptor{
			ID:        attachment.ID,
			FileName:  attachment.FileName,
			MIMEType:  attachment.MIMEType,
			SizeBytes: attachment.SizeBytes,
			Width:     attachment.Width,
			Height:    attachment.Height,
		})
	}
	sort.Slice(descriptors, func(i, j int) bool {
		return descriptors[i].ID < descriptors[j].ID
	})
	return descriptors
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

	// Fire webhooks after the run reaches a terminal state. The deferred
	// function reads the final run record from the store so that it sees the
	// committed status regardless of which code path ended the run.
	defer func() {
		finalRun, err := r.store.GetRun(run.WorkspaceID, run.ID)
		if err != nil {
			return
		}
		r.fireWebhooks(finalRun)
	}()

	attachments, err := r.store.ListAttachmentsForRun(run.WorkspaceID, run.ID)
	if err != nil {
		r.failRun(run, fmt.Sprintf("failed to load attachments: %v", err))
		return
	}

	availableTools := r.availableTools(actor, def, run.Mode)
	toolDescriptors := r.toolDescriptorsForDefinition(actor, def, run.Mode)
	systemPrompt := def.BuildSystemPrompt(RunPromptContext{
		Tools:         toolDescriptors,
		Mode:          run.Mode,
		AnalysisDepth: run.AnalysisDepth,
		Attachments:   attachmentDescriptors(attachments),
	})

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
			r.handleToolCall(ctx, actor, run, attachments, availableTools, assistantMessage, envelope, &conversation)
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

func (r *Runtime) handleToolCall(
	ctx context.Context,
	actor *auth.KeyRecord,
	run *Run,
	attachments []*Attachment,
	availableTools map[string]ToolDefinition,
	assistantMessage string,
	envelope ToolCallEnvelope,
	conversation *[]types.Message,
) {
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

	result, err := tool.Handler(ctx, ToolCallContext{Run: run, Actor: actor, Attachments: attachments}, envelope.Arguments)
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

// fireWebhooks looks up active webhook subscribers for the run's terminal
// status and dispatches each delivery in a separate goroutine so that
// webhook latency never delays run completion or SSE polling.
func (r *Runtime) fireWebhooks(run *Run) {
	webhooks, err := r.activeWebhooksForRun(run)
	if err != nil || len(webhooks) == 0 {
		return
	}

	payload, err := json.Marshal(map[string]interface{}{
		"event":  string(run.Status),
		"run":    run,
		"output": run.FinalOutput,
	})
	if err != nil {
		return
	}

	for _, wh := range webhooks {
		wh := wh // capture loop variable
		go r.deliverWebhook(wh, payload)
	}
}

func (r *Runtime) activeWebhooksForRun(run *Run) ([]*WebhookConfig, error) {
	if run == nil {
		return nil, nil
	}

	events := []string{webhookEventRunComplete}
	if status := strings.TrimSpace(string(run.Status)); status != "" {
		events = append(events, status)
	}

	merged := make([]*WebhookConfig, 0)
	seen := make(map[string]struct{})
	for _, event := range events {
		webhooks, err := r.store.GetActiveWebhooksForEvent(run.WorkspaceID, event)
		if err != nil {
			return nil, err
		}
		for _, webhook := range webhooks {
			if _, ok := seen[webhook.ID]; ok {
				continue
			}
			seen[webhook.ID] = struct{}{}
			merged = append(merged, webhook)
		}
	}
	return merged, nil
}

// deliverWebhook performs a single HTTPS POST to the registered webhook URL.
// It signs the payload with HMAC-SHA256 when a secret is configured and
// silently discards errors so that a misbehaving receiver never affects the
// gateway's request path.
func (r *Runtime) deliverWebhook(wh *WebhookConfig, payload []byte) {
	if _, err := validateWebhookURL(wh.URL); err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wh.URL, bytes.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Infera-Event", webhookEventRunComplete)
	if eventStatus := payloadEventStatus(payload); eventStatus != "" {
		req.Header.Set("X-Infera-Run-Status", eventStatus)
	}

	// HMAC-SHA256 signature so receivers can verify the request origin.
	if wh.Secret != "" {
		mac := hmac.New(sha256.New, []byte(wh.Secret))
		mac.Write(payload)
		sig := hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-Infera-Signature", "sha256="+sig)
	}

	resp, err := r.webhookClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
}

func payloadEventStatus(payload []byte) string {
	var body struct {
		Event string `json:"event"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return ""
	}
	return strings.TrimSpace(body.Event)
}

func normalizeWebhookEvents(events []string) ([]string, error) {
	if len(events) == 0 {
		return []string{webhookEventRunComplete}, nil
	}

	allowed := map[string]struct{}{
		webhookEventRunComplete: {},
		"succeeded":             {},
		"failed":                {},
		"canceled":              {},
	}
	seen := make(map[string]struct{}, len(events))
	out := make([]string, 0, len(events))
	for _, event := range events {
		normalized := strings.TrimSpace(event)
		if _, ok := allowed[normalized]; !ok {
			return nil, fmt.Errorf("unsupported webhook event %q", event)
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out, nil
}

func validateWebhookURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("url is required")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid webhook url: %w", err)
	}
	if strings.ToLower(parsed.Scheme) != "https" {
		return "", fmt.Errorf("webhook url must use https")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("webhook url host is required")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("webhook url must not include userinfo")
	}

	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		return "", fmt.Errorf("webhook url host is required")
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || !strings.Contains(host, ".") {
		return "", fmt.Errorf("webhook url must target a public host")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified() {
			return "", fmt.Errorf("webhook url must target a public host")
		}
	}

	parsed.Fragment = ""
	if err := egress.ValidateURL(parsed, []string{"https"}); err != nil {
		return "", fmt.Errorf("webhook url must target a public host: %w", err)
	}
	return parsed.String(), nil
}

func collectResearchSources(steps []*RunStep) []ResearchSource {
	seen := make(map[string]bool)
	sources := make([]ResearchSource, 0)
	for _, step := range steps {
		if step.ToolName != "web_search" || step.Type != StepTypeToolResult {
			continue
		}
		var payload struct {
			OK     bool `json:"ok"`
			Result struct {
				Results []ResearchSource `json:"results"`
			} `json:"result"`
		}
		if err := json.Unmarshal(step.Payload, &payload); err != nil {
			continue
		}
		for _, source := range payload.Result.Results {
			if strings.TrimSpace(source.URL) == "" || seen[source.URL] {
				continue
			}
			seen[source.URL] = true
			sources = append(sources, source)
		}
	}
	return sources
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
	if envelope.Type != "" && envelope.Type != "tool_call" && envelope.Type != "final" && envelope.Message == "" {
		if envelope.ToolName == "" {
			envelope.ToolName = envelope.Type
		}
		envelope.Type = "tool_call"
	}
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
