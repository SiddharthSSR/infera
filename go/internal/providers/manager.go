// Package providers implements GPU instance management.
package providers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Manager orchestrates multiple GPU providers.
type Manager struct {
	providers map[ProviderType]Provider
	instances map[string]*Instance // instanceID -> Instance
	costs     *CostTracker
	mu        sync.RWMutex

	// Configuration
	defaultProvider ProviderType
	workerImage     string
	gatewayAddress  string

	workspaceProviderConfigResolver func(workspaceID string, providerType ProviderType) (*ProviderConfig, error)
}

// ManagerConfig configures the instance manager.
type ManagerConfig struct {
	DefaultProvider ProviderType
	WorkerImage     string // Docker image for workers
	GatewayAddress  string // Gateway address for workers to connect
	CostDBPath      string // Path to SQLite DB for persistent cost tracking (empty = in-memory)
}

// NewManager creates a new instance manager.
func NewManager(config ManagerConfig) (*Manager, error) {
	var costs *CostTracker
	if config.CostDBPath != "" {
		var err error
		costs, err = NewPersistentCostTracker(config.CostDBPath)
		if err != nil {
			return nil, fmt.Errorf("initialize persistent cost tracker %q: %w", config.CostDBPath, err)
		}
	} else {
		costs = NewCostTracker()
	}

	return &Manager{
		providers:       make(map[ProviderType]Provider),
		instances:       make(map[string]*Instance),
		costs:           costs,
		defaultProvider: config.DefaultProvider,
		workerImage:     config.WorkerImage,
		gatewayAddress:  config.GatewayAddress,
	}, nil
}

// Close releases provider manager resources.
func (m *Manager) Close() error {
	return m.costs.Close()
}

// RegisterProvider adds a provider to the manager.
func (m *Manager) RegisterProvider(provider Provider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.providers[provider.Name()] = provider
}

func (m *Manager) SetWorkspaceProviderConfigResolver(resolver func(workspaceID string, providerType ProviderType) (*ProviderConfig, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workspaceProviderConfigResolver = resolver
}

// GetProvider returns a provider by type.
func (m *Manager) GetProvider(providerType ProviderType) (Provider, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, exists := m.providers[providerType]
	return p, exists
}

func (m *Manager) resolveProvider(workspaceID string, providerType ProviderType) (Provider, error) {
	m.mu.RLock()
	resolver := m.workspaceProviderConfigResolver
	m.mu.RUnlock()

	if workspaceID != "" && resolver != nil && providerType != ProviderMock {
		config, err := resolver(workspaceID, providerType)
		if err != nil {
			return nil, &ProviderError{
				Provider: providerType,
				Code:     ProviderErrorInvalidConfig,
				Message:  err.Error(),
			}
		}
		if config != nil {
			if config.Type == "" {
				config.Type = providerType
			}
			return CreateProvider(*config)
		}
		return nil, &ProviderError{
			Provider: providerType,
			Code:     ProviderErrorInvalidConfig,
			Message:  fmt.Sprintf("provider %s is not configured for workspace %s", providerType, workspaceID),
		}
	}

	provider, exists := m.GetProvider(providerType)
	if !exists {
		return nil, fmt.Errorf("provider %s not registered", providerType)
	}
	return provider, nil
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
	// Determine provider
	providerType := req.Provider
	if providerType == "" {
		providerType = m.defaultProvider
	}

	provider, err := m.resolveProvider(req.WorkspaceID, providerType)
	if err != nil {
		return nil, err
	}

	// Set defaults
	if req.DockerImage == "" {
		req.DockerImage = m.workerImage
	}
	if req.GPUCount == 0 {
		req.GPUCount = 1
	}
	if req.GatewayAddress == "" {
		req.GatewayAddress = m.gatewayAddress
	}

	// Create instance via provider
	instance, err := provider.Provision(ctx, req)
	if err != nil {
		return nil, err
	}

	// Generate internal ID if not set
	if instance.ID == "" {
		instance.ID = uuid.New().String()[:8]
	}
	if instance.WorkspaceID == "" {
		instance.WorkspaceID = req.WorkspaceID
	}

	// Track instance
	m.mu.Lock()
	m.instances[instance.ID] = instance
	m.mu.Unlock()

	// Start cost tracking
	m.costs.StartTracking(instance)

	return instance, nil
}

// Terminate destroys an instance.
func (m *Manager) Terminate(ctx context.Context, instanceID string) error {
	instance, exists := m.GetInstance(instanceID)
	if !exists {
		return fmt.Errorf("instance %s not found", instanceID)
	}

	provider, err := m.resolveProvider(instance.WorkspaceID, instance.Provider)
	if err != nil {
		return err
	}

	// Terminate via provider
	if err := provider.Terminate(ctx, instance.ProviderID); err != nil {
		return err
	}

	// Stop cost tracking
	m.costs.StopTracking(instanceID)

	// Update status
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

	provider, err := m.resolveProvider(instance.WorkspaceID, instance.Provider)
	if err != nil {
		return err
	}

	if err := provider.Start(ctx, instance.ProviderID); err != nil {
		return err
	}

	// Resume cost tracking
	m.costs.StartTracking(instance)

	return nil
}

// Stop stops a running instance.
func (m *Manager) Stop(ctx context.Context, instanceID string) error {
	instance, exists := m.GetInstance(instanceID)
	if !exists {
		return fmt.Errorf("instance %s not found", instanceID)
	}

	provider, err := m.resolveProvider(instance.WorkspaceID, instance.Provider)
	if err != nil {
		return err
	}

	if err := provider.Stop(ctx, instance.ProviderID); err != nil {
		return err
	}

	// Stop cost tracking
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

// ListInstancesByProvider returns instances for a specific provider.
func (m *Manager) ListInstancesByProvider(providerType ProviderType) []*Instance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var instances []*Instance
	for _, inst := range m.instances {
		if inst.Provider == providerType {
			instances = append(instances, inst)
		}
	}
	return instances
}

func (m *Manager) ListInstancesByWorkspace(workspaceID string) []*Instance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var instances []*Instance
	for _, inst := range m.instances {
		if inst.WorkspaceID == workspaceID {
			instances = append(instances, inst)
		}
	}
	return instances
}

// RefreshInstances updates instance status from providers.
func (m *Manager) RefreshInstances(ctx context.Context) error {
	m.mu.RLock()
	instances := make([]*Instance, 0, len(m.instances))
	for _, inst := range m.instances {
		instances = append(instances, inst)
	}
	m.mu.RUnlock()

	for _, inst := range instances {
		provider, err := m.resolveProvider(inst.WorkspaceID, inst.Provider)
		if err != nil {
			continue
		}
		refreshed, err := provider.GetInstance(ctx, inst.ProviderID)
		if err != nil {
			continue
		}

		m.mu.Lock()
		if existing, exists := m.instances[inst.ID]; exists {
			existing.Status = refreshed.Status
			existing.PublicIP = refreshed.PublicIP
			existing.ErrorMessage = refreshed.ErrorMessage
			existing.WorkerID = refreshed.WorkerID
		}
		m.mu.Unlock()
	}

	return nil
}

// ListOfferings returns available GPU configurations across all providers.
func (m *Manager) ListOfferings(ctx context.Context) ([]*GPUOffering, error) {
	return m.ListOfferingsForWorkspace(ctx, "")
}

func (m *Manager) ListOfferingsForWorkspace(ctx context.Context, workspaceID string) ([]*GPUOffering, error) {
	providerTypes := RegisteredProviderTypes()

	var allOfferings []*GPUOffering
	for _, providerType := range providerTypes {
		provider, err := m.resolveProvider(workspaceID, providerType)
		if err != nil {
			continue
		}
		offerings, err := provider.ListOfferings(ctx)
		if err != nil {
			slog.Warn("providers.list_offerings_failed",
				slog.String("provider", string(providerType)),
				slog.String("error", err.Error()),
			)
			continue // Skip failed providers
		}
		allOfferings = append(allOfferings, offerings...)
	}

	return allOfferings, nil
}

// GetProviderStatus returns status for all providers.
func (m *Manager) GetProviderStatus(ctx context.Context) []*ProviderStatus {
	return m.GetProviderStatusForWorkspace(ctx, "")
}

func (m *Manager) GetProviderStatusForWorkspace(ctx context.Context, workspaceID string) []*ProviderStatus {
	providerTypes := RegisteredProviderTypes()

	var statuses []*ProviderStatus
	for _, providerType := range providerTypes {
		provider, err := m.resolveProvider(workspaceID, providerType)
		if err != nil {
			continue
		}
		status, err := provider.GetStatus(ctx)
		if err != nil {
			failed := &ProviderStatus{
				Provider:     providerType,
				Connected:    false,
				ErrorMessage: err.Error(),
			}
			var providerErr *ProviderError
			if errors.As(err, &providerErr) {
				failed.ErrorCode = providerErr.Code
			}
			statuses = append(statuses, failed)
			continue
		}

		statuses = append(statuses, status)
	}

	return statuses
}

// GetCostSummary returns current cost information.
func (m *Manager) GetCostSummary() *CostSummary {
	return m.costs.GetSummary()
}

func (m *Manager) GetCostSummaryForWorkspace(workspaceID string) *CostSummary {
	return m.costs.GetSummaryByWorkspace(workspaceID)
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

// UnlinkWorker removes worker association from an instance.
func (m *Manager) UnlinkWorker(instanceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if instance, exists := m.instances[instanceID]; exists {
		instance.WorkerID = ""
	}
}

// GetInstanceByWorker finds an instance by its linked worker ID.
func (m *Manager) GetInstanceByWorker(workerID string) (*Instance, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, inst := range m.instances {
		if inst.WorkerID == workerID {
			return inst, true
		}
	}
	return nil, false
}
