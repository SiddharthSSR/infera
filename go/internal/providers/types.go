// Package providers defines types for GPU cloud providers.
package providers

import (
	"crypto/sha256"
	"strings"
	"time"
)

// ProviderType identifies a GPU cloud provider.
type ProviderType string

const (
	ProviderE2E    ProviderType = "e2e"
	ProviderRunPod ProviderType = "runpod"
	ProviderVastAI ProviderType = "vastai"
	ProviderLambda ProviderType = "lambda"
	ProviderMock   ProviderType = "mock"
)

// InferenceEngine identifies a worker inference runtime.
type InferenceEngine string

const (
	EngineVLLM        InferenceEngine = "vllm"
	EngineSGLang      InferenceEngine = "sglang"
	EngineTensorRTLLM InferenceEngine = "tensorrt_llm"
	EngineMock        InferenceEngine = "mock"
)

var inferenceEngineAliases = map[string]InferenceEngine{
	"":             EngineVLLM,
	"vllm":         EngineVLLM,
	"sglang":       EngineSGLang,
	"mock":         EngineMock,
	"tensorrt_llm": EngineTensorRTLLM,
	"tensorrt-llm": EngineTensorRTLLM,
	"trtllm":       EngineTensorRTLLM,
	"trt-llm":      EngineTensorRTLLM,
}

func NormalizeInferenceEngine(value string) InferenceEngine {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if engine, ok := inferenceEngineAliases[normalized]; ok {
		return engine
	}
	return InferenceEngine(normalized)
}

func (e InferenceEngine) Valid() bool {
	switch e {
	case EngineVLLM, EngineSGLang, EngineTensorRTLLM, EngineMock:
		return true
	default:
		return false
	}
}

func (e InferenceEngine) OrDefault() InferenceEngine {
	normalized := NormalizeInferenceEngine(string(e))
	if normalized.Valid() {
		return normalized
	}
	return EngineVLLM
}

// GPUType represents a GPU model.
type GPUType string

const (
	GPURTX4090 GPUType = "RTX_4090"
	GPURTX4080 GPUType = "RTX_4080"
	GPUA100_40 GPUType = "A100_40GB"
	GPUA100_80 GPUType = "A100_80GB"
	GPUH100    GPUType = "H100"
	GPUL40S    GPUType = "L40S"
)

// GPUSpec contains GPU specifications.
type GPUSpec struct {
	Type       GPUType `json:"type"`
	VRAM       int     `json:"vram_gb"`
	MemoryBW   int     `json:"memory_bw_gbps"`
	TensorCore bool    `json:"tensor_core"`
}

// Known GPU specs
var GPUSpecs = map[GPUType]GPUSpec{
	GPURTX4090: {Type: GPURTX4090, VRAM: 24, MemoryBW: 1008, TensorCore: true},
	GPURTX4080: {Type: GPURTX4080, VRAM: 16, MemoryBW: 717, TensorCore: true},
	GPUA100_40: {Type: GPUA100_40, VRAM: 40, MemoryBW: 1555, TensorCore: true},
	GPUA100_80: {Type: GPUA100_80, VRAM: 80, MemoryBW: 2039, TensorCore: true},
	GPUH100:    {Type: GPUH100, VRAM: 80, MemoryBW: 3350, TensorCore: true},
	GPUL40S:    {Type: GPUL40S, VRAM: 48, MemoryBW: 864, TensorCore: true},
}

// InstanceStatus represents the lifecycle state of an instance.
type InstanceStatus string

const (
	InstanceStatusPending      InstanceStatus = "pending"
	InstanceStatusProvisioning InstanceStatus = "provisioning"
	InstanceStatusRunning      InstanceStatus = "running"
	InstanceStatusStopping     InstanceStatus = "stopping"
	InstanceStatusStopped      InstanceStatus = "stopped"
	InstanceStatusTerminating  InstanceStatus = "terminating"
	InstanceStatusTerminated   InstanceStatus = "terminated"
	InstanceStatusError        InstanceStatus = "error"
)

// WorkerRegistrationStatus represents the worker-side lifecycle for a provider instance.
type WorkerRegistrationStatus string

const (
	WorkerRegistrationPending                     WorkerRegistrationStatus = "pending"
	WorkerRegistrationProviderRunningNoNetwork    WorkerRegistrationStatus = "provider_running_no_network"
	WorkerRegistrationProviderRunningUnregistered WorkerRegistrationStatus = "provider_running_worker_unregistered"
	WorkerRegistrationWorkerUnreachable           WorkerRegistrationStatus = "worker_unreachable"
	WorkerRegistrationHealthUnavailable           WorkerRegistrationStatus = "worker_health_unavailable"
	WorkerRegistrationModelLoading                WorkerRegistrationStatus = "model_loading"
	WorkerRegistrationModelLoadFailed             WorkerRegistrationStatus = "model_load_failed"
	WorkerRegistrationFailed                      WorkerRegistrationStatus = "registration_failed"
	WorkerRegistrationHeartbeatMissing            WorkerRegistrationStatus = "heartbeat_missing"
	WorkerRegistrationRegisteredUnhealthy         WorkerRegistrationStatus = "registered_unhealthy"
	WorkerRegistrationReady                       WorkerRegistrationStatus = "ready"
)

// Instance represents a GPU instance from any provider.
type Instance struct {
	ID          string         `json:"id"`
	ProviderID  string         `json:"provider_id"`
	Provider    ProviderType   `json:"provider"`
	WorkspaceID string         `json:"workspace_id,omitempty"`
	Name        string         `json:"name"`
	Status      InstanceStatus `json:"status"`

	// Hardware
	GPUType   GPUType `json:"gpu_type"`
	GPUCount  int     `json:"gpu_count"`
	VCPU      int     `json:"vcpu"`
	MemoryGB  int     `json:"memory_gb"`
	StorageGB int     `json:"storage_gb"`

	// Network
	PublicIP string `json:"public_ip,omitempty"`
	SSHPort  int    `json:"ssh_port,omitempty"`
	HTTPPort int    `json:"http_port,omitempty"`

	// Infera
	WorkerID string          `json:"worker_id,omitempty"`
	Models   []string        `json:"models,omitempty"`
	Engine   InferenceEngine `json:"engine,omitempty"`

	// Worker registration lifecycle
	WorkerRegistrationStatus      WorkerRegistrationStatus `json:"worker_registration_status,omitempty"`
	WorkerRegistrationDeadline    *time.Time               `json:"worker_registration_deadline,omitempty"`
	LastWorkerRegistrationError   string                   `json:"last_worker_registration_error,omitempty"`
	LastWorkerRegistrationCheckAt *time.Time               `json:"last_worker_registration_check_at,omitempty"`
	WorkerRegisteredAt            *time.Time               `json:"worker_registered_at,omitempty"`
	WorkerLastHeartbeatAt         *time.Time               `json:"worker_last_heartbeat_at,omitempty"`
	WorkerHealthURL               string                   `json:"worker_health_url,omitempty"`
	ProviderNetworkReady          bool                     `json:"provider_network_ready"`
	ProviderNetworkError          string                   `json:"provider_network_error,omitempty"`

	// Cost
	CostPerHour  float64 `json:"cost_per_hour"`
	SpotInstance bool    `json:"spot_instance"`

	// Timestamps
	CreatedAt time.Time  `json:"created_at"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	StoppedAt *time.Time `json:"stopped_at,omitempty"`

	// Provider-specific metadata
	Metadata             map[string]string `json:"metadata,omitempty"`
	WorkerCredential     string            `json:"-"`
	WorkerCredentialHash [sha256.Size]byte `json:"-"`

	// Error info
	ErrorMessage string `json:"error_message,omitempty"`
}

const (
	PriceSnapshotVersionV1 = "provider-instance-hourly-v1"
	PriceCurrencyUSD       = "USD"
	PriceTimeUnitHour      = "hour"
)

// PriceSnapshot is the immutable input used to attribute instance cost to an
// inference execution. AmountNano is integer nanocurrency units per TimeUnit.
// The current provider adapters expose hourly USD instance prices, so callers
// persist both units rather than relying on implicit dollar/hour semantics.
type PriceSnapshot struct {
	Version    string
	Provider   ProviderType
	InstanceID string
	AmountNano int64
	Currency   string
	TimeUnit   string
	CapturedAt time.Time
}

// ProvisionRequest contains parameters for creating a new instance.
type ProvisionRequest struct {
	Name                string       `json:"name"`
	Provider            ProviderType `json:"provider"`
	WorkspaceID         string       `json:"workspace_id,omitempty"`
	GPUType             GPUType      `json:"gpu_type"`
	ProviderGPUTypeID   string       `json:"provider_gpu_type_id,omitempty"`
	GPUCount            int          `json:"gpu_count"`
	Region              string       `json:"region,omitempty"`
	SpotInstance        bool         `json:"spot_instance"`
	MaxCostHour         float64      `json:"max_cost_hour,omitempty"` // Budget limit
	AllowedCudaVersions []string     `json:"allowed_cuda_versions,omitempty"`

	// Worker configuration
	Models          []string        `json:"models,omitempty"`
	Engine          InferenceEngine `json:"engine,omitempty"`
	DockerImage     string          `json:"docker_image,omitempty"`
	GatewayAddress  string          `json:"gateway_address,omitempty"` // Address for worker to connect back
	WorkerToken     string          `json:"-"`
	ReleaseID       string          `json:"-"`
	ProtocolVersion string          `json:"-"`

	// SSH key for access
	SSHPublicKey string `json:"ssh_public_key,omitempty"`

	// Provider-specific options
	Options map[string]string `json:"options,omitempty"`
}

// GPUOffering represents an available GPU configuration from a provider.
type GPUOffering struct {
	Provider          ProviderType `json:"provider"`
	GPUType           GPUType      `json:"gpu_type"`
	DisplayName       string       `json:"display_name,omitempty"`
	ProviderGPUTypeID string       `json:"provider_gpu_type_id,omitempty"`
	GPUCount          int          `json:"gpu_count"`
	VCPU              int          `json:"vcpu"`
	MemoryGB          int          `json:"memory_gb"`
	StorageGB         int          `json:"storage_gb"`
	CostPerHour       float64      `json:"cost_per_hour"`
	SpotPrice         float64      `json:"spot_price,omitempty"`
	Region            string       `json:"region"`
	Available         int          `json:"available"`
}

// ProviderStatus contains provider health and quota info.
type ProviderStatus struct {
	Provider     ProviderType         `json:"provider"`
	Connected    bool                 `json:"connected"`
	AccountID    string               `json:"account_id,omitempty"`
	Balance      float64              `json:"balance,omitempty"`
	ActiveCount  int                  `json:"active_instances"`
	QuotaLimit   int                  `json:"quota_limit,omitempty"`
	ErrorCode    string               `json:"error_code,omitempty"`
	ErrorMessage string               `json:"error_message,omitempty"`
	Capabilities ProviderCapabilities `json:"capabilities"`
}

// ProviderCapabilities describes what the current adapter actually supports.
type ProviderCapabilities struct {
	SupportsSpot            bool     `json:"supports_spot"`
	SupportsCustomImages    bool     `json:"supports_custom_images"`
	SupportsRegionSelection bool     `json:"supports_region_selection"`
	SupportsPublicIP        bool     `json:"supports_public_ip"`
	SupportsSSHKeys         bool     `json:"supports_ssh_keys"`
	SupportsStartStop       bool     `json:"supports_start_stop"`
	StartupScriptLimit      int      `json:"startup_script_limit,omitempty"`
	KnownRegions            []string `json:"known_regions,omitempty"`
}

// CostSummary contains cost information.
type CostSummary struct {
	CurrentHourly  float64            `json:"current_hourly"`
	TodayTotal     float64            `json:"today_total"`
	MonthTotal     float64            `json:"month_total"`
	ByProvider     map[string]float64 `json:"by_provider"`
	ByGPU          map[string]float64 `json:"by_gpu"`
	ProjectedMonth float64            `json:"projected_month"`
}
