package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
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
