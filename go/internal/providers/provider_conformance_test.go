package providers_test

import (
	"context"
	"errors"
	"testing"

	"github.com/infera/infera/go/internal/providers"
	"github.com/infera/infera/go/internal/providers/mock"
)

func TestMockProviderConformance(t *testing.T) {
	runProviderConformanceSuite(t, func(t *testing.T) providers.Provider {
		t.Helper()
		return mock.New()
	})
}

func runProviderConformanceSuite(t *testing.T, newProvider func(t *testing.T) providers.Provider) {
	t.Helper()

	ctx := context.Background()
	provider := newProvider(t)

	if provider.Name() == "" {
		t.Fatal("provider name must not be empty")
	}

	req := &providers.ProvisionRequest{
		Name:     "conformance-worker",
		GPUType:  providers.GPURTX4090,
		GPUCount: 1,
		Models:   []string{"meta-llama/Meta-Llama-3.1-8B-Instruct"},
	}

	instance, err := provider.Provision(ctx, req)
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}
	if instance == nil {
		t.Fatal("Provision returned nil instance")
	}
	if instance.Provider != provider.Name() {
		t.Fatalf("expected instance provider %q, got %q", provider.Name(), instance.Provider)
	}
	if instance.ProviderID == "" {
		t.Fatal("provisioned instance must include provider_id")
	}

	got, err := provider.GetInstance(ctx, instance.ProviderID)
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}
	if got.ProviderID != instance.ProviderID {
		t.Fatalf("expected provider_id %q, got %q", instance.ProviderID, got.ProviderID)
	}

	listed, err := provider.ListInstances(ctx)
	if err != nil {
		t.Fatalf("ListInstances failed: %v", err)
	}
	if len(listed) == 0 {
		t.Fatal("ListInstances returned no instances after Provision")
	}
	if !containsInstance(listed, instance.ProviderID) {
		t.Fatalf("ListInstances did not include provider_id %q", instance.ProviderID)
	}

	offerings, err := provider.ListOfferings(ctx)
	if err != nil {
		t.Fatalf("ListOfferings failed: %v", err)
	}
	if len(offerings) == 0 {
		t.Fatal("ListOfferings returned no offerings")
	}
	for _, offering := range offerings {
		if offering.Provider != provider.Name() {
			t.Fatalf("expected offering provider %q, got %q", provider.Name(), offering.Provider)
		}
	}

	status, err := provider.GetStatus(ctx)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.Provider != provider.Name() {
		t.Fatalf("expected status provider %q, got %q", provider.Name(), status.Provider)
	}

	if err := provider.WaitForReady(ctx, instance.ProviderID); err != nil {
		t.Fatalf("WaitForReady failed: %v", err)
	}

	if err := provider.Stop(ctx, instance.ProviderID); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	stopped, err := provider.GetInstance(ctx, instance.ProviderID)
	if err != nil {
		t.Fatalf("GetInstance after Stop failed: %v", err)
	}
	if stopped.Status != providers.InstanceStatusStopped && stopped.Status != providers.InstanceStatusStopping {
		t.Fatalf("expected stopped or stopping after Stop, got %q", stopped.Status)
	}

	if err := provider.Start(ctx, instance.ProviderID); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	restarted, err := provider.GetInstance(ctx, instance.ProviderID)
	if err != nil {
		t.Fatalf("GetInstance after Start failed: %v", err)
	}
	if restarted.Status != providers.InstanceStatusRunning &&
		restarted.Status != providers.InstanceStatusPending &&
		restarted.Status != providers.InstanceStatusProvisioning {
		t.Fatalf("expected running/pending/provisioning after Start, got %q", restarted.Status)
	}

	if err := provider.Terminate(ctx, instance.ProviderID); err != nil {
		t.Fatalf("Terminate failed: %v", err)
	}
	terminated, err := provider.GetInstance(ctx, instance.ProviderID)
	if err != nil {
		var providerErr *providers.ProviderError
		if !errors.As(err, &providerErr) || providerErr.Code != "not_found" {
			t.Fatalf("expected not_found or terminated instance after Terminate, got %v", err)
		}
	} else if terminated.Status != providers.InstanceStatusTerminated {
		t.Fatalf("expected terminated status after Terminate, got %q", terminated.Status)
	}

	_, err = provider.GetInstance(ctx, "does-not-exist")
	if err == nil {
		t.Fatal("expected not_found error for missing instance")
	}
	var providerErr *providers.ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("expected ProviderError for missing instance, got %T", err)
	}
	if providerErr.Code != "not_found" {
		t.Fatalf("expected not_found error code, got %q", providerErr.Code)
	}
	if providerErr.Provider != provider.Name() {
		t.Fatalf("expected ProviderError provider %q, got %q", provider.Name(), providerErr.Provider)
	}
}

func containsInstance(instances []*providers.Instance, providerID string) bool {
	for _, instance := range instances {
		if instance != nil && instance.ProviderID == providerID {
			return true
		}
	}
	return false
}
