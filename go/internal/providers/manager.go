// Package providers implements GPU instance management.
package providers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Manager orchestrates multiple GPU providers.
type Manager struct {
	providers       map[ProviderType]Provider
	instances       instanceStore
	costs           *CostTracker
	mu              sync.RWMutex
	workerBindingMu sync.Mutex

	// Configuration
	defaultProvider           ProviderType
	workerImage               string
	workerImages              map[InferenceEngine]string
	gatewayAddress            string
	workerRegistrationTimeout time.Duration
	workerHeartbeatTimeout    time.Duration
	now                       func() time.Time

	workspaceProviderConfigResolver WorkspaceProviderConfigResolver
}

// WorkspaceProviderConfigResolver resolves workspace-scoped provider credentials
// and endpoint settings for the manager. The default wiring still uses an
// in-process function adapter, but the manager no longer depends on a raw
// callback type internally.
type WorkspaceProviderConfigResolver interface {
	ResolveProviderConfig(workspaceID string, providerType ProviderType) (*ProviderConfig, error)
}

type workspaceProviderConfigResolverFunc func(workspaceID string, providerType ProviderType) (*ProviderConfig, error)

func (f workspaceProviderConfigResolverFunc) ResolveProviderConfig(workspaceID string, providerType ProviderType) (*ProviderConfig, error) {
	return f(workspaceID, providerType)
}

// ManagerConfig configures the instance manager.
type ManagerConfig struct {
	DefaultProvider           ProviderType
	WorkerImage               string                     // Default Docker image for workers
	WorkerImages              map[InferenceEngine]string // Engine-specific worker images
	GatewayAddress            string                     // Gateway address for workers to connect
	CostDBPath                string                     // Path to SQLite DB for persistent cost tracking (empty = in-memory)
	WorkerRegistrationTimeout time.Duration              // Max time a running provider instance may remain unregistered
	WorkerHeartbeatTimeout    time.Duration              // Max age of a linked worker heartbeat before lifecycle becomes unhealthy
	Now                       func() time.Time           // Optional clock used by deterministic lifecycle tests
}

// NewManager creates a new instance manager.
func NewManager(config ManagerConfig) (*Manager, error) {
	return NewManagerWithStore(config, newInMemoryInstanceStore())
}

// NewManagerWithStore creates a manager with an explicit tracked-instance store.
func NewManagerWithStore(config ManagerConfig, store instanceStore) (*Manager, error) {
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
	if store == nil {
		store = newInMemoryInstanceStore()
	}
	timeout := config.WorkerRegistrationTimeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	heartbeatTimeout := config.WorkerHeartbeatTimeout
	if heartbeatTimeout <= 0 {
		heartbeatTimeout = 90 * time.Second
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}

	return &Manager{
		providers:                 make(map[ProviderType]Provider),
		instances:                 store,
		costs:                     costs,
		defaultProvider:           config.DefaultProvider,
		workerImage:               config.WorkerImage,
		workerImages:              cloneWorkerImages(config.WorkerImages),
		gatewayAddress:            config.GatewayAddress,
		workerRegistrationTimeout: timeout,
		workerHeartbeatTimeout:    heartbeatTimeout,
		now:                       now,
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
	if resolver == nil {
		m.SetWorkspaceProviderConfigSource(nil)
		return
	}
	m.SetWorkspaceProviderConfigSource(workspaceProviderConfigResolverFunc(resolver))
}

func (m *Manager) SetWorkspaceProviderConfigSource(resolver WorkspaceProviderConfigResolver) {
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
		config, err := resolver.ResolveProviderConfig(workspaceID, providerType)
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
	if req.GPUCount == 0 {
		req.GPUCount = 1
	}
	req.Engine = req.Engine.OrDefault()
	if req.DockerImage == "" {
		req.DockerImage = resolveWorkerImage(req.Engine, m.workerImage, m.workerImages)
	}
	// The credential-bearing callback destination is platform configuration,
	// never a caller-controlled provisioning option.
	req.GatewayAddress = strings.TrimSpace(m.gatewayAddress)
	credential := make([]byte, 32)
	if _, err := rand.Read(credential); err != nil {
		return nil, fmt.Errorf("generate worker credential: %w", err)
	}
	req.WorkerToken = base64.RawURLEncoding.EncodeToString(credential)
	credentialHash := sha256.Sum256([]byte(req.WorkerToken))
	ApplyRuntimeDefaults(req)
	if providerType != ProviderMock {
		if err := ValidateWorkerImageRef(req.DockerImage); err != nil {
			return nil, &ProviderError{
				Provider: providerType,
				Code:     ProviderErrorInvalidRequest,
				Message:  err.Error(),
			}
		}
	}

	if reusable := m.findReusableStoppedInstance(providerType, req); reusable != nil {
		if err := m.Start(ctx, reusable.ID); err != nil {
			return nil, err
		}
		return reusable, nil
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
	if instance.Engine == "" {
		instance.Engine = req.Engine
	}
	// Keep the deployment-bound credential available only in the internal
	// instance record so the gateway can authenticate inference requests back
	// to this worker. Both credential forms are excluded from JSON responses.
	instance.WorkerCredential = req.WorkerToken
	instance.WorkerCredentialHash = credentialHash

	// Track instance and begin the worker-registration deadline.
	m.initializeWorkerRegistration(instance, m.now())
	m.instances.put(instance)

	// Start cost tracking
	m.costs.StartTracking(instance)

	return instance, nil
}

// AuthenticateWorkerToken resolves a provisioned instance from its unique worker credential.
func (m *Manager) AuthenticateWorkerToken(token string) (*Instance, bool) {
	tokenHash := sha256.Sum256([]byte(strings.TrimSpace(token)))
	if strings.TrimSpace(token) == "" {
		return nil, false
	}
	for _, instance := range m.instances.list() {
		if subtle.ConstantTimeCompare(tokenHash[:], instance.WorkerCredentialHash[:]) == 1 {
			return instance, true
		}
	}
	return nil, false
}

// AuthorizeWorkerBinding prevents one deployment credential from claiming another deployment's worker ID.
func (m *Manager) AuthorizeWorkerBinding(instanceID, workerID string) error {
	m.workerBindingMu.Lock()
	defer m.workerBindingMu.Unlock()
	return m.authorizeWorkerBinding(instanceID, workerID)
}

func (m *Manager) authorizeWorkerBinding(instanceID, workerID string) error {
	instance, ok := m.getTrackedInstance(instanceID)
	if !ok {
		return fmt.Errorf("instance %s not found", instanceID)
	}
	if instance.WorkerID != "" && instance.WorkerID != workerID {
		return fmt.Errorf("instance is already bound to worker %s", instance.WorkerID)
	}
	if owner, found := m.GetInstanceByWorker(workerID); found && owner.ID != instanceID {
		return fmt.Errorf("worker %s is already bound to another instance", workerID)
	}
	return nil
}

// Terminate destroys an instance.
func (m *Manager) Terminate(ctx context.Context, instanceID string) error {
	instance, exists := m.getTrackedInstance(instanceID)
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
	m.instances.update(instanceID, func(instance *Instance) {
		instance.Status = InstanceStatusTerminated
		now := m.now()
		instance.StoppedAt = &now
		m.clearWorkerRegistration(instance)
	})

	return nil
}

// Start starts a stopped instance.
func (m *Manager) Start(ctx context.Context, instanceID string) error {
	instance, exists := m.getTrackedInstance(instanceID)
	if !exists {
		return fmt.Errorf("instance %s not found", instanceID)
	}
	if instance.Status == InstanceStatusTerminated {
		return &ProviderError{
			Provider: instance.Provider,
			Code:     ProviderErrorNotFound,
			Message:  "instance can no longer be started because the provider no longer reports it",
		}
	}

	provider, err := m.resolveProvider(instance.WorkspaceID, instance.Provider)
	if err != nil {
		return err
	}

	if starter, ok := provider.(InstanceStarter); ok {
		if err := starter.StartWithInstance(ctx, instance); err != nil {
			return err
		}
	} else {
		if err := provider.Start(ctx, instance.ProviderID); err != nil {
			return err
		}
	}

	// Resume cost tracking
	m.costs.StartTracking(instance)

	m.instances.update(instanceID, func(instance *Instance) {
		now := m.now()
		instance.Status = InstanceStatusProvisioning
		instance.StartedAt = &now
		instance.StoppedAt = nil
		instance.ErrorMessage = ""
		m.initializeWorkerRegistration(instance, now)
	})

	return nil
}

// Stop stops a running instance.
func (m *Manager) Stop(ctx context.Context, instanceID string) error {
	instance, exists := m.getTrackedInstance(instanceID)
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

	m.instances.update(instanceID, func(instance *Instance) {
		now := m.now()
		instance.Status = InstanceStatusStopped
		instance.StoppedAt = &now
		m.clearWorkerRegistration(instance)
	})

	return nil
}

func (m *Manager) getTrackedInstance(instanceID string) (*Instance, bool) {
	return m.instances.get(instanceID)
}

func cloneInstance(instance *Instance) *Instance {
	if instance == nil {
		return nil
	}
	cloned := *instance
	if instance.Models != nil {
		cloned.Models = slices.Clone(instance.Models)
	}
	if instance.Metadata != nil {
		cloned.Metadata = make(map[string]string, len(instance.Metadata))
		for key, value := range instance.Metadata {
			cloned.Metadata[key] = value
		}
	}
	return &cloned
}

// GetInstance returns a defensive copy of an instance by ID.
func (m *Manager) GetInstance(instanceID string) (*Instance, bool) {
	m.refreshWorkerRegistrationStates(m.now())
	instance, exists := m.getTrackedInstance(instanceID)
	if !exists {
		return nil, false
	}
	return cloneInstance(instance), true
}

// ListInstances returns all tracked instances.
func (m *Manager) ListInstances() []*Instance {
	m.refreshWorkerRegistrationStates(m.now())
	return m.instances.list()
}

// ListInstancesByProvider returns instances for a specific provider.
func (m *Manager) ListInstancesByProvider(providerType ProviderType) []*Instance {
	m.refreshWorkerRegistrationStates(m.now())
	return m.instances.listByProvider(providerType)
}

func (m *Manager) ListInstancesByWorkspace(workspaceID string) []*Instance {
	m.refreshWorkerRegistrationStates(m.now())
	return m.instances.listByWorkspace(workspaceID)
}

func (m *Manager) findReusableStoppedInstance(providerType ProviderType, req *ProvisionRequest) *Instance {
	return m.instances.findReusableStopped(providerType, req)
}

// RefreshInstances updates instance status from providers.
func (m *Manager) RefreshInstances(ctx context.Context) error {
	instances := m.instances.list()

	for _, inst := range instances {
		provider, err := m.resolveProvider(inst.WorkspaceID, inst.Provider)
		if err != nil {
			continue
		}
		refreshed, err := provider.GetInstance(ctx, inst.ProviderID)
		if err != nil {
			var providerErr *ProviderError
			if errors.As(err, &providerErr) && providerErr.Code == ProviderErrorNotFound {
				m.instances.update(inst.ID, func(existing *Instance) {
					existing.Status = InstanceStatusTerminated
					existing.ErrorMessage = "Provider no longer reports this instance"
					now := m.now()
					if existing.StoppedAt == nil {
						existing.StoppedAt = &now
					}
				})
				m.costs.StopTracking(inst.ID)
			}
			continue
		}

		m.instances.update(inst.ID, func(existing *Instance) {
			existing.Status = refreshed.Status
			existing.PublicIP = refreshed.PublicIP
			existing.HTTPPort = refreshed.HTTPPort
			existing.SSHPort = refreshed.SSHPort
			existing.ErrorMessage = refreshed.ErrorMessage
			// Only update WorkerID from provider if non-empty; providers don't
			// track our worker process so a blank value from the refresh loop
			// must not overwrite a link we established via heartbeat.
			if refreshed.WorkerID != "" {
				existing.WorkerID = refreshed.WorkerID
			}
			m.evaluateWorkerRegistration(existing, m.now())
		})
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
	m.workerBindingMu.Lock()
	defer m.workerBindingMu.Unlock()
	if err := m.authorizeWorkerBinding(instanceID, workerID); err != nil {
		return err
	}
	if updated := m.instances.update(instanceID, func(instance *Instance) {
		instance.WorkerID = workerID
		now := m.now()
		if instance.WorkerRegisteredAt == nil {
			instance.WorkerRegisteredAt = &now
		}
		instance.WorkerLastHeartbeatAt = &now
		instance.LastWorkerRegistrationCheckAt = &now
		instance.WorkerRegistrationStatus = WorkerRegistrationReady
		instance.LastWorkerRegistrationError = ""
	}); !updated {
		return fmt.Errorf("instance %s not found", instanceID)
	}
	return nil
}

// RecordWorkerUnhealthy marks a linked worker unhealthy until a later heartbeat.
func (m *Manager) RecordWorkerUnhealthy(workerID string, observedAt time.Time) bool {
	instance, exists := m.instances.findByWorker(workerID)
	if !exists {
		return false
	}
	return m.instances.update(instance.ID, func(stored *Instance) {
		at := observedAt
		if at.IsZero() {
			at = m.now()
		}
		stored.LastWorkerRegistrationCheckAt = &at
		stored.WorkerRegistrationStatus = WorkerRegistrationRegisteredUnhealthy
		stored.LastWorkerRegistrationError = "Gateway registry reports the linked worker as unhealthy"
	})
}

// RecordWorkerHeartbeat updates the lifecycle timestamps for a linked worker.
func (m *Manager) RecordWorkerHeartbeat(workerID string, heartbeatAt time.Time) bool {
	instance, exists := m.instances.findByWorker(workerID)
	if !exists {
		return false
	}
	return m.instances.update(instance.ID, func(stored *Instance) {
		at := heartbeatAt
		if at.IsZero() {
			at = m.now()
		}
		stored.WorkerLastHeartbeatAt = &at
		if stored.WorkerRegisteredAt == nil {
			stored.WorkerRegisteredAt = &at
		}
		stored.LastWorkerRegistrationCheckAt = &at
		stored.WorkerRegistrationStatus = WorkerRegistrationReady
		stored.LastWorkerRegistrationError = ""
	})
}

// UnlinkWorker removes worker association from an instance.
func (m *Manager) UnlinkWorker(instanceID string) {
	m.instances.update(instanceID, func(instance *Instance) {
		instance.WorkerID = ""
		instance.WorkerRegisteredAt = nil
		instance.WorkerLastHeartbeatAt = nil
		m.evaluateWorkerRegistration(instance, m.now())
	})
}

// GetInstanceByWorker finds an instance by its linked worker ID.
func (m *Manager) GetInstanceByWorker(workerID string) (*Instance, bool) {
	return m.instances.findByWorker(workerID)
}

// WorkerCredentialForWorker returns the deployment-bound credential for a
// linked worker. It intentionally fails closed for unknown or legacy records.
func (m *Manager) WorkerCredentialForWorker(workerID string) (string, bool) {
	instance, found := m.instances.findByWorker(workerID)
	if !found {
		return "", false
	}
	credential := strings.TrimSpace(instance.WorkerCredential)
	return credential, credential != ""
}

// GetInstanceByProviderRef finds an instance by provider type and provider-native ID.
func (m *Manager) GetInstanceByProviderRef(providerType ProviderType, providerID string) (*Instance, bool) {
	return m.instances.findByProviderRef(providerType, providerID)
}

func (m *Manager) initializeWorkerRegistration(instance *Instance, now time.Time) {
	if instance == nil {
		return
	}
	if instance.StartedAt == nil && instance.Status == InstanceStatusRunning {
		startedAt := now
		instance.StartedAt = &startedAt
	}
	if instance.Status == InstanceStatusRunning || instance.Status == InstanceStatusProvisioning || instance.Status == InstanceStatusPending {
		base := now
		if instance.StartedAt != nil {
			base = *instance.StartedAt
		} else if !instance.CreatedAt.IsZero() {
			base = instance.CreatedAt
		}
		deadline := base.Add(m.workerRegistrationTimeout)
		instance.WorkerRegistrationDeadline = &deadline
		if instance.WorkerRegistrationStatus == "" {
			instance.WorkerRegistrationStatus = WorkerRegistrationPending
		}
	}
	m.evaluateWorkerRegistration(instance, now)
}

func (m *Manager) refreshWorkerRegistrationStates(now time.Time) {
	for _, instance := range m.instances.list() {
		m.instances.update(instance.ID, func(stored *Instance) {
			m.evaluateWorkerRegistration(stored, now)
		})
	}
}

func (m *Manager) evaluateWorkerRegistration(instance *Instance, now time.Time) {
	if instance == nil {
		return
	}
	previousCheckAt := instance.LastWorkerRegistrationCheckAt
	instance.LastWorkerRegistrationCheckAt = &now
	instance.WorkerHealthURL = workerHealthURL(instance)
	instance.ProviderNetworkReady = providerNetworkReady(instance)
	instance.ProviderNetworkError = ""

	switch instance.Status {
	case InstanceStatusStopped, InstanceStatusStopping, InstanceStatusTerminated, InstanceStatusTerminating:
		m.clearWorkerRegistration(instance)
		return
	case InstanceStatusError:
		instance.WorkerRegistrationStatus = WorkerRegistrationFailed
		if instance.LastWorkerRegistrationError == "" {
			instance.LastWorkerRegistrationError = firstNonEmpty(instance.ErrorMessage, "Provider reported an instance error")
		}
		return
	}

	if instance.WorkerID != "" {
		if instance.WorkerLastHeartbeatAt == nil {
			instance.WorkerRegistrationStatus = WorkerRegistrationHeartbeatMissing
			instance.LastWorkerRegistrationError = "Worker is linked to the instance, but no heartbeat timestamp is available"
			return
		}
		if instance.WorkerRegistrationStatus == WorkerRegistrationRegisteredUnhealthy && previousCheckAt != nil && previousCheckAt.After(*instance.WorkerLastHeartbeatAt) {
			return
		}
		if m.workerHeartbeatTimeout > 0 && now.Sub(*instance.WorkerLastHeartbeatAt) > m.workerHeartbeatTimeout {
			instance.WorkerRegistrationStatus = WorkerRegistrationRegisteredUnhealthy
			instance.LastWorkerRegistrationError = "Worker is registered, but its last heartbeat is stale"
			return
		}
		instance.WorkerRegistrationStatus = WorkerRegistrationReady
		instance.LastWorkerRegistrationError = ""
		return
	}
	if instance.Status != InstanceStatusRunning {
		instance.WorkerRegistrationStatus = WorkerRegistrationPending
		instance.LastWorkerRegistrationError = ""
		return
	}
	if !instance.ProviderNetworkReady {
		instance.WorkerRegistrationStatus = WorkerRegistrationProviderRunningNoNetwork
		instance.ProviderNetworkError = "Provider reports instance running, but no public/proxy endpoint is available yet"
		instance.LastWorkerRegistrationError = instance.ProviderNetworkError
		return
	}
	if instance.WorkerRegistrationDeadline == nil {
		base := now
		if instance.StartedAt != nil {
			base = *instance.StartedAt
		} else if !instance.CreatedAt.IsZero() {
			base = instance.CreatedAt
		}
		deadline := base.Add(m.workerRegistrationTimeout)
		instance.WorkerRegistrationDeadline = &deadline
	}
	if now.After(*instance.WorkerRegistrationDeadline) {
		instance.WorkerRegistrationStatus = WorkerRegistrationProviderRunningUnregistered
		instance.LastWorkerRegistrationError = "Provider reports instance running, but no gateway worker registered before the deadline"
		return
	}
	instance.WorkerRegistrationStatus = WorkerRegistrationPending
	instance.LastWorkerRegistrationError = ""
}

func (m *Manager) clearWorkerRegistration(instance *Instance) {
	if instance == nil {
		return
	}
	instance.WorkerID = ""
	instance.WorkerRegistrationStatus = ""
	instance.WorkerRegistrationDeadline = nil
	instance.LastWorkerRegistrationError = ""
	instance.LastWorkerRegistrationCheckAt = nil
	instance.WorkerRegisteredAt = nil
	instance.WorkerLastHeartbeatAt = nil
	instance.WorkerHealthURL = ""
	instance.ProviderNetworkReady = false
	instance.ProviderNetworkError = ""
}

func providerNetworkReady(instance *Instance) bool {
	return instance != nil && strings.TrimSpace(instance.PublicIP) != "" && instance.HTTPPort > 0
}

func workerHealthURL(instance *Instance) string {
	if instance == nil {
		return ""
	}
	if strings.TrimSpace(instance.PublicIP) != "" && instance.HTTPPort > 0 {
		return fmt.Sprintf("http://%s:%d/health", strings.TrimSpace(instance.PublicIP), instance.HTTPPort)
	}
	if instance.Provider == ProviderRunPod && strings.TrimSpace(instance.ProviderID) != "" {
		return fmt.Sprintf("https://%s-8081.proxy.runpod.net/health", strings.TrimSpace(instance.ProviderID))
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
