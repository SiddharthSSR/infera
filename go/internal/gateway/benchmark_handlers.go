package gateway

import (
	"encoding/json"
	"net/http"

	"github.com/infera/infera/go/internal/auth"
)

func (h *InstanceHandlers) handleBenchmarkCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed")
		return
	}
	if !requireGatewayPermission(w, r, auth.PermissionViewInfrastructure, "Infrastructure view access required") {
		return
	}
	payload, err := h.benchmarkService.CatalogPayload()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "catalog_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"catalog": payload,
	})
}

func (h *InstanceHandlers) handleBenchmarkValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST is allowed")
		return
	}
	if !requireGatewayPermission(w, r, auth.PermissionViewInfrastructure, "Infrastructure view access required") {
		return
	}
	var raw json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON")
		return
	}
	payload, err := h.benchmarkService.ValidateSuite(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_suite", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (h *InstanceHandlers) handleBenchmarkCompare(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST is allowed")
		return
	}
	if !requireGatewayPermission(w, r, auth.PermissionViewInfrastructure, "Infrastructure view access required") {
		return
	}
	var req struct {
		Objective string            `json:"objective"`
		Indexes   []json.RawMessage `json:"indexes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON")
		return
	}
	if len(req.Indexes) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "indexes is required")
		return
	}
	indexes := make([][]byte, 0, len(req.Indexes))
	for _, raw := range req.Indexes {
		indexes = append(indexes, []byte(raw))
	}
	payload, err := h.benchmarkService.CompareResultIndexes(indexes, req.Objective)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_comparison", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, payload)
}
