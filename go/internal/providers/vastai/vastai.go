// Package vastai implements the Vast.ai GPU cloud provider.
package vastai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/infera/infera/go/internal/providers"
)

const (
	defaultEndpoint = "https://console.vast.ai/api/v0"
)

var (
	pollInterval = 5 * time.Second
	readyTimeout = 10 * time.Minute
)

// Provider implements the Vast.ai GPU provider.
type Provider struct {
	apiKey     string
	endpoint   string
	httpClient *http.Client
}

// Config for Vast.ai provider.
type Config struct {
	APIKey     string
	Endpoint   string
	HTTPClient *http.Client
}

// New creates a new Vast.ai provider.
func New(config Config) (*Provider, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, &providers.ProviderError{
			Provider: providers.ProviderVastAI,
			Code:     providers.ProviderErrorMissingAPIKey,
			Message:  "Vast.ai API key is required",
		}
	}

	endpoint := strings.TrimRight(strings.TrimSpace(config.Endpoint), "/")
	if endpoint == "" {
		endpoint = defaultEndpoint
	}

	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	return &Provider{
		apiKey:     strings.TrimSpace(config.APIKey),
		endpoint:   endpoint,
		httpClient: httpClient,
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

func (p *Provider) Provision(ctx context.Context, req *providers.ProvisionRequest) (*providers.Instance, error) {
	offerings, err := p.listOfferingsRaw(ctx)
	if err != nil {
		return nil, err
	}

	selected, err := chooseOffering(offerings, req)
	if err != nil {
		return nil, err
	}

	dockerImage := strings.TrimSpace(req.DockerImage)
	if err := providers.ValidateWorkerImageRef(dockerImage); err != nil {
		return nil, &providers.ProviderError{
			Provider: providers.ProviderVastAI,
			Code:     providers.ProviderErrorInvalidRequest,
			Message:  err.Error(),
		}
	}

	env := p.buildEnv(req)
	body := map[string]any{
		"offer_id":       selected.ID,
		"name":           req.Name,
		"image":          dockerImage,
		"disk_gb":        maxInt(50, 50+(len(req.Models)*20)),
		"gpu_count":      req.GPUCount,
		"public_ip":      true,
		"env":            env,
		"spot":           req.SpotInstance,
		"template_id":    "",
		"workspace_hint": req.WorkspaceID,
	}
	if req.SSHPublicKey != "" {
		body["ssh_public_key"] = req.SSHPublicKey
	}
	if req.Region != "" {
		body["region"] = req.Region
	}

	var created vastInstance
	if err := p.doJSON(ctx, http.MethodPost, "/instances", body, &created); err != nil {
		return nil, err
	}

	instance := convertInstance(&created)
	instance.Provider = providers.ProviderVastAI
	instance.ProviderID = created.ID
	instance.Name = firstNonEmpty(created.Name, req.Name)
	instance.GPUType = req.GPUType
	instance.GPUCount = req.GPUCount
	instance.CostPerHour = firstPositive(created.CostPerHour, selected.OnDemandPrice)
	instance.SpotInstance = req.SpotInstance
	instance.Models = cloneStrings(req.Models)
	instance.WorkspaceID = req.WorkspaceID
	if instance.Metadata == nil {
		instance.Metadata = map[string]string{}
	}
	instance.Metadata["offer_id"] = selected.ID
	return instance, nil
}

func (p *Provider) Terminate(ctx context.Context, instanceID string) error {
	return p.doJSON(ctx, http.MethodDelete, "/instances/"+instanceID, nil, nil)
}

func (p *Provider) Start(ctx context.Context, instanceID string) error {
	return p.doJSON(ctx, http.MethodPost, "/instances/"+instanceID+"/start", nil, nil)
}

func (p *Provider) Stop(ctx context.Context, instanceID string) error {
	return p.doJSON(ctx, http.MethodPost, "/instances/"+instanceID+"/stop", nil, nil)
}

func (p *Provider) GetInstance(ctx context.Context, instanceID string) (*providers.Instance, error) {
	var instance vastInstance
	if err := p.doJSON(ctx, http.MethodGet, "/instances/"+instanceID, nil, &instance); err != nil {
		return nil, err
	}
	converted := convertInstance(&instance)
	converted.Provider = providers.ProviderVastAI
	return converted, nil
}

func (p *Provider) ListInstances(ctx context.Context) ([]*providers.Instance, error) {
	var instances []vastInstance
	if err := p.doJSON(ctx, http.MethodGet, "/instances", nil, &instances); err != nil {
		return nil, err
	}
	converted := make([]*providers.Instance, 0, len(instances))
	for i := range instances {
		instance := convertInstance(&instances[i])
		instance.Provider = providers.ProviderVastAI
		converted = append(converted, instance)
	}
	return converted, nil
}

func (p *Provider) ListOfferings(ctx context.Context) ([]*providers.GPUOffering, error) {
	offers, err := p.listOfferingsRaw(ctx)
	if err != nil {
		return p.staticOfferings(), nil
	}
	if len(offers) == 0 {
		return p.staticOfferings(), nil
	}

	out := make([]*providers.GPUOffering, 0, len(offers))
	for _, offer := range offers {
		out = append(out, &providers.GPUOffering{
			Provider:    providers.ProviderVastAI,
			GPUType:     offer.GPUType,
			GPUCount:    maxInt(1, offer.GPUCount),
			VCPU:        offer.VCPU,
			MemoryGB:    offer.MemoryGB,
			StorageGB:   offer.StorageGB,
			CostPerHour: offer.OnDemandPrice,
			SpotPrice:   offer.SpotPrice,
			Region:      firstNonEmpty(offer.Region, "global"),
			Available:   offer.Available,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CostPerHour == out[j].CostPerHour {
			return out[i].GPUType < out[j].GPUType
		}
		return out[i].CostPerHour < out[j].CostPerHour
	})
	return out, nil
}

func (p *Provider) GetStatus(ctx context.Context) (*providers.ProviderStatus, error) {
	instances, err := p.ListInstances(ctx)
	if err != nil {
		var providerErr *providers.ProviderError
		if asProviderError(err, &providerErr) {
			return &providers.ProviderStatus{
				Provider:     providers.ProviderVastAI,
				Connected:    false,
				ErrorCode:    providerErr.Code,
				ErrorMessage: providerErr.Message,
				Capabilities: p.capabilities(),
			}, nil
		}
		return nil, err
	}

	activeCount := 0
	for _, instance := range instances {
		if instance.Status == providers.InstanceStatusRunning {
			activeCount++
		}
	}

	return &providers.ProviderStatus{
		Provider:     providers.ProviderVastAI,
		Connected:    true,
		ActiveCount:  activeCount,
		Capabilities: p.capabilities(),
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
			return &providers.ProviderError{
				Provider: providers.ProviderVastAI,
				Code:     providers.ProviderErrorTimeout,
				Message:  "instance did not become ready in time",
			}
		case <-ticker.C:
			instance, err := p.GetInstance(ctx, instanceID)
			if err != nil {
				var providerErr *providers.ProviderError
				if asProviderError(err, &providerErr) && providerErr.Code == providers.ProviderErrorNotFound {
					return err
				}
				continue
			}
			switch instance.Status {
			case providers.InstanceStatusRunning:
				return nil
			case providers.InstanceStatusError:
				return &providers.ProviderError{
					Provider: providers.ProviderVastAI,
					Code:     providers.ProviderErrorInstanceError,
					Message:  firstNonEmpty(instance.ErrorMessage, "instance entered error state"),
				}
			case providers.InstanceStatusTerminated:
				return &providers.ProviderError{
					Provider: providers.ProviderVastAI,
					Code:     providers.ProviderErrorTerminated,
					Message:  "instance was terminated",
				}
			}
		}
	}
}

func (p *Provider) capabilities() providers.ProviderCapabilities {
	return providers.ProviderCapabilities{
		SupportsSpot:            true,
		SupportsCustomImages:    true,
		SupportsRegionSelection: true,
		SupportsPublicIP:        true,
		SupportsSSHKeys:         true,
		SupportsStartStop:       true,
		KnownRegions:            []string{"global"},
	}
}

func (p *Provider) listOfferingsRaw(ctx context.Context) ([]vastOffer, error) {
	var offers []vastOffer
	if err := p.doJSON(ctx, http.MethodGet, "/offers", nil, &offers); err != nil {
		return nil, err
	}
	return offers, nil
}

func chooseOffering(offers []vastOffer, req *providers.ProvisionRequest) (*vastOffer, error) {
	var candidates []vastOffer
	for _, offer := range offers {
		if offer.GPUType != req.GPUType {
			continue
		}
		if req.Region != "" && offer.Region != "" && !strings.EqualFold(offer.Region, req.Region) {
			continue
		}
		price := offer.OnDemandPrice
		if req.SpotInstance && offer.SpotPrice > 0 {
			price = offer.SpotPrice
		}
		if req.MaxCostHour > 0 && price > req.MaxCostHour {
			continue
		}
		if offer.Available == 0 {
			continue
		}
		candidates = append(candidates, offer)
	}
	if len(candidates) == 0 {
		return nil, &providers.ProviderError{
			Provider: providers.ProviderVastAI,
			Code:     providers.ProviderErrorNotFound,
			Message:  "no matching Vast.ai offers found",
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		left := candidates[i].OnDemandPrice
		right := candidates[j].OnDemandPrice
		if req.SpotInstance {
			left = firstPositive(candidates[i].SpotPrice, left)
			right = firstPositive(candidates[j].SpotPrice, right)
		}
		if left == right {
			return candidates[i].ID < candidates[j].ID
		}
		return left < right
	})
	selected := candidates[0]
	return &selected, nil
}

func (p *Provider) buildEnv(req *providers.ProvisionRequest) map[string]string {
	env := map[string]string{
		"INFERA_ENGINE":    "vllm",
		"INFERA_HTTP_PORT": "8081",
		"INFERA_LOG_LEVEL": "INFO",
	}

	gatewayAddress := strings.TrimSpace(req.GatewayAddress)
	if gatewayAddress == "" {
		gatewayAddress = strings.TrimSpace(os.Getenv("INFERA_GATEWAY_ADDRESS"))
	}
	if gatewayAddress != "" {
		env["INFERA_ROUTER_ADDRESS"] = gatewayAddress
	}

	if workerToken := strings.TrimSpace(os.Getenv("INFERA_WORKER_SHARED_TOKEN")); workerToken != "" {
		env["INFERA_WORKER_SHARED_TOKEN"] = workerToken
	}

	if len(req.Models) > 0 {
		if modelsJSON, err := json.Marshal(req.Models); err == nil {
			env["INFERA_PRELOAD_MODELS"] = string(modelsJSON)
		}
	}

	if hfToken := strings.TrimSpace(os.Getenv("HF_TOKEN")); hfToken != "" {
		env["HF_TOKEN"] = hfToken
		env["HUGGING_FACE_HUB_TOKEN"] = hfToken
	}

	for key, value := range providers.WorkerRuntimeEnv(req) {
		env[key] = value
	}

	return env
}

func (p *Provider) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, p.endpoint+path, reader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return &providers.ProviderError{
			Provider: providers.ProviderVastAI,
			Code:     providers.ProviderErrorRequestFailed,
			Message:  err.Error(),
		}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return mapHTTPError(resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	if out == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	return nil
}

func mapHTTPError(statusCode int, body string) error {
	code := providers.ProviderErrorAPIError
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		code = providers.ProviderErrorAuthFailed
	case http.StatusNotFound:
		code = providers.ProviderErrorNotFound
	case http.StatusTooManyRequests:
		code = providers.ProviderErrorRateLimited
	case http.StatusBadRequest:
		code = providers.ProviderErrorInvalidRequest
	case http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout:
		code = providers.ProviderErrorServiceUnavailable
	}
	if body == "" {
		body = http.StatusText(statusCode)
	}
	return &providers.ProviderError{
		Provider:   providers.ProviderVastAI,
		Code:       code,
		Message:    body,
		StatusCode: statusCode,
		RetryAfter: retryAfterForStatus(statusCode),
	}
}

func retryAfterForStatus(statusCode int) int {
	if statusCode == http.StatusTooManyRequests {
		return 60
	}
	return 0
}

func convertInstance(instance *vastInstance) *providers.Instance {
	if instance == nil {
		return nil
	}
	createdAt := time.Now()
	if parsed, err := parseProviderTime(instance.CreatedAt); err == nil {
		createdAt = parsed
	}
	return &providers.Instance{
		ID:           instance.ID,
		ProviderID:   instance.ID,
		Provider:     providers.ProviderVastAI,
		Name:         instance.Name,
		Status:       mapStatus(instance.Status),
		GPUType:      instance.GPUType,
		GPUCount:     maxInt(1, instance.GPUCount),
		VCPU:         instance.VCPU,
		MemoryGB:     instance.MemoryGB,
		StorageGB:    instance.StorageGB,
		PublicIP:     instance.PublicIP,
		SSHPort:      instance.SSHPort,
		HTTPPort:     instance.HTTPPort,
		CostPerHour:  instance.CostPerHour,
		SpotInstance: instance.Spot,
		CreatedAt:    createdAt,
		Metadata: map[string]string{
			"region": firstNonEmpty(instance.Region, "global"),
		},
		ErrorMessage: instance.ErrorMessage,
	}
}

func (p *Provider) staticOfferings() []*providers.GPUOffering {
	return []*providers.GPUOffering{
		{Provider: providers.ProviderVastAI, GPUType: providers.GPURTX4090, GPUCount: 1, MemoryGB: 24, CostPerHour: 0.30, SpotPrice: 0.18, Region: "global", Available: -1},
		{Provider: providers.ProviderVastAI, GPUType: providers.GPURTX4080, GPUCount: 1, MemoryGB: 16, CostPerHour: 0.25, SpotPrice: 0.15, Region: "global", Available: -1},
		{Provider: providers.ProviderVastAI, GPUType: providers.GPUA100_40, GPUCount: 1, MemoryGB: 40, CostPerHour: 0.95, SpotPrice: 0.60, Region: "global", Available: -1},
		{Provider: providers.ProviderVastAI, GPUType: providers.GPUA100_80, GPUCount: 1, MemoryGB: 80, CostPerHour: 1.20, SpotPrice: 0.75, Region: "global", Available: -1},
		{Provider: providers.ProviderVastAI, GPUType: providers.GPUH100, GPUCount: 1, MemoryGB: 80, CostPerHour: 1.90, SpotPrice: 1.25, Region: "global", Available: -1},
		{Provider: providers.ProviderVastAI, GPUType: providers.GPUL40S, GPUCount: 1, MemoryGB: 48, CostPerHour: 0.70, SpotPrice: 0.45, Region: "global", Available: -1},
	}
}

type vastOffer struct {
	ID            string            `json:"id"`
	GPUType       providers.GPUType `json:"gpu_type"`
	GPUCount      int               `json:"gpu_count"`
	VCPU          int               `json:"vcpu"`
	MemoryGB      int               `json:"memory_gb"`
	StorageGB     int               `json:"storage_gb"`
	OnDemandPrice float64           `json:"cost_per_hour"`
	SpotPrice     float64           `json:"spot_price"`
	Region        string            `json:"region"`
	Available     int               `json:"available"`
}

type vastInstance struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Status       string            `json:"status"`
	GPUType      providers.GPUType `json:"gpu_type"`
	GPUCount     int               `json:"gpu_count"`
	VCPU         int               `json:"vcpu"`
	MemoryGB     int               `json:"memory_gb"`
	StorageGB    int               `json:"storage_gb"`
	PublicIP     string            `json:"public_ip"`
	SSHPort      int               `json:"ssh_port"`
	HTTPPort     int               `json:"http_port"`
	CostPerHour  float64           `json:"cost_per_hour"`
	Spot         bool              `json:"spot"`
	Region       string            `json:"region"`
	CreatedAt    string            `json:"created_at"`
	ErrorMessage string            `json:"error_message"`
	Env          map[string]string `json:"env,omitempty"`
}

func mapStatus(status string) providers.InstanceStatus {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "running":
		return providers.InstanceStatusRunning
	case "stopped":
		return providers.InstanceStatusStopped
	case "stopping":
		return providers.InstanceStatusStopping
	case "terminated", "destroyed":
		return providers.InstanceStatusTerminated
	case "pending", "creating":
		return providers.InstanceStatusPending
	case "loading", "starting", "provisioning":
		return providers.InstanceStatusProvisioning
	case "error", "failed":
		return providers.InstanceStatusError
	default:
		return providers.InstanceStatusPending
	}
}

func parseProviderTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported time format")
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstPositive(values ...float64) float64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func asProviderError(err error, target **providers.ProviderError) bool {
	if err == nil {
		return false
	}
	if providerErr, ok := err.(*providers.ProviderError); ok {
		*target = providerErr
		return true
	}
	return false
}
