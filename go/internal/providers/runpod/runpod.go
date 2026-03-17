// Package runpod implements the RunPod GPU cloud provider.
package runpod

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/infera/infera/go/internal/providers"
)

const (
	defaultEndpoint    = "https://api.runpod.io/graphql"
	pollInterval       = 5 * time.Second
	readyTimeout       = 10 * time.Minute
	defaultWorkerImage = "codingtensor/infera-worker:latest"
	workspaceMountPath = "/workspace"
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
	if gpuTypeID == "" {
		return nil, &providers.ProviderError{
			Provider: providers.ProviderRunPod,
			Code:     "invalid_gpu_type",
			Message:  fmt.Sprintf("unsupported RunPod GPU type: %s", req.GPUType),
		}
	}

	// Default to the custom infera worker image with vLLM 0.16+
	dockerImage := strings.TrimSpace(req.DockerImage)

	if dockerImage == "" {
		dockerImage = defaultWorkerImage
		slog.Warn("runpod.provision.using_fallback_worker_image",
			slog.String("image", dockerImage),
			slog.String("recommendation", "Set INFERA_WORKER_IMAGE to a pinned tag or digest"),
		)
	} else if usesFloatingImageRef(dockerImage) {
		slog.Warn("runpod.provision.using_unpinned_worker_image",
			slog.String("image", dockerImage),
			slog.String("recommendation", "Use a pinned tag or digest to keep warm restarts predictable"),
		)
	}

	// Build environment variables
	env := []map[string]string{
		{"key": "INFERA_ENGINE", "value": "vllm"},
		{"key": "INFERA_HTTP_PORT", "value": "8081"},
		{"key": "INFERA_LOG_LEVEL", "value": "INFO"},
		{"key": "XDG_CACHE_HOME", "value": workspaceMountPath + "/.cache"},
		{"key": "HF_HOME", "value": workspaceMountPath + "/.cache/huggingface"},
		{"key": "HUGGINGFACE_HUB_CACHE", "value": workspaceMountPath + "/.cache/huggingface/hub"},
		{"key": "TRANSFORMERS_CACHE", "value": workspaceMountPath + "/.cache/huggingface/hub"},
		{"key": "TORCH_HOME", "value": workspaceMountPath + "/.cache/torch"},
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

	// Add shared worker auth token so worker can register/heartbeat on protected gateway endpoints.
	if workerToken := os.Getenv("INFERA_WORKER_SHARED_TOKEN"); workerToken != "" {
		env = append(env, map[string]string{
			"key": "INFERA_WORKER_SHARED_TOKEN", "value": workerToken,
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
	volumeSize := containerDiskSize

	// Build the input for RunPod API
	// The persistent volume mounted at /workspace keeps model caches warm across stop/start.
	input := map[string]interface{}{
		"name":              req.Name,
		"imageName":         dockerImage,
		"gpuTypeId":         gpuTypeID,
		"gpuCount":          req.GPUCount,
		"containerDiskInGb": containerDiskSize,
		"volumeInGb":        volumeSize,
		"volumeMountPath":   workspaceMountPath,
		"minVcpuCount":      4,
		"minMemoryInGb":     16,
		"ports":             "8081/http,22/tcp",
		"env":               env,
		"supportPublicIp":   true, // Ensure we get a public IP for worker registration
	}

	// Log the request for debugging
	logInput := make(map[string]interface{}, len(input))
	for k, v := range input {
		logInput[k] = v
	}
	logInput["env"] = redactEnvForLog(env)
	slog.Info("runpod.provision.request",
		slog.String("provider", string(providers.ProviderRunPod)),
		slog.Any("input", logInput),
	)

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

	// Use full pod ID for consistency
	podID := pod.ID

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
		ID:           podID, // Use full ID, not truncated
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
			"machine_id":      pod.MachineID,
			"image":           pod.ImageName,
			"workspace_mount": workspaceMountPath,
			"volume_gb":       fmt.Sprintf("%d", volumeSize),
		},
	}, nil
}

func redactEnvForLog(env []map[string]string) []map[string]string {
	secretKeys := map[string]struct{}{
		"INFERA_WORKER_SHARED_TOKEN": {},
		"HF_TOKEN":                   {},
		"HUGGING_FACE_HUB_TOKEN":     {},
	}

	redacted := make([]map[string]string, 0, len(env))
	for _, pair := range env {
		copied := map[string]string{
			"key":   pair["key"],
			"value": pair["value"],
		}
		if _, isSecret := secretKeys[pair["key"]]; isSecret {
			copied["value"] = maskSecret(pair["value"])
		}
		redacted = append(redacted, copied)
	}
	return redacted
}

func maskSecret(value string) string {
	if value == "" {
		return ""
	}
	if len(value) <= 4 {
		return "****"
	}
	return value[:2] + "****" + value[len(value)-2:]
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
	gpuCount := 1
	instance, err := p.GetInstance(ctx, instanceID)
	if err == nil && instance.GPUCount > 0 {
		gpuCount = instance.GPUCount
	}

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
			"gpuCount": gpuCount,
		},
	}

	_, err = p.graphQL(ctx, query, variables)
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

// ListOfferings returns available GPU configurations with real pricing from RunPod API.
func (p *Provider) ListOfferings(ctx context.Context) ([]*providers.GPUOffering, error) {
	// Query gpuTypes with pricing fields from RunPod's GraphQL API
	query := `
		query GpuTypes {
			gpuTypes {
				id
				displayName
				memoryInGb
				securePrice
				communityPrice
				secureSpotPrice
				communitySpotPrice
				maxGpuCountCommunityCloud
				maxGpuCountSecureCloud
				lowestPrice(input: { gpuCount: 1 }) {
					minimumBidPrice
					uninterruptablePrice
				}
			}
		}
	`

	resp, err := p.graphQL(ctx, query, nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		GpuTypes []struct {
			ID                     string  `json:"id"`
			DisplayName            string  `json:"displayName"`
			MemoryInGb             int     `json:"memoryInGb"`
			SecurePrice            float64 `json:"securePrice"`
			CommunityPrice         float64 `json:"communityPrice"`
			SecureSpotPrice        float64 `json:"secureSpotPrice"`
			CommunitySpotPrice     float64 `json:"communitySpotPrice"`
			MaxGPUCountCommunity   int     `json:"maxGpuCountCommunityCloud"`
			MaxGPUCountSecureCloud int     `json:"maxGpuCountSecureCloud"`
			LowestPrice            *struct {
				MinimumBidPrice      float64 `json:"minimumBidPrice"`
				UninterruptablePrice float64 `json:"uninterruptablePrice"`
			} `json:"lowestPrice"`
		} `json:"gpuTypes"`
	}

	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse gpuTypes response: %w", err)
	}

	if len(result.GpuTypes) == 0 {
		return nil, &providers.ProviderError{
			Provider: providers.ProviderRunPod,
			Code:     "no_offerings",
			Message:  "RunPod returned no live GPU offerings",
		}
	}

	offerings := make([]*providers.GPUOffering, 0, len(result.GpuTypes))
	for _, gpu := range result.GpuTypes {
		gpuType, ok := mapDisplayNameToGPUType(gpu.DisplayName)
		if !ok {
			continue
		}
		available := gpu.MaxGPUCountCommunity
		if available == 0 {
			available = gpu.MaxGPUCountSecureCloud
		}
		if available == 0 {
			continue
		}

		// Determine the best on-demand price: prefer community, fall back to secure, then lowestPrice
		price := gpu.CommunityPrice
		if price == 0 {
			price = gpu.SecurePrice
		}
		if price == 0 && gpu.LowestPrice != nil {
			price = gpu.LowestPrice.UninterruptablePrice
		}
		if price == 0 {
			price = getEstimatedPrice(gpuType)
		}

		// Determine spot price: prefer community spot, fall back to secure spot, then lowestPrice bid
		spotPrice := gpu.CommunitySpotPrice
		if spotPrice == 0 {
			spotPrice = gpu.SecureSpotPrice
		}
		if spotPrice == 0 && gpu.LowestPrice != nil {
			spotPrice = gpu.LowestPrice.MinimumBidPrice
		}
		if spotPrice == 0 {
			spotPrice = price * 0.5
		}

		offerings = append(offerings, &providers.GPUOffering{
			Provider:    providers.ProviderRunPod,
			GPUType:     gpuType,
			GPUCount:    1,
			MemoryGB:    gpu.MemoryInGb,
			CostPerHour: price,
			SpotPrice:   spotPrice,
			Region:      "global",
			Available:   available,
		})
	}

	if len(offerings) == 0 {
		return nil, &providers.ProviderError{
			Provider: providers.ProviderRunPod,
			Code:     "no_supported_offerings",
			Message:  "RunPod returned no supported live GPU offerings",
		}
	}

	return offerings, nil
}

// getEstimatedPrice returns estimated hourly price for a GPU type.
// Used as fallback when the API doesn't return pricing.
// Prices reflect RunPod community cloud on-demand rates as of March 2026.
func getEstimatedPrice(gpuType providers.GPUType) float64 {
	switch gpuType {
	case providers.GPURTX4090:
		return 0.34
	case providers.GPURTX4080:
		return 0.29
	case providers.GPUA100_40:
		return 1.19
	case providers.GPUA100_80:
		return 1.19
	case providers.GPUH100:
		return 1.99
	case providers.GPUL40S:
		return 0.79
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
		Capabilities: providers.ProviderCapabilities{
			SupportsSpot:            false,
			SupportsCustomImages:    true,
			SupportsRegionSelection: true,
			SupportsPublicIP:        true,
			SupportsSSHKeys:         true,
			SupportsStartStop:       true,
		},
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
		GPUs          []struct {
			ID string `json:"id"`
		} `json:"gpus"`
		Ports []struct {
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
		ID:         pod.ID, // Use full ID, not truncated
		ProviderID: pod.ID,
		Provider:   providers.ProviderRunPod,
		Name:       pod.Name,
		Status:     mapStatus(pod.DesiredStatus),
		CreatedAt:  time.Now(), // Not available from API
	}

	if pod.Machine != nil {
		if gpuType, ok := mapDisplayNameToGPUType(pod.Machine.GPUDisplayName); ok {
			instance.GPUType = gpuType
		}
		instance.CostPerHour = pod.Machine.CostPerHr
	}

	if pod.Runtime != nil {
		if len(pod.Runtime.GPUs) > 0 {
			instance.GPUCount = len(pod.Runtime.GPUs)
		}
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

func usesFloatingImageRef(image string) bool {
	if strings.Contains(image, "@sha256:") {
		return false
	}
	lastSlash := strings.LastIndex(image, "/")
	lastColon := strings.LastIndex(image, ":")
	if lastColon <= lastSlash {
		return true
	}
	return strings.EqualFold(image[lastColon+1:], "latest")
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
		return ""
	}
}

func mapDisplayNameToGPUType(displayName string) (providers.GPUType, bool) {
	switch displayName {
	case "NVIDIA GeForce RTX 4090", "RTX 4090":
		return providers.GPURTX4090, true
	case "NVIDIA GeForce RTX 4080", "RTX 4080":
		return providers.GPURTX4080, true
	case "NVIDIA A100 40GB PCIe", "A100 40GB":
		return providers.GPUA100_40, true
	case "NVIDIA A100 80GB PCIe", "A100 80GB":
		return providers.GPUA100_80, true
	case "NVIDIA H100 PCIe", "H100":
		return providers.GPUH100, true
	case "NVIDIA L40S", "L40S":
		return providers.GPUL40S, true
	default:
		return "", false
	}
}
