package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// contextKey is a private type for context keys defined in this package.
// Using a dedicated unexported type (rather than a plain string) avoids
// collisions with context keys set by other packages or third-party
// middleware — this is the standard Go idiom for context key safety.
type contextKey string

// userIDContextKey is the key under which the authenticated user's
// public user_id is stored in the request context after successful
// JWT validation.
const userIDContextKey contextKey = "user_id"

// UserIDFromContext retrieves the authenticated user's user_id from a
// request context. It is intended to be called from within handlers
// that sit behind Middleware — calling it on a context that never
// passed through Middleware will correctly return ("", false).
//
// This function is the ONLY sanctioned way for a handler to learn
// "who is making this request." Handlers must never parse the
// Authorization header themselves.
func UserIDFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(userIDContextKey).(string)
	return userID, ok
}

// errorResponse is a minimal local JSON error shape used only for
// middleware-level auth failures (401s). The handlers package will
// define its own richer response types for business-logic errors;
// this stays intentionally small and self-contained so the auth
// package has zero dependency on the handlers package.
type errorResponse struct {
	Error string `json:"error"`
}

// writeUnauthorized writes a uniform 401 JSON response and stops
// request processing. Centralizing this ensures every auth failure —
// missing header, malformed header, invalid signature, expired token —
// produces byte-for-byte identical output. That uniformity matters: it
// prevents a client (or attacker) from distinguishing "you sent no
// token" from "your token is expired" from "your token is forged" by
// inspecting response differences.
func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(errorResponse{Error: "unauthorized"})
}

// Middleware returns an http.Handler-wrapping middleware that enforces
// JWT authentication. It is a method on TokenManager (rather than a
// free function taking a secret) so it has direct access to
// ValidateToken without needing the secret passed around separately —
// consistent with the TokenManager design in jwt.go.
//
// Usage (in main.go, once router wiring happens):
//
//	protected := r.PathPrefix("/api").Subrouter()
//	protected.Use(tokenManager.Middleware)
//	protected.HandleFunc("/me", handlers.Me).Methods("GET")
func (tm *TokenManager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			writeUnauthorized(w)
			return
		}

		// Expect exactly "Bearer <token>". Splitting on whitespace with
		// a strict length check (rather than TrimPrefix) rejects
		// malformed headers like "Bearer" (no token) or
		// "Bearer  token" (double space) up front, instead of passing
		// a garbage string into ValidateToken and relying on it to fail
		// for the right reason.
		parts := strings.Fields(authHeader)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			writeUnauthorized(w)
			return
		}
		tokenString := parts[1]

		claims, err := tm.ValidateToken(tokenString)
		if err != nil {
			writeUnauthorized(w)
			return
		}

		// Inject the validated user_id into the request context and
		// pass control to the next handler in the chain.
		ctx := context.WithValue(r.Context(), userIDContextKey, claims.UserID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}