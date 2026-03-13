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
	mux.HandleFunc("/api/auth/workspaces", corsWrap(h.RequireAdmin(h.handleWorkspaces)))
	mux.HandleFunc("/api/auth/workspaces/", corsWrap(h.RequireAdmin(h.handleWorkspaceByID)))
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

func (h *Handler) handleWorkspaces(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.handleListWorkspaces(w, r)
	case http.MethodPost:
		h.handleCreateWorkspace(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{
			"error": map[string]string{"message": "Method not allowed"},
		})
	}
}

func (h *Handler) handleWorkspaceByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/auth/workspaces/")
	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": map[string]string{"message": "Workspace path required"},
		})
		return
	}
	if !strings.HasSuffix(path, "/quota") {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"error": map[string]string{"message": "Not found"},
		})
		return
	}
	workspaceID := strings.TrimSuffix(path, "/quota")
	workspaceID = strings.TrimSuffix(workspaceID, "/")
	if workspaceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": map[string]string{"message": "Workspace ID required"},
		})
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.handleGetWorkspaceQuota(w, r, workspaceID)
	case http.MethodPut:
		h.handlePutWorkspaceQuota(w, r, workspaceID)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{
			"error": map[string]string{"message": "Method not allowed"},
		})
	}
}

func (h *Handler) handleListKeys(w http.ResponseWriter, r *http.Request) {
	current := KeyFromContext(r.Context())
	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspaceID == "" && current != nil {
		workspaceID = current.WorkspaceID
	}
	if current != nil && current.WorkspaceID != DefaultWorkspaceID && workspaceID != "" && workspaceID != current.WorkspaceID {
		writeAuthError(w, http.StatusForbidden, "Workspace-scoped admins can only list keys in their own workspace.")
		return
	}

	keys, err := h.store.ListKeysByWorkspace(workspaceID)
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
		Name        string `json:"name"`
		Role        string `json:"role"`
		WorkspaceID string `json:"workspace_id"`
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

	current := KeyFromContext(r.Context())
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" && current != nil {
		workspaceID = current.WorkspaceID
	}
	if current != nil && current.WorkspaceID != DefaultWorkspaceID && workspaceID != "" && workspaceID != current.WorkspaceID {
		writeAuthError(w, http.StatusForbidden, "Workspace-scoped admins can only create keys in their own workspace.")
		return
	}

	fullKey, record, err := h.store.CreateKeyInWorkspace(workspaceID, req.Name, req.Role)
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
	workspaceID := ""
	if current := KeyFromContext(r.Context()); current != nil {
		workspaceID = current.WorkspaceID
	}
	if err := h.store.RevokeKeyInWorkspace(id, workspaceID); err != nil {
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

func (h *Handler) handleListWorkspaces(w http.ResponseWriter, r *http.Request) {
	current := KeyFromContext(r.Context())
	workspaces, err := h.store.ListWorkspaces()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"error": map[string]string{"message": "Failed to list workspaces: " + err.Error()},
		})
		return
	}

	if current != nil && current.WorkspaceID != DefaultWorkspaceID {
		filtered := make([]*WorkspaceRecord, 0, 1)
		for _, workspace := range workspaces {
			if workspace.ID == current.WorkspaceID {
				filtered = append(filtered, workspace)
				break
			}
		}
		workspaces = filtered
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"workspaces": workspaces,
		"total":      len(workspaces),
	})
}

func (h *Handler) handleCreateWorkspace(w http.ResponseWriter, r *http.Request) {
	current := KeyFromContext(r.Context())
	if current != nil && current.WorkspaceID != DefaultWorkspaceID {
		writeAuthError(w, http.StatusForbidden, "Only platform admins can create workspaces.")
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": map[string]string{"message": "Invalid JSON"},
		})
		return
	}

	workspace, err := h.store.CreateWorkspace(req.Name)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": map[string]string{"message": err.Error()},
		})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"workspace": workspace,
	})
}

func (h *Handler) handleGetWorkspaceQuota(w http.ResponseWriter, r *http.Request, workspaceID string) {
	current := KeyFromContext(r.Context())
	if current != nil && current.WorkspaceID != DefaultWorkspaceID && workspaceID != current.WorkspaceID {
		writeAuthError(w, http.StatusForbidden, "Workspace-scoped admins can only view quota for their own workspace.")
		return
	}

	quota, err := h.store.GetWorkspaceQuota(workspaceID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"error": map[string]string{"message": err.Error()},
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"quota": quota,
	})
}

func (h *Handler) handlePutWorkspaceQuota(w http.ResponseWriter, r *http.Request, workspaceID string) {
	current := KeyFromContext(r.Context())
	if current != nil && current.WorkspaceID != DefaultWorkspaceID && workspaceID != current.WorkspaceID {
		writeAuthError(w, http.StatusForbidden, "Workspace-scoped admins can only update quota for their own workspace.")
		return
	}

	var req struct {
		MonthlyRequestLimit *int64 `json:"monthly_request_limit"`
		MonthlyTokenLimit   *int64 `json:"monthly_token_limit"`
		EnforceHardLimits   *bool  `json:"enforce_hard_limits"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": map[string]string{"message": "Invalid JSON"},
		})
		return
	}

	enforceHardLimits := true
	if req.EnforceHardLimits != nil {
		enforceHardLimits = *req.EnforceHardLimits
	}
	quota, err := h.store.UpsertWorkspaceQuota(workspaceID, req.MonthlyRequestLimit, req.MonthlyTokenLimit, enforceHardLimits)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": map[string]string{"message": err.Error()},
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"quota": quota,
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
			"id":             keyRecord.ID,
			"key_prefix":     keyRecord.KeyPrefix,
			"name":           keyRecord.Name,
			"role":           keyRecord.Role,
			"workspace_id":   keyRecord.WorkspaceID,
			"workspace_slug": keyRecord.WorkspaceSlug,
			"workspace_name": keyRecord.WorkspaceName,
		},
		"workspace": map[string]interface{}{
			"id":   keyRecord.WorkspaceID,
			"slug": keyRecord.WorkspaceSlug,
			"name": keyRecord.WorkspaceName,
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
			"id":             keyRecord.ID,
			"key_prefix":     keyRecord.KeyPrefix,
			"name":           keyRecord.Name,
			"role":           keyRecord.Role,
			"workspace_id":   keyRecord.WorkspaceID,
			"workspace_slug": keyRecord.WorkspaceSlug,
			"workspace_name": keyRecord.WorkspaceName,
		},
		"workspace": map[string]interface{}{
			"id":   keyRecord.WorkspaceID,
			"slug": keyRecord.WorkspaceSlug,
			"name": keyRecord.WorkspaceName,
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
