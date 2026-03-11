package gateway

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/infera/infera/go/internal/auth"
)

func TestRateLimiterAllowsBurst(t *testing.T) {
	rl := NewRateLimiter(RateLimiterConfig{
		RequestsPerMinute: 60,
		BurstSize:         5,
		CleanupInterval:   5 * 60 * 1e9, // 5 min, won't trigger in test
	})
	defer rl.Stop()

	// Should allow burst (5 + 1 second of tokens = 6)
	allowed := 0
	for i := 0; i < 10; i++ {
		if rl.Allow("key-1") {
			allowed++
		}
	}

	if allowed < 5 {
		t.Errorf("expected at least 5 burst requests allowed, got %d", allowed)
	}
	if allowed > 7 {
		t.Errorf("expected at most 7 requests allowed, got %d", allowed)
	}
}

func TestRateLimiterSeparateKeys(t *testing.T) {
	rl := NewRateLimiter(RateLimiterConfig{
		RequestsPerMinute: 60,
		BurstSize:         2,
		CleanupInterval:   5 * 60 * 1e9,
	})
	defer rl.Stop()

	// Exhaust key-1
	for i := 0; i < 10; i++ {
		rl.Allow("key-1")
	}

	// key-2 should still work
	if !rl.Allow("key-2") {
		t.Error("key-2 should not be affected by key-1 exhaustion")
	}
}

func TestRateLimiterRetryAfter(t *testing.T) {
	rl := NewRateLimiter(RateLimiterConfig{
		RequestsPerMinute: 60, // 1 per second
		BurstSize:         0,
		CleanupInterval:   5 * 60 * 1e9,
	})
	defer rl.Stop()

	// Exhaust the bucket
	for i := 0; i < 5; i++ {
		rl.Allow("key-1")
	}

	retryAfter := rl.RetryAfter("key-1")
	if retryAfter < 1 {
		t.Errorf("expected retry-after >= 1, got %d", retryAfter)
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	rl := NewRateLimiter(RateLimiterConfig{
		RequestsPerMinute: 60,
		BurstSize:         1,
		CleanupInterval:   5 * 60 * 1e9,
	})
	defer rl.Stop()

	handler := RateLimitMiddleware(rl)(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("allows requests within limit", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		req.Header.Set("Authorization", "Bearer test-key-1")
		rec := httptest.NewRecorder()
		handler(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("rejects when rate limited", func(t *testing.T) {
		// Exhaust the bucket for this key
		for i := 0; i < 10; i++ {
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			req.Header.Set("Authorization", "Bearer test-key-exhaust")
			rec := httptest.NewRecorder()
			handler(rec, req)
		}

		// Next request should be rate limited
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		req.Header.Set("Authorization", "Bearer test-key-exhaust")
		rec := httptest.NewRecorder()
		handler(rec, req)

		if rec.Code != http.StatusTooManyRequests {
			t.Errorf("expected 429, got %d", rec.Code)
		}
		if rec.Header().Get("Retry-After") == "" {
			t.Error("expected Retry-After header")
		}
	})

	t.Run("no key passes through to next handler", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		rec := httptest.NewRecorder()
		handler(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 for no-key request, got %d", rec.Code)
		}
	})

	t.Run("uses auth context when session auth is present", func(t *testing.T) {
		dir := t.TempDir()
		store, err := auth.NewStore(filepath.Join(dir, "auth.db"))
		if err != nil {
			t.Fatalf("failed to create auth store: %v", err)
		}
		defer store.Close()

		_, keyRecord, err := store.CreateKey("session-user", "admin")
		if err != nil {
			t.Fatalf("failed to create API key: %v", err)
		}
		sessionToken, _, err := store.CreateSession(keyRecord.ID)
		if err != nil {
			t.Fatalf("failed to create session: %v", err)
		}

		authHandler := auth.NewHandler(store)
		authHandler.SetSecure(false)
		sessionProtectedHandler := authHandler.RequireAuth(handler)

		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		req.AddCookie(&http.Cookie{Name: "infera_session", Value: sessionToken})
		rec := httptest.NewRecorder()
		sessionProtectedHandler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected first session-authenticated request to pass, got %d", rec.Code)
		}

		for i := 0; i < 10; i++ {
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			req.AddCookie(&http.Cookie{Name: "infera_session", Value: sessionToken})
			rec := httptest.NewRecorder()
			sessionProtectedHandler(rec, req)
		}

		req = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		req.AddCookie(&http.Cookie{Name: "infera_session", Value: sessionToken})
		rec = httptest.NewRecorder()
		sessionProtectedHandler(rec, req)
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("expected 429 for session-authenticated request, got %d", rec.Code)
		}
	})
}

func TestDefaultRateLimiterConfig(t *testing.T) {
	cfg := DefaultRateLimiterConfig()
	if cfg.RequestsPerMinute != 60 {
		t.Errorf("expected 60 rpm, got %d", cfg.RequestsPerMinute)
	}
	if cfg.BurstSize != 10 {
		t.Errorf("expected burst 10, got %d", cfg.BurstSize)
	}
}
