package auth

import (
	"encoding/json"
	"net/http"
	"strings"
)

// RegisterRoutes registers auth API routes on the mux.
// corsWrap is the CORS middleware from the gateway.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, corsWrap func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("/api/auth/keys", corsWrap(h.RequireAdmin(h.handleKeys)))
	mux.HandleFunc("/api/auth/keys/", corsWrap(h.RequireAdmin(h.handleKeyByID)))
}

func (h *Handler) handleKeys(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.handleListKeys(w, r)
	case http.MethodPost:
		h.handleCreateKey(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{
			"error": map[string]string{"message": "Method not allowed"},
		})
	}
}

func (h *Handler) handleKeyByID(w http.ResponseWriter, r *http.Request) {
	// Extract ID from /api/auth/keys/{id}
	path := strings.TrimPrefix(r.URL.Path, "/api/auth/keys/")
	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": map[string]string{"message": "Key ID required"},
		})
		return
	}

	switch r.Method {
	case http.MethodDelete:
		h.handleRevokeKey(w, r, path)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{
			"error": map[string]string{"message": "Method not allowed"},
		})
	}
}

func (h *Handler) handleListKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.store.ListKeys()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": map[string]string{"message": "Failed to list keys: " + err.Error()},
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"keys":  keys,
		"total": len(keys),
	})
}

func (h *Handler) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": map[string]string{"message": "Invalid JSON"},
		})
		return
	}

	if req.Role == "" {
		req.Role = "user"
	}

	fullKey, record, err := h.store.CreateKey(req.Name, req.Role)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": map[string]string{"message": err.Error()},
		})
		return
	}

	// Return full key ONCE — it cannot be retrieved again
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"key":    fullKey,
		"record": record,
	})
}

func (h *Handler) handleRevokeKey(w http.ResponseWriter, r *http.Request, id string) {
	if err := h.store.RevokeKey(id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"error": map[string]string{"message": err.Error()},
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Key revoked",
	})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
