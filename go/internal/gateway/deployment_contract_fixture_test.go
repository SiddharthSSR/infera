package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/infera/infera/go/internal/auth"
	"github.com/infera/infera/go/internal/deployments"
	"github.com/infera/infera/go/internal/providers"
)

func TestHandleDeploymentVerificationMatchesSharedFixture(t *testing.T) {
	h, store := newDeploymentFixtureHandlers(t)
	attemptID := seedDeploymentFixtureAttempt(t, h, store)

	rec := httptest.NewRecorder()
	req := deploymentFixtureRequest(
		http.MethodPut,
		"/api/deployments/"+attemptID+"/verification",
		loadDeploymentHistoryFixtureBytes(t, DeploymentHistoryFixtureDeploymentAttemptVerificationRequest),
	)
	req.Header.Set("Content-Type", "application/json")

	h.handleDeploymentByID(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertDeploymentHistoryFixtureEqual(t, DeploymentHistoryFixtureDeploymentAttemptVerificationResponse, rec.Body.Bytes())
}

func TestHandleDeploymentAutoVerificationMatchesSharedFixture(t *testing.T) {
	h, store := newDeploymentFixtureHandlers(t)
	attemptID := seedDeploymentFixtureAttempt(t, h, store)
	applyDeploymentVerificationFixture(t, h, attemptID)

	rec := httptest.NewRecorder()
	req := deploymentFixtureRequest(
		http.MethodPut,
		"/api/deployments/"+attemptID+"/auto-verification",
		loadDeploymentHistoryFixtureBytes(t, DeploymentHistoryFixtureDeploymentAttemptAutoVerificationRequest),
	)
	req.Header.Set("Content-Type", "application/json")

	h.handleDeploymentByID(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertDeploymentHistoryFixtureEqual(t, DeploymentHistoryFixtureDeploymentAttemptAutoVerificationResponse, rec.Body.Bytes())
}

func TestHandleDeploymentsMatchesSharedFixture(t *testing.T) {
	h, store := newDeploymentFixtureHandlers(t)
	attemptID := seedDeploymentFixtureAttempt(t, h, store)
	applyDeploymentVerificationFixture(t, h, attemptID)
	applyDeploymentAutoVerificationFixture(t, h, attemptID)

	rec := httptest.NewRecorder()
	req := deploymentFixtureRequest(http.MethodGet, "/api/deployments", nil)

	h.handleDeployments(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertDeploymentHistoryFixtureEqual(t, DeploymentHistoryFixtureDeploymentAttemptsListResponse, rec.Body.Bytes())
}

func newDeploymentFixtureHandlers(t *testing.T) (*InstanceHandlers, *deployments.Store) {
	t.Helper()

	manager, err := providers.NewManager(providers.ManagerConfig{
		DefaultProvider: providers.ProviderMock,
	})
	if err != nil {
		t.Fatalf("create manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	manager.RegisterProvider(newDeploymentFixtureProvider())

	store := newTestDeploymentStore(t)
	h := NewInstanceHandlers(manager)
	h.SetDeploymentStore(store)
	return h, store
}

func seedDeploymentFixtureAttempt(t *testing.T, h *InstanceHandlers, store *deployments.Store) string {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"name":                "fixture-worker",
		"provider":            "mock",
		"engine":              "sglang",
		"gpu_type":            "RTX_4090",
		"gpu_count":           1,
		"models":              []string{"org/model-a"},
		"options":             map[string]string{"INFERA_SGLANG_MAX_RUNNING_REQUESTS": "32"},
		"selected_model_name": "Model A",
	})
	if err != nil {
		t.Fatalf("marshal provision body: %v", err)
	}

	rec := httptest.NewRecorder()
	req := deploymentFixtureRequest(http.MethodPost, "/api/instances/provision", body)
	req.Header.Set("Content-Type", "application/json")

	h.handleProvision(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	attempts, err := store.ListAttempts("ws_alpha", 10)
	if err != nil {
		t.Fatalf("list attempts: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(attempts))
	}
	return attempts[0].ID
}

func applyDeploymentVerificationFixture(t *testing.T, h *InstanceHandlers, attemptID string) {
	t.Helper()

	rec := httptest.NewRecorder()
	req := deploymentFixtureRequest(
		http.MethodPut,
		"/api/deployments/"+attemptID+"/verification",
		loadDeploymentHistoryFixtureBytes(t, DeploymentHistoryFixtureDeploymentAttemptVerificationRequest),
	)
	req.Header.Set("Content-Type", "application/json")

	h.handleDeploymentByID(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func applyDeploymentAutoVerificationFixture(t *testing.T, h *InstanceHandlers, attemptID string) {
	t.Helper()

	rec := httptest.NewRecorder()
	req := deploymentFixtureRequest(
		http.MethodPut,
		"/api/deployments/"+attemptID+"/auto-verification",
		loadDeploymentHistoryFixtureBytes(t, DeploymentHistoryFixtureDeploymentAttemptAutoVerificationRequest),
	)
	req.Header.Set("Content-Type", "application/json")

	h.handleDeploymentByID(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func deploymentFixtureRequest(method, path string, body []byte) *http.Request {
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}

	req := httptest.NewRequest(method, path, reader)
	return req.WithContext(auth.ContextWithKey(req.Context(), &auth.KeyRecord{
		ID:            "key_fixture",
		Role:          auth.RoleOperator,
		PrincipalType: auth.PrincipalHuman,
		Status:        "active",
		WorkspaceID:   "ws_alpha",
	}))
}

type deploymentFixtureProvider struct{}

func newDeploymentFixtureProvider() *deploymentFixtureProvider {
	return &deploymentFixtureProvider{}
}

func (p *deploymentFixtureProvider) Name() providers.ProviderType {
	return providers.ProviderMock
}

func (p *deploymentFixtureProvider) Provision(ctx context.Context, req *providers.ProvisionRequest) (*providers.Instance, error) {
	startedAt := time.Date(2026, time.April, 10, 0, 0, 5, 0, time.UTC)
	return &providers.Instance{
		ID:           "inst_fixture_1",
		ProviderID:   "mock-inst_fixture_1",
		Provider:     providers.ProviderMock,
		WorkspaceID:  req.WorkspaceID,
		Name:         req.Name,
		Status:       providers.InstanceStatusRunning,
		GPUType:      req.GPUType,
		GPUCount:     req.GPUCount,
		VCPU:         8,
		MemoryGB:     32,
		StorageGB:    100,
		PublicIP:     "127.0.0.1",
		HTTPPort:     8081,
		SSHPort:      22,
		Models:       append([]string(nil), req.Models...),
		Engine:       req.Engine.OrDefault(),
		CostPerHour:  0.4,
		SpotInstance: req.SpotInstance,
		CreatedAt:    time.Date(2026, time.April, 10, 0, 0, 0, 0, time.UTC),
		StartedAt:    &startedAt,
	}, nil
}

func (p *deploymentFixtureProvider) Terminate(ctx context.Context, instanceID string) error {
	return nil
}

func (p *deploymentFixtureProvider) Start(ctx context.Context, instanceID string) error {
	return nil
}

func (p *deploymentFixtureProvider) Stop(ctx context.Context, instanceID string) error {
	return nil
}

func (p *deploymentFixtureProvider) GetInstance(ctx context.Context, instanceID string) (*providers.Instance, error) {
	return nil, &providers.ProviderError{
		Provider: providers.ProviderMock,
		Code:     providers.ProviderErrorNotFound,
		Message:  "instance not found",
	}
}

func (p *deploymentFixtureProvider) ListInstances(ctx context.Context) ([]*providers.Instance, error) {
	return nil, nil
}

func (p *deploymentFixtureProvider) ListOfferings(ctx context.Context) ([]*providers.GPUOffering, error) {
	return nil, nil
}

func (p *deploymentFixtureProvider) GetStatus(ctx context.Context) (*providers.ProviderStatus, error) {
	return &providers.ProviderStatus{Provider: providers.ProviderMock, Connected: true}, nil
}

func (p *deploymentFixtureProvider) WaitForReady(ctx context.Context, instanceID string) error {
	return nil
}

func loadDeploymentHistoryFixtureBytes(t *testing.T, name string) []byte {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "contracts", "deployment_history", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func assertDeploymentHistoryFixtureEqual(t *testing.T, fixtureName string, got []byte) {
	t.Helper()

	want := decodeJSONMap(t, loadDeploymentHistoryFixtureBytes(t, fixtureName))
	gotValue := decodeJSONMap(t, got)
	normalizeDeploymentHistoryContract(want)
	normalizeDeploymentHistoryContract(gotValue)

	if !reflect.DeepEqual(want, gotValue) {
		t.Fatalf(
			"json mismatch for %s\nwant: %s\ngot: %s",
			fixtureName,
			strings.TrimSpace(string(loadDeploymentHistoryFixtureBytes(t, fixtureName))),
			strings.TrimSpace(string(got)),
		)
	}
}

func normalizeDeploymentHistoryContract(value map[string]any) {
	if attempt, ok := value["attempt"].(map[string]any); ok {
		normalizeDeploymentHistoryAttempt(attempt)
	}
	if attempts, ok := value["attempts"].([]any); ok {
		for _, rawAttempt := range attempts {
			attempt, ok := rawAttempt.(map[string]any)
			if !ok {
				continue
			}
			normalizeDeploymentHistoryAttempt(attempt)
		}
	}
}

func normalizeDeploymentHistoryAttempt(value map[string]any) {
	if _, ok := value["request"]; !ok {
		return
	}
	if _, ok := value["outcome"]; !ok {
		return
	}
	value["id"] = "normalized-attempt-id"
	if _, ok := value["created_at"]; ok {
		value["created_at"] = "normalized-created-at"
	}
}
