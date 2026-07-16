package gateway

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func TestRecoveryMiddleware(t *testing.T) {
	t.Run("catches panic and returns 500", func(t *testing.T) {
		handler := recoveryMiddleware(testLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("unexpected error")
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %d", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Fatalf("expected JSON content type, got %q", ct)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "internal_error") {
			t.Fatalf("expected internal_error in body, got %q", body)
		}
	})

	t.Run("passes through on no panic", func(t *testing.T) {
		handler := recoveryMiddleware(testLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write([]byte("ok")); err != nil {
				panic(err)
			}
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})
}

func TestRequestIDMiddleware(t *testing.T) {
	t.Run("generates ID when missing", func(t *testing.T) {
		handler := requestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if id := r.Header.Get(HeaderRequestID); id == "" {
				t.Error("expected request ID to be injected into request header")
			}
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if id := rec.Header().Get(HeaderRequestID); id == "" {
			t.Error("expected request ID in response header")
		}
	})

	t.Run("preserves existing ID", func(t *testing.T) {
		handler := requestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if id := r.Header.Get(HeaderRequestID); id != "existing-id" {
				t.Errorf("expected existing-id, got %q", id)
			}
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(HeaderRequestID, "existing-id")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if id := rec.Header().Get(HeaderRequestID); id != "existing-id" {
			t.Errorf("expected existing-id in response, got %q", id)
		}
	})
}

func TestBodySizeLimitMiddleware(t *testing.T) {
	t.Run("allows small body", func(t *testing.T) {
		handler := bodySizeLimitMiddleware(100)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("unexpected error reading body: %v", err)
			}
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("small body"))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("rejects oversized body", func(t *testing.T) {
		handler := bodySizeLimitMiddleware(10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, err := io.ReadAll(r.Body)
			if err != nil {
				// MaxBytesReader returns an error — handler should detect this
				http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
				return
			}
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("this body is way too large for the limit"))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("expected 413, got %d", rec.Code)
		}
	})
}

func TestChainMiddleware(t *testing.T) {
	var order []string

	m1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "m1-before")
			next.ServeHTTP(w, r)
			order = append(order, "m1-after")
		})
	}
	m2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "m2-before")
			next.ServeHTTP(w, r)
			order = append(order, "m2-after")
		})
	}

	handler := chainMiddleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "handler")
		}),
		m1, m2,
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	expected := []string{"m1-before", "m2-before", "handler", "m2-after", "m1-after"}
	if len(order) != len(expected) {
		t.Fatalf("expected %d calls, got %d: %v", len(expected), len(order), order)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("position %d: expected %q, got %q", i, v, order[i])
		}
	}
}
