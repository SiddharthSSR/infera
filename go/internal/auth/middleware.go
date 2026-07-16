package auth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/infera/infera/go/internal/providers"
)

type contextKey string

const keyContextKey contextKey = "auth_key"
const sessionContextKey contextKey = "auth_session"
const sessionCookieName = "infera_session"

// KeyFromContext extracts the KeyRecord from the request context.
func KeyFromContext(ctx context.Context) *KeyRecord {
	if v := ctx.Value(keyContextKey); v != nil {
		return v.(*KeyRecord)
	}
	return nil
}

// SessionFromContext extracts the SessionRecord from the request context.
func SessionFromContext(ctx context.Context) *SessionRecord {
	if v := ctx.Value(sessionContextKey); v != nil {
		return v.(*SessionRecord)
	}
	return nil
}

// ContextWithKey injects a key record into a context for internal handlers and tests.
func ContextWithKey(ctx context.Context, record *KeyRecord) context.Context {
	return context.WithValue(ctx, keyContextKey, record)
}

// Handler wraps the auth store and provides middleware.
type Handler struct {
	store                   *Store
	secure                  bool // true = Secure flag on cookies (HTTPS only)
	providerConfigValidator ProviderConfigValidator
}

// ProviderConfigValidator verifies draft provider credentials before they are
// persisted. Production wiring uses a live provider status request; tests may
// inject a deterministic validator.
type ProviderConfigValidator func(context.Context, providers.ProviderConfig) error

// NewHandler creates a new auth handler.
func NewHandler(store *Store) *Handler {
	return &Handler{store: store, secure: true}
}

// SetSecure controls the Secure flag on session cookies.
// Set to false for local development (HTTP).
func (h *Handler) SetSecure(secure bool) {
	h.secure = secure
}

// SetProviderConfigValidator installs validation for workspace provider
// credentials. A nil validator preserves store-only behavior for callers that
// do not manage live providers.
func (h *Handler) SetProviderConfigValidator(validator ProviderConfigValidator) {
	h.providerConfigValidator = validator
}

// Store returns the underlying store.
func (h *Handler) Store() *Store {
	return h.store
}

// RequireAuth middleware rejects requests without a valid API key or session cookie.
// It tries the session cookie first, then falls back to Bearer/X-API-Key headers.
func (h *Handler) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Try session cookie first
		if cookie, err := r.Cookie(sessionCookieName); err == nil && cookie.Value != "" {
			session, keyRecord, err := h.store.ValidateSession(cookie.Value)
			if err == nil {
				ctx := context.WithValue(r.Context(), keyContextKey, keyRecord)
				ctx = context.WithValue(ctx, sessionContextKey, session)
				next(w, r.WithContext(ctx))
				return
			}
			// Cookie invalid/expired — clear it and fall through to Bearer
			http.SetCookie(w, h.expiredSessionCookie())
		}

		// Fall back to Bearer / X-API-Key
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
		if record == nil || (record.Role != RoleAdmin && record.Role != RoleOwner) {
			writeAuthorizationError(w, "Admin access required.")
			return
		}
		next(w, r)
	})
}

func (h *Handler) RequirePermission(permission, message string, next http.HandlerFunc) http.HandlerFunc {
	return h.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		record := KeyFromContext(r.Context())
		if !HasPermission(record, permission) {
			writeAuthorizationError(w, message)
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

func (h *Handler) sessionCookie(token string) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(sessionDuration.Seconds()),
	}
}

func (h *Handler) expiredSessionCookie() *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   h.secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	}
}

func writeAuthError(w http.ResponseWriter, status int, message string) {
	writeTypedError(w, status, "authentication_error", message)
}

func writeAuthorizationError(w http.ResponseWriter, message string) {
	writeTypedError(w, http.StatusForbidden, "authorization_error", message)
}

func writeInvalidRequestError(w http.ResponseWriter, message string) {
	writeTypedError(w, http.StatusBadRequest, "invalid_request_error", message)
}

func writeNotFoundError(w http.ResponseWriter, message string) {
	writeTypedError(w, http.StatusNotFound, "not_found_error", message)
}

func writeMethodNotAllowedError(w http.ResponseWriter) {
	writeTypedError(w, http.StatusMethodNotAllowed, "invalid_request_error", "Method not allowed")
}

func writeInternalError(w http.ResponseWriter, message string) {
	writeTypedError(w, http.StatusInternalServerError, "internal_error", message)
}

func writeTypedError(w http.ResponseWriter, status int, errType, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	payload := map[string]any{
		"error": map[string]string{
			"type":    errType,
			"message": message,
		},
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Error("failed to encode auth error response", slog.String("error", err.Error()))
	}
}
