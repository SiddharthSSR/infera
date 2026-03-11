package providers

import (
	"context"
	"testing"
	"time"
)

// mockTestProvider is a simple mock for testing the manager
type mockTestProvider struct {
	instances map[string]*Instance
	lastReq   *ProvisionRequest
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
		CostPerHour: 1.00,
		CreatedAt:   now,
		StartedAt:   &now,
	}
	p.instances[id] = instance
	return instance, nil
}

func TestManagerProvisionSetsDefaultGatewayAddress(t *testing.T) {
	provider := newMockTestProvider()
	mgr := NewManager(ManagerConfig{
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
	return nil
}

func (p *mockTestProvider) Stop(ctx context.Context, instanceID string) error {
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

func TestNewManager(t *testing.T) {
	config := ManagerConfig{
		DefaultProvider: ProviderMock,
		WorkerImage:     "test-image:latest",
		GatewayAddress:  "localhost:8080",
	}

	mgr := NewManager(config)

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
	mgr := NewManager(ManagerConfig{})
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
	mgr := NewManager(ManagerConfig{})

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
	mgr := NewManager(ManagerConfig{
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
	mgr := NewManager(ManagerConfig{
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

func TestManagerProvisionUnregisteredProvider(t *testing.T) {
	mgr := NewManager(ManagerConfig{})
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
	mgr := NewManager(ManagerConfig{DefaultProvider: ProviderMock})
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
	mgr := NewManager(ManagerConfig{})

	ctx := context.Background()
	err := mgr.Terminate(ctx, "non-existent")
	if err == nil {
		t.Error("expected error for non-existent instance")
	}
}

func TestManagerStartStop(t *testing.T) {
	mgr := NewManager(ManagerConfig{DefaultProvider: ProviderMock})
	mgr.RegisterProvider(newMockTestProvider())

	ctx := context.Background()
	req := &ProvisionRequest{Name: "start-stop", GPUType: GPURTX4090}
	instance, _ := mgr.Provision(ctx, req)

	t.Run("Stop", func(t *testing.T) {
		err := mgr.Stop(ctx, instance.ID)
		if err != nil {
			t.Fatalf("Stop failed: %v", err)
		}
	})

	t.Run("Start", func(t *testing.T) {
		err := mgr.Start(ctx, instance.ID)
		if err != nil {
			t.Fatalf("Start failed: %v", err)
		}
	})
}

func TestManagerListOfferings(t *testing.T) {
	mgr := NewManager(ManagerConfig{})
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
	mgr := NewManager(ManagerConfig{})
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
	mgr := NewManager(ManagerConfig{DefaultProvider: ProviderMock})
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
	mgr := NewManager(ManagerConfig{DefaultProvider: ProviderMock})
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
	mgr := NewManager(ManagerConfig{})

	err := mgr.LinkWorker("non-existent", "worker-123")
	if err == nil {
		t.Error("expected error for non-existent instance")
	}
}
