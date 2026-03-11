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
	mux.HandleFunc("/api/auth/session", corsWrap(h.handleSession))
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

func (h *Handler) handleSession(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.handleCreateSession(w, r)
	case http.MethodGet:
		h.handleGetSession(w, r)
	case http.MethodDelete:
		h.handleDeleteSession(w, r)
	case http.MethodOptions:
		w.WriteHeader(http.StatusOK)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{
			"error": map[string]string{"message": "Method not allowed"},
		})
	}
}

func (h *Handler) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		APIKey string `json:"api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.APIKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": map[string]string{"message": "api_key is required"},
		})
		return
	}

	// Validate the API key
	keyRecord, err := h.store.ValidateKey(req.APIKey)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, "Invalid or revoked API key.")
		return
	}

	// Only admin keys can create dashboard sessions
	if keyRecord.Role != "admin" {
		writeAuthError(w, http.StatusForbidden, "Admin access required.")
		return
	}

	// Create session
	rawToken, session, err := h.store.CreateSession(keyRecord.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": map[string]string{"message": "Failed to create session"},
		})
		return
	}

	http.SetCookie(w, h.sessionCookie(rawToken))
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session": map[string]interface{}{
			"id":         session.ID,
			"expires_at": session.ExpiresAt,
		},
		"key": map[string]interface{}{
			"id":         keyRecord.ID,
			"key_prefix": keyRecord.KeyPrefix,
			"name":       keyRecord.Name,
			"role":       keyRecord.Role,
		},
	})
}

func (h *Handler) handleGetSession(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		writeAuthError(w, http.StatusUnauthorized, "No session cookie.")
		return
	}

	session, keyRecord, err := h.store.ValidateSession(cookie.Value)
	if err != nil {
		http.SetCookie(w, h.expiredSessionCookie())
		writeAuthError(w, http.StatusUnauthorized, "Invalid or expired session.")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session": map[string]interface{}{
			"id":         session.ID,
			"expires_at": session.ExpiresAt,
		},
		"key": map[string]interface{}{
			"id":         keyRecord.ID,
			"key_prefix": keyRecord.KeyPrefix,
			"name":       keyRecord.Name,
			"role":       keyRecord.Role,
		},
	})
}

func (h *Handler) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil && cookie.Value != "" {
		_ = h.store.DeleteSessionByToken(cookie.Value)
	}
	http.SetCookie(w, h.expiredSessionCookie())
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Session destroyed",
	})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
