package mock

import (
	"context"
	"testing"

	"github.com/infera/infera/go/internal/providers"
)

func TestNew(t *testing.T) {
	p := New()
	if p == nil {
		t.Fatal("New() returned nil")
	}
	if p.instances == nil {
		t.Error("instances map should be initialized")
	}
}

func TestName(t *testing.T) {
	p := New()
	if p.Name() != providers.ProviderMock {
		t.Errorf("expected %s, got %s", providers.ProviderMock, p.Name())
	}
}

func TestProvision(t *testing.T) {
	p := New()
	ctx := context.Background()

	req := &providers.ProvisionRequest{
		Name:         "test-worker",
		GPUType:      providers.GPURTX4090,
		GPUCount:     2,
		SpotInstance: false,
	}

	instance, err := p.Provision(ctx, req)
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	t.Run("Instance created", func(t *testing.T) {
		if instance == nil {
			t.Fatal("instance is nil")
		}
	})

	t.Run("ID generated", func(t *testing.T) {
		if instance.ID == "" {
			t.Error("ID should not be empty")
		}
		if len(instance.ID) != 8 {
			t.Errorf("ID should be 8 chars, got %d", len(instance.ID))
		}
	})

	t.Run("Provider ID set", func(t *testing.T) {
		expected := "mock-" + instance.ID
		if instance.ProviderID != expected {
			t.Errorf("expected %s, got %s", expected, instance.ProviderID)
		}
	})

	t.Run("Status is running", func(t *testing.T) {
		if instance.Status != providers.InstanceStatusRunning {
			t.Errorf("expected running, got %s", instance.Status)
		}
	})

	t.Run("GPU settings correct", func(t *testing.T) {
		if instance.GPUType != providers.GPURTX4090 {
			t.Errorf("expected RTX_4090, got %s", instance.GPUType)
		}
		if instance.GPUCount != 2 {
			t.Errorf("expected 2, got %d", instance.GPUCount)
		}
	})

	t.Run("Cost calculated", func(t *testing.T) {
		expectedCost := 0.40 * 2 // RTX 4090 = $0.40/hr * 2 GPUs
		if instance.CostPerHour != expectedCost {
			t.Errorf("expected %.2f, got %.2f", expectedCost, instance.CostPerHour)
		}
	})

	t.Run("Network configured", func(t *testing.T) {
		if instance.PublicIP != "127.0.0.1" {
			t.Errorf("expected 127.0.0.1, got %s", instance.PublicIP)
		}
		if instance.HTTPPort != 8081 {
			t.Errorf("expected 8081, got %d", instance.HTTPPort)
		}
	})
}

func TestProvisionSpotInstance(t *testing.T) {
	p := New()
	ctx := context.Background()

	req := &providers.ProvisionRequest{
		Name:         "spot-worker",
		GPUType:      providers.GPURTX4090,
		GPUCount:     1,
		SpotInstance: true,
	}

	instance, err := p.Provision(ctx, req)
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	t.Run("Spot instance flag set", func(t *testing.T) {
		if !instance.SpotInstance {
			t.Error("SpotInstance should be true")
		}
	})

	t.Run("Spot discount applied", func(t *testing.T) {
		expectedCost := 0.40 * 0.5 // 50% discount for spot
		if instance.CostPerHour != expectedCost {
			t.Errorf("expected %.2f, got %.2f", expectedCost, instance.CostPerHour)
		}
	})
}

func TestTerminate(t *testing.T) {
	p := New()
	ctx := context.Background()

	// Create an instance first
	req := &providers.ProvisionRequest{
		Name:    "to-terminate",
		GPUType: providers.GPURTX4090,
	}
	instance, _ := p.Provision(ctx, req)

	t.Run("Terminate by ID", func(t *testing.T) {
		err := p.Terminate(ctx, instance.ID)
		if err != nil {
			t.Fatalf("Terminate failed: %v", err)
		}

		// Check status changed
		inst, _ := p.GetInstance(ctx, instance.ID)
		if inst.Status != providers.InstanceStatusTerminated {
			t.Errorf("expected terminated, got %s", inst.Status)
		}
	})

	t.Run("Terminate by ProviderID", func(t *testing.T) {
		// Create new instance
		instance2, _ := p.Provision(ctx, req)

		err := p.Terminate(ctx, instance2.ProviderID)
		if err != nil {
			t.Fatalf("Terminate by ProviderID failed: %v", err)
		}

		inst, _ := p.GetInstance(ctx, instance2.ID)
		if inst.Status != providers.InstanceStatusTerminated {
			t.Errorf("expected terminated, got %s", inst.Status)
		}
	})

	t.Run("Terminate non-existent", func(t *testing.T) {
		err := p.Terminate(ctx, "non-existent-id")
		if err == nil {
			t.Error("expected error for non-existent instance")
		}
	})
}

func TestStartStop(t *testing.T) {
	p := New()
	ctx := context.Background()

	req := &providers.ProvisionRequest{
		Name:    "start-stop-test",
		GPUType: providers.GPURTX4090,
	}
	instance, _ := p.Provision(ctx, req)

	t.Run("Stop instance", func(t *testing.T) {
		err := p.Stop(ctx, instance.ID)
		if err != nil {
			t.Fatalf("Stop failed: %v", err)
		}

		inst, _ := p.GetInstance(ctx, instance.ID)
		if inst.Status != providers.InstanceStatusStopped {
			t.Errorf("expected stopped, got %s", inst.Status)
		}
		if inst.StoppedAt == nil {
			t.Error("StoppedAt should be set")
		}
	})

	t.Run("Start instance", func(t *testing.T) {
		err := p.Start(ctx, instance.ID)
		if err != nil {
			t.Fatalf("Start failed: %v", err)
		}

		inst, _ := p.GetInstance(ctx, instance.ID)
		if inst.Status != providers.InstanceStatusRunning {
			t.Errorf("expected running, got %s", inst.Status)
		}
		if inst.StartedAt == nil {
			t.Error("StartedAt should be set")
		}
		if inst.StoppedAt != nil {
			t.Error("StoppedAt should be nil after start")
		}
	})

	t.Run("Stop by ProviderID", func(t *testing.T) {
		err := p.Stop(ctx, instance.ProviderID)
		if err != nil {
			t.Fatalf("Stop by ProviderID failed: %v", err)
		}
	})

	t.Run("Start by ProviderID", func(t *testing.T) {
		err := p.Start(ctx, instance.ProviderID)
		if err != nil {
			t.Fatalf("Start by ProviderID failed: %v", err)
		}
	})
}

func TestGetInstance(t *testing.T) {
	p := New()
	ctx := context.Background()

	req := &providers.ProvisionRequest{
		Name:    "get-test",
		GPUType: providers.GPURTX4090,
	}
	created, _ := p.Provision(ctx, req)

	t.Run("Get by ID", func(t *testing.T) {
		instance, err := p.GetInstance(ctx, created.ID)
		if err != nil {
			t.Fatalf("GetInstance failed: %v", err)
		}
		if instance.ID != created.ID {
			t.Errorf("expected %s, got %s", created.ID, instance.ID)
		}
	})

	t.Run("Get non-existent", func(t *testing.T) {
		_, err := p.GetInstance(ctx, "non-existent")
		if err == nil {
			t.Error("expected error for non-existent instance")
		}
	})
}

func TestListInstances(t *testing.T) {
	p := New()
	ctx := context.Background()

	t.Run("Empty list", func(t *testing.T) {
		instances, err := p.ListInstances(ctx)
		if err != nil {
			t.Fatalf("ListInstances failed: %v", err)
		}
		if len(instances) != 0 {
			t.Errorf("expected 0, got %d", len(instances))
		}
	})

	// Create some instances
	for i := 0; i < 3; i++ {
		req := &providers.ProvisionRequest{
			Name:    "list-test",
			GPUType: providers.GPURTX4090,
		}
		p.Provision(ctx, req)
	}

	t.Run("List all", func(t *testing.T) {
		instances, err := p.ListInstances(ctx)
		if err != nil {
			t.Fatalf("ListInstances failed: %v", err)
		}
		if len(instances) != 3 {
			t.Errorf("expected 3, got %d", len(instances))
		}
	})
}

func TestListOfferings(t *testing.T) {
	p := New()
	ctx := context.Background()

	offerings, err := p.ListOfferings(ctx)
	if err != nil {
		t.Fatalf("ListOfferings failed: %v", err)
	}

	t.Run("Has offerings", func(t *testing.T) {
		if len(offerings) == 0 {
			t.Error("expected at least one offering")
		}
	})

	t.Run("Offerings have required fields", func(t *testing.T) {
		for _, o := range offerings {
			if o.Provider != providers.ProviderMock {
				t.Errorf("expected mock provider, got %s", o.Provider)
			}
			if o.CostPerHour <= 0 {
				t.Error("CostPerHour should be positive")
			}
			if o.SpotPrice <= 0 {
				t.Error("SpotPrice should be positive")
			}
			if o.SpotPrice >= o.CostPerHour {
				t.Error("SpotPrice should be less than CostPerHour")
			}
		}
	})
}

func TestGetStatus(t *testing.T) {
	p := New()
	ctx := context.Background()

	t.Run("Initial status", func(t *testing.T) {
		status, err := p.GetStatus(ctx)
		if err != nil {
			t.Fatalf("GetStatus failed: %v", err)
		}
		if !status.Connected {
			t.Error("should be connected")
		}
		if status.ActiveCount != 0 {
			t.Errorf("expected 0 active, got %d", status.ActiveCount)
		}
	})

	// Create some instances
	for i := 0; i < 2; i++ {
		req := &providers.ProvisionRequest{Name: "status-test", GPUType: providers.GPURTX4090}
		p.Provision(ctx, req)
	}

	t.Run("With active instances", func(t *testing.T) {
		status, err := p.GetStatus(ctx)
		if err != nil {
			t.Fatalf("GetStatus failed: %v", err)
		}
		if status.ActiveCount != 2 {
			t.Errorf("expected 2 active, got %d", status.ActiveCount)
		}
	})
}

func TestWaitForReady(t *testing.T) {
	p := New()
	ctx := context.Background()

	// Mock provider should return immediately
	err := p.WaitForReady(ctx, "any-id")
	if err != nil {
		t.Errorf("WaitForReady should succeed: %v", err)
	}
}

func TestGetCostForGPU(t *testing.T) {
	tests := []struct {
		gpu      providers.GPUType
		expected float64
	}{
		{providers.GPURTX4090, 0.40},
		{providers.GPURTX4080, 0.30},
		{providers.GPUA100_40, 1.20},
		{providers.GPUA100_80, 2.00},
		{providers.GPUH100, 3.50},
		{providers.GPUL40S, 1.50},
		{providers.GPUType("unknown"), 0.50}, // Default
	}

	for _, tt := range tests {
		t.Run(string(tt.gpu), func(t *testing.T) {
			cost := getCostForGPU(tt.gpu)
			if cost != tt.expected {
				t.Errorf("expected %.2f, got %.2f", tt.expected, cost)
			}
		})
	}
}

func TestFactory(t *testing.T) {
	config := providers.ProviderConfig{
		Type: providers.ProviderMock,
	}

	provider, err := Factory(config)
	if err != nil {
		t.Fatalf("Factory failed: %v", err)
	}

	if provider.Name() != providers.ProviderMock {
		t.Errorf("expected mock, got %s", provider.Name())
	}
}
