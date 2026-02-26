// Package vastai implements the Vast.ai GPU cloud provider.
package vastai

import (
	"context"
	"net/http"
	"time"

	"github.com/infera/infera/go/internal/providers"
)

const (
	defaultEndpoint = "https://console.vast.ai/api/v0"
)

// Provider implements the Vast.ai GPU provider.
type Provider struct {
	apiKey     string
	endpoint   string
	httpClient *http.Client
}

// Config for Vast.ai provider.
type Config struct {
	APIKey   string
	Endpoint string
}

// New creates a new Vast.ai provider.
func New(config Config) (*Provider, error) {
	if config.APIKey == "" {
		return nil, &providers.ProviderError{
			Provider: providers.ProviderVastAI,
			Code:     "missing_api_key",
			Message:  "Vast.ai API key is required",
		}
	}

	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}

	return &Provider{
		apiKey:   config.APIKey,
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// Factory creates a Vast.ai provider from generic config.
func Factory(config providers.ProviderConfig) (providers.Provider, error) {
	return New(Config{
		APIKey:   config.APIKey,
		Endpoint: config.Endpoint,
	})
}

// Register the provider factory.
func init() {
	providers.RegisterProvider(providers.ProviderVastAI, Factory)
}

// Name returns the provider type.
func (p *Provider) Name() providers.ProviderType {
	return providers.ProviderVastAI
}

// Provision creates a new GPU instance.
// TODO: Implement Vast.ai API calls
func (p *Provider) Provision(ctx context.Context, req *providers.ProvisionRequest) (*providers.Instance, error) {
	return nil, &providers.ProviderError{
		Provider: providers.ProviderVastAI,
		Code:     "not_implemented",
		Message:  "Vast.ai provisioning not yet implemented",
	}
}

// Terminate destroys an instance.
func (p *Provider) Terminate(ctx context.Context, instanceID string) error {
	return &providers.ProviderError{
		Provider: providers.ProviderVastAI,
		Code:     "not_implemented",
		Message:  "Vast.ai termination not yet implemented",
	}
}

// Start starts a stopped instance.
func (p *Provider) Start(ctx context.Context, instanceID string) error {
	return &providers.ProviderError{
		Provider: providers.ProviderVastAI,
		Code:     "not_implemented",
		Message:  "Vast.ai start not yet implemented",
	}
}

// Stop stops a running instance.
func (p *Provider) Stop(ctx context.Context, instanceID string) error {
	return &providers.ProviderError{
		Provider: providers.ProviderVastAI,
		Code:     "not_implemented",
		Message:  "Vast.ai stop not yet implemented",
	}
}

// GetInstance returns instance details.
func (p *Provider) GetInstance(ctx context.Context, instanceID string) (*providers.Instance, error) {
	return nil, &providers.ProviderError{
		Provider: providers.ProviderVastAI,
		Code:     "not_implemented",
		Message:  "Vast.ai get instance not yet implemented",
	}
}

// ListInstances returns all instances.
func (p *Provider) ListInstances(ctx context.Context) ([]*providers.Instance, error) {
	return nil, &providers.ProviderError{
		Provider: providers.ProviderVastAI,
		Code:     "not_implemented",
		Message:  "Vast.ai list instances not yet implemented",
	}
}

// ListOfferings returns available GPU configurations.
func (p *Provider) ListOfferings(ctx context.Context) ([]*providers.GPUOffering, error) {
	return nil, &providers.ProviderError{
		Provider: providers.ProviderVastAI,
		Code:     "not_implemented",
		Message:  "Vast.ai list offerings not yet implemented",
	}
}

// GetStatus returns provider health and account info.
func (p *Provider) GetStatus(ctx context.Context) (*providers.ProviderStatus, error) {
	return &providers.ProviderStatus{
		Provider:     providers.ProviderVastAI,
		Connected:    false,
		ErrorMessage: "Vast.ai not yet implemented",
	}, nil
}

// WaitForReady blocks until instance is ready.
func (p *Provider) WaitForReady(ctx context.Context, instanceID string) error {
	return &providers.ProviderError{
		Provider: providers.ProviderVastAI,
		Code:     "not_implemented",
		Message:  "Vast.ai wait for ready not yet implemented",
	}
}
