package gateway

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/infera/infera/go/internal/auth"
	"github.com/infera/infera/go/internal/deployments"
	"github.com/infera/infera/go/internal/providers"
)

type InstanceHandlers struct {
	manager          *providers.Manager
	deploymentStore  deploymentHistoryStore
	benchmarkService BenchmarkService
}

func NewInstanceHandlers(manager *providers.Manager) *InstanceHandlers {
	return &InstanceHandlers{
		manager:          manager,
		benchmarkService: defaultBenchmarkService{},
	}
}

func (h *InstanceHandlers) SetDeploymentStore(store deploymentHistoryStore) {
	h.deploymentStore = store
}

func (h *InstanceHandlers) RegisterRoutes(mux *http.ServeMux, corsHandler func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("/api/instances", corsHandler(h.handleInstances))
	mux.HandleFunc("/api/instances/", corsHandler(h.handleInstanceByID))
	mux.HandleFunc("/api/instances/provision", corsHandler(h.handleProvision))
	mux.HandleFunc("/api/benchmarks/catalog", corsHandler(h.handleBenchmarkCatalog))
	mux.HandleFunc("/api/benchmarks/validate", corsHandler(h.handleBenchmarkValidate))
	mux.HandleFunc("/api/benchmarks/compare", corsHandler(h.handleBenchmarkCompare))
	mux.HandleFunc("/api/deployments", corsHandler(h.handleDeployments))
	mux.HandleFunc("/api/deployments/", corsHandler(h.handleDeploymentByID))
	mux.HandleFunc("/api/offerings", corsHandler(h.handleOfferings))
	mux.HandleFunc("/api/providers", corsHandler(h.handleProviders))
	mux.HandleFunc("/api/costs", corsHandler(h.handleCosts))
}

func (h *InstanceHandlers) handleInstances(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed")
		return
	}
	if !requireGatewayPermission(w, r, auth.PermissionViewInfrastructure, "Infrastructure view access required") {
		return
	}

	response := h.listInstanceEntriesForWorkspace(currentWorkspaceID(r))

	writeJSON(w, http.StatusOK, map[string]interface{}{"instances": response, "total": len(response)})
}

func (h *InstanceHandlers) handleInstanceByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/instances/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "Instance ID required")
		return
	}

	instanceID := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch r.Method {
	case http.MethodGet:
		if !requireGatewayPermission(w, r, auth.PermissionViewInfrastructure, "Infrastructure view access required") {
			return
		}
		current := auth.KeyFromContext(r.Context())
		instance, exists := h.manager.GetInstance(instanceID)
		if !exists {
			writeError(w, http.StatusNotFound, "not_found", "Instance not found")
			return
		}
		if current != nil && effectiveWorkspaceID(current) != auth.DefaultWorkspaceID && instance.WorkspaceID != effectiveWorkspaceID(current) {
			writeError(w, http.StatusNotFound, "not_found", "Instance not found")
			return
		}
		writeJSON(w, http.StatusOK, instanceToMap(instance))

	case http.MethodDelete:
		if !requireGatewayPermission(w, r, auth.PermissionManageInfrastructure, "Infrastructure management access required") {
			return
		}
		current := auth.KeyFromContext(r.Context())
		if current != nil && effectiveWorkspaceID(current) != auth.DefaultWorkspaceID {
			instance, exists := h.manager.GetInstance(instanceID)
			if !exists || instance.WorkspaceID != effectiveWorkspaceID(current) {
				writeError(w, http.StatusNotFound, "not_found", "Instance not found")
				return
			}
		}
		if err := h.manager.Terminate(r.Context(), instanceID); err != nil {
			writeProviderActionError(w, "terminate_failed", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "instance_id": instanceID})

	case http.MethodPost:
		if !requireGatewayPermission(w, r, auth.PermissionManageInfrastructure, "Infrastructure management access required") {
			return
		}
		current := auth.KeyFromContext(r.Context())
		if current != nil && effectiveWorkspaceID(current) != auth.DefaultWorkspaceID {
			instance, exists := h.manager.GetInstance(instanceID)
			if !exists || instance.WorkspaceID != effectiveWorkspaceID(current) {
				writeError(w, http.StatusNotFound, "not_found", "Instance not found")
				return
			}
		}
		switch action {
		case "start":
			if err := h.manager.Start(r.Context(), instanceID); err != nil {
				writeProviderActionError(w, "start_failed", err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
		case "stop":
			if err := h.manager.Stop(r.Context(), instanceID); err != nil {
				writeProviderActionError(w, "stop_failed", err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
		default:
			writeError(w, http.StatusBadRequest, "invalid_action", "Unknown action")
		}
	}
}

// handleProvision handles POST /api/instances/provision
func (h *InstanceHandlers) handleProvision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST is allowed")
		return
	}
	if !requireGatewayPermission(w, r, auth.PermissionManageInfrastructure, "Infrastructure management access required") {
		return
	}

	var req struct {
		Name                string            `json:"name"`
		Provider            string            `json:"provider"`
		Engine              string            `json:"engine"`
		GPUType             string            `json:"gpu_type"`
		ProviderGPUTypeID   string            `json:"provider_gpu_type_id"`
		GPUCount            int               `json:"gpu_count"`
		AllowedCudaVersions []string          `json:"allowed_cuda_versions"`
		Options             map[string]string `json:"options"`
		Region              string            `json:"region"`
		SpotInstance        bool              `json:"spot_instance"`
		MaxCostHour         float64           `json:"max_cost_hour"`
		Models              []string          `json:"models"`
		SelectedModelName   string            `json:"selected_model_name"`
		GatewayAddress      string            `json:"gateway_address"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON")
		return
	}

	// Validate required fields
	if req.GPUType == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "gpu_type is required")
		return
	}
	engine := providers.NormalizeInferenceEngine(req.Engine)
	if !engine.Valid() {
		writeError(w, http.StatusBadRequest, "invalid_request", "engine must be one of: vllm, sglang, tensorrt_llm, mock")
		return
	}
	if err := providers.ValidateRuntimeOptions(engine, req.Options); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	// Get gateway address from request, env, or default
	gatewayAddress := req.GatewayAddress
	if gatewayAddress == "" {
		gatewayAddress = os.Getenv("INFERA_GATEWAY_ADDRESS")
	}

	provisionReq := &providers.ProvisionRequest{
		Name:                req.Name,
		Provider:            providers.ProviderType(req.Provider),
		WorkspaceID:         currentWorkspaceID(r),
		GPUType:             providers.GPUType(req.GPUType),
		ProviderGPUTypeID:   strings.TrimSpace(req.ProviderGPUTypeID),
		GPUCount:            req.GPUCount,
		AllowedCudaVersions: slices.Clone(req.AllowedCudaVersions),
		Options:             cloneStringMap(req.Options),
		Region:              req.Region,
		SpotInstance:        req.SpotInstance,
		MaxCostHour:         req.MaxCostHour,
		Models:              req.Models,
		Engine:              engine,
		GatewayAddress:      gatewayAddress,
	}

	if provisionReq.Name == "" {
		provisionReq.Name = "infera-worker"
	}
	if provisionReq.GPUCount == 0 {
		provisionReq.GPUCount = 1
	}

	instance, err := h.manager.Provision(r.Context(), provisionReq)
	if err != nil {
		if h.deploymentStore != nil {
			if _, storeErr := h.deploymentStore.RecordFailedAttempt(
				currentWorkspaceID(r),
				currentKeyID(r),
				*provisionReq,
				req.SelectedModelName,
				deploymentFailureReason(err),
			); storeErr != nil {
				slog.Warn("deployments.record_failed_attempt_failed", slog.String("error", storeErr.Error()))
			}
		}
		writeProviderActionError(w, "provision_failed", err)
		return
	}

	if h.deploymentStore != nil {
		if _, storeErr := h.deploymentStore.RecordProvisionedAttempt(
			currentWorkspaceID(r),
			currentKeyID(r),
			*provisionReq,
			req.SelectedModelName,
			instance,
		); storeErr != nil {
			slog.Warn("deployments.record_provisioned_attempt_failed", slog.String("error", storeErr.Error()))
		}
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"success":  true,
		"instance": instanceToMap(instance),
	})
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func (h *InstanceHandlers) handleDeployments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed")
		return
	}
	if !requireGatewayPermission(w, r, auth.PermissionViewInfrastructure, "Infrastructure view access required") {
		return
	}
	if h.deploymentStore == nil {
		writeError(w, http.StatusServiceUnavailable, "deployment_history_unavailable", "Deployment history store is not configured")
		return
	}

	attempts, err := h.listDeploymentEntries(currentWorkspaceID(r), 25)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "deployment_history_failed", "Failed to load deployment history")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"attempts": attempts,
		"total":    len(attempts),
	})
}

func (h *InstanceHandlers) handleDeploymentByID(w http.ResponseWriter, r *http.Request) {
	if !requireGatewayPermission(w, r, auth.PermissionViewInfrastructure, "Infrastructure view access required") {
		return
	}
	if h.deploymentStore == nil {
		writeError(w, http.StatusServiceUnavailable, "deployment_history_unavailable", "Deployment history store is not configured")
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/deployments/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || strings.TrimSpace(parts[0]) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "Deployment attempt ID and action are required")
		return
	}

	attemptID := strings.TrimSpace(parts[0])
	action := strings.TrimSpace(parts[1])
	switch {
	case r.Method == http.MethodPut && action == "verification":
		var req struct {
			Status          string `json:"status"`
			VerifiedAt      string `json:"verified_at"`
			LatencyMS       *int64 `json:"latency_ms"`
			Model           string `json:"model"`
			ResponsePreview string `json:"response_preview"`
			Error           string `json:"error"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON")
			return
		}
		verifiedAt := time.Now().UTC()
		if strings.TrimSpace(req.VerifiedAt) != "" {
			parsed, err := time.Parse(time.RFC3339Nano, req.VerifiedAt)
			if err != nil {
				parsed, err = time.Parse(time.RFC3339, req.VerifiedAt)
				if err != nil {
					writeError(w, http.StatusBadRequest, "invalid_request", "verified_at must be a valid RFC3339 timestamp")
					return
				}
			}
			verifiedAt = parsed.UTC()
		}

		attempt, err := h.deploymentStore.UpdateVerification(currentWorkspaceID(r), attemptID, deployments.InferenceVerification{
			Status:          req.Status,
			VerifiedAt:      verifiedAt,
			LatencyMS:       req.LatencyMS,
			Model:           req.Model,
			ResponsePreview: req.ResponsePreview,
			Error:           req.Error,
		})
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Deployment attempt not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "deployment_history_failed", "Failed to update verification")
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"attempt": attempt})
	case r.Method == http.MethodPut && action == "auto-verification":
		var req struct {
			RequestedAt string `json:"requested_at"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON")
			return
		}
		requestedAt := time.Now().UTC()
		if strings.TrimSpace(req.RequestedAt) != "" {
			parsed, err := time.Parse(time.RFC3339Nano, req.RequestedAt)
			if err != nil {
				parsed, err = time.Parse(time.RFC3339, req.RequestedAt)
				if err != nil {
					writeError(w, http.StatusBadRequest, "invalid_request", "requested_at must be a valid RFC3339 timestamp")
					return
				}
			}
			requestedAt = parsed.UTC()
		}
		attempt, err := h.deploymentStore.MarkAutoVerificationRequested(currentWorkspaceID(r), attemptID, requestedAt)
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "Deployment attempt not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "deployment_history_failed", "Failed to mark auto verification")
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"attempt": attempt})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Unsupported deployment action")
	}
}

func (h *InstanceHandlers) handleOfferings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed")
		return
	}
	if !requireGatewayPermission(w, r, auth.PermissionViewInfrastructure, "Infrastructure view access required") {
		return
	}

	offerings, err := h.manager.ListOfferingsForWorkspace(r.Context(), currentWorkspaceID(r))
	if err != nil {
		writeProviderActionError(w, "offerings_unavailable", err)
		return
	}

	response := make([]map[string]interface{}, 0, len(offerings))
	for _, o := range offerings {
		response = append(response, map[string]interface{}{
			"provider": o.Provider, "gpu_type": o.GPUType, "gpu_count": o.GPUCount,
			"display_name": o.DisplayName, "provider_gpu_type_id": o.ProviderGPUTypeID,
			"vcpu": o.VCPU, "memory_gb": o.MemoryGB, "storage_gb": o.StorageGB,
			"cost_per_hour": o.CostPerHour, "spot_price": o.SpotPrice,
			"region": o.Region, "available": o.Available,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"offerings": response, "total": len(response)})
}

func (h *InstanceHandlers) handleProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed")
		return
	}
	if !requireGatewayPermission(w, r, auth.PermissionViewInfrastructure, "Infrastructure view access required") {
		return
	}

	response := h.listProviderEntries(r.Context(), currentWorkspaceID(r))
	writeJSON(w, http.StatusOK, map[string]interface{}{"providers": response})
}

func (h *InstanceHandlers) handleCosts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed")
		return
	}
	if !requireGatewayPermission(w, r, auth.PermissionViewUsage, "Usage access required") {
		return
	}
	summary := h.manager.GetCostSummary()
	current := auth.KeyFromContext(r.Context())
	if current != nil && effectiveWorkspaceID(current) != auth.DefaultWorkspaceID {
		summary = h.manager.GetCostSummaryForWorkspace(effectiveWorkspaceID(current))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"current_hourly": summary.CurrentHourly, "today_total": summary.TodayTotal,
		"month_total": summary.MonthTotal, "projected_month": summary.ProjectedMonth,
		"by_provider": summary.ByProvider, "by_gpu": summary.ByGPU,
	})
}

func instanceToMap(inst *providers.Instance) map[string]interface{} {
	m := map[string]interface{}{
		"id": inst.ID, "provider_id": inst.ProviderID, "provider": inst.Provider,
		"workspace_id": inst.WorkspaceID,
		"name":         inst.Name, "status": inst.Status, "gpu_type": inst.GPUType,
		"gpu_count": inst.GPUCount, "vcpu": inst.VCPU, "memory_gb": inst.MemoryGB,
		"storage_gb": inst.StorageGB, "public_ip": inst.PublicIP,
		"http_port": inst.HTTPPort, "ssh_port": inst.SSHPort,
		"worker_id": inst.WorkerID, "models": inst.Models, "engine": inst.Engine,
		"cost_per_hour": inst.CostPerHour, "spot_instance": inst.SpotInstance,
		"created_at": inst.CreatedAt, "error": inst.ErrorMessage,
	}
	if inst.StartedAt != nil {
		m["started_at"] = inst.StartedAt
	}
	if inst.StoppedAt != nil {
		m["stopped_at"] = inst.StoppedAt
	}
	return m
}

func currentWorkspaceID(r *http.Request) string {
	current := auth.KeyFromContext(r.Context())
	if current == nil {
		return auth.DefaultWorkspaceID
	}
	return effectiveWorkspaceID(current)
}

func currentKeyID(r *http.Request) string {
	current := auth.KeyFromContext(r.Context())
	if current == nil {
		return ""
	}
	return current.ID
}

func effectiveWorkspaceID(record *auth.KeyRecord) string {
	if record == nil || strings.TrimSpace(record.WorkspaceID) == "" {
		return auth.DefaultWorkspaceID
	}
	return record.WorkspaceID
}

func deploymentFailureReason(err error) string {
	var providerErr *providers.ProviderError
	if errors.As(err, &providerErr) && strings.TrimSpace(providerErr.Message) != "" {
		return providerErr.Message
	}
	return err.Error()
}

func writeProviderActionError(w http.ResponseWriter, fallbackType string, err error) {
	var providerErr *providers.ProviderError
	if !errors.As(err, &providerErr) {
		writeError(w, http.StatusInternalServerError, fallbackType, err.Error())
		return
	}

	payload := map[string]interface{}{
		"error": map[string]interface{}{
			"type":                providerErr.APIErrorType(),
			"message":             providerErr.Message,
			"provider":            providerErr.Provider,
			"provider_error_code": providerErr.Code,
			"retryable":           providerErr.IsRetryable(),
		},
	}
	if providerErr.RetryAfter > 0 {
		payload["error"].(map[string]interface{})["retry_after_seconds"] = providerErr.RetryAfter
	}
	writeJSON(w, providerErr.HTTPStatus(http.StatusInternalServerError), payload)
}

func requireGatewayPermission(w http.ResponseWriter, r *http.Request, permission, message string) bool {
	if !auth.HasPermission(auth.KeyFromContext(r.Context()), permission) {
		writeError(w, http.StatusForbidden, "forbidden", message)
		return false
	}
	return true
}
