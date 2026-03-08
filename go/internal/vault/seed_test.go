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
		if count != 8 {
			t.Errorf("expected 8 seeded models, got %d", count)
		}

		// Verify families
		families, _ := s.ListFamilies()
		if len(families) < 4 {
			t.Errorf("expected at least 4 families, got %d: %v", len(families), families)
		}
	})

	t.Run("skips if models already exist", func(t *testing.T) {
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
		if count != 1 {
			t.Errorf("expected 1 model (seed should skip), got %d", count)
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
		if count != 8 {
			t.Errorf("expected 8 models after double seed, got %d", count)
		}
	})
}
