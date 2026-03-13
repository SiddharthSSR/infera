// Package providers defines types for GPU cloud providers.
package providers

import "time"

// ProviderType identifies a GPU cloud provider.
type ProviderType string

const (
	ProviderRunPod ProviderType = "runpod"
	ProviderVastAI ProviderType = "vastai"
	ProviderLambda ProviderType = "lambda"
	ProviderMock   ProviderType = "mock"
)

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

// Instance represents a GPU instance from any provider.
type Instance struct {
	ID         string         `json:"id"`
	ProviderID string         `json:"provider_id"`
	Provider   ProviderType   `json:"provider"`
	Name       string         `json:"name"`
	Status     InstanceStatus `json:"status"`

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
	WorkerID string   `json:"worker_id,omitempty"`
	Models   []string `json:"models,omitempty"`

	// Cost
	CostPerHour  float64 `json:"cost_per_hour"`
	SpotInstance bool    `json:"spot_instance"`

	// Timestamps
	CreatedAt time.Time  `json:"created_at"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	StoppedAt *time.Time `json:"stopped_at,omitempty"`

	// Provider-specific metadata
	Metadata map[string]string `json:"metadata,omitempty"`

	// Error info
	ErrorMessage string `json:"error_message,omitempty"`
}

// ProvisionRequest contains parameters for creating a new instance.
type ProvisionRequest struct {
	Name         string       `json:"name"`
	Provider     ProviderType `json:"provider"`
	GPUType      GPUType      `json:"gpu_type"`
	GPUCount     int          `json:"gpu_count"`
	Region       string       `json:"region,omitempty"`
	SpotInstance bool         `json:"spot_instance"`
	MaxCostHour  float64      `json:"max_cost_hour,omitempty"` // Budget limit

	// Worker configuration
	Models         []string `json:"models,omitempty"`
	DockerImage    string   `json:"docker_image,omitempty"`
	GatewayAddress string   `json:"gateway_address,omitempty"` // Address for worker to connect back

	// SSH key for access
	SSHPublicKey string `json:"ssh_public_key,omitempty"`

	// Provider-specific options
	Options map[string]string `json:"options,omitempty"`
}

// GPUOffering represents an available GPU configuration from a provider.
type GPUOffering struct {
	Provider    ProviderType `json:"provider"`
	GPUType     GPUType      `json:"gpu_type"`
	GPUCount    int          `json:"gpu_count"`
	VCPU        int          `json:"vcpu"`
	MemoryGB    int          `json:"memory_gb"`
	StorageGB   int          `json:"storage_gb"`
	CostPerHour float64      `json:"cost_per_hour"`
	SpotPrice   float64      `json:"spot_price,omitempty"`
	Region      string       `json:"region"`
	Available   int          `json:"available"`
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
