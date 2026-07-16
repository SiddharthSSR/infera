package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestHandleCreateKeyMatchesSharedFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	adminKey, _, workspace := newAuthAccessFixtureWorkspaceAdmin(t, store, "Fixture Team")

	rec := httptest.NewRecorder()
	req := authAccessFixtureRequest(
		http.MethodPost,
		"/api/auth/keys",
		loadAuthAccessFixtureBytes(t, AuthAccessFixtureApiKeyCreateRequest),
		adminKey,
	)
	req.Header.Set("Content-Type", "application/json")

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertAuthAccessFixtureEqual(t, AuthAccessFixtureApiKeyCreateResponse, rec.Body.Bytes(), workspace.ID)
}

func TestHandleListKeysMatchesSharedFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	adminKey, _, workspace := newAuthAccessFixtureWorkspaceAdmin(t, store, "Fixture Team")

	rec := httptest.NewRecorder()
	createReq := authAccessFixtureRequest(
		http.MethodPost,
		"/api/auth/keys",
		loadAuthAccessFixtureBytes(t, AuthAccessFixtureApiKeyCreateRequest),
		adminKey,
	)
	createReq.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rec, createReq)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req := authAccessFixtureRequest(http.MethodGet, "/api/auth/keys", nil, adminKey)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertAuthAccessFixtureEqual(t, AuthAccessFixtureApiKeysListResponse, rec.Body.Bytes(), workspace.ID)
}

func TestHandleCreateSessionMatchesSharedFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	adminKey, _, workspace := newAuthAccessFixtureWorkspaceAdmin(t, store, "Fixture Team")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/auth/session",
		strings.NewReader(string(loadAuthAccessSessionCreateRequestBody(t, adminKey))),
	)
	req.Header.Set("Content-Type", "application/json")

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertAuthAccessFixtureEqual(t, AuthAccessFixtureSessionResponse, rec.Body.Bytes(), workspace.ID)
}

func TestHandleGetSessionMatchesSharedFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	_, adminRecord, workspace := newAuthAccessFixtureWorkspaceAdmin(t, store, "Fixture Team")

	sessionToken, _, err := store.CreateSession(adminRecord.ID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionToken})

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertAuthAccessFixtureEqual(t, AuthAccessFixtureSessionResponse, rec.Body.Bytes(), workspace.ID)
}

func TestHandleGetSessionWithMembershipMatchesSharedFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	_, adminRecord, workspace := newAuthAccessFixtureWorkspaceAdmin(t, store, "Fixture Team")

	token, _, err := store.CreateWorkspaceInvitation(
		workspace.ID,
		"member@example.com",
		"Joined Member",
		RoleOperator,
		adminRecord.ID,
		time.Now().Add(7*24*time.Hour),
	)
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation: %v", err)
	}
	_, _, record, err := store.AcceptWorkspaceInvitation(token, "Joined Member")
	if err != nil {
		t.Fatalf("AcceptWorkspaceInvitation: %v", err)
	}
	sessionToken, _, err := store.CreateSession(record.ID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionToken})

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertAuthAccessFixtureEqual(t, AuthAccessFixtureSessionResponseMember, rec.Body.Bytes(), workspace.ID)
}

func TestHandleSwitchSessionWorkspaceMatchesSharedFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	_, adminRecord, workspaceAlpha := newAuthAccessFixtureWorkspaceAdmin(t, store, "Fixture Team")
	workspaceBeta, err := store.CreateWorkspace("Beta Team")
	if err != nil {
		t.Fatalf("CreateWorkspace beta: %v", err)
	}

	tokenAlpha, _, err := store.CreateWorkspaceInvitation(
		workspaceAlpha.ID,
		"member@example.com",
		"Joined Member",
		RoleOperator,
		adminRecord.ID,
		time.Now().Add(7*24*time.Hour),
	)
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation alpha: %v", err)
	}
	_, _, alphaRecord, err := store.AcceptWorkspaceInvitation(tokenAlpha, "Joined Member")
	if err != nil {
		t.Fatalf("AcceptWorkspaceInvitation alpha: %v", err)
	}

	tokenBeta, _, err := store.CreateWorkspaceInvitation(
		workspaceBeta.ID,
		"member@example.com",
		"Joined Member",
		RoleDeveloper,
		adminRecord.ID,
		time.Now().Add(7*24*time.Hour),
	)
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation beta: %v", err)
	}
	if _, _, _, err := store.AcceptWorkspaceInvitationForIdentity(tokenBeta, "Joined Member", alphaRecord); err != nil {
		t.Fatalf("AcceptWorkspaceInvitation beta: %v", err)
	}

	sessionToken, _, err := store.CreateSession(alphaRecord.ID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPut,
		"/api/auth/session/workspace",
		strings.NewReader(string(loadAuthAccessSwitchWorkspaceRequestBody(t, workspaceBeta.ID))),
	)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionToken})

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertAuthAccessFixtureEqual(t, AuthAccessFixtureSessionResponseSwitchedWorkspace, rec.Body.Bytes(), workspaceBeta.ID)
}

func TestHandleListAccessibleWorkspacesMatchesSharedFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	_, adminRecord, workspaceAlpha := newAuthAccessFixtureWorkspaceAdmin(t, store, "Fixture Team")
	workspaceBeta, err := store.CreateWorkspace("Beta Team")
	if err != nil {
		t.Fatalf("CreateWorkspace beta: %v", err)
	}

	tokenAlpha, _, err := store.CreateWorkspaceInvitation(
		workspaceAlpha.ID,
		"member@example.com",
		"Joined Member",
		RoleOperator,
		adminRecord.ID,
		time.Now().Add(7*24*time.Hour),
	)
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation alpha: %v", err)
	}
	_, fullKey, alphaRecord, err := store.AcceptWorkspaceInvitation(tokenAlpha, "Joined Member")
	if err != nil {
		t.Fatalf("AcceptWorkspaceInvitation alpha: %v", err)
	}

	tokenBeta, _, err := store.CreateWorkspaceInvitation(
		workspaceBeta.ID,
		"member@example.com",
		"Joined Member",
		RoleDeveloper,
		adminRecord.ID,
		time.Now().Add(7*24*time.Hour),
	)
	if err != nil {
		t.Fatalf("CreateWorkspaceInvitation beta: %v", err)
	}
	if _, _, _, err := store.AcceptWorkspaceInvitationForIdentity(tokenBeta, "Joined Member", alphaRecord); err != nil {
		t.Fatalf("AcceptWorkspaceInvitation beta: %v", err)
	}

	createReq := httptest.NewRequest("POST", "/api/auth/session", strings.NewReader(string(loadAuthAccessSessionCreateRequestBody(t, fullKey))))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("expected session create 200, got %d body=%s", createRec.Code, createRec.Body.String())
	}

	cookies := createRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie")
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/workspaces", nil)
	req.AddCookie(cookies[0])

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertAuthAccessFixtureEqual(t, AuthAccessFixtureWorkspacesListResponse, rec.Body.Bytes(), workspaceAlpha.ID)
}

func TestHandleCreateSessionInvalidKeyMatchesSharedErrorFixture(t *testing.T) {
	_, _, mux := newTestHandlerWithRoutes(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/auth/session",
		strings.NewReader(string(loadAuthAccessSessionCreateRequestBody(t, "inf_invalid"))),
	)
	req.Header.Set("Content-Type", "application/json")

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertAuthAccessFixtureEqual(t, AuthAccessFixtureAuthErrorInvalidApiKey, rec.Body.Bytes(), "")
}

func TestHandleCreateSessionServiceAccountForbiddenMatchesSharedErrorFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	serviceKey, _, err := store.CreateKeyWithPrincipal("svc-bot", RoleOperator, PrincipalServiceAccount)
	if err != nil {
		t.Fatalf("CreateKeyWithPrincipal: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/auth/session",
		strings.NewReader(string(loadAuthAccessSessionCreateRequestBody(t, serviceKey))),
	)
	req.Header.Set("Content-Type", "application/json")

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertAuthAccessFixtureEqual(t, AuthAccessFixtureAuthErrorServiceAccountSessionForbidden, rec.Body.Bytes(), "")
}

func TestHandleGetSessionNoCookieMatchesSharedErrorFixture(t *testing.T) {
	_, _, mux := newTestHandlerWithRoutes(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertAuthAccessFixtureEqual(t, AuthAccessFixtureAuthErrorMissingSessionCookie, rec.Body.Bytes(), "")
}

func TestHandleListAccessibleWorkspacesWithoutDashboardAccessMatchesSharedErrorFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	userKey, _, err := store.CreateKey("user", RoleUser)
	if err != nil {
		t.Fatalf("CreateKey user: %v", err)
	}

	rec := httptest.NewRecorder()
	req := authAccessFixtureRequest(http.MethodGet, "/api/auth/workspaces", nil, userKey)

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertAuthAccessFixtureEqual(t, AuthAccessFixtureAuthErrorWorkspaceAccessRequired, rec.Body.Bytes(), "")
}

func TestHandleListKeysWithoutPermissionMatchesSharedErrorFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	userKey, _, err := store.CreateKey("user", RoleUser)
	if err != nil {
		t.Fatalf("CreateKey user: %v", err)
	}

	rec := httptest.NewRecorder()
	req := authAccessFixtureRequest(http.MethodGet, "/api/auth/keys", nil, userKey)

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertAuthAccessFixtureEqual(t, AuthAccessFixtureAuthErrorKeyManagementAccessRequired, rec.Body.Bytes(), "")
}

func TestHandleKeysMethodNotAllowedMatchesSharedErrorFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	adminKey, _, _ := newAuthAccessFixtureWorkspaceAdmin(t, store, "Fixture Team")

	rec := httptest.NewRecorder()
	req := authAccessFixtureRequest(http.MethodPatch, "/api/auth/keys", nil, adminKey)

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertAuthAccessFixtureEqual(t, AuthAccessFixtureAuthErrorMethodNotAllowed, rec.Body.Bytes(), "")
}

func TestHandleSwitchSessionWorkspaceMissingWorkspaceIDMatchesSharedErrorFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	adminKey, adminRecord, workspace := newAuthAccessFixtureWorkspaceAdmin(t, store, "Fixture Team")
	sessionToken, _, err := store.CreateSession(adminRecord.ID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPut,
		"/api/auth/session/workspace",
		strings.NewReader(`{}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminKey)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionToken})

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertAuthAccessFixtureEqual(t, AuthAccessFixtureAuthErrorMissingWorkspaceId, rec.Body.Bytes(), workspace.ID)
}

func newAuthAccessFixtureWorkspaceAdmin(
	t *testing.T,
	store *Store,
	workspaceName string,
) (adminKey string, adminRecord *KeyRecord, workspace *WorkspaceRecord) {
	t.Helper()

	workspace, err := store.CreateWorkspace(workspaceName)
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	adminKey, adminRecord, err = store.CreateKeyInWorkspace(workspace.ID, "Fixture Admin", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateKeyInWorkspace: %v", err)
	}
	return adminKey, adminRecord, workspace
}

func authAccessFixtureRequest(method, path string, body []byte, apiKey string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	return req
}

func loadAuthAccessFixtureBytes(t *testing.T, name string) []byte {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "contracts", "auth_access", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func assertAuthAccessFixtureEqual(t *testing.T, fixtureName string, got []byte, workspaceID string) {
	t.Helper()

	want := decodeAuthAccessJSONMap(t, loadAuthAccessFixtureBytes(t, fixtureName))
	gotValue := decodeAuthAccessJSONMap(t, got)
	normalizeAuthAccessContract(want, workspaceID)
	normalizeAuthAccessContract(gotValue, workspaceID)

	if !reflect.DeepEqual(want, gotValue) {
		t.Fatalf(
			"json mismatch for %s\nwant: %s\ngot: %s",
			fixtureName,
			strings.TrimSpace(string(loadAuthAccessFixtureBytes(t, fixtureName))),
			strings.TrimSpace(string(got)),
		)
	}
}

func decodeAuthAccessJSONMap(t *testing.T, payload []byte) map[string]any {
	t.Helper()

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	return decoded
}

func loadAuthAccessSessionCreateRequestBody(t *testing.T, apiKey string) []byte {
	t.Helper()

	payload := decodeAuthAccessJSONMap(t, loadAuthAccessFixtureBytes(t, AuthAccessFixtureSessionCreateRequest))
	payload["api_key"] = apiKey
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal session create request: %v", err)
	}
	return body
}

func loadAuthAccessSwitchWorkspaceRequestBody(t *testing.T, workspaceID string) []byte {
	t.Helper()

	payload := decodeAuthAccessJSONMap(t, loadAuthAccessFixtureBytes(t, AuthAccessFixtureSessionSwitchWorkspaceRequest))
	payload["workspace_id"] = workspaceID
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal session switch request: %v", err)
	}
	return body
}

func normalizeAuthAccessContract(value map[string]any, workspaceID string) {
	if session, ok := value["session"].(map[string]any); ok {
		session["id"] = "normalized-session-id"
		if _, exists := session["expires_at"]; exists {
			session["expires_at"] = "normalized-expires-at"
		}
	}

	if topLevelKey, ok := value["key"].(string); ok && strings.HasPrefix(topLevelKey, "inf_") {
		value["key"] = "normalized-secret-key"
	} else if sessionKey, ok := value["key"].(map[string]any); ok {
		normalizeAuthAccessSessionKey(sessionKey, workspaceID)
	}

	if workspace, ok := value["workspace"].(map[string]any); ok {
		workspace["id"] = "normalized-workspace-id"
	}

	if workspaces, ok := value["workspaces"].([]any); ok {
		for index, raw := range workspaces {
			workspace, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			workspace["id"] = "normalized-workspace-id"
			if _, exists := workspace["created_at"]; exists {
				workspace["created_at"] = "normalized-created-at"
			}
			if index == 0 {
				workspace["slug"] = "fixture-team"
			}
			if index == 1 {
				workspace["slug"] = "beta-team"
			}
		}
	}

	if member, ok := value["member"].(map[string]any); ok {
		member["id"] = "normalized-membership-id"
	}

	if record, ok := value["record"].(map[string]any); ok {
		normalizeAuthAccessAPIKey(record, workspaceID)
	}

	if keys, ok := value["keys"].([]any); ok {
		for _, raw := range keys {
			record, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			normalizeAuthAccessAPIKey(record, workspaceID)
		}
	}
}

func normalizeAuthAccessSessionKey(record map[string]any, workspaceID string) {
	record["id"] = "normalized-key-id"
	record["key_prefix"] = "normalized-key-prefix"
	record["workspace_id"] = "normalized-workspace-id"
	_ = workspaceID
}

func normalizeAuthAccessAPIKey(record map[string]any, workspaceID string) {
	record["id"] = "normalized-key-id"
	record["workspace_id"] = "normalized-workspace-id"
	record["key_prefix"] = "normalized-key-prefix"
	if _, exists := record["created_at"]; exists {
		record["created_at"] = "normalized-created-at"
	}
	if _, exists := record["last_used"]; exists {
		record["last_used"] = "normalized-last-used-at"
	}
	if _, exists := record["membership_id"]; exists {
		record["membership_id"] = "normalized-membership-id"
	}
	_ = workspaceID
}
