package mock

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/infera/infera/go/internal/providers"
)

// Provider is a mock GPU provider for testing.
type Provider struct {
	instances map[string]*providers.Instance
	mu        sync.RWMutex
}

// New creates a new mock provider.
func New() *Provider {
	return &Provider{
		instances: make(map[string]*providers.Instance),
	}
}

// Factory creates a mock provider from generic config.
func Factory(config providers.ProviderConfig) (providers.Provider, error) {
	return New(), nil
}

func init() {
	providers.RegisterProvider(providers.ProviderMock, Factory)
}

func (p *Provider) Name() providers.ProviderType {
	return providers.ProviderMock
}

func (p *Provider) Provision(ctx context.Context, req *providers.ProvisionRequest) (*providers.Instance, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	id := uuid.New().String()[:8]
	now := time.Now()

	costPerHour := getCostForGPU(req.GPUType) * float64(req.GPUCount)
	if req.SpotInstance {
		costPerHour *= 0.5
	}

	instance := &providers.Instance{
		ID:           id,
		ProviderID:   "mock-" + id,
		Provider:     providers.ProviderMock,
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
		CostPerHour:  costPerHour,
		SpotInstance: req.SpotInstance,
		Models:       req.Models,
		CreatedAt:    now,
		StartedAt:    &now,
	}

	p.instances[id] = instance
	return instance, nil
}

func (p *Provider) Terminate(ctx context.Context, instanceID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	instance, exists := p.instances[instanceID]
	if !exists {
		return &providers.ProviderError{
			Provider: providers.ProviderMock,
			Code:     "not_found",
			Message:  "instance not found",
		}
	}

	instance.Status = providers.InstanceStatusTerminated
	now := time.Now()
	instance.StoppedAt = &now
	return nil
}

func (p *Provider) Start(ctx context.Context, instanceID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	instance, exists := p.instances[instanceID]
	if !exists {
		return &providers.ProviderError{Provider: providers.ProviderMock, Code: "not_found", Message: "instance not found"}
	}

	instance.Status = providers.InstanceStatusRunning
	now := time.Now()
	instance.StartedAt = &now
	instance.StoppedAt = nil
	return nil
}

func (p *Provider) Stop(ctx context.Context, instanceID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	instance, exists := p.instances[instanceID]
	if !exists {
		return &providers.ProviderError{Provider: providers.ProviderMock, Code: "not_found", Message: "instance not found"}
	}

	instance.Status = providers.InstanceStatusStopped
	now := time.Now()
	instance.StoppedAt = &now
	return nil
}

func (p *Provider) GetInstance(ctx context.Context, instanceID string) (*providers.Instance, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	instance, exists := p.instances[instanceID]
	if !exists {
		return nil, &providers.ProviderError{Provider: providers.ProviderMock, Code: "not_found", Message: "instance not found"}
	}
	return instance, nil
}

func (p *Provider) ListInstances(ctx context.Context) ([]*providers.Instance, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	instances := make([]*providers.Instance, 0, len(p.instances))
	for _, inst := range p.instances {
		instances = append(instances, inst)
	}
	return instances, nil
}

func (p *Provider) ListOfferings(ctx context.Context) ([]*providers.GPUOffering, error) {
	return []*providers.GPUOffering{
		{Provider: providers.ProviderMock, GPUType: providers.GPURTX4090, GPUCount: 1, VCPU: 8, MemoryGB: 32, StorageGB: 100, CostPerHour: 0.40, SpotPrice: 0.20, Region: "mock", Available: 100},
		{Provider: providers.ProviderMock, GPUType: providers.GPUA100_40, GPUCount: 1, VCPU: 16, MemoryGB: 64, StorageGB: 200, CostPerHour: 1.20, SpotPrice: 0.60, Region: "mock", Available: 50},
		{Provider: providers.ProviderMock, GPUType: providers.GPUA100_80, GPUCount: 1, VCPU: 16, MemoryGB: 128, StorageGB: 200, CostPerHour: 2.00, SpotPrice: 1.00, Region: "mock", Available: 25},
		{Provider: providers.ProviderMock, GPUType: providers.GPUH100, GPUCount: 1, VCPU: 24, MemoryGB: 256, StorageGB: 500, CostPerHour: 3.50, SpotPrice: 1.75, Region: "mock", Available: 10},
	}, nil
}

func (p *Provider) GetStatus(ctx context.Context) (*providers.ProviderStatus, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	activeCount := 0
	for _, inst := range p.instances {
		if inst.Status == providers.InstanceStatusRunning {
			activeCount++
		}
	}

	return &providers.ProviderStatus{
		Provider:    providers.ProviderMock,
		Connected:   true,
		AccountID:   "mock-account",
		Balance:     1000.00,
		ActiveCount: activeCount,
		QuotaLimit:  100,
	}, nil
}

func (p *Provider) WaitForReady(ctx context.Context, instanceID string) error {
	return nil
}

func getCostForGPU(gpuType providers.GPUType) float64 {
	switch gpuType {
	case providers.GPURTX4090:
		return 0.40
	case providers.GPURTX4080:
		return 0.30
	case providers.GPUA100_40:
		return 1.20
	case providers.GPUA100_80:
		return 2.00
	case providers.GPUH100:
		return 3.50
	case providers.GPUL40S:
		return 1.50
	default:
		return 0.50
	}
}
