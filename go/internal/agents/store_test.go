package agents

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "agents.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestStoreCreateAppendAndDetail(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Round(time.Second)

	run, err := store.CreateRun("ws_alpha", "key_1", "hermes", RunModeOperations, AnalysisDepthStandard, "model-a", "inspect workers", 4, now)
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if err := store.MarkRunRunning(run.WorkspaceID, run.ID, now.Add(time.Second)); err != nil {
		t.Fatalf("MarkRunRunning: %v", err)
	}
	if _, err := store.AppendStep(run.WorkspaceID, run.ID, StepTypeToolCall, "list_workers", map[string]any{
		"arguments": map[string]any{},
	}, now.Add(2*time.Second)); err != nil {
		t.Fatalf("AppendStep tool_call: %v", err)
	}
	if _, err := store.AppendStep(run.WorkspaceID, run.ID, StepTypeFinal, "", map[string]any{
		"message": "all workers look healthy",
	}, now.Add(3*time.Second)); err != nil {
		t.Fatalf("AppendStep final: %v", err)
	}
	if err := store.CompleteRun(run.WorkspaceID, run.ID, StatusSucceeded, "all workers look healthy", "", now.Add(4*time.Second)); err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}

	detail, err := store.GetRunDetail(run.WorkspaceID, run.ID)
	if err != nil {
		t.Fatalf("GetRunDetail: %v", err)
	}
	if detail.Run.Status != StatusSucceeded {
		t.Fatalf("expected succeeded status, got %s", detail.Run.Status)
	}
	if detail.Run.Mode != RunModeOperations {
		t.Fatalf("expected operations mode, got %s", detail.Run.Mode)
	}
	if detail.Run.AnalysisDepth != AnalysisDepthStandard {
		t.Fatalf("expected standard analysis depth, got %s", detail.Run.AnalysisDepth)
	}
	if detail.Run.CurrentStep != 2 {
		t.Fatalf("expected current_step=2, got %d", detail.Run.CurrentStep)
	}
	if len(detail.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(detail.Steps))
	}
	if detail.Steps[0].Type != StepTypeToolCall {
		t.Fatalf("expected first step tool_call, got %s", detail.Steps[0].Type)
	}
	if detail.Steps[1].Type != StepTypeFinal {
		t.Fatalf("expected second step final, got %s", detail.Steps[1].Type)
	}
}

func TestStoreMarkInterruptedRuns(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Round(time.Second)

	queuedRun, err := store.CreateRun("ws_alpha", "key_1", "hermes", RunModeOperations, AnalysisDepthStandard, "model-a", "queued", 4, now)
	if err != nil {
		t.Fatalf("CreateRun queued: %v", err)
	}
	runningRun, err := store.CreateRun("ws_alpha", "key_1", "hermes", RunModeResearch, AnalysisDepthDeep, "model-a", "running", 4, now)
	if err != nil {
		t.Fatalf("CreateRun running: %v", err)
	}
	if err := store.MarkRunRunning(runningRun.WorkspaceID, runningRun.ID, now.Add(time.Second)); err != nil {
		t.Fatalf("MarkRunRunning: %v", err)
	}

	affected, err := store.MarkInterruptedRuns(now.Add(2*time.Second), "gateway restarted")
	if err != nil {
		t.Fatalf("MarkInterruptedRuns: %v", err)
	}
	if affected != 2 {
		t.Fatalf("expected 2 interrupted runs, got %d", affected)
	}

	for _, runID := range []string{queuedRun.ID, runningRun.ID} {
		run, err := store.GetRun("ws_alpha", runID)
		if err != nil {
			t.Fatalf("GetRun %s: %v", runID, err)
		}
		if run.Status != StatusFailed {
			t.Fatalf("expected failed status for %s, got %s", runID, run.Status)
		}
		if run.FailureReason != "gateway restarted" {
			t.Fatalf("expected failure reason to be recorded for %s, got %q", runID, run.FailureReason)
		}
	}
}

func TestListStepsAfter(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC().Round(time.Second)

	run, err := store.CreateRun("ws_alpha", "key_1", "hermes", RunModeOperations, AnalysisDepthStandard, "model-a", "inspect", 4, now)
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if err := store.MarkRunRunning(run.WorkspaceID, run.ID, now.Add(time.Second)); err != nil {
		t.Fatalf("MarkRunRunning: %v", err)
	}
	for i := 0; i < 4; i++ {
		if _, err := store.AppendStep(run.WorkspaceID, run.ID, StepTypeToolCall, "echo", map[string]any{
			"index": i,
		}, now.Add(time.Duration(i+2)*time.Second)); err != nil {
			t.Fatalf("AppendStep %d: %v", i, err)
		}
	}

	// Steps are 1-indexed: indices 1, 2, 3, 4
	// After index 0 → all 4 steps
	steps, err := store.ListStepsAfter(run.WorkspaceID, run.ID, 0)
	if err != nil {
		t.Fatalf("ListStepsAfter(0): %v", err)
	}
	if len(steps) != 4 {
		t.Fatalf("expected 4 steps after index 0, got %d", len(steps))
	}
	for i, step := range steps {
		if step.Index != i+1 {
			t.Fatalf("step[%d].Index = %d, want %d", i, step.Index, i+1)
		}
	}

	// After index 2 → steps 3 and 4
	steps, err = store.ListStepsAfter(run.WorkspaceID, run.ID, 2)
	if err != nil {
		t.Fatalf("ListStepsAfter(2): %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps after index 2, got %d", len(steps))
	}

	// After index 99 → empty
	steps, err = store.ListStepsAfter(run.WorkspaceID, run.ID, 99)
	if err != nil {
		t.Fatalf("ListStepsAfter(99): %v", err)
	}
	if len(steps) != 0 {
		t.Fatalf("expected 0 steps after index 99, got %d", len(steps))
	}

	// Wrong workspace → empty
	steps, err = store.ListStepsAfter("ws_other", run.ID, 0)
	if err != nil {
		t.Fatalf("ListStepsAfter wrong ws: %v", err)
	}
	if len(steps) != 0 {
		t.Fatalf("expected 0 steps for wrong workspace, got %d", len(steps))
	}
}

func TestCreateCustomDefinition(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC()

	def, err := store.CreateCustomDefinition("ws_alpha", "My Agent", "A test agent", "You are helpful.", []string{"echo", "search"}, 10, 30, "model-a", now)
	if err != nil {
		t.Fatalf("CreateCustomDefinition: %v", err)
	}
	if def.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if def.WorkspaceID != "ws_alpha" {
		t.Fatalf("unexpected workspace: %q", def.WorkspaceID)
	}
	if def.Name != "My Agent" {
		t.Fatalf("unexpected name: %q", def.Name)
	}
	if def.SystemPrompt != "You are helpful." {
		t.Fatalf("unexpected system_prompt: %q", def.SystemPrompt)
	}
	if len(def.Tools) != 2 || def.Tools[0] != "echo" || def.Tools[1] != "search" {
		t.Fatalf("unexpected tools: %v", def.Tools)
	}
	if def.MaxSteps != 10 {
		t.Fatalf("expected max_steps=10, got %d", def.MaxSteps)
	}
	if def.TimeoutSeconds != 30 {
		t.Fatalf("expected timeout_seconds=30, got %d", def.TimeoutSeconds)
	}
}

func TestCreateCustomDefinitionValidation(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC()

	// Missing name
	_, err := store.CreateCustomDefinition("ws_alpha", "", "", "prompt", nil, 8, 30, "", now)
	if err == nil {
		t.Fatal("expected error for empty name")
	}

	// Missing system prompt
	_, err = store.CreateCustomDefinition("ws_alpha", "Agent", "", "", nil, 8, 30, "", now)
	if err == nil {
		t.Fatal("expected error for empty system_prompt")
	}
}

func TestListCustomDefinitions(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC()

	// Empty initially
	defs, err := store.ListCustomDefinitions("ws_alpha")
	if err != nil {
		t.Fatalf("ListCustomDefinitions: %v", err)
	}
	if len(defs) != 0 {
		t.Fatalf("expected 0 definitions, got %d", len(defs))
	}

	// Create two
	_, _ = store.CreateCustomDefinition("ws_alpha", "Agent A", "", "Prompt A", nil, 8, 30, "", now)
	_, _ = store.CreateCustomDefinition("ws_alpha", "Agent B", "", "Prompt B", nil, 8, 30, "", now.Add(time.Second))

	defs, err = store.ListCustomDefinitions("ws_alpha")
	if err != nil {
		t.Fatalf("ListCustomDefinitions: %v", err)
	}
	if len(defs) != 2 {
		t.Fatalf("expected 2 definitions, got %d", len(defs))
	}

	// Different workspace sees nothing
	defs, err = store.ListCustomDefinitions("ws_other")
	if err != nil {
		t.Fatalf("ListCustomDefinitions other ws: %v", err)
	}
	if len(defs) != 0 {
		t.Fatalf("expected 0 definitions for other workspace, got %d", len(defs))
	}
}

func TestDeleteCustomDefinition(t *testing.T) {
	store := newTestStore(t)
	now := time.Now().UTC()

	def, err := store.CreateCustomDefinition("ws_alpha", "Agent", "", "Prompt", nil, 8, 30, "", now)
	if err != nil {
		t.Fatalf("CreateCustomDefinition: %v", err)
	}

	if err := store.DeleteCustomDefinition("ws_alpha", def.ID); err != nil {
		t.Fatalf("DeleteCustomDefinition: %v", err)
	}

	defs, _ := store.ListCustomDefinitions("ws_alpha")
	if len(defs) != 0 {
		t.Fatalf("expected 0 definitions after delete, got %d", len(defs))
	}

	// Deleting again should error
	err = store.DeleteCustomDefinition("ws_alpha", def.ID)
	if err == nil {
		t.Fatal("expected error deleting nonexistent definition")
	}
}

func TestCreateWebhookConfig(t *testing.T) {
	store := newTestStore(t)

	wh, err := store.CreateWebhookConfig("ws_alpha", "https://example.com/hook", "secret123", []string{"succeeded", "failed"})
	if err != nil {
		t.Fatalf("CreateWebhookConfig: %v", err)
	}
	if wh.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if wh.URL != "https://example.com/hook" {
		t.Fatalf("unexpected URL: %q", wh.URL)
	}
	if wh.Secret != "secret123" {
		t.Fatalf("unexpected secret: %q", wh.Secret)
	}
	if !wh.Active {
		t.Fatal("expected webhook to be active")
	}
	if len(wh.Events) != 2 {
		t.Fatalf("unexpected events: %v", wh.Events)
	}
}

func TestCreateWebhookConfigDefaultEvents(t *testing.T) {
	store := newTestStore(t)

	wh, err := store.CreateWebhookConfig("ws_alpha", "https://example.com/hook", "", nil)
	if err != nil {
		t.Fatalf("CreateWebhookConfig: %v", err)
	}
	if len(wh.Events) != 2 || wh.Events[0] != "succeeded" || wh.Events[1] != "failed" {
		t.Fatalf("expected default events [succeeded, failed], got %v", wh.Events)
	}
}

func TestCreateWebhookConfigEmptyURLReturnsError(t *testing.T) {
	store := newTestStore(t)

	_, err := store.CreateWebhookConfig("ws_alpha", "", "secret", nil)
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
}

func TestListWebhookConfigs(t *testing.T) {
	store := newTestStore(t)

	webhooks, _ := store.ListWebhookConfigs("ws_alpha")
	if len(webhooks) != 0 {
		t.Fatalf("expected 0 webhooks, got %d", len(webhooks))
	}

	_, _ = store.CreateWebhookConfig("ws_alpha", "https://a.example.com/hook", "", nil)
	_, _ = store.CreateWebhookConfig("ws_alpha", "https://b.example.com/hook", "", nil)

	webhooks, err := store.ListWebhookConfigs("ws_alpha")
	if err != nil {
		t.Fatalf("ListWebhookConfigs: %v", err)
	}
	if len(webhooks) != 2 {
		t.Fatalf("expected 2 webhooks, got %d", len(webhooks))
	}

	// Other workspace sees nothing
	webhooks, _ = store.ListWebhookConfigs("ws_other")
	if len(webhooks) != 0 {
		t.Fatalf("expected 0 webhooks for other workspace, got %d", len(webhooks))
	}
}

func TestDeleteWebhookConfig(t *testing.T) {
	store := newTestStore(t)

	wh, err := store.CreateWebhookConfig("ws_alpha", "https://example.com/hook", "", nil)
	if err != nil {
		t.Fatalf("CreateWebhookConfig: %v", err)
	}

	if err := store.DeleteWebhookConfig("ws_alpha", wh.ID); err != nil {
		t.Fatalf("DeleteWebhookConfig: %v", err)
	}

	webhooks, _ := store.ListWebhookConfigs("ws_alpha")
	if len(webhooks) != 0 {
		t.Fatalf("expected 0 webhooks after delete, got %d", len(webhooks))
	}

	// Deleting again should error
	err = store.DeleteWebhookConfig("ws_alpha", wh.ID)
	if err == nil {
		t.Fatal("expected error deleting nonexistent webhook")
	}
}

func TestGetActiveWebhooksForEvent(t *testing.T) {
	store := newTestStore(t)

	_, _ = store.CreateWebhookConfig("ws_alpha", "https://a.example.com/hook", "", []string{"succeeded"})
	_, _ = store.CreateWebhookConfig("ws_alpha", "https://b.example.com/hook", "", []string{"succeeded", "failed"})
	_, _ = store.CreateWebhookConfig("ws_alpha", "https://c.example.com/hook", "", []string{"failed"})

	// "succeeded" → A and B
	webhooks, err := store.GetActiveWebhooksForEvent("ws_alpha", "succeeded")
	if err != nil {
		t.Fatalf("GetActiveWebhooksForEvent succeeded: %v", err)
	}
	if len(webhooks) != 2 {
		t.Fatalf("expected 2 webhooks for 'succeeded', got %d", len(webhooks))
	}

	// "failed" → B and C
	webhooks, err = store.GetActiveWebhooksForEvent("ws_alpha", "failed")
	if err != nil {
		t.Fatalf("GetActiveWebhooksForEvent failed: %v", err)
	}
	if len(webhooks) != 2 {
		t.Fatalf("expected 2 webhooks for 'failed', got %d", len(webhooks))
	}

	// "canceled" → none
	webhooks, _ = store.GetActiveWebhooksForEvent("ws_alpha", "canceled")
	if len(webhooks) != 0 {
		t.Fatalf("expected 0 webhooks for 'canceled', got %d", len(webhooks))
	}

	// Wrong workspace → empty
	webhooks, _ = store.GetActiveWebhooksForEvent("ws_other", "succeeded")
	if len(webhooks) != 0 {
		t.Fatalf("expected 0 webhooks for wrong workspace, got %d", len(webhooks))
	}
}
