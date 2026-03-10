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
