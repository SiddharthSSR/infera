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
