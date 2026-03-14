package auth

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/infera/infera/go/internal/providers"
)

// RegisterRoutes registers auth API routes on the mux.
// corsWrap is the CORS middleware from the gateway.
func (h *Handler) RegisterRoutes(mux *http.ServeMux, corsWrap func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("/api/auth/keys", corsWrap(h.RequireAuth(h.handleKeys)))
	mux.HandleFunc("/api/auth/keys/", corsWrap(h.RequireAuth(h.handleKeyByID)))
	mux.HandleFunc("/api/auth/workspaces", corsWrap(h.RequireAuth(h.handleWorkspaces)))
	mux.HandleFunc("/api/auth/workspaces/", corsWrap(h.RequireAuth(h.handleWorkspaceByID)))
	mux.HandleFunc("/api/auth/invitations/preview", corsWrap(h.handlePreviewInvitation))
	mux.HandleFunc("/api/auth/invitations/accept", corsWrap(h.handleAcceptInvitation))
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
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"error": map[string]string{"message": "Not found"},
		})
		return
	}
	workspaceID := strings.TrimSpace(parts[0])
	if workspaceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": map[string]string{"message": "Workspace ID required"},
		})
		return
	}

	switch parts[1] {
	case "quota":
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
	case "members":
		if len(parts) == 2 {
			h.handleWorkspaceMembers(w, r, workspaceID)
			return
		}
		h.handleWorkspaceMemberByID(w, r, workspaceID, parts[2])
	case "invites":
		if len(parts) == 2 {
			h.handleWorkspaceInvites(w, r, workspaceID)
			return
		}
		h.handleWorkspaceInviteByID(w, r, workspaceID, parts[2])
	case "providers":
		if len(parts) == 2 {
			h.handleWorkspaceProviders(w, r, workspaceID)
			return
		}
		h.handleWorkspaceProviderByID(w, r, workspaceID, parts[2])
	default:
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"error": map[string]string{"message": "Not found"},
		})
	}
}

func (h *Handler) handleWorkspaceProviders(w http.ResponseWriter, r *http.Request, workspaceID string) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{
			"error": map[string]string{"message": "Method not allowed"},
		})
		return
	}
	if !h.requirePermission(w, r, PermissionManageProviderConfigs, "Provider configuration access required.") {
		return
	}
	current := KeyFromContext(r.Context())
	if current != nil && current.WorkspaceID != DefaultWorkspaceID && current.WorkspaceID != workspaceID {
		writeAuthorizationError(w, "Workspace-scoped identities can only view provider configs in their own workspace.")
		return
	}
	configs, err := h.store.ListWorkspaceProviderConfigs(workspaceID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": map[string]string{"message": err.Error()},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"providers": configs,
		"total":     len(configs),
	})
}

func (h *Handler) handleWorkspaceProviderByID(w http.ResponseWriter, r *http.Request, workspaceID, providerName string) {
	if !providers.IsRegisteredProviderType(providers.ProviderType(providerName)) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": map[string]string{"message": "Unknown provider"},
		})
		return
	}
	if !h.requirePermission(w, r, PermissionManageProviderConfigs, "Provider configuration access required.") {
		return
	}
	current := KeyFromContext(r.Context())
	if current != nil && current.WorkspaceID != DefaultWorkspaceID && current.WorkspaceID != workspaceID {
		writeAuthorizationError(w, "Workspace-scoped identities can only manage provider configs in their own workspace.")
		return
	}

	switch r.Method {
	case http.MethodGet:
		config, err := h.store.GetWorkspaceProviderConfig(workspaceID, providerName)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{
				"error": map[string]string{"message": err.Error()},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"provider": config})
	case http.MethodPut:
		var req struct {
			APIKey    string `json:"api_key"`
			APISecret string `json:"api_secret"`
			Endpoint  string `json:"endpoint"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"error": map[string]string{"message": "Invalid JSON"},
			})
			return
		}
		config, err := h.store.UpsertWorkspaceProviderConfig(workspaceID, providerName, req.APIKey, req.APISecret, req.Endpoint)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"error": map[string]string{"message": err.Error()},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"provider": config})
	case http.MethodDelete:
		if err := h.store.DeleteWorkspaceProviderConfig(workspaceID, providerName); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{
				"error": map[string]string{"message": err.Error()},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{
			"error": map[string]string{"message": "Method not allowed"},
		})
	}
}

func (h *Handler) handleListKeys(w http.ResponseWriter, r *http.Request) {
	if !h.requirePermission(w, r, PermissionManageKeys, "Key management access required.") {
		return
	}
	current := KeyFromContext(r.Context())
	workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if workspaceID == "" && current != nil {
		workspaceID = current.WorkspaceID
	}
	if current != nil && current.WorkspaceID != DefaultWorkspaceID && workspaceID != "" && workspaceID != current.WorkspaceID {
		writeAuthorizationError(w, "Workspace-scoped identities can only list keys in their own workspace.")
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
		Name          string `json:"name"`
		Role          string `json:"role"`
		PrincipalType string `json:"principal_type"`
		WorkspaceID   string `json:"workspace_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": map[string]string{"message": "Invalid JSON"},
		})
		return
	}

	if req.Role == "" {
		req.Role = RoleUser
	}
	if req.PrincipalType == "" {
		req.PrincipalType = PrincipalHuman
	}

	if !h.requirePermission(w, r, PermissionManageKeys, "Key management access required.") {
		return
	}

	current := KeyFromContext(r.Context())
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" && current != nil {
		workspaceID = current.WorkspaceID
	}
	if current != nil && current.WorkspaceID != DefaultWorkspaceID && workspaceID != "" && workspaceID != current.WorkspaceID {
		writeAuthorizationError(w, "Workspace-scoped identities can only create keys in their own workspace.")
		return
	}
	if current != nil && !CanAssignRole(current, req.Role) {
		writeAuthorizationError(w, "You cannot assign that role.")
		return
	}

	fullKey, record, err := h.store.CreateKeyWithPrincipalInWorkspace(workspaceID, req.Name, req.Role, req.PrincipalType)
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
	if !h.requirePermission(w, r, PermissionManageKeys, "Key management access required.") {
		return
	}
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
	if current == nil || current.Role == RoleUser {
		writeAuthorizationError(w, "Workspace access required.")
		return
	}
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
	if !h.requirePermission(w, r, PermissionManageWorkspaces, "Workspace management access required.") {
		return
	}
	if current != nil && current.WorkspaceID != DefaultWorkspaceID {
		writeAuthorizationError(w, "Only platform admins can create workspaces.")
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
	if !h.requirePermission(w, r, PermissionViewUsage, "Usage access required.") {
		return
	}
	current := KeyFromContext(r.Context())
	if current != nil && current.WorkspaceID != DefaultWorkspaceID && workspaceID != current.WorkspaceID {
		writeAuthorizationError(w, "Workspace-scoped identities can only view quota for their own workspace.")
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
	if !h.requirePermission(w, r, PermissionManageQuotas, "Quota management access required.") {
		return
	}
	current := KeyFromContext(r.Context())
	if current != nil && current.WorkspaceID != DefaultWorkspaceID && workspaceID != current.WorkspaceID {
		writeAuthorizationError(w, "Workspace-scoped identities can only update quota for their own workspace.")
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

func (h *Handler) handleWorkspaceMembers(w http.ResponseWriter, r *http.Request, workspaceID string) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{
			"error": map[string]string{"message": "Method not allowed"},
		})
		return
	}
	if !h.requirePermission(w, r, PermissionManageMemberships, "Membership management access required.") {
		return
	}
	current := KeyFromContext(r.Context())
	if current != nil && current.WorkspaceID != DefaultWorkspaceID && current.WorkspaceID != workspaceID {
		writeAuthorizationError(w, "Workspace-scoped identities can only view members in their own workspace.")
		return
	}
	members, err := h.store.ListWorkspaceMemberships(workspaceID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": map[string]string{"message": err.Error()},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"members": members,
		"total":   len(members),
	})
}

func (h *Handler) handleWorkspaceMemberByID(w http.ResponseWriter, r *http.Request, workspaceID, membershipID string) {
	if !h.requirePermission(w, r, PermissionManageMemberships, "Membership management access required.") {
		return
	}
	current := KeyFromContext(r.Context())
	if current != nil && current.WorkspaceID != DefaultWorkspaceID && current.WorkspaceID != workspaceID {
		writeAuthorizationError(w, "Workspace-scoped identities can only manage members in their own workspace.")
		return
	}

	switch r.Method {
	case http.MethodPut:
		var req struct {
			Role string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"error": map[string]string{"message": "Invalid JSON"},
			})
			return
		}
		if req.Role == "" {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"error": map[string]string{"message": "role is required"},
			})
			return
		}
		if current != nil && !CanAssignRole(current, req.Role) {
			writeAuthorizationError(w, "You cannot assign that role.")
			return
		}
		if current != nil && current.MembershipID != nil && *current.MembershipID == membershipID && current.Role != req.Role {
			writeAuthorizationError(w, "You cannot change your own membership role.")
			return
		}
		member, err := h.store.UpdateWorkspaceMembershipRole(workspaceID, membershipID, req.Role)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"error": map[string]string{"message": err.Error()},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"member": member})
	case http.MethodDelete:
		if current != nil && current.MembershipID != nil && *current.MembershipID == membershipID {
			writeAuthorizationError(w, "You cannot remove your own membership.")
			return
		}
		if err := h.store.RemoveWorkspaceMembership(workspaceID, membershipID); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"error": map[string]string{"message": err.Error()},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{
			"error": map[string]string{"message": "Method not allowed"},
		})
	}
}

func (h *Handler) handleWorkspaceInvites(w http.ResponseWriter, r *http.Request, workspaceID string) {
	switch r.Method {
	case http.MethodGet:
		h.handleListWorkspaceInvites(w, r, workspaceID)
	case http.MethodPost:
		h.handleCreateWorkspaceInvite(w, r, workspaceID)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{
			"error": map[string]string{"message": "Method not allowed"},
		})
	}
}

func (h *Handler) handleWorkspaceInviteByID(w http.ResponseWriter, r *http.Request, workspaceID, inviteID string) {
	if r.Method != http.MethodDelete {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{
			"error": map[string]string{"message": "Method not allowed"},
		})
		return
	}
	if !h.requirePermission(w, r, PermissionManageMemberships, "Membership management access required.") {
		return
	}
	current := KeyFromContext(r.Context())
	if current != nil && current.WorkspaceID != DefaultWorkspaceID && current.WorkspaceID != workspaceID {
		writeAuthorizationError(w, "Workspace-scoped identities can only manage invites in their own workspace.")
		return
	}
	if err := h.store.RevokeWorkspaceInvitation(workspaceID, inviteID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{
			"error": map[string]string{"message": err.Error()},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *Handler) handleListWorkspaceInvites(w http.ResponseWriter, r *http.Request, workspaceID string) {
	if !h.requirePermission(w, r, PermissionManageMemberships, "Membership management access required.") {
		return
	}
	current := KeyFromContext(r.Context())
	if current != nil && current.WorkspaceID != DefaultWorkspaceID && current.WorkspaceID != workspaceID {
		writeAuthorizationError(w, "Workspace-scoped identities can only view invites in their own workspace.")
		return
	}
	invitations, err := h.store.ListWorkspaceInvitations(workspaceID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": map[string]string{"message": err.Error()},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"invitations": invitations,
		"total":       len(invitations),
	})
}

func (h *Handler) handleCreateWorkspaceInvite(w http.ResponseWriter, r *http.Request, workspaceID string) {
	if !h.requirePermission(w, r, PermissionManageMemberships, "Membership management access required.") {
		return
	}
	current := KeyFromContext(r.Context())
	if current != nil && current.WorkspaceID != DefaultWorkspaceID && current.WorkspaceID != workspaceID {
		writeAuthorizationError(w, "Workspace-scoped identities can only manage invites in their own workspace.")
		return
	}

	var req struct {
		Email       string `json:"email"`
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": map[string]string{"message": "Invalid JSON"},
		})
		return
	}
	if req.Role == "" {
		req.Role = RoleDeveloper
	}
	if !CanAssignRole(current, req.Role) {
		writeAuthorizationError(w, "You cannot assign that role.")
		return
	}

	token, invitation, err := h.store.CreateWorkspaceInvitation(workspaceID, req.Email, req.DisplayName, req.Role, current.ID, time.Now().Add(7*24*time.Hour))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": map[string]string{"message": err.Error()},
		})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"invitation_token": token,
		"invitation":       invitation,
	})
}

func (h *Handler) handlePreviewInvitation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{
			"error": map[string]string{"message": "Method not allowed"},
		})
		return
	}

	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": map[string]string{"message": "token is required"},
		})
		return
	}

	preview, err := h.store.GetWorkspaceInvitationPreview(token)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": map[string]string{"message": err.Error()},
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"invitation": preview,
	})
}

func (h *Handler) handleAcceptInvitation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{
			"error": map[string]string{"message": "Method not allowed"},
		})
		return
	}

	var req struct {
		InvitationToken string `json:"invitation_token"`
		DisplayName     string `json:"display_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": map[string]string{"message": "Invalid JSON"},
		})
		return
	}
	membership, fullKey, record, err := h.store.AcceptWorkspaceInvitation(req.InvitationToken, req.DisplayName)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{
			"error": map[string]string{"message": err.Error()},
		})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"membership": membership,
		"key":        fullKey,
		"record":     record,
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

	// Only human principals with dashboard access can create sessions.
	if keyRecord.PrincipalType == PrincipalServiceAccount {
		writeAuthorizationError(w, "Service accounts cannot create dashboard sessions.")
		return
	}
	if !CanCreateSession(keyRecord) {
		writeAuthorizationError(w, "Dashboard access required.")
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
			"principal_type": keyRecord.PrincipalType,
			"workspace_id":   keyRecord.WorkspaceID,
			"workspace_slug": keyRecord.WorkspaceSlug,
			"workspace_name": keyRecord.WorkspaceName,
		},
		"workspace": map[string]interface{}{
			"id":   keyRecord.WorkspaceID,
			"slug": keyRecord.WorkspaceSlug,
			"name": keyRecord.WorkspaceName,
		},
		"member": membershipPayload(keyRecord),
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
			"principal_type": keyRecord.PrincipalType,
			"workspace_id":   keyRecord.WorkspaceID,
			"workspace_slug": keyRecord.WorkspaceSlug,
			"workspace_name": keyRecord.WorkspaceName,
		},
		"workspace": map[string]interface{}{
			"id":   keyRecord.WorkspaceID,
			"slug": keyRecord.WorkspaceSlug,
			"name": keyRecord.WorkspaceName,
		},
		"member": membershipPayload(keyRecord),
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

func (h *Handler) requirePermission(w http.ResponseWriter, r *http.Request, permission, message string) bool {
	record := KeyFromContext(r.Context())
	if !HasPermission(record, permission) {
		writeAuthorizationError(w, message)
		return false
	}
	return true
}

func membershipPayload(keyRecord *KeyRecord) map[string]interface{} {
	if keyRecord == nil || keyRecord.MembershipID == nil {
		return nil
	}
	payload := map[string]interface{}{
		"id": *keyRecord.MembershipID,
	}
	if keyRecord.MemberEmail != nil {
		payload["email"] = *keyRecord.MemberEmail
	}
	if keyRecord.MemberName != nil {
		payload["display_name"] = *keyRecord.MemberName
	}
	return payload
}
