package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/infera/infera/go/internal/auth"
	"github.com/infera/infera/go/internal/providers"
)

func TestHandleInstancesMatchesSharedInfrastructureFixture(t *testing.T) {
	h := newInfrastructureFixtureHandlers(t, true)
	req := authedWorkspaceRequest(httptest.NewRequest(http.MethodGet, "/api/instances", nil), auth.RoleOperator, "ws_alpha")
	rec := httptest.NewRecorder()

	h.handleInstances(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertJSONEqual(t, loadInfrastructureFixtureBytes(t, InfrastructureFixtureInstancesListResponse), rec.Body.Bytes())
}

func TestHandleOfferingsMatchesSharedInfrastructureFixture(t *testing.T) {
	h := newInfrastructureFixtureHandlers(t, false)
	req := authedWorkspaceRequest(httptest.NewRequest(http.MethodGet, "/api/offerings", nil), auth.RoleOperator, "ws_alpha")
	rec := httptest.NewRecorder()

	h.handleOfferings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertJSONEqual(t, loadInfrastructureFixtureBytes(t, InfrastructureFixtureOfferingsListResponse), rec.Body.Bytes())
}

func TestHandleProvidersMatchesSharedInfrastructureFixture(t *testing.T) {
	h := newInfrastructureFixtureHandlers(t, true)
	req := authedWorkspaceRequest(httptest.NewRequest(http.MethodGet, "/api/providers", nil), auth.RoleOperator, "ws_alpha")
	rec := httptest.NewRecorder()

	h.handleProviders(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertJSONEqual(t, loadInfrastructureFixtureBytes(t, InfrastructureFixtureProvidersListResponse), rec.Body.Bytes())
}

func TestHandleCostsMatchesSharedInfrastructureFixture(t *testing.T) {
	h := newInfrastructureFixtureHandlers(t, false)
	req := authedWorkspaceRequest(httptest.NewRequest(http.MethodGet, "/api/costs", nil), auth.RoleAdmin, "ws_alpha")
	rec := httptest.NewRecorder()

	h.handleCosts(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertJSONEqual(t, loadInfrastructureFixtureBytes(t, InfrastructureFixtureCostSummaryResponse), rec.Body.Bytes())
}

func newInfrastructureFixtureHandlers(t *testing.T, withInstance bool) *InstanceHandlers {
	t.Helper()

	providers.RegisterProvider(providers.ProviderRunPod, func(config providers.ProviderConfig) (providers.Provider, error) {
		return newInfrastructureFixtureProvider(), nil
	})

	manager, err := providers.NewManager(providers.ManagerConfig{
		DefaultProvider: providers.ProviderRunPod,
	})
	if err != nil {
		t.Fatalf("create provider manager: %v", err)
	}

	provider := newInfrastructureFixtureProvider()
	manager.RegisterProvider(provider)

	if withInstance {
		if _, err := manager.Provision(context.Background(), &providers.ProvisionRequest{
			Name:         "Fixture Worker",
			Provider:     providers.ProviderRunPod,
			WorkspaceID:  "ws_alpha",
			GPUType:      providers.GPUH100,
			GPUCount:     1,
			SpotInstance: false,
			Models:       []string{"Qwen/Qwen2.5-7B-Instruct"},
			Engine:       providers.EngineSGLang,
			DockerImage:  "ghcr.io/example/infera-worker:test",
		}); err != nil {
			t.Fatalf("seed fixture instance: %v", err)
		}
	}

	return NewInstanceHandlers(manager)
}

type infrastructureFixtureProvider struct {
	instances map[string]*providers.Instance
}

func newInfrastructureFixtureProvider() *infrastructureFixtureProvider {
	return &infrastructureFixtureProvider{
		instances: make(map[string]*providers.Instance),
	}
}

func (p *infrastructureFixtureProvider) Name() providers.ProviderType {
	return providers.ProviderRunPod
}

func (p *infrastructureFixtureProvider) Provision(ctx context.Context, req *providers.ProvisionRequest) (*providers.Instance, error) {
	startedAt := time.Date(2026, time.April, 10, 0, 5, 0, 0, time.UTC)
	instance := &providers.Instance{
		ID:           "inst_fixture_1",
		ProviderID:   "runpod-fixture-1",
		Provider:     providers.ProviderRunPod,
		Name:         req.Name,
		Status:       providers.InstanceStatusRunning,
		GPUType:      req.GPUType,
		GPUCount:     req.GPUCount,
		VCPU:         32,
		MemoryGB:     80,
		StorageGB:    500,
		PublicIP:     "203.0.113.10",
		HTTPPort:     8081,
		SSHPort:      22,
		WorkerID:     "worker-fixture-1",
		Models:       append([]string(nil), req.Models...),
		Engine:       req.Engine,
		CostPerHour:  3.5,
		SpotInstance: req.SpotInstance,
		CreatedAt:    time.Date(2026, time.April, 10, 0, 0, 0, 0, time.UTC),
		StartedAt:    &startedAt,
		ErrorMessage: "",
	}
	p.instances[instance.ID] = instance
	return instance, nil
}

func (p *infrastructureFixtureProvider) Terminate(ctx context.Context, instanceID string) error {
	return nil
}

func (p *infrastructureFixtureProvider) Start(ctx context.Context, instanceID string) error {
	return nil
}

func (p *infrastructureFixtureProvider) Stop(ctx context.Context, instanceID string) error {
	return nil
}

func (p *infrastructureFixtureProvider) GetInstance(ctx context.Context, instanceID string) (*providers.Instance, error) {
	for _, instance := range p.instances {
		if instance.ID == instanceID || instance.ProviderID == instanceID {
			return instance, nil
		}
	}
	return nil, &providers.ProviderError{
		Provider: providers.ProviderRunPod,
		Code:     providers.ProviderErrorNotFound,
		Message:  "instance not found",
	}
}

func (p *infrastructureFixtureProvider) ListInstances(ctx context.Context) ([]*providers.Instance, error) {
	instances := make([]*providers.Instance, 0, len(p.instances))
	for _, instance := range p.instances {
		instances = append(instances, instance)
	}
	return instances, nil
}

func (p *infrastructureFixtureProvider) ListOfferings(ctx context.Context) ([]*providers.GPUOffering, error) {
	return []*providers.GPUOffering{
		{
			Provider:          providers.ProviderRunPod,
			GPUType:           providers.GPUH100,
			DisplayName:       "NVIDIA H100 SXM",
			ProviderGPUTypeID: "h100-sxm",
			GPUCount:          1,
			VCPU:              32,
			MemoryGB:          80,
			StorageGB:         500,
			CostPerHour:       3.5,
			SpotPrice:         2.75,
			Region:            "us-east-1",
			Available:         3,
		},
	}, nil
}

func (p *infrastructureFixtureProvider) GetStatus(ctx context.Context) (*providers.ProviderStatus, error) {
	return &providers.ProviderStatus{
		Provider:     providers.ProviderRunPod,
		Connected:    true,
		AccountID:    "acct_fixture",
		Balance:      42.5,
		ActiveCount:  len(p.instances),
		QuotaLimit:   8,
		ErrorCode:    "",
		ErrorMessage: "",
		Capabilities: providers.ProviderCapabilities{
			SupportsSpot:            true,
			SupportsCustomImages:    true,
			SupportsRegionSelection: true,
			SupportsPublicIP:        true,
			SupportsSSHKeys:         false,
			SupportsStartStop:       true,
			StartupScriptLimit:      16384,
			KnownRegions:            []string{"us-east-1"},
		},
	}, nil
}

func (p *infrastructureFixtureProvider) WaitForReady(ctx context.Context, instanceID string) error {
	return nil
}

func loadInfrastructureFixtureBytes(t *testing.T, name string) []byte {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "contracts", "infrastructure_dashboard", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}
