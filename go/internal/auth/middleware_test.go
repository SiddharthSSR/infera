package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func newTestHandler(t *testing.T) (*Handler, *Store) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_auth.db")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return NewHandler(s), s
}

func TestRequireAuth(t *testing.T) {
	h, s := newTestHandler(t)

	// Create a valid key
	fullKey, _, err := s.CreateKey("auth-test", "user")
	if err != nil {
		t.Fatalf("CreateKey failed: %v", err)
	}

	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	t.Run("valid Bearer token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+fullKey)
		w := httptest.NewRecorder()

		h.RequireAuth(okHandler)(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("valid X-API-Key header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("X-API-Key", fullKey)
		w := httptest.NewRecorder()

		h.RequireAuth(okHandler)(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("KeyRecord in context", func(t *testing.T) {
		var gotRecord *KeyRecord
		contextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotRecord = KeyFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer "+fullKey)
		w := httptest.NewRecorder()

		h.RequireAuth(contextHandler)(w, req)

		if gotRecord == nil {
			t.Fatal("expected KeyRecord in context, got nil")
		}
		if gotRecord.Name != "auth-test" {
			t.Errorf("expected name auth-test, got %s", gotRecord.Name)
		}
	})

	t.Run("no key returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()

		h.RequireAuth(okHandler)(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("invalid key returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Authorization", "Bearer inf_invalidkey000000000000000000000000000000000000")
		w := httptest.NewRecorder()

		h.RequireAuth(okHandler)(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})
}

func TestRequireAdmin(t *testing.T) {
	h, s := newTestHandler(t)

	adminKey, _, err := s.CreateKey("admin-key", "admin")
	if err != nil {
		t.Fatalf("CreateKey failed for admin key: %v", err)
	}
	userKey, _, err := s.CreateKey("user-key", "user")
	if err != nil {
		t.Fatalf("CreateKey failed for user key: %v", err)
	}

	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("admin key returns 200", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		req.Header.Set("Authorization", "Bearer "+adminKey)
		w := httptest.NewRecorder()

		h.RequireAdmin(okHandler)(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("user key returns 403", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		req.Header.Set("Authorization", "Bearer "+userKey)
		w := httptest.NewRecorder()

		h.RequireAdmin(okHandler)(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("no key returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		w := httptest.NewRecorder()

		h.RequireAdmin(okHandler)(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})
}

func TestKeyFromContext(t *testing.T) {
	t.Run("returns nil when no key in context", func(t *testing.T) {
		ctx := context.Background()
		record := KeyFromContext(ctx)
		if record != nil {
			t.Error("expected nil, got a record")
		}
	})
}
