package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/infera/infera/go/internal/auth"
	"github.com/infera/infera/go/internal/deployments"
	"github.com/infera/infera/go/internal/providers"
	"github.com/infera/infera/go/internal/providers/mock"
)

func setupTestHandlers(t *testing.T) *InstanceHandlers {
	t.Helper()
	mgr, err := providers.NewManager(providers.ManagerConfig{
		DefaultProvider: providers.ProviderMock,
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	mgr.RegisterProvider(mock.New())
	return NewInstanceHandlers(mgr)
}

func newTestDeploymentStore(t *testing.T) *deployments.Store {
	t.Helper()
	store, err := deployments.NewStore(filepath.Join(t.TempDir(), "deployments.db"))
	if err != nil {
		t.Fatalf("deployments.NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

type failingProvider struct {
	provisionErr      error
	provisionInstance *providers.Instance
	lastReq           *providers.ProvisionRequest
	startErr          error
	stopErr           error
	terminateErr      error
	status            *providers.ProviderStatus
	instances         map[string]*providers.Instance
}

func (p *failingProvider) Name() providers.ProviderType { return providers.ProviderMock }
func (p *failingProvider) Provision(ctx context.Context, req *providers.ProvisionRequest) (*providers.Instance, error) {
	if p.provisionErr != nil {
		return nil, p.provisionErr
	}
	p.lastReq = req
	if p.provisionInstance != nil {
		inst := *p.provisionInstance
		inst.Models = append([]string(nil), p.provisionInstance.Models...)
		inst.Metadata = make(map[string]string, len(p.provisionInstance.Metadata))
		for key, value := range p.provisionInstance.Metadata {
			inst.Metadata[key] = value
		}
		if p.instances == nil {
			p.instances = map[string]*providers.Instance{}
		}
		p.instances[inst.ID] = &inst
		return &inst, nil
	}
	if p.instances == nil {
		p.instances = map[string]*providers.Instance{}
	}
	inst := &providers.Instance{
		ID:         "inst-1",
		ProviderID: "mock-inst-1",
		Provider:   providers.ProviderMock,
		Name:       req.Name,
		Status:     providers.InstanceStatusStopped,
		CreatedAt:  time.Now(),
	}
	p.instances[inst.ID] = inst
	return inst, nil
}
func (p *failingProvider) Terminate(ctx context.Context, instanceID string) error {
	return p.terminateErr
}
func (p *failingProvider) Start(ctx context.Context, instanceID string) error { return p.startErr }
func (p *failingProvider) Stop(ctx context.Context, instanceID string) error  { return p.stopErr }
func (p *failingProvider) GetInstance(ctx context.Context, instanceID string) (*providers.Instance, error) {
	if p.instances != nil {
		if inst, ok := p.instances[instanceID]; ok {
			return inst, nil
		}
	}
	return nil, &providers.ProviderError{Provider: providers.ProviderMock, Code: providers.ProviderErrorNotFound, Message: "instance not found"}
}
func (p *failingProvider) ListInstances(ctx context.Context) ([]*providers.Instance, error) {
	if p.instances == nil {
		return nil, nil
	}
	out := make([]*providers.Instance, 0, len(p.instances))
	for _, inst := range p.instances {
		out = append(out, inst)
	}
	return out, nil
}
func (p *failingProvider) ListOfferings(ctx context.Context) ([]*providers.GPUOffering, error) {
	return nil, nil
}
func (p *failingProvider) GetStatus(ctx context.Context) (*providers.ProviderStatus, error) {
	if p.status != nil {
		return p.status, nil
	}
	return &providers.ProviderStatus{Provider: providers.ProviderMock, Connected: true}, nil
}
func (p *failingProvider) WaitForReady(ctx context.Context, instanceID string) error { return nil }

func authedRequest(req *http.Request, role string) *http.Request {
	return req.WithContext(auth.ContextWithKey(req.Context(), &auth.KeyRecord{
		Role:          role,
		PrincipalType: auth.PrincipalHuman,
		Status:        "active",
	}))
}

func authedWorkspaceRequest(req *http.Request, role, workspaceID string) *http.Request {
	return req.WithContext(auth.ContextWithKey(req.Context(), &auth.KeyRecord{
		Role:          role,
		PrincipalType: auth.PrincipalHuman,
		Status:        "active",
		WorkspaceID:   workspaceID,
	}))
}

func TestHandleInstances(t *testing.T) {
	h := setupTestHandlers(t)

	t.Run("GET empty list", func(t *testing.T) {
		req := authedRequest(httptest.NewRequest(http.MethodGet, "/api/instances", nil), auth.RoleOperator)
		w := httptest.NewRecorder()

		h.handleInstances(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}

		instances := resp["instances"].([]interface{})
		if len(instances) != 0 {
			t.Errorf("expected 0 instances, got %d", len(instances))
		}
	})

	t.Run("Method not allowed", func(t *testing.T) {
		req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/instances", nil), auth.RoleOperator)
		w := httptest.NewRecorder()

		h.handleInstances(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", w.Code)
		}
	})
}

func TestHandleProvision(t *testing.T) {
	h := setupTestHandlers(t)

	t.Run("Successful provision", func(t *testing.T) {
		provider := &failingProvider{}
		mgr, err := providers.NewManager(providers.ManagerConfig{
			DefaultProvider: providers.ProviderMock,
		})
		if err != nil {
			t.Fatalf("failed to create manager: %v", err)
		}
		mgr.RegisterProvider(provider)
		h := NewInstanceHandlers(mgr)

		body := map[string]interface{}{
			"name":      "test-worker",
			"provider":  "mock",
			"engine":    "sglang",
			"gpu_type":  "RTX_4090",
			"gpu_count": 1,
			"options": map[string]string{
				"INFERA_SGLANG_MAX_RUNNING_REQUESTS": "32",
			},
		}
		bodyBytes, _ := json.Marshal(body)

		req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/instances/provision", bytes.NewReader(bodyBytes)), auth.RoleOperator)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.handleProvision(w, req)

		if w.Code != http.StatusCreated {
			t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}

		if resp["success"] != true {
			t.Error("expected success to be true")
		}
		if resp["instance"] == nil {
			t.Error("expected instance in response")
		}
		instance := resp["instance"].(map[string]interface{})
		if instance["engine"] != string(providers.EngineSGLang) {
			t.Fatalf("expected engine sglang, got %v", instance["engine"])
		}
		if provider.lastReq == nil {
			t.Fatal("expected provider provision request to be recorded")
		}
		if provider.lastReq.Options["INFERA_SGLANG_MAX_RUNNING_REQUESTS"] != "32" {
			t.Fatalf("expected runtime option to be preserved, got %#v", provider.lastReq.Options)
		}
	})

	t.Run("Invalid engine", func(t *testing.T) {
		body := map[string]interface{}{
			"name":     "test-worker",
			"provider": "mock",
			"engine":   "unsupported",
			"gpu_type": "RTX_4090",
		}
		bodyBytes, _ := json.Marshal(body)

		req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/instances/provision", bytes.NewReader(bodyBytes)), auth.RoleOperator)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.handleProvision(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})

	t.Run("Rejects unsupported runtime option", func(t *testing.T) {
		body := map[string]interface{}{
			"name":     "test-worker",
			"provider": "mock",
			"engine":   "vllm",
			"gpu_type": "RTX_4090",
			"options": map[string]string{
				"UNEXPECTED_ENV": "1",
			},
		}
		bodyBytes, _ := json.Marshal(body)

		req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/instances/provision", bytes.NewReader(bodyBytes)), auth.RoleOperator)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.handleProvision(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})

	t.Run("Rejects invalid runtime option value", func(t *testing.T) {
		body := map[string]interface{}{
			"name":     "test-worker",
			"provider": "mock",
			"engine":   "vllm",
			"gpu_type": "RTX_4090",
			"options": map[string]string{
				"INFERA_VLLM_GPU_MEMORY_UTILIZATION": "1.5",
			},
		}
		bodyBytes, _ := json.Marshal(body)

		req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/instances/provision", bytes.NewReader(bodyBytes)), auth.RoleOperator)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.handleProvision(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})

	t.Run("Missing gpu_type", func(t *testing.T) {
		body := map[string]interface{}{
			"name":     "test-worker",
			"provider": "mock",
		}
		bodyBytes, _ := json.Marshal(body)

		req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/instances/provision", bytes.NewReader(bodyBytes)), auth.RoleOperator)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.handleProvision(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("Invalid JSON", func(t *testing.T) {
		req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/instances/provision", bytes.NewReader([]byte("invalid"))), auth.RoleOperator)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.handleProvision(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("Method not allowed", func(t *testing.T) {
		req := authedRequest(httptest.NewRequest(http.MethodGet, "/api/instances/provision", nil), auth.RoleOperator)
		w := httptest.NewRecorder()

		h.handleProvision(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", w.Code)
		}
	})
}

func TestHandleInstanceByID(t *testing.T) {
	h := setupTestHandlers(t)

	// First provision an instance
	body := map[string]interface{}{
		"name":     "test-instance",
		"provider": "mock",
		"gpu_type": "RTX_4090",
	}
	bodyBytes, _ := json.Marshal(body)

	provReq := authedRequest(httptest.NewRequest(http.MethodPost, "/api/instances/provision", bytes.NewReader(bodyBytes)), auth.RoleOperator)
	provReq.Header.Set("Content-Type", "application/json")
	provW := httptest.NewRecorder()
	h.handleProvision(provW, provReq)

	var provResp map[string]interface{}
	if err := json.Unmarshal(provW.Body.Bytes(), &provResp); err != nil {
		t.Fatalf("json.Unmarshal provision response: %v", err)
	}
	instance := provResp["instance"].(map[string]interface{})
	instanceID := instance["id"].(string)

	t.Run("GET instance", func(t *testing.T) {
		req := authedRequest(httptest.NewRequest(http.MethodGet, "/api/instances/"+instanceID, nil), auth.RoleOperator)
		w := httptest.NewRecorder()

		h.handleInstanceByID(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}

		if resp["id"] != instanceID {
			t.Errorf("expected %s, got %s", instanceID, resp["id"])
		}
	})

	t.Run("GET non-existent", func(t *testing.T) {
		req := authedRequest(httptest.NewRequest(http.MethodGet, "/api/instances/non-existent", nil), auth.RoleOperator)
		w := httptest.NewRecorder()

		h.handleInstanceByID(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}
	})

	t.Run("DELETE instance", func(t *testing.T) {
		req := authedRequest(httptest.NewRequest(http.MethodDelete, "/api/instances/"+instanceID, nil), auth.RoleOperator)
		w := httptest.NewRecorder()

		h.handleInstanceByID(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}

		if resp["success"] != true {
			t.Error("expected success to be true")
		}
	})
}

func TestProvisionControlStateFailureReturnsGenericUnavailable(t *testing.T) {
	raw := "postgres decrypt failed password=do-not-leak"
	provider := &failingProvider{provisionErr: fmt.Errorf("%w: %s", providers.ErrControlStateUnavailable, raw)}
	mgr, err := providers.NewManager(providers.ManagerConfig{DefaultProvider: providers.ProviderMock})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	mgr.RegisterProvider(provider)
	h := NewInstanceHandlers(mgr)
	req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/instances/provision", strings.NewReader(`{"provider":"mock","gpu_type":"RTX_4090"}`)), auth.RoleOperator)
	rec := httptest.NewRecorder()
	h.handleProvision(rec, req)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "control_state_unavailable") || strings.Contains(rec.Body.String(), raw) {
		t.Fatalf("unexpected provision control-state response: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if reason := deploymentFailureReason(provider.provisionErr); reason != "Infrastructure state is temporarily unavailable" {
		t.Fatalf("deployment failure leaked details: %q", reason)
	}
}

func TestStopControlStateFailureReturnsGenericUnavailable(t *testing.T) {
	provider := &failingProvider{provisionInstance: &providers.Instance{
		ID: "inst-1", ProviderID: "mock-inst-1", Provider: providers.ProviderMock,
		Status: providers.InstanceStatusRunning, CreatedAt: time.Now(),
	}}
	mgr, err := providers.NewManager(providers.ManagerConfig{DefaultProvider: providers.ProviderMock})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	mgr.RegisterProvider(provider)
	instance, err := mgr.Provision(context.Background(), &providers.ProvisionRequest{
		Name: "lifecycle-unavailable", Provider: providers.ProviderMock,
		GPUType: providers.GPURTX4090, GPUCount: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := "sql connection refused password=do-not-leak"
	provider.stopErr = fmt.Errorf("%w: %s", providers.ErrWorkerCredentialIntegrity, raw)
	h := NewInstanceHandlers(mgr)
	req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/instances/"+instance.ID+"/stop", nil), auth.RoleOperator)
	rec := httptest.NewRecorder()
	h.handleInstanceByID(rec, req)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "control_state_unavailable") || strings.Contains(rec.Body.String(), raw) {
		t.Fatalf("unexpected stop control-state response: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleStartStop(t *testing.T) {
	h := setupTestHandlers(t)

	// Provision an instance
	body := map[string]interface{}{
		"name":     "start-stop-test",
		"provider": "mock",
		"gpu_type": "RTX_4090",
	}
	bodyBytes, _ := json.Marshal(body)

	provReq := authedRequest(httptest.NewRequest(http.MethodPost, "/api/instances/provision", bytes.NewReader(bodyBytes)), auth.RoleOperator)
	provReq.Header.Set("Content-Type", "application/json")
	provW := httptest.NewRecorder()
	h.handleProvision(provW, provReq)

	var provResp map[string]interface{}
	if err := json.Unmarshal(provW.Body.Bytes(), &provResp); err != nil {
		t.Fatalf("json.Unmarshal provision response: %v", err)
	}
	instance := provResp["instance"].(map[string]interface{})
	instanceID := instance["id"].(string)

	t.Run("Stop instance", func(t *testing.T) {
		req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/instances/"+instanceID+"/stop", nil), auth.RoleOperator)
		w := httptest.NewRecorder()

		h.handleInstanceByID(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("Start instance", func(t *testing.T) {
		req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/instances/"+instanceID+"/start", nil), auth.RoleOperator)
		w := httptest.NewRecorder()

		h.handleInstanceByID(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("Unknown action", func(t *testing.T) {
		req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/instances/"+instanceID+"/unknown", nil), auth.RoleOperator)
		w := httptest.NewRecorder()

		h.handleInstanceByID(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})
}

func TestHandleOfferings(t *testing.T) {
	h := setupTestHandlers(t)

	t.Run("GET offerings", func(t *testing.T) {
		req := authedRequest(httptest.NewRequest(http.MethodGet, "/api/offerings", nil), auth.RoleOperator)
		w := httptest.NewRecorder()

		h.handleOfferings(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}

		offerings := resp["offerings"].([]interface{})
		if len(offerings) == 0 {
			t.Error("expected at least one offering")
		}

		first := offerings[0].(map[string]interface{})
		if first["display_name"] == "" {
			t.Error("expected display_name to be included in offering response")
		}
		if first["provider_gpu_type_id"] == "" {
			t.Error("expected provider_gpu_type_id to be included in offering response")
		}
	})

	t.Run("Method not allowed", func(t *testing.T) {
		req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/offerings", nil), auth.RoleOperator)
		w := httptest.NewRecorder()

		h.handleOfferings(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", w.Code)
		}
	})
}

func TestHandleProviders(t *testing.T) {
	h := setupTestHandlers(t)

	t.Run("GET providers", func(t *testing.T) {
		req := authedRequest(httptest.NewRequest(http.MethodGet, "/api/providers", nil), auth.RoleOperator)
		w := httptest.NewRecorder()

		h.handleProviders(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}

		providers := resp["providers"].([]interface{})
		if len(providers) == 0 {
			t.Error("expected at least one provider")
		}

		// Check mock provider is connected
		mockProvider := providers[0].(map[string]interface{})
		if mockProvider["connected"] != true {
			t.Error("mock provider should be connected")
		}
		capabilities, ok := mockProvider["capabilities"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected capabilities object, got %#v", mockProvider["capabilities"])
		}
		if capabilities["supports_start_stop"] != true {
			t.Fatalf("expected supports_start_stop=true, got %#v", capabilities["supports_start_stop"])
		}
	})

	t.Run("Method not allowed", func(t *testing.T) {
		req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/providers", nil), auth.RoleOperator)
		w := httptest.NewRecorder()

		h.handleProviders(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", w.Code)
		}
	})
}

func TestWorkspaceScopedInstanceIsolation(t *testing.T) {
	h := setupTestHandlers(t)

	body := map[string]interface{}{
		"name":     "team-worker",
		"provider": "mock",
		"gpu_type": "RTX_4090",
	}
	bodyBytes, _ := json.Marshal(body)

	provReq := authedWorkspaceRequest(httptest.NewRequest(http.MethodPost, "/api/instances/provision", bytes.NewReader(bodyBytes)), auth.RoleOperator, "ws_alpha")
	provReq.Header.Set("Content-Type", "application/json")
	provW := httptest.NewRecorder()
	h.handleProvision(provW, provReq)
	if provW.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", provW.Code, provW.Body.String())
	}

	var provResp map[string]interface{}
	if err := json.Unmarshal(provW.Body.Bytes(), &provResp); err != nil {
		t.Fatalf("json.Unmarshal provision response: %v", err)
	}
	instance := provResp["instance"].(map[string]interface{})
	instanceID := instance["id"].(string)

	listReq := authedWorkspaceRequest(httptest.NewRequest(http.MethodGet, "/api/instances", nil), auth.RoleOperator, "ws_beta")
	listW := httptest.NewRecorder()
	h.handleInstances(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", listW.Code, listW.Body.String())
	}
	var listResp map[string]interface{}
	if err := json.Unmarshal(listW.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("json.Unmarshal list response: %v", err)
	}
	if got := len(listResp["instances"].([]interface{})); got != 0 {
		t.Fatalf("expected 0 instances for unrelated workspace, got %d", got)
	}

	getReq := authedWorkspaceRequest(httptest.NewRequest(http.MethodGet, "/api/instances/"+instanceID, nil), auth.RoleOperator, "ws_beta")
	getW := httptest.NewRecorder()
	h.handleInstanceByID(getW, getReq)
	if getW.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-workspace read, got %d: %s", getW.Code, getW.Body.String())
	}

	deleteReq := authedWorkspaceRequest(httptest.NewRequest(http.MethodDelete, "/api/instances/"+instanceID, nil), auth.RoleOperator, "ws_beta")
	deleteW := httptest.NewRecorder()
	h.handleInstanceByID(deleteW, deleteReq)
	if deleteW.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-workspace delete, got %d: %s", deleteW.Code, deleteW.Body.String())
	}
}

func TestHandleProvisionMapsProviderErrors(t *testing.T) {
	mgr, err := providers.NewManager(providers.ManagerConfig{DefaultProvider: providers.ProviderMock})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	mgr.RegisterProvider(&failingProvider{
		provisionErr: &providers.ProviderError{
			Provider:   providers.ProviderMock,
			Code:       providers.ProviderErrorRateLimited,
			Message:    "provider rate limited",
			StatusCode: 429,
			RetryAfter: 30,
		},
	})
	h := NewInstanceHandlers(mgr)

	body := map[string]interface{}{
		"name":     "test-worker",
		"provider": "mock",
		"gpu_type": "RTX_4090",
	}
	bodyBytes, _ := json.Marshal(body)

	req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/instances/provision", bytes.NewReader(bodyBytes)), auth.RoleOperator)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.handleProvision(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"]["type"] != "provider_rate_limited" {
		t.Fatalf("expected provider_rate_limited, got %#v", resp)
	}
	if resp["error"]["provider_error_code"] != providers.ProviderErrorRateLimited {
		t.Fatalf("expected provider error code rate_limited, got %#v", resp)
	}
	if resp["error"]["retryable"] != true {
		t.Fatalf("expected retryable=true, got %#v", resp)
	}
}

func TestHandleProvisionExposesRetryableCapacityContract(t *testing.T) {
	mgr, err := providers.NewManager(providers.ManagerConfig{DefaultProvider: providers.ProviderMock})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	mgr.RegisterProvider(&failingProvider{
		provisionErr: &providers.ProviderError{
			Provider: providers.ProviderRunPod,
			Code:     providers.ProviderErrorCapacityUnavailable,
			Message:  "provider capacity unavailable",
		},
	})
	h := NewInstanceHandlers(mgr)

	bodyBytes, _ := json.Marshal(map[string]interface{}{
		"name": "recovery-worker", "provider": "mock", "gpu_type": "RTX_4090",
	})
	req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/instances/provision", bytes.NewReader(bodyBytes)), auth.RoleOperator)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.handleProvision(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"]["provider_error_code"] != providers.ProviderErrorCapacityUnavailable || resp["error"]["retryable"] != true {
		t.Fatalf("expected retryable capacity contract, got %#v", resp)
	}
}

func TestHandleDeployments(t *testing.T) {
	h := setupTestHandlers(t)
	store := newTestDeploymentStore(t)
	h.SetDeploymentStore(store)

	body := map[string]interface{}{
		"name":                "test-worker",
		"provider":            "mock",
		"gpu_type":            "RTX_4090",
		"gpu_count":           1,
		"selected_model_name": "Model A",
	}
	bodyBytes, _ := json.Marshal(body)

	provReq := authedWorkspaceRequest(httptest.NewRequest(http.MethodPost, "/api/instances/provision", bytes.NewReader(bodyBytes)), auth.RoleOperator, "ws_alpha")
	provReq.Header.Set("Content-Type", "application/json")
	provW := httptest.NewRecorder()
	h.handleProvision(provW, provReq)
	if provW.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", provW.Code, provW.Body.String())
	}

	req := authedWorkspaceRequest(httptest.NewRequest(http.MethodGet, "/api/deployments", nil), auth.RoleOperator, "ws_alpha")
	w := httptest.NewRecorder()
	h.handleDeployments(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Attempts []map[string]interface{} `json:"attempts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Attempts) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(resp.Attempts))
	}
	if resp.Attempts[0]["selected_model_name"] != "Model A" {
		t.Fatalf("expected selected_model_name to be persisted, got %#v", resp.Attempts[0])
	}

	reqOther := authedWorkspaceRequest(httptest.NewRequest(http.MethodGet, "/api/deployments", nil), auth.RoleOperator, "ws_beta")
	wOther := httptest.NewRecorder()
	h.handleDeployments(wOther, reqOther)
	if wOther.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", wOther.Code, wOther.Body.String())
	}
	var otherResp struct {
		Attempts []map[string]interface{} `json:"attempts"`
	}
	if err := json.Unmarshal(wOther.Body.Bytes(), &otherResp); err != nil {
		t.Fatalf("decode other response: %v", err)
	}
	if len(otherResp.Attempts) != 0 {
		t.Fatalf("expected 0 attempts for other workspace, got %d", len(otherResp.Attempts))
	}
}

func TestHandleDeploymentVerificationUpdates(t *testing.T) {
	h := setupTestHandlers(t)
	store := newTestDeploymentStore(t)
	h.SetDeploymentStore(store)

	body := map[string]interface{}{
		"name":      "test-worker",
		"provider":  "mock",
		"gpu_type":  "RTX_4090",
		"gpu_count": 1,
	}
	bodyBytes, _ := json.Marshal(body)

	provReq := authedWorkspaceRequest(httptest.NewRequest(http.MethodPost, "/api/instances/provision", bytes.NewReader(bodyBytes)), auth.RoleOperator, "ws_alpha")
	provReq.Header.Set("Content-Type", "application/json")
	provW := httptest.NewRecorder()
	h.handleProvision(provW, provReq)
	if provW.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", provW.Code, provW.Body.String())
	}

	attempts, err := store.ListAttempts("ws_alpha", 10)
	if err != nil {
		t.Fatalf("ListAttempts: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(attempts))
	}

	verifyBody, _ := json.Marshal(map[string]interface{}{
		"status":      "passed",
		"verified_at": "2026-03-16T00:01:00Z",
		"latency_ms":  321,
		"model":       "org/model-a",
	})
	verifyReq := authedWorkspaceRequest(httptest.NewRequest(http.MethodPut, "/api/deployments/"+attempts[0].ID+"/verification", bytes.NewReader(verifyBody)), auth.RoleOperator, "ws_alpha")
	verifyReq.Header.Set("Content-Type", "application/json")
	verifyW := httptest.NewRecorder()
	h.handleDeploymentByID(verifyW, verifyReq)
	if verifyW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", verifyW.Code, verifyW.Body.String())
	}

	autoBody, _ := json.Marshal(map[string]interface{}{
		"requested_at": "2026-03-16T00:00:30Z",
	})
	autoReq := authedWorkspaceRequest(httptest.NewRequest(http.MethodPut, "/api/deployments/"+attempts[0].ID+"/auto-verification", bytes.NewReader(autoBody)), auth.RoleOperator, "ws_alpha")
	autoReq.Header.Set("Content-Type", "application/json")
	autoW := httptest.NewRecorder()
	h.handleDeploymentByID(autoW, autoReq)
	if autoW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", autoW.Code, autoW.Body.String())
	}

	updated, err := store.ListAttempts("ws_alpha", 10)
	if err != nil {
		t.Fatalf("ListAttempts updated: %v", err)
	}
	if updated[0].InferenceVerification == nil || updated[0].InferenceVerification.Status != "passed" {
		t.Fatalf("expected verification to be persisted, got %#v", updated[0].InferenceVerification)
	}
	if updated[0].AutoVerificationRequestedAt == nil {
		t.Fatalf("expected auto verification timestamp to be persisted")
	}
}

func TestHandleDeploymentVerificationMutationRoleMatrix(t *testing.T) {
	cases := []struct {
		name          string
		role          string
		principalType string
		wantStatus    int
	}{
		{name: "owner", role: auth.RoleOwner, principalType: auth.PrincipalHuman, wantStatus: http.StatusOK},
		{name: "admin", role: auth.RoleAdmin, principalType: auth.PrincipalHuman, wantStatus: http.StatusOK},
		{name: "operator", role: auth.RoleOperator, principalType: auth.PrincipalHuman, wantStatus: http.StatusOK},
		{name: "operator service", role: auth.RoleOperator, principalType: auth.PrincipalServiceAccount, wantStatus: http.StatusOK},
		{name: "read only", role: auth.RoleReadOnly, principalType: auth.PrincipalHuman, wantStatus: http.StatusForbidden},
		{name: "read only service", role: auth.RoleReadOnly, principalType: auth.PrincipalServiceAccount, wantStatus: http.StatusForbidden},
		{name: "developer", role: auth.RoleDeveloper, principalType: auth.PrincipalHuman, wantStatus: http.StatusForbidden},
		{name: "billing", role: auth.RoleBilling, principalType: auth.PrincipalHuman, wantStatus: http.StatusForbidden},
	}
	actions := []struct {
		name string
		path string
		body string
	}{
		{name: "verification", path: "/api/deployments/attempt-1/verification", body: `{"status":"passed"}`},
		{name: "auto verification", path: "/api/deployments/attempt-1/auto-verification", body: `{}`},
	}

	for _, action := range actions {
		for _, tc := range cases {
			t.Run(action.name+"/"+tc.name, func(t *testing.T) {
				h := setupTestHandlers(t)
				h.SetDeploymentStore(&stubDeploymentHistoryStore{})
				req := httptest.NewRequest(http.MethodPut, action.path, strings.NewReader(action.body))
				req = req.WithContext(auth.ContextWithKey(req.Context(), &auth.KeyRecord{
					Role:          tc.role,
					PrincipalType: tc.principalType,
					Status:        "active",
					WorkspaceID:   "ws_alpha",
				}))
				rec := httptest.NewRecorder()
				h.handleDeploymentByID(rec, req)
				if rec.Code != tc.wantStatus {
					t.Fatalf("expected %d, got %d: %s", tc.wantStatus, rec.Code, rec.Body.String())
				}
			})
		}
	}
}

func TestHandleStartStopMapsProviderErrors(t *testing.T) {
	mgr, err := providers.NewManager(providers.ManagerConfig{DefaultProvider: providers.ProviderMock})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	provider := &failingProvider{
		startErr: &providers.ProviderError{
			Provider: providers.ProviderMock,
			Code:     providers.ProviderErrorNotImplemented,
			Message:  "start not implemented",
		},
	}
	mgr.RegisterProvider(provider)
	if _, err := mgr.Provision(context.Background(), &providers.ProvisionRequest{
		Name:     "failing",
		Provider: providers.ProviderMock,
		GPUType:  providers.GPURTX4090,
	}); err != nil {
		t.Fatalf("provision instance: %v", err)
	}
	h := NewInstanceHandlers(mgr)

	req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/instances/inst-1/start", nil), auth.RoleOperator)
	w := httptest.NewRecorder()

	h.handleInstanceByID(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"]["type"] != "not_implemented" {
		t.Fatalf("expected not_implemented, got %#v", resp)
	}
}

func TestHandleCosts(t *testing.T) {
	h := setupTestHandlers(t)

	t.Run("GET costs - empty", func(t *testing.T) {
		req := authedRequest(httptest.NewRequest(http.MethodGet, "/api/costs", nil), auth.RoleBilling)
		w := httptest.NewRecorder()

		h.handleCosts(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}

		if resp["current_hourly"].(float64) != 0 {
			t.Errorf("expected 0 hourly cost with no instances, got %f", resp["current_hourly"])
		}
	})

	t.Run("GET costs - with instances", func(t *testing.T) {
		// Provision an instance first
		body := map[string]interface{}{
			"name":     "cost-test",
			"provider": "mock",
			"gpu_type": "RTX_4090",
		}
		bodyBytes, _ := json.Marshal(body)

		provReq := authedRequest(httptest.NewRequest(http.MethodPost, "/api/instances/provision", bytes.NewReader(bodyBytes)), auth.RoleOperator)
		provReq.Header.Set("Content-Type", "application/json")
		provW := httptest.NewRecorder()
		h.handleProvision(provW, provReq)

		// Now check costs
		req := authedRequest(httptest.NewRequest(http.MethodGet, "/api/costs", nil), auth.RoleBilling)
		w := httptest.NewRecorder()

		h.handleCosts(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}

		if resp["current_hourly"].(float64) <= 0 {
			t.Error("expected positive hourly cost with running instance")
		}
	})

	t.Run("GET costs - operators are forbidden", func(t *testing.T) {
		req := authedRequest(httptest.NewRequest(http.MethodGet, "/api/costs", nil), auth.RoleOperator)
		w := httptest.NewRecorder()

		h.handleCosts(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
		}
	})
}

func TestInstanceToMap(t *testing.T) {
	now := time.Now()
	instance := &providers.Instance{
		ID:           "map-test",
		ProviderID:   "mock-map-test",
		Provider:     providers.ProviderMock,
		Name:         "Test Instance",
		Status:       providers.InstanceStatusRunning,
		GPUType:      providers.GPURTX4090,
		GPUCount:     2,
		VCPU:         16,
		MemoryGB:     64,
		StorageGB:    200,
		PublicIP:     "192.168.1.1",
		HTTPPort:     8080,
		SSHPort:      22,
		WorkerID:     "worker-123",
		Models:       []string{"llama-3-8b"},
		Engine:       providers.EngineTensorRTLLM,
		CostPerHour:  0.80,
		SpotInstance: true,
		CreatedAt:    now,
		StartedAt:    &now,
	}

	m := instanceToMap(instance)

	tests := []struct {
		key      string
		expected interface{}
	}{
		{"id", "map-test"},
		{"provider_id", "mock-map-test"},
		{"provider", providers.ProviderMock},
		{"name", "Test Instance"},
		{"status", providers.InstanceStatusRunning},
		{"gpu_type", providers.GPURTX4090},
		{"gpu_count", 2},
		{"vcpu", 16},
		{"memory_gb", 64},
		{"storage_gb", 200},
		{"public_ip", "192.168.1.1"},
		{"http_port", 8080},
		{"ssh_port", 22},
		{"worker_id", "worker-123"},
		{"engine", providers.EngineTensorRTLLM},
		{"cost_per_hour", 0.80},
		{"spot_instance", true},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if m[tt.key] != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, m[tt.key])
			}
		})
	}

	t.Run("started_at is set", func(t *testing.T) {
		if m["started_at"] == nil {
			t.Error("started_at should be set")
		}
	})

	t.Run("stopped_at is nil", func(t *testing.T) {
		if _, exists := m["stopped_at"]; exists {
			t.Error("stopped_at should not exist when nil")
		}
	})
}

func TestInstanceToMapIncludesWorkerRegistrationLifecycle(t *testing.T) {
	now := time.Now()
	deadline := now.Add(10 * time.Minute)
	registeredAt := now.Add(-2 * time.Minute)
	heartbeatAt := now.Add(-30 * time.Second)
	instance := &providers.Instance{
		ID:                            "map-lifecycle",
		ProviderID:                    "runpod-lifecycle",
		Provider:                      providers.ProviderRunPod,
		Name:                          "Lifecycle Instance",
		Status:                        providers.InstanceStatusRunning,
		GPUType:                       providers.GPUA100_80,
		GPUCount:                      1,
		CostPerHour:                   1.19,
		CreatedAt:                     now.Add(-15 * time.Minute),
		WorkerID:                      "worker-lifecycle",
		WorkerRegistrationStatus:      providers.WorkerRegistrationProviderRunningNoNetwork,
		WorkerRegistrationDeadline:    &deadline,
		LastWorkerRegistrationError:   "Provider reports instance running, but no public/proxy endpoint is available yet",
		LastWorkerRegistrationCheckAt: &now,
		WorkerRegisteredAt:            &registeredAt,
		WorkerLastHeartbeatAt:         &heartbeatAt,
		WorkerHealthURL:               "https://runpod-lifecycle-8081.proxy.runpod.net/health",
		ProviderNetworkReady:          false,
		ProviderNetworkError:          "Provider reports instance running, but no public/proxy endpoint is available yet",
		Metadata: map[string]string{
			"api_key":       "secret",
			"authorization": "Bearer secret",
		},
	}

	m := instanceToMap(instance)

	if got := m["worker_registration_status"]; got != providers.WorkerRegistrationProviderRunningNoNetwork {
		t.Fatalf("expected lifecycle status, got %v", got)
	}
	if got := m["last_worker_registration_error"]; got == "" {
		t.Fatal("expected last registration error")
	}
	if got := m["worker_health_url"]; got != "https://runpod-lifecycle-8081.proxy.runpod.net/health" {
		t.Fatalf("expected worker health url, got %v", got)
	}
	if got := m["provider_network_ready"]; got != false {
		t.Fatalf("expected provider network not ready, got %v", got)
	}
	for _, key := range []string{
		"worker_registration_deadline",
		"last_worker_registration_check_at",
		"worker_registered_at",
		"worker_last_heartbeat_at",
	} {
		if _, ok := m[key]; !ok {
			t.Fatalf("expected %s in instance map", key)
		}
	}
	if _, ok := m["metadata"]; ok {
		t.Fatal("instance map must not expose provider metadata")
	}
	if _, ok := m["api_key"]; ok {
		t.Fatal("instance map must not expose API keys")
	}
	if _, ok := m["authorization"]; ok {
		t.Fatal("instance map must not expose authorization headers")
	}
}
