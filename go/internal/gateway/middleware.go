package gateway

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/google/uuid"
)

const (
	// HeaderRequestID is the header for request ID propagation.
	HeaderRequestID = "X-Request-ID"

	// maxRequestBodyBytes is the maximum allowed request body size (1MB).
	maxRequestBodyBytes = 1 << 20 // 1MB
)

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
