package providers

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Manager orchestrates multiple GPU providers.
type Manager struct {
	providers map[ProviderType]Provider
	instances map[string]*Instance
	costs     *CostTracker
	mu        sync.RWMutex

	defaultProvider ProviderType
	workerImage     string
	gatewayAddress  string
}

// ManagerConfig configures the instance manager.
type ManagerConfig struct {
	DefaultProvider ProviderType
	WorkerImage     string
	GatewayAddress  string
}

// NewManager creates a new instance manager.
func NewManager(config ManagerConfig) *Manager {
	return &Manager{
		providers:       make(map[ProviderType]Provider),
		instances:       make(map[string]*Instance),
		costs:           NewCostTracker(),
		defaultProvider: config.DefaultProvider,
		workerImage:     config.WorkerImage,
		gatewayAddress:  config.GatewayAddress,
	}
}

// RegisterProvider adds a provider to the manager.
func (m *Manager) RegisterProvider(provider Provider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.providers[provider.Name()] = provider
}

// GetProvider returns a provider by type.
func (m *Manager) GetProvider(providerType ProviderType) (Provider, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, exists := m.providers[providerType]
	return p, exists
}

// ListProviders returns all registered providers.
func (m *Manager) ListProviders() []ProviderType {
	m.mu.RLock()
	defer m.mu.RUnlock()

	types := make([]ProviderType, 0, len(m.providers))
	for t := range m.providers {
		types = append(types, t)
	}
	return types
}

// Provision creates a new GPU instance.
func (m *Manager) Provision(ctx context.Context, req *ProvisionRequest) (*Instance, error) {
	providerType := req.Provider
	if providerType == "" {
		providerType = m.defaultProvider
	}

	provider, exists := m.GetProvider(providerType)
	if !exists {
		return nil, fmt.Errorf("provider %s not registered", providerType)
	}

	if req.DockerImage == "" {
		req.DockerImage = m.workerImage
	}
	if req.GPUCount == 0 {
		req.GPUCount = 1
	}

	instance, err := provider.Provision(ctx, req)
	if err != nil {
		return nil, err
	}

	if instance.ID == "" {
		instance.ID = uuid.New().String()[:8]
	}

	m.mu.Lock()
	m.instances[instance.ID] = instance
	m.mu.Unlock()

	m.costs.StartTracking(instance)

	return instance, nil
}

// Terminate destroys an instance.
func (m *Manager) Terminate(ctx context.Context, instanceID string) error {
	instance, exists := m.GetInstance(instanceID)
	if !exists {
		return fmt.Errorf("instance %s not found", instanceID)
	}

	provider, exists := m.GetProvider(instance.Provider)
	if !exists {
		return fmt.Errorf("provider %s not registered", instance.Provider)
	}

	if err := provider.Terminate(ctx, instance.ProviderID); err != nil {
		return err
	}

	m.costs.StopTracking(instanceID)

	m.mu.Lock()
	instance.Status = InstanceStatusTerminated
	now := time.Now()
	instance.StoppedAt = &now
	m.mu.Unlock()

	return nil
}

// Start starts a stopped instance.
func (m *Manager) Start(ctx context.Context, instanceID string) error {
	instance, exists := m.GetInstance(instanceID)
	if !exists {
		return fmt.Errorf("instance %s not found", instanceID)
	}

	provider, exists := m.GetProvider(instance.Provider)
	if !exists {
		return fmt.Errorf("provider %s not registered", instance.Provider)
	}

	if err := provider.Start(ctx, instance.ProviderID); err != nil {
		return err
	}

	m.costs.StartTracking(instance)
	return nil
}

// Stop stops a running instance.
func (m *Manager) Stop(ctx context.Context, instanceID string) error {
	instance, exists := m.GetInstance(instanceID)
	if !exists {
		return fmt.Errorf("instance %s not found", instanceID)
	}

	provider, exists := m.GetProvider(instance.Provider)
	if !exists {
		return fmt.Errorf("provider %s not registered", instance.Provider)
	}

	if err := provider.Stop(ctx, instance.ProviderID); err != nil {
		return err
	}

	m.costs.StopTracking(instanceID)
	return nil
}

// GetInstance returns an instance by ID.
func (m *Manager) GetInstance(instanceID string) (*Instance, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	instance, exists := m.instances[instanceID]
	return instance, exists
}

// ListInstances returns all tracked instances.
func (m *Manager) ListInstances() []*Instance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	instances := make([]*Instance, 0, len(m.instances))
	for _, inst := range m.instances {
		instances = append(instances, inst)
	}
	return instances
}

// ListOfferings returns available GPU configurations across all providers.
func (m *Manager) ListOfferings(ctx context.Context) ([]*GPUOffering, error) {
	m.mu.RLock()
	providers := make([]Provider, 0, len(m.providers))
	for _, p := range m.providers {
		providers = append(providers, p)
	}
	m.mu.RUnlock()

	var allOfferings []*GPUOffering
	for _, provider := range providers {
		offerings, err := provider.ListOfferings(ctx)
		if err != nil {
			continue
		}
		allOfferings = append(allOfferings, offerings...)
	}

	return allOfferings, nil
}

// GetProviderStatus returns status for all providers.
func (m *Manager) GetProviderStatus(ctx context.Context) []*ProviderStatus {
	m.mu.RLock()
	providers := make([]Provider, 0, len(m.providers))
	for _, p := range m.providers {
		providers = append(providers, p)
	}
	m.mu.RUnlock()

	var statuses []*ProviderStatus
	for _, provider := range providers {
		status, err := provider.GetStatus(ctx)
		if err != nil {
			statuses = append(statuses, &ProviderStatus{
				Provider:     provider.Name(),
				Connected:    false,
				ErrorMessage: err.Error(),
			})
		} else {
			statuses = append(statuses, status)
		}
	}

	return statuses
}

// GetCostSummary returns current cost information.
func (m *Manager) GetCostSummary() *CostSummary {
	return m.costs.GetSummary()
}

// LinkWorker associates a worker with an instance.
func (m *Manager) LinkWorker(instanceID, workerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	instance, exists := m.instances[instanceID]
	if !exists {
		return fmt.Errorf("instance %s not found", instanceID)
	}

	instance.WorkerID = workerID
	return nil
}
