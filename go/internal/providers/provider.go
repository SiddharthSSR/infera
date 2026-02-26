// Package providers defines the interface for GPU cloud providers.
package providers

import "context"

// Provider defines the interface that all GPU providers must implement.
type Provider interface {
	Name() ProviderType
	Provision(ctx context.Context, req *ProvisionRequest) (*Instance, error)
	Terminate(ctx context.Context, instanceID string) error
	Start(ctx context.Context, instanceID string) error
	Stop(ctx context.Context, instanceID string) error
	GetInstance(ctx context.Context, instanceID string) (*Instance, error)
	ListInstances(ctx context.Context) ([]*Instance, error)
	ListOfferings(ctx context.Context) ([]*GPUOffering, error)
	GetStatus(ctx context.Context) (*ProviderStatus, error)
	WaitForReady(ctx context.Context, instanceID string) error
}

// ProviderConfig contains configuration for a provider.
type ProviderConfig struct {
	Type        ProviderType
	APIKey      string
	APISecret   string
	Endpoint    string
	DefaultOpts map[string]string
}

// ProviderFactory creates provider instances.
type ProviderFactory func(config ProviderConfig) (Provider, error)

// Global registry of provider factories
var providerFactories = make(map[ProviderType]ProviderFactory)

// RegisterProvider registers a provider factory.
func RegisterProvider(providerType ProviderType, factory ProviderFactory) {
	providerFactories[providerType] = factory
}

// CreateProvider creates a provider from config.
func CreateProvider(config ProviderConfig) (Provider, error) {
	factory, exists := providerFactories[config.Type]
	if !exists {
		return nil, &ProviderError{
			Provider: config.Type,
			Code:     "unknown_provider",
			Message:  "provider type not registered",
		}
	}
	return factory(config)
}

// ProviderError represents a provider-specific error.
type ProviderError struct {
	Provider   ProviderType
	Code       string
	Message    string
	StatusCode int
	RetryAfter int
}

func (e *ProviderError) Error() string {
	return string(e.Provider) + ": " + e.Message
}

func (e *ProviderError) IsRetryable() bool {
	switch e.Code {
	case "rate_limited", "service_unavailable", "timeout":
		return true
	default:
		return false
	}
}
