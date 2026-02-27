// Package runpod implements the RunPod GPU cloud provider.
package runpod

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/infera/infera/go/internal/providers"
)

const (
	defaultEndpoint = "https://api.runpod.io/graphql"
	pollInterval    = 5 * time.Second
	readyTimeout    = 10 * time.Minute
)

// Provider implements the RunPod GPU provider.
type Provider struct {
	apiKey     string
	endpoint   string
	httpClient *http.Client
}

// Config for RunPod provider.
type Config struct {
	APIKey   string
	Endpoint string
}

// New creates a new RunPod provider.
func New(config Config) (*Provider, error) {
	if config.APIKey == "" {
		return nil, &providers.ProviderError{
			Provider: providers.ProviderRunPod,
			Code:     "missing_api_key",
			Message:  "RunPod API key is required",
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

// Factory creates a RunPod provider from generic config.
func Factory(config providers.ProviderConfig) (providers.Provider, error) {
	return New(Config{
		APIKey:   config.APIKey,
		Endpoint: config.Endpoint,
	})
}

// Register the provider factory.
func init() {
	providers.RegisterProvider(providers.ProviderRunPod, Factory)
}

// Name returns the provider type.
func (p *Provider) Name() providers.ProviderType {
	return providers.ProviderRunPod
}

// GraphQL request/response types
type graphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors,omitempty"`
}

// Provision creates a new GPU pod.
func (p *Provider) Provision(ctx context.Context, req *providers.ProvisionRequest) (*providers.Instance, error) {
	// Map our GPU types to RunPod GPU IDs
	gpuTypeID := mapGPUType(req.GPUType)

	// Use RunPod's base image with vLLM pre-installed for faster startup
	dockerImage := "runpod/pytorch:2.1.0-py3.10-cuda11.8.0-devel-ubuntu22.04"

	// If custom image provided, use it
	if req.DockerImage != "" {
		dockerImage = req.DockerImage
	}

	// Build environment variables
	env := []map[string]string{
		{"key": "INFERA_ENGINE", "value": "vllm"},
		{"key": "INFERA_HTTP_PORT", "value": "8081"},
		{"key": "INFERA_LOG_LEVEL", "value": "INFO"},
	}

	// Add gateway address for worker registration
	gatewayAddress := req.GatewayAddress
	if gatewayAddress == "" {
		gatewayAddress = os.Getenv("INFERA_GATEWAY_ADDRESS")
	}
	if gatewayAddress != "" {
		env = append(env, map[string]string{
			"key": "INFERA_ROUTER_ADDRESS", "value": gatewayAddress,
		})
	}

	// Add models to preload
	if len(req.Models) > 0 {
		// Convert to JSON array string
		modelsJSON, err := json.Marshal(req.Models)
		if err == nil {
			env = append(env, map[string]string{
				"key": "INFERA_PRELOAD_MODELS", "value": string(modelsJSON),
			})
		}
	} else {
		// Default model if none specified
		defaultModel := os.Getenv("INFERA_DEFAULT_MODEL")
		if defaultModel == "" {
			defaultModel = "mistralai/Mistral-7B-Instruct-v0.2"
		}
		env = append(env, map[string]string{
			"key": "INFERA_PRELOAD_MODELS", "value": defaultModel,
		})
	}

	// Add HuggingFace token if available (needed for gated models like Llama)
	if hfToken := os.Getenv("HF_TOKEN"); hfToken != "" {
		env = append(env, map[string]string{
			"key": "HF_TOKEN", "value": hfToken,
		})
		// Also set as HUGGING_FACE_HUB_TOKEN for compatibility
		env = append(env, map[string]string{
			"key": "HUGGING_FACE_HUB_TOKEN", "value": hfToken,
		})
	}

	// Build mutation - use the current RunPod API
	query := `
    mutation CreatePod($input: PodFindAndDeployOnDemandInput!) {
        podFindAndDeployOnDemand(input: $input) {
            id
            name
            desiredStatus
            imageName
            machineId
            machine {
                gpuDisplayName
            }
        }
    }
`

	// Calculate container disk size based on model (larger models need more space)
	// 50GB base + 20GB per model for HuggingFace cache
	containerDiskSize := 50
	if len(req.Models) > 0 {
		containerDiskSize = 50 + (len(req.Models) * 20)
	}

	// Build the input for RunPod API
	// We use containerDiskInGb for model storage (no persistent volume)
	// This avoids the "volume cannot be default" error
	input := map[string]interface{}{
		"name":              req.Name,
		"imageName":         dockerImage,
		"gpuTypeId":         gpuTypeID,
		"gpuCount":          req.GPUCount,
		"containerDiskInGb": containerDiskSize,
		"minVcpuCount":      4,
		"minMemoryInGb":     16,
		"ports":             "8081/http,22/tcp",
		"env":               env,
		"supportPublicIp":   true, // Ensure we get a public IP for worker registration
	}

	// Log the request for debugging
	inputJSON, _ := json.Marshal(input)
	fmt.Printf("[RunPod] Provisioning pod with input: %s\n", string(inputJSON))

	variables := map[string]interface{}{
		"input": input,
	}

	resp, err := p.graphQL(ctx, query, variables)
	if err != nil {
		return nil, err
	}

	var result struct {
		PodFindAndDeployOnDemand struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			DesiredStatus string `json:"desiredStatus"`
			ImageName     string `json:"imageName"`
			MachineID     string `json:"machineId"`
			Machine       struct {
				GPUDisplayName string `json:"gpuDisplayName"`
			} `json:"machine"`
		} `json:"podFindAndDeployOnDemand"`
	}

	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	pod := result.PodFindAndDeployOnDemand

	// Handle case where pod ID might be empty
	podID := pod.ID
	shortID := podID
	if len(podID) >= 8 {
		shortID = podID[:8]
	}

	// Use provided models or default
	models := req.Models
	if len(models) == 0 {
		defaultModel := os.Getenv("INFERA_DEFAULT_MODEL")
		if defaultModel == "" {
			defaultModel = "mistralai/Mistral-7B-Instruct-v0.2"
		}
		models = []string{defaultModel}
	}

	return &providers.Instance{
		ID:           shortID,
		ProviderID:   podID,
		Provider:     providers.ProviderRunPod,
		Name:         pod.Name,
		Status:       providers.InstanceStatusProvisioning,
		GPUType:      req.GPUType,
		GPUCount:     req.GPUCount,
		CostPerHour:  getEstimatedPrice(req.GPUType) * float64(req.GPUCount),
		SpotInstance: req.SpotInstance,
		Models:       models,
		CreatedAt:    time.Now(),
		Metadata: map[string]string{
			"machine_id": pod.MachineID,
			"image":      pod.ImageName,
		},
	}, nil
}

// Terminate destroys a pod.
func (p *Provider) Terminate(ctx context.Context, instanceID string) error {
	query := `
		mutation TerminatePod($input: PodTerminateInput!) {
			podTerminate(input: $input)
		}
	`

	variables := map[string]interface{}{
		"input": map[string]interface{}{
			"podId": instanceID,
		},
	}

	_, err := p.graphQL(ctx, query, variables)
	return err
}

// Start starts a stopped pod.
func (p *Provider) Start(ctx context.Context, instanceID string) error {
	query := `
		mutation ResumePod($input: PodResumeInput!) {
			podResume(input: $input) {
				id
				desiredStatus
			}
		}
	`

	variables := map[string]interface{}{
		"input": map[string]interface{}{
			"podId":    instanceID,
			"gpuCount": 1, // Resume with same GPU count
		},
	}

	_, err := p.graphQL(ctx, query, variables)
	return err
}

// Stop stops a running pod.
func (p *Provider) Stop(ctx context.Context, instanceID string) error {
	query := `
		mutation StopPod($input: PodStopInput!) {
			podStop(input: $input) {
				id
				desiredStatus
			}
		}
	`

	variables := map[string]interface{}{
		"input": map[string]interface{}{
			"podId": instanceID,
		},
	}

	_, err := p.graphQL(ctx, query, variables)
	return err
}

// GetInstance returns pod details.
func (p *Provider) GetInstance(ctx context.Context, instanceID string) (*providers.Instance, error) {
	query := `
		query GetPod($input: PodFilter!) {
			pod(input: $input) {
				id
				name
				desiredStatus
				runtime {
					uptimeInSeconds
					ports {
						ip
						isIpPublic
						privatePort
						publicPort
					}
					gpus {
						id
						gpuUtilPercent
						memoryUtilPercent
					}
				}
				machine {
					gpuDisplayName
					costPerHr
				}
			}
		}
	`

	variables := map[string]interface{}{
		"input": map[string]interface{}{
			"podId": instanceID,
		},
	}

	resp, err := p.graphQL(ctx, query, variables)
	if err != nil {
		return nil, err
	}

	var result struct {
		Pod *runpodPod `json:"pod"`
	}

	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if result.Pod == nil {
		return nil, &providers.ProviderError{
			Provider: providers.ProviderRunPod,
			Code:     "not_found",
			Message:  "pod not found",
		}
	}

	return p.convertPod(result.Pod), nil
}

// ListInstances returns all pods.
func (p *Provider) ListInstances(ctx context.Context) ([]*providers.Instance, error) {
	query := `
		query GetPods {
			myself {
				pods {
					id
					name
					desiredStatus
					runtime {
						uptimeInSeconds
						ports {
							ip
							isIpPublic
							privatePort
							publicPort
						}
					}
					machine {
						gpuDisplayName
						costPerHr
					}
				}
			}
		}
	`

	resp, err := p.graphQL(ctx, query, nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Myself struct {
			Pods []*runpodPod `json:"pods"`
		} `json:"myself"`
	}

	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	instances := make([]*providers.Instance, 0, len(result.Myself.Pods))
	for _, pod := range result.Myself.Pods {
		instances = append(instances, p.convertPod(pod))
	}

	return instances, nil
}

// ListOfferings returns available GPU configurations.
func (p *Provider) ListOfferings(ctx context.Context) ([]*providers.GPUOffering, error) {
	// RunPod's gpuTypes query to get available GPUs
	query := `
		query GpuTypes {
			gpuTypes {
				id
				displayName
				memoryInGb
			}
		}
	`

	resp, err := p.graphQL(ctx, query, nil)
	if err != nil {
		// If the query fails, return a static list of common RunPod GPUs
		return p.getStaticOfferings(), nil
	}

	var result struct {
		GpuTypes []struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
			MemoryInGb  int    `json:"memoryInGb"`
		} `json:"gpuTypes"`
	}

	if err := json.Unmarshal(resp.Data, &result); err != nil {
		// Return static offerings on parse error
		return p.getStaticOfferings(), nil
	}

	if len(result.GpuTypes) == 0 {
		return p.getStaticOfferings(), nil
	}

	offerings := make([]*providers.GPUOffering, 0, len(result.GpuTypes))
	for _, gpu := range result.GpuTypes {
		gpuType := mapDisplayNameToGPUType(gpu.DisplayName)
		price := getEstimatedPrice(gpuType)

		offerings = append(offerings, &providers.GPUOffering{
			Provider:    providers.ProviderRunPod,
			GPUType:     gpuType,
			GPUCount:    1,
			MemoryGB:    gpu.MemoryInGb,
			CostPerHour: price,
			SpotPrice:   price * 0.5, // Estimate spot at 50%
			Region:      "global",
			Available:   -1, // Unknown
		})
	}

	return offerings, nil
}

// getStaticOfferings returns a static list of common RunPod GPU offerings
func (p *Provider) getStaticOfferings() []*providers.GPUOffering {
	return []*providers.GPUOffering{
		{
			Provider:    providers.ProviderRunPod,
			GPUType:     providers.GPURTX4090,
			GPUCount:    1,
			MemoryGB:    24,
			CostPerHour: 0.44,
			SpotPrice:   0.22,
			Region:      "global",
			Available:   -1,
		},
		{
			Provider:    providers.ProviderRunPod,
			GPUType:     providers.GPUA100_40,
			GPUCount:    1,
			MemoryGB:    40,
			CostPerHour: 0.79,
			SpotPrice:   0.39,
			Region:      "global",
			Available:   -1,
		},
		{
			Provider:    providers.ProviderRunPod,
			GPUType:     providers.GPUA100_80,
			GPUCount:    1,
			MemoryGB:    80,
			CostPerHour: 1.19,
			SpotPrice:   0.59,
			Region:      "global",
			Available:   -1,
		},
		{
			Provider:    providers.ProviderRunPod,
			GPUType:     providers.GPUH100,
			GPUCount:    1,
			MemoryGB:    80,
			CostPerHour: 2.49,
			SpotPrice:   1.24,
			Region:      "global",
			Available:   -1,
		},
		{
			Provider:    providers.ProviderRunPod,
			GPUType:     providers.GPUL40S,
			GPUCount:    1,
			MemoryGB:    48,
			CostPerHour: 0.99,
			SpotPrice:   0.49,
			Region:      "global",
			Available:   -1,
		},
	}
}

// getEstimatedPrice returns estimated hourly price for a GPU type
func getEstimatedPrice(gpuType providers.GPUType) float64 {
	switch gpuType {
	case providers.GPURTX4090:
		return 0.44
	case providers.GPURTX4080:
		return 0.34
	case providers.GPUA100_40:
		return 0.79
	case providers.GPUA100_80:
		return 1.19
	case providers.GPUH100:
		return 2.49
	case providers.GPUL40S:
		return 0.99
	default:
		return 0.50
	}
}

// GetStatus returns RunPod account status.
func (p *Provider) GetStatus(ctx context.Context) (*providers.ProviderStatus, error) {
	query := `
		query GetMyself {
			myself {
				id
				currentSpendPerHr
				machineQuota
			}
		}
	`

	resp, err := p.graphQL(ctx, query, nil)
	if err != nil {
		return &providers.ProviderStatus{
			Provider:     providers.ProviderRunPod,
			Connected:    false,
			ErrorMessage: err.Error(),
		}, nil
	}

	var result struct {
		Myself struct {
			ID             string  `json:"id"`
			CurrentSpendHr float64 `json:"currentSpendPerHr"`
			MachineQuota   int     `json:"machineQuota"`
		} `json:"myself"`
	}

	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Get pod count from ListInstances instead
	pods, _ := p.ListInstances(ctx)
	podCount := len(pods)

	return &providers.ProviderStatus{
		Provider:    providers.ProviderRunPod,
		Connected:   true,
		AccountID:   result.Myself.ID,
		Balance:     result.Myself.CurrentSpendHr, // This is spend, not balance
		ActiveCount: podCount,
		QuotaLimit:  result.Myself.MachineQuota,
	}, nil
}

// WaitForReady waits until the pod is running.
func (p *Provider) WaitForReady(ctx context.Context, instanceID string) error {
	timeout := time.After(readyTimeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return &providers.ProviderError{
				Provider: providers.ProviderRunPod,
				Code:     "timeout",
				Message:  "instance did not become ready in time",
			}
		case <-ticker.C:
			instance, err := p.GetInstance(ctx, instanceID)
			if err != nil {
				continue // Retry
			}

			switch instance.Status {
			case providers.InstanceStatusRunning:
				return nil
			case providers.InstanceStatusError:
				return &providers.ProviderError{
					Provider: providers.ProviderRunPod,
					Code:     "instance_error",
					Message:  instance.ErrorMessage,
				}
			case providers.InstanceStatusTerminated:
				return &providers.ProviderError{
					Provider: providers.ProviderRunPod,
					Code:     "terminated",
					Message:  "instance was terminated",
				}
			}
		}
	}
}

// graphQL executes a GraphQL request.
func (p *Provider) graphQL(ctx context.Context, query string, variables map[string]interface{}) (*graphQLResponse, error) {
	reqBody := graphQLRequest{
		Query:     query,
		Variables: variables,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, &providers.ProviderError{
			Provider: providers.ProviderRunPod,
			Code:     "request_failed",
			Message:  err.Error(),
		}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == 429 {
		return nil, &providers.ProviderError{
			Provider:   providers.ProviderRunPod,
			Code:       "rate_limited",
			Message:    "rate limited",
			StatusCode: 429,
			RetryAfter: 60,
		}
	}

	if resp.StatusCode != 200 {
		return nil, &providers.ProviderError{
			Provider:   providers.ProviderRunPod,
			Code:       "api_error",
			Message:    string(respBody),
			StatusCode: resp.StatusCode,
		}
	}

	var gqlResp graphQLResponse
	if err := json.Unmarshal(respBody, &gqlResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(gqlResp.Errors) > 0 {
		return nil, &providers.ProviderError{
			Provider: providers.ProviderRunPod,
			Code:     "graphql_error",
			Message:  gqlResp.Errors[0].Message,
		}
	}

	return &gqlResp, nil
}

// Internal types
type runpodPod struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	DesiredStatus string `json:"desiredStatus"`
	Runtime       *struct {
		UptimeSeconds int `json:"uptimeInSeconds"`
		Ports         []struct {
			IP          string `json:"ip"`
			IsPublic    bool   `json:"isIpPublic"`
			PrivatePort int    `json:"privatePort"`
			PublicPort  int    `json:"publicPort"`
		} `json:"ports"`
	} `json:"runtime"`
	Machine *struct {
		GPUDisplayName string  `json:"gpuDisplayName"`
		CostPerHr      float64 `json:"costPerHr"`
	} `json:"machine"`
}

func (p *Provider) convertPod(pod *runpodPod) *providers.Instance {
	instance := &providers.Instance{
		ID:         pod.ID[:8],
		ProviderID: pod.ID,
		Provider:   providers.ProviderRunPod,
		Name:       pod.Name,
		Status:     mapStatus(pod.DesiredStatus),
		CreatedAt:  time.Now(), // Not available from API
	}

	if pod.Machine != nil {
		instance.GPUType = mapDisplayNameToGPUType(pod.Machine.GPUDisplayName)
		instance.CostPerHour = pod.Machine.CostPerHr
	}

	if pod.Runtime != nil {
		for _, port := range pod.Runtime.Ports {
			if port.IsPublic && port.PrivatePort == 8081 {
				instance.PublicIP = port.IP
				instance.HTTPPort = port.PublicPort
			}
			if port.IsPublic && port.PrivatePort == 22 {
				instance.SSHPort = port.PublicPort
			}
		}
	}

	return instance
}

func mapStatus(status string) providers.InstanceStatus {
	switch status {
	case "RUNNING":
		return providers.InstanceStatusRunning
	case "EXITED", "STOPPED":
		return providers.InstanceStatusStopped
	case "CREATED", "PENDING":
		return providers.InstanceStatusProvisioning
	case "TERMINATED":
		return providers.InstanceStatusTerminated
	default:
		return providers.InstanceStatusPending
	}
}

func mapGPUType(gpuType providers.GPUType) string {
	switch gpuType {
	case providers.GPURTX4090:
		return "NVIDIA GeForce RTX 4090"
	case providers.GPURTX4080:
		return "NVIDIA GeForce RTX 4080"
	case providers.GPUA100_40:
		return "NVIDIA A100 40GB PCIe"
	case providers.GPUA100_80:
		return "NVIDIA A100 80GB PCIe"
	case providers.GPUH100:
		return "NVIDIA H100 PCIe"
	case providers.GPUL40S:
		return "NVIDIA L40S"
	default:
		return "NVIDIA GeForce RTX 4090"
	}
}

func mapDisplayNameToGPUType(displayName string) providers.GPUType {
	switch displayName {
	case "NVIDIA GeForce RTX 4090", "RTX 4090":
		return providers.GPURTX4090
	case "NVIDIA GeForce RTX 4080", "RTX 4080":
		return providers.GPURTX4080
	case "NVIDIA A100 40GB PCIe", "A100 40GB":
		return providers.GPUA100_40
	case "NVIDIA A100 80GB PCIe", "A100 80GB":
		return providers.GPUA100_80
	case "NVIDIA H100 PCIe", "H100":
		return providers.GPUH100
	case "NVIDIA L40S", "L40S":
		return providers.GPUL40S
	default:
		return providers.GPURTX4090
	}
}
