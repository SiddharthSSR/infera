package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// HeaderRequestID is the header for request ID propagation.
	HeaderRequestID = "X-Request-ID"

	// HeaderTraceparent is the W3C trace context header.
	HeaderTraceparent = "traceparent"

	// maxRequestBodyBytes is the maximum allowed request body size (1MB).
	maxRequestBodyBytes = 1 << 20 // 1MB
)

type contextKey int

const traceparentKey contextKey = iota

// traceparentFromContext returns the traceparent stored in the context, or empty string.
func traceparentFromContext(ctx context.Context) string {
	v, _ := ctx.Value(traceparentKey).(string)
	return v
}

// generateTraceparent creates a new W3C traceparent header value.
// Format: 00-{16-byte trace-id hex}-{8-byte parent-id hex}-01
func generateTraceparent() string {
	traceID := make([]byte, 16)
	parentID := make([]byte, 8)
	_, _ = rand.Read(traceID)
	_, _ = rand.Read(parentID)
	return fmt.Sprintf("00-%s-%s-01", hex.EncodeToString(traceID), hex.EncodeToString(parentID))
}

// traceparentMiddleware reads the incoming W3C traceparent header (or generates one)
// and stores it in the request context for downstream propagation to workers.
func traceparentMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tp := strings.TrimSpace(r.Header.Get(HeaderTraceparent))
		if tp == "" {
			tp = generateTraceparent()
		}
		ctx := context.WithValue(r.Context(), traceparentKey, tp)
		w.Header().Set(HeaderTraceparent, tp)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// recoveryMiddleware catches panics in handlers and returns 500 instead of crashing.
func recoveryMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					stack := debug.Stack()
					logger.Error("panic recovered",
						slog.String("method", r.Method),
						slog.String("path", r.URL.Path),
						slog.Any("panic", rec),
						slog.String("stack", string(stack)),
						slog.String("request_id", r.Header.Get(HeaderRequestID)),
					)

					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					fmt.Fprintf(w, `{"error":{"type":"internal_error","message":"Internal server error"}}`)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// requestIDMiddleware injects an X-Request-ID header if one is not already present.
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get(HeaderRequestID)
		if reqID == "" {
			reqID = uuid.New().String()
			r.Header.Set(HeaderRequestID, reqID)
		}
		w.Header().Set(HeaderRequestID, reqID)
		next.ServeHTTP(w, r)
	})
}

// timeoutMiddleware applies a deadline to non-streaming requests.
// Streaming endpoints (/v1/chat/completions with stream=true) set their own
// timeout inside the handler, so this is a safety net for everything else.
func timeoutMiddleware(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.TimeoutHandler(next, timeout, `{"error":{"type":"timeout","message":"Request timed out"}}`)
	}
}

// bodySizeLimitMiddleware restricts the request body to maxBytes.
func bodySizeLimitMiddleware(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// chainMiddleware applies middleware in order (first applied = outermost).
func chainMiddleware(h http.Handler, middlewares ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}
