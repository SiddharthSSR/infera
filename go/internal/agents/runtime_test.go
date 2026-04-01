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
		Tools: []string{"echo", "secure", "usage"},
		BuildSystemPrompt: func(tools []ToolDescriptor) string {
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
	if err := runtime.RegisterTool(ToolDefinition{
		Name:        "usage",
		Description: "usage tool",
		Permission:  auth.PermissionViewUsage,
		Handler: func(ctx context.Context, call ToolCallContext, arguments json.RawMessage) (any, error) {
			return map[string]any{"usage": true}, nil
		},
	}); err != nil {
		t.Fatalf("RegisterTool usage: %v", err)
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

func TestRuntimeListDefinitionsFiltersToolsByPermission(t *testing.T) {
	runtime := newTestRuntime(t, &fakeRunner{})

	cases := []struct {
		name string
		key  *auth.KeyRecord
		want []string
	}{
		{
			name: "owner sees infra and usage tools",
			key:  &auth.KeyRecord{Role: auth.RoleOwner, PrincipalType: auth.PrincipalHuman, Status: "active"},
			want: []string{"echo", "secure", "usage"},
		},
		{
			name: "operator sees infra but not usage",
			key:  &auth.KeyRecord{Role: auth.RoleOperator, PrincipalType: auth.PrincipalHuman, Status: "active"},
			want: []string{"echo", "secure"},
		},
		{
			name: "billing sees usage but not infra",
			key:  &auth.KeyRecord{Role: auth.RoleBilling, PrincipalType: auth.PrincipalHuman, Status: "active"},
			want: []string{"echo", "usage"},
		},
		{
			name: "read only sees both read bundles",
			key:  &auth.KeyRecord{Role: auth.RoleReadOnly, PrincipalType: auth.PrincipalHuman, Status: "active"},
			want: []string{"echo", "secure", "usage"},
		},
		{
			name: "user sees only unrestricted tool",
			key:  &auth.KeyRecord{Role: auth.RoleUser, PrincipalType: auth.PrincipalHuman, Status: "active"},
			want: []string{"echo"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			definitions := runtime.ListDefinitions(tc.key)
			if len(definitions) != 1 {
				t.Fatalf("expected one definition, got %d", len(definitions))
			}

			got := make([]string, 0, len(definitions[0].Tools))
			for _, tool := range definitions[0].Tools {
				got = append(got, tool.Name)
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("expected tools %v, got %v", tc.want, got)
			}
		})
	}
}
