package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/infera/infera/go/internal/audit"
	"github.com/infera/infera/go/internal/providers"
	providermock "github.com/infera/infera/go/internal/providers/mock"
	"github.com/infera/infera/go/pkg/types"
)

func TestRequestCostAttributionAmortizesObservedConcurrency(t *testing.T) {
	manager, err := providers.NewManager(providers.ManagerConfig{DefaultProvider: providers.ProviderMock})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	manager.RegisterProvider(providermock.New())
	instance, err := manager.Provision(context.Background(), &providers.ProvisionRequest{
		Name: "cost-test", GPUType: providers.GPURTX4090, GPUCount: 1,
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if err := manager.LinkWorker(instance.ID, "worker-cost"); err != nil {
		t.Fatalf("LinkWorker: %v", err)
	}

	active := 1
	gateway := New(DefaultConfig(), nil, manager)
	got := gateway.requestCostAttribution("worker-cost", types.RoutingDecision{WorkerActiveRequests: &active}, 2*time.Second)
	if got.CostAccuracy != audit.CostAccuracyEstimated || got.ObservedActiveConcurrency != 2 {
		t.Fatalf("unexpected accuracy/concurrency: %+v", got)
	}
	if got.PriceAmountNano != 400_000_000 || got.PriceCurrency != "USD" || got.PriceTimeUnit != "hour" {
		t.Fatalf("unexpected explicit price snapshot: %+v", got)
	}
	if got.CostNano != 111_111 {
		t.Fatalf("expected rounded shared cost 111111 nano-USD, got %d", got.CostNano)
	}
}

func TestRequestCostAttributionMarksMissingPriceUnavailable(t *testing.T) {
	gateway := New(DefaultConfig(), nil, nil)
	got := gateway.requestCostAttribution("worker-without-instance", types.RoutingDecision{}, time.Second)
	if got.CostAccuracy != audit.CostAccuracyUnavailable || got.CostNano != 0 {
		t.Fatalf("missing price must be unavailable, got %+v", got)
	}
}
