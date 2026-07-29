package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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

func TestDeactivateWorkspaceSerializesRacingMutators(t *testing.T) {
	store := newTestStore(t)

	_, platformAdmin, err := store.CreateKey("platform-admin", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateKey platform admin: %v", err)
	}
	target, err := store.CreateWorkspace("Concurrent Cleanup Target")
	if err != nil {
		t.Fatalf("CreateWorkspace target: %v", err)
	}
	_, targetKey, err := store.CreateKeyInWorkspace(target.ID, "target-key", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateKeyInWorkspace target: %v", err)
	}
	invitationToken, invitation, err := store.CreateWorkspaceInvitation(
		target.ID, "racing@example.com", "Racing Member", RoleDeveloper, platformAdmin.ID, time.Now().Add(24*time.Hour),
	)
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation racing member: %v", err)
	}

	nonzeroRequests := int64(999)
	nonzeroTokens := int64(9999)
	mutations := []struct {
		name string
		run  func() error
	}{
		{
			name: "accept invitation",
			run: func() error {
				_, _, _, err := store.AcceptWorkspaceInvitation(invitationToken, "Racing Member")
				return err
			},
		},
		{
			name: "create key",
			run: func() error {
				_, _, err := store.CreateKeyInWorkspace(target.ID, "late-key", RoleDeveloper)
				return err
			},
		},
		{
			name: "create invitation",
			run: func() error {
				_, _, err := store.CreateWorkspaceInvitation(
					target.ID, "late@example.com", "Late Member", RoleDeveloper, platformAdmin.ID, time.Now().Add(24*time.Hour),
				)
				return err
			},
		},
		{
			name: "write quota",
			run: func() error {
				_, err := store.UpsertWorkspaceQuota(target.ID, &nonzeroRequests, &nonzeroTokens, false)
				return err
			},
		},
		{
			name: "write provider config",
			run: func() error {
				_, err := store.UpsertWorkspaceProviderConfig(
					target.ID, "runpod", "late-api-key", "late-api-secret", "https://example.invalid", nil,
				)
				return err
			},
		},
		{
			name: "create session",
			run: func() error {
				_, _, err := store.CreateSession(targetKey.ID)
				return err
			},
		},
	}

	locked := make(chan struct{}, len(mutations))
	releaseMutations := make(chan struct{})
	store.workspaceMutationLockedHook = func() {
		locked <- struct{}{}
		<-releaseMutations
	}

	type mutationResult struct {
		name string
		err  error
	}
	results := make(chan mutationResult, len(mutations))

	// Hold every mutator after it has acquired the shared lifecycle boundary.
	// DeactivateWorkspace must wait for all of them to finish, then its cleanup
	// must remove every operational record they created.
	for _, mutation := range mutations {
		mutation := mutation
		go func() {
			results <- mutationResult{name: mutation.name, err: mutation.run()}
		}()
	}
	for range mutations {
		select {
		case <-locked:
		case result := <-results:
			t.Fatalf("%s returned before the lifecycle boundary was released: %v", result.name, result.err)
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for racing mutators to acquire the workspace lifecycle boundary")
		}
	}

	type deactivationResult struct {
		workspace *WorkspaceRecord
		err       error
	}
	deactivationStarted := make(chan struct{})
	deactivationDone := make(chan deactivationResult, 1)
	go func() {
		close(deactivationStarted)
		workspace, err := store.DeactivateWorkspace(target.ID)
		deactivationDone <- deactivationResult{workspace: workspace, err: err}
	}()
	<-deactivationStarted
	close(releaseMutations)

	for range mutations {
		result := <-results
		if result.err != nil {
			t.Fatalf("%s failed before deactivation: %v", result.name, result.err)
		}
	}
	store.workspaceMutationLockedHook = nil

	deactivation := <-deactivationDone
	if deactivation.err != nil {
		t.Fatalf("DeactivateWorkspace: %v", deactivation.err)
	}
	deactivated := deactivation.workspace
	if deactivated.Status != WorkspaceStatusDeactivated {
		t.Fatalf("expected deactivated status, got %+v", deactivated)
	}

	// The same writes queued after the lifecycle transition must all fail and
	// cannot recreate any operational state after DeactivateWorkspace returns.
	lateResults := make(chan mutationResult, len(mutations))
	for _, mutation := range mutations {
		mutation := mutation
		go func() {
			lateResults <- mutationResult{name: mutation.name, err: mutation.run()}
		}()
	}
	for range mutations {
		result := <-lateResults
		if result.err == nil {
			t.Fatalf("%s unexpectedly succeeded after deactivation", result.name)
		}
	}

	var activeKeys, activeMemberships, pendingInvitations, acceptedInvitations, sessions, providerConfigs int
	if err := store.db.QueryRow(`
		SELECT COUNT(*) FROM api_keys
		WHERE workspace_id = ? AND status = 'active'`, target.ID).Scan(&activeKeys); err != nil {
		t.Fatalf("count active keys: %v", err)
	}
	if err := store.db.QueryRow(`
		SELECT COUNT(*) FROM workspace_memberships
		WHERE workspace_id = ? AND status = 'active'`, target.ID).Scan(&activeMemberships); err != nil {
		t.Fatalf("count active memberships: %v", err)
	}
	if err := store.db.QueryRow(`
		SELECT COUNT(*) FROM workspace_invitations
		WHERE workspace_id = ? AND status = 'pending'`, target.ID).Scan(&pendingInvitations); err != nil {
		t.Fatalf("count pending invitations: %v", err)
	}
	if err := store.db.QueryRow(`
		SELECT COUNT(*) FROM workspace_invitations
		WHERE workspace_id = ? AND status = 'accepted'`, target.ID).Scan(&acceptedInvitations); err != nil {
		t.Fatalf("count accepted invitations: %v", err)
	}
	if err := store.db.QueryRow(`
		SELECT COUNT(*)
		FROM sessions s
		JOIN api_keys k ON k.id = s.key_id
		WHERE k.workspace_id = ?`, target.ID).Scan(&sessions); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if err := store.db.QueryRow(`
		SELECT COUNT(*) FROM workspace_provider_configs
		WHERE workspace_id = ?`, target.ID).Scan(&providerConfigs); err != nil {
		t.Fatalf("count provider configs: %v", err)
	}
	if activeKeys != 0 || activeMemberships != 0 || pendingInvitations != 0 || sessions != 0 || providerConfigs != 0 {
		t.Fatalf(
			"deactivation invariant violated: keys=%d memberships=%d invitations=%d sessions=%d providers=%d",
			activeKeys, activeMemberships, pendingInvitations, sessions, providerConfigs,
		)
	}
	if acceptedInvitations != 1 {
		t.Fatalf("expected one preserved pre-deactivation accepted invitation, got %d", acceptedInvitations)
	}

	var requestLimit, tokenLimit int64
	var enforceHardLimits int
	if err := store.db.QueryRow(`
		SELECT monthly_request_limit, monthly_token_limit, enforce_hard_limits
		FROM workspace_quotas
		WHERE workspace_id = ?`, target.ID).Scan(&requestLimit, &tokenLimit, &enforceHardLimits); err != nil {
		t.Fatalf("load quota invariant: %v", err)
	}
	if requestLimit != 0 || tokenLimit != 0 || enforceHardLimits != 1 {
		t.Fatalf("expected zero hard quota, got requests=%d tokens=%d hard=%d", requestLimit, tokenLimit, enforceHardLimits)
	}

	var invitationStatus, lateInvitationStatus, keyStatus string
	if err := store.db.QueryRow(`SELECT status FROM workspace_invitations WHERE id = ?`, invitation.ID).Scan(&invitationStatus); err != nil {
		t.Fatalf("load retained invitation: %v", err)
	}
	if err := store.db.QueryRow(`
		SELECT status FROM workspace_invitations
		WHERE workspace_id = ? AND email = ?`, target.ID, "late@example.com").Scan(&lateInvitationStatus); err != nil {
		t.Fatalf("load racing pending invitation: %v", err)
	}
	if err := store.db.QueryRow(`SELECT status FROM api_keys WHERE id = ?`, targetKey.ID).Scan(&keyStatus); err != nil {
		t.Fatalf("load retained key: %v", err)
	}
	if invitationStatus != "accepted" || lateInvitationStatus != "revoked" || keyStatus != "revoked" {
		t.Fatalf(
			"unexpected retained history: accepted_invitation=%q pending_invitation=%q key=%q",
			invitationStatus, lateInvitationStatus, keyStatus,
		)
	}

	if _, err := store.DeactivateWorkspace(target.ID); err != nil {
		t.Fatalf("idempotent DeactivateWorkspace retry: %v", err)
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
