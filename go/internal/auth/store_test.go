package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(filepath.Join(dir, "auth_test.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// ---------- Key CRUD ----------

func TestCreateKey(t *testing.T) {
	s := newTestStore(t)
	fullKey, rec, err := s.CreateKey("test-key", "user")
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}
	if !strings.HasPrefix(fullKey, "inf_") {
		t.Errorf("expected key prefix inf_, got %s", fullKey[:4])
	}
	if rec.Name != "test-key" || rec.Role != "user" || rec.Status != "active" {
		t.Errorf("unexpected record: %+v", rec)
	}
}

func TestCreateKey_MissingName(t *testing.T) {
	s := newTestStore(t)
	_, _, err := s.CreateKey("", "user")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestCreateKey_InvalidRole(t *testing.T) {
	s := newTestStore(t)
	_, _, err := s.CreateKey("k", "superuser")
	if err == nil {
		t.Fatal("expected error for invalid role")
	}
}

func TestValidateKey(t *testing.T) {
	s := newTestStore(t)
	fullKey, _, err := s.CreateKey("k", "admin")
	if err != nil {
		t.Fatalf("CreateKey: %v", err)
	}

	rec, err := s.ValidateKey(fullKey)
	if err != nil {
		t.Fatalf("ValidateKey: %v", err)
	}
	if rec.Role != "admin" {
		t.Errorf("expected admin, got %s", rec.Role)
	}
}

func TestValidateKey_Invalid(t *testing.T) {
	s := newTestStore(t)
	_, err := s.ValidateKey("inf_0000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected error for nonexistent key")
	}
}

func TestValidateKey_Revoked(t *testing.T) {
	s := newTestStore(t)
	fullKey, rec, _ := s.CreateKey("k", "user")
	_ = s.RevokeKey(rec.ID)

	_, err := s.ValidateKey(fullKey)
	if err == nil {
		t.Fatal("expected error for revoked key")
	}
}

func TestListKeys(t *testing.T) {
	s := newTestStore(t)
	s.CreateKey("a", "user")
	s.CreateKey("b", "admin")

	keys, err := s.ListKeys()
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
}

func TestRevokeKey(t *testing.T) {
	s := newTestStore(t)
	_, rec, _ := s.CreateKey("k", "user")
	if err := s.RevokeKey(rec.ID); err != nil {
		t.Fatalf("RevokeKey: %v", err)
	}
	if err := s.RevokeKey(rec.ID); err == nil {
		t.Fatal("expected error revoking already-revoked key")
	}
}

func TestDeleteKey(t *testing.T) {
	s := newTestStore(t)
	_, rec, _ := s.CreateKey("k", "user")
	if err := s.DeleteKey(rec.ID); err != nil {
		t.Fatalf("DeleteKey: %v", err)
	}
	if err := s.DeleteKey(rec.ID); err == nil {
		t.Fatal("expected error deleting nonexistent key")
	}
}

func TestCount(t *testing.T) {
	s := newTestStore(t)
	c, _ := s.Count()
	if c != 0 {
		t.Fatalf("expected 0, got %d", c)
	}
	s.CreateKey("a", "user")
	s.CreateKey("b", "user")
	c, _ = s.Count()
	if c != 2 {
		t.Fatalf("expected 2, got %d", c)
	}
}

func TestCreateKeyFromRaw(t *testing.T) {
	s := newTestStore(t)
	raw := "inf_" + strings.Repeat("ab", 24)
	rec, err := s.CreateKeyFromRaw(raw, "bootstrap", "admin")
	if err != nil {
		t.Fatalf("CreateKeyFromRaw: %v", err)
	}
	if rec.Role != "admin" {
		t.Errorf("expected admin, got %s", rec.Role)
	}

	validated, err := s.ValidateKey(raw)
	if err != nil {
		t.Fatalf("ValidateKey after CreateKeyFromRaw: %v", err)
	}
	if validated.ID != rec.ID {
		t.Errorf("ID mismatch")
	}
}

func TestCreateKeyFromRaw_InvalidFormat(t *testing.T) {
	s := newTestStore(t)
	_, err := s.CreateKeyFromRaw("bad_key", "test", "admin")
	if err == nil {
		t.Fatal("expected error for invalid key format")
	}
}

// ---------- Session CRUD ----------

func TestCreateSession(t *testing.T) {
	s := newTestStore(t)
	_, rec, _ := s.CreateKey("k", "admin")

	token, session, err := s.CreateSession(rec.ID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	if session.KeyID != rec.ID {
		t.Errorf("expected key_id %s, got %s", rec.ID, session.KeyID)
	}
	if session.ExpiresAt.Before(time.Now()) {
		t.Error("session already expired")
	}
}

func TestValidateSession(t *testing.T) {
	s := newTestStore(t)
	_, rec, _ := s.CreateKey("k", "admin")
	token, _, _ := s.CreateSession(rec.ID)

	session, key, err := s.ValidateSession(token)
	if err != nil {
		t.Fatalf("ValidateSession: %v", err)
	}
	if session.KeyID != rec.ID {
		t.Errorf("session key_id mismatch")
	}
	if key.ID != rec.ID {
		t.Errorf("key ID mismatch")
	}
}

func TestValidateSession_Invalid(t *testing.T) {
	s := newTestStore(t)
	_, _, err := s.ValidateSession("nonexistent_token")
	if err == nil {
		t.Fatal("expected error for invalid session token")
	}
}

func TestValidateSession_Expired(t *testing.T) {
	s := newTestStore(t)
	_, rec, _ := s.CreateKey("k", "admin")
	token, session, _ := s.CreateSession(rec.ID)

	_, err := s.db.Exec("UPDATE sessions SET expires_at = ? WHERE id = ?",
		time.Now().Add(-1*time.Hour), session.ID)
	if err != nil {
		t.Fatalf("failed to expire session: %v", err)
	}

	_, _, err = s.ValidateSession(token)
	if err == nil {
		t.Fatal("expected error for expired session")
	}
}

func TestValidateSession_RevokedKey(t *testing.T) {
	s := newTestStore(t)
	_, rec, _ := s.CreateKey("k", "admin")
	token, _, _ := s.CreateSession(rec.ID)

	_ = s.RevokeKey(rec.ID)

	_, _, err := s.ValidateSession(token)
	if err == nil {
		t.Fatal("expected error for session with revoked key")
	}
}

func TestDeleteSession(t *testing.T) {
	s := newTestStore(t)
	_, rec, _ := s.CreateKey("k", "admin")
	token, session, _ := s.CreateSession(rec.ID)

	if err := s.DeleteSession(session.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	_, _, err := s.ValidateSession(token)
	if err == nil {
		t.Fatal("expected error after DeleteSession")
	}
}

func TestDeleteSessionByToken(t *testing.T) {
	s := newTestStore(t)
	_, rec, _ := s.CreateKey("k", "admin")
	token, _, _ := s.CreateSession(rec.ID)

	if err := s.DeleteSessionByToken(token); err != nil {
		t.Fatalf("DeleteSessionByToken: %v", err)
	}

	_, _, err := s.ValidateSession(token)
	if err == nil {
		t.Fatal("expected error after DeleteSessionByToken")
	}
}

func TestNewStore_BadPath(t *testing.T) {
	_, err := NewStore(filepath.Join(os.DevNull, "impossible", "path.db"))
	if err == nil {
		t.Fatal("expected error for bad path")
	}
}
