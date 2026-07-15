package gateway

import (
	"time"

	"github.com/infera/infera/go/internal/deployments"
	"github.com/infera/infera/go/internal/providers"
)

// deploymentHistoryStore isolates gateway deployment-history access behind a
// small interface so the control plane does not depend on a single store
// implementation.
type deploymentHistoryStore interface {
	ListAttempts(workspaceID string, limit int) ([]*deployments.AttemptRecord, error)
	RecordProvisionedAttempt(
		workspaceID string,
		createdByKeyID string,
		req providers.ProvisionRequest,
		selectedModelName string,
		instance *providers.Instance,
	) (*deployments.AttemptRecord, error)
	RecordFailedAttempt(
		workspaceID string,
		createdByKeyID string,
		req providers.ProvisionRequest,
		selectedModelName string,
		failureReason string,
	) (*deployments.AttemptRecord, error)
	UpdateVerification(workspaceID, attemptID string, verification deployments.InferenceVerification) (*deployments.AttemptRecord, error)
	MarkAutoVerificationRequested(workspaceID, attemptID string, requestedAt time.Time) (*deployments.AttemptRecord, error)
}
