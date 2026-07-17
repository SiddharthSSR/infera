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
	"math"
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
	releaseID                 string
	workerProtocolVersion     string
	workerRegistrationTimeout time.Duration
	workerHeartbeatTimeout    time.Duration
	providerCleanupTimeout    time.Duration
	lifecycleOperationTimeout time.Duration
	lifecycleReconcileLease   time.Duration
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
	ReleaseID                 string                     // Release identity injected into managed workers
	WorkerProtocolVersion     string                     // Gateway/worker control-plane protocol version
	CostDBPath                string                     // Path to SQLite DB for persistent cost tracking (empty = in-memory)
	WorkerRegistrationTimeout time.Duration              // Max time a running provider instance may remain unregistered
	WorkerHeartbeatTimeout    time.Duration              // Max age of a linked worker heartbeat before lifecycle becomes unhealthy
	ProviderCleanupTimeout    time.Duration              // Max time allowed to clean up an orphan after persistence failure
	LifecycleOperationTimeout time.Duration              // Provider-call bound for lifecycle actions
	LifecycleReconcileLease   time.Duration              // Minimum claim age before another replica may recover an interrupted action
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
	cleanupTimeout := config.ProviderCleanupTimeout
	if cleanupTimeout <= 0 {
		cleanupTimeout = 15 * time.Second
	}
	operationTimeout := config.LifecycleOperationTimeout
	if operationTimeout <= 0 {
		operationTimeout = 2 * time.Minute
	}
	reconcileLease := config.LifecycleReconcileLease
	if reconcileLease <= operationTimeout {
		reconcileLease = 2 * operationTimeout
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
		releaseID:                 strings.TrimSpace(config.ReleaseID),
		workerProtocolVersion:     strings.TrimSpace(config.WorkerProtocolVersion),
		workerRegistrationTimeout: timeout,
		workerHeartbeatTimeout:    heartbeatTimeout,
		providerCleanupTimeout:    cleanupTimeout,
		lifecycleOperationTimeout: operationTimeout,
		lifecycleReconcileLease:   reconcileLease,
		now:                       now,
	}, nil
}

// Close releases provider manager resources.
func (m *Manager) Close() error {
	var storeErr error
	if store, ok := m.instances.(credentialInstanceStore); ok {
		storeErr = store.Close()
	}
	return errors.Join(m.costs.Close(), storeErr)
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
	req.ReleaseID = strings.TrimSpace(m.releaseID)
	req.ProtocolVersion = strings.TrimSpace(m.workerProtocolVersion)
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

	reusable, err := m.findReusableStoppedInstance(providerType, req)
	if err != nil {
		return nil, err
	}
	if reusable != nil {
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
	if err := m.putTrackedInstance(instance); err != nil {
		// A provider resource without its credential record cannot safely join
		// the control plane. Best-effort cleanup avoids leaving a paid orphan.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), m.providerCleanupTimeout)
		cleanupErr := provider.Terminate(cleanupCtx, instance.ProviderID)
		cancel()
		persistErr := fmt.Errorf("persist managed instance: %w", err)
		if cleanupErr != nil {
			return nil, errors.Join(persistErr, fmt.Errorf("terminate orphaned provider instance: %w", cleanupErr))
		}
		return nil, persistErr
	}

	// Start cost tracking
	m.costs.StartTracking(instance)

	return instance, nil
}

var ErrLifecycleConflict = errors.New("provider instance lifecycle changed concurrently")

func lifecycleTransitionInProgress(status InstanceStatus) bool {
	switch status {
	case InstanceStatusStarting, InstanceStatusStopping, InstanceStatusTerminating:
		return true
	default:
		return false
	}
}

func lifecycleActionAllowed(action, status InstanceStatus) bool {
	switch action {
	case InstanceStatusStarting:
		return status == InstanceStatusStopped
	case InstanceStatusStopping:
		return status == InstanceStatusPending || status == InstanceStatusProvisioning || status == InstanceStatusRunning
	case InstanceStatusTerminating:
		return status != InstanceStatusTerminated && !lifecycleTransitionInProgress(status)
	default:
		return false
	}
}

func providerInstanceNotFound(err error) bool {
	var providerErr *ProviderError
	return errors.As(err, &providerErr) && providerErr.Code == ProviderErrorNotFound
}

func (m *Manager) claimLifecycle(instance *Instance, action InstanceStatus) (int64, error) {
	if !lifecycleActionAllowed(action, instance.Status) {
		return 0, fmt.Errorf("%w: cannot move instance %s from %s to %s", ErrLifecycleConflict, instance.ID, instance.Status, action)
	}
	updated, err := m.instances.updateIfLifecycleVersion(instance.ID, instance.LifecycleVersion, func(stored *Instance) {
		stored.Status = action
		now := m.now()
		stored.LifecycleClaimedAt = &now
	})
	if err != nil {
		return 0, err
	}
	if !updated {
		return 0, fmt.Errorf("%w: instance %s", ErrLifecycleConflict, instance.ID)
	}
	return instance.LifecycleVersion + 1, nil
}

func (m *Manager) finalizeLifecycle(instanceID string, claimedVersion int64, action string, apply func(*Instance)) error {
	updated, err := m.instances.updateIfLifecycleVersion(instanceID, claimedVersion, func(instance *Instance) {
		apply(instance)
		instance.LifecycleClaimedAt = nil
	})
	if err != nil {
		return fmt.Errorf("provider %s succeeded but durable lifecycle finalization failed; instance remains inactive: %w", action, err)
	}
	if !updated {
		return fmt.Errorf("provider %s succeeded but lifecycle finalization lost its claim: %w", action, ErrLifecycleConflict)
	}
	return nil
}

func (m *Manager) finalizeProviderMissing(instanceID string, claimedVersion int64, action string) error {
	m.costs.StopTracking(instanceID)
	return m.finalizeLifecycle(instanceID, claimedVersion, action, func(instance *Instance) {
		instance.Status = InstanceStatusTerminated
		stoppedAt := m.now()
		instance.StoppedAt = &stoppedAt
		m.clearWorkerRegistration(instance)
	})
}

func (m *Manager) reconcileTransitionalLifecycle(ctx context.Context, instance *Instance) error {
	now := m.now()
	if instance.LifecycleClaimedAt != nil && now.Sub(*instance.LifecycleClaimedAt) < m.lifecycleReconcileLease {
		return nil
	}
	provider, err := m.resolveProvider(instance.WorkspaceID, instance.Provider)
	if err != nil {
		return nil
	}
	claimedVersion := instance.LifecycleVersion + 1
	claimed, err := m.instances.updateIfLifecycleVersion(instance.ID, instance.LifecycleVersion, func(stored *Instance) {
		stored.LifecycleClaimedAt = &now
	})
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	actionCtx, cancel := context.WithTimeout(ctx, m.lifecycleOperationTimeout)
	defer cancel()
	observed, observeErr := provider.GetInstance(actionCtx, instance.ProviderID)
	if providerInstanceNotFound(observeErr) || (observeErr == nil && observed != nil && observed.Status == InstanceStatusTerminated) {
		return m.finalizeProviderMissing(instance.ID, claimedVersion, "lifecycle recovery")
	}
	if observeErr != nil || observed == nil {
		// Without a provider observation, retrying an action could duplicate a
		// request that succeeded after its response was lost. Retain the inactive
		// claim for a later bounded reconciliation attempt.
		return nil
	}

	switch instance.Status {
	case InstanceStatusStarting:
		switch observed.Status {
		case InstanceStatusPending, InstanceStatusProvisioning, InstanceStatusRunning:
			m.costs.StartTracking(instance)
			return m.finalizeLifecycle(instance.ID, claimedVersion, "start recovery observation", func(stored *Instance) {
				stored.Status = InstanceStatusProvisioning
				stored.PublicIP = observed.PublicIP
				stored.HTTPPort = observed.HTTPPort
				stored.SSHPort = observed.SSHPort
				startedAt := m.now()
				stored.StartedAt = &startedAt
				stored.StoppedAt = nil
				stored.ErrorMessage = ""
				m.initializeWorkerRegistration(stored, startedAt)
			})
		case InstanceStatusStopped:
			// The provider confirms the original start did not take effect.
		default:
			return nil
		}
		if starter, ok := provider.(InstanceStarter); ok {
			err = starter.StartWithInstance(actionCtx, instance)
		} else {
			err = provider.Start(actionCtx, instance.ProviderID)
		}
		if err != nil {
			if providerInstanceNotFound(err) {
				return m.finalizeProviderMissing(instance.ID, claimedVersion, "start recovery")
			}
			return nil
		}
		m.costs.StartTracking(instance)
		return m.finalizeLifecycle(instance.ID, claimedVersion, "start recovery", func(stored *Instance) {
			stored.Status = InstanceStatusProvisioning
			startedAt := m.now()
			stored.StartedAt = &startedAt
			stored.StoppedAt = nil
			stored.ErrorMessage = ""
			m.initializeWorkerRegistration(stored, startedAt)
		})
	case InstanceStatusStopping:
		switch observed.Status {
		case InstanceStatusStopped:
			m.costs.StopTracking(instance.ID)
			return m.finalizeLifecycle(instance.ID, claimedVersion, "stop recovery observation", func(stored *Instance) {
				stored.Status = InstanceStatusStopped
				stoppedAt := m.now()
				stored.StoppedAt = &stoppedAt
				m.clearWorkerRegistration(stored)
			})
		case InstanceStatusPending, InstanceStatusProvisioning, InstanceStatusRunning:
			// The provider confirms the instance is still active, so retry stop.
		default:
			return nil
		}
		err = provider.Stop(actionCtx, instance.ProviderID)
		if err != nil && !providerInstanceNotFound(err) {
			return nil
		}
		m.costs.StopTracking(instance.ID)
		return m.finalizeLifecycle(instance.ID, claimedVersion, "stop recovery", func(stored *Instance) {
			stored.Status = InstanceStatusStopped
			if providerInstanceNotFound(err) {
				stored.Status = InstanceStatusTerminated
			}
			stoppedAt := m.now()
			stored.StoppedAt = &stoppedAt
			m.clearWorkerRegistration(stored)
		})
	case InstanceStatusTerminating:
		if observed.Status == InstanceStatusStarting || observed.Status == InstanceStatusTerminating {
			return nil
		}
		err = provider.Terminate(actionCtx, instance.ProviderID)
		if err != nil && !providerInstanceNotFound(err) {
			return nil
		}
		m.costs.StopTracking(instance.ID)
		return m.finalizeLifecycle(instance.ID, claimedVersion, "termination recovery", func(stored *Instance) {
			stored.Status = InstanceStatusTerminated
			stoppedAt := m.now()
			stored.StoppedAt = &stoppedAt
			m.clearWorkerRegistration(stored)
		})
	default:
		return nil
	}
}

// AuthenticateWorkerToken resolves a provisioned instance from its unique worker credential.
func (m *Manager) AuthenticateWorkerToken(token string) (*Instance, bool, error) {
	tokenHash := sha256.Sum256([]byte(strings.TrimSpace(token)))
	if strings.TrimSpace(token) == "" {
		return nil, false, nil
	}
	if store, ok := m.instances.(credentialInstanceStore); ok {
		return store.authenticateWorkerTokenHash(tokenHash)
	}
	instances, err := m.instances.list()
	if err != nil {
		return nil, false, err
	}
	for _, instance := range instances {
		if workerCredentialActive(instance.Status) && subtle.ConstantTimeCompare(tokenHash[:], instance.WorkerCredentialHash[:]) == 1 {
			return instance, true, nil
		}
	}
	return nil, false, nil
}

// AuthorizeWorkerBinding prevents one deployment credential from claiming another deployment's worker ID.
func (m *Manager) AuthorizeWorkerBinding(instanceID, workerID string) error {
	if store, ok := m.instances.(credentialInstanceStore); ok {
		return store.authorizeWorkerBinding(instanceID, workerID)
	}
	m.workerBindingMu.Lock()
	defer m.workerBindingMu.Unlock()
	return m.authorizeWorkerBinding(instanceID, workerID)
}

func (m *Manager) authorizeWorkerBinding(instanceID, workerID string) error {
	instance, ok, err := m.getTrackedInstance(instanceID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("instance %s not found", instanceID)
	}
	if instance.WorkerID != "" && instance.WorkerID != workerID {
		return fmt.Errorf("%w: instance is already bound to worker %s", ErrWorkerIdentityConflict, instance.WorkerID)
	}
	owner, found, err := m.GetInstanceByWorkerWithError(workerID)
	if err != nil {
		return err
	}
	if found && owner.ID != instanceID {
		return fmt.Errorf("%w: worker %s is already bound to another instance", ErrWorkerIdentityConflict, workerID)
	}
	return nil
}

// Terminate destroys an instance.
func (m *Manager) Terminate(ctx context.Context, instanceID string) error {
	instance, exists, err := m.getTrackedInstance(instanceID)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("instance %s not found", instanceID)
	}

	provider, err := m.resolveProvider(instance.WorkspaceID, instance.Provider)
	if err != nil {
		return err
	}

	claimedVersion, err := m.claimLifecycle(instance, InstanceStatusTerminating)
	if err != nil {
		return err
	}
	actionCtx, cancel := context.WithTimeout(ctx, m.lifecycleOperationTimeout)
	defer cancel()
	if err := provider.Terminate(actionCtx, instance.ProviderID); err != nil {
		if providerInstanceNotFound(err) {
			return m.finalizeProviderMissing(instanceID, claimedVersion, "termination")
		}
		// Termination errors can be ambiguous: the provider may have completed
		// server-side after the client timed out. Keep credentials inactive and
		// let leased reconciliation retry the idempotent operation.
		return err
	}

	// Stop cost tracking
	m.costs.StopTracking(instanceID)

	// Update status
	err = m.finalizeLifecycle(instanceID, claimedVersion, "termination", func(instance *Instance) {
		instance.Status = InstanceStatusTerminated
		now := m.now()
		instance.StoppedAt = &now
		m.clearWorkerRegistration(instance)
	})
	return err
}

// Start starts a stopped instance.
func (m *Manager) Start(ctx context.Context, instanceID string) error {
	instance, exists, err := m.getTrackedInstance(instanceID)
	if err != nil {
		return err
	}
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

	claimedVersion, err := m.claimLifecycle(instance, InstanceStatusStarting)
	if err != nil {
		return err
	}
	actionCtx, cancel := context.WithTimeout(ctx, m.lifecycleOperationTimeout)
	defer cancel()
	if starter, ok := provider.(InstanceStarter); ok {
		if err := starter.StartWithInstance(actionCtx, instance); err != nil {
			if providerInstanceNotFound(err) {
				return m.finalizeProviderMissing(instanceID, claimedVersion, "start")
			}
			return err
		}
	} else {
		if err := provider.Start(actionCtx, instance.ProviderID); err != nil {
			if providerInstanceNotFound(err) {
				return m.finalizeProviderMissing(instanceID, claimedVersion, "start")
			}
			return err
		}
	}

	// Resume cost tracking
	m.costs.StartTracking(instance)

	err = m.finalizeLifecycle(instanceID, claimedVersion, "start", func(instance *Instance) {
		now := m.now()
		instance.Status = InstanceStatusProvisioning
		instance.StartedAt = &now
		instance.StoppedAt = nil
		instance.ErrorMessage = ""
		m.initializeWorkerRegistration(instance, now)
	})
	return err
}

// Stop stops a running instance.
func (m *Manager) Stop(ctx context.Context, instanceID string) error {
	instance, exists, err := m.getTrackedInstance(instanceID)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("instance %s not found", instanceID)
	}

	provider, err := m.resolveProvider(instance.WorkspaceID, instance.Provider)
	if err != nil {
		return err
	}

	claimedVersion, err := m.claimLifecycle(instance, InstanceStatusStopping)
	if err != nil {
		return err
	}
	actionCtx, cancel := context.WithTimeout(ctx, m.lifecycleOperationTimeout)
	defer cancel()
	if err := provider.Stop(actionCtx, instance.ProviderID); err != nil {
		if providerInstanceNotFound(err) {
			return m.finalizeProviderMissing(instanceID, claimedVersion, "stop")
		}
		// A timeout or transport failure does not prove the provider kept the
		// instance running. Preserve the inactive claim for reconciliation.
		return err
	}

	// Stop cost tracking
	m.costs.StopTracking(instanceID)

	err = m.finalizeLifecycle(instanceID, claimedVersion, "stop", func(instance *Instance) {
		now := m.now()
		instance.Status = InstanceStatusStopped
		instance.StoppedAt = &now
		m.clearWorkerRegistration(instance)
	})
	return err
}

func (m *Manager) getTrackedInstance(instanceID string) (*Instance, bool, error) {
	return m.instances.get(instanceID)
}

func (m *Manager) putTrackedInstance(instance *Instance) error {
	return m.instances.put(instance)
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
func (m *Manager) GetInstanceWithError(instanceID string) (*Instance, bool, error) {
	if err := m.refreshWorkerRegistrationStates(m.now()); err != nil {
		return nil, false, err
	}
	instance, exists, err := m.getTrackedInstance(instanceID)
	if err != nil {
		return nil, false, err
	}
	if !exists {
		return nil, false, nil
	}
	return cloneInstance(instance), true, nil
}

// GetInstance is the in-memory-compatible read API. Production callers must
// use GetInstanceWithError so shared-store failures cannot become not-found.
func (m *Manager) GetInstance(instanceID string) (*Instance, bool) {
	instance, found, _ := m.GetInstanceWithError(instanceID)
	return instance, found
}

// ListInstances returns all tracked instances.
func (m *Manager) ListInstancesWithError() ([]*Instance, error) {
	if err := m.refreshWorkerRegistrationStates(m.now()); err != nil {
		return nil, err
	}
	return m.instances.list()
}

func (m *Manager) ListInstances() []*Instance {
	instances, _ := m.ListInstancesWithError()
	return instances
}

// ListInstancesByProvider returns instances for a specific provider.
func (m *Manager) ListInstancesByProviderWithError(providerType ProviderType) ([]*Instance, error) {
	if err := m.refreshWorkerRegistrationStates(m.now()); err != nil {
		return nil, err
	}
	return m.instances.listByProvider(providerType)
}

func (m *Manager) ListInstancesByProvider(providerType ProviderType) []*Instance {
	instances, _ := m.ListInstancesByProviderWithError(providerType)
	return instances
}

func (m *Manager) ListInstancesByWorkspaceWithError(workspaceID string) ([]*Instance, error) {
	if err := m.refreshWorkerRegistrationStatesByWorkspace(workspaceID, m.now()); err != nil {
		return nil, err
	}
	return m.instances.listByWorkspace(workspaceID)
}

func (m *Manager) ListInstancesByWorkspace(workspaceID string) []*Instance {
	instances, _ := m.ListInstancesByWorkspaceWithError(workspaceID)
	return instances
}

func (m *Manager) findReusableStoppedInstance(providerType ProviderType, req *ProvisionRequest) (*Instance, error) {
	return m.instances.findReusableStopped(providerType, req)
}

// RefreshInstances updates instance status from providers.
func (m *Manager) RefreshInstances(ctx context.Context) error {
	instances, err := m.instances.list()
	if err != nil {
		return err
	}

	for _, inst := range instances {
		if lifecycleTransitionInProgress(inst.Status) {
			if err := m.reconcileTransitionalLifecycle(ctx, inst); err != nil {
				return err
			}
			continue
		}
		provider, err := m.resolveProvider(inst.WorkspaceID, inst.Provider)
		if err != nil {
			continue
		}
		refreshed, err := provider.GetInstance(ctx, inst.ProviderID)
		if err != nil {
			var providerErr *ProviderError
			if errors.As(err, &providerErr) && providerErr.Code == ProviderErrorNotFound {
				updated, updateErr := m.instances.updateIfLifecycleVersion(inst.ID, inst.LifecycleVersion, func(existing *Instance) {
					existing.Status = InstanceStatusTerminated
					existing.ErrorMessage = "Provider no longer reports this instance"
					now := m.now()
					if existing.StoppedAt == nil {
						existing.StoppedAt = &now
					}
				})
				if updateErr != nil {
					return updateErr
				}
				// A concurrent lifecycle mutation won the version fence. Its newer
				// state is authoritative over this stale provider snapshot.
				if !updated {
					continue
				}
				m.costs.StopTracking(inst.ID)
			}
			continue
		}

		updated, updateErr := m.instances.updateIfLifecycleVersion(inst.ID, inst.LifecycleVersion, func(existing *Instance) {
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
		if updateErr != nil {
			return updateErr
		}
		if !updated {
			continue
		}
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
	if store, ok := m.instances.(credentialInstanceStore); ok {
		return store.linkWorker(instanceID, workerID, m.now())
	}
	m.workerBindingMu.Lock()
	defer m.workerBindingMu.Unlock()
	if err := m.authorizeWorkerBinding(instanceID, workerID); err != nil {
		return err
	}
	updated, err := m.instances.update(instanceID, func(instance *Instance) {
		instance.WorkerID = workerID
		now := m.now()
		if instance.WorkerRegisteredAt == nil {
			instance.WorkerRegisteredAt = &now
		}
		instance.WorkerLastHeartbeatAt = &now
		instance.LastWorkerRegistrationCheckAt = &now
		instance.WorkerRegistrationStatus = WorkerRegistrationReady
		instance.LastWorkerRegistrationError = ""
	})
	if err != nil {
		return err
	}
	if !updated {
		return fmt.Errorf("instance %s not found", instanceID)
	}
	return nil
}

// RecordWorkerUnhealthy marks a linked worker unhealthy until a later heartbeat.
func (m *Manager) RecordWorkerUnhealthyWithError(workerID string, observedAt time.Time) (bool, error) {
	instance, exists, err := m.instances.findByWorker(workerID)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
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

func (m *Manager) RecordWorkerUnhealthy(workerID string, observedAt time.Time) bool {
	updated, _ := m.RecordWorkerUnhealthyWithError(workerID, observedAt)
	return updated
}

// RecordWorkerHeartbeat updates the lifecycle timestamps for a linked worker.
func (m *Manager) RecordWorkerHeartbeatWithError(workerID string, heartbeatAt time.Time) (bool, error) {
	instance, exists, err := m.instances.findByWorker(workerID)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
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

func (m *Manager) RecordWorkerHeartbeat(workerID string, heartbeatAt time.Time) bool {
	updated, _ := m.RecordWorkerHeartbeatWithError(workerID, heartbeatAt)
	return updated
}

// UnlinkWorker removes worker association from an instance.
func (m *Manager) UnlinkWorker(instanceID string) error {
	_, err := m.instances.update(instanceID, func(instance *Instance) {
		instance.WorkerID = ""
		instance.WorkerRegisteredAt = nil
		instance.WorkerLastHeartbeatAt = nil
		m.evaluateWorkerRegistration(instance, m.now())
	})
	return err
}

// GetInstanceByWorker finds an instance by its linked worker ID.
func (m *Manager) GetInstanceByWorkerWithError(workerID string) (*Instance, bool, error) {
	return m.instances.findByWorker(workerID)
}

func (m *Manager) GetInstanceByWorker(workerID string) (*Instance, bool) {
	instance, found, _ := m.GetInstanceByWorkerWithError(workerID)
	return instance, found
}

// GetPriceSnapshotForWorker captures the provisioned instance price that was
// recorded when the worker's instance was created. Provider refreshes may
// affect future instances, but never mutate an audit row that has persisted
// this snapshot.
func (m *Manager) GetPriceSnapshotForWorkerWithError(workerID string) (PriceSnapshot, bool, error) {
	instance, found, err := m.instances.findByWorker(workerID)
	if err != nil {
		return PriceSnapshot{}, false, err
	}
	if !found || instance == nil || !validHourlyPrice(instance.CostPerHour) {
		return PriceSnapshot{}, false, nil
	}
	amountNano := int64(math.Round(instance.CostPerHour * 1_000_000_000))
	if amountNano <= 0 {
		return PriceSnapshot{}, false, nil
	}
	capturedAt := instance.CreatedAt.UTC()
	if capturedAt.IsZero() {
		capturedAt = m.now().UTC()
	}
	return PriceSnapshot{
		Version:    PriceSnapshotVersionV1,
		Provider:   instance.Provider,
		InstanceID: instance.ID,
		AmountNano: amountNano,
		Currency:   PriceCurrencyUSD,
		TimeUnit:   PriceTimeUnitHour,
		CapturedAt: capturedAt,
	}, true, nil
}

func (m *Manager) GetPriceSnapshotForWorker(workerID string) (PriceSnapshot, bool) {
	snapshot, found, _ := m.GetPriceSnapshotForWorkerWithError(workerID)
	return snapshot, found
}

func validHourlyPrice(price float64) bool {
	if math.IsNaN(price) || math.IsInf(price, 0) || price <= 0 {
		return false
	}
	scaled := price * 1_000_000_000
	// float64(math.MaxInt64) rounds up to 1<<63, which cannot be converted
	// safely to int64. Reject that boundary and anything larger.
	return !math.IsInf(scaled, 0) && scaled < float64(math.MaxInt64)
}

// WorkerCredentialForWorker returns the deployment-bound credential for a
// linked worker. It intentionally fails closed for unknown or legacy records.
func (m *Manager) WorkerCredentialForWorker(workerID string) (string, bool, error) {
	if store, ok := m.instances.(credentialInstanceStore); ok {
		return store.workerCredentialForWorker(workerID)
	}
	instance, found, err := m.instances.findByWorker(workerID)
	if err != nil {
		return "", false, err
	}
	if !found {
		return "", false, nil
	}
	credential := strings.TrimSpace(instance.WorkerCredential)
	if !workerCredentialActive(instance.Status) {
		return "", false, nil
	}
	return credential, credential != "", nil
}

// GetInstanceByProviderRef finds an instance by provider type and provider-native ID.
func (m *Manager) GetInstanceByProviderRefWithError(providerType ProviderType, providerID string) (*Instance, bool, error) {
	return m.instances.findByProviderRef(providerType, providerID)
}

func (m *Manager) GetInstanceByProviderRef(providerType ProviderType, providerID string) (*Instance, bool) {
	instance, found, _ := m.GetInstanceByProviderRefWithError(providerType, providerID)
	return instance, found
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

func (m *Manager) refreshWorkerRegistrationStates(now time.Time) error {
	instances, err := m.instances.list()
	if err != nil {
		return err
	}
	return m.refreshWorkerRegistrationStateRows(instances, now)
}

func (m *Manager) refreshWorkerRegistrationStatesByWorkspace(workspaceID string, now time.Time) error {
	instances, err := m.instances.listByWorkspace(workspaceID)
	if err != nil {
		return err
	}
	return m.refreshWorkerRegistrationStateRows(instances, now)
}

func (m *Manager) refreshWorkerRegistrationStateRows(instances []*Instance, now time.Time) error {
	for _, instance := range instances {
		if _, err := m.instances.update(instance.ID, func(stored *Instance) {
			m.evaluateWorkerRegistration(stored, now)
		}); err != nil {
			return err
		}
	}
	return nil
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
	case InstanceStatusStarting, InstanceStatusStopped, InstanceStatusStopping, InstanceStatusTerminated, InstanceStatusTerminating:
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
	if instance == nil {
		return false
	}
	if instance.Provider == ProviderRunPod {
		return strings.TrimSpace(instance.ProviderID) != ""
	}
	return strings.TrimSpace(instance.PublicIP) != "" && instance.HTTPPort > 0
}

func workerHealthURL(instance *Instance) string {
	if instance == nil {
		return ""
	}
	providerID := strings.TrimSpace(instance.ProviderID)
	publicIP := strings.TrimSpace(instance.PublicIP)
	if instance.Provider == ProviderRunPod && providerID != "" {
		return fmt.Sprintf("https://%s-8081.proxy.runpod.net/health", providerID)
	}
	if publicIP != "" && instance.HTTPPort > 0 {
		return fmt.Sprintf("http://%s:%d/health", publicIP, instance.HTTPPort)
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
