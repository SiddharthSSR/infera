package providers

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newTestManager(t *testing.T, config ManagerConfig) *Manager {
	t.Helper()
	mgr, err := NewManager(config)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	t.Cleanup(func() {
		_ = mgr.Close()
	})
	return mgr
}

// mockTestProvider is a simple mock for testing the manager
type mockTestProvider struct {
	instances map[string]*Instance
	lastReq   *ProvisionRequest
	started   []string
	stopped   []string
}

type workspaceConfiguredProvider struct {
	apiKey string
}

func newMockTestProvider() *mockTestProvider {
	return &mockTestProvider{
		instances: make(map[string]*Instance),
	}
}

func (p *mockTestProvider) Name() ProviderType {
	return ProviderMock
}

func (p *mockTestProvider) Provision(ctx context.Context, req *ProvisionRequest) (*Instance, error) {
	p.lastReq = req
	id := "test-" + time.Now().Format("150405")
	now := time.Now()
	instance := &Instance{
		ID:          id,
		ProviderID:  "mock-" + id,
		Provider:    ProviderMock,
		Name:        req.Name,
		Status:      InstanceStatusRunning,
		GPUType:     req.GPUType,
		GPUCount:    req.GPUCount,
		Models:      append([]string(nil), req.Models...),
		CostPerHour: 1.00,
		CreatedAt:   now,
		StartedAt:   &now,
	}
	p.instances[id] = instance
	return instance, nil
}

func TestManagerProvisionSetsDefaultGatewayAddress(t *testing.T) {
	provider := newMockTestProvider()
	mgr := newTestManager(t, ManagerConfig{
		DefaultProvider: ProviderMock,
		WorkerImage:     "worker:latest",
		GatewayAddress:  "https://inferai.co.in",
	})
	mgr.RegisterProvider(provider)

	ctx := context.Background()
	req := &ProvisionRequest{
		Name:    "gateway-default-test",
		GPUType: GPURTX4090,
	}

	if _, err := mgr.Provision(ctx, req); err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	if provider.lastReq == nil {
		t.Fatal("expected provider to receive provision request")
	}
	if provider.lastReq.GatewayAddress != "https://inferai.co.in" {
		t.Fatalf("expected default gateway address to be injected, got %q", provider.lastReq.GatewayAddress)
	}
}

func (p *mockTestProvider) Terminate(ctx context.Context, instanceID string) error {
	return nil
}

func (p *mockTestProvider) Start(ctx context.Context, instanceID string) error {
	p.started = append(p.started, instanceID)
	if inst := p.findInstance(instanceID); inst != nil {
		inst.Status = InstanceStatusRunning
		now := time.Now()
		inst.StartedAt = &now
		inst.StoppedAt = nil
	}
	return nil
}

func (p *mockTestProvider) Stop(ctx context.Context, instanceID string) error {
	p.stopped = append(p.stopped, instanceID)
	if inst := p.findInstance(instanceID); inst != nil {
		inst.Status = InstanceStatusStopped
		now := time.Now()
		inst.StoppedAt = &now
	}
	return nil
}

func (p *mockTestProvider) GetInstance(ctx context.Context, instanceID string) (*Instance, error) {
	if inst, ok := p.instances[instanceID]; ok {
		return inst, nil
	}
	return nil, &ProviderError{Code: "not_found", Message: "not found"}
}

func (p *mockTestProvider) ListInstances(ctx context.Context) ([]*Instance, error) {
	result := make([]*Instance, 0, len(p.instances))
	for _, inst := range p.instances {
		result = append(result, inst)
	}
	return result, nil
}

func (p *mockTestProvider) ListOfferings(ctx context.Context) ([]*GPUOffering, error) {
	return []*GPUOffering{
		{Provider: ProviderMock, GPUType: GPURTX4090, CostPerHour: 0.50},
	}, nil
}

func (p *mockTestProvider) GetStatus(ctx context.Context) (*ProviderStatus, error) {
	return &ProviderStatus{
		Provider:    ProviderMock,
		Connected:   true,
		ActiveCount: len(p.instances),
	}, nil
}

func (p *mockTestProvider) WaitForReady(ctx context.Context, instanceID string) error {
	return nil
}

func (p *mockTestProvider) findInstance(id string) *Instance {
	if inst, ok := p.instances[id]; ok {
		return inst
	}
	for _, inst := range p.instances {
		if inst.ProviderID == id {
			return inst
		}
	}
	return nil
}

func (p *workspaceConfiguredProvider) Name() ProviderType { return ProviderRunPod }
func (p *workspaceConfiguredProvider) Provision(ctx context.Context, req *ProvisionRequest) (*Instance, error) {
	now := time.Now()
	return &Instance{
		ID:          "workspace-inst",
		ProviderID:  "workspace-provider-inst",
		Provider:    ProviderRunPod,
		WorkspaceID: req.WorkspaceID,
		Name:        req.Name,
		Status:      InstanceStatusRunning,
		GPUType:     req.GPUType,
		GPUCount:    req.GPUCount,
		CostPerHour: 2.0,
		CreatedAt:   now,
		StartedAt:   &now,
	}, nil
}
func (p *workspaceConfiguredProvider) Terminate(ctx context.Context, instanceID string) error {
	return nil
}
func (p *workspaceConfiguredProvider) Start(ctx context.Context, instanceID string) error { return nil }
func (p *workspaceConfiguredProvider) Stop(ctx context.Context, instanceID string) error  { return nil }
func (p *workspaceConfiguredProvider) GetInstance(ctx context.Context, instanceID string) (*Instance, error) {
	return &Instance{ID: "workspace-inst", ProviderID: instanceID, Provider: ProviderRunPod, Status: InstanceStatusRunning}, nil
}
func (p *workspaceConfiguredProvider) ListInstances(ctx context.Context) ([]*Instance, error) {
	return nil, nil
}
func (p *workspaceConfiguredProvider) ListOfferings(ctx context.Context) ([]*GPUOffering, error) {
	return []*GPUOffering{{Provider: ProviderRunPod, GPUType: GPUL40S, CostPerHour: 2.0}}, nil
}
func (p *workspaceConfiguredProvider) GetStatus(ctx context.Context) (*ProviderStatus, error) {
	return &ProviderStatus{Provider: ProviderRunPod, Connected: p.apiKey != "", ActiveCount: 1}, nil
}
func (p *workspaceConfiguredProvider) WaitForReady(ctx context.Context, instanceID string) error {
	return nil
}

func TestNewManager(t *testing.T) {
	config := ManagerConfig{
		DefaultProvider: ProviderMock,
		WorkerImage:     "test-image:latest",
		GatewayAddress:  "localhost:8080",
	}

	mgr := newTestManager(t, config)

	if mgr == nil {
		t.Fatal("NewManager returned nil")
	}
	if mgr.defaultProvider != ProviderMock {
		t.Errorf("expected mock, got %s", mgr.defaultProvider)
	}
	if mgr.workerImage != "test-image:latest" {
		t.Errorf("expected test-image:latest, got %s", mgr.workerImage)
	}
}

func TestRegisterProvider(t *testing.T) {
	mgr := newTestManager(t, ManagerConfig{})
	provider := newMockTestProvider()

	mgr.RegisterProvider(provider)

	p, exists := mgr.GetProvider(ProviderMock)
	if !exists {
		t.Error("provider should exist after registration")
	}
	if p.Name() != ProviderMock {
		t.Errorf("expected mock, got %s", p.Name())
	}
}

func TestListProviders(t *testing.T) {
	mgr := newTestManager(t, ManagerConfig{})

	t.Run("Empty initially", func(t *testing.T) {
		providers := mgr.ListProviders()
		if len(providers) != 0 {
			t.Errorf("expected 0, got %d", len(providers))
		}
	})

	mgr.RegisterProvider(newMockTestProvider())

	t.Run("Has provider after registration", func(t *testing.T) {
		providers := mgr.ListProviders()
		if len(providers) != 1 {
			t.Errorf("expected 1, got %d", len(providers))
		}
	})
}

func TestManagerProvision(t *testing.T) {
	mgr := newTestManager(t, ManagerConfig{
		DefaultProvider: ProviderMock,
		WorkerImage:     "worker:latest",
	})
	mgr.RegisterProvider(newMockTestProvider())

	ctx := context.Background()
	req := &ProvisionRequest{
		Name:     "test-instance",
		Provider: ProviderMock,
		GPUType:  GPURTX4090,
		GPUCount: 1,
	}

	instance, err := mgr.Provision(ctx, req)
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	t.Run("Instance created", func(t *testing.T) {
		if instance == nil {
			t.Fatal("instance is nil")
		}
		if instance.ID == "" {
			t.Error("instance ID should not be empty")
		}
	})

	t.Run("Instance tracked", func(t *testing.T) {
		instances := mgr.ListInstances()
		if len(instances) != 1 {
			t.Errorf("expected 1 instance, got %d", len(instances))
		}
	})

	t.Run("Can retrieve instance", func(t *testing.T) {
		inst, exists := mgr.GetInstance(instance.ID)
		if !exists {
			t.Error("instance should exist")
		}
		if inst.Name != "test-instance" {
			t.Errorf("expected test-instance, got %s", inst.Name)
		}
	})
}

func TestManagerProvisionWithDefaults(t *testing.T) {
	mgr := newTestManager(t, ManagerConfig{
		DefaultProvider: ProviderMock,
		WorkerImage:     "default-worker:latest",
	})
	mgr.RegisterProvider(newMockTestProvider())

	ctx := context.Background()
	req := &ProvisionRequest{
		Name:    "test",
		GPUType: GPURTX4090,
		// No provider specified - should use default
		// No GPU count - should default to 1
	}

	instance, err := mgr.Provision(ctx, req)
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	if instance.Provider != ProviderMock {
		t.Errorf("expected default provider mock, got %s", instance.Provider)
	}
}

func TestManagerWorkspaceScopedProviderResolution(t *testing.T) {
	RegisterProvider(ProviderRunPod, func(config ProviderConfig) (Provider, error) {
		return &workspaceConfiguredProvider{apiKey: config.APIKey}, nil
	})

	mgr := newTestManager(t, ManagerConfig{DefaultProvider: ProviderMock})
	mgr.RegisterProvider(newMockTestProvider())
	mgr.SetWorkspaceProviderConfigResolver(func(workspaceID string, providerType ProviderType) (*ProviderConfig, error) {
		if workspaceID == "ws_alpha" && providerType == ProviderRunPod {
			return &ProviderConfig{Type: providerType, APIKey: "workspace-key"}, nil
		}
		return nil, context.Canceled
	})

	offerings, err := mgr.ListOfferingsForWorkspace(context.Background(), "ws_alpha")
	if err != nil {
		t.Fatalf("ListOfferingsForWorkspace: %v", err)
	}
	if len(offerings) == 0 {
		t.Fatal("expected workspace offerings")
	}

	statuses := mgr.GetProviderStatusForWorkspace(context.Background(), "ws_alpha")
	found := false
	for _, status := range statuses {
		if status.Provider == ProviderRunPod {
			found = true
			if !status.Connected {
				t.Fatalf("expected workspace-scoped provider to be connected: %+v", status)
			}
		}
	}
	if !found {
		t.Fatal("expected runpod status for configured workspace")
	}
}

func TestManagerWorkspaceProviderResolutionFallsBackToRegisteredProvider(t *testing.T) {
	mgr := newTestManager(t, ManagerConfig{DefaultProvider: ProviderMock})
	mgr.RegisterProvider(newMockTestProvider())
	mgr.SetWorkspaceProviderConfigResolver(func(workspaceID string, providerType ProviderType) (*ProviderConfig, error) {
		return nil, nil
	})

	offerings, err := mgr.ListOfferingsForWorkspace(context.Background(), "ws_alpha")
	if err != nil {
		t.Fatalf("ListOfferingsForWorkspace: %v", err)
	}
	foundOffering := false
	for _, offering := range offerings {
		if offering.Provider == ProviderMock {
			foundOffering = true
		}
	}
	if !foundOffering {
		t.Fatalf("expected fallback mock offering, got %+v", offerings)
	}

	statuses := mgr.GetProviderStatusForWorkspace(context.Background(), "ws_alpha")
	foundStatus := false
	for _, status := range statuses {
		if status.Provider == ProviderMock {
			foundStatus = true
			if !status.Connected {
				t.Fatalf("expected fallback mock provider to be connected: %+v", status)
			}
		}
	}
	if !foundStatus {
		t.Fatalf("expected fallback mock status, got %+v", statuses)
	}
}

func TestManagerProvisionUnregisteredProvider(t *testing.T) {
	mgr := newTestManager(t, ManagerConfig{})
	// Don't register any providers

	ctx := context.Background()
	req := &ProvisionRequest{
		Name:     "test",
		Provider: ProviderRunPod, // Not registered
		GPUType:  GPURTX4090,
	}

	_, err := mgr.Provision(ctx, req)
	if err == nil {
		t.Error("expected error for unregistered provider")
	}
}

func TestManagerTerminate(t *testing.T) {
	mgr := newTestManager(t, ManagerConfig{DefaultProvider: ProviderMock})
	mgr.RegisterProvider(newMockTestProvider())

	ctx := context.Background()
	req := &ProvisionRequest{Name: "to-terminate", GPUType: GPURTX4090}
	instance, _ := mgr.Provision(ctx, req)

	err := mgr.Terminate(ctx, instance.ID)
	if err != nil {
		t.Fatalf("Terminate failed: %v", err)
	}

	// Check status updated
	inst, exists := mgr.GetInstance(instance.ID)
	if !exists {
		t.Error("instance should still exist (just terminated)")
	}
	if inst.Status != InstanceStatusTerminated {
		t.Errorf("expected terminated, got %s", inst.Status)
	}
}

func TestManagerTerminateNonExistent(t *testing.T) {
	mgr := newTestManager(t, ManagerConfig{})

	ctx := context.Background()
	err := mgr.Terminate(ctx, "non-existent")
	if err == nil {
		t.Error("expected error for non-existent instance")
	}
}

func TestManagerStartStop(t *testing.T) {
	mgr := newTestManager(t, ManagerConfig{DefaultProvider: ProviderMock})
	provider := newMockTestProvider()
	mgr.RegisterProvider(provider)

	ctx := context.Background()
	req := &ProvisionRequest{Name: "start-stop", GPUType: GPURTX4090}
	instance, _ := mgr.Provision(ctx, req)

	t.Run("Stop", func(t *testing.T) {
		err := mgr.Stop(ctx, instance.ID)
		if err != nil {
			t.Fatalf("Stop failed: %v", err)
		}
		inst, _ := mgr.GetInstance(instance.ID)
		if inst.Status != InstanceStatusStopped {
			t.Fatalf("expected stopped status, got %s", inst.Status)
		}
	})

	t.Run("Start", func(t *testing.T) {
		err := mgr.Start(ctx, instance.ID)
		if err != nil {
			t.Fatalf("Start failed: %v", err)
		}
		inst, _ := mgr.GetInstance(instance.ID)
		if inst.Status != InstanceStatusProvisioning {
			t.Fatalf("expected provisioning status after start, got %s", inst.Status)
		}
		if len(provider.started) != 1 {
			t.Fatalf("expected 1 provider start call, got %d", len(provider.started))
		}
	})
}

func TestManagerStartRejectedForTerminatedInstance(t *testing.T) {
	mgr := newTestManager(t, ManagerConfig{DefaultProvider: ProviderMock})
	provider := newMockTestProvider()
	mgr.RegisterProvider(provider)

	ctx := context.Background()
	req := &ProvisionRequest{Name: "terminated-start", GPUType: GPURTX4090}
	instance, _ := mgr.Provision(ctx, req)
	instance.Status = InstanceStatusTerminated

	err := mgr.Start(ctx, instance.ID)
	if err == nil {
		t.Fatal("expected terminated instance start to fail")
	}

	var providerErr *ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderError, got %T", err)
	}
	if providerErr.Code != ProviderErrorNotFound {
		t.Fatalf("expected not_found, got %q", providerErr.Code)
	}
}

func TestManagerRefreshMarksMissingInstanceTerminated(t *testing.T) {
	mgr := newTestManager(t, ManagerConfig{DefaultProvider: ProviderMock})
	provider := newMockTestProvider()
	mgr.RegisterProvider(provider)

	ctx := context.Background()
	req := &ProvisionRequest{Name: "missing-on-refresh", GPUType: GPURTX4090}
	instance, _ := mgr.Provision(ctx, req)
	delete(provider.instances, instance.ProviderID)

	if err := mgr.RefreshInstances(ctx); err != nil {
		t.Fatalf("RefreshInstances failed: %v", err)
	}

	refreshed, ok := mgr.GetInstance(instance.ID)
	if !ok {
		t.Fatal("expected tracked instance to still exist")
	}
	if refreshed.Status != InstanceStatusTerminated {
		t.Fatalf("expected terminated status, got %s", refreshed.Status)
	}
	if refreshed.ErrorMessage == "" {
		t.Fatal("expected refresh to persist missing-instance error")
	}
}

func TestManagerProvisionReusesStoppedInstance(t *testing.T) {
	mgr := newTestManager(t, ManagerConfig{DefaultProvider: ProviderMock, WorkerImage: "worker:v1"})
	provider := newMockTestProvider()
	mgr.RegisterProvider(provider)

	ctx := context.Background()
	req := &ProvisionRequest{
		Name:        "warm-reuse",
		Provider:    ProviderMock,
		WorkspaceID: "ws_alpha",
		GPUType:     GPURTX4090,
		GPUCount:    1,
		Models:      []string{"Qwen/Qwen2.5-7B-Instruct"},
	}

	first, err := mgr.Provision(ctx, req)
	if err != nil {
		t.Fatalf("first provision failed: %v", err)
	}
	if err := mgr.Stop(ctx, first.ID); err != nil {
		t.Fatalf("stop failed: %v", err)
	}

	provider.lastReq = nil

	second, err := mgr.Provision(ctx, req)
	if err != nil {
		t.Fatalf("second provision failed: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected stopped instance %s to be reused, got %s", first.ID, second.ID)
	}
	if provider.lastReq != nil {
		t.Fatal("expected no second provider provision call when a stopped instance matches")
	}
	if len(provider.started) == 0 {
		t.Fatal("expected provider start to be called for warm reuse")
	}
}

func TestManagerListOfferings(t *testing.T) {
	mgr := newTestManager(t, ManagerConfig{})
	mgr.RegisterProvider(newMockTestProvider())

	ctx := context.Background()
	offerings, err := mgr.ListOfferings(ctx)
	if err != nil {
		t.Fatalf("ListOfferings failed: %v", err)
	}

	if len(offerings) == 0 {
		t.Error("expected at least one offering")
	}
}

func TestManagerGetProviderStatus(t *testing.T) {
	mgr := newTestManager(t, ManagerConfig{})
	mgr.RegisterProvider(newMockTestProvider())

	ctx := context.Background()
	statuses := mgr.GetProviderStatus(ctx)

	if len(statuses) != 1 {
		t.Errorf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].Provider != ProviderMock {
		t.Errorf("expected mock, got %s", statuses[0].Provider)
	}
}

func TestManagerGetCostSummary(t *testing.T) {
	mgr := newTestManager(t, ManagerConfig{DefaultProvider: ProviderMock})
	mgr.RegisterProvider(newMockTestProvider())

	// Create some instances
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		req := &ProvisionRequest{Name: "cost-test", GPUType: GPURTX4090}
		mgr.Provision(ctx, req)
	}

	summary := mgr.GetCostSummary()
	if summary == nil {
		t.Fatal("summary is nil")
	}

	// With 3 instances at $1/hr each, current hourly should be $3
	if summary.CurrentHourly < 0 {
		t.Error("CurrentHourly should not be negative")
	}
}

func TestManagerLinkWorker(t *testing.T) {
	mgr := newTestManager(t, ManagerConfig{DefaultProvider: ProviderMock})
	mgr.RegisterProvider(newMockTestProvider())

	ctx := context.Background()
	req := &ProvisionRequest{Name: "link-test", GPUType: GPURTX4090}
	instance, _ := mgr.Provision(ctx, req)

	err := mgr.LinkWorker(instance.ID, "worker-123")
	if err != nil {
		t.Fatalf("LinkWorker failed: %v", err)
	}

	inst, _ := mgr.GetInstance(instance.ID)
	if inst.WorkerID != "worker-123" {
		t.Errorf("expected worker-123, got %s", inst.WorkerID)
	}
}

func TestManagerLinkWorkerNonExistent(t *testing.T) {
	mgr := newTestManager(t, ManagerConfig{})

	err := mgr.LinkWorker("non-existent", "worker-123")
	if err == nil {
		t.Error("expected error for non-existent instance")
	}
}

func TestManagerGetInstanceByProviderRef(t *testing.T) {
	mgr := newTestManager(t, ManagerConfig{DefaultProvider: ProviderMock})
	mgr.RegisterProvider(newMockTestProvider())

	ctx := context.Background()
	instance, err := mgr.Provision(ctx, &ProvisionRequest{Name: "provider-ref-test", GPUType: GPURTX4090})
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	instance.Provider = ProviderRunPod
	instance.ProviderID = "pod-123"

	found, ok := mgr.GetInstanceByProviderRef(ProviderRunPod, "pod-123")
	if !ok {
		t.Fatal("expected provider ref lookup to find instance")
	}
	if found.ID != instance.ID {
		t.Fatalf("expected instance %q, got %q", instance.ID, found.ID)
	}
}
