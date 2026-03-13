package gateway

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/infera/infera/go/internal/auth"
	"github.com/infera/infera/go/internal/providers"
)

type InstanceHandlers struct {
	manager *providers.Manager
}

func NewInstanceHandlers(manager *providers.Manager) *InstanceHandlers {
	return &InstanceHandlers{manager: manager}
}

func (h *InstanceHandlers) RegisterRoutes(mux *http.ServeMux, corsHandler func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("/api/instances", corsHandler(h.handleInstances))
	mux.HandleFunc("/api/instances/", corsHandler(h.handleInstanceByID))
	mux.HandleFunc("/api/instances/provision", corsHandler(h.handleProvision))
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

	instances := h.manager.ListInstances()
	response := make([]map[string]interface{}, 0, len(instances))
	for _, inst := range instances {
		response = append(response, instanceToMap(inst))
	}

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
		instance, exists := h.manager.GetInstance(instanceID)
		if !exists {
			writeError(w, http.StatusNotFound, "not_found", "Instance not found")
			return
		}
		writeJSON(w, http.StatusOK, instanceToMap(instance))

	case http.MethodDelete:
		if !requireGatewayPermission(w, r, auth.PermissionManageInfrastructure, "Infrastructure management access required") {
			return
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
		Name           string   `json:"name"`
		Provider       string   `json:"provider"`
		GPUType        string   `json:"gpu_type"`
		GPUCount       int      `json:"gpu_count"`
		Region         string   `json:"region"`
		SpotInstance   bool     `json:"spot_instance"`
		MaxCostHour    float64  `json:"max_cost_hour"`
		Models         []string `json:"models"`
		GatewayAddress string   `json:"gateway_address"`
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

	// Get gateway address from request, env, or default
	gatewayAddress := req.GatewayAddress
	if gatewayAddress == "" {
		gatewayAddress = os.Getenv("INFERA_GATEWAY_ADDRESS")
	}

	provisionReq := &providers.ProvisionRequest{
		Name:           req.Name,
		Provider:       providers.ProviderType(req.Provider),
		GPUType:        providers.GPUType(req.GPUType),
		GPUCount:       req.GPUCount,
		Region:         req.Region,
		SpotInstance:   req.SpotInstance,
		MaxCostHour:    req.MaxCostHour,
		Models:         req.Models,
		GatewayAddress: gatewayAddress,
	}

	if provisionReq.Name == "" {
		provisionReq.Name = "infera-worker"
	}
	if provisionReq.GPUCount == 0 {
		provisionReq.GPUCount = 1
	}

	instance, err := h.manager.Provision(r.Context(), provisionReq)
	if err != nil {
		writeProviderActionError(w, "provision_failed", err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"success":  true,
		"instance": instanceToMap(instance),
	})
}

func (h *InstanceHandlers) handleOfferings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed")
		return
	}
	if !requireGatewayPermission(w, r, auth.PermissionViewInfrastructure, "Infrastructure view access required") {
		return
	}

	offerings, _ := h.manager.ListOfferings(r.Context())

	response := make([]map[string]interface{}, 0, len(offerings))
	for _, o := range offerings {
		response = append(response, map[string]interface{}{
			"provider": o.Provider, "gpu_type": o.GPUType, "gpu_count": o.GPUCount,
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

	statuses := h.manager.GetProviderStatus(r.Context())

	response := make([]map[string]interface{}, 0, len(statuses))
	for _, s := range statuses {
		response = append(response, map[string]interface{}{
			"provider": s.Provider, "connected": s.Connected, "account_id": s.AccountID,
			"balance": s.Balance, "active_instances": s.ActiveCount,
			"quota_limit": s.QuotaLimit, "error": s.ErrorMessage, "error_code": s.ErrorCode,
			"capabilities": s.Capabilities,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"providers": response})
}

func (h *InstanceHandlers) handleCosts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed")
		return
	}
	if !requireGatewayPermission(w, r, auth.PermissionViewInfrastructure, "Infrastructure view access required") {
		return
	}
	summary := h.manager.GetCostSummary()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"current_hourly": summary.CurrentHourly, "today_total": summary.TodayTotal,
		"month_total": summary.MonthTotal, "projected_month": summary.ProjectedMonth,
		"by_provider": summary.ByProvider, "by_gpu": summary.ByGPU,
	})
}

func instanceToMap(inst *providers.Instance) map[string]interface{} {
	m := map[string]interface{}{
		"id": inst.ID, "provider_id": inst.ProviderID, "provider": inst.Provider,
		"name": inst.Name, "status": inst.Status, "gpu_type": inst.GPUType,
		"gpu_count": inst.GPUCount, "vcpu": inst.VCPU, "memory_gb": inst.MemoryGB,
		"storage_gb": inst.StorageGB, "public_ip": inst.PublicIP,
		"http_port": inst.HTTPPort, "ssh_port": inst.SSHPort,
		"worker_id": inst.WorkerID, "models": inst.Models,
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

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, errType, message string) {
	writeJSON(w, status, map[string]interface{}{"error": map[string]interface{}{"type": errType, "message": message}})
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
