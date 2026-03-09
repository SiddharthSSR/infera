package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/infera/infera/go/internal/router"
)

func TestHandleCORSAllowedOrigin(t *testing.T) {
	g := New(Config{
		EnableCORS:     true,
		AllowedOrigins: []string{"https://app.example.com"},
	}, nil, nil)

	handler := g.handleCORS(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodOptions, "/api/health", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("expected allow origin header to match request origin, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("expected credentials to be enabled for explicit origin, got %q", got)
	}
}

func TestHandleCORSDisallowedOrigin(t *testing.T) {
	g := New(Config{
		EnableCORS:     true,
		AllowedOrigins: []string{"https://app.example.com"},
	}, nil, nil)

	handler := g.handleCORS(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodOptions, "/api/health", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for disallowed origin, got %d", rec.Code)
	}
}

func TestHandleCORSWildcardOrigin(t *testing.T) {
	g := New(Config{
		EnableCORS:     true,
		AllowedOrigins: []string{"*"},
	}, nil, nil)

	handler := g.handleCORS(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("expected wildcard allow origin, got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("expected credentials header to be empty for wildcard origin, got %q", got)
	}
}

func TestRequireWorkerTokenOnRegister(t *testing.T) {
	r := router.New(router.DefaultConfig())
	defer r.Stop()

	g := New(Config{WorkerSharedToken: "secret-token"}, r, nil)
	handler := g.requireWorkerToken(g.handleRegisterWorker)

	body := `{"worker_id":"w1","address":"localhost:8081","status":"healthy","loaded_models":[]}`

	reqNoToken := httptest.NewRequest(http.MethodPost, "/api/workers/register", strings.NewReader(body))
	recNoToken := httptest.NewRecorder()
	handler(recNoToken, reqNoToken)
	if recNoToken.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", recNoToken.Code)
	}

	reqWithToken := httptest.NewRequest(http.MethodPost, "/api/workers/register", strings.NewReader(body))
	reqWithToken.Header.Set("X-Worker-Token", "secret-token")
	recWithToken := httptest.NewRecorder()
	handler(recWithToken, reqWithToken)
	if recWithToken.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid token, got %d", recWithToken.Code)
	}
}
