package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandleWorkspaceMembersMatchesSharedFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	adminKey, adminRecord, workspace := newWorkspaceMembershipFixtureContext(t, store)

	token, _, err := store.CreateWorkspaceInvitation(
		workspace.ID,
		"member@example.com",
		"Fixture Member",
		RoleDeveloper,
		adminRecord.ID,
		time.Now().Add(7*24*time.Hour),
	)
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation: %v", err)
	}
	member, _, _, err := store.AcceptWorkspaceInvitation(token, "Fixture Member")
	if err != nil {
		t.Fatalf("AcceptWorkspaceInvitation: %v", err)
	}
	if _, err := store.UpdateWorkspaceMembershipRole(workspace.ID, member.ID, RoleOperator); err != nil {
		t.Fatalf("UpdateWorkspaceMembershipRole: %v", err)
	}

	rec := httptest.NewRecorder()
	req := workspaceAdminFixtureRequest(
		http.MethodGet,
		"/api/auth/workspaces/"+workspace.ID+"/members",
		nil,
		adminKey,
	)

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertWorkspaceAdminFixtureEqual(t, WorkspaceAdminFixtureWorkspaceMembersListResponse, rec.Body.Bytes())
}

func TestHandlePutWorkspaceMemberMatchesSharedFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	adminKey, adminRecord, workspace := newWorkspaceMembershipFixtureContext(t, store)

	token, _, err := store.CreateWorkspaceInvitation(
		workspace.ID,
		"member@example.com",
		"Fixture Member",
		RoleDeveloper,
		adminRecord.ID,
		time.Now().Add(7*24*time.Hour),
	)
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation: %v", err)
	}
	member, _, _, err := store.AcceptWorkspaceInvitation(token, "Fixture Member")
	if err != nil {
		t.Fatalf("AcceptWorkspaceInvitation: %v", err)
	}

	rec := httptest.NewRecorder()
	req := workspaceAdminFixtureRequest(
		http.MethodPut,
		"/api/auth/workspaces/"+workspace.ID+"/members/"+member.ID,
		loadWorkspaceAdminFixtureBytes(t, WorkspaceAdminFixtureWorkspaceMemberUpdateRequest),
		adminKey,
	)
	req.Header.Set("Content-Type", "application/json")

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertWorkspaceAdminFixtureEqual(t, WorkspaceAdminFixtureWorkspaceMemberResponse, rec.Body.Bytes())
}

func TestHandlePutWorkspaceMemberMissingRoleMatchesSharedErrorFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	adminKey, _, workspace := newWorkspaceMembershipFixtureContext(t, store)

	rec := httptest.NewRecorder()
	req := workspaceAdminFixtureRequest(
		http.MethodPut,
		"/api/auth/workspaces/"+workspace.ID+"/members/mbr_fixture_member",
		[]byte(`{}`),
		adminKey,
	)
	req.Header.Set("Content-Type", "application/json")

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertWorkspaceAdminFixtureEqual(t, WorkspaceAdminFixtureAuthErrorMissingMemberRole, rec.Body.Bytes())
}

func TestHandleWorkspaceInvitesMatchesSharedFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	adminKey, adminRecord, workspace := newWorkspaceMembershipFixtureContext(t, store)

	_, _, err := store.CreateWorkspaceInvitation(
		workspace.ID,
		"invitee@example.com",
		"Invitee Example",
		RoleDeveloper,
		adminRecord.ID,
		time.Now().Add(7*24*time.Hour),
	)
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation: %v", err)
	}

	rec := httptest.NewRecorder()
	req := workspaceAdminFixtureRequest(
		http.MethodGet,
		"/api/auth/workspaces/"+workspace.ID+"/invites",
		nil,
		adminKey,
	)

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertWorkspaceAdminFixtureEqual(t, WorkspaceAdminFixtureWorkspaceInvitationsListResponse, rec.Body.Bytes())
}

func TestHandleCreateWorkspaceInviteMatchesSharedFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	adminKey, _, workspace := newWorkspaceMembershipFixtureContext(t, store)

	rec := httptest.NewRecorder()
	req := workspaceAdminFixtureRequest(
		http.MethodPost,
		"/api/auth/workspaces/"+workspace.ID+"/invites",
		loadWorkspaceAdminFixtureBytes(t, WorkspaceAdminFixtureWorkspaceInvitationCreateRequest),
		adminKey,
	)
	req.Header.Set("Content-Type", "application/json")

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertWorkspaceAdminFixtureEqual(t, WorkspaceAdminFixtureWorkspaceInvitationCreateResponse, rec.Body.Bytes())
}

func TestHandleCreateWorkspaceInviteCannotAssignRoleMatchesSharedErrorFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	adminKey, _, workspace := newWorkspaceMembershipFixtureContext(t, store)

	rec := httptest.NewRecorder()
	req := workspaceAdminFixtureRequest(
		http.MethodPost,
		"/api/auth/workspaces/"+workspace.ID+"/invites",
		[]byte(`{"email":"owner@example.com","role":"admin"}`),
		adminKey,
	)
	req.Header.Set("Content-Type", "application/json")

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertWorkspaceAdminFixtureEqual(t, WorkspaceAdminFixtureAuthErrorCannotAssignRole, rec.Body.Bytes())
}

func TestHandlePreviewInvitationMatchesSharedFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	_, adminRecord, workspace := newWorkspaceMembershipFixtureContext(t, store)

	token, _, err := store.CreateWorkspaceInvitation(
		workspace.ID,
		"invitee@example.com",
		"Invitee Example",
		RoleDeveloper,
		adminRecord.ID,
		time.Now().Add(7*24*time.Hour),
	)
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/invitations/preview?token="+token, nil)

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertWorkspaceAdminFixtureEqual(t, WorkspaceAdminFixtureWorkspaceInvitationPreviewResponse, rec.Body.Bytes())
}

func TestHandlePreviewInvitationMissingTokenMatchesSharedErrorFixture(t *testing.T) {
	_, _, mux := newTestHandlerWithRoutes(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/invitations/preview", nil)

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertWorkspaceAdminFixtureEqual(t, WorkspaceAdminFixtureAuthErrorMissingPreviewToken, rec.Body.Bytes())
}

func TestHandleAcceptInvitationMatchesSharedFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	_, adminRecord, workspace := newWorkspaceMembershipFixtureContext(t, store)

	token, _, err := store.CreateWorkspaceInvitation(
		workspace.ID,
		"invitee@example.com",
		"Invitee Example",
		RoleDeveloper,
		adminRecord.ID,
		time.Now().Add(7*24*time.Hour),
	)
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/auth/invitations/accept",
		strings.NewReader(string(loadWorkspaceAdminInvitationAcceptRequestBody(t, token))),
	)
	req.Header.Set("Content-Type", "application/json")

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertWorkspaceAdminFixtureEqual(t, WorkspaceAdminFixtureWorkspaceInvitationAcceptResponse, rec.Body.Bytes())
}

func TestHandleAcceptInvitationInvalidTokenMatchesSharedErrorFixture(t *testing.T) {
	_, _, mux := newTestHandlerWithRoutes(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/auth/invitations/accept",
		strings.NewReader(`{"invitation_token":"invite_invalid","display_name":"Joined Example"}`),
	)
	req.Header.Set("Content-Type", "application/json")

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertWorkspaceAdminFixtureEqual(t, WorkspaceAdminFixtureAuthErrorInvalidInvitation, rec.Body.Bytes())
}

func loadWorkspaceAdminInvitationAcceptRequestBody(t *testing.T, token string) []byte {
	t.Helper()

	payload := decodeWorkspaceAdminJSONMap(
		t,
		loadWorkspaceAdminFixtureBytes(t, WorkspaceAdminFixtureWorkspaceInvitationAcceptRequest),
	)
	payload["invitation_token"] = token
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal invitation accept request: %v", err)
	}
	return body
}
