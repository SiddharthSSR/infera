package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func newTestHandlerForHTTP(t *testing.T) *Handler {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_auth.db")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return NewHandler(s)
}

func TestHandleCreateKey(t *testing.T) {
	h := newTestHandlerForHTTP(t)

	t.Run("create key successfully", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"name": "my-key", "role": "user"})
		req := httptest.NewRequest(http.MethodPost, "/api/auth/keys", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.handleCreateKey(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)

		key, ok := resp["key"].(string)
		if !ok || key == "" {
			t.Error("expected full key in response")
		}

		record, ok := resp["record"].(map[string]interface{})
		if !ok {
			t.Fatal("expected record in response")
		}
		if record["name"] != "my-key" {
			t.Errorf("expected name my-key, got %v", record["name"])
		}
		if record["role"] != "user" {
			t.Errorf("expected role user, got %v", record["role"])
		}
	})

	t.Run("missing name returns 400", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"role": "user"})
		req := httptest.NewRequest(http.MethodPost, "/api/auth/keys", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.handleCreateKey(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("invalid JSON returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/keys", bytes.NewReader([]byte("not json")))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.handleCreateKey(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})
}

func TestHandleListKeys(t *testing.T) {
	h := newTestHandlerForHTTP(t)

	t.Run("empty list", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/auth/keys", nil)
		w := httptest.NewRecorder()

		h.handleListKeys(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)

		keys := resp["keys"].([]interface{})
		if len(keys) != 0 {
			t.Errorf("expected 0 keys, got %d", len(keys))
		}
		if resp["total"].(float64) != 0 {
			t.Errorf("expected total 0, got %v", resp["total"])
		}
	})

	t.Run("lists created keys", func(t *testing.T) {
		h.store.CreateKey("key-1", "user")
		h.store.CreateKey("key-2", "admin")

		req := httptest.NewRequest(http.MethodGet, "/api/auth/keys", nil)
		w := httptest.NewRecorder()

		h.handleListKeys(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)

		keys := resp["keys"].([]interface{})
		if len(keys) != 2 {
			t.Errorf("expected 2 keys, got %d", len(keys))
		}
	})
}

func TestHandleRevokeKey(t *testing.T) {
	h := newTestHandlerForHTTP(t)

	t.Run("revoke existing key", func(t *testing.T) {
		_, record, _ := h.store.CreateKey("revoke-target", "user")

		req := httptest.NewRequest(http.MethodDelete, "/api/auth/keys/"+record.ID, nil)
		w := httptest.NewRecorder()

		h.handleRevokeKey(w, req, record.ID)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)

		if resp["success"] != true {
			t.Error("expected success to be true")
		}
	})

	t.Run("nonexistent key returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/auth/keys/nonexistent", nil)
		w := httptest.NewRecorder()

		h.handleRevokeKey(w, req, "nonexistent")

		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}
	})
}

func TestHandleKeys(t *testing.T) {
	h := newTestHandlerForHTTP(t)

	t.Run("method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/api/auth/keys", nil)
		w := httptest.NewRecorder()

		h.handleKeys(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", w.Code)
		}
	})
}

func TestHandleKeyByID(t *testing.T) {
	h := newTestHandlerForHTTP(t)

	t.Run("empty ID returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/auth/keys/", nil)
		w := httptest.NewRecorder()

		h.handleKeyByID(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/api/auth/keys/some-id", nil)
		w := httptest.NewRecorder()

		h.handleKeyByID(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", w.Code)
		}
	})
}
