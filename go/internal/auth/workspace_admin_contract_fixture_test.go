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
)

func TestHandlePutWorkspaceQuotaMatchesSharedFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	adminKey, workspaceID := newWorkspaceAdminFixtureContext(t, store)

	rec := httptest.NewRecorder()
	req := workspaceAdminFixtureRequest(
		http.MethodPut,
		"/api/auth/workspaces/"+workspaceID+"/quota",
		loadWorkspaceAdminFixtureBytes(t, WorkspaceAdminFixtureWorkspaceQuotaUpdateRequest),
		adminKey,
	)
	req.Header.Set("Content-Type", "application/json")

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertWorkspaceAdminFixtureEqual(t, WorkspaceAdminFixtureWorkspaceQuotaResponse, rec.Body.Bytes())
}

func TestHandleGetWorkspaceQuotaMatchesSharedFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	adminKey, workspaceID := newWorkspaceAdminFixtureContext(t, store)
	applyWorkspaceQuotaFixture(t, mux, adminKey, workspaceID)

	rec := httptest.NewRecorder()
	req := workspaceAdminFixtureRequest(http.MethodGet, "/api/auth/workspaces/"+workspaceID+"/quota", nil, adminKey)

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertWorkspaceAdminFixtureEqual(t, WorkspaceAdminFixtureWorkspaceQuotaResponse, rec.Body.Bytes())
}

func TestHandlePutWorkspaceProviderConfigMatchesSharedFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	adminKey, workspaceID := newWorkspaceAdminFixtureContext(t, store)

	rec := httptest.NewRecorder()
	req := workspaceAdminFixtureRequest(
		http.MethodPut,
		"/api/auth/workspaces/"+workspaceID+"/providers/runpod",
		loadWorkspaceAdminFixtureBytes(t, WorkspaceAdminFixtureWorkspaceProviderConfigUpsertRequest),
		adminKey,
	)
	req.Header.Set("Content-Type", "application/json")

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertWorkspaceAdminFixtureEqual(t, WorkspaceAdminFixtureWorkspaceProviderConfigResponse, rec.Body.Bytes())
}

func TestHandleWorkspaceProvidersMatchesSharedFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	adminKey, workspaceID := newWorkspaceAdminFixtureContext(t, store)
	applyWorkspaceProviderConfigFixture(t, mux, adminKey, workspaceID)

	rec := httptest.NewRecorder()
	req := workspaceAdminFixtureRequest(
		http.MethodGet,
		"/api/auth/workspaces/"+workspaceID+"/providers",
		nil,
		adminKey,
	)

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertWorkspaceAdminFixtureEqual(t, WorkspaceAdminFixtureWorkspaceProviderConfigsListResponse, rec.Body.Bytes())
}

func TestHandleWorkspaceByIDMissingPathMatchesSharedErrorFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	adminKey, _ := newWorkspaceAdminFixtureContext(t, store)

	rec := httptest.NewRecorder()
	req := workspaceAdminFixtureRequest(http.MethodGet, "/api/auth/workspaces/", nil, adminKey)

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertWorkspaceAdminFixtureEqual(t, WorkspaceAdminFixtureAuthErrorWorkspacePathRequired, rec.Body.Bytes())
}

func TestHandleWorkspaceByIDUnknownSubresourceMatchesSharedErrorFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	adminKey, workspaceID := newWorkspaceAdminFixtureContext(t, store)

	rec := httptest.NewRecorder()
	req := workspaceAdminFixtureRequest(http.MethodGet, "/api/auth/workspaces/"+workspaceID+"/unknown", nil, adminKey)

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertWorkspaceAdminFixtureEqual(t, WorkspaceAdminFixtureAuthErrorNotFound, rec.Body.Bytes())
}

func TestHandleGetWorkspaceQuotaWithoutUsageAccessMatchesSharedErrorFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	_, workspaceID := newWorkspaceAdminFixtureContext(t, store)
	developerKey, _, err := store.CreateKey("developer", RoleDeveloper)
	if err != nil {
		t.Fatalf("CreateKey developer: %v", err)
	}

	rec := httptest.NewRecorder()
	req := workspaceAdminFixtureRequest(http.MethodGet, "/api/auth/workspaces/"+workspaceID+"/quota", nil, developerKey)

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertWorkspaceAdminFixtureEqual(t, WorkspaceAdminFixtureAuthErrorUsageAccessRequired, rec.Body.Bytes())
}

func TestHandleWorkspaceMembersMethodNotAllowedMatchesSharedErrorFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	adminKey, _, workspace := newWorkspaceMembershipFixtureContext(t, store)

	rec := httptest.NewRecorder()
	req := workspaceAdminFixtureRequest(http.MethodPost, "/api/auth/workspaces/"+workspace.ID+"/members", nil, adminKey)

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertWorkspaceAdminFixtureEqual(t, WorkspaceAdminFixtureAuthErrorMethodNotAllowed, rec.Body.Bytes())
}

func TestHandlePutWorkspaceProviderConfigUnknownProviderMatchesSharedErrorFixture(t *testing.T) {
	_, store, mux := newTestHandlerWithRoutes(t)
	adminKey, workspaceID := newWorkspaceAdminFixtureContext(t, store)

	rec := httptest.NewRecorder()
	req := workspaceAdminFixtureRequest(
		http.MethodPut,
		"/api/auth/workspaces/"+workspaceID+"/providers/not-a-provider",
		loadWorkspaceAdminFixtureBytes(t, WorkspaceAdminFixtureWorkspaceProviderConfigUpsertRequest),
		adminKey,
	)
	req.Header.Set("Content-Type", "application/json")

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertWorkspaceAdminFixtureEqual(t, WorkspaceAdminFixtureAuthErrorUnknownProvider, rec.Body.Bytes())
}

func newWorkspaceAdminFixtureContext(t *testing.T, store *Store) (adminKey string, workspaceID string) {
	t.Helper()

	adminKey, _, err := store.CreateKey("admin", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateKey admin: %v", err)
	}
	workspace, err := store.CreateWorkspace("Fixture Team")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	return adminKey, workspace.ID
}

func newWorkspaceMembershipFixtureContext(t *testing.T, store *Store) (adminKey string, adminRecord *KeyRecord, workspace *WorkspaceRecord) {
	t.Helper()

	adminKey, adminRecord, err := store.CreateKey("admin", RoleAdmin)
	if err != nil {
		t.Fatalf("CreateKey admin: %v", err)
	}
	workspace, err = store.CreateWorkspace("Fixture Team")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	return adminKey, adminRecord, workspace
}

func applyWorkspaceQuotaFixture(t *testing.T, mux *http.ServeMux, adminKey, workspaceID string) {
	t.Helper()

	rec := httptest.NewRecorder()
	req := workspaceAdminFixtureRequest(
		http.MethodPut,
		"/api/auth/workspaces/"+workspaceID+"/quota",
		loadWorkspaceAdminFixtureBytes(t, WorkspaceAdminFixtureWorkspaceQuotaUpdateRequest),
		adminKey,
	)
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func applyWorkspaceProviderConfigFixture(t *testing.T, mux *http.ServeMux, adminKey, workspaceID string) {
	t.Helper()

	rec := httptest.NewRecorder()
	req := workspaceAdminFixtureRequest(
		http.MethodPut,
		"/api/auth/workspaces/"+workspaceID+"/providers/runpod",
		loadWorkspaceAdminFixtureBytes(t, WorkspaceAdminFixtureWorkspaceProviderConfigUpsertRequest),
		adminKey,
	)
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func workspaceAdminFixtureRequest(method, path string, body []byte, adminKey string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer "+adminKey)
	return req
}

func loadWorkspaceAdminFixtureBytes(t *testing.T, name string) []byte {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "contracts", "workspace_admin", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func assertWorkspaceAdminFixtureEqual(t *testing.T, fixtureName string, got []byte) {
	t.Helper()

	want := decodeWorkspaceAdminJSONMap(t, loadWorkspaceAdminFixtureBytes(t, fixtureName))
	gotValue := decodeWorkspaceAdminJSONMap(t, got)
	normalizeWorkspaceAdminContract(want)
	normalizeWorkspaceAdminContract(gotValue)

	if !reflect.DeepEqual(want, gotValue) {
		t.Fatalf(
			"json mismatch for %s\nwant: %s\ngot: %s",
			fixtureName,
			strings.TrimSpace(string(loadWorkspaceAdminFixtureBytes(t, fixtureName))),
			strings.TrimSpace(string(got)),
		)
	}
}

func decodeWorkspaceAdminJSONMap(t *testing.T, payload []byte) map[string]any {
	t.Helper()

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	return decoded
}

func normalizeWorkspaceAdminContract(value map[string]any) {
	if quota, ok := value["quota"].(map[string]any); ok {
		quota["workspace_id"] = "normalized-workspace-id"
		if _, exists := quota["updated_at"]; exists {
			quota["updated_at"] = "normalized-updated-at"
		}
	}

	if provider, ok := value["provider"].(map[string]any); ok {
		normalizeWorkspaceAdminProvider(provider)
	}

	if providers, ok := value["providers"].([]any); ok {
		for _, raw := range providers {
			provider, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			normalizeWorkspaceAdminProvider(provider)
		}
	}

	if member, ok := value["member"].(map[string]any); ok {
		normalizeWorkspaceAdminMember(member)
	}

	if members, ok := value["members"].([]any); ok {
		for _, raw := range members {
			member, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			normalizeWorkspaceAdminMember(member)
		}
	}

	if invitation, ok := value["invitation"].(map[string]any); ok {
		if _, hasWorkspaceSlug := invitation["workspace_slug"]; hasWorkspaceSlug {
			normalizeWorkspaceAdminInvitationPreview(invitation)
		} else {
			normalizeWorkspaceAdminInvitation(invitation)
		}
	}

	if invitations, ok := value["invitations"].([]any); ok {
		for _, raw := range invitations {
			invitation, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			normalizeWorkspaceAdminInvitation(invitation)
		}
	}

	if membership, ok := value["membership"].(map[string]any); ok {
		normalizeWorkspaceAdminMember(membership)
	}

	if record, ok := value["record"].(map[string]any); ok {
		normalizeWorkspaceAdminAcceptedKeyRecord(record)
	}

	if _, exists := value["invitation_token"]; exists {
		value["invitation_token"] = "normalized-invitation-token"
	}

	if _, exists := value["key"]; exists {
		value["key"] = "normalized-api-key"
	}
}

func normalizeWorkspaceAdminProvider(provider map[string]any) {
	provider["workspace_id"] = "normalized-workspace-id"
	if _, exists := provider["created_at"]; exists {
		provider["created_at"] = "normalized-created-at"
	}
	if _, exists := provider["updated_at"]; exists {
		provider["updated_at"] = "normalized-updated-at"
	}
}

func normalizeWorkspaceAdminMember(member map[string]any) {
	member["id"] = "normalized-membership-id"
	member["workspace_id"] = "normalized-workspace-id"
	if _, exists := member["created_at"]; exists {
		member["created_at"] = "normalized-created-at"
	}
}

func normalizeWorkspaceAdminInvitation(invitation map[string]any) {
	invitation["id"] = "normalized-invitation-id"
	invitation["workspace_id"] = "normalized-workspace-id"
	invitation["invited_by_key_id"] = "normalized-invited-by-key-id"
	if _, exists := invitation["created_at"]; exists {
		invitation["created_at"] = "normalized-created-at"
	}
	if _, exists := invitation["expires_at"]; exists {
		invitation["expires_at"] = "normalized-expires-at"
	}
}

func normalizeWorkspaceAdminInvitationPreview(invitation map[string]any) {
	invitation["workspace_id"] = "normalized-workspace-id"
	if _, exists := invitation["expires_at"]; exists {
		invitation["expires_at"] = "normalized-expires-at"
	}
}

func normalizeWorkspaceAdminAcceptedKeyRecord(record map[string]any) {
	record["id"] = "normalized-key-id"
	record["workspace_id"] = "normalized-workspace-id"
	record["key_prefix"] = "normalized-key-prefix"
	record["membership_id"] = "normalized-membership-id"
	if _, exists := record["created_at"]; exists {
		record["created_at"] = "normalized-created-at"
	}
	if _, exists := record["last_used"]; exists {
		record["last_used"] = "normalized-last-used-at"
	}
}
