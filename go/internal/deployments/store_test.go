package deployments

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/infera/infera/go/internal/providers"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "deployments.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestRecordAndListAttempts(t *testing.T) {
	store := newTestStore(t)

	instance := &providers.Instance{
		ID:         "inst_1",
		Name:       "worker-1",
		Provider:   providers.ProviderRunPod,
		Status:     providers.InstanceStatusRunning,
		GPUType:    providers.GPUA100_80,
		GPUCount:   1,
		CreatedAt:  time.Now().UTC(),
		WorkspaceID: "ws_alpha",
	}

	if _, err := store.RecordProvisionedAttempt(
		"ws_alpha",
		"key_1",
		providers.ProvisionRequest{
			Name:      "worker-1",
			Provider:  providers.ProviderRunPod,
			WorkspaceID: "ws_alpha",
			GPUType:   providers.GPUA100_80,
			GPUCount:  1,
			Models:    []string{"org/model-a"},
		},
		"Model A",
		instance,
	); err != nil {
		t.Fatalf("RecordProvisionedAttempt: %v", err)
	}

	if _, err := store.RecordFailedAttempt(
		"ws_beta",
		"key_2",
		providers.ProvisionRequest{
			Provider: providers.ProviderRunPod,
			GPUType:  providers.GPURTX4090,
		},
		"",
		"provider auth failed",
	); err != nil {
		t.Fatalf("RecordFailedAttempt: %v", err)
	}

	alpha, err := store.ListAttempts("ws_alpha", 10)
	if err != nil {
		t.Fatalf("ListAttempts ws_alpha: %v", err)
	}
	if len(alpha) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(alpha))
	}
	if alpha[0].SelectedModelName != "Model A" {
		t.Fatalf("expected selected model name, got %#v", alpha[0])
	}

	beta, err := store.ListAttempts("ws_beta", 10)
	if err != nil {
		t.Fatalf("ListAttempts ws_beta: %v", err)
	}
	if len(beta) != 1 || beta[0].FailureReason != "provider auth failed" {
		t.Fatalf("unexpected beta attempts: %#v", beta)
	}
}

func TestUpdateVerificationAndAutoVerify(t *testing.T) {
	store := newTestStore(t)
	instance := &providers.Instance{
		ID:        "inst_1",
		Name:      "worker-1",
		Provider:  providers.ProviderRunPod,
		Status:    providers.InstanceStatusRunning,
		GPUType:   providers.GPUA100_80,
		GPUCount:  1,
		CreatedAt: time.Now().UTC(),
	}

	attempt, err := store.RecordProvisionedAttempt(
		"ws_alpha",
		"key_1",
		providers.ProvisionRequest{
			Provider: providers.ProviderRunPod,
			GPUType:  providers.GPUA100_80,
			GPUCount: 1,
			Models:   []string{"org/model-a"},
		},
		"Model A",
		instance,
	)
	if err != nil {
		t.Fatalf("RecordProvisionedAttempt: %v", err)
	}

	latency := int64(420)
	updated, err := store.UpdateVerification("ws_alpha", attempt.ID, InferenceVerification{
		Status:          "passed",
		VerifiedAt:      time.Date(2026, 3, 16, 10, 0, 0, 0, time.UTC),
		LatencyMS:       &latency,
		Model:           "org/model-a",
		ResponsePreview: "ready",
	})
	if err != nil {
		t.Fatalf("UpdateVerification: %v", err)
	}
	if updated.InferenceVerification == nil || updated.InferenceVerification.Status != "passed" {
		t.Fatalf("expected verification to be persisted, got %#v", updated.InferenceVerification)
	}

	auto, err := store.MarkAutoVerificationRequested("ws_alpha", attempt.ID, time.Date(2026, 3, 16, 10, 1, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("MarkAutoVerificationRequested: %v", err)
	}
	if auto.AutoVerificationRequestedAt == nil {
		t.Fatalf("expected auto verification timestamp")
	}
}

func TestWorkspaceScopedUpdates(t *testing.T) {
	store := newTestStore(t)
	instance := &providers.Instance{
		ID:        "inst_1",
		Name:      "worker-1",
		Provider:  providers.ProviderRunPod,
		Status:    providers.InstanceStatusRunning,
		GPUType:   providers.GPUA100_80,
		GPUCount:  1,
		CreatedAt: time.Now().UTC(),
	}

	attempt, err := store.RecordProvisionedAttempt(
		"ws_alpha",
		"key_1",
		providers.ProvisionRequest{Provider: providers.ProviderRunPod, GPUType: providers.GPUA100_80, GPUCount: 1},
		"",
		instance,
	)
	if err != nil {
		t.Fatalf("RecordProvisionedAttempt: %v", err)
	}

	_, err = store.UpdateVerification("ws_beta", attempt.ID, InferenceVerification{Status: "failed", VerifiedAt: time.Now().UTC()})
	if err == nil {
		t.Fatal("expected cross-workspace update failure")
	}
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}
