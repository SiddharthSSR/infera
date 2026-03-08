package auth

import (
	"path/filepath"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_auth.db")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateKey(t *testing.T) {
	s := newTestStore(t)

	t.Run("valid key", func(t *testing.T) {
		fullKey, record, err := s.CreateKey("test-key", "user")
		if err != nil {
			t.Fatalf("CreateKey failed: %v", err)
		}
		if !strings.HasPrefix(fullKey, "inf_") {
			t.Errorf("expected key to start with inf_, got %s", fullKey[:8])
		}
		if record.Name != "test-key" {
			t.Errorf("expected name test-key, got %s", record.Name)
		}
		if record.Role != "user" {
			t.Errorf("expected role user, got %s", record.Role)
		}
		if record.Status != "active" {
			t.Errorf("expected status active, got %s", record.Status)
		}
		if record.ID == "" {
			t.Error("expected non-empty ID")
		}
		if record.KeyPrefix == "" {
			t.Error("expected non-empty key prefix")
		}
	})

	t.Run("admin role", func(t *testing.T) {
		_, record, err := s.CreateKey("admin-key", "admin")
		if err != nil {
			t.Fatalf("CreateKey failed: %v", err)
		}
		if record.Role != "admin" {
			t.Errorf("expected role admin, got %s", record.Role)
		}
	})

	t.Run("empty role defaults to user", func(t *testing.T) {
		_, record, err := s.CreateKey("default-role", "")
		if err != nil {
			t.Fatalf("CreateKey failed: %v", err)
		}
		if record.Role != "user" {
			t.Errorf("expected role user, got %s", record.Role)
		}
	})

	t.Run("invalid role returns error", func(t *testing.T) {
		_, _, err := s.CreateKey("bad-role", "superuser")
		if err == nil {
			t.Error("expected error for invalid role")
		}
	})

	t.Run("empty name returns error", func(t *testing.T) {
		_, _, err := s.CreateKey("", "user")
		if err == nil {
			t.Error("expected error for empty name")
		}
	})
}

func TestValidateKey(t *testing.T) {
	s := newTestStore(t)

	t.Run("valid active key", func(t *testing.T) {
		fullKey, original, err := s.CreateKey("validate-test", "user")
		if err != nil {
			t.Fatalf("CreateKey failed: %v", err)
		}

		record, err := s.ValidateKey(fullKey)
		if err != nil {
			t.Fatalf("ValidateKey failed: %v", err)
		}
		if record.ID != original.ID {
			t.Errorf("expected ID %s, got %s", original.ID, record.ID)
		}
		if record.Name != "validate-test" {
			t.Errorf("expected name validate-test, got %s", record.Name)
		}
	})

	t.Run("revoked key returns error", func(t *testing.T) {
		fullKey, record, err := s.CreateKey("revoke-validate", "user")
		if err != nil {
			t.Fatalf("CreateKey failed: %v", err)
		}
		if err := s.RevokeKey(record.ID); err != nil {
			t.Fatalf("RevokeKey failed: %v", err)
		}

		_, err = s.ValidateKey(fullKey)
		if err == nil {
			t.Error("expected error validating revoked key")
		}
	})

	t.Run("nonexistent key returns error", func(t *testing.T) {
		_, err := s.ValidateKey("inf_nonexistent000000000000000000000000000000000000")
		if err == nil {
			t.Error("expected error for nonexistent key")
		}
	})
}

func TestListKeys(t *testing.T) {
	s := newTestStore(t)

	t.Run("empty list", func(t *testing.T) {
		keys, err := s.ListKeys()
		if err != nil {
			t.Fatalf("ListKeys failed: %v", err)
		}
		if len(keys) != 0 {
			t.Errorf("expected 0 keys, got %d", len(keys))
		}
	})

	t.Run("returns all keys ordered by created_at DESC", func(t *testing.T) {
		s.CreateKey("first", "user")
		s.CreateKey("second", "admin")

		keys, err := s.ListKeys()
		if err != nil {
			t.Fatalf("ListKeys failed: %v", err)
		}
		if len(keys) != 2 {
			t.Fatalf("expected 2 keys, got %d", len(keys))
		}
		// Most recent first
		if keys[0].Name != "second" {
			t.Errorf("expected first key to be 'second', got %s", keys[0].Name)
		}
		if keys[1].Name != "first" {
			t.Errorf("expected second key to be 'first', got %s", keys[1].Name)
		}
	})
}

func TestRevokeKey(t *testing.T) {
	s := newTestStore(t)

	t.Run("revoke active key", func(t *testing.T) {
		fullKey, record, err := s.CreateKey("revoke-me", "user")
		if err != nil {
			t.Fatalf("CreateKey failed: %v", err)
		}

		if err := s.RevokeKey(record.ID); err != nil {
			t.Fatalf("RevokeKey failed: %v", err)
		}

		// Key should no longer validate
		_, err = s.ValidateKey(fullKey)
		if err == nil {
			t.Error("expected error validating revoked key")
		}

		// Key should still appear in list with revoked status
		keys, _ := s.ListKeys()
		found := false
		for _, k := range keys {
			if k.ID == record.ID {
				found = true
				if k.Status != "revoked" {
					t.Errorf("expected status revoked, got %s", k.Status)
				}
			}
		}
		if !found {
			t.Error("revoked key should still appear in list")
		}
	})

	t.Run("nonexistent ID returns error", func(t *testing.T) {
		err := s.RevokeKey("nonexistent-id")
		if err == nil {
			t.Error("expected error revoking nonexistent key")
		}
	})
}

func TestDeleteKey(t *testing.T) {
	s := newTestStore(t)

	t.Run("delete existing key", func(t *testing.T) {
		_, record, err := s.CreateKey("delete-me", "user")
		if err != nil {
			t.Fatalf("CreateKey failed: %v", err)
		}

		if err := s.DeleteKey(record.ID); err != nil {
			t.Fatalf("DeleteKey failed: %v", err)
		}

		// Key should not appear in list
		keys, _ := s.ListKeys()
		for _, k := range keys {
			if k.ID == record.ID {
				t.Error("deleted key should not appear in list")
			}
		}
	})

	t.Run("nonexistent ID returns error", func(t *testing.T) {
		err := s.DeleteKey("nonexistent-id")
		if err == nil {
			t.Error("expected error deleting nonexistent key")
		}
	})
}

func TestCount(t *testing.T) {
	s := newTestStore(t)

	count, err := s.Count()
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0, got %d", count)
	}

	_, r1, _ := s.CreateKey("key1", "user")
	s.CreateKey("key2", "admin")

	count, err = s.Count()
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2, got %d", count)
	}

	// Revoke one — count should decrease
	s.RevokeKey(r1.ID)

	count, err = s.Count()
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 after revoke, got %d", count)
	}
}

func TestCreateKeyFromRaw(t *testing.T) {
	s := newTestStore(t)

	rawKey := "inf_abcdef1234567890abcdef1234567890abcdef12345678"
	record, err := s.CreateKeyFromRaw(rawKey, "bootstrap-admin", "admin")
	if err != nil {
		t.Fatalf("CreateKeyFromRaw failed: %v", err)
	}
	if record.Name != "bootstrap-admin" {
		t.Errorf("expected name bootstrap-admin, got %s", record.Name)
	}
	if record.Role != "admin" {
		t.Errorf("expected role admin, got %s", record.Role)
	}

	// Should validate
	validated, err := s.ValidateKey(rawKey)
	if err != nil {
		t.Fatalf("ValidateKey failed: %v", err)
	}
	if validated.ID != record.ID {
		t.Errorf("expected ID %s, got %s", record.ID, validated.ID)
	}

	t.Run("rejects short key", func(t *testing.T) {
		_, err := s.CreateKeyFromRaw("inf_short", "bad", "admin")
		if err == nil {
			t.Fatal("expected error for short key")
		}
	})

	t.Run("rejects invalid prefix", func(t *testing.T) {
		_, err := s.CreateKeyFromRaw("bad_abcdef1234567890abcdef1234567890abcdef12345678", "bad", "admin")
		if err == nil {
			t.Fatal("expected error for invalid key prefix")
		}
	})
}
