package providers

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
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

type workspaceConfiguredProviderResolver struct {
	resolve func(workspaceID string, providerType ProviderType) (*ProviderConfig, error)
}

func (r workspaceConfiguredProviderResolver) ResolveProviderConfig(workspaceID string, providerType ProviderType) (*ProviderConfig, error) {
	return r.resolve(workspaceID, providerType)
}

type instanceAwareStartProvider struct {
	*mockTestProvider
	startedWithInstance []string
}

type delayedRefreshProvider struct {
	*mockTestProvider
	snapshotTaken chan struct{}
	release       chan struct{}
}

type lifecycleFaultStore struct {
	instanceStore
	mu             sync.Mutex
	putErr         error
	updateCalls    int
	failUpdateCall int
	failUpdateErr  error
}

func (s *lifecycleFaultStore) put(instance *Instance) error {
	if s.putErr != nil {
		return s.putErr
	}
	return s.instanceStore.put(instance)
}

func (s *lifecycleFaultStore) updateIfLifecycleVersion(instanceID string, expectedVersion int64, apply func(*Instance)) (bool, error) {
	s.mu.Lock()
	s.updateCalls++
	call := s.updateCalls
	s.mu.Unlock()
	if call == s.failUpdateCall {
		return false, s.failUpdateErr
	}
	return s.instanceStore.updateIfLifecycleVersion(instanceID, expectedVersion, apply)
}

type blockingLifecycleProvider struct {
	*mockTestProvider
	startEntered     chan struct{}
	startRelease     chan struct{}
	stopEntered      chan struct{}
	stopRelease      chan struct{}
	terminateEntered chan struct{}
	terminateRelease chan struct{}
	stopCalls        atomic.Int32
	startCalls       atomic.Int32
	terminateCalls   atomic.Int32
	stopErr          error
	startErr         error
	terminateErr     error
}

func (p *blockingLifecycleProvider) Start(ctx context.Context, instanceID string) error {
	p.startCalls.Add(1)
	if p.startEntered != nil {
		p.startEntered <- struct{}{}
	}
	if p.startRelease != nil {
		select {
		case <-p.startRelease:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if p.startErr != nil {
		return p.startErr
	}
	return p.mockTestProvider.Start(ctx, instanceID)
}

func (p *blockingLifecycleProvider) Stop(ctx context.Context, instanceID string) error {
	p.stopCalls.Add(1)
	if p.stopEntered != nil {
		p.stopEntered <- struct{}{}
	}
	if p.stopRelease != nil {
		select {
		case <-p.stopRelease:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if p.stopErr != nil {
		return p.stopErr
	}
	return p.mockTestProvider.Stop(ctx, instanceID)
}

func (p *blockingLifecycleProvider) Terminate(ctx context.Context, instanceID string) error {
	p.terminateCalls.Add(1)
	if p.terminateEntered != nil {
		p.terminateEntered <- struct{}{}
	}
	if p.terminateRelease != nil {
		select {
		case <-p.terminateRelease:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return p.terminateErr
}

type cleanupObservingProvider struct {
	*mockTestProvider
	terminateErr error
	cleanup      chan cleanupContextObservation
}

type cleanupContextObservation struct {
	err         error
	hasDeadline bool
}

func (p *cleanupObservingProvider) Terminate(ctx context.Context, instanceID string) error {
	_, hasDeadline := ctx.Deadline()
	p.cleanup <- cleanupContextObservation{err: ctx.Err(), hasDeadline: hasDeadline}
	return p.terminateErr
}

func (p *delayedRefreshProvider) GetInstance(ctx context.Context, instanceID string) (*Instance, error) {
	instance := p.findInstance(instanceID)
	if instance == nil {
		return nil, &ProviderError{Code: ProviderErrorNotFound, Message: "not found"}
	}
	snapshot := cloneInstance(instance)
	close(p.snapshotTaken)
	select {
	case <-p.release:
		return snapshot, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func newMockTestProvider() *mockTestProvider {
	return &mockTestProvider{
		instances: make(map[string]*Instance),
	}
}

func newInstanceAwareStartProvider() *instanceAwareStartProvider {
	return &instanceAwareStartProvider{
		mockTestProvider: newMockTestProvider(),
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
		DefaultProvider:       ProviderMock,
		WorkerImage:           "worker:latest",
		GatewayAddress:        "https://inferai.co.in",
		ReleaseID:             "release-1",
		WorkerProtocolVersion: "1",
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
	if provider.lastReq.ReleaseID != "release-1" || provider.lastReq.ProtocolVersion != "1" {
		t.Fatalf("expected rollout identity to be injected, got release=%q protocol=%q", provider.lastReq.ReleaseID, provider.lastReq.ProtocolVersion)
	}
}

func TestManagerPriceSnapshotIsVersionedAndDefensive(t *testing.T) {
	provider := newMockTestProvider()
	mgr := newTestManager(t, ManagerConfig{DefaultProvider: ProviderMock})
	mgr.RegisterProvider(provider)
	instance, err := mgr.Provision(context.Background(), &ProvisionRequest{Name: "priced", GPUType: GPURTX4090})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if err := mgr.LinkWorker(instance.ID, "priced-worker"); err != nil {
		t.Fatalf("LinkWorker: %v", err)
	}
	snapshot, ok := mgr.GetPriceSnapshotForWorker("priced-worker")
	if !ok {
		t.Fatal("expected price snapshot")
	}
	if snapshot.Version != PriceSnapshotVersionV1 || snapshot.Currency != PriceCurrencyUSD || snapshot.TimeUnit != PriceTimeUnitHour || snapshot.AmountNano != 1_000_000_000 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	snapshot.AmountNano = 99
	again, ok := mgr.GetPriceSnapshotForWorker("priced-worker")
	if !ok || again.AmountNano != 1_000_000_000 {
		t.Fatalf("caller mutated stored price: %+v", again)
	}
}

func TestManagerPriceSnapshotRejectsInvalidOrOverflowingPrices(t *testing.T) {
	provider := newMockTestProvider()
	mgr := newTestManager(t, ManagerConfig{DefaultProvider: ProviderMock})
	mgr.RegisterProvider(provider)
	instance, err := mgr.Provision(context.Background(), &ProvisionRequest{Name: "priced", GPUType: GPURTX4090})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if err := mgr.LinkWorker(instance.ID, "priced-worker"); err != nil {
		t.Fatalf("LinkWorker: %v", err)
	}

	invalid := []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1), float64(math.MaxInt64) / 1_000_000_000}
	for _, price := range invalid {
		mgr.instances.update(instance.ID, func(stored *Instance) { stored.CostPerHour = price })
		if snapshot, ok := mgr.GetPriceSnapshotForWorker("priced-worker"); ok {
			t.Fatalf("price %v must be unavailable, got %+v", price, snapshot)
		}
	}
}

func TestManagerPriceSnapshotRoundsToNearestNanoUSD(t *testing.T) {
	provider := newMockTestProvider()
	mgr := newTestManager(t, ManagerConfig{DefaultProvider: ProviderMock})
	mgr.RegisterProvider(provider)
	instance, err := mgr.Provision(context.Background(), &ProvisionRequest{Name: "priced", GPUType: GPURTX4090})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if err := mgr.LinkWorker(instance.ID, "priced-worker"); err != nil {
		t.Fatalf("LinkWorker: %v", err)
	}
	mgr.instances.update(instance.ID, func(stored *Instance) { stored.CostPerHour = 0.4000000006 })
	snapshot, ok := mgr.GetPriceSnapshotForWorker("priced-worker")
	if !ok || snapshot.AmountNano != 400_000_001 {
		t.Fatalf("expected nearest-nano rounding, got %+v, ok=%v", snapshot, ok)
	}
}

func TestManagerSurfacesRunningInstanceWithoutNetwork(t *testing.T) {
	provider := newMockTestProvider()
	mgr := newTestManager(t, ManagerConfig{
		DefaultProvider:           ProviderMock,
		WorkerRegistrationTimeout: 5 * time.Minute,
	})
	mgr.RegisterProvider(provider)

	inst, err := mgr.Provision(context.Background(), &ProvisionRequest{
		Name:    "missing-network",
		GPUType: GPUA100_80,
		Models:  []string{"Qwen/Qwen2.5-7B-Instruct"},
	})
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	if inst.WorkerRegistrationStatus != WorkerRegistrationProviderRunningNoNetwork {
		t.Fatalf("expected provider_running_no_network, got %q", inst.WorkerRegistrationStatus)
	}
	if inst.ProviderNetworkReady {
		t.Fatal("expected provider network to be not ready")
	}
	if inst.ProviderNetworkError == "" || inst.LastWorkerRegistrationError == "" {
		t.Fatalf("expected network and registration errors, got provider=%q registration=%q", inst.ProviderNetworkError, inst.LastWorkerRegistrationError)
	}
	if inst.WorkerRegistrationDeadline == nil {
		t.Fatal("expected worker registration deadline")
	}
}

func TestManagerUsesRunPodProxyForNetworkReadinessAndRegistrationTimeout(t *testing.T) {
	provider := newMockTestProvider()
	mgr := newTestManager(t, ManagerConfig{
		DefaultProvider:           ProviderMock,
		WorkerRegistrationTimeout: 5 * time.Minute,
	})
	mgr.RegisterProvider(provider)

	inst, err := mgr.Provision(context.Background(), &ProvisionRequest{
		Name:    "runpod-proxy",
		GPUType: GPURTX4090,
	})
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}
	old := time.Now().Add(-10 * time.Minute)
	mgr.instances.update(inst.ID, func(stored *Instance) {
		stored.Provider = ProviderRunPod
		stored.ProviderID = "pod-123"
		stored.PublicIP = ""
		stored.HTTPPort = 0
		stored.StartedAt = &old
		stored.CreatedAt = old
		stored.WorkerRegistrationDeadline = nil
		mgr.evaluateWorkerRegistration(stored, time.Now())
	})

	got, ok := mgr.GetInstance(inst.ID)
	if !ok {
		t.Fatalf("expected instance %s", inst.ID)
	}
	if !got.ProviderNetworkReady {
		t.Fatal("expected deterministic RunPod proxy endpoint to be network-ready")
	}
	if got.WorkerHealthURL != "https://pod-123-8081.proxy.runpod.net/health" {
		t.Fatalf("unexpected RunPod worker health URL %q", got.WorkerHealthURL)
	}
	if got.WorkerRegistrationStatus != WorkerRegistrationProviderRunningUnregistered {
		t.Fatalf("expected registration timeout to remain enforced, got %q", got.WorkerRegistrationStatus)
	}
}

func TestWorkerHealthURLPrefersRunPodProxyOverDirectIP(t *testing.T) {
	instance := &Instance{
		Provider:   ProviderRunPod,
		ProviderID: "pod-deterministic",
		PublicIP:   "203.0.113.10",
		HTTPPort:   8081,
	}
	if got := workerHealthURL(instance); got != "https://pod-deterministic-8081.proxy.runpod.net/health" {
		t.Fatalf("RunPod health URL did not use deterministic proxy: %q", got)
	}
	if !providerNetworkReady(instance) {
		t.Fatal("expected RunPod proxy endpoint to be network-ready")
	}
}

func TestManagerSurfacesRunningInstanceRegistrationTimeout(t *testing.T) {
	provider := newMockTestProvider()
	mgr := newTestManager(t, ManagerConfig{
		DefaultProvider:           ProviderMock,
		WorkerRegistrationTimeout: 5 * time.Minute,
	})
	mgr.RegisterProvider(provider)

	inst, err := mgr.Provision(context.Background(), &ProvisionRequest{
		Name:    "registration-timeout",
		GPUType: GPUA100_80,
	})
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}
	old := time.Now().Add(-10 * time.Minute)
	mgr.instances.update(inst.ID, func(stored *Instance) {
		stored.StartedAt = &old
		stored.CreatedAt = old
		stored.PublicIP = "203.0.113.10"
		stored.HTTPPort = 8081
		stored.WorkerRegistrationDeadline = nil
		mgr.evaluateWorkerRegistration(stored, time.Now())
	})

	got, ok := mgr.GetInstance(inst.ID)
	if !ok {
		t.Fatalf("expected instance %s", inst.ID)
	}
	if got.WorkerRegistrationStatus != WorkerRegistrationProviderRunningUnregistered {
		t.Fatalf("expected provider_running_worker_unregistered, got %q", got.WorkerRegistrationStatus)
	}
	if !got.ProviderNetworkReady {
		t.Fatal("expected provider network to be ready")
	}
	if got.WorkerHealthURL != "http://203.0.113.10:8081/health" {
		t.Fatalf("unexpected worker health URL %q", got.WorkerHealthURL)
	}
	if got.LastWorkerRegistrationError == "" {
		t.Fatal("expected registration timeout error")
	}
}

func TestManagerLinkWorkerMarksInstanceReady(t *testing.T) {
	provider := newMockTestProvider()
	mgr := newTestManager(t, ManagerConfig{DefaultProvider: ProviderMock})
	mgr.RegisterProvider(provider)

	inst, err := mgr.Provision(context.Background(), &ProvisionRequest{
		Name:    "ready-worker",
		GPUType: GPUA100_80,
	})
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	if err := mgr.LinkWorker(inst.ID, "worker-123"); err != nil {
		t.Fatalf("LinkWorker failed: %v", err)
	}
	if !mgr.RecordWorkerHeartbeat("worker-123", time.Now()) {
		t.Fatal("expected heartbeat to be recorded")
	}

	got, ok := mgr.GetInstance(inst.ID)
	if !ok {
		t.Fatalf("expected instance %s", inst.ID)
	}
	if got.WorkerRegistrationStatus != WorkerRegistrationReady {
		t.Fatalf("expected ready, got %q", got.WorkerRegistrationStatus)
	}
	if got.WorkerRegisteredAt == nil || got.WorkerLastHeartbeatAt == nil {
		t.Fatalf("expected registration and heartbeat timestamps, got registered=%v heartbeat=%v", got.WorkerRegisteredAt, got.WorkerLastHeartbeatAt)
	}
	if got.LastWorkerRegistrationError != "" {
		t.Fatalf("expected no registration error, got %q", got.LastWorkerRegistrationError)
	}
}

func TestManagerMarksStaleWorkerHeartbeatUnhealthy(t *testing.T) {
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	provider := newMockTestProvider()
	mgr := newTestManager(t, ManagerConfig{
		DefaultProvider:        ProviderMock,
		WorkerHeartbeatTimeout: time.Minute,
		Now:                    func() time.Time { return now },
	})
	mgr.RegisterProvider(provider)

	inst, err := mgr.Provision(context.Background(), &ProvisionRequest{
		Name:    "stale-heartbeat",
		GPUType: GPUA100_80,
		Models:  []string{"Qwen/Qwen2.5-7B-Instruct"},
	})
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}
	if err := mgr.LinkWorker(inst.ID, "worker-stale"); err != nil {
		t.Fatalf("LinkWorker failed: %v", err)
	}
	if !mgr.RecordWorkerHeartbeat("worker-stale", now) {
		t.Fatal("expected heartbeat to be recorded")
	}

	now = now.Add(2 * time.Minute)
	got, ok := mgr.GetInstance(inst.ID)
	if !ok {
		t.Fatalf("expected instance %s", inst.ID)
	}
	if got.WorkerRegistrationStatus != WorkerRegistrationRegisteredUnhealthy {
		t.Fatalf("expected registered_unhealthy, got %q", got.WorkerRegistrationStatus)
	}
	if got.LastWorkerRegistrationError == "" || got.WorkerLastHeartbeatAt == nil {
		t.Fatalf("expected stale heartbeat diagnostics, error=%q heartbeat=%v", got.LastWorkerRegistrationError, got.WorkerLastHeartbeatAt)
	}

	if !mgr.RecordWorkerHeartbeat("worker-stale", now) {
		t.Fatal("expected fresh heartbeat to be recorded")
	}
	got, _ = mgr.GetInstance(inst.ID)
	if got.WorkerRegistrationStatus != WorkerRegistrationReady {
		t.Fatalf("expected fresh heartbeat to restore ready, got %q", got.WorkerRegistrationStatus)
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

func (p *instanceAwareStartProvider) StartWithInstance(ctx context.Context, instance *Instance) error {
	p.startedWithInstance = append(p.startedWithInstance, instance.ID)
	return p.mockTestProvider.Start(ctx, instance.ProviderID)
}

func TestNewManager(t *testing.T) {
	config := ManagerConfig{
		DefaultProvider: ProviderMock,
		WorkerImage:     "test-image:latest",
		WorkerImages: map[InferenceEngine]string{
			EngineSGLang: "sglang-worker:v1",
		},
		GatewayAddress: "localhost:8080",
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
	if got := mgr.workerImages[EngineSGLang]; got != "sglang-worker:v1" {
		t.Errorf("expected cloned engine-specific image, got %q", got)
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

	t.Run("GetInstance returns clone", func(t *testing.T) {
		inst, exists := mgr.GetInstance(instance.ID)
		if !exists {
			t.Fatal("instance should exist")
		}
		inst.Name = "mutated"

		reloaded, exists := mgr.GetInstance(instance.ID)
		if !exists {
			t.Fatal("instance should still exist")
		}
		if reloaded.Name != "test-instance" {
			t.Fatalf("expected tracked instance to remain unchanged, got %q", reloaded.Name)
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

func TestManagerProvisionUsesEngineSpecificWorkerImage(t *testing.T) {
	mgr := newTestManager(t, ManagerConfig{
		DefaultProvider: ProviderMock,
		WorkerImage:     "default-worker:v1",
		WorkerImages: map[InferenceEngine]string{
			EngineSGLang: "sglang-worker:v2",
		},
	})
	provider := newMockTestProvider()
	mgr.RegisterProvider(provider)

	req := &ProvisionRequest{
		Name:     "sglang-test",
		Provider: ProviderMock,
		Engine:   EngineSGLang,
		GPUType:  GPURTX4090,
	}

	if _, err := mgr.Provision(context.Background(), req); err != nil {
		t.Fatalf("Provision failed: %v", err)
	}
	if provider.lastReq == nil {
		t.Fatal("expected provider to receive provision request")
	}
	if provider.lastReq.DockerImage != "sglang-worker:v2" {
		t.Fatalf("expected engine-specific image, got %q", provider.lastReq.DockerImage)
	}
}

func TestManagerProvisionFallsBackToDefaultWorkerImage(t *testing.T) {
	mgr := newTestManager(t, ManagerConfig{
		DefaultProvider: ProviderMock,
		WorkerImage:     "default-worker:v1",
		WorkerImages: map[InferenceEngine]string{
			EngineSGLang: "sglang-worker:v2",
		},
	})
	provider := newMockTestProvider()
	mgr.RegisterProvider(provider)

	req := &ProvisionRequest{
		Name:     "vllm-test",
		Provider: ProviderMock,
		Engine:   EngineVLLM,
		GPUType:  GPURTX4090,
	}

	if _, err := mgr.Provision(context.Background(), req); err != nil {
		t.Fatalf("Provision failed: %v", err)
	}
	if provider.lastReq == nil {
		t.Fatal("expected provider to receive provision request")
	}
	if provider.lastReq.DockerImage != "default-worker:v1" {
		t.Fatalf("expected default worker image, got %q", provider.lastReq.DockerImage)
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

func TestManagerWorkspaceScopedProviderResolutionUsesTypedResolver(t *testing.T) {
	RegisterProvider(ProviderRunPod, func(config ProviderConfig) (Provider, error) {
		return &workspaceConfiguredProvider{apiKey: config.APIKey}, nil
	})

	mgr := newTestManager(t, ManagerConfig{DefaultProvider: ProviderMock})
	mgr.RegisterProvider(newMockTestProvider())
	mgr.SetWorkspaceProviderConfigSource(workspaceConfiguredProviderResolver{
		resolve: func(workspaceID string, providerType ProviderType) (*ProviderConfig, error) {
			if workspaceID == "ws_beta" && providerType == ProviderRunPod {
				return &ProviderConfig{Type: providerType, APIKey: "typed-workspace-key"}, nil
			}
			return nil, context.Canceled
		},
	})

	statuses := mgr.GetProviderStatusForWorkspace(context.Background(), "ws_beta")
	found := false
	for _, status := range statuses {
		if status.Provider == ProviderRunPod {
			found = true
			if !status.Connected {
				t.Fatalf("expected typed workspace-scoped provider to be connected: %+v", status)
			}
		}
	}
	if !found {
		t.Fatal("expected runpod status from typed workspace resolver")
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

func TestManagerStartUsesInstanceStarterWhenAvailable(t *testing.T) {
	mgr := newTestManager(t, ManagerConfig{DefaultProvider: ProviderMock})
	provider := newInstanceAwareStartProvider()
	mgr.RegisterProvider(provider)

	instance := &Instance{
		ID:         "inst-1",
		ProviderID: "mock-inst-1",
		Provider:   ProviderMock,
		Status:     InstanceStatusStopped,
		CreatedAt:  time.Now(),
	}
	provider.instances[instance.ID] = instance
	mgr.instances.put(instance)

	if err := mgr.Start(context.Background(), instance.ID); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if len(provider.startedWithInstance) != 1 || provider.startedWithInstance[0] != instance.ID {
		t.Fatalf("expected StartWithInstance to be used, got %#v", provider.startedWithInstance)
	}
}

func TestNewManagerWithStoreUsesInjectedInstanceStore(t *testing.T) {
	store := newInMemoryInstanceStore()
	instance := &Instance{
		ID:          "inst-store",
		ProviderID:  "mock-store",
		Provider:    ProviderMock,
		Status:      InstanceStatusStopped,
		GPUType:     GPURTX4090,
		GPUCount:    1,
		WorkspaceID: "ws_store",
		Models:      []string{"Qwen/Qwen2.5-7B-Instruct"},
		CreatedAt:   time.Now(),
	}
	store.put(instance)

	mgr := newTestManager(t, ManagerConfig{DefaultProvider: ProviderMock})
	mgrWithStore, err := NewManagerWithStore(ManagerConfig{DefaultProvider: ProviderMock}, store)
	if err != nil {
		t.Fatalf("NewManagerWithStore failed: %v", err)
	}
	t.Cleanup(func() { _ = mgrWithStore.Close() })

	if _, exists := mgr.GetInstance("inst-store"); exists {
		t.Fatal("unexpected instance in default manager")
	}
	if found, exists := mgrWithStore.GetInstance("inst-store"); !exists || found.ID != "inst-store" {
		t.Fatalf("expected injected store instance, got %+v exists=%v", found, exists)
	}
}

func TestInMemoryManagerCredentialsRequireExplicitActiveStatus(t *testing.T) {
	store := newInMemoryInstanceStore()
	mgr, err := NewManagerWithStore(ManagerConfig{DefaultProvider: ProviderMock}, store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	for _, status := range []InstanceStatus{
		InstanceStatusStarting, InstanceStatusStopping, InstanceStatusStopped, InstanceStatusTerminating,
		InstanceStatusTerminated, InstanceStatusError, "", "future_corrupt",
	} {
		credential := "credential-" + string(status)
		hash := sha256.Sum256([]byte(credential))
		instance := &Instance{
			ID: "instance-" + string(status), ProviderID: "provider-" + string(status),
			Provider: ProviderMock, WorkspaceID: "ws-test", Status: status,
			WorkerID: "worker-" + string(status), WorkerCredential: credential,
			WorkerCredentialHash: hash, CreatedAt: time.Now().UTC(),
		}
		if err := store.put(instance); err != nil {
			t.Fatal(err)
		}
		if authenticated, found, err := mgr.AuthenticateWorkerToken(credential); err != nil || found || authenticated != nil {
			t.Fatalf("inactive %q authenticated: found=%v err=%v", status, found, err)
		}
		if outbound, found, err := mgr.WorkerCredentialForWorker(instance.WorkerID); err != nil || found || outbound != "" {
			t.Fatalf("inactive %q returned outbound credential: found=%v err=%v", status, found, err)
		}
	}
}

func TestManagerStartRejectedForTerminatedInstance(t *testing.T) {
	mgr := newTestManager(t, ManagerConfig{DefaultProvider: ProviderMock})
	provider := newMockTestProvider()
	mgr.RegisterProvider(provider)

	ctx := context.Background()
	req := &ProvisionRequest{Name: "terminated-start", GPUType: GPURTX4090}
	instance, _ := mgr.Provision(ctx, req)
	mgr.instances.update(instance.ID, func(stored *Instance) {
		stored.Status = InstanceStatusTerminated
	})

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

func TestManagerRefreshDoesNotOverwriteConcurrentStopWithStaleRunningSnapshot(t *testing.T) {
	provider := &delayedRefreshProvider{
		mockTestProvider: newMockTestProvider(),
		snapshotTaken:    make(chan struct{}),
		release:          make(chan struct{}),
	}
	mgr := newTestManager(t, ManagerConfig{DefaultProvider: ProviderMock})
	mgr.RegisterProvider(provider)
	instance, err := mgr.Provision(context.Background(), &ProvisionRequest{
		Name: "version-fenced-refresh", Provider: ProviderMock,
		GPUType: GPURTX4090, GPUCount: 1, Models: []string{"model-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	credential := provider.lastReq.WorkerToken
	refreshResult := make(chan error, 1)
	go func() { refreshResult <- mgr.RefreshInstances(context.Background()) }()
	<-provider.snapshotTaken
	if err := mgr.Stop(context.Background(), instance.ID); err != nil {
		t.Fatalf("stop while refresh delayed: %v", err)
	}
	close(provider.release)
	if err := <-refreshResult; err != nil {
		t.Fatalf("delayed refresh: %v", err)
	}
	stored, found, err := mgr.GetInstanceWithError(instance.ID)
	if err != nil || !found {
		t.Fatalf("read stopped instance: found=%v err=%v", found, err)
	}
	if stored.Status != InstanceStatusStopped {
		t.Fatalf("stale running snapshot overwrote stop: %s", stored.Status)
	}
	if authenticated, found, err := mgr.AuthenticateWorkerToken(credential); err != nil || found || authenticated != nil {
		t.Fatalf("stale refresh restored stopped credential: found=%v err=%v", found, err)
	}
}

func TestManagerRefreshAppliesProviderTransitionAcrossConcurrentWorkerHeartbeat(t *testing.T) {
	provider := &delayedRefreshProvider{
		mockTestProvider: newMockTestProvider(),
		snapshotTaken:    make(chan struct{}),
		release:          make(chan struct{}),
	}
	mgr := newTestManager(t, ManagerConfig{DefaultProvider: ProviderMock})
	mgr.RegisterProvider(provider)
	instance, err := mgr.Provision(context.Background(), &ProvisionRequest{
		Name: "worker-write-does-not-starve-refresh", Provider: ProviderMock,
		GPUType: GPURTX4090, GPUCount: 1, Models: []string{"model-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.LinkWorker(instance.ID, "worker-during-refresh"); err != nil {
		t.Fatalf("link worker before refresh: %v", err)
	}
	providerInstance := provider.findInstance(instance.ProviderID)
	providerInstance.PublicIP = "203.0.113.42"
	providerInstance.HTTPPort = 18443

	refreshResult := make(chan error, 1)
	go func() { refreshResult <- mgr.RefreshInstances(context.Background()) }()
	<-provider.snapshotTaken
	heartbeatAt := time.Now().UTC().Add(time.Minute)
	updated, err := mgr.RecordWorkerHeartbeatWithError("worker-during-refresh", heartbeatAt)
	if err != nil || !updated {
		t.Fatalf("record heartbeat while refresh delayed: updated=%v err=%v", updated, err)
	}
	close(provider.release)
	if err := <-refreshResult; err != nil {
		t.Fatalf("delayed refresh: %v", err)
	}
	stored, found, err := mgr.GetInstanceWithError(instance.ID)
	if err != nil || !found {
		t.Fatalf("read refreshed instance: found=%v err=%v", found, err)
	}
	if stored.PublicIP != "203.0.113.42" || stored.HTTPPort != 18443 {
		t.Fatalf("provider transition was not applied: %+v", stored)
	}
	if stored.WorkerID != "worker-during-refresh" {
		t.Fatalf("provider refresh erased concurrent worker link: %+v", stored)
	}
	if stored.WorkerLastHeartbeatAt == nil || !stored.WorkerLastHeartbeatAt.Equal(heartbeatAt) {
		t.Fatalf("provider refresh erased concurrent worker heartbeat: %+v", stored)
	}
}

func TestManagerLifecycleClaimRejectsConflictingCrossReplicaAction(t *testing.T) {
	store := newInMemoryInstanceStore()
	credential := "cross-replica-lifecycle-credential"
	credentialHash := sha256.Sum256([]byte(credential))
	instance := &Instance{
		ID: "cross-replica", ProviderID: "provider-cross-replica", Provider: ProviderMock,
		Status: InstanceStatusRunning, WorkerCredential: credential, WorkerCredentialHash: credentialHash,
		CreatedAt: time.Now().UTC(),
	}
	if err := store.put(instance); err != nil {
		t.Fatal(err)
	}
	provider := &blockingLifecycleProvider{
		mockTestProvider: newMockTestProvider(),
		stopEntered:      make(chan struct{}, 2),
		stopRelease:      make(chan struct{}),
	}
	provider.instances[instance.ID] = cloneInstance(instance)
	mgrA, err := NewManagerWithStore(ManagerConfig{DefaultProvider: ProviderMock}, store)
	if err != nil {
		t.Fatal(err)
	}
	defer mgrA.Close()
	mgrB, err := NewManagerWithStore(ManagerConfig{DefaultProvider: ProviderMock}, store)
	if err != nil {
		t.Fatal(err)
	}
	defer mgrB.Close()
	mgrA.RegisterProvider(provider)
	mgrB.RegisterProvider(provider)

	stopResult := make(chan error, 1)
	go func() { stopResult <- mgrA.Stop(context.Background(), instance.ID) }()
	<-provider.stopEntered
	claimed, found, err := store.get(instance.ID)
	if err != nil || !found {
		t.Fatalf("read claimed lifecycle: found=%v err=%v", found, err)
	}
	if claimed.Status != InstanceStatusStopping || claimed.LifecycleClaimedAt == nil {
		t.Fatalf("stop did not durably claim inactive transitional state: %+v", claimed)
	}
	if authenticated, found, err := mgrB.AuthenticateWorkerToken(credential); err != nil || found || authenticated != nil {
		t.Fatalf("stopping credential remained active: instance=%+v found=%v err=%v", authenticated, found, err)
	}
	if err := mgrB.Terminate(context.Background(), instance.ID); !errors.Is(err, ErrLifecycleConflict) {
		t.Fatalf("conflicting terminate did not lose lifecycle claim: %v", err)
	}
	if calls := provider.terminateCalls.Load(); calls != 0 {
		t.Fatalf("losing replica reached provider I/O: terminate calls=%d", calls)
	}
	close(provider.stopRelease)
	if err := <-stopResult; err != nil {
		t.Fatalf("winning stop: %v", err)
	}
}

func TestManagerStartRemainsCredentialInactiveUntilProviderAndStoreFinalize(t *testing.T) {
	store := newInMemoryInstanceStore()
	credential := "starting-credential"
	credentialHash := sha256.Sum256([]byte(credential))
	instance := &Instance{
		ID: "starting-inactive", ProviderID: "provider-starting-inactive", Provider: ProviderMock,
		Status: InstanceStatusStopped, WorkerCredential: credential, WorkerCredentialHash: credentialHash,
		CreatedAt: time.Now().UTC(),
	}
	if err := store.put(instance); err != nil {
		t.Fatal(err)
	}
	provider := &blockingLifecycleProvider{
		mockTestProvider: newMockTestProvider(),
		startEntered:     make(chan struct{}, 1),
		startRelease:     make(chan struct{}),
	}
	provider.instances[instance.ID] = cloneInstance(instance)
	mgr, err := NewManagerWithStore(ManagerConfig{DefaultProvider: ProviderMock}, store)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	mgr.RegisterProvider(provider)
	startResult := make(chan error, 1)
	go func() { startResult <- mgr.Start(context.Background(), instance.ID) }()
	<-provider.startEntered
	claimed, found, err := store.get(instance.ID)
	if err != nil || !found || claimed.Status != InstanceStatusStarting || claimed.LifecycleClaimedAt == nil {
		t.Fatalf("start did not claim inactive starting state: found=%v err=%v instance=%+v", found, err, claimed)
	}
	if authenticated, found, err := mgr.AuthenticateWorkerToken(credential); err != nil || found || authenticated != nil {
		t.Fatalf("starting credential became active before finalization: instance=%+v found=%v err=%v", authenticated, found, err)
	}
	close(provider.startRelease)
	if err := <-startResult; err != nil {
		t.Fatalf("start: %v", err)
	}
	final, found, err := store.get(instance.ID)
	if err != nil || !found || final.Status != InstanceStatusProvisioning || final.LifecycleClaimedAt != nil {
		t.Fatalf("start did not finalize provisioning: found=%v err=%v instance=%+v", found, err, final)
	}
}

func TestManagerLifecycleProviderOperationIsBounded(t *testing.T) {
	store := newInMemoryInstanceStore()
	instance := &Instance{
		ID: "bounded-start", ProviderID: "provider-bounded-start", Provider: ProviderMock,
		Status: InstanceStatusStopped, CreatedAt: time.Now().UTC(),
	}
	if err := store.put(instance); err != nil {
		t.Fatal(err)
	}
	provider := &blockingLifecycleProvider{
		mockTestProvider: newMockTestProvider(),
		startEntered:     make(chan struct{}, 1),
		startRelease:     make(chan struct{}),
	}
	provider.instances[instance.ID] = cloneInstance(instance)
	mgr, err := NewManagerWithStore(ManagerConfig{
		DefaultProvider: ProviderMock, LifecycleOperationTimeout: 25 * time.Millisecond,
		LifecycleReconcileLease: time.Second,
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	mgr.RegisterProvider(provider)
	started := time.Now()
	err = mgr.Start(context.Background(), instance.ID)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected bounded provider deadline, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("provider lifecycle operation exceeded configured bound: %v", elapsed)
	}
	stored, found, readErr := store.get(instance.ID)
	if readErr != nil || !found || stored.Status != InstanceStatusStarting || stored.LifecycleClaimedAt == nil {
		t.Fatalf("timed-out start did not remain recoverable and inactive: found=%v err=%v instance=%+v", found, readErr, stored)
	}
}

func TestManagerLifecycleFinalizationFailureRecoversExpiredStop(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	persistErr := errors.New("final lifecycle write unavailable")
	baseStore := newInMemoryInstanceStore()
	store := &lifecycleFaultStore{
		instanceStore: baseStore, failUpdateCall: 2,
		failUpdateErr: fmt.Errorf("%w: %v", ErrControlStateUnavailable, persistErr),
	}
	instance := &Instance{
		ID: "recover-stop", ProviderID: "provider-recover-stop", Provider: ProviderMock,
		Status: InstanceStatusRunning, CreatedAt: now,
	}
	if err := store.put(instance); err != nil {
		t.Fatal(err)
	}
	provider := &blockingLifecycleProvider{mockTestProvider: newMockTestProvider()}
	provider.instances[instance.ID] = cloneInstance(instance)
	mgr, err := NewManagerWithStore(ManagerConfig{
		DefaultProvider: ProviderMock, LifecycleOperationTimeout: 30 * time.Second,
		LifecycleReconcileLease: time.Minute,
		Now:                     func() time.Time { return now },
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	mgr.RegisterProvider(provider)

	if err := mgr.Stop(context.Background(), instance.ID); !errors.Is(err, ErrControlStateUnavailable) {
		t.Fatalf("expected final persistence failure, got %v", err)
	}
	transition, found, err := baseStore.get(instance.ID)
	if err != nil || !found || transition.Status != InstanceStatusStopping || transition.LifecycleClaimedAt == nil {
		t.Fatalf("failed finalization did not remain recoverable and inactive: found=%v err=%v instance=%+v", found, err, transition)
	}
	now = now.Add(2 * time.Minute)
	if err := mgr.RefreshInstances(context.Background()); err != nil {
		t.Fatalf("recover expired stop: %v", err)
	}
	recovered, found, err := baseStore.get(instance.ID)
	if err != nil || !found || recovered.Status != InstanceStatusStopped || recovered.LifecycleClaimedAt != nil {
		t.Fatalf("stop recovery did not finalize: found=%v err=%v instance=%+v", found, err, recovered)
	}
	if calls := provider.stopCalls.Load(); calls != 2 {
		t.Fatalf("expected original and idempotent recovery stop calls, got %d", calls)
	}
}

func TestManagerExpiredTerminationRecoveryHasSingleCrossReplicaClaimer(t *testing.T) {
	claimedAt := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	now := claimedAt.Add(30 * time.Second)
	store := newInMemoryInstanceStore()
	instance := &Instance{
		ID: "recover-terminate", ProviderID: "provider-recover-terminate", Provider: ProviderMock,
		Status: InstanceStatusTerminating, LifecycleClaimedAt: &claimedAt, CreatedAt: claimedAt,
	}
	if err := store.put(instance); err != nil {
		t.Fatal(err)
	}
	provider := &blockingLifecycleProvider{
		mockTestProvider: newMockTestProvider(),
		terminateEntered: make(chan struct{}, 2),
		terminateRelease: make(chan struct{}),
	}
	config := ManagerConfig{
		DefaultProvider: ProviderMock, LifecycleOperationTimeout: 30 * time.Second,
		LifecycleReconcileLease: time.Minute,
		Now:                     func() time.Time { return now },
	}
	mgrA, err := NewManagerWithStore(config, store)
	if err != nil {
		t.Fatal(err)
	}
	defer mgrA.Close()
	mgrB, err := NewManagerWithStore(config, store)
	if err != nil {
		t.Fatal(err)
	}
	defer mgrB.Close()
	mgrA.RegisterProvider(provider)
	mgrB.RegisterProvider(provider)
	if err := mgrB.RefreshInstances(context.Background()); err != nil {
		t.Fatalf("refresh before recovery lease: %v", err)
	}
	if calls := provider.terminateCalls.Load(); calls != 0 {
		t.Fatalf("transition was reclaimed before lease despite operation window ending: calls=%d", calls)
	}
	now = claimedAt.Add(2 * time.Minute)

	firstResult := make(chan error, 1)
	go func() { firstResult <- mgrA.RefreshInstances(context.Background()) }()
	<-provider.terminateEntered
	if err := mgrB.RefreshInstances(context.Background()); err != nil {
		t.Fatalf("competing recovery refresh: %v", err)
	}
	if calls := provider.terminateCalls.Load(); calls != 1 {
		t.Fatalf("multiple replicas performed provider termination: calls=%d", calls)
	}
	close(provider.terminateRelease)
	if err := <-firstResult; err != nil {
		t.Fatalf("winning recovery refresh: %v", err)
	}
	stored, found, err := store.get(instance.ID)
	if err != nil || !found || stored.Status != InstanceStatusTerminated || stored.LifecycleClaimedAt != nil {
		t.Fatalf("termination recovery did not finalize: found=%v err=%v instance=%+v", found, err, stored)
	}
}

func TestManagerLifecycleRecoveryTreatsProviderNotFoundAsTerminated(t *testing.T) {
	for _, status := range []InstanceStatus{InstanceStatusStarting, InstanceStatusStopping, InstanceStatusTerminating} {
		t.Run(string(status), func(t *testing.T) {
			now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
			claimedAt := now.Add(-2 * time.Minute)
			store := newInMemoryInstanceStore()
			instance := &Instance{
				ID: "not-found-" + string(status), ProviderID: "provider-not-found", Provider: ProviderMock,
				Status: status, LifecycleClaimedAt: &claimedAt, CreatedAt: claimedAt,
			}
			if err := store.put(instance); err != nil {
				t.Fatal(err)
			}
			notFound := &ProviderError{Provider: ProviderMock, Code: ProviderErrorNotFound, Message: "gone"}
			provider := &blockingLifecycleProvider{
				mockTestProvider: newMockTestProvider(), startErr: notFound, stopErr: notFound, terminateErr: notFound,
			}
			mgr, err := NewManagerWithStore(ManagerConfig{
				DefaultProvider: ProviderMock, LifecycleOperationTimeout: 30 * time.Second,
				LifecycleReconcileLease: time.Minute, Now: func() time.Time { return now },
			}, store)
			if err != nil {
				t.Fatal(err)
			}
			defer mgr.Close()
			mgr.RegisterProvider(provider)
			if err := mgr.RefreshInstances(context.Background()); err != nil {
				t.Fatalf("recover not-found lifecycle: %v", err)
			}
			stored, found, err := store.get(instance.ID)
			if err != nil || !found || stored.Status != InstanceStatusTerminated || stored.LifecycleClaimedAt != nil {
				t.Fatalf("not-found did not finalize terminated: found=%v err=%v instance=%+v", found, err, stored)
			}
		})
	}
}

func TestManagerDirectLifecycleNotFoundFinalizesTerminated(t *testing.T) {
	for _, action := range []string{"start", "stop", "terminate"} {
		t.Run(action, func(t *testing.T) {
			notFound := &ProviderError{Provider: ProviderMock, Code: ProviderErrorNotFound, Message: "gone"}
			provider := &blockingLifecycleProvider{mockTestProvider: newMockTestProvider()}
			mgr := newTestManager(t, ManagerConfig{DefaultProvider: ProviderMock})
			mgr.RegisterProvider(provider)
			instance, err := mgr.Provision(context.Background(), &ProvisionRequest{Name: "missing-" + action, GPUType: GPURTX4090})
			if err != nil {
				t.Fatal(err)
			}
			if action == "start" {
				if err := mgr.Stop(context.Background(), instance.ID); err != nil {
					t.Fatalf("prepare stopped instance: %v", err)
				}
				provider.startErr = notFound
				err = mgr.Start(context.Background(), instance.ID)
			} else if action == "stop" {
				provider.stopErr = notFound
				err = mgr.Stop(context.Background(), instance.ID)
			} else {
				provider.terminateErr = notFound
				err = mgr.Terminate(context.Background(), instance.ID)
			}
			if err != nil {
				t.Fatalf("not-found %s should be idempotent success: %v", action, err)
			}
			stored, found, err := mgr.GetInstanceWithError(instance.ID)
			if err != nil || !found || stored.Status != InstanceStatusTerminated || stored.LifecycleClaimedAt != nil {
				t.Fatalf("direct not-found did not finalize terminated: found=%v err=%v instance=%+v", found, err, stored)
			}
		})
	}
}

func TestManagerLifecycleRecoveryFailureRemainsInactiveForRetry(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	claimedAt := now.Add(-2 * time.Minute)
	store := newInMemoryInstanceStore()
	instance := &Instance{
		ID: "retry-stop", ProviderID: "provider-retry-stop", Provider: ProviderMock,
		Status: InstanceStatusStopping, LifecycleClaimedAt: &claimedAt, CreatedAt: claimedAt,
	}
	if err := store.put(instance); err != nil {
		t.Fatal(err)
	}
	provider := &blockingLifecycleProvider{
		mockTestProvider: newMockTestProvider(), stopErr: errors.New("temporary provider failure"),
	}
	mgr, err := NewManagerWithStore(ManagerConfig{
		DefaultProvider: ProviderMock, LifecycleOperationTimeout: 30 * time.Second,
		LifecycleReconcileLease: time.Minute, Now: func() time.Time { return now },
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	mgr.RegisterProvider(provider)
	if err := mgr.RefreshInstances(context.Background()); err != nil {
		t.Fatalf("failed recovery refresh: %v", err)
	}
	stored, found, err := store.get(instance.ID)
	if err != nil || !found || stored.Status != InstanceStatusStopping || stored.LifecycleClaimedAt == nil || !stored.LifecycleClaimedAt.Equal(now) {
		t.Fatalf("failed recovery did not remain leased and inactive: found=%v err=%v instance=%+v", found, err, stored)
	}
}

func TestManagerAmbiguousLifecycleFailureStaysInactiveAndRecovers(t *testing.T) {
	for _, action := range []string{"start", "stop", "terminate"} {
		t.Run(action, func(t *testing.T) {
			now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
			provider := &blockingLifecycleProvider{mockTestProvider: newMockTestProvider()}
			if action == "stop" {
				provider.stopErr = context.DeadlineExceeded
			} else if action == "terminate" {
				provider.terminateErr = context.DeadlineExceeded
			}
			mgr := newTestManager(t, ManagerConfig{
				DefaultProvider: ProviderMock, LifecycleOperationTimeout: 30 * time.Second,
				LifecycleReconcileLease: time.Minute, Now: func() time.Time { return now },
			})
			mgr.RegisterProvider(provider)
			instance, err := mgr.Provision(context.Background(), &ProvisionRequest{Name: "ambiguous-" + action, GPUType: GPURTX4090})
			if err != nil {
				t.Fatal(err)
			}
			credential := provider.lastReq.WorkerToken
			if action == "start" {
				if err := mgr.Stop(context.Background(), instance.ID); err != nil {
					t.Fatalf("prepare stopped instance: %v", err)
				}
				provider.startErr = context.DeadlineExceeded
				err = mgr.Start(context.Background(), instance.ID)
				provider.startErr = nil
			} else if action == "stop" {
				err = mgr.Stop(context.Background(), instance.ID)
				provider.stopErr = nil
			} else {
				err = mgr.Terminate(context.Background(), instance.ID)
				provider.terminateErr = nil
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected ambiguous provider timeout, got %v", err)
			}
			transition, found, readErr := mgr.instances.get(instance.ID)
			expectedTransition := InstanceStatusStarting
			if action == "stop" {
				expectedTransition = InstanceStatusStopping
			} else if action == "terminate" {
				expectedTransition = InstanceStatusTerminating
			}
			if readErr != nil || !found || transition.Status != expectedTransition || transition.LifecycleClaimedAt == nil {
				t.Fatalf("ambiguous failure did not remain transitional: found=%v err=%v instance=%+v", found, readErr, transition)
			}
			if authenticated, found, authErr := mgr.AuthenticateWorkerToken(credential); authErr != nil || found || authenticated != nil {
				t.Fatalf("ambiguous failure re-enabled credential: instance=%+v found=%v err=%v", authenticated, found, authErr)
			}
			now = now.Add(2 * time.Minute)
			if err := mgr.RefreshInstances(context.Background()); err != nil {
				t.Fatalf("leased recovery: %v", err)
			}
			stored, found, readErr := mgr.instances.get(instance.ID)
			expectedFinal := InstanceStatusProvisioning
			if action == "stop" {
				expectedFinal = InstanceStatusStopped
			} else if action == "terminate" {
				expectedFinal = InstanceStatusTerminated
			}
			if readErr != nil || !found || stored.Status != expectedFinal || stored.LifecycleClaimedAt != nil {
				t.Fatalf("ambiguous action did not recover: found=%v err=%v instance=%+v", found, readErr, stored)
			}
		})
	}
}

func TestManagerProvisionCleanupUsesIndependentBoundedContextAndJoinsErrors(t *testing.T) {
	persistErr := errors.New("persist failed")
	cleanupErr := errors.New("cleanup failed")
	store := &lifecycleFaultStore{instanceStore: newInMemoryInstanceStore(), putErr: persistErr}
	provider := &cleanupObservingProvider{
		mockTestProvider: newMockTestProvider(), terminateErr: cleanupErr,
		cleanup: make(chan cleanupContextObservation, 1),
	}
	mgr, err := NewManagerWithStore(ManagerConfig{
		DefaultProvider: ProviderMock, ProviderCleanupTimeout: time.Second,
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	mgr.RegisterProvider(provider)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = mgr.Provision(ctx, &ProvisionRequest{Name: "cleanup-orphan", GPUType: GPURTX4090})
	if !errors.Is(err, persistErr) || !errors.Is(err, cleanupErr) {
		t.Fatalf("provision did not retain persistence and cleanup errors: %v", err)
	}
	observation := <-provider.cleanup
	if observation.err != nil || !observation.hasDeadline {
		t.Fatalf("cleanup reused canceled or unbounded request context: %+v", observation)
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
		if _, err := mgr.Provision(ctx, req); err != nil {
			t.Fatalf("Provision %d: %v", i, err)
		}
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

	mgr.instances.update(instance.ID, func(stored *Instance) {
		stored.Provider = ProviderRunPod
		stored.ProviderID = "pod-123"
	})

	found, ok := mgr.GetInstanceByProviderRef(ProviderRunPod, "pod-123")
	if !ok {
		t.Fatal("expected provider ref lookup to find instance")
	}
	if found.ID != instance.ID {
		t.Fatalf("expected instance %q, got %q", instance.ID, found.ID)
	}
}
