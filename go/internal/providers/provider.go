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

const (
	ProviderErrorUnknownProvider    = "unknown_provider"
	ProviderErrorMissingAPIKey      = "missing_api_key"
	ProviderErrorAuthFailed         = "auth_failed"
	ProviderErrorInvalidConfig      = "invalid_config"
	ProviderErrorInvalidRequest     = "invalid_request"
	ProviderErrorNotFound           = "not_found"
	ProviderErrorRateLimited        = "rate_limited"
	ProviderErrorServiceUnavailable = "service_unavailable"
	ProviderErrorTimeout            = "timeout"
	ProviderErrorRequestFailed      = "request_failed"
	ProviderErrorAPIError           = "api_error"
	ProviderErrorGraphQLError       = "graphql_error"
	ProviderErrorInstanceError      = "instance_error"
	ProviderErrorTerminated         = "terminated"
	ProviderErrorNotImplemented     = "not_implemented"
)

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
			Code:     ProviderErrorUnknownProvider,
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
	case ProviderErrorRateLimited, ProviderErrorServiceUnavailable, ProviderErrorTimeout, ProviderErrorRequestFailed:
		return true
	default:
		return false
	}
}

func (e *ProviderError) HTTPStatus(defaultStatus int) int {
	if e.StatusCode > 0 {
		return e.StatusCode
	}

	switch e.Code {
	case ProviderErrorNotFound:
		return 404
	case ProviderErrorRateLimited:
		return 429
	case ProviderErrorMissingAPIKey, ProviderErrorAuthFailed, ProviderErrorInvalidConfig, ProviderErrorInvalidRequest:
		return 400
	case ProviderErrorServiceUnavailable, ProviderErrorRequestFailed, ProviderErrorTimeout, ProviderErrorAPIError, ProviderErrorGraphQLError, ProviderErrorInstanceError:
		return 503
	case ProviderErrorNotImplemented:
		return 501
	default:
		return defaultStatus
	}
}

func (e *ProviderError) APIErrorType() string {
	switch e.Code {
	case ProviderErrorNotFound:
		return "not_found"
	case ProviderErrorRateLimited:
		return "provider_rate_limited"
	case ProviderErrorMissingAPIKey, ProviderErrorAuthFailed:
		return "provider_auth_failed"
	case ProviderErrorInvalidConfig, ProviderErrorInvalidRequest:
		return "provider_invalid_config"
	case ProviderErrorServiceUnavailable, ProviderErrorRequestFailed:
		return "provider_unavailable"
	case ProviderErrorTimeout:
		return "provider_timeout"
	case ProviderErrorNotImplemented:
		return "not_implemented"
	default:
		return "provider_error"
	}
}
