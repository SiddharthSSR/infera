package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestDeactivateWorkspaceLifecycleIsCompleteAndIdempotent(t *testing.T) {
	store := newTestStore(t)

	_, platformAdmin, err := store.CreateKey("platform-admin", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateKey platform admin: %v", err)
	}
	target, err := store.CreateWorkspace("Synthetic Cleanup Target")
	if err != nil {
		t.Fatalf("CreateWorkspace target: %v", err)
	}
	other, err := store.CreateWorkspace("Still Active")
	if err != nil {
		t.Fatalf("CreateWorkspace other: %v", err)
	}

	targetInviteToken, targetAcceptedInvite, err := store.CreateWorkspaceInvitation(
		target.ID, "member@example.com", "Member", RoleAdmin, platformAdmin.ID, time.Now().Add(24*time.Hour),
	)
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation target member: %v", err)
	}
	targetMembership, targetRawKey, targetKey, err := store.AcceptWorkspaceInvitation(targetInviteToken, "Member")
	if err != nil {
		t.Fatalf("AcceptWorkspaceInvitation target: %v", err)
	}

	otherInviteToken, _, err := store.CreateWorkspaceInvitation(
		other.ID, "member@example.com", "Member", RoleOperator, platformAdmin.ID, time.Now().Add(24*time.Hour),
	)
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation other member: %v", err)
	}
	_, otherRawKey, otherKey, err := store.AcceptWorkspaceInvitationForIdentity(otherInviteToken, "Member", targetKey)
	if err != nil {
		t.Fatalf("AcceptWorkspaceInvitationForIdentity other: %v", err)
	}

	targetSessionToken, _, err := store.CreateSession(targetKey.ID)
	if err != nil {
		t.Fatalf("CreateSession target: %v", err)
	}
	otherSessionToken, _, err := store.CreateSession(otherKey.ID)
	if err != nil {
		t.Fatalf("CreateSession other: %v", err)
	}
	_, pendingInvite, err := store.CreateWorkspaceInvitation(
		target.ID, "pending@example.com", "Pending", RoleDeveloper, platformAdmin.ID, time.Now().Add(24*time.Hour),
	)
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation pending: %v", err)
	}
	requestLimit := int64(500)
	tokenLimit := int64(10000)
	if _, err := store.UpsertWorkspaceQuota(target.ID, &requestLimit, &tokenLimit, false); err != nil {
		t.Fatalf("UpsertWorkspaceQuota: %v", err)
	}
	if _, err := store.UpsertWorkspaceProviderConfig(
		target.ID, "runpod", "secret-api-key", "secret-api-secret", "https://example.invalid", nil,
	); err != nil {
		t.Fatalf("UpsertWorkspaceProviderConfig: %v", err)
	}

	deactivated, err := store.DeactivateWorkspace(target.ID)
	if err != nil {
		t.Fatalf("DeactivateWorkspace: %v", err)
	}
	if deactivated.ID != target.ID || deactivated.Status != WorkspaceStatusDeactivated {
		t.Fatalf("unexpected deactivation result: %+v", deactivated)
	}

	if _, err := store.ValidateKey(targetRawKey); err == nil {
		t.Fatal("target workspace API key must be revoked")
	}
	if _, _, err := store.ValidateSession(targetSessionToken); err == nil {
		t.Fatal("target workspace session must be invalidated")
	}
	if _, err := store.ValidateKey(otherRawKey); err != nil {
		t.Fatalf("other workspace key must remain valid: %v", err)
	}
	if _, _, err := store.ValidateSession(otherSessionToken); err != nil {
		t.Fatalf("other workspace session must remain valid: %v", err)
	}
	if _, _, err := store.SwitchSessionWorkspace(otherSessionToken, target.ID); err == nil {
		t.Fatal("switching into a deactivated workspace must fail")
	}
	if _, _, err := store.CreateKeyInWorkspace(target.ID, "late-key", RoleUser); err == nil {
		t.Fatal("creating a key in a deactivated workspace must fail")
	}
	if _, _, _, _, err := store.ResolveWorkspaceProviderConfig(target.ID, "runpod"); err == nil {
		t.Fatal("provider config resolution for a deactivated workspace must fail")
	}

	activeWorkspaces, err := store.ListWorkspaces()
	if err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	for _, workspace := range activeWorkspaces {
		if workspace.ID == target.ID {
			t.Fatal("deactivated workspace must not appear in active workspace listings")
		}
	}
	accessible, err := store.ListAccessibleWorkspaces(otherKey)
	if err != nil {
		t.Fatalf("ListAccessibleWorkspaces: %v", err)
	}
	if len(accessible) != 1 || accessible[0].ID != other.ID {
		t.Fatalf("expected only the remaining active workspace, got %+v", accessible)
	}

	var workspaceStatus string
	if err := store.db.QueryRow(`SELECT status FROM workspaces WHERE id = ?`, target.ID).Scan(&workspaceStatus); err != nil {
		t.Fatalf("load retained workspace: %v", err)
	}
	if workspaceStatus != WorkspaceStatusDeactivated {
		t.Fatalf("expected retained workspace status %q, got %q", WorkspaceStatusDeactivated, workspaceStatus)
	}
	var keyStatus string
	if err := store.db.QueryRow(`SELECT status FROM api_keys WHERE id = ?`, targetKey.ID).Scan(&keyStatus); err != nil {
		t.Fatalf("load retained key history: %v", err)
	}
	if keyStatus != "revoked" {
		t.Fatalf("expected retained key status revoked, got %q", keyStatus)
	}
	var membershipStatus string
	if err := store.db.QueryRow(`SELECT status FROM workspace_memberships WHERE id = ?`, targetMembership.ID).Scan(&membershipStatus); err != nil {
		t.Fatalf("load retained membership history: %v", err)
	}
	if membershipStatus != "removed" {
		t.Fatalf("expected retained membership status removed, got %q", membershipStatus)
	}
	var acceptedInviteStatus string
	if err := store.db.QueryRow(`SELECT status FROM workspace_invitations WHERE id = ?`, targetAcceptedInvite.ID).Scan(&acceptedInviteStatus); err != nil {
		t.Fatalf("load retained accepted invitation: %v", err)
	}
	if acceptedInviteStatus != "accepted" {
		t.Fatalf("accepted invitation history changed to %q", acceptedInviteStatus)
	}
	var pendingInviteStatus string
	if err := store.db.QueryRow(`SELECT status FROM workspace_invitations WHERE id = ?`, pendingInvite.ID).Scan(&pendingInviteStatus); err != nil {
		t.Fatalf("load retained pending invitation: %v", err)
	}
	if pendingInviteStatus != "revoked" {
		t.Fatalf("expected pending invitation to be revoked, got %q", pendingInviteStatus)
	}
	var sessionCount int
	if err := store.db.QueryRow(`
		SELECT COUNT(*)
		FROM sessions s
		JOIN api_keys k ON k.id = s.key_id
		WHERE k.workspace_id = ?`, target.ID).Scan(&sessionCount); err != nil {
		t.Fatalf("count target sessions: %v", err)
	}
	if sessionCount != 0 {
		t.Fatalf("expected zero target sessions, got %d", sessionCount)
	}
	var quotaRequests, quotaTokens int64
	var enforceHardLimits int
	if err := store.db.QueryRow(`
		SELECT monthly_request_limit, monthly_token_limit, enforce_hard_limits
		FROM workspace_quotas
		WHERE workspace_id = ?`, target.ID).Scan(&quotaRequests, &quotaTokens, &enforceHardLimits); err != nil {
		t.Fatalf("load deactivated quota: %v", err)
	}
	if quotaRequests != 0 || quotaTokens != 0 || enforceHardLimits != 1 {
		t.Fatalf("expected zero hard quota, got requests=%d tokens=%d hard=%d", quotaRequests, quotaTokens, enforceHardLimits)
	}
	var providerConfigCount int
	if err := store.db.QueryRow(`
		SELECT COUNT(*) FROM workspace_provider_configs WHERE workspace_id = ?`, target.ID).Scan(&providerConfigCount); err != nil {
		t.Fatalf("count provider configs: %v", err)
	}
	if providerConfigCount != 0 {
		t.Fatalf("expected provider configs to be removed, got %d", providerConfigCount)
	}

	retried, err := store.DeactivateWorkspace(target.ID)
	if err != nil {
		t.Fatalf("DeactivateWorkspace retry: %v", err)
	}
	if retried.Status != WorkspaceStatusDeactivated {
		t.Fatalf("expected idempotent retry to remain deactivated, got %+v", retried)
	}
}

func TestDeactivateWorkspaceRejectsDefault(t *testing.T) {
	store := newTestStore(t)

	if _, err := store.DeactivateWorkspace(DefaultWorkspaceID); !errors.Is(err, ErrDefaultWorkspaceDeactivation) {
		t.Fatalf("expected ErrDefaultWorkspaceDeactivation, got %v", err)
	}
	workspace, err := store.getWorkspace(DefaultWorkspaceID)
	if err != nil {
		t.Fatalf("default workspace must remain active: %v", err)
	}
	if workspace.Status != WorkspaceStatusActive {
		t.Fatalf("expected default workspace active, got %q", workspace.Status)
	}
}

func TestDeactivateWorkspaceSerializesCommittedMutationsAcrossStores(t *testing.T) {
	storeA, storeB := newSharedDatabaseStores(t)

	_, platformAdmin, err := storeA.CreateKey("platform-admin", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateKey platform admin: %v", err)
	}
	target, err := storeA.CreateWorkspace("Committed Cleanup Target")
	if err != nil {
		t.Fatalf("CreateWorkspace target: %v", err)
	}
	_, targetKey, err := storeB.CreateKeyInWorkspace(target.ID, "target-key", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateKeyInWorkspace target: %v", err)
	}
	acceptedToken, acceptedInvitation, err := storeB.CreateWorkspaceInvitation(
		target.ID, "accepted@example.com", "Accepted Member", RoleDeveloper, platformAdmin.ID, time.Now().Add(24*time.Hour),
	)
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation accepted member: %v", err)
	}
	if _, _, _, err := storeB.AcceptWorkspaceInvitation(acceptedToken, "Accepted Member"); err != nil {
		t.Fatalf("AcceptWorkspaceInvitation: %v", err)
	}
	_, pendingInvitation, err := storeB.CreateWorkspaceInvitation(
		target.ID, "pending@example.com", "Pending Member", RoleDeveloper, platformAdmin.ID, time.Now().Add(24*time.Hour),
	)
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation pending member: %v", err)
	}
	if _, _, err := storeB.CreateSession(targetKey.ID); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	requestLimit, tokenLimit := int64(999), int64(9999)
	if _, err := storeB.UpsertWorkspaceQuota(target.ID, &requestLimit, &tokenLimit, false); err != nil {
		t.Fatalf("UpsertWorkspaceQuota: %v", err)
	}
	if _, err := storeB.UpsertWorkspaceProviderConfig(
		target.ID, "runpod", "api-key", "api-secret", "https://example.invalid", nil,
	); err != nil {
		t.Fatalf("UpsertWorkspaceProviderConfig: %v", err)
	}

	if _, err := storeA.DeactivateWorkspace(target.ID); err != nil {
		t.Fatalf("DeactivateWorkspace: %v", err)
	}
	assertWorkspaceDeactivationInvariant(t, storeB, target.ID, 1)

	var acceptedStatus, pendingStatus, keyStatus string
	if err := storeB.db.QueryRow(`SELECT status FROM workspace_invitations WHERE id = ?`, acceptedInvitation.ID).Scan(&acceptedStatus); err != nil {
		t.Fatalf("load accepted invitation: %v", err)
	}
	if err := storeB.db.QueryRow(`SELECT status FROM workspace_invitations WHERE id = ?`, pendingInvitation.ID).Scan(&pendingStatus); err != nil {
		t.Fatalf("load pending invitation: %v", err)
	}
	if err := storeB.db.QueryRow(`SELECT status FROM api_keys WHERE id = ?`, targetKey.ID).Scan(&keyStatus); err != nil {
		t.Fatalf("load key: %v", err)
	}
	if acceptedStatus != "accepted" || pendingStatus != "revoked" || keyStatus != "revoked" {
		t.Fatalf("unexpected retained history: accepted=%q pending=%q key=%q", acceptedStatus, pendingStatus, keyStatus)
	}
}

func TestDeactivateWorkspaceRejectsRacingMutationsAcrossStores(t *testing.T) {
	storeA, storeB := newSharedDatabaseStores(t)

	_, platformAdmin, err := storeA.CreateKey("platform-admin", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateKey platform admin: %v", err)
	}
	target, err := storeA.CreateWorkspace("Racing Cleanup Target")
	if err != nil {
		t.Fatalf("CreateWorkspace target: %v", err)
	}
	_, targetKey, err := storeA.CreateKeyInWorkspace(target.ID, "target-key", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateKeyInWorkspace target: %v", err)
	}
	invitationToken, invitation, err := storeA.CreateWorkspaceInvitation(
		target.ID, "racing@example.com", "Racing Member", RoleDeveloper, platformAdmin.ID, time.Now().Add(24*time.Hour),
	)
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation racing member: %v", err)
	}
	if _, _, err := storeA.CreateSession(targetKey.ID); err != nil {
		t.Fatalf("CreateSession before deactivation: %v", err)
	}

	tx, err := storeA.db.Begin()
	if err != nil {
		t.Fatalf("begin deactivation transaction: %v", err)
	}
	if _, err := storeA.deactivateWorkspaceTx(tx, target.ID); err != nil {
		_ = tx.Rollback()
		t.Fatalf("deactivate workspace transaction: %v", err)
	}

	nonzeroRequests, nonzeroTokens := int64(999), int64(9999)
	mutations := []struct {
		name string
		run  func() error
	}{
		{"accept invitation", func() error {
			_, _, _, err := storeB.AcceptWorkspaceInvitation(invitationToken, "Racing Member")
			return err
		}},
		{"create key", func() error {
			_, _, err := storeB.CreateKeyInWorkspace(target.ID, "late-key", RoleDeveloper)
			return err
		}},
		{"create invitation", func() error {
			_, _, err := storeB.CreateWorkspaceInvitation(
				target.ID, "late@example.com", "Late Member", RoleDeveloper, platformAdmin.ID, time.Now().Add(24*time.Hour),
			)
			return err
		}},
		{"write quota", func() error {
			_, err := storeB.UpsertWorkspaceQuota(target.ID, &nonzeroRequests, &nonzeroTokens, false)
			return err
		}},
		{"write provider config", func() error {
			_, err := storeB.UpsertWorkspaceProviderConfig(
				target.ID, "runpod", "late-api-key", "late-api-secret", "https://example.invalid", nil,
			)
			return err
		}},
		{"create session", func() error {
			_, _, err := storeB.CreateSession(targetKey.ID)
			return err
		}},
	}

	type mutationResult struct {
		name string
		err  error
	}
	started := make(chan struct{}, len(mutations))
	results := make(chan mutationResult, len(mutations))
	// Store A already holds SQLite's database-wide writer boundary with the
	// lifecycle transition applied. Store B either waits on that boundary or
	// reaches its guarded statement after commit; both deterministic orderings
	// must observe the terminal status and fail closed without timing sleeps.
	for _, mutation := range mutations {
		mutation := mutation
		go func() {
			started <- struct{}{}
			results <- mutationResult{name: mutation.name, err: mutation.run()}
		}()
	}
	for range mutations {
		<-started
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit deactivation transaction: %v", err)
	}

	for range mutations {
		result := <-results
		if result.err == nil {
			t.Fatalf("%s unexpectedly succeeded after the lifecycle transition", result.name)
		}
	}
	assertWorkspaceDeactivationInvariant(t, storeB, target.ID, 0)

	var invitationStatus, keyStatus string
	if err := storeB.db.QueryRow(`SELECT status FROM workspace_invitations WHERE id = ?`, invitation.ID).Scan(&invitationStatus); err != nil {
		t.Fatalf("load retained invitation: %v", err)
	}
	if err := storeB.db.QueryRow(`SELECT status FROM api_keys WHERE id = ?`, targetKey.ID).Scan(&keyStatus); err != nil {
		t.Fatalf("load retained key: %v", err)
	}
	if invitationStatus != "revoked" || keyStatus != "revoked" {
		t.Fatalf("unexpected retained history: invitation=%q key=%q", invitationStatus, keyStatus)
	}
}

func newSharedDatabaseStores(t *testing.T) (*Store, *Store) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "auth_test.db")
	storeA, err := NewStoreWithProviderCredentialEncryption(dbPath, testProviderCredentialEncryptionKey)
	if err != nil {
		t.Fatalf("NewStore A: %v", err)
	}
	t.Cleanup(func() { storeA.Close() })
	storeB, err := NewStoreWithProviderCredentialEncryption(dbPath, testProviderCredentialEncryptionKey)
	if err != nil {
		t.Fatalf("NewStore B: %v", err)
	}
	t.Cleanup(func() { storeB.Close() })
	return storeA, storeB
}

func assertWorkspaceDeactivationInvariant(t *testing.T, store *Store, workspaceID string, acceptedInvitations int) {
	t.Helper()
	var status string
	if err := store.db.QueryRow(`SELECT status FROM workspaces WHERE id = ?`, workspaceID).Scan(&status); err != nil {
		t.Fatalf("load retained workspace: %v", err)
	}
	if status != WorkspaceStatusDeactivated {
		t.Fatalf("expected retained deactivated workspace, got %q", status)
	}

	var activeKeys, activeMemberships, pendingInvitations, accepted, sessions, providerConfigs int
	queries := []struct {
		name string
		sql  string
		dest *int
	}{
		{"active keys", `SELECT COUNT(*) FROM api_keys WHERE workspace_id = ? AND status = 'active'`, &activeKeys},
		{"active memberships", `SELECT COUNT(*) FROM workspace_memberships WHERE workspace_id = ? AND status = 'active'`, &activeMemberships},
		{"pending invitations", `SELECT COUNT(*) FROM workspace_invitations WHERE workspace_id = ? AND status = 'pending'`, &pendingInvitations},
		{"accepted invitations", `SELECT COUNT(*) FROM workspace_invitations WHERE workspace_id = ? AND status = 'accepted'`, &accepted},
		{"sessions", `SELECT COUNT(*) FROM sessions s JOIN api_keys k ON k.id = s.key_id WHERE k.workspace_id = ?`, &sessions},
		{"provider configs", `SELECT COUNT(*) FROM workspace_provider_configs WHERE workspace_id = ?`, &providerConfigs},
	}
	for _, query := range queries {
		if err := store.db.QueryRow(query.sql, workspaceID).Scan(query.dest); err != nil {
			t.Fatalf("count %s: %v", query.name, err)
		}
	}
	if activeKeys != 0 || activeMemberships != 0 || pendingInvitations != 0 || sessions != 0 || providerConfigs != 0 || accepted != acceptedInvitations {
		t.Fatalf(
			"deactivation invariant violated: keys=%d memberships=%d pending=%d accepted=%d sessions=%d providers=%d",
			activeKeys, activeMemberships, pendingInvitations, accepted, sessions, providerConfigs,
		)
	}

	var requestLimit, tokenLimit int64
	var enforceHardLimits int
	if err := store.db.QueryRow(`
		SELECT monthly_request_limit, monthly_token_limit, enforce_hard_limits
		FROM workspace_quotas
		WHERE workspace_id = ?`, workspaceID).Scan(&requestLimit, &tokenLimit, &enforceHardLimits); err != nil {
		t.Fatalf("load quota invariant: %v", err)
	}
	if requestLimit != 0 || tokenLimit != 0 || enforceHardLimits != 1 {
		t.Fatalf("expected zero hard quota, got requests=%d tokens=%d hard=%d", requestLimit, tokenLimit, enforceHardLimits)
	}
}

func TestHandleDeactivateWorkspaceAuthorizationAndRetry(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	platformAdminKey, _, err := store.CreateKey("platform-admin", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateKey platform admin: %v", err)
	}
	platformOperatorKey, _, err := store.CreateKey("platform-operator", RoleOperator)
	if err != nil {
		t.Fatalf("CreateKey platform operator: %v", err)
	}
	target, err := store.CreateWorkspace("Handler Target")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	workspaceAdminKey, _, err := store.CreateKeyInWorkspace(target.ID, "workspace-admin", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateKeyInWorkspace: %v", err)
	}

	assertDeleteStatus := func(name, rawKey, workspaceID string, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodDelete, "/api/auth/workspaces/"+workspaceID, nil)
		req.Header.Set("Authorization", "Bearer "+rawKey)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("%s: expected %d, got %d: %s", name, want, rec.Code, rec.Body.String())
		}
		return rec
	}

	assertDeleteStatus("workspace admin denied cross-tenant operation", workspaceAdminKey, target.ID, http.StatusForbidden)
	assertDeleteStatus("non-admin denied", platformOperatorKey, target.ID, http.StatusForbidden)

	rec := assertDeleteStatus("platform admin deactivates", platformAdminKey, target.ID, http.StatusOK)
	var response struct {
		Workspace WorkspaceRecord `json:"workspace"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode deactivation response: %v", err)
	}
	if response.Workspace.ID != target.ID || response.Workspace.Status != WorkspaceStatusDeactivated {
		t.Fatalf("unexpected deactivation response: %+v", response.Workspace)
	}
	assertDeleteStatus("platform admin retry succeeds", platformAdminKey, target.ID, http.StatusOK)
	assertDeleteStatus("default workspace rejected", platformAdminKey, DefaultWorkspaceID, http.StatusBadRequest)
	assertDeleteStatus("unknown workspace not found", platformAdminKey, "ws_missing", http.StatusNotFound)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/workspaces", nil)
	req.Header.Set("Authorization", "Bearer "+workspaceAdminKey)
	authRec := httptest.NewRecorder()
	mux.ServeHTTP(authRec, req)
	if authRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected deactivated workspace key to fail auth with 401, got %d: %s", authRec.Code, authRec.Body.String())
	}
}
