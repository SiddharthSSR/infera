package auth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

type contextKey string

const keyContextKey contextKey = "auth_key"

// KeyFromContext extracts the KeyRecord from the request context.
func KeyFromContext(ctx context.Context) *KeyRecord {
	if v := ctx.Value(keyContextKey); v != nil {
		return v.(*KeyRecord)
	}
	return nil
}

// Handler wraps the auth store and provides middleware.
type Handler struct {
	store *Store
}

// NewHandler creates a new auth handler.
func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

// Store returns the underlying store.
func (h *Handler) Store() *Store {
	return h.store
}

// RequireAuth middleware rejects requests without a valid API key.
func (h *Handler) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := extractKey(r)
		if key == "" {
			writeAuthError(w, http.StatusUnauthorized, "Authentication required. Provide API key via Authorization: Bearer <key> or X-API-Key header.")
			return
		}

		record, err := h.store.ValidateKey(key)
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, "Invalid or revoked API key.")
			return
		}

		ctx := context.WithValue(r.Context(), keyContextKey, record)
		next(w, r.WithContext(ctx))
	}
}

// RequireAdmin middleware rejects requests without an admin API key.
func (h *Handler) RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return h.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		record := KeyFromContext(r.Context())
		if record == nil || record.Role != "admin" {
			writeAuthError(w, http.StatusForbidden, "Admin access required.")
			return
		}
		next(w, r)
	})
}

func extractKey(r *http.Request) string {
	// Try Authorization: Bearer <key>
	auth := r.Header.Get("Authorization")
	fields := strings.Fields(auth)
	if len(fields) >= 2 && strings.EqualFold(fields[0], "Bearer") {
		return fields[1]
	}

	// Try X-API-Key header
	if key := r.Header.Get("X-API-Key"); key != "" {
		return key
	}

	return ""
}

func writeAuthError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	payload := map[string]any{
		"error": map[string]string{
			"type":    "authentication_error",
			"message": message,
		},
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Error("failed to encode auth error response", slog.String("error", err.Error()))
	}
}
