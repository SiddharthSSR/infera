package gateway

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infera/infera/go/internal/auth"
)

func TestDeactivatedWorkspaceCannotRouteOrProvision(t *testing.T) {
	store, err := auth.NewStore(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	workspace, err := store.CreateWorkspace("Inactive Gateway Workspace")
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	rawKey, _, err := store.CreateKeyInWorkspace(workspace.ID, "operator", auth.RoleOperator)
	if err != nil {
		t.Fatalf("CreateKeyInWorkspace: %v", err)
	}
	if _, err := store.DeactivateWorkspace(workspace.ID); err != nil {
		t.Fatalf("DeactivateWorkspace: %v", err)
	}

	authHandler := auth.NewHandler(store)
	gateway := &Gateway{}
	instanceHandlers := &InstanceHandlers{}
	tests := []struct {
		name    string
		path    string
		body    string
		handler http.HandlerFunc
	}{
		{
			name:    "inference routing",
			path:    "/v1/chat/completions",
			body:    `{"model":"test","messages":[]}`,
			handler: gateway.handleChatCompletions,
		},
		{
			name:    "instance provisioning",
			path:    "/api/instances/provision",
			body:    `{"gpu_type":"RTX_4090","engine":"mock"}`,
			handler: instanceHandlers.handleProvision,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			req.Header.Set("Authorization", "Bearer "+rawKey)
			rec := httptest.NewRecorder()

			authHandler.RequireAuth(test.handler).ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected deactivated workspace auth to fail with 401, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}
