package vault

import (
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_vault.db")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreate(t *testing.T) {
	s := newTestStore(t)

	t.Run("creates model with defaults", func(t *testing.T) {
		m := &Model{
			Name:      "Test Model",
			SourceURI: "test/model-7b",
		}
		if err := s.Create(m); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
		if m.ID == "" {
			t.Error("expected ID to be generated")
		}
		if m.Source != "huggingface" {
			t.Errorf("expected default source huggingface, got %s", m.Source)
		}
		if m.Status != "available" {
			t.Errorf("expected default status available, got %s", m.Status)
		}
		if m.Quantization != "none" {
			t.Errorf("expected default quantization none, got %s", m.Quantization)
		}
		if m.MaxContext != 4096 {
			t.Errorf("expected default max_context 4096, got %d", m.MaxContext)
		}
		if m.Tags == nil {
			t.Error("expected tags to be initialized")
		}
		if m.Metadata == nil {
			t.Error("expected metadata to be initialized")
		}
	})

	t.Run("creates model with explicit fields", func(t *testing.T) {
		m := &Model{
			Name:         "Llama 3",
			Source:       "huggingface",
			SourceURI:    "meta-llama/llama-3-8b",
			Parameters:   "8B",
			Quantization: "awq",
			VRAMRequired: 8192,
			MaxContext:   131072,
			Family:       "llama",
			Tags:         []string{"chat", "coding"},
			Metadata:     map[string]string{"license": "apache-2.0"},
			Status:       "testing",
		}
		if err := s.Create(m); err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		got, err := s.Get(m.ID)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if got.Family != "llama" {
			t.Errorf("expected family llama, got %s", got.Family)
		}
		if got.VRAMRequired != 8192 {
			t.Errorf("expected vram 8192, got %d", got.VRAMRequired)
		}
		if len(got.Tags) != 2 {
			t.Errorf("expected 2 tags, got %d", len(got.Tags))
		}
		if got.Metadata["license"] != "apache-2.0" {
			t.Errorf("expected metadata license apache-2.0, got %s", got.Metadata["license"])
		}
	})

	t.Run("creates model with custom ID", func(t *testing.T) {
		m := &Model{
			ID:        "custom-id-123",
			Name:      "Custom ID Model",
			SourceURI: "test/custom",
		}
		if err := s.Create(m); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
		if m.ID != "custom-id-123" {
			t.Errorf("expected custom ID, got %s", m.ID)
		}
	})
}

func TestGet(t *testing.T) {
	s := newTestStore(t)

	t.Run("returns model by ID", func(t *testing.T) {
		m := &Model{Name: "Get Test", SourceURI: "test/get"}
		if err := s.Create(m); err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		got, err := s.Get(m.ID)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if got.Name != "Get Test" {
			t.Errorf("expected name Get Test, got %s", got.Name)
		}
	})

	t.Run("returns error for nonexistent ID", func(t *testing.T) {
		_, err := s.Get("nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent model")
		}
	})
}

func TestUpdate(t *testing.T) {
	s := newTestStore(t)

	t.Run("updates existing model", func(t *testing.T) {
		m := &Model{Name: "Original", SourceURI: "test/update", Family: "test"}
		if err := s.Create(m); err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		m.Name = "Updated"
		m.Family = "updated-family"
		m.Tags = []string{"new-tag"}
		m.Metadata = map[string]string{"key": "value"}
		if err := s.Update(m); err != nil {
			t.Fatalf("Update failed: %v", err)
		}

		got, _ := s.Get(m.ID)
		if got.Name != "Updated" {
			t.Errorf("expected name Updated, got %s", got.Name)
		}
		if got.Family != "updated-family" {
			t.Errorf("expected family updated-family, got %s", got.Family)
		}
		if len(got.Tags) != 1 || got.Tags[0] != "new-tag" {
			t.Errorf("expected tags [new-tag], got %v", got.Tags)
		}
	})

	t.Run("returns error for nonexistent model", func(t *testing.T) {
		m := &Model{ID: "nonexistent", Name: "X", SourceURI: "x"}
		err := s.Update(m)
		if err == nil {
			t.Error("expected error updating nonexistent model")
		}
	})
}

func TestDelete(t *testing.T) {
	s := newTestStore(t)

	t.Run("deletes existing model", func(t *testing.T) {
		m := &Model{Name: "Delete Me", SourceURI: "test/delete"}
		if err := s.Create(m); err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		if err := s.Delete(m.ID); err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		_, err := s.Get(m.ID)
		if err == nil {
			t.Error("expected error getting deleted model")
		}
	})

	t.Run("returns error for nonexistent model", func(t *testing.T) {
		err := s.Delete("nonexistent")
		if err == nil {
			t.Error("expected error deleting nonexistent model")
		}
	})
}

func TestList(t *testing.T) {
	s := newTestStore(t)

	// Seed some models
	if err := s.Create(&Model{Name: "Llama 8B", SourceURI: "meta-llama/llama-8b", Family: "llama", Quantization: "none", VRAMRequired: 16384, Tags: []string{"chat"}, Status: "available"}); err != nil {
		t.Fatalf("Create llama: %v", err)
	}
	if err := s.Create(&Model{Name: "Mistral 7B AWQ", SourceURI: "mistral/7b-awq", Family: "mistral", Quantization: "awq", VRAMRequired: 6144, Tags: []string{"chat", "quantized"}, Status: "available"}); err != nil {
		t.Fatalf("Create mistral: %v", err)
	}
	if err := s.Create(&Model{Name: "Old Model", SourceURI: "old/model", Family: "llama", VRAMRequired: 9000, Status: "deprecated"}); err != nil {
		t.Fatalf("Create old model: %v", err)
	}

	t.Run("list all", func(t *testing.T) {
		models, err := s.List(nil)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(models) != 3 {
			t.Errorf("expected 3 models, got %d", len(models))
		}
	})

	t.Run("filter by family", func(t *testing.T) {
		models, _ := s.List(&ModelFilter{Family: "llama"})
		if len(models) != 2 {
			t.Errorf("expected 2 llama models, got %d", len(models))
		}
	})

	t.Run("filter by status", func(t *testing.T) {
		models, _ := s.List(&ModelFilter{Status: "deprecated"})
		if len(models) != 1 {
			t.Errorf("expected 1 deprecated model, got %d", len(models))
		}
	})

	t.Run("filter by quantization", func(t *testing.T) {
		models, _ := s.List(&ModelFilter{Quantization: "awq"})
		if len(models) != 1 {
			t.Errorf("expected 1 awq model, got %d", len(models))
		}
	})

	t.Run("filter by min VRAM", func(t *testing.T) {
		models, _ := s.List(&ModelFilter{MinVRAM: 10000})
		if len(models) != 1 {
			t.Errorf("expected 1 model with vram >= 10000, got %d", len(models))
		}
	})

	t.Run("filter by max VRAM", func(t *testing.T) {
		models, _ := s.List(&ModelFilter{MaxVRAM: 8000})
		if len(models) != 1 {
			t.Errorf("expected 1 model with vram <= 8000, got %d", len(models))
		}
	})

	t.Run("filter by tag", func(t *testing.T) {
		models, _ := s.List(&ModelFilter{Tag: "quantized"})
		if len(models) != 1 {
			t.Errorf("expected 1 model with tag quantized, got %d", len(models))
		}
	})

	t.Run("filter by search", func(t *testing.T) {
		models, _ := s.List(&ModelFilter{Search: "mistral"})
		if len(models) != 1 {
			t.Errorf("expected 1 model matching mistral, got %d", len(models))
		}
	})

	t.Run("combined filters", func(t *testing.T) {
		models, _ := s.List(&ModelFilter{Family: "llama", Status: "available"})
		if len(models) != 1 {
			t.Errorf("expected 1 available llama model, got %d", len(models))
		}
	})

	t.Run("empty result", func(t *testing.T) {
		models, err := s.List(&ModelFilter{Family: "nonexistent"})
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}
		if len(models) != 0 {
			t.Errorf("expected 0 models, got %d", len(models))
		}
	})
}

func TestListFamilies(t *testing.T) {
	s := newTestStore(t)

	if err := s.Create(&Model{Name: "A", SourceURI: "a", Family: "llama"}); err != nil {
		t.Fatalf("Create A: %v", err)
	}
	if err := s.Create(&Model{Name: "B", SourceURI: "b", Family: "mistral"}); err != nil {
		t.Fatalf("Create B: %v", err)
	}
	if err := s.Create(&Model{Name: "C", SourceURI: "c", Family: "llama"}); err != nil {
		t.Fatalf("Create C: %v", err)
	}
	if err := s.Create(&Model{Name: "D", SourceURI: "d", Family: ""}); err != nil { // no family
		t.Fatalf("Create D: %v", err)
	}

	families, err := s.ListFamilies()
	if err != nil {
		t.Fatalf("ListFamilies failed: %v", err)
	}
	if len(families) != 2 {
		t.Errorf("expected 2 families, got %d: %v", len(families), families)
	}
}

func TestStats(t *testing.T) {
	s := newTestStore(t)

	if err := s.Create(&Model{Name: "A", SourceURI: "a", Family: "llama", Status: "available"}); err != nil {
		t.Fatalf("Create A: %v", err)
	}
	if err := s.Create(&Model{Name: "B", SourceURI: "b", Family: "mistral", Status: "available"}); err != nil {
		t.Fatalf("Create B: %v", err)
	}
	if err := s.Create(&Model{Name: "C", SourceURI: "c", Family: "llama", Status: "deprecated"}); err != nil {
		t.Fatalf("Create C: %v", err)
	}

	stats, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}
	if stats.TotalModels != 3 {
		t.Errorf("expected 3 total, got %d", stats.TotalModels)
	}
	if stats.AvailableModels != 2 {
		t.Errorf("expected 2 available, got %d", stats.AvailableModels)
	}
	if stats.DeprecatedModels != 1 {
		t.Errorf("expected 1 deprecated, got %d", stats.DeprecatedModels)
	}
	if stats.ModelFamilies != 2 {
		t.Errorf("expected 2 families, got %d", stats.ModelFamilies)
	}
}

func TestCount(t *testing.T) {
	s := newTestStore(t)

	count, _ := s.Count()
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}

	if err := s.Create(&Model{Name: "A", SourceURI: "a"}); err != nil {
		t.Fatalf("Create A: %v", err)
	}
	if err := s.Create(&Model{Name: "B", SourceURI: "b"}); err != nil {
		t.Fatalf("Create B: %v", err)
	}

	count, _ = s.Count()
	if count != 2 {
		t.Errorf("expected 2, got %d", count)
	}
}
