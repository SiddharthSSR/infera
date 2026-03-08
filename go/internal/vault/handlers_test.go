package vault

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_vault.db")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return NewHandler(s)
}

func TestHandleCreateModel(t *testing.T) {
	h := newTestHandler(t)

	t.Run("creates model successfully", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"name":       "Test Model",
			"source_uri": "test/model-7b",
			"family":     "test",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/vault/models", bytes.NewReader(body))
		w := httptest.NewRecorder()

		h.createModel(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}

		var resp Model
		json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Name != "Test Model" {
			t.Errorf("expected name Test Model, got %s", resp.Name)
		}
		if resp.ID == "" {
			t.Error("expected ID in response")
		}
	})

	t.Run("missing name returns 400", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"source_uri": "test/model"})
		req := httptest.NewRequest(http.MethodPost, "/api/vault/models", bytes.NewReader(body))
		w := httptest.NewRecorder()

		h.createModel(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("missing source_uri returns 400", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"name": "X"})
		req := httptest.NewRequest(http.MethodPost, "/api/vault/models", bytes.NewReader(body))
		w := httptest.NewRecorder()

		h.createModel(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("invalid JSON returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/vault/models", bytes.NewReader([]byte("not json")))
		w := httptest.NewRecorder()

		h.createModel(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})
}

func TestHandleListModels(t *testing.T) {
	h := newTestHandler(t)

	h.store.Create(&Model{Name: "A", SourceURI: "a", Family: "llama", Status: "available"})
	h.store.Create(&Model{Name: "B", SourceURI: "b", Family: "mistral", Status: "available"})

	t.Run("lists all models", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/vault/models", nil)
		w := httptest.NewRecorder()

		h.listModels(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		models := resp["models"].([]interface{})
		if len(models) != 2 {
			t.Errorf("expected 2 models, got %d", len(models))
		}
		if resp["count"].(float64) != 2 {
			t.Errorf("expected count 2, got %v", resp["count"])
		}
	})

	t.Run("filters by family query param", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/vault/models?family=llama", nil)
		w := httptest.NewRecorder()

		h.listModels(w, req)

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		models := resp["models"].([]interface{})
		if len(models) != 1 {
			t.Errorf("expected 1 model, got %d", len(models))
		}
	})
}

func TestHandleModelByID(t *testing.T) {
	h := newTestHandler(t)

	m := &Model{Name: "Target", SourceURI: "target/model", Family: "test"}
	h.store.Create(m)

	t.Run("GET model by ID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/vault/models/"+m.ID, nil)
		w := httptest.NewRecorder()

		h.handleModelByID(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp Model
		json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Name != "Target" {
			t.Errorf("expected name Target, got %s", resp.Name)
		}
	})

	t.Run("GET nonexistent returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/vault/models/nonexistent", nil)
		w := httptest.NewRecorder()

		h.handleModelByID(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}
	})

	t.Run("PUT updates model", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"name":       "Updated Target",
			"source_uri": "target/model",
			"family":     "updated",
		})
		req := httptest.NewRequest(http.MethodPut, "/api/vault/models/"+m.ID, bytes.NewReader(body))
		w := httptest.NewRecorder()

		h.handleModelByID(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp Model
		json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Name != "Updated Target" {
			t.Errorf("expected Updated Target, got %s", resp.Name)
		}
	})

	t.Run("DELETE model", func(t *testing.T) {
		dm := &Model{Name: "Delete", SourceURI: "delete/me"}
		h.store.Create(dm)

		req := httptest.NewRequest(http.MethodDelete, "/api/vault/models/"+dm.ID, nil)
		w := httptest.NewRecorder()

		h.handleModelByID(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("DELETE nonexistent returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/vault/models/nonexistent", nil)
		w := httptest.NewRecorder()

		h.handleModelByID(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}
	})

	t.Run("empty ID returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/vault/models/", nil)
		w := httptest.NewRecorder()

		h.handleModelByID(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})
}

func TestHandleFamilies(t *testing.T) {
	h := newTestHandler(t)

	h.store.Create(&Model{Name: "A", SourceURI: "a", Family: "llama"})
	h.store.Create(&Model{Name: "B", SourceURI: "b", Family: "mistral"})

	t.Run("returns families", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/vault/models/families", nil)
		w := httptest.NewRecorder()

		h.handleFamilies(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		families := resp["families"].([]interface{})
		if len(families) != 2 {
			t.Errorf("expected 2 families, got %d", len(families))
		}
	})

	t.Run("method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/vault/models/families", nil)
		w := httptest.NewRecorder()

		h.handleFamilies(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", w.Code)
		}
	})
}

func TestHandleStats(t *testing.T) {
	h := newTestHandler(t)

	h.store.Create(&Model{Name: "A", SourceURI: "a", Family: "llama", Status: "available"})
	h.store.Create(&Model{Name: "B", SourceURI: "b", Family: "mistral", Status: "deprecated"})

	req := httptest.NewRequest(http.MethodGet, "/api/vault/stats", nil)
	w := httptest.NewRecorder()

	h.handleStats(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp Stats
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.TotalModels != 2 {
		t.Errorf("expected 2 total, got %d", resp.TotalModels)
	}
	if resp.AvailableModels != 1 {
		t.Errorf("expected 1 available, got %d", resp.AvailableModels)
	}
}

func TestHandleModelsRouting(t *testing.T) {
	h := newTestHandler(t)

	t.Run("method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/api/vault/models", nil)
		w := httptest.NewRecorder()

		h.handleModels(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", w.Code)
		}
	})
}
