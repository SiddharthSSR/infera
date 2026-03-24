package vault

import (
	"path/filepath"
	"testing"
)

func TestSeedDefaultModels(t *testing.T) {
	t.Run("seeds empty database", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "test_seed.db")
		s, err := NewStore(dbPath)
		if err != nil {
			t.Fatalf("failed to create store: %v", err)
		}
		defer s.Close()

			if err := SeedDefaultModels(s); err != nil {
				t.Fatalf("SeedDefaultModels failed: %v", err)
			}

			count, _ := s.Count()
			if count != 13 {
				t.Errorf("expected 13 seeded models, got %d", count)
			}

			// Verify families
			families, _ := s.ListFamilies()
			if len(families) < 5 {
				t.Errorf("expected at least 5 families, got %d: %v", len(families), families)
			}

			quantized, err := s.List(&ModelFilter{Tag: "quantized"})
			if err != nil {
				t.Fatalf("list quantized models: %v", err)
			}
			if len(quantized) != 4 {
				t.Fatalf("expected 4 seeded quantized models, got %d", len(quantized))
			}

			wantSourceURIs := []string{
				"solidrust/Mistral-7B-Instruct-v0.3-AWQ",
				"Qwen/Qwen2.5-7B-Instruct-AWQ",
				"Qwen/Qwen2.5-7B-Instruct-GPTQ-Int4",
				"Qwen/Qwen2.5-7B-Instruct-GPTQ-Int8",
			}
			for _, sourceURI := range wantSourceURIs {
				if _, err := s.GetBySourceURI(sourceURI); err != nil {
					t.Fatalf("expected seeded model %q: %v", sourceURI, err)
				}
			}
		})

	t.Run("adds missing defaults without duplicating existing models", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "test_seed_skip.db")
		s, err := NewStore(dbPath)
		if err != nil {
			t.Fatalf("failed to create store: %v", err)
		}
		defer s.Close()

		// Add one model first
		s.Create(&Model{Name: "Existing", SourceURI: "existing/model"})

			if err := SeedDefaultModels(s); err != nil {
				t.Fatalf("SeedDefaultModels failed: %v", err)
			}

			count, _ := s.Count()
			if count != 14 {
				t.Errorf("expected 14 models after additive seed, got %d", count)
			}
		})

	t.Run("idempotent — double seed same result", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "test_seed_idem.db")
		s, err := NewStore(dbPath)
		if err != nil {
			t.Fatalf("failed to create store: %v", err)
		}
		defer s.Close()

			SeedDefaultModels(s)
			SeedDefaultModels(s)

			count, _ := s.Count()
			if count != 13 {
				t.Errorf("expected 13 models after double seed, got %d", count)
			}
		})
	}
