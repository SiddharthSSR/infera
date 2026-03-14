package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/infera/infera/go/internal/providers/mock"
	_ "github.com/infera/infera/go/internal/providers/runpod"
)

func newTestHandlerWithRoutes(t *testing.T) (*Handler, *Store, *http.ServeMux) {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(filepath.Join(dir, "auth_test.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	h := NewHandler(s)
	h.SetSecure(false)

	mux := http.NewServeMux()
	noopCORS := func(next http.HandlerFunc) http.HandlerFunc { return next }
	h.RegisterRoutes(mux, noopCORS)
	return h, s, mux
}

// ---------- Key handlers ----------

func TestHandleCreateKey(t *testing.T) {
	_, s, mux := newTestHandlerWithRoutes(t)
	adminKey, _, _ := s.CreateKey("admin", "admin")

	body := `{"name":"test-key","role":"user"}`
	req := httptest.NewRequest("POST", "/api/auth/keys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["key"] == nil {
		t.Error("expected key in response")
	}
	record := resp["record"].(map[string]interface{})
	if record["workspace_id"] == "" {
		t.Fatal("expected workspace_id in key record")
	}
	if record["principal_type"] != PrincipalHuman {
		t.Fatalf("expected human principal, got %v", record["principal_type"])
	}
}

func TestHandleCreateKey_MissingName(t *testing.T) {
	_, s, mux := newTestHandlerWithRoutes(t)
	adminKey, _, _ := s.CreateKey("admin", "admin")

	body := `{"role":"user"}`
	req := httptest.NewRequest("POST", "/api/auth/keys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestHandleListKeys(t *testing.T) {
	_, s, mux := newTestHandlerWithRoutes(t)
	adminKey, _, _ := s.CreateKey("admin", "admin")

	req := httptest.NewRequest("GET", "/api/auth/keys", nil)
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestHandleCreateWorkspace(t *testing.T) {
	_, s, mux := newTestHandlerWithRoutes(t)
	adminKey, _, _ := s.CreateKey("admin", "admin")

	body := `{"name":"Acme Team"}`
	req := httptest.NewRequest("POST", "/api/auth/workspaces", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Workspace map[string]any `json:"workspace"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp.Workspace["slug"] != "acme-team" {
		t.Fatalf("expected slug acme-team, got %v", resp.Workspace["slug"])
	}
}

func TestHandleCreateWorkspace_WorkspaceAdminForbidden(t *testing.T) {
	_, s, mux := newTestHandlerWithRoutes(t)
	workspace, err := s.CreateWorkspace("Acme Team")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	workspaceAdminKey, _, err := s.CreateKeyInWorkspace(workspace.ID, "workspace-admin", "admin")
	if err != nil {
		t.Fatalf("CreateKeyInWorkspace: %v", err)
	}

	body := `{"name":"Another Team"}`
	req := httptest.NewRequest("POST", "/api/auth/workspaces", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+workspaceAdminKey)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleWorkspaceQuota(t *testing.T) {
	_, s, mux := newTestHandlerWithRoutes(t)
	adminKey, _, _ := s.CreateKey("admin", "admin")
	workspace, err := s.CreateWorkspace("Billing Team")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	putBody := `{"monthly_request_limit":100,"monthly_token_limit":5000,"enforce_hard_limits":true}`
	putReq := httptest.NewRequest("PUT", "/api/auth/workspaces/"+workspace.ID+"/quota", strings.NewReader(putBody))
	putReq.Header.Set("Content-Type", "application/json")
	putReq.Header.Set("Authorization", "Bearer "+adminKey)
	putRec := httptest.NewRecorder()
	mux.ServeHTTP(putRec, putReq)

	if putRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", putRec.Code, putRec.Body.String())
	}

	getReq := httptest.NewRequest("GET", "/api/auth/workspaces/"+workspace.ID+"/quota", nil)
	getReq.Header.Set("Authorization", "Bearer "+adminKey)
	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", getRec.Code, getRec.Body.String())
	}

	var resp struct {
		Quota map[string]any `json:"quota"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp.Quota["monthly_request_limit"] != float64(100) {
		t.Fatalf("expected request limit 100, got %v", resp.Quota["monthly_request_limit"])
	}
}

func TestHandleWorkspaceQuota_BillingRole(t *testing.T) {
	_, s, mux := newTestHandlerWithRoutes(t)
	workspace, err := s.CreateWorkspace("Finance Team")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	billingKey, _, err := s.CreateKeyInWorkspace(workspace.ID, "billing", RoleBilling)
	if err != nil {
		t.Fatalf("CreateKeyInWorkspace: %v", err)
	}

	putBody := `{"monthly_request_limit":250}`
	putReq := httptest.NewRequest("PUT", "/api/auth/workspaces/"+workspace.ID+"/quota", strings.NewReader(putBody))
	putReq.Header.Set("Content-Type", "application/json")
	putReq.Header.Set("Authorization", "Bearer "+billingKey)
	putRec := httptest.NewRecorder()
	mux.ServeHTTP(putRec, putReq)

	if putRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", putRec.Code, putRec.Body.String())
	}
}

func TestHandleWorkspaceProviderConfig(t *testing.T) {
	_, s, mux := newTestHandlerWithRoutes(t)
	adminKey, _, _ := s.CreateKey("admin", "admin")
	workspace, err := s.CreateWorkspace("Infra Team")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	putBody := `{"api_key":"rp_key","endpoint":"https://api.runpod.io/graphql"}`
	putReq := httptest.NewRequest("PUT", "/api/auth/workspaces/"+workspace.ID+"/providers/runpod", strings.NewReader(putBody))
	putReq.Header.Set("Content-Type", "application/json")
	putReq.Header.Set("Authorization", "Bearer "+adminKey)
	putRec := httptest.NewRecorder()
	mux.ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", putRec.Code, putRec.Body.String())
	}

	getReq := httptest.NewRequest("GET", "/api/auth/workspaces/"+workspace.ID+"/providers/runpod", nil)
	getReq.Header.Set("Authorization", "Bearer "+adminKey)
	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", getRec.Code, getRec.Body.String())
	}

	listReq := httptest.NewRequest("GET", "/api/auth/workspaces/"+workspace.ID+"/providers", nil)
	listReq.Header.Set("Authorization", "Bearer "+adminKey)
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", listRec.Code, listRec.Body.String())
	}

	deleteReq := httptest.NewRequest("DELETE", "/api/auth/workspaces/"+workspace.ID+"/providers/runpod", nil)
	deleteReq.Header.Set("Authorization", "Bearer "+adminKey)
	deleteRec := httptest.NewRecorder()
	mux.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", deleteRec.Code, deleteRec.Body.String())
	}
}

func TestHandleCreateKey_BillingForbidden(t *testing.T) {
	_, s, mux := newTestHandlerWithRoutes(t)
	workspace, err := s.CreateWorkspace("Finance Team")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	billingKey, _, err := s.CreateKeyInWorkspace(workspace.ID, "billing", RoleBilling)
	if err != nil {
		t.Fatalf("CreateKeyInWorkspace: %v", err)
	}

	body := `{"name":"svc","role":"operator","principal_type":"service_account"}`
	req := httptest.NewRequest("POST", "/api/auth/keys", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+billingKey)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleRevokeKey(t *testing.T) {
	_, s, mux := newTestHandlerWithRoutes(t)
	adminKey, _, _ := s.CreateKey("admin", "admin")
	_, userRec, _ := s.CreateKey("user", "user")

	req := httptest.NewRequest("DELETE", "/api/auth/keys/"+userRec.ID, nil)
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleListKeys_WorkspaceScoped(t *testing.T) {
	_, s, mux := newTestHandlerWithRoutes(t)
	workspace, err := s.CreateWorkspace("Acme Team")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	adminKey, _, err := s.CreateKeyInWorkspace(workspace.ID, "workspace-admin", "admin")
	if err != nil {
		t.Fatalf("CreateKeyInWorkspace admin: %v", err)
	}
	if _, _, err := s.CreateKeyInWorkspace(workspace.ID, "workspace-user", "user"); err != nil {
		t.Fatalf("CreateKeyInWorkspace user: %v", err)
	}
	if _, _, err := s.CreateKey("default-user", "user"); err != nil {
		t.Fatalf("CreateKey default: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/auth/keys", nil)
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Keys []map[string]any `json:"keys"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(resp.Keys) != 2 {
		t.Fatalf("expected 2 keys in scoped workspace, got %d", len(resp.Keys))
	}
	for _, key := range resp.Keys {
		if key["workspace_id"] != workspace.ID {
			t.Fatalf("expected workspace %q, got %v", workspace.ID, key["workspace_id"])
		}
	}
}

func TestHandleWorkspaceInvitesAndMembers(t *testing.T) {
	_, s, mux := newTestHandlerWithRoutes(t)
	adminKey, adminRec, err := s.CreateKey("admin", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateKey admin: %v", err)
	}
	workspace, err := s.CreateWorkspace("Members Team")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	createBody := `{"email":"teammate@example.com","display_name":"Teammate","role":"developer"}`
	createReq := httptest.NewRequest("POST", "/api/auth/workspaces/"+workspace.ID+"/invites", strings.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("Authorization", "Bearer "+adminKey)
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", createRec.Code, createRec.Body.String())
	}

	var createResp struct {
		InvitationToken string         `json:"invitation_token"`
		Invitation      map[string]any `json:"invitation"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if createResp.InvitationToken == "" {
		t.Fatal("expected invitation token")
	}
	inviteID, _ := createResp.Invitation["id"].(string)
	if inviteID == "" {
		t.Fatal("expected invitation id")
	}

	listReq := httptest.NewRequest("GET", "/api/auth/workspaces/"+workspace.ID+"/invites", nil)
	listReq.Header.Set("Authorization", "Bearer "+adminKey)
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", listRec.Code, listRec.Body.String())
	}

	acceptBody := `{"invitation_token":"` + createResp.InvitationToken + `","display_name":"Joined User"}`
	acceptReq := httptest.NewRequest("POST", "/api/auth/invitations/accept", strings.NewReader(acceptBody))
	acceptReq.Header.Set("Content-Type", "application/json")
	acceptRec := httptest.NewRecorder()
	mux.ServeHTTP(acceptRec, acceptReq)
	if acceptRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", acceptRec.Code, acceptRec.Body.String())
	}

	membersReq := httptest.NewRequest("GET", "/api/auth/workspaces/"+workspace.ID+"/members", nil)
	membersReq.Header.Set("Authorization", "Bearer "+adminKey)
	membersRec := httptest.NewRecorder()
	mux.ServeHTTP(membersRec, membersReq)
	if membersRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", membersRec.Code, membersRec.Body.String())
	}

	var membersResp struct {
		Members []map[string]any `json:"members"`
	}
	if err := json.Unmarshal(membersRec.Body.Bytes(), &membersResp); err != nil {
		t.Fatalf("json.Unmarshal members: %v", err)
	}
	if len(membersResp.Members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(membersResp.Members))
	}
	if membersResp.Members[0]["email"] != "teammate@example.com" {
		t.Fatalf("expected teammate email, got %v", membersResp.Members[0]["email"])
	}

	deleteReq := httptest.NewRequest("DELETE", "/api/auth/workspaces/"+workspace.ID+"/invites/"+inviteID, nil)
	deleteReq.Header.Set("Authorization", "Bearer "+adminKey)
	deleteRec := httptest.NewRecorder()
	mux.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 deleting accepted invite, got %d: %s", deleteRec.Code, deleteRec.Body.String())
	}

	_ = adminRec
}

func TestHandlePreviewInvitation(t *testing.T) {
	_, s, mux := newTestHandlerWithRoutes(t)
	_, adminRec, err := s.CreateKey("admin", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateKey admin: %v", err)
	}
	workspace, err := s.CreateWorkspace("Preview Workspace")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	token, _, err := s.CreateWorkspaceInvitation(workspace.ID, "preview@example.com", "Preview User", RoleDeveloper, adminRec.ID, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/auth/invitations/preview?token="+token, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Invitation map[string]any `json:"invitation"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal preview: %v", err)
	}
	if resp.Invitation["workspace_name"] != workspace.Name {
		t.Fatalf("expected workspace_name %q, got %v", workspace.Name, resp.Invitation["workspace_name"])
	}
	if resp.Invitation["role"] != RoleDeveloper {
		t.Fatalf("expected role %q, got %v", RoleDeveloper, resp.Invitation["role"])
	}
}

func TestHandleWorkspaceMemberUpdateAndRemoval(t *testing.T) {
	_, s, mux := newTestHandlerWithRoutes(t)
	workspace, err := s.CreateWorkspace("Membership Admin")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	adminKey, adminRec, err := s.CreateKeyInWorkspace(workspace.ID, "workspace-owner", RoleOwner)
	if err != nil {
		t.Fatalf("CreateKeyInWorkspace owner: %v", err)
	}

	token, _, err := s.CreateWorkspaceInvitation(workspace.ID, "dev@example.com", "Dev", RoleDeveloper, adminRec.ID, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation: %v", err)
	}
	membership, _, _, err := s.AcceptWorkspaceInvitation(token, "Dev")
	if err != nil {
		t.Fatalf("AcceptWorkspaceInvitation: %v", err)
	}

	updateReq := httptest.NewRequest("PUT", "/api/auth/workspaces/"+workspace.ID+"/members/"+membership.ID, strings.NewReader(`{"role":"operator"}`))
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq.Header.Set("Authorization", "Bearer "+adminKey)
	updateRec := httptest.NewRecorder()
	mux.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on update, got %d: %s", updateRec.Code, updateRec.Body.String())
	}

	deleteReq := httptest.NewRequest("DELETE", "/api/auth/workspaces/"+workspace.ID+"/members/"+membership.ID, nil)
	deleteReq.Header.Set("Authorization", "Bearer "+adminKey)
	deleteRec := httptest.NewRecorder()
	mux.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on delete, got %d: %s", deleteRec.Code, deleteRec.Body.String())
	}
}

func TestHandleWorkspaceMemberSelfRemovalForbidden(t *testing.T) {
	_, s, mux := newTestHandlerWithRoutes(t)
	workspace, err := s.CreateWorkspace("Membership Self")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	ownerKey, ownerRec, err := s.CreateKeyInWorkspace(workspace.ID, "owner", RoleOwner)
	if err != nil {
		t.Fatalf("CreateKeyInWorkspace: %v", err)
	}

	ownerMembershipID := "mbr_owner"
	if _, err := s.db.Exec(`
		INSERT INTO workspace_memberships (id, workspace_id, email, display_name, role, status, created_at)
		VALUES (?, ?, ?, ?, ?, 'active', ?)`,
		ownerMembershipID, workspace.ID, "owner@example.com", "Owner", RoleOwner, time.Now(),
	); err != nil {
		t.Fatalf("insert membership: %v", err)
	}
	if _, err := s.db.Exec(`UPDATE api_keys SET membership_id = ? WHERE id = ?`, ownerMembershipID, ownerRec.ID); err != nil {
		t.Fatalf("link membership: %v", err)
	}

	deleteReq := httptest.NewRequest("DELETE", "/api/auth/workspaces/"+workspace.ID+"/members/"+ownerMembershipID, nil)
	deleteReq.Header.Set("Authorization", "Bearer "+ownerKey)
	deleteRec := httptest.NewRecorder()
	mux.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on self removal, got %d: %s", deleteRec.Code, deleteRec.Body.String())
	}
}

func TestHandleCreateWorkspaceInvite_RoleEscalationForbidden(t *testing.T) {
	_, s, mux := newTestHandlerWithRoutes(t)
	workspace, err := s.CreateWorkspace("Role Team")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	adminKey, _, err := s.CreateKeyInWorkspace(workspace.ID, "workspace-admin", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateKeyInWorkspace: %v", err)
	}

	body := `{"email":"owner@example.com","display_name":"Owner","role":"owner"}`
	req := httptest.NewRequest("POST", "/api/auth/workspaces/"+workspace.ID+"/invites", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---------- Session handlers ----------

func TestHandleCreateSession_AdminKey(t *testing.T) {
	_, s, mux := newTestHandlerWithRoutes(t)
	adminKey, _, _ := s.CreateKey("admin", "admin")

	body := `{"api_key":"` + adminKey + `"}`
	req := httptest.NewRequest("POST", "/api/auth/session", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	cookies := rr.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == sessionCookieName {
			found = true
			if !c.HttpOnly {
				t.Error("cookie should be HttpOnly")
			}
		}
	}
	if !found {
		t.Error("expected session cookie to be set")
	}
}

func TestHandleCreateSession_UserKey(t *testing.T) {
	_, s, mux := newTestHandlerWithRoutes(t)
	userKey, _, _ := s.CreateKey("user", "user")

	body := `{"api_key":"` + userKey + `"}`
	req := httptest.NewRequest("POST", "/api/auth/session", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

func TestHandleCreateSession_ServiceAccountForbidden(t *testing.T) {
	_, s, mux := newTestHandlerWithRoutes(t)
	serviceKey, _, err := s.CreateKeyWithPrincipal("svc-bot", RoleOperator, PrincipalServiceAccount)
	if err != nil {
		t.Fatalf("CreateKeyWithPrincipal: %v", err)
	}

	body := `{"api_key":"` + serviceKey + `"}`
	req := httptest.NewRequest("POST", "/api/auth/session", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestHandleCreateSession_OperatorKey(t *testing.T) {
	_, s, mux := newTestHandlerWithRoutes(t)
	operatorKey, _, err := s.CreateKey("ops", RoleOperator)
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	body := `{"api_key":"` + operatorKey + `"}`
	req := httptest.NewRequest("POST", "/api/auth/session", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestHandleCreateSession_InvalidKey(t *testing.T) {
	_, _, mux := newTestHandlerWithRoutes(t)

	body := `{"api_key":"inf_invalid"}`
	req := httptest.NewRequest("POST", "/api/auth/session", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestHandleGetSession_Valid(t *testing.T) {
	_, s, mux := newTestHandlerWithRoutes(t)
	_, rec, _ := s.CreateKey("admin", "admin")
	token, _, _ := s.CreateSession(rec.ID)

	req := httptest.NewRequest("GET", "/api/auth/session", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["session"] == nil || resp["key"] == nil {
		t.Error("expected session and key in response")
	}
	if resp["workspace"] == nil {
		t.Error("expected workspace in session response")
	}
	key := resp["key"].(map[string]interface{})
	if key["principal_type"] != PrincipalHuman {
		t.Fatalf("expected human principal in session payload, got %v", key["principal_type"])
	}
}

func TestHandleGetSession_WithMembership(t *testing.T) {
	_, s, mux := newTestHandlerWithRoutes(t)
	adminKey, adminRec, err := s.CreateKey("admin", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateKey admin: %v", err)
	}
	_ = adminKey
	workspace, err := s.CreateWorkspace("Session Team")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	token, _, err := s.CreateWorkspaceInvitation(workspace.ID, "member@example.com", "Member", RoleOperator, adminRec.ID, mustTime(t))
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation: %v", err)
	}
	fullKey, _, _, err := func() (string, *WorkspaceMembershipRecord, *KeyRecord, error) {
		membership, key, record, err := s.AcceptWorkspaceInvitation(token, "Joined Member")
		return key, membership, record, err
	}()
	if err != nil {
		t.Fatalf("AcceptWorkspaceInvitation: %v", err)
	}

	createBody := `{"api_key":"` + fullKey + `"}`
	createReq := httptest.NewRequest("POST", "/api/auth/session", strings.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", createRec.Code, createRec.Body.String())
	}

	cookies := createRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie")
	}
	getReq := httptest.NewRequest("GET", "/api/auth/session", nil)
	getReq.AddCookie(cookies[0])
	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", getRec.Code, getRec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(getRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	member, ok := resp["member"].(map[string]any)
	if !ok {
		t.Fatalf("expected member payload, got %T", resp["member"])
	}
	if member["email"] != "member@example.com" {
		t.Fatalf("expected member email, got %v", member["email"])
	}
}

func TestHandleListWorkspaces_WithMembershipListsAccessibleWorkspaces(t *testing.T) {
	_, s, mux := newTestHandlerWithRoutes(t)
	_, adminRec, err := s.CreateKey("admin", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateKey admin: %v", err)
	}

	workspaceA, err := s.CreateWorkspace("Alpha Team")
	if err != nil {
		t.Fatalf("CreateWorkspace alpha: %v", err)
	}
	workspaceB, err := s.CreateWorkspace("Beta Team")
	if err != nil {
		t.Fatalf("CreateWorkspace beta: %v", err)
	}

	tokenA, _, err := s.CreateWorkspaceInvitation(workspaceA.ID, "member@example.com", "Member", RoleOperator, adminRec.ID, mustTime(t))
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation alpha: %v", err)
	}
	tokenB, _, err := s.CreateWorkspaceInvitation(workspaceB.ID, "member@example.com", "Member", RoleDeveloper, adminRec.ID, mustTime(t))
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation beta: %v", err)
	}

	fullKey, _, _, err := func() (string, *WorkspaceMembershipRecord, *KeyRecord, error) {
		membership, key, record, err := s.AcceptWorkspaceInvitation(tokenA, "Joined Member")
		return key, membership, record, err
	}()
	if err != nil {
		t.Fatalf("AcceptWorkspaceInvitation alpha: %v", err)
	}
	if _, _, _, err := s.AcceptWorkspaceInvitation(tokenB, "Joined Member"); err != nil {
		t.Fatalf("AcceptWorkspaceInvitation beta: %v", err)
	}

	createReq := httptest.NewRequest("POST", "/api/auth/session", strings.NewReader(`{"api_key":"`+fullKey+`"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", createRec.Code, createRec.Body.String())
	}

	req := httptest.NewRequest("GET", "/api/auth/workspaces", nil)
	req.AddCookie(createRec.Result().Cookies()[0])
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Workspaces []WorkspaceRecord `json:"workspaces"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(resp.Workspaces) != 2 {
		t.Fatalf("expected 2 accessible workspaces, got %d", len(resp.Workspaces))
	}
}

func TestHandleSwitchSessionWorkspace(t *testing.T) {
	_, s, mux := newTestHandlerWithRoutes(t)
	_, adminRec, err := s.CreateKey("admin", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateKey admin: %v", err)
	}

	workspaceA, err := s.CreateWorkspace("Alpha Team")
	if err != nil {
		t.Fatalf("CreateWorkspace alpha: %v", err)
	}
	workspaceB, err := s.CreateWorkspace("Beta Team")
	if err != nil {
		t.Fatalf("CreateWorkspace beta: %v", err)
	}

	tokenA, _, err := s.CreateWorkspaceInvitation(workspaceA.ID, "member@example.com", "Member", RoleOperator, adminRec.ID, mustTime(t))
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation alpha: %v", err)
	}
	tokenB, _, err := s.CreateWorkspaceInvitation(workspaceB.ID, "member@example.com", "Member", RoleDeveloper, adminRec.ID, mustTime(t))
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation beta: %v", err)
	}

	fullKey, _, _, err := func() (string, *WorkspaceMembershipRecord, *KeyRecord, error) {
		membership, key, record, err := s.AcceptWorkspaceInvitation(tokenA, "Joined Member")
		return key, membership, record, err
	}()
	if err != nil {
		t.Fatalf("AcceptWorkspaceInvitation alpha: %v", err)
	}
	if _, _, _, err := s.AcceptWorkspaceInvitation(tokenB, "Joined Member"); err != nil {
		t.Fatalf("AcceptWorkspaceInvitation beta: %v", err)
	}

	createReq := httptest.NewRequest("POST", "/api/auth/session", strings.NewReader(`{"api_key":"`+fullKey+`"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", createRec.Code, createRec.Body.String())
	}

	switchReq := httptest.NewRequest("PUT", "/api/auth/session/workspace", strings.NewReader(`{"workspace_id":"`+workspaceB.ID+`"}`))
	switchReq.Header.Set("Content-Type", "application/json")
	switchReq.AddCookie(createRec.Result().Cookies()[0])
	switchRec := httptest.NewRecorder()
	mux.ServeHTTP(switchRec, switchReq)
	if switchRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", switchRec.Code, switchRec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(switchRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	workspace, ok := resp["workspace"].(map[string]any)
	if !ok {
		t.Fatalf("expected workspace payload, got %T", resp["workspace"])
	}
	if workspace["id"] != workspaceB.ID {
		t.Fatalf("expected switched workspace %q, got %v", workspaceB.ID, workspace["id"])
	}
}

func TestHandleGetSession_NoCookie(t *testing.T) {
	_, _, mux := newTestHandlerWithRoutes(t)

	req := httptest.NewRequest("GET", "/api/auth/session", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestHandleDeleteSession(t *testing.T) {
	_, s, mux := newTestHandlerWithRoutes(t)
	_, rec, _ := s.CreateKey("admin", "admin")
	token, _, _ := s.CreateSession(rec.ID)

	req := httptest.NewRequest("DELETE", "/api/auth/session", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	req2 := httptest.NewRequest("GET", "/api/auth/session", nil)
	req2.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rr2 := httptest.NewRecorder()
	mux.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 after delete, got %d", rr2.Code)
	}
}

func TestHandleDeleteSession_NoCookie(t *testing.T) {
	_, _, mux := newTestHandlerWithRoutes(t)

	req := httptest.NewRequest("DELETE", "/api/auth/session", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for idempotent logout, got %d", rr.Code)
	}
}

func mustTime(t *testing.T) time.Time {
	t.Helper()
	return time.Now().Add(24 * time.Hour)
}
