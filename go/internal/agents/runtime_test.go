package agents

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/infera/infera/go/internal/auth"
	"github.com/infera/infera/go/pkg/types"
)

type fakeRunner struct {
	mu        sync.Mutex
	responses []string
	errs      []error
	block     chan struct{}
}

func (f *fakeRunner) Run(ctx context.Context, req ModelRunRequest) (*types.InferenceResponse, error) {
	if f.block != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-f.block:
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		return nil, err
	}
	if len(f.responses) == 0 {
		return nil, errors.New("no fake response configured")
	}
	content := f.responses[0]
	f.responses = f.responses[1:]
	return &types.InferenceResponse{
		RequestID: req.Run.ID,
		ModelID:   req.Run.Model,
		Choices: []types.Choice{
			{
				Index: 0,
				Message: types.Message{
					Role:    types.RoleAssistant,
					Content: content,
				},
				FinishReason: types.FinishReasonStop,
			},
		},
	}, nil
}

func newTestRuntime(t *testing.T, runner ModelRunner) *Runtime {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "agents.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	runtime := NewRuntime(store, runner)
	def := Definition{
		ID:              "hermes",
		Name:            "Hermes",
		Description:     "test agent",
		DefaultMaxSteps: 4,
		Timeout:         2 * time.Second,
		ModelParameters: func() types.InferenceParameters {
			params := types.DefaultInferenceParameters()
			params.MaxTokens = 256
			return params
		}(),
		Tools: []string{"echo", "secure"},
		BuildSystemPrompt: func(ctx RunPromptContext) string {
			return "Return JSON only."
		},
	}
	if err := runtime.RegisterDefinition(def); err != nil {
		t.Fatalf("RegisterDefinition: %v", err)
	}
	if err := runtime.RegisterTool(ToolDefinition{
		Name:        "echo",
		Description: "echo tool",
		Handler: func(ctx context.Context, call ToolCallContext, arguments json.RawMessage) (any, error) {
			return map[string]any{"echo": "ok"}, nil
		},
	}); err != nil {
		t.Fatalf("RegisterTool echo: %v", err)
	}
	if err := runtime.RegisterTool(ToolDefinition{
		Name:        "secure",
		Description: "restricted tool",
		Permission:  auth.PermissionViewInfrastructure,
		Handler: func(ctx context.Context, call ToolCallContext, arguments json.RawMessage) (any, error) {
			return map[string]any{"secure": true}, nil
		},
	}); err != nil {
		t.Fatalf("RegisterTool secure: %v", err)
	}
	return runtime
}

func waitForStatus(t *testing.T, runtime *Runtime, workspaceID, runID string, allowed ...Status) *RunDetail {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		detail, err := runtime.GetRunDetail(workspaceID, runID)
		if err == nil {
			for _, status := range allowed {
				if detail.Run.Status == status {
					return detail
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	detail, err := runtime.GetRunDetail(workspaceID, runID)
	if err != nil {
		t.Fatalf("GetRunDetail: %v", err)
	}
	t.Fatalf("timed out waiting for statuses %v, got %s", allowed, detail.Run.Status)
	return nil
}

func TestRuntimeHappyPath(t *testing.T) {
	runner := &fakeRunner{
		responses: []string{
			`{"type":"tool_call","tool_name":"echo","arguments":{}}`,
			`{"type":"final","message":"done"}`,
		},
	}
	runtime := newTestRuntime(t, runner)
	actor := &auth.KeyRecord{ID: "key-1", WorkspaceID: "ws_alpha", Role: auth.RoleOwner, PrincipalType: auth.PrincipalHuman, Status: "active"}

	run, err := runtime.CreateRun(context.Background(), actor, nil, CreateRunRequest{
		Model: "model-a",
		Input: "inspect",
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	detail := waitForStatus(t, runtime, "ws_alpha", run.ID, StatusSucceeded)
	if detail.Run.FinalOutput != "done" {
		t.Fatalf("expected final output 'done', got %q", detail.Run.FinalOutput)
	}
	if len(detail.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(detail.Steps))
	}
	if detail.Steps[0].Type != StepTypeToolCall || detail.Steps[1].Type != StepTypeToolResult || detail.Steps[2].Type != StepTypeFinal {
		t.Fatalf("unexpected step order: %+v", detail.Steps)
	}
}

func TestRuntimeFinalWithTopLevelSourcesSucceeds(t *testing.T) {
	runner := &fakeRunner{
		responses: []string{
			`{"type":"final","message":"done","Sources":["https://status.openai.com/"]}`,
		},
	}
	runtime := newTestRuntime(t, runner)
	actor := &auth.KeyRecord{ID: "key-1", WorkspaceID: "ws_alpha", Role: auth.RoleOwner, PrincipalType: auth.PrincipalHuman, Status: "active"}

	run, err := runtime.CreateRun(context.Background(), actor, nil, CreateRunRequest{
		Model: "model-a",
		Input: "inspect",
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	detail := waitForStatus(t, runtime, "ws_alpha", run.ID, StatusSucceeded)
	if detail.Run.FinalOutput != "done" {
		t.Fatalf("expected final output 'done', got %q", detail.Run.FinalOutput)
	}
	if len(detail.Steps) != 1 || detail.Steps[0].Type != StepTypeFinal {
		t.Fatalf("expected one final step, got %+v", detail.Steps)
	}
}

func TestRuntimeNormalizesShortcutToolAction(t *testing.T) {
	runner := &fakeRunner{
		responses: []string{
			`{"type":"echo","tool_name":"echo","arguments":{}}`,
			`{"type":"final","message":"done"}`,
		},
	}
	runtime := newTestRuntime(t, runner)
	actor := &auth.KeyRecord{ID: "key-1", WorkspaceID: "ws_alpha", Role: auth.RoleOwner, PrincipalType: auth.PrincipalHuman, Status: "active"}

	run, err := runtime.CreateRun(context.Background(), actor, nil, CreateRunRequest{
		Model: "model-a",
		Input: "inspect",
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	detail := waitForStatus(t, runtime, "ws_alpha", run.ID, StatusSucceeded)
	if detail.Run.FinalOutput != "done" {
		t.Fatalf("expected final output 'done', got %q", detail.Run.FinalOutput)
	}
	if len(detail.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(detail.Steps))
	}
	if detail.Steps[0].ToolName != "echo" || detail.Steps[0].Type != StepTypeToolCall {
		t.Fatalf("expected normalized tool call, got %+v", detail.Steps[0])
	}
}

func TestRuntimeInvalidJSONFails(t *testing.T) {
	runtime := newTestRuntime(t, &fakeRunner{responses: []string{"not json"}})
	actor := &auth.KeyRecord{ID: "key-1", WorkspaceID: "ws_alpha", Role: auth.RoleOwner, PrincipalType: auth.PrincipalHuman, Status: "active"}

	run, err := runtime.CreateRun(context.Background(), actor, nil, CreateRunRequest{
		Model: "model-a",
		Input: "inspect",
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	detail := waitForStatus(t, runtime, "ws_alpha", run.ID, StatusFailed)
	if len(detail.Steps) != 1 || detail.Steps[0].Type != StepTypeError {
		t.Fatalf("expected one error step, got %+v", detail.Steps)
	}
}

func TestRuntimeUnknownToolCanRecover(t *testing.T) {
	runtime := newTestRuntime(t, &fakeRunner{
		responses: []string{
			`{"type":"tool_call","tool_name":"missing","arguments":{}}`,
			`{"type":"final","message":"fallback"}`,
		},
	})
	actor := &auth.KeyRecord{ID: "key-1", WorkspaceID: "ws_alpha", Role: auth.RoleOwner, PrincipalType: auth.PrincipalHuman, Status: "active"}

	run, err := runtime.CreateRun(context.Background(), actor, nil, CreateRunRequest{
		Model: "model-a",
		Input: "inspect",
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	detail := waitForStatus(t, runtime, "ws_alpha", run.ID, StatusSucceeded)
	if detail.Run.FinalOutput != "fallback" {
		t.Fatalf("expected fallback final output, got %q", detail.Run.FinalOutput)
	}
	if len(detail.Steps) < 2 || detail.Steps[1].Type != StepTypeToolResult {
		t.Fatalf("expected tool result error before final, got %+v", detail.Steps)
	}
}

func TestRuntimeUnauthorizedToolCanRecover(t *testing.T) {
	runtime := newTestRuntime(t, &fakeRunner{
		responses: []string{
			`{"type":"tool_call","tool_name":"secure","arguments":{}}`,
			`{"type":"final","message":"no access"}`,
		},
	})
	actor := &auth.KeyRecord{ID: "key-1", WorkspaceID: "ws_alpha", Role: auth.RoleUser, PrincipalType: auth.PrincipalHuman, Status: "active"}

	run, err := runtime.CreateRun(context.Background(), actor, nil, CreateRunRequest{
		Model: "model-a",
		Input: "inspect",
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	detail := waitForStatus(t, runtime, "ws_alpha", run.ID, StatusSucceeded)
	if detail.Run.FinalOutput != "no access" {
		t.Fatalf("expected final output after unauthorized tool, got %q", detail.Run.FinalOutput)
	}
}

func TestRuntimeStepBudgetExceeded(t *testing.T) {
	runtime := newTestRuntime(t, &fakeRunner{
		responses: []string{
			`{"type":"tool_call","tool_name":"echo","arguments":{}}`,
			`{"type":"tool_call","tool_name":"echo","arguments":{}}`,
		},
	})
	actor := &auth.KeyRecord{ID: "key-1", WorkspaceID: "ws_alpha", Role: auth.RoleOwner, PrincipalType: auth.PrincipalHuman, Status: "active"}

	run, err := runtime.CreateRun(context.Background(), actor, nil, CreateRunRequest{
		Model:    "model-a",
		Input:    "inspect",
		MaxSteps: 1,
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	detail := waitForStatus(t, runtime, "ws_alpha", run.ID, StatusFailed)
	if !strings.Contains(detail.Run.FailureReason, "max steps") {
		t.Fatalf("expected max steps failure reason, got %q", detail.Run.FailureReason)
	}
}

func TestRuntimeCancelRun(t *testing.T) {
	block := make(chan struct{})
	runtime := newTestRuntime(t, &fakeRunner{block: block})
	actor := &auth.KeyRecord{ID: "key-1", WorkspaceID: "ws_alpha", Role: auth.RoleOwner, PrincipalType: auth.PrincipalHuman, Status: "active"}

	run, err := runtime.CreateRun(context.Background(), actor, nil, CreateRunRequest{
		Model: "model-a",
		Input: "inspect",
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	canceled, err := runtime.CancelRun("ws_alpha", run.ID)
	if err != nil {
		t.Fatalf("CancelRun: %v", err)
	}
	if canceled.Status != StatusCanceled && canceled.Status != StatusRunning {
		t.Fatalf("expected canceled or running during cancel transition, got %s", canceled.Status)
	}

	close(block)
	detail := waitForStatus(t, runtime, "ws_alpha", run.ID, StatusCanceled)
	if detail.Run.Status != StatusCanceled {
		t.Fatalf("expected canceled status, got %s", detail.Run.Status)
	}
}

func TestRuntimeDeepAnalysisUsesLargerDefaultBudget(t *testing.T) {
	runtime := newTestRuntime(t, &fakeRunner{
		responses: []string{`{"type":"final","message":"done"}`},
	})
	actor := &auth.KeyRecord{ID: "key-1", WorkspaceID: "ws_alpha", Role: auth.RoleOwner, PrincipalType: auth.PrincipalHuman, Status: "active"}

	run, err := runtime.CreateRun(context.Background(), actor, nil, CreateRunRequest{
		Model:         "model-a",
		Input:         "inspect deeply",
		AnalysisDepth: AnalysisDepthDeep,
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if run.AnalysisDepth != AnalysisDepthDeep {
		t.Fatalf("expected deep analysis depth, got %s", run.AnalysisDepth)
	}
	if run.MaxSteps != 12 {
		t.Fatalf("expected deep analysis default max steps 12, got %d", run.MaxSteps)
	}
}

func TestListDefinitionsIncludesModeSpecificTools(t *testing.T) {
	runtime := newTestRuntime(t, &fakeRunner{responses: []string{`{"type":"final","message":"done"}`}})
	if err := runtime.RegisterTool(ToolDefinition{
		Name:        "research_only",
		Description: "research tool",
		Modes:       []RunMode{RunModeResearch},
		Handler: func(ctx context.Context, call ToolCallContext, arguments json.RawMessage) (any, error) {
			return nil, nil
		},
	}); err != nil {
		t.Fatalf("RegisterTool research_only: %v", err)
	}
	if err := runtime.RegisterTool(ToolDefinition{
		Name:        "vision_only",
		Description: "vision tool",
		Modes:       []RunMode{RunModeMultimodal},
		Handler: func(ctx context.Context, call ToolCallContext, arguments json.RawMessage) (any, error) {
			return nil, nil
		},
	}); err != nil {
		t.Fatalf("RegisterTool vision_only: %v", err)
	}

	def := runtime.definitions["hermes"]
	def.Tools = append(def.Tools, "research_only", "vision_only")
	runtime.definitions["hermes"] = def

	descriptors := runtime.ListDefinitions(&auth.KeyRecord{Role: auth.RoleOwner, PrincipalType: auth.PrincipalHuman, Status: "active"})
	if len(descriptors) != 1 {
		t.Fatalf("expected one descriptor, got %+v", descriptors)
	}
	toolNames := make([]string, 0, len(descriptors[0].Tools))
	toolModes := make(map[string][]RunMode, len(descriptors[0].Tools))
	for _, tool := range descriptors[0].Tools {
		toolNames = append(toolNames, tool.Name)
		toolModes[tool.Name] = tool.Modes
	}
	if !strings.Contains(strings.Join(toolNames, ","), "research_only") || !strings.Contains(strings.Join(toolNames, ","), "vision_only") {
		t.Fatalf("expected mode-specific tools in descriptor, got %+v", toolNames)
	}
	if got := toolModes["research_only"]; len(got) != 1 || got[0] != RunModeResearch {
		t.Fatalf("expected research_only modes [research], got %+v", got)
	}
	if got := toolModes["vision_only"]; len(got) != 1 || got[0] != RunModeMultimodal {
		t.Fatalf("expected vision_only modes [multimodal], got %+v", got)
	}
	if got := toolModes["echo"]; len(got) != 3 {
		t.Fatalf("expected echo to be available in all modes, got %+v", got)
	}
}

func TestRuntimeGetRunDetailCollectsResearchSources(t *testing.T) {
	runtime := newTestRuntime(t, &fakeRunner{
		responses: []string{
			`{"type":"tool_call","tool_name":"echo","arguments":{}}`,
			`{"type":"final","message":"done"}`,
		},
	})

	now := time.Now().UTC()
	run, err := runtime.store.CreateRun("ws_alpha", "key-1", "hermes", RunModeResearch, AnalysisDepthStandard, "model-a", "inspect", 4, now)
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if _, err := runtime.store.AppendStep(run.WorkspaceID, run.ID, StepTypeToolResult, "web_search", map[string]any{
		"ok": true,
		"result": map[string]any{
			"results": []map[string]any{
				{
					"title":  "RunPod Status",
					"url":    "https://status.runpod.io/",
					"domain": "status.runpod.io",
				},
			},
		},
	}, now.Add(time.Second)); err != nil {
		t.Fatalf("AppendStep: %v", err)
	}

	detail, err := runtime.GetRunDetail("ws_alpha", run.ID)
	if err != nil {
		t.Fatalf("GetRunDetail: %v", err)
	}
	if len(detail.Sources) != 1 || detail.Sources[0].Domain != "status.runpod.io" {
		t.Fatalf("expected derived research source, got %+v", detail.Sources)
	}
}

func TestNormalizeWebhookEventsAcceptsCompletedEvent(t *testing.T) {
	events, err := normalizeWebhookEvents([]string{webhookEventRunComplete, "failed"})
	if err != nil {
		t.Fatalf("normalizeWebhookEvents: %v", err)
	}
	if len(events) != 2 || events[0] != webhookEventRunComplete || events[1] != "failed" {
		t.Fatalf("unexpected normalized events: %v", events)
	}
}

func TestRuntimeActiveWebhooksForRunDeduplicatesMatchingSubscriptions(t *testing.T) {
	runtime := newTestRuntime(t, &fakeRunner{responses: []string{`{"type":"final","message":"done"}`}})
	if _, err := runtime.store.CreateWebhookConfig("ws_alpha", "https://a.example.com/hook", "", []string{webhookEventRunComplete}); err != nil {
		t.Fatalf("CreateWebhookConfig completed: %v", err)
	}
	if _, err := runtime.store.CreateWebhookConfig("ws_alpha", "https://b.example.com/hook", "", []string{"failed"}); err != nil {
		t.Fatalf("CreateWebhookConfig failed: %v", err)
	}
	combined, err := runtime.store.CreateWebhookConfig("ws_alpha", "https://c.example.com/hook", "", []string{webhookEventRunComplete, "failed"})
	if err != nil {
		t.Fatalf("CreateWebhookConfig combined: %v", err)
	}

	webhooks, err := runtime.activeWebhooksForRun(&Run{
		ID:          "run_1",
		WorkspaceID: "ws_alpha",
		Status:      StatusFailed,
	})
	if err != nil {
		t.Fatalf("activeWebhooksForRun: %v", err)
	}
	if len(webhooks) != 3 {
		t.Fatalf("expected 3 unique webhooks, got %d", len(webhooks))
	}

	count := 0
	for _, webhook := range webhooks {
		if webhook.ID == combined.ID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected combined webhook once, got %d occurrences", count)
	}
}

func TestCreateRunSupportsCustomDefinitions(t *testing.T) {
	runtime := newTestRuntime(t, &fakeRunner{
		responses: []string{`{"type":"final","message":"done"}`},
	})
	actor := &auth.KeyRecord{ID: "key-1", WorkspaceID: "ws_alpha", Role: auth.RoleOwner, PrincipalType: auth.PrincipalHuman, Status: "active"}

	custom, err := runtime.CreateCustomDefinition("ws_alpha", CreateCustomDefinitionRequest{
		Name:         "Workspace Investigator",
		SystemPrompt: "Investigate the workspace carefully.",
		Model:        "model-a",
	})
	if err != nil {
		t.Fatalf("CreateCustomDefinition: %v", err)
	}

	run, err := runtime.CreateRun(context.Background(), actor, nil, CreateRunRequest{
		AgentID: custom.ID,
		Input:   "inspect",
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if run.Model != "model-a" {
		t.Fatalf("expected custom definition model fallback, got %q", run.Model)
	}

	detail := waitForStatus(t, runtime, "ws_alpha", run.ID, StatusSucceeded)
	if detail.Run.AgentID != custom.ID {
		t.Fatalf("expected custom agent id %q, got %q", custom.ID, detail.Run.AgentID)
	}
}
