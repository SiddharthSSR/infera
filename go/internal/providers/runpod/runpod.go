package runpod

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/infera/infera/go/internal/providers"
)

const (
	defaultEndpoint = "https://api.runpod.io/graphql"
	pollInterval    = 5 * time.Second
	readyTimeout    = 10 * time.Minute
)

type Provider struct {
	apiKey     string
	endpoint   string
	httpClient *http.Client
}

type Config struct {
	APIKey   string
	Endpoint string
}

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
		apiKey:     config.APIKey,
		endpoint:   endpoint,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func Factory(config providers.ProviderConfig) (providers.Provider, error) {
	return New(Config{APIKey: config.APIKey, Endpoint: config.Endpoint})
}

func init() {
	providers.RegisterProvider(providers.ProviderRunPod, Factory)
}

func (p *Provider) Name() providers.ProviderType {
	return providers.ProviderRunPod
}

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

func (p *Provider) Provision(ctx context.Context, req *providers.ProvisionRequest) (*providers.Instance, error) {
	gpuTypeID := mapGPUType(req.GPUType)

	query := `
		mutation CreatePod($input: PodFindAndDeployOnDemandInput!) {
			podFindAndDeployOnDemand(input: $input) {
				id
				name
				desiredStatus
				imageName
				machineId
				machine { gpuDisplayName }
			}
		}
	`

	variables := map[string]interface{}{
		"input": map[string]interface{}{
			"name":              req.Name,
			"imageName":         req.DockerImage,
			"gpuTypeId":         gpuTypeID,
			"gpuCount":          req.GPUCount,
			"volumeInGb":        50,
			"containerDiskInGb": 20,
			"minVcpuCount":      4,
			"minMemoryInGb":     16,
			"ports":             "8081/http,22/tcp",
			"env": []map[string]string{
				{"key": "INFERA_ENGINE", "value": "vllm"},
				{"key": "INFERA_HTTP_PORT", "value": "8081"},
			},
		},
	}

	if req.SpotInstance {
		variables["input"].(map[string]interface{})["bidPerGpu"] = req.MaxCostHour / float64(req.GPUCount)
	}

	resp, err := p.graphQL(ctx, query, variables)
	if err != nil {
		return nil, err
	}

	var result struct {
		PodFindAndDeployOnDemand struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Machine struct {
				GPUDisplayName string `json:"gpuDisplayName"`
			} `json:"machine"`
		} `json:"podFindAndDeployOnDemand"`
	}

	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	pod := result.PodFindAndDeployOnDemand

	return &providers.Instance{
		ID:           pod.ID[:8],
		ProviderID:   pod.ID,
		Provider:     providers.ProviderRunPod,
		Name:         pod.Name,
		Status:       providers.InstanceStatusProvisioning,
		GPUType:      req.GPUType,
		GPUCount:     req.GPUCount,
		CostPerHour:  req.MaxCostHour,
		SpotInstance: req.SpotInstance,
		Models:       req.Models,
		CreatedAt:    time.Now(),
	}, nil
}

func (p *Provider) Terminate(ctx context.Context, instanceID string) error {
	query := `mutation TerminatePod($input: PodTerminateInput!) { podTerminate(input: $input) }`
	variables := map[string]interface{}{"input": map[string]interface{}{"podId": instanceID}}
	_, err := p.graphQL(ctx, query, variables)
	return err
}

func (p *Provider) Start(ctx context.Context, instanceID string) error {
	query := `mutation ResumePod($input: PodResumeInput!) { podResume(input: $input) { id } }`
	variables := map[string]interface{}{"input": map[string]interface{}{"podId": instanceID, "gpuCount": 1}}
	_, err := p.graphQL(ctx, query, variables)
	return err
}

func (p *Provider) Stop(ctx context.Context, instanceID string) error {
	query := `mutation StopPod($input: PodStopInput!) { podStop(input: $input) { id } }`
	variables := map[string]interface{}{"input": map[string]interface{}{"podId": instanceID}}
	_, err := p.graphQL(ctx, query, variables)
	return err
}

func (p *Provider) GetInstance(ctx context.Context, instanceID string) (*providers.Instance, error) {
	query := `
		query GetPod($input: PodFilter!) {
			pod(input: $input) {
				id name desiredStatus
				runtime { uptimeInSeconds ports { ip isIpPublic privatePort publicPort } }
				machine { gpuDisplayName costPerHr }
			}
		}
	`
	variables := map[string]interface{}{"input": map[string]interface{}{"podId": instanceID}}

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
		return nil, &providers.ProviderError{Provider: providers.ProviderRunPod, Code: "not_found", Message: "pod not found"}
	}

	return p.convertPod(result.Pod), nil
}

func (p *Provider) ListInstances(ctx context.Context) ([]*providers.Instance, error) {
	query := `query GetPods { myself { pods { id name desiredStatus runtime { ports { ip isIpPublic privatePort publicPort } } machine { gpuDisplayName costPerHr } } } }`

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

func (p *Provider) ListOfferings(ctx context.Context) ([]*providers.GPUOffering, error) {
	query := `query GetGpuTypes { gpuTypes { id displayName memoryInGb lowestPrice { minimumBidPrice uninterruptablePrice } } }`

	resp, err := p.graphQL(ctx, query, nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		GpuTypes []struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
			MemoryInGb  int    `json:"memoryInGb"`
			LowestPrice struct {
				MinimumBidPrice      float64 `json:"minimumBidPrice"`
				UninterruptablePrice float64 `json:"uninterruptablePrice"`
			} `json:"lowestPrice"`
		} `json:"gpuTypes"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	offerings := make([]*providers.GPUOffering, 0, len(result.GpuTypes))
	for _, gpu := range result.GpuTypes {
		offerings = append(offerings, &providers.GPUOffering{
			Provider:    providers.ProviderRunPod,
			GPUType:     mapDisplayNameToGPUType(gpu.DisplayName),
			GPUCount:    1,
			MemoryGB:    gpu.MemoryInGb,
			CostPerHour: gpu.LowestPrice.UninterruptablePrice,
			SpotPrice:   gpu.LowestPrice.MinimumBidPrice,
			Region:      "global",
			Available:   -1,
		})
	}
	return offerings, nil
}

func (p *Provider) GetStatus(ctx context.Context) (*providers.ProviderStatus, error) {
	query := `query GetMyself { myself { id currentSpendPerHr machineQuota podCount } }`

	resp, err := p.graphQL(ctx, query, nil)
	if err != nil {
		return &providers.ProviderStatus{Provider: providers.ProviderRunPod, Connected: false, ErrorMessage: err.Error()}, nil
	}

	var result struct {
		Myself struct {
			ID             string  `json:"id"`
			CurrentSpendHr float64 `json:"currentSpendPerHr"`
			MachineQuota   int     `json:"machineQuota"`
			PodCount       int     `json:"podCount"`
		} `json:"myself"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &providers.ProviderStatus{
		Provider:    providers.ProviderRunPod,
		Connected:   true,
		AccountID:   result.Myself.ID,
		Balance:     result.Myself.CurrentSpendHr,
		ActiveCount: result.Myself.PodCount,
		QuotaLimit:  result.Myself.MachineQuota,
	}, nil
}

func (p *Provider) WaitForReady(ctx context.Context, instanceID string) error {
	timeout := time.After(readyTimeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return &providers.ProviderError{Provider: providers.ProviderRunPod, Code: "timeout", Message: "instance did not become ready"}
		case <-ticker.C:
			instance, err := p.GetInstance(ctx, instanceID)
			if err != nil {
				continue
			}
			if instance.Status == providers.InstanceStatusRunning {
				return nil
			}
			if instance.Status == providers.InstanceStatusError {
				return &providers.ProviderError{Provider: providers.ProviderRunPod, Code: "instance_error", Message: instance.ErrorMessage}
			}
		}
	}
}

func (p *Provider) graphQL(ctx context.Context, query string, variables map[string]interface{}) (*graphQLResponse, error) {
	reqBody := graphQLRequest{Query: query, Variables: variables}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequestWithContext(ctx, "POST", p.endpoint, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, &providers.ProviderError{Provider: providers.ProviderRunPod, Code: "request_failed", Message: err.Error()}
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == 429 {
		return nil, &providers.ProviderError{Provider: providers.ProviderRunPod, Code: "rate_limited", Message: "rate limited", RetryAfter: 60}
	}
	if resp.StatusCode != 200 {
		return nil, &providers.ProviderError{Provider: providers.ProviderRunPod, Code: "api_error", Message: string(respBody), StatusCode: resp.StatusCode}
	}

	var gqlResp graphQLResponse
	if err := json.Unmarshal(respBody, &gqlResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	if len(gqlResp.Errors) > 0 {
		return nil, &providers.ProviderError{Provider: providers.ProviderRunPod, Code: "graphql_error", Message: gqlResp.Errors[0].Message}
	}

	return &gqlResp, nil
}

type runpodPod struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	DesiredStatus string `json:"desiredStatus"`
	Runtime       *struct {
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
		ID:         pod.ID[:8],
		ProviderID: pod.ID,
		Provider:   providers.ProviderRunPod,
		Name:       pod.Name,
		Status:     mapStatus(pod.DesiredStatus),
		CreatedAt:  time.Now(),
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
	case providers.GPUA100_40:
		return "NVIDIA A100 40GB PCIe"
	case providers.GPUA100_80:
		return "NVIDIA A100 80GB PCIe"
	case providers.GPUH100:
		return "NVIDIA H100 PCIe"
	default:
		return "NVIDIA GeForce RTX 4090"
	}
}

func mapDisplayNameToGPUType(displayName string) providers.GPUType {
	switch displayName {
	case "NVIDIA GeForce RTX 4090", "RTX 4090":
		return providers.GPURTX4090
	case "NVIDIA A100 40GB PCIe", "A100 40GB":
		return providers.GPUA100_40
	case "NVIDIA A100 80GB PCIe", "A100 80GB":
		return providers.GPUA100_80
	case "NVIDIA H100 PCIe", "H100":
		return providers.GPUH100
	default:
		return providers.GPURTX4090
	}
}
