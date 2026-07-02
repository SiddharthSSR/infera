package auth

import (
	"errors"
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
	if rec.PrincipalType != PrincipalHuman {
		t.Fatalf("expected human principal, got %q", rec.PrincipalType)
	}
	if rec.WorkspaceID != DefaultWorkspaceID {
		t.Fatalf("expected default workspace, got %q", rec.WorkspaceID)
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

func TestCreateKey_InvalidPrincipalType(t *testing.T) {
	s := newTestStore(t)
	_, _, err := s.CreateKeyWithPrincipal("svc", RoleAdmin, "robot")
	if err == nil {
		t.Fatal("expected error for invalid principal type")
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
	if rec.PrincipalType != PrincipalHuman {
		t.Fatalf("expected human principal, got %q", rec.PrincipalType)
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
	for _, key := range keys {
		if key.WorkspaceID == "" {
			t.Fatalf("expected workspace data on listed key: %+v", key)
		}
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
	if validated.WorkspaceID != DefaultWorkspaceID {
		t.Fatalf("expected default workspace, got %q", validated.WorkspaceID)
	}
}

func TestCreateWorkspaceAndScopedKey(t *testing.T) {
	s := newTestStore(t)

	workspace, err := s.CreateWorkspace("Acme Team")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	_, rec, err := s.CreateKeyInWorkspace(workspace.ID, "acme-admin", "admin")
	if err != nil {
		t.Fatalf("CreateKeyInWorkspace: %v", err)
	}

	if rec.WorkspaceID != workspace.ID {
		t.Fatalf("expected workspace %q, got %q", workspace.ID, rec.WorkspaceID)
	}
	if rec.WorkspaceSlug != "acme-team" {
		t.Fatalf("expected slug acme-team, got %q", rec.WorkspaceSlug)
	}

	keys, err := s.ListKeysByWorkspace(workspace.ID)
	if err != nil {
		t.Fatalf("ListKeysByWorkspace: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key in workspace, got %d", len(keys))
	}
}

func TestCreateServiceAccountKey(t *testing.T) {
	s := newTestStore(t)

	key, rec, err := s.CreateKeyWithPrincipal("ci-bot", RoleOperator, PrincipalServiceAccount)
	if err != nil {
		t.Fatalf("CreateKeyWithPrincipal: %v", err)
	}
	if rec.PrincipalType != PrincipalServiceAccount {
		t.Fatalf("expected service_account principal, got %q", rec.PrincipalType)
	}

	validated, err := s.ValidateKey(key)
	if err != nil {
		t.Fatalf("ValidateKey: %v", err)
	}
	if validated.PrincipalType != PrincipalServiceAccount {
		t.Fatalf("expected service_account principal after validate, got %q", validated.PrincipalType)
	}
}

func TestListAccessibleWorkspaces_MembershipSession(t *testing.T) {
	s := newTestStore(t)

	_, adminRec, err := s.CreateKey("admin", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateKey admin: %v", err)
	}

	workspaceA, err := s.CreateWorkspace("Alpha Team")
	if err != nil {
		t.Fatalf("CreateWorkspace alpha: %v", err)
	}
	workspaceB, err := s.CreateWorkspace("Beta Team")
	if err != nil {
		t.Fatalf("CreateWorkspace beta: %v", err)
	}

	tokenA, _, err := s.CreateWorkspaceInvitation(workspaceA.ID, "member@example.com", "Member", RoleOperator, adminRec.ID, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation alpha: %v", err)
	}
	tokenB, _, err := s.CreateWorkspaceInvitation(workspaceB.ID, "member@example.com", "Member", RoleDeveloper, adminRec.ID, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation beta: %v", err)
	}

	_, _, currentKey, err := s.AcceptWorkspaceInvitation(tokenA, "Joined Member")
	if err != nil {
		t.Fatalf("AcceptWorkspaceInvitation alpha: %v", err)
	}
	if _, _, _, err := s.AcceptWorkspaceInvitation(tokenB, "Joined Member"); err != nil {
		t.Fatalf("AcceptWorkspaceInvitation beta: %v", err)
	}

	workspaces, err := s.ListAccessibleWorkspaces(currentKey)
	if err != nil {
		t.Fatalf("ListAccessibleWorkspaces: %v", err)
	}
	if len(workspaces) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(workspaces))
	}
}

func TestSwitchSessionWorkspace(t *testing.T) {
	s := newTestStore(t)

	_, adminRec, err := s.CreateKey("admin", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateKey admin: %v", err)
	}

	workspaceA, err := s.CreateWorkspace("Alpha Team")
	if err != nil {
		t.Fatalf("CreateWorkspace alpha: %v", err)
	}
	workspaceB, err := s.CreateWorkspace("Beta Team")
	if err != nil {
		t.Fatalf("CreateWorkspace beta: %v", err)
	}

	tokenA, _, err := s.CreateWorkspaceInvitation(workspaceA.ID, "member@example.com", "Member", RoleOperator, adminRec.ID, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation alpha: %v", err)
	}
	tokenB, _, err := s.CreateWorkspaceInvitation(workspaceB.ID, "member@example.com", "Member", RoleDeveloper, adminRec.ID, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation beta: %v", err)
	}

	_, fullKey, record, err := s.AcceptWorkspaceInvitation(tokenA, "Joined Member")
	if err != nil {
		t.Fatalf("AcceptWorkspaceInvitation alpha: %v", err)
	}
	if _, _, _, err := s.AcceptWorkspaceInvitation(tokenB, "Joined Member"); err != nil {
		t.Fatalf("AcceptWorkspaceInvitation beta: %v", err)
	}

	validated, err := s.ValidateKey(fullKey)
	if err != nil {
		t.Fatalf("ValidateKey: %v", err)
	}
	sessionToken, _, err := s.CreateSession(validated.ID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	switchedSession, switchedKey, err := s.SwitchSessionWorkspace(sessionToken, workspaceB.ID)
	if err != nil {
		t.Fatalf("SwitchSessionWorkspace: %v", err)
	}
	if switchedSession.ID == "" {
		t.Fatal("expected session record after switch")
	}
	if switchedKey.WorkspaceID != workspaceB.ID {
		t.Fatalf("expected switched workspace %q, got %q", workspaceB.ID, switchedKey.WorkspaceID)
	}
	if switchedKey.MemberEmail == nil || *switchedKey.MemberEmail != "member@example.com" {
		t.Fatalf("expected switched member email, got %+v", switchedKey.MemberEmail)
	}

	_, currentSessionKey, err := s.ValidateSession(sessionToken)
	if err != nil {
		t.Fatalf("ValidateSession after switch: %v", err)
	}
	if currentSessionKey.WorkspaceID != workspaceB.ID {
		t.Fatalf("expected persisted switched workspace %q, got %q", workspaceB.ID, currentSessionKey.WorkspaceID)
	}
	if record.WorkspaceID == workspaceB.ID {
		t.Fatal("expected original record to remain on first workspace")
	}
}

func TestWorkspaceQuotaLifecycle(t *testing.T) {
	s := newTestStore(t)

	workspace, err := s.CreateWorkspace("Billing Team")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	quota, err := s.GetWorkspaceQuota(workspace.ID)
	if err != nil {
		t.Fatalf("GetWorkspaceQuota default: %v", err)
	}
	if quota.MonthlyRequestLimit != nil || quota.MonthlyTokenLimit != nil {
		t.Fatalf("expected empty default quota, got %+v", quota)
	}
	if !quota.EnforceHardLimits {
		t.Fatal("expected hard limit enforcement enabled by default")
	}

	requestLimit := int64(1000)
	tokenLimit := int64(50000)
	quota, err = s.UpsertWorkspaceQuota(workspace.ID, &requestLimit, &tokenLimit, false)
	if err != nil {
		t.Fatalf("UpsertWorkspaceQuota: %v", err)
	}
	if quota.MonthlyRequestLimit == nil || *quota.MonthlyRequestLimit != requestLimit {
		t.Fatalf("expected request limit %d, got %+v", requestLimit, quota.MonthlyRequestLimit)
	}
	if quota.MonthlyTokenLimit == nil || *quota.MonthlyTokenLimit != tokenLimit {
		t.Fatalf("expected token limit %d, got %+v", tokenLimit, quota.MonthlyTokenLimit)
	}
	if quota.EnforceHardLimits {
		t.Fatal("expected hard limit enforcement to be false after update")
	}
}

func TestWorkspaceProviderConfigLifecycle(t *testing.T) {
	s := newTestStore(t)

	workspace, err := s.CreateWorkspace("Infra Team")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	config, err := s.UpsertWorkspaceProviderConfig(workspace.ID, "runpod", "rp_key", "", "https://api.runpod.io/graphql")
	if err != nil {
		t.Fatalf("UpsertWorkspaceProviderConfig: %v", err)
	}
	if !config.Configured {
		t.Fatal("expected configured=true")
	}
	if config.Endpoint != "https://api.runpod.io/graphql" {
		t.Fatalf("expected endpoint to round-trip, got %q", config.Endpoint)
	}

	listed, err := s.ListWorkspaceProviderConfigs(workspace.ID)
	if err != nil {
		t.Fatalf("ListWorkspaceProviderConfigs: %v", err)
	}
	if len(listed) != 1 || listed[0].Provider != "runpod" {
		t.Fatalf("expected one runpod config, got %+v", listed)
	}

	apiKey, apiSecret, endpoint, err := s.ResolveWorkspaceProviderConfig(workspace.ID, "runpod")
	if err != nil {
		t.Fatalf("ResolveWorkspaceProviderConfig: %v", err)
	}
	if apiKey != "rp_key" || apiSecret != "" || endpoint != "https://api.runpod.io/graphql" {
		t.Fatalf("unexpected resolved provider config: %q %q %q", apiKey, apiSecret, endpoint)
	}

	if err := s.DeleteWorkspaceProviderConfig(workspace.ID, "runpod"); err != nil {
		t.Fatalf("DeleteWorkspaceProviderConfig: %v", err)
	}
	if _, _, _, err := s.ResolveWorkspaceProviderConfig(workspace.ID, "runpod"); err == nil {
		t.Fatal("expected resolve to fail after delete")
	} else if !errors.Is(err, ErrWorkspaceProviderConfigNotFound) {
		t.Fatalf("expected ErrWorkspaceProviderConfigNotFound, got %v", err)
	}
}

func TestCreateKeyFromRaw_InvalidFormat(t *testing.T) {
	s := newTestStore(t)
	_, err := s.CreateKeyFromRaw("bad_key", "test", "admin")
	if err == nil {
		t.Fatal("expected error for invalid key format")
	}
}

func TestWorkspaceInvitationLifecycle(t *testing.T) {
	s := newTestStore(t)

	adminKey, adminRec, err := s.CreateKey("admin", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateKey admin: %v", err)
	}
	_ = adminKey

	workspace, err := s.CreateWorkspace("Acme Team")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	token, invitation, err := s.CreateWorkspaceInvitation(workspace.ID, "teammate@example.com", "Teammate", RoleDeveloper, adminRec.ID, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation: %v", err)
	}
	if token == "" {
		t.Fatal("expected invitation token")
	}
	if invitation.Status != "pending" {
		t.Fatalf("expected pending invitation, got %q", invitation.Status)
	}

	invitations, err := s.ListWorkspaceInvitations(workspace.ID)
	if err != nil {
		t.Fatalf("ListWorkspaceInvitations: %v", err)
	}
	if len(invitations) != 1 {
		t.Fatalf("expected 1 invitation, got %d", len(invitations))
	}

	membership, fullKey, record, err := s.AcceptWorkspaceInvitation(token, "Teammate Override")
	if err != nil {
		t.Fatalf("AcceptWorkspaceInvitation: %v", err)
	}
	if membership.WorkspaceID != workspace.ID {
		t.Fatalf("expected membership workspace %q, got %q", workspace.ID, membership.WorkspaceID)
	}
	if membership.DisplayName != "Teammate Override" {
		t.Fatalf("expected overridden display name, got %q", membership.DisplayName)
	}
	if record.MembershipID == nil || *record.MembershipID != membership.ID {
		t.Fatalf("expected key linked to membership, got %+v", record.MembershipID)
	}
	if record.MemberEmail == nil || *record.MemberEmail != "teammate@example.com" {
		t.Fatalf("expected member email on key record, got %+v", record.MemberEmail)
	}
	validated, err := s.ValidateKey(fullKey)
	if err != nil {
		t.Fatalf("ValidateKey: %v", err)
	}
	if validated.Role != RoleDeveloper {
		t.Fatalf("expected developer role from membership, got %q", validated.Role)
	}

	invitations, err = s.ListWorkspaceInvitations(workspace.ID)
	if err != nil {
		t.Fatalf("ListWorkspaceInvitations after accept: %v", err)
	}
	if len(invitations) != 1 {
		t.Fatalf("expected accepted invitation to remain in history, got %d", len(invitations))
	}
	if invitations[0].Status != "accepted" {
		t.Fatalf("expected accepted invitation status, got %q", invitations[0].Status)
	}

	members, err := s.ListWorkspaceMemberships(workspace.ID)
	if err != nil {
		t.Fatalf("ListWorkspaceMemberships: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("expected 1 membership, got %d", len(members))
	}
}

func TestWorkspaceInvitationPreview(t *testing.T) {
	s := newTestStore(t)

	_, adminRec, err := s.CreateKey("admin", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateKey admin: %v", err)
	}
	workspace, err := s.CreateWorkspace("Preview Team")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	token, _, err := s.CreateWorkspaceInvitation(workspace.ID, "preview@example.com", "Preview User", RoleDeveloper, adminRec.ID, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation: %v", err)
	}

	preview, err := s.GetWorkspaceInvitationPreview(token)
	if err != nil {
		t.Fatalf("GetWorkspaceInvitationPreview: %v", err)
	}
	if preview.WorkspaceID != workspace.ID {
		t.Fatalf("expected workspace %s, got %s", workspace.ID, preview.WorkspaceID)
	}
	if preview.WorkspaceName != workspace.Name {
		t.Fatalf("expected workspace name %q, got %q", workspace.Name, preview.WorkspaceName)
	}
	if preview.Role != RoleDeveloper {
		t.Fatalf("expected role %q, got %q", RoleDeveloper, preview.Role)
	}
}

func TestRevokeWorkspaceInvitation(t *testing.T) {
	s := newTestStore(t)

	_, adminRec, err := s.CreateKey("admin", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateKey admin: %v", err)
	}
	workspace, err := s.CreateWorkspace("Revocation Team")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	token, invitation, err := s.CreateWorkspaceInvitation(workspace.ID, "revoke@example.com", "Revoke Me", RoleReadOnly, adminRec.ID, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation: %v", err)
	}
	if err := s.RevokeWorkspaceInvitation(workspace.ID, invitation.ID); err != nil {
		t.Fatalf("RevokeWorkspaceInvitation: %v", err)
	}
	invitations, err := s.ListWorkspaceInvitations(workspace.ID)
	if err != nil {
		t.Fatalf("ListWorkspaceInvitations after revoke: %v", err)
	}
	if len(invitations) != 1 {
		t.Fatalf("expected revoked invitation to remain in history, got %d", len(invitations))
	}
	if invitations[0].Status != "revoked" {
		t.Fatalf("expected revoked invitation status, got %q", invitations[0].Status)
	}
	if _, _, _, err := s.AcceptWorkspaceInvitation(token, "Should Fail"); err == nil {
		t.Fatal("expected revoked invitation acceptance to fail")
	}
}

func TestListWorkspaceInvitations_ExpiredLifecycle(t *testing.T) {
	s := newTestStore(t)

	_, adminRec, err := s.CreateKey("admin", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateKey admin: %v", err)
	}
	workspace, err := s.CreateWorkspace("Expired Team")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	if _, _, err := s.CreateWorkspaceInvitation(workspace.ID, "expired@example.com", "Expired User", RoleDeveloper, adminRec.ID, time.Now().Add(-2*time.Hour)); err != nil {
		t.Fatalf("CreateWorkspaceInvitation: %v", err)
	}

	invitations, err := s.ListWorkspaceInvitations(workspace.ID)
	if err != nil {
		t.Fatalf("ListWorkspaceInvitations: %v", err)
	}
	if len(invitations) != 1 {
		t.Fatalf("expected 1 invitation, got %d", len(invitations))
	}
	if invitations[0].Status != "expired" {
		t.Fatalf("expected expired invitation status, got %q", invitations[0].Status)
	}
}

func TestValidateSession_WithMembership(t *testing.T) {
	s := newTestStore(t)

	_, adminRec, err := s.CreateKey("admin", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateKey admin: %v", err)
	}
	workspace, err := s.CreateWorkspace("Session Team")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	token, _, err := s.CreateWorkspaceInvitation(workspace.ID, "member@example.com", "Member", RoleOperator, adminRec.ID, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation: %v", err)
	}
	membership, fullKey, _, err := s.AcceptWorkspaceInvitation(token, "Member")
	if err != nil {
		t.Fatalf("AcceptWorkspaceInvitation: %v", err)
	}

	validatedKey, err := s.ValidateKey(fullKey)
	if err != nil {
		t.Fatalf("ValidateKey: %v", err)
	}
	sessionToken, _, err := s.CreateSession(validatedKey.ID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	_, sessionKey, err := s.ValidateSession(sessionToken)
	if err != nil {
		t.Fatalf("ValidateSession: %v", err)
	}
	if sessionKey.MembershipID == nil || *sessionKey.MembershipID != membership.ID {
		t.Fatalf("expected membership on session key, got %+v", sessionKey.MembershipID)
	}
	if sessionKey.MemberName == nil || *sessionKey.MemberName != "Member" {
		t.Fatalf("expected member display name, got %+v", sessionKey.MemberName)
	}
}

func TestWorkspaceMembershipRoleUpdateAndRemoval(t *testing.T) {
	s := newTestStore(t)

	_, adminRec, err := s.CreateKey("admin", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateKey admin: %v", err)
	}
	workspace, err := s.CreateWorkspace("Membership Ops")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	token, _, err := s.CreateWorkspaceInvitation(workspace.ID, "member@example.com", "Member", RoleDeveloper, adminRec.ID, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation: %v", err)
	}
	membership, fullKey, _, err := s.AcceptWorkspaceInvitation(token, "Member")
	if err != nil {
		t.Fatalf("AcceptWorkspaceInvitation: %v", err)
	}

	updated, err := s.UpdateWorkspaceMembershipRole(workspace.ID, membership.ID, RoleOperator)
	if err != nil {
		t.Fatalf("UpdateWorkspaceMembershipRole: %v", err)
	}
	if updated.Role != RoleOperator {
		t.Fatalf("expected operator role, got %q", updated.Role)
	}

	validated, err := s.ValidateKey(fullKey)
	if err != nil {
		t.Fatalf("ValidateKey: %v", err)
	}
	if validated.Role != RoleOperator {
		t.Fatalf("expected operator role from membership, got %q", validated.Role)
	}

	if err := s.RemoveWorkspaceMembership(workspace.ID, membership.ID); err != nil {
		t.Fatalf("RemoveWorkspaceMembership: %v", err)
	}
	if _, err := s.ValidateKey(fullKey); err == nil {
		t.Fatal("expected membership-linked key to be revoked")
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
	if key.WorkspaceID != rec.WorkspaceID {
		t.Fatalf("expected session key workspace %q, got %q", rec.WorkspaceID, key.WorkspaceID)
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
