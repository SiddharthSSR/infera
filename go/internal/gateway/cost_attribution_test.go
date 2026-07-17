package gateway

import (
	"context"
	"math"
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

func TestAttributedCostNanoUsesDeterministicHalfUpRounding(t *testing.T) {
	got, ok := attributedCostNano(1, millisecondsPerHour/2, 1)
	if !ok || got != 1 {
		t.Fatalf("expected exact half nano-USD to round up, got %d, ok=%v", got, ok)
	}
}

func TestAttributedCostNanoRejectsInvalidAndOverflowingInputs(t *testing.T) {
	tests := []struct {
		name                   string
		price, elapsed, active int64
	}{
		{name: "zero price", elapsed: 1, active: 1},
		{name: "negative price", price: -1, elapsed: 1, active: 1},
		{name: "negative elapsed", price: 1, elapsed: -1, active: 1},
		{name: "zero concurrency", price: 1, elapsed: 1},
		{name: "result overflow", price: math.MaxInt64, elapsed: time.Duration(math.MaxInt64).Milliseconds(), active: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got, ok := attributedCostNano(test.price, test.elapsed, test.active); ok {
				t.Fatalf("expected unavailable result, got %d", got)
			}
		})
	}
}
