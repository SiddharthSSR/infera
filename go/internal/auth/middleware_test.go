package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func newTestHandler(t *testing.T) (*Handler, *Store) {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(filepath.Join(dir, "auth_test.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	h := NewHandler(s)
	h.SetSecure(false)
	return h, s
}

func okHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("ok")); err != nil {
		panic(err)
	}
}

// ---------- Bearer / X-API-Key ----------

func TestRequireAuth_ValidBearer(t *testing.T) {
	h, s := newTestHandler(t)
	fullKey, _, _ := s.CreateKey("k", "user")

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+fullKey)
	rr := httptest.NewRecorder()
	h.RequireAuth(okHandler)(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestRequireAuth_ValidXAPIKey(t *testing.T) {
	h, s := newTestHandler(t)
	fullKey, _, _ := s.CreateKey("k", "user")

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", fullKey)
	rr := httptest.NewRecorder()
	h.RequireAuth(okHandler)(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestRequireAuth_NoKey(t *testing.T) {
	h, _ := newTestHandler(t)

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()
	h.RequireAuth(okHandler)(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestRequireAuth_InvalidKey(t *testing.T) {
	h, _ := newTestHandler(t)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer inf_invalid")
	rr := httptest.NewRecorder()
	h.RequireAuth(okHandler)(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

// ---------- RequireAdmin ----------

func TestRequireAdmin_AdminKey(t *testing.T) {
	h, s := newTestHandler(t)
	fullKey, _, _ := s.CreateKey("k", "admin")

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+fullKey)
	rr := httptest.NewRecorder()
	h.RequireAdmin(okHandler)(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestRequireAdmin_OwnerKey(t *testing.T) {
	h, s := newTestHandler(t)
	fullKey, _, _ := s.CreateKey("k", RoleOwner)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+fullKey)
	rr := httptest.NewRecorder()
	h.RequireAdmin(okHandler)(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestRequireAdmin_UserKey(t *testing.T) {
	h, s := newTestHandler(t)
	fullKey, _, _ := s.CreateKey("k", "user")

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+fullKey)
	rr := httptest.NewRecorder()
	h.RequireAdmin(okHandler)(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}

	var payload struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if payload.Error.Type != "authorization_error" {
		t.Fatalf("expected authorization_error, got %q", payload.Error.Type)
	}
}

func TestRequireAdmin_NoKey(t *testing.T) {
	h, _ := newTestHandler(t)

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()
	h.RequireAdmin(okHandler)(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestRequirePermission_BillingCanManageQuotas(t *testing.T) {
	h, s := newTestHandler(t)
	fullKey, _, _ := s.CreateKey("billing", RoleBilling)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+fullKey)
	rr := httptest.NewRecorder()
	h.RequirePermission(PermissionManageQuotas, "quota access required", okHandler)(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestRequirePermission_BillingCannotManageInfrastructure(t *testing.T) {
	h, s := newTestHandler(t)
	fullKey, _, _ := s.CreateKey("billing", RoleBilling)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+fullKey)
	rr := httptest.NewRecorder()
	h.RequirePermission(PermissionManageInfrastructure, "infra access required", okHandler)(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

// ---------- Cookie auth ----------

func TestRequireAuth_ValidCookie(t *testing.T) {
	h, s := newTestHandler(t)
	_, rec, _ := s.CreateKey("k", "admin")
	token, _, _ := s.CreateSession(rec.ID)

	req := httptest.NewRequest("GET", "/test", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rr := httptest.NewRecorder()

	var ctxKey *KeyRecord
	var ctxSession *SessionRecord
	h.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		ctxKey = KeyFromContext(r.Context())
		ctxSession = SessionFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if ctxKey == nil || ctxKey.ID != rec.ID {
		t.Error("KeyFromContext missing or wrong after cookie auth")
	}
	if ctxSession == nil {
		t.Error("SessionFromContext missing after cookie auth")
	}
}

func TestRequireAuth_ExpiredCookieFallsThrough(t *testing.T) {
	h, _ := newTestHandler(t)

	req := httptest.NewRequest("GET", "/test", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "expired_token"})
	rr := httptest.NewRecorder()
	h.RequireAuth(okHandler)(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestRequireAuth_InvalidCookieValidBearer(t *testing.T) {
	h, s := newTestHandler(t)
	fullKey, _, _ := s.CreateKey("k", "user")

	req := httptest.NewRequest("GET", "/test", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "bad_token"})
	req.Header.Set("Authorization", "Bearer "+fullKey)
	rr := httptest.NewRecorder()
	h.RequireAuth(okHandler)(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 (Bearer fallback), got %d", rr.Code)
	}
}

func TestKeyFromContext_Nil(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	if KeyFromContext(req.Context()) != nil {
		t.Error("expected nil for empty context")
	}
}

func TestSessionFromContext_Nil(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	if SessionFromContext(req.Context()) != nil {
		t.Error("expected nil for empty context")
	}
}
