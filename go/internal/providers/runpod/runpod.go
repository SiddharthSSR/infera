// Package runpod implements the RunPod GPU cloud provider.
package runpod

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/infera/infera/go/internal/egress"
	"github.com/infera/infera/go/internal/providers"
)

const (
	defaultEndpoint              = "https://api.runpod.io/graphql"
	pollInterval                 = 5 * time.Second
	readyTimeout                 = 10 * time.Minute
	priceReconciliationInterval  = 500 * time.Millisecond
	priceReconciliationTimeout   = 20 * time.Second
	provisionCleanupTimeout      = 15 * time.Second
	workspaceMountPath           = "/workspace"
	metadataAllowedCudaVersions  = "allowed_cuda_versions"
	metadataCostCurrency         = "cost_evidence_currency"
	metadataCostUnit             = "cost_evidence_unit"
	metadataCostSource           = "cost_evidence_source"
	metadataCostCapturedAt       = "cost_evidence_captured_at"
	metadataCostAdvertised       = "cost_evidence_advertised_usd_per_hour"
	metadataCostState            = "cost_evidence_state"
	metadataCapacityState        = "capacity_evidence_state"
	costCurrencyUSD              = "USD"
	costUnitInstanceHour         = "instance-hour"
	costSourceRunPodMachine      = "runpod.pod.machine.costPerHr"
	capacityStateAdvertised      = "advertised_not_confirmed"
	costStateConfirmed           = "confirmed"
	costStateConfirmedDrift      = "confirmed_price_drift"
	costStateConfirmedNoAdvert   = "confirmed_advertised_price_unavailable"
	insufficientMachineResources = "This machine does not have the resources to deploy your pod. Please try a different machine"
	placementUnavailable         = "There are no longer any instances available with the requested specifications. Please refresh and try again."
)

// Provider implements the RunPod GPU provider.
type Provider struct {
	apiKey     string
	endpoint   string
	httpClient *http.Client
	hfToken    string

	pricePollInterval time.Duration
	pricePollTimeout  time.Duration
	cleanupTimeout    time.Duration
}

// Config for RunPod provider.
type Config struct {
	APIKey   string
	Endpoint string
	HFToken  string
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

	endpoint := strings.TrimSpace(config.Endpoint)
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	parsedEndpoint, err := url.ParseRequestURI(endpoint)
	if err != nil || egress.ValidateURL(parsedEndpoint, []string{"https"}) != nil {
		return nil, &providers.ProviderError{Provider: providers.ProviderRunPod, Code: providers.ProviderErrorInvalidConfig, Message: "RunPod endpoint must be a public HTTPS URL"}
	}

	return &Provider{
		apiKey:            config.APIKey,
		endpoint:          endpoint,
		httpClient:        egress.NewPublicClient(egress.ClientOptions{Timeout: 30 * time.Second, AllowedSchemes: []string{"https"}}),
		hfToken:           strings.TrimSpace(config.HFToken),
		pricePollInterval: priceReconciliationInterval,
		pricePollTimeout:  priceReconciliationTimeout,
		cleanupTimeout:    provisionCleanupTimeout,
	}, nil
}

// Factory creates a RunPod provider from generic config.
func Factory(config providers.ProviderConfig) (providers.Provider, error) {
	return New(Config{
		APIKey:   config.APIKey,
		Endpoint: config.Endpoint,
		HFToken:  config.APISecret,
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

const runPodGPUOfferingFields = `
	id
	displayName
	memoryInGb
	securePrice
	communityPrice
	secureSpotPrice
	communitySpotPrice
	maxGpuCountCommunityCloud
	maxGpuCountSecureCloud
	lowestPrice1: lowestPrice(input: { gpuCount: 1 }) {
		minimumBidPrice
		uninterruptablePrice
	}
	lowestPrice2: lowestPrice(input: { gpuCount: 2 }) {
		minimumBidPrice
		uninterruptablePrice
	}
	lowestPrice4: lowestPrice(input: { gpuCount: 4 }) {
		minimumBidPrice
		uninterruptablePrice
	}
	lowestPrice8: lowestPrice(input: { gpuCount: 8 }) {
		minimumBidPrice
		uninterruptablePrice
	}
	lowestPrice16: lowestPrice(input: { gpuCount: 16 }) {
		minimumBidPrice
		uninterruptablePrice
	}
`

// Provision creates a new GPU pod.
func (p *Provider) Provision(ctx context.Context, req *providers.ProvisionRequest) (*providers.Instance, error) {
	if req == nil {
		return nil, &providers.ProviderError{
			Provider: providers.ProviderRunPod,
			Code:     providers.ProviderErrorInvalidRequest,
			Message:  "provision request is required",
		}
	}
	if math.IsNaN(req.MaxCostHour) || math.IsInf(req.MaxCostHour, 0) || req.MaxCostHour < 0 {
		return nil, &providers.ProviderError{
			Provider: providers.ProviderRunPod,
			Code:     providers.ProviderErrorInvalidRequest,
			Message:  "max_cost_hour must be finite and non-negative",
		}
	}
	if req.MaxCostHour > 0 && !validHourlyPrice(req.MaxCostHour) {
		return nil, &providers.ProviderError{
			Provider: providers.ProviderRunPod,
			Code:     providers.ProviderErrorInvalidRequest,
			Message:  "max_cost_hour exceeds the supported price range",
		}
	}
	if req.SpotInstance {
		return nil, &providers.ProviderError{
			Provider: providers.ProviderRunPod,
			Code:     providers.ProviderErrorInvalidRequest,
			Message:  "RunPod spot placement is not supported by this adapter",
		}
	}

	// Map our GPU types to RunPod GPU IDs
	gpuTypeID := resolveRunPodGPUTypeID(req)
	if gpuTypeID == "" {
		return nil, &providers.ProviderError{
			Provider: providers.ProviderRunPod,
			Code:     "invalid_gpu_type",
			Message:  fmt.Sprintf("unsupported RunPod GPU type: %s", req.GPUType),
		}
	}
	if req.GPUCount <= 0 {
		return nil, &providers.ProviderError{
			Provider: providers.ProviderRunPod,
			Code:     providers.ProviderErrorInvalidRequest,
			Message:  "GPU count must be positive",
		}
	}

	dockerImage := strings.TrimSpace(req.DockerImage)
	if err := providers.ValidateWorkerImageRef(dockerImage); err != nil {
		return nil, &providers.ProviderError{
			Provider: providers.ProviderRunPod,
			Code:     providers.ProviderErrorInvalidRequest,
			Message:  err.Error(),
		}
	}
	offeringEvidence, err := p.requireProvisionOffering(ctx, req, gpuTypeID)
	if err != nil {
		return nil, err
	}

	// Build environment variables
	env := []map[string]string{
		{"key": "INFERA_ENGINE", "value": string(req.Engine.OrDefault())},
		{"key": "INFERA_HTTP_PORT", "value": "8081"},
		{"key": "INFERA_LOG_LEVEL", "value": "INFO"},
		{"key": "XDG_CACHE_HOME", "value": workspaceMountPath + "/.cache"},
		{"key": "HF_HOME", "value": workspaceMountPath + "/.cache/huggingface"},
		{"key": "HUGGINGFACE_HUB_CACHE", "value": workspaceMountPath + "/.cache/huggingface/hub"},
		{"key": "TRANSFORMERS_CACHE", "value": workspaceMountPath + "/.cache/huggingface/hub"},
		{"key": "TORCH_HOME", "value": workspaceMountPath + "/.cache/torch"},
	}

	// Add gateway address for worker registration
	gatewayAddress := strings.TrimSpace(req.GatewayAddress)
	if gatewayAddress != "" {
		env = append(env, map[string]string{
			"key": "INFERA_ROUTER_ADDRESS", "value": gatewayAddress,
		})
	}

	// Add shared worker auth token so worker can register/heartbeat on protected gateway endpoints.
	if workerToken := strings.TrimSpace(req.WorkerToken); workerToken != "" {
		env = append(env, map[string]string{
			"key": "INFERA_WORKER_SHARED_TOKEN", "value": workerToken,
		})
	}
	if releaseID := strings.TrimSpace(req.ReleaseID); releaseID != "" {
		env = append(env, map[string]string{"key": "INFERA_RELEASE_ID", "value": releaseID})
		env = append(env, map[string]string{"key": "INFERA_VERSION", "value": releaseID})
	}
	if protocolVersion := strings.TrimSpace(req.ProtocolVersion); protocolVersion != "" {
		env = append(env, map[string]string{"key": "INFERA_WORKER_PROTOCOL_VERSION", "value": protocolVersion})
	}

	// Add models to preload
	if len(req.Models) > 0 {
		// Convert to JSON array string
		modelsJSON, err := json.Marshal(req.Models)
		if err == nil {
			env = append(env, map[string]string{
				"key": "INFERA_PRELOAD_MODELS", "value": string(modelsJSON),
			})
			env = append(env, map[string]string{
				"key": "INFERA_ALLOWED_MODELS", "value": string(modelsJSON),
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
		env = append(env, map[string]string{
			"key": "INFERA_ALLOWED_MODELS", "value": defaultModel,
		})
	}

	// A workspace-owned provider secret may be delegated for gated models.
	env = appendHuggingFaceEnv(env, p.hfToken)

	for key, value := range providers.WorkerRuntimeEnv(req) {
		env = append(env, map[string]string{
			"key": key, "value": value,
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
					costPerHr
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
		"cloudType":         "ALL",
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
	allowedCudaVersions := sanitizeAllowedCudaVersions(req.AllowedCudaVersions)
	if len(allowedCudaVersions) > 0 {
		input["allowedCudaVersions"] = allowedCudaVersions
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
				GPUDisplayName string   `json:"gpuDisplayName"`
				CostPerHr      *float64 `json:"costPerHr"`
			} `json:"machine"`
		} `json:"podFindAndDeployOnDemand"`
	}

	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	pod := result.PodFindAndDeployOnDemand

	// Use full pod ID for consistency
	podID := pod.ID
	if strings.TrimSpace(podID) == "" {
		return nil, &providers.ProviderError{
			Provider: providers.ProviderRunPod,
			Code:     providers.ProviderErrorGraphQLError,
			Message:  "RunPod create response omitted the pod ID",
		}
	}
	createdAt := time.Now().UTC()

	confirmedPrice, capturedAt, err := p.reconcileCreatedPrice(ctx, podID, pod.Machine.CostPerHr)
	if err != nil {
		return nil, p.cleanupCreatedPod(podID, err)
	}
	if exceedsHourlyCap(confirmedPrice, req.MaxCostHour) {
		primary := &providers.ProviderError{
			Provider: providers.ProviderRunPod,
			Code:     providers.ProviderErrorInvalidRequest,
			Message: fmt.Sprintf(
				"RunPod confirmed hourly price %s USD exceeds max_cost_hour %s USD",
				formatHourlyPrice(confirmedPrice),
				formatHourlyPrice(req.MaxCostHour),
			),
		}
		return nil, p.cleanupCreatedPod(podID, primary)
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
	metadata := map[string]string{
		"machine_id":           pod.MachineID,
		"image":                pod.ImageName,
		"workspace_mount":      workspaceMountPath,
		"volume_gb":            fmt.Sprintf("%d", volumeSize),
		metadataCostCurrency:   costCurrencyUSD,
		metadataCostUnit:       costUnitInstanceHour,
		metadataCostSource:     costSourceRunPodMachine,
		metadataCostCapturedAt: capturedAt.UTC().Format(time.RFC3339Nano),
		metadataCostAdvertised: formatHourlyPrice(offeringEvidence.AdvertisedPrice),
		metadataCostState:      reconciledCostState(offeringEvidence.AdvertisedPrice, confirmedPrice),
		metadataCapacityState:  capacityStateAdvertised,
	}
	if len(allowedCudaVersions) > 0 {
		metadata[metadataAllowedCudaVersions] = strings.Join(allowedCudaVersions, ",")
	}

	return &providers.Instance{
		ID:           podID, // Use full ID, not truncated
		ProviderID:   podID,
		Provider:     providers.ProviderRunPod,
		Name:         pod.Name,
		Status:       providers.InstanceStatusProvisioning,
		GPUType:      req.GPUType,
		GPUCount:     req.GPUCount,
		CostPerHour:  confirmedPrice,
		SpotInstance: req.SpotInstance,
		Models:       models,
		CreatedAt:    createdAt,
		Metadata:     metadata,
	}, nil
}

type provisionOfferingEvidence struct {
	AdvertisedPrice float64
}

func (p *Provider) requireProvisionOffering(ctx context.Context, req *providers.ProvisionRequest, gpuTypeID string) (provisionOfferingEvidence, error) {
	query := fmt.Sprintf(`
		query GpuPlacementEvidence {
			gpuTypes {
				%s
			}
		}
	`, runPodGPUOfferingFields)
	resp, err := p.graphQL(ctx, query, nil)
	if err != nil {
		return provisionOfferingEvidence{}, err
	}

	var result struct {
		GpuTypes []runpodGPUOfferingEvidence `json:"gpuTypes"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return provisionOfferingEvidence{}, &providers.ProviderError{
			Provider: providers.ProviderRunPod,
			Code:     providers.ProviderErrorGraphQLError,
			Message:  "RunPod returned malformed placement evidence",
		}
	}

	for _, gpu := range result.GpuTypes {
		if gpu.ID != gpuTypeID {
			continue
		}
		if !runPodGPUTypeMatches(req.GPUType, gpu.DisplayName) {
			return provisionOfferingEvidence{}, &providers.ProviderError{
				Provider: providers.ProviderRunPod,
				Code:     providers.ProviderErrorInvalidRequest,
				Message:  "RunPod offering GPU type does not match the requested GPU type",
			}
		}
		if gpu.MaxGPUCountCommunity == nil || gpu.MaxGPUCountSecureCloud == nil ||
			*gpu.MaxGPUCountCommunity < 0 || *gpu.MaxGPUCountSecureCloud < 0 {
			return provisionOfferingEvidence{}, &providers.ProviderError{
				Provider: providers.ProviderRunPod,
				Code:     providers.ProviderErrorGraphQLError,
				Message:  "RunPod returned malformed placement availability",
			}
		}
		if *gpu.MaxGPUCountCommunity < req.GPUCount && *gpu.MaxGPUCountSecureCloud < req.GPUCount {
			return provisionOfferingEvidence{}, &providers.ProviderError{
				Provider: providers.ProviderRunPod,
				Code:     providers.ProviderErrorCapacityUnavailable,
				Message:  "RunPod advertises insufficient availability for the requested GPU type",
			}
		}

		advertisedPrice := gpu.priceFor(req.GPUCount, req.SpotInstance)
		if req.MaxCostHour > 0 && !validHourlyPrice(advertisedPrice) {
			return provisionOfferingEvidence{}, &providers.ProviderError{
				Provider: providers.ProviderRunPod,
				Code:     providers.ProviderErrorServiceUnavailable,
				Message:  "RunPod did not provide trustworthy advertised hourly price evidence for the requested placement",
			}
		}
		if exceedsHourlyCap(advertisedPrice, req.MaxCostHour) {
			return provisionOfferingEvidence{}, &providers.ProviderError{
				Provider: providers.ProviderRunPod,
				Code:     providers.ProviderErrorInvalidRequest,
				Message: fmt.Sprintf(
					"RunPod advertised hourly price %s USD exceeds max_cost_hour %s USD",
					formatHourlyPrice(advertisedPrice),
					formatHourlyPrice(req.MaxCostHour),
				),
			}
		}
		return provisionOfferingEvidence{AdvertisedPrice: advertisedPrice}, nil
	}

	return provisionOfferingEvidence{}, &providers.ProviderError{
		Provider: providers.ProviderRunPod,
		Code:     providers.ProviderErrorGraphQLError,
		Message:  "RunPod placement evidence omitted the requested provider GPU ID",
	}
}

type runpodGPUOfferingEvidence struct {
	ID                     string             `json:"id"`
	DisplayName            string             `json:"displayName"`
	SecurePrice            *float64           `json:"securePrice"`
	CommunityPrice         *float64           `json:"communityPrice"`
	SecureSpotPrice        *float64           `json:"secureSpotPrice"`
	CommunitySpotPrice     *float64           `json:"communitySpotPrice"`
	MaxGPUCountCommunity   *int               `json:"maxGpuCountCommunityCloud"`
	MaxGPUCountSecureCloud *int               `json:"maxGpuCountSecureCloud"`
	LowestPrice1           *runpodLowestPrice `json:"lowestPrice1"`
	LowestPrice2           *runpodLowestPrice `json:"lowestPrice2"`
	LowestPrice4           *runpodLowestPrice `json:"lowestPrice4"`
	LowestPrice8           *runpodLowestPrice `json:"lowestPrice8"`
	LowestPrice16          *runpodLowestPrice `json:"lowestPrice16"`
}

func (e runpodGPUOfferingEvidence) priceFor(count int, spot bool) float64 {
	lowest := map[int]*runpodLowestPrice{
		1: e.LowestPrice1, 2: e.LowestPrice2, 4: e.LowestPrice4,
		8: e.LowestPrice8, 16: e.LowestPrice16,
	}[count]
	if spot {
		if price := spotPriceFromRunPodPrice(lowest); validHourlyPrice(price) {
			return price
		}
		return e.availableTierPrice(count, e.CommunitySpotPrice, e.SecureSpotPrice)
	}
	if price := priceFromRunPodPrice(lowest); validHourlyPrice(price) {
		return price
	}
	return e.availableTierPrice(count, e.CommunityPrice, e.SecurePrice)
}

func (e runpodGPUOfferingEvidence) availableTierPrice(count int, communityPrice, securePrice *float64) float64 {
	var price float64
	if e.MaxGPUCountCommunity != nil && *e.MaxGPUCountCommunity >= count &&
		communityPrice != nil && validHourlyPrice(*communityPrice) {
		price = *communityPrice
	}
	if e.MaxGPUCountSecureCloud != nil && *e.MaxGPUCountSecureCloud >= count &&
		securePrice != nil && validHourlyPrice(*securePrice) && *securePrice > price {
		// cloudType "ALL" may choose either tier, so cap enforcement uses the higher eligible price.
		price = *securePrice
	}
	if validHourlyPrice(price * float64(count)) {
		return price * float64(count)
	}
	return 0
}

func maxSupportedHourlyPrice() float64 {
	return float64(math.MaxInt64-1) / 1_000_000_000
}

func validHourlyPrice(price float64) bool {
	if price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
		return false
	}
	return price < maxSupportedHourlyPrice()
}

func hourlyPriceNano(price float64) int64 {
	if !validHourlyPrice(price) {
		return 0
	}
	return int64(math.Round(price * 1_000_000_000))
}

func exceedsHourlyCap(price, cap float64) bool {
	if cap <= 0 {
		return false
	}
	if !validHourlyPrice(price) {
		return true
	}
	if !validHourlyPrice(cap) {
		return false
	}
	return hourlyPriceNano(price) > hourlyPriceNano(cap)
}

func formatHourlyPrice(price float64) string {
	return strconv.FormatFloat(price, 'f', -1, 64)
}

func reconciledCostState(advertised, confirmed float64) string {
	if !validHourlyPrice(advertised) {
		return costStateConfirmedNoAdvert
	}
	if validHourlyPrice(advertised) && hourlyPriceNano(advertised) != hourlyPriceNano(confirmed) {
		return costStateConfirmedDrift
	}
	return costStateConfirmed
}

func (p *Provider) reconcileCreatedPrice(ctx context.Context, podID string, createPrice *float64) (float64, time.Time, error) {
	if createPrice != nil && validHourlyPrice(*createPrice) {
		return *createPrice, time.Now().UTC(), nil
	}

	timeout := p.pricePollTimeout
	if timeout <= 0 {
		timeout = priceReconciliationTimeout
	}
	interval := p.pricePollInterval
	if interval <= 0 {
		interval = priceReconciliationInterval
	}
	reconcileCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lastErr error
	for {
		instance, err := p.GetInstance(reconcileCtx, podID)
		if err == nil && instance != nil && validHourlyPrice(instance.CostPerHour) {
			return instance.CostPerHour, time.Now().UTC(), nil
		}
		if err != nil {
			lastErr = err
		}

		timer := time.NewTimer(interval)
		select {
		case <-reconcileCtx.Done():
			timer.Stop()
			message := "RunPod actual hourly price could not be confirmed within the bounded reconciliation window"
			if lastErr != nil {
				message += ": " + lastErr.Error()
			}
			return 0, time.Time{}, &providers.ProviderError{
				Provider: providers.ProviderRunPod,
				Code:     providers.ProviderErrorTimeout,
				Message:  message,
			}
		case <-timer.C:
		}
	}
}

func runPodGPUTypeMatches(requested providers.GPUType, displayName string) bool {
	advertised, known := mapDisplayNameToGPUType(displayName)
	if known {
		return advertised == requested
	}
	normalizedRequested := compactGPUDisplayName(string(requested))
	normalizedAdvertised := compactGPUDisplayName(displayName)
	return normalizedRequested != "" && strings.EqualFold(normalizedRequested, normalizedAdvertised)
}

func (p *Provider) cleanupCreatedPod(podID string, primary error) error {
	timeout := p.cleanupTimeout
	if timeout <= 0 {
		timeout = provisionCleanupTimeout
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := p.Terminate(cleanupCtx, podID); err != nil {
		return errors.Join(primary, fmt.Errorf("terminate RunPod pod %s after price validation failure: %w", podID, err))
	}
	return primary
}

func appendHuggingFaceEnv(env []map[string]string, token string) []map[string]string {
	token = strings.TrimSpace(token)
	if token == "" {
		return env
	}
	return append(env,
		map[string]string{"key": "HF_TOKEN", "value": token},
		map[string]string{"key": "HUGGING_FACE_HUB_TOKEN", "value": token},
	)
}

func sanitizeAllowedCudaVersions(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
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

	return p.resumePod(ctx, instanceID, gpuCount)
}

func (p *Provider) StartWithInstance(ctx context.Context, instance *providers.Instance) error {
	if instance == nil {
		return &providers.ProviderError{
			Provider: providers.ProviderRunPod,
			Code:     providers.ProviderErrorInvalidRequest,
			Message:  "instance metadata is required",
		}
	}
	instanceID := instance.ProviderID
	if instanceID == "" {
		instanceID = instance.ID
	}
	gpuCount := instance.GPUCount
	if gpuCount <= 0 {
		refreshed, err := p.GetInstance(ctx, instanceID)
		if err == nil && refreshed.GPUCount > 0 {
			gpuCount = refreshed.GPUCount
		}
	}
	if gpuCount <= 0 {
		gpuCount = 1
	}
	return p.resumePod(ctx, instanceID, gpuCount)
}

func (p *Provider) resumePod(ctx context.Context, instanceID string, gpuCount int) error {
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

// ListOfferings returns advertised GPU configurations with live pricing from RunPod API.
func (p *Provider) ListOfferings(ctx context.Context) ([]*providers.GPUOffering, error) {
	query := fmt.Sprintf(`
		query GpuTypes {
			gpuTypes {
				%s
			}
		}
	`, runPodGPUOfferingFields)

	resp, err := p.graphQL(ctx, query, nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		GpuTypes []struct {
			ID                     string             `json:"id"`
			DisplayName            string             `json:"displayName"`
			MemoryInGb             int                `json:"memoryInGb"`
			SecurePrice            float64            `json:"securePrice"`
			CommunityPrice         float64            `json:"communityPrice"`
			SecureSpotPrice        float64            `json:"secureSpotPrice"`
			CommunitySpotPrice     float64            `json:"communitySpotPrice"`
			MaxGPUCountCommunity   int                `json:"maxGpuCountCommunityCloud"`
			MaxGPUCountSecureCloud int                `json:"maxGpuCountSecureCloud"`
			LowestPrice1           *runpodLowestPrice `json:"lowestPrice1"`
			LowestPrice2           *runpodLowestPrice `json:"lowestPrice2"`
			LowestPrice4           *runpodLowestPrice `json:"lowestPrice4"`
			LowestPrice8           *runpodLowestPrice `json:"lowestPrice8"`
			LowestPrice16          *runpodLowestPrice `json:"lowestPrice16"`
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

	offerings := make([]*providers.GPUOffering, 0, len(result.GpuTypes)*4)
	for _, gpu := range result.GpuTypes {
		gpuType := gpuTypeFromDisplayName(gpu.DisplayName)
		available := gpu.MaxGPUCountCommunity
		if available == 0 {
			available = gpu.MaxGPUCountSecureCloud
		}
		if available == 0 {
			continue
		}

		// Surface only live provider pricing. A missing price remains zero so
		// callers cannot mistake a static estimate for current provider evidence.
		price := gpu.CommunityPrice
		if price == 0 {
			price = gpu.SecurePrice
		}
		if price == 0 && gpu.LowestPrice1 != nil {
			price = gpu.LowestPrice1.UninterruptablePrice
		}

		spotPrice := gpu.CommunitySpotPrice
		if spotPrice == 0 {
			spotPrice = gpu.SecureSpotPrice
		}
		if spotPrice == 0 && gpu.LowestPrice1 != nil {
			spotPrice = gpu.LowestPrice1.MinimumBidPrice
		}

		priceByCount := map[int]float64{
			1:  price,
			2:  firstPositive(priceFromRunPodPrice(gpu.LowestPrice2), price*2),
			4:  firstPositive(priceFromRunPodPrice(gpu.LowestPrice4), price*4),
			8:  firstPositive(priceFromRunPodPrice(gpu.LowestPrice8), price*8),
			16: firstPositive(priceFromRunPodPrice(gpu.LowestPrice16), price*16),
		}
		spotByCount := map[int]float64{
			1:  spotPrice,
			2:  firstPositive(spotPriceFromRunPodPrice(gpu.LowestPrice2), spotPrice*2),
			4:  firstPositive(spotPriceFromRunPodPrice(gpu.LowestPrice4), spotPrice*4),
			8:  firstPositive(spotPriceFromRunPodPrice(gpu.LowestPrice8), spotPrice*8),
			16: firstPositive(spotPriceFromRunPodPrice(gpu.LowestPrice16), spotPrice*16),
		}

		for _, count := range practicalGPUCounts(available) {
			offerings = append(offerings, &providers.GPUOffering{
				Provider:          providers.ProviderRunPod,
				GPUType:           gpuType,
				DisplayName:       compactGPUDisplayName(gpu.DisplayName),
				ProviderGPUTypeID: gpu.ID,
				GPUCount:          count,
				MemoryGB:          gpu.MemoryInGb * count,
				CostPerHour:       firstPositive(priceByCount[count], price*float64(count)),
				SpotPrice:         firstPositive(spotByCount[count], spotPrice*float64(count)),
				Region:            "global",
				Available:         available,
			})
		}
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

func practicalGPUCounts(maxCount int) []int {
	if maxCount <= 0 {
		return nil
	}

	counts := []int{1}
	for _, count := range []int{2, 4, 8, 16} {
		if count <= maxCount {
			counts = append(counts, count)
		}
	}
	// Only append maxCount if it's a large non-standard value (≥8) and not already included.
	// Small odd counts like 3 or 5 are not standard RunPod configurations and would confuse users.
	if maxCount >= 8 && !slices.Contains(counts, maxCount) {
		counts = append(counts, maxCount)
	}
	slices.Sort(counts)
	return counts
}

func firstPositive(values ...float64) float64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

type runpodLowestPrice struct {
	MinimumBidPrice      float64 `json:"minimumBidPrice"`
	UninterruptablePrice float64 `json:"uninterruptablePrice"`
}

func priceFromRunPodPrice(price *runpodLowestPrice) float64 {
	if price == nil {
		return 0
	}
	return price.UninterruptablePrice
}

func spotPriceFromRunPodPrice(price *runpodLowestPrice) float64 {
	if price == nil {
		return 0
	}
	return price.MinimumBidPrice
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
		status := &providers.ProviderStatus{
			Provider:     providers.ProviderRunPod,
			Connected:    false,
			ErrorMessage: err.Error(),
		}
		var providerErr *providers.ProviderError
		if errors.As(err, &providerErr) {
			status.ErrorCode = providerErr.Code
		}
		return status, nil
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

	pods, err := p.ListInstances(ctx)
	if err != nil {
		status := &providers.ProviderStatus{
			Provider:     providers.ProviderRunPod,
			Connected:    false,
			AccountID:    result.Myself.ID,
			Balance:      result.Myself.CurrentSpendHr,
			QuotaLimit:   result.Myself.MachineQuota,
			ErrorMessage: err.Error(),
		}
		var providerErr *providers.ProviderError
		if errors.As(err, &providerErr) {
			status.ErrorCode = providerErr.Code
		}
		return status, nil
	}

	activeCount := 0
	for _, pod := range pods {
		if pod.Status == providers.InstanceStatusRunning {
			activeCount++
		}
	}

	return &providers.ProviderStatus{
		Provider:    providers.ProviderRunPod,
		Connected:   true,
		AccountID:   result.Myself.ID,
		Balance:     result.Myself.CurrentSpendHr, // This is spend, not balance
		ActiveCount: activeCount,
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

	respBody, err := providers.ReadResponseBody(providers.ProviderRunPod, resp.Body)
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

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, &providers.ProviderError{
			Provider:   providers.ProviderRunPod,
			Code:       providers.ProviderErrorAuthFailed,
			Message:    "RunPod rejected the supplied API key",
			StatusCode: resp.StatusCode,
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
		message := strings.TrimSpace(gqlResp.Errors[0].Message)
		code := providers.ProviderErrorGraphQLError
		if strings.EqualFold(message, insufficientMachineResources) ||
			strings.EqualFold(message, placementUnavailable) {
			code = providers.ProviderErrorCapacityUnavailable
		}
		return nil, &providers.ProviderError{
			Provider: providers.ProviderRunPod,
			Code:     code,
			Message:  message,
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
		instance.GPUType = gpuTypeFromDisplayName(pod.Machine.GPUDisplayName)
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

func resolveRunPodGPUTypeID(req *providers.ProvisionRequest) string {
	if req == nil {
		return ""
	}

	if providerGPUTypeID := strings.TrimSpace(req.ProviderGPUTypeID); providerGPUTypeID != "" {
		return providerGPUTypeID
	}

	if mappedGPUType := mapGPUType(req.GPUType); mappedGPUType != "" {
		return mappedGPUType
	}

	rawGPUType := strings.TrimSpace(string(req.GPUType))
	if strings.Contains(rawGPUType, " ") {
		return rawGPUType
	}

	return ""
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

func gpuTypeFromDisplayName(displayName string) providers.GPUType {
	if gpuType, ok := mapDisplayNameToGPUType(displayName); ok {
		return gpuType
	}

	normalized := strings.TrimSpace(displayName)
	if normalized == "" {
		return providers.GPUType("UNKNOWN_GPU")
	}
	return providers.GPUType(normalized)
}

func compactGPUDisplayName(displayName string) string {
	displayName = strings.TrimSpace(displayName)
	displayName = strings.TrimPrefix(displayName, "NVIDIA ")
	displayName = strings.TrimPrefix(displayName, "GeForce ")
	return displayName
}
