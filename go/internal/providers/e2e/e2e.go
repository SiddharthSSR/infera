// Package e2e implements the E2E TIR GPU cloud provider.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/infera/infera/go/internal/egress"
	"github.com/infera/infera/go/internal/providers"
)

const (
	defaultEndpoint   = "https://tir.e2enetworks.com/api/v1"
	defaultLocation   = "Delhi"
	defaultHTTPPort   = 8081
	defaultStorageGB  = 50
	pollInterval      = 5 * time.Second
	readyTimeout      = 12 * time.Minute
	optionActiveIAM   = "active_iam"
	optionTeamID      = "team_id"
	optionProjectID   = "project_id"
	optionLocation    = "location"
	optionImageType   = "image_type"
	optionEnableSSH   = "enable_ssh"
	optionWorkerAddr  = "worker_address"
	optionIngressHost = "ingress_host"
)

// Provider implements the E2E TIR provider.
type Provider struct {
	apiKey     string
	authToken  string
	endpoint   string
	options    map[string]string
	httpClient *http.Client
}

// Config configures the E2E provider.
type Config struct {
	APIKey     string
	AuthToken  string
	Endpoint   string
	Options    map[string]string
	HTTPClient *http.Client
}

// New creates a new E2E provider.
func New(config Config) (*Provider, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, &providers.ProviderError{
			Provider: providers.ProviderE2E,
			Code:     providers.ProviderErrorMissingAPIKey,
			Message:  "E2E API key is required",
		}
	}
	if strings.TrimSpace(config.AuthToken) == "" {
		return nil, &providers.ProviderError{
			Provider: providers.ProviderE2E,
			Code:     providers.ProviderErrorAuthFailed,
			Message:  "E2E auth token is required",
		}
	}
	options := normalizeOptions(config.Options)
	for _, key := range []string{optionActiveIAM, optionTeamID, optionProjectID} {
		if strings.TrimSpace(options[key]) == "" {
			return nil, &providers.ProviderError{
				Provider: providers.ProviderE2E,
				Code:     providers.ProviderErrorInvalidConfig,
				Message:  fmt.Sprintf("E2E option %q is required", key),
			}
		}
	}
	endpoint := strings.TrimRight(strings.TrimSpace(config.Endpoint), "/")
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	parsedEndpoint, err := url.ParseRequestURI(endpoint)
	if err != nil || egress.ValidateURL(parsedEndpoint, []string{"https"}) != nil {
		return nil, &providers.ProviderError{Provider: providers.ProviderE2E, Code: providers.ProviderErrorInvalidConfig, Message: "E2E endpoint must be a public HTTPS URL"}
	}
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = egress.NewPublicClient(egress.ClientOptions{Timeout: 30 * time.Second, AllowedSchemes: []string{"https"}})
	}
	return &Provider{
		apiKey:     strings.TrimSpace(config.APIKey),
		authToken:  strings.TrimSpace(config.AuthToken),
		endpoint:   endpoint,
		options:    options,
		httpClient: httpClient,
	}, nil
}

// Factory creates a provider from the shared provider config.
func Factory(config providers.ProviderConfig) (providers.Provider, error) {
	return New(Config{
		APIKey:    config.APIKey,
		AuthToken: config.APISecret,
		Endpoint:  config.Endpoint,
		Options:   config.DefaultOpts,
	})
}

func init() {
	providers.RegisterProvider(providers.ProviderE2E, Factory)
}

// Name returns the provider type.
func (p *Provider) Name() providers.ProviderType {
	return providers.ProviderE2E
}

func (p *Provider) Provision(ctx context.Context, req *providers.ProvisionRequest) (*providers.Instance, error) {
	dockerImage := strings.TrimSpace(req.DockerImage)
	if err := providers.ValidateWorkerImageRef(dockerImage); err != nil {
		return nil, &providers.ProviderError{
			Provider: providers.ProviderE2E,
			Code:     providers.ProviderErrorInvalidRequest,
			Message:  err.Error(),
		}
	}

	planID := strings.TrimSpace(req.ProviderGPUTypeID)
	if planID == "" {
		selected, err := p.selectOffering(ctx, req)
		if err != nil {
			return nil, err
		}
		planID = selected.ProviderGPUTypeID
	}

	body := map[string]any{
		"name":       firstNonEmpty(strings.TrimSpace(req.Name), "infera-worker"),
		"plan_id":    planID,
		"location":   p.location(),
		"image_type": firstNonEmpty(p.options[optionImageType], "public"),
		"image_url":  dockerImage,
		"env":        p.buildEnv(req),
		"storage_gb": maxInt(defaultStorageGB, defaultStorageGB+(len(req.Models)*20)),
		"enable_ssh": p.options[optionEnableSSH] == "true",
	}
	if req.Region != "" {
		body["location"] = req.Region
	}

	var created tirNotebook
	if err := p.doJSON(ctx, http.MethodPost, p.notebooksPath(), map[string]string{
		optionActiveIAM: p.options[optionActiveIAM],
	}, body, &created); err != nil {
		return nil, err
	}
	instance := convertNotebook(&created)
	instance.Provider = providers.ProviderE2E
	instance.WorkspaceID = req.WorkspaceID
	instance.Models = append([]string(nil), req.Models...)
	if instance.GPUType == "" {
		instance.GPUType = req.GPUType
	}
	if instance.GPUCount == 0 {
		instance.GPUCount = maxInt(1, req.GPUCount)
	}
	if instance.Name == "" {
		instance.Name = body["name"].(string)
	}
	if instance.Metadata == nil {
		instance.Metadata = map[string]string{}
	}
	instance.Metadata["plan_id"] = planID
	return instance, nil
}

func (p *Provider) Terminate(ctx context.Context, instanceID string) error {
	return p.doJSON(ctx, http.MethodDelete, p.notebookPath(instanceID), map[string]string{
		optionActiveIAM: p.options[optionActiveIAM],
	}, nil, nil)
}

func (p *Provider) Start(ctx context.Context, instanceID string) error {
	return p.doJSON(ctx, http.MethodPost, p.notebookActionPath(instanceID, "start"), map[string]string{
		optionActiveIAM: p.options[optionActiveIAM],
	}, nil, nil)
}

func (p *Provider) Stop(ctx context.Context, instanceID string) error {
	return p.doJSON(ctx, http.MethodPost, p.notebookActionPath(instanceID, "stop"), map[string]string{
		optionActiveIAM: p.options[optionActiveIAM],
	}, nil, nil)
}

func (p *Provider) GetInstance(ctx context.Context, instanceID string) (*providers.Instance, error) {
	var notebook tirNotebook
	if err := p.doJSON(ctx, http.MethodGet, p.notebookPath(instanceID), map[string]string{
		optionActiveIAM: p.options[optionActiveIAM],
	}, nil, &notebook); err != nil {
		return nil, err
	}
	instance := convertNotebook(&notebook)
	instance.Provider = providers.ProviderE2E
	return instance, nil
}

func (p *Provider) ListInstances(ctx context.Context) ([]*providers.Instance, error) {
	notebooks, err := p.listNotebooks(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*providers.Instance, 0, len(notebooks))
	for i := range notebooks {
		instance := convertNotebook(&notebooks[i])
		instance.Provider = providers.ProviderE2E
		out = append(out, instance)
	}
	return out, nil
}

func (p *Provider) ListOfferings(ctx context.Context) ([]*providers.GPUOffering, error) {
	var payload json.RawMessage
	if err := p.doJSON(ctx, http.MethodGet, p.plansPath(), map[string]string{
		optionActiveIAM: p.options[optionActiveIAM],
		optionLocation:  p.location(),
	}, nil, &payload); err != nil {
		return nil, err
	}
	plans, err := decodePlanList(payload)
	if err != nil {
		return nil, err
	}
	out := make([]*providers.GPUOffering, 0, len(plans))
	for _, plan := range plans {
		if plan.ID == "" {
			continue
		}
		out = append(out, &providers.GPUOffering{
			Provider:          providers.ProviderE2E,
			GPUType:           normalizeGPUType(plan.GPUName, plan.GPUMemoryGB),
			DisplayName:       firstNonEmpty(strings.TrimSpace(plan.Name), strings.TrimSpace(plan.GPUName)),
			ProviderGPUTypeID: plan.ID,
			GPUCount:          maxInt(1, plan.GPUCount),
			VCPU:              maxInt(plan.VCPU, plan.CPU),
			MemoryGB:          maxInt(plan.MemoryGB, plan.RAMGB),
			StorageGB:         maxInt(plan.StorageGB, defaultStorageGB),
			CostPerHour:       firstPositive(plan.PricePerHour, plan.HourlyPrice),
			Region:            firstNonEmpty(plan.Location, p.location()),
			Available:         plan.Available,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CostPerHour == out[j].CostPerHour {
			if out[i].GPUType == out[j].GPUType {
				return out[i].GPUCount < out[j].GPUCount
			}
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
		if ok := asProviderError(err, &providerErr); ok {
			return &providers.ProviderStatus{
				Provider:     providers.ProviderE2E,
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
		Provider:     providers.ProviderE2E,
		Connected:    true,
		AccountID:    p.options[optionTeamID],
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
				Provider: providers.ProviderE2E,
				Code:     providers.ProviderErrorTimeout,
				Message:  "timed out waiting for E2E instance to become ready",
			}
		case <-ticker.C:
			instance, err := p.GetInstance(ctx, instanceID)
			if err != nil {
				return err
			}
			if instance.Status == providers.InstanceStatusError {
				return &providers.ProviderError{
					Provider: providers.ProviderE2E,
					Code:     providers.ProviderErrorInstanceError,
					Message:  firstNonEmpty(instance.ErrorMessage, "E2E instance entered error state"),
				}
			}
			if instance.Status == providers.InstanceStatusRunning {
				return nil
			}
		}
	}
}

func (p *Provider) listNotebooks(ctx context.Context) ([]tirNotebook, error) {
	var payload json.RawMessage
	if err := p.doJSON(ctx, http.MethodGet, p.notebooksPath(), map[string]string{
		optionActiveIAM: p.options[optionActiveIAM],
	}, nil, &payload); err != nil {
		return nil, err
	}
	return decodeNotebookList(payload)
}

func (p *Provider) selectOffering(ctx context.Context, req *providers.ProvisionRequest) (*providers.GPUOffering, error) {
	offerings, err := p.ListOfferings(ctx)
	if err != nil {
		return nil, err
	}
	var selected *providers.GPUOffering
	for _, offering := range offerings {
		if offering.GPUType != req.GPUType {
			continue
		}
		if maxInt(1, offering.GPUCount) != maxInt(1, req.GPUCount) {
			continue
		}
		if req.Region != "" && !strings.EqualFold(strings.TrimSpace(offering.Region), strings.TrimSpace(req.Region)) {
			continue
		}
		if selected == nil || offering.CostPerHour < selected.CostPerHour {
			selected = offering
		}
	}
	if selected == nil {
		return nil, &providers.ProviderError{
			Provider: providers.ProviderE2E,
			Code:     providers.ProviderErrorNotFound,
			Message:  fmt.Sprintf("no matching E2E GPU offering found for %s x%d", req.GPUType, maxInt(1, req.GPUCount)),
		}
	}
	return selected, nil
}

func (p *Provider) buildEnv(req *providers.ProvisionRequest) map[string]string {
	env := map[string]string{
		"INFERA_ENGINE":      "vllm",
		"INFERA_HTTP_PORT":   strconv.Itoa(defaultHTTPPort),
		"INFERA_LOG_LEVEL":   "INFO",
		"INFERA_E2E_TEAM":    p.options[optionTeamID],
		"INFERA_E2E_PROJECT": p.options[optionProjectID],
	}
	if gatewayAddress := strings.TrimSpace(req.GatewayAddress); gatewayAddress != "" {
		env["INFERA_ROUTER_ADDRESS"] = gatewayAddress
	}
	if workerToken := strings.TrimSpace(req.WorkerToken); workerToken != "" {
		env["INFERA_WORKER_SHARED_TOKEN"] = workerToken
	}
	if releaseID := strings.TrimSpace(req.ReleaseID); releaseID != "" {
		env["INFERA_RELEASE_ID"] = releaseID
		env["INFERA_VERSION"] = releaseID
	}
	if protocolVersion := strings.TrimSpace(req.ProtocolVersion); protocolVersion != "" {
		env["INFERA_WORKER_PROTOCOL_VERSION"] = protocolVersion
	}
	if workerAddress := strings.TrimSpace(p.options[optionWorkerAddr]); workerAddress != "" {
		env["INFERA_WORKER_ADDRESS"] = workerAddress
	} else if ingressHost := strings.TrimSpace(p.options[optionIngressHost]); ingressHost != "" {
		env["INFERA_E2E_INGRESS_HOST"] = ingressHost
	}
	if len(req.Models) > 0 {
		modelsJSON, _ := json.Marshal(req.Models)
		env["INFERA_PRELOAD_MODELS"] = string(modelsJSON)
	}
	for key, value := range providers.WorkerRuntimeEnv(req) {
		env[key] = value
	}
	return env
}

func (p *Provider) capabilities() providers.ProviderCapabilities {
	regions := []string{"Delhi", "Chennai"}
	if location := strings.TrimSpace(p.location()); location != "" {
		regions = []string{location}
	}
	return providers.ProviderCapabilities{
		SupportsSpot:            true,
		SupportsCustomImages:    true,
		SupportsRegionSelection: true,
		SupportsPublicIP:        true,
		SupportsSSHKeys:         true,
		SupportsStartStop:       true,
		KnownRegions:            regions,
	}
}

func (p *Provider) location() string {
	return firstNonEmpty(p.options[optionLocation], defaultLocation)
}

func (p *Provider) notebooksPath() string {
	return fmt.Sprintf("/teams/%s/projects/%s/notebooks/", p.options[optionTeamID], p.options[optionProjectID])
}

func (p *Provider) notebookPath(instanceID string) string {
	return p.notebooksPath() + strings.TrimSpace(instanceID) + "/"
}

func (p *Provider) notebookActionPath(instanceID, action string) string {
	return p.notebookPath(instanceID) + strings.Trim(strings.TrimSpace(action), "/") + "/"
}

func (p *Provider) plansPath() string {
	return fmt.Sprintf("/teams/%s/projects/%s/plans/", p.options[optionTeamID], p.options[optionProjectID])
}

func (p *Provider) doJSON(ctx context.Context, method, path string, query map[string]string, body any, out any) error {
	requestURL, err := url.Parse(p.endpoint + path)
	if err != nil {
		return &providers.ProviderError{
			Provider: providers.ProviderE2E,
			Code:     providers.ProviderErrorInvalidConfig,
			Message:  fmt.Sprintf("invalid E2E endpoint: %v", err),
		}
	}
	values := requestURL.Query()
	values.Set("api_key", p.apiKey)
	for key, value := range query {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			values.Set(key, trimmed)
		}
	}
	requestURL.RawQuery = values.Encode()

	var payload io.Reader
	if body != nil {
		bytesBody, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		payload = bytes.NewReader(bytesBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, requestURL.String(), payload)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.authToken)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return &providers.ProviderError{
			Provider: providers.ProviderE2E,
			Code:     providers.ProviderErrorRequestFailed,
			Message:  err.Error(),
		}
	}
	defer resp.Body.Close()

	bodyBytes, err := providers.ReadResponseBody(providers.ProviderE2E, resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return buildProviderError(resp.StatusCode, bodyBytes)
	}
	if out == nil {
		return nil
	}
	if rawOut, ok := out.(*json.RawMessage); ok {
		*rawOut = append((*rawOut)[:0], bodyBytes...)
		return nil
	}
	return decodePayload(bodyBytes, out)
}

func decodePayload(body []byte, out any) error {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && len(envelope.Data) > 0 {
		return json.Unmarshal(envelope.Data, out)
	}
	return json.Unmarshal(body, out)
}

func buildProviderError(statusCode int, body []byte) error {
	var payload struct {
		Message string `json:"message"`
		Error   struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &payload)
	message := firstNonEmpty(strings.TrimSpace(payload.Message), strings.TrimSpace(payload.Error.Message), strings.TrimSpace(string(body)))
	err := &providers.ProviderError{
		Provider:   providers.ProviderE2E,
		StatusCode: statusCode,
		Message:    message,
	}
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		err.Code = providers.ProviderErrorAuthFailed
	case http.StatusNotFound:
		err.Code = providers.ProviderErrorNotFound
	case http.StatusTooManyRequests:
		err.Code = providers.ProviderErrorRateLimited
	case http.StatusBadRequest:
		err.Code = providers.ProviderErrorInvalidRequest
	default:
		if statusCode >= 500 {
			err.Code = providers.ProviderErrorServiceUnavailable
		} else {
			err.Code = providers.ProviderErrorAPIError
		}
	}
	return err
}

func convertNotebook(notebook *tirNotebook) *providers.Instance {
	if notebook == nil {
		return &providers.Instance{Provider: providers.ProviderE2E}
	}
	instance := &providers.Instance{
		ID:           firstNonEmpty(notebook.ID, notebook.NotebookID),
		ProviderID:   firstNonEmpty(notebook.ID, notebook.NotebookID),
		Provider:     providers.ProviderE2E,
		Name:         firstNonEmpty(notebook.Name, notebook.DisplayName),
		Status:       mapNotebookStatus(notebook.Status),
		GPUType:      normalizeGPUType(notebook.GPUName, notebook.GPUMemoryGB),
		GPUCount:     maxInt(1, notebook.GPUCount),
		VCPU:         maxInt(notebook.VCPU, notebook.CPU),
		MemoryGB:     maxInt(notebook.MemoryGB, notebook.RAMGB),
		StorageGB:    maxInt(notebook.StorageGB, defaultStorageGB),
		PublicIP:     firstNonEmpty(notebook.PublicIP, notebook.IngressHost),
		HTTPPort:     firstPositiveInt(notebook.HTTPPort, defaultHTTPPort),
		SSHPort:      notebook.SSHPort,
		CostPerHour:  firstPositive(notebook.PricePerHour, notebook.HourlyPrice),
		SpotInstance: notebook.SpotInstance,
		Metadata:     map[string]string{},
		ErrorMessage: notebook.Error,
	}
	if createdAt := parseOptionalTime(notebook.CreatedAt); createdAt != nil {
		instance.CreatedAt = *createdAt
	} else {
		instance.CreatedAt = time.Now()
	}
	if startedAt := parseOptionalTime(notebook.StartedAt); startedAt != nil {
		instance.StartedAt = startedAt
	}
	if stoppedAt := parseOptionalTime(notebook.StoppedAt); stoppedAt != nil {
		instance.StoppedAt = stoppedAt
	}
	if notebook.PublicURL != "" {
		instance.Metadata["public_url"] = notebook.PublicURL
		if host, port, ok := splitHostPort(notebook.PublicURL); ok {
			instance.PublicIP = host
			instance.HTTPPort = port
		}
	}
	if notebook.Location != "" {
		instance.Metadata["location"] = notebook.Location
	}
	if notebook.PlanID != "" {
		instance.Metadata["plan_id"] = notebook.PlanID
	}
	if len(instance.Metadata) == 0 {
		instance.Metadata = nil
	}
	return instance
}

func mapNotebookStatus(value string) providers.InstanceStatus {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "running", "started", "ready", "active":
		return providers.InstanceStatusRunning
	case "pending", "queued":
		return providers.InstanceStatusPending
	case "provisioning", "creating", "starting":
		return providers.InstanceStatusProvisioning
	case "stopping":
		return providers.InstanceStatusStopping
	case "stopped":
		return providers.InstanceStatusStopped
	case "terminating", "deleting":
		return providers.InstanceStatusTerminating
	case "terminated", "deleted":
		return providers.InstanceStatusTerminated
	case "error", "failed":
		return providers.InstanceStatusError
	default:
		return providers.InstanceStatusPending
	}
}

func normalizeGPUType(name string, memoryGB int) providers.GPUType {
	normalized := strings.ToLower(strings.TrimSpace(name))
	switch {
	case strings.Contains(normalized, "4090"):
		return providers.GPURTX4090
	case strings.Contains(normalized, "4080"):
		return providers.GPURTX4080
	case strings.Contains(normalized, "h100"):
		return providers.GPUH100
	case strings.Contains(normalized, "l40s"):
		return providers.GPUL40S
	case strings.Contains(normalized, "a100") && memoryGB >= 80:
		return providers.GPUA100_80
	case strings.Contains(normalized, "a100"):
		return providers.GPUA100_40
	case strings.TrimSpace(name) != "":
		return providers.GPUType(strings.TrimSpace(name))
	default:
		return providers.GPUType("")
	}
}

func decodeNotebookList(payload json.RawMessage) ([]tirNotebook, error) {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(payload, &envelope); err == nil && len(envelope.Data) > 0 {
		return decodeNotebookList(envelope.Data)
	}
	var direct []tirNotebook
	if err := json.Unmarshal(payload, &direct); err == nil && direct != nil {
		return direct, nil
	}
	var wrapped struct {
		Items     []tirNotebook `json:"items"`
		Notebooks []tirNotebook `json:"notebooks"`
		Results   []tirNotebook `json:"results"`
	}
	if err := json.Unmarshal(payload, &wrapped); err != nil {
		return nil, err
	}
	switch {
	case len(wrapped.Items) > 0:
		return wrapped.Items, nil
	case len(wrapped.Notebooks) > 0:
		return wrapped.Notebooks, nil
	default:
		return wrapped.Results, nil
	}
}

func decodePlanList(payload json.RawMessage) ([]tirPlan, error) {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(payload, &envelope); err == nil && len(envelope.Data) > 0 {
		return decodePlanList(envelope.Data)
	}
	var direct []tirPlan
	if err := json.Unmarshal(payload, &direct); err == nil && direct != nil {
		return direct, nil
	}
	var wrapped struct {
		Items   []tirPlan `json:"items"`
		Plans   []tirPlan `json:"plans"`
		Results []tirPlan `json:"results"`
	}
	if err := json.Unmarshal(payload, &wrapped); err != nil {
		return nil, err
	}
	switch {
	case len(wrapped.Items) > 0:
		return wrapped.Items, nil
	case len(wrapped.Plans) > 0:
		return wrapped.Plans, nil
	default:
		return wrapped.Results, nil
	}
}

func parseOptionalTime(value string) *time.Time {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return nil
	}
	return &parsed
}

func splitHostPort(rawURL string) (string, int, bool) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return "", 0, false
	}
	host := parsed.Host
	if strings.Contains(host, ":") {
		name, portRaw, err := net.SplitHostPort(host)
		if err == nil {
			port, err := strconv.Atoi(portRaw)
			if err == nil {
				return name, port, true
			}
		}
	}
	if parsed.Scheme == "https" {
		return host, 443, true
	}
	return host, 80, true
}

func normalizeOptions(options map[string]string) map[string]string {
	if len(options) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(options))
	for key, value := range options {
		trimmedKey := strings.TrimSpace(key)
		trimmedValue := strings.TrimSpace(value)
		if trimmedKey == "" || trimmedValue == "" {
			continue
		}
		out[trimmedKey] = trimmedValue
	}
	return out
}

func asProviderError(err error, target **providers.ProviderError) bool {
	if err == nil {
		return false
	}
	typed, ok := err.(*providers.ProviderError)
	if ok {
		*target = typed
	}
	return ok
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
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

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func maxInt(values ...int) int {
	max := 0
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	return max
}

type tirNotebook struct {
	ID           string  `json:"id"`
	NotebookID   string  `json:"notebook_id"`
	Name         string  `json:"name"`
	DisplayName  string  `json:"display_name"`
	Status       string  `json:"status"`
	GPUName      string  `json:"gpu_name"`
	GPUMemoryGB  int     `json:"gpu_memory_gb"`
	GPUCount     int     `json:"gpu_count"`
	VCPU         int     `json:"vcpu"`
	CPU          int     `json:"cpu"`
	MemoryGB     int     `json:"memory_gb"`
	RAMGB        int     `json:"ram_gb"`
	StorageGB    int     `json:"storage_gb"`
	PublicIP     string  `json:"public_ip"`
	PublicURL    string  `json:"public_url"`
	IngressHost  string  `json:"ingress_host"`
	HTTPPort     int     `json:"http_port"`
	SSHPort      int     `json:"ssh_port"`
	PricePerHour float64 `json:"price_per_hour"`
	HourlyPrice  float64 `json:"hourly_price"`
	SpotInstance bool    `json:"spot_instance"`
	Location     string  `json:"location"`
	PlanID       string  `json:"plan_id"`
	Error        string  `json:"error"`
	CreatedAt    string  `json:"created_at"`
	StartedAt    string  `json:"started_at"`
	StoppedAt    string  `json:"stopped_at"`
}

type tirPlan struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	GPUName      string  `json:"gpu_name"`
	GPUMemoryGB  int     `json:"gpu_memory_gb"`
	GPUCount     int     `json:"gpu_count"`
	VCPU         int     `json:"vcpu"`
	CPU          int     `json:"cpu"`
	MemoryGB     int     `json:"memory_gb"`
	RAMGB        int     `json:"ram_gb"`
	StorageGB    int     `json:"storage_gb"`
	PricePerHour float64 `json:"price_per_hour"`
	HourlyPrice  float64 `json:"hourly_price"`
	Location     string  `json:"location"`
	Available    int     `json:"available"`
}
