package gateway

import (
	"testing"
	"time"

	"github.com/infera/infera/go/internal/deployments"
	"github.com/infera/infera/go/internal/providers"
	"github.com/infera/infera/go/internal/providers/mock"
)

type stubDeploymentHistoryStore struct {
	listAttemptsWorkspace string
	listAttemptsLimit     int
	listAttempts          []*deployments.AttemptRecord
}

func (s *stubDeploymentHistoryStore) ListAttempts(workspaceID string, limit int) ([]*deployments.AttemptRecord, error) {
	s.listAttemptsWorkspace = workspaceID
	s.listAttemptsLimit = limit
	return s.listAttempts, nil
}

func (s *stubDeploymentHistoryStore) RecordProvisionedAttempt(
	workspaceID string,
	createdByKeyID string,
	req providers.ProvisionRequest,
	selectedModelName string,
	instance *providers.Instance,
) (*deployments.AttemptRecord, error) {
	return nil, nil
}

func (s *stubDeploymentHistoryStore) RecordFailedAttempt(
	workspaceID string,
	createdByKeyID string,
	req providers.ProvisionRequest,
	selectedModelName string,
	failureReason string,
) (*deployments.AttemptRecord, error) {
	return nil, nil
}

func (s *stubDeploymentHistoryStore) UpdateVerification(workspaceID, attemptID string, verification deployments.InferenceVerification) (*deployments.AttemptRecord, error) {
	return nil, nil
}

func (s *stubDeploymentHistoryStore) MarkAutoVerificationRequested(workspaceID, attemptID string, requestedAt time.Time) (*deployments.AttemptRecord, error) {
	return nil, nil
}

func TestGatewaySetDeploymentStorePropagatesToInstanceHandlers(t *testing.T) {
	manager, err := providers.NewManager(providers.ManagerConfig{
		DefaultProvider: providers.ProviderMock,
	})
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	manager.RegisterProvider(mock.New())

	g := New(DefaultConfig(), nil, manager)
	store := &stubDeploymentHistoryStore{
		listAttempts: []*deployments.AttemptRecord{{
			ID:          "attempt-1",
			WorkspaceID: "ws_alpha",
		}},
	}

	g.SetDeploymentStore(store)

	if g.deploymentStore != store {
		t.Fatalf("expected gateway to retain injected deployment store")
	}
	if g.instanceHandlers == nil {
		t.Fatalf("expected instance handlers to be configured")
	}

	attempts, err := g.instanceHandlers.listDeploymentEntries("ws_alpha", 7)
	if err != nil {
		t.Fatalf("listDeploymentEntries: %v", err)
	}
	if store.listAttemptsWorkspace != "ws_alpha" {
		t.Fatalf("expected workspace ws_alpha, got %q", store.listAttemptsWorkspace)
	}
	if store.listAttemptsLimit != 7 {
		t.Fatalf("expected limit 7, got %d", store.listAttemptsLimit)
	}
	if len(attempts) != 1 || attempts[0].ID != "attempt-1" {
		t.Fatalf("expected injected attempts to be returned, got %#v", attempts)
	}
}
