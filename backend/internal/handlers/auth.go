package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"operation-advertise/backend/internal/auth"
	"operation-advertise/backend/internal/database"
	"operation-advertise/backend/internal/util"
)

// AuthHandler bundles the dependencies needed to serve authentication
// routes: a database connection and a token manager for issuing JWTs.
//
// Using a struct with methods (rather than package-level functions
// closing over globals) means main.go explicitly constructs one
// AuthHandler at startup with its real dependencies, and tests can
// construct one with a test database and a throwaway signing secret.
// No hidden global state anywhere in the request path.
type AuthHandler struct {
	DB           *database.DB
	TokenManager *auth.TokenManager
}

// NewAuthHandler constructs an AuthHandler. A constructor function
// (rather than exposing the struct literal everywhere) gives us one
// place to add validation later if these dependencies grow.
func NewAuthHandler(db *database.DB, tm *auth.TokenManager) *AuthHandler {
	return &AuthHandler{DB: db, TokenManager: tm}
}

// ---------------------------------------------------------------------
// Request / response payloads
// ---------------------------------------------------------------------

// registerRequest is the expected JSON body for POST /api/register.
type registerRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// loginRequest is the expected JSON body for POST /api/login.
type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// authResponse is the JSON body returned by both register and login on
// success. Both endpoints return the same shape deliberately: from the
// client's perspective, "just registered" and "just logged in" both
// end in the same state — authenticated, holding a token and knowing
// their user_id.
type authResponse struct {
	Token  string `json:"token"`
	UserID string `json:"user_id"`
}

// apiError is the uniform JSON error shape used across all handlers in
// this package (and, for consistency, matches the shape used in
// auth/middleware.go's errorResponse). Centralizing this as one type
// used everywhere means clients only ever need to handle one error
// response format.
type apiError struct {
	Error string `json:"error"`
}

// ---------------------------------------------------------------------
// Shared response helpers
// ---------------------------------------------------------------------

// writeJSON marshals v and writes it with the given status code. Every
// handler funnels its responses through this to guarantee consistent
// headers and to centralize encode-error logging in one place.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("handlers: failed to encode JSON response: %v", err)
	}
}

// writeError writes a uniform apiError response. Kept as a thin wrapper
// around writeJSON specifically so every error path in this package
// reads identically at the call site: writeError(w, status, "message").
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, apiError{Error: message})
}

// ---------------------------------------------------------------------
// POST /api/register
// ---------------------------------------------------------------------

// maxUserIDGenerationAttempts bounds the retry loop that handles the
// astronomically unlikely case of a user_id collision (see
// util/random.go's doc comment on the ~218 trillion ID space). A finite
// bound prevents any possibility of an infinite loop; if we ever
// legitimately exhaust this many attempts, something is badly wrong
// (e.g. the RNG is broken) and we should fail loudly rather than spin
// forever.
const maxUserIDGenerationAttempts = 5

// Register handles POST /api/register. It validates input, hashes the
// password, generates a unique user_id (retrying on the rare collision
// case), creates the user row, and returns a freshly issued JWT — so a
// newly registered user is immediately logged in without a separate
// login round-trip.
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	username := strings.TrimSpace(req.Username)
	if username == "" {
		writeError(w, http.StatusBadRequest, "username is required")
		return
	}
	// Username length bounds are enforced here, not just relying on the
	// DB's UNIQUE constraint (which says nothing about length). Bounds
	// chosen to be generous but not unlimited — an unbounded username
	// is a minor but real resource-exhaustion vector (extremely long
	// strings hashed/stored repeatedly).
	if len(username) < 3 || len(username) > 32 {
		writeError(w, http.StatusBadRequest, "username must be between 3 and 32 characters")
		return
	}

	passwordHash, err := auth.HashPassword(req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrPasswordTooShort) {
			writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
			return
		}
		log.Printf("handlers: register: hash password: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Generate a unique user_id, retrying on the rare collision case.
	// This loop is the actual consumer of database.ErrUserIDCollision —
	// the retry strategy that error was designed to support.
	var newUserID string
	var createdID int64
	for attempt := 0; attempt < maxUserIDGenerationAttempts; attempt++ {
		candidateID, err := util.GenerateUserID()
		if err != nil {
			log.Printf("handlers: register: generate user_id: %v", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		createdID, err = h.DB.CreateUser(username, passwordHash, candidateID)
		if err == nil {
			newUserID = candidateID
			break
		}

		if errors.Is(err, database.ErrUsernameTaken) {
			writeError(w, http.StatusConflict, "username is already taken")
			return
		}

		if errors.Is(err, database.ErrUserIDCollision) {
			// Extremely rare. Log it so we'd notice if it ever starts
			// happening at a suspicious frequency (which could indicate
			// the RNG or ID space assumptions are wrong), then retry
			// with a fresh candidate ID.
			log.Printf("handlers: register: user_id collision on attempt %d, retrying", attempt+1)
			continue
		}

		// Any other error is unexpected and not retryable.
		log.Printf("handlers: register: create user: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if newUserID == "" {
		// Exhausted all attempts without success. This should be
		// effectively unreachable given the ID space size, but we
		// handle it explicitly rather than silently falling through.
		log.Printf("handlers: register: exhausted %d user_id generation attempts", maxUserIDGenerationAttempts)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	token, err := h.TokenManager.GenerateToken(newUserID)
	if err != nil {
		// The user row was already created successfully at this point.
		// Failing to issue a token here is a genuine edge case (signing
		// failure) — the account exists, but we can't hand back a
		// session. Logging the internal DB id is useful for manual
		// investigation/support without exposing it externally.
		log.Printf("handlers: register: generate token for new user (db id %d): %v", createdID, err)
		writeError(w, http.StatusInternalServerError, "account created but failed to issue session token")
		return
	}

	writeJSON(w, http.StatusCreated, authResponse{Token: token, UserID: newUserID})
}

// ---------------------------------------------------------------------
// POST /api/login
// ---------------------------------------------------------------------

// Login handles POST /api/login. It looks up the user by username,
// verifies the password, and issues a JWT on success.
//
// Per the anti-enumeration reasoning documented in auth/hash.go and
// auth/middleware.go, "username not found" and "wrong password" are
// deliberately indistinguishable from the client's perspective — both
// produce an identical 401 response.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	username := strings.TrimSpace(req.Username)
	if username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "username and password are required")
		return
	}

	user, err := h.DB.GetUserByUsername(username)
	if err != nil {
		if errors.Is(err, database.ErrUserNotFound) {
			// Deliberately generic — see doc comment above.
			writeError(w, http.StatusUnauthorized, "invalid username or password")
			return
		}
		log.Printf("handlers: login: get user by username: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if err := auth.CheckPassword(req.Password, user.PasswordHash); err != nil {
		// Same generic response as "user not found" above — this is the
		// other half of the anti-enumeration pairing and must stay in
		// sync with it.
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	// The death-cycle lockout (database.RecordDeath) is checked only
	// AFTER the password succeeds, deliberately — checking it earlier
	// would let a wrong-password attempt distinguish "this account
	// exists and is locked" from "this account doesn't exist," which
	// is exactly the enumeration leak the generic 401 above exists to
	// prevent. A correct password revealing lockout state is fine:
	// the real owner is allowed to know they're locked out.
	if user.LockedUntil > time.Now().Unix() {
		unlockAt := time.Unix(user.LockedUntil, 0).UTC()
		writeError(w, http.StatusLocked, fmt.Sprintf(
			"account locked until %s after five deaths — try again later",
			unlockAt.Format(time.RFC3339),
		))
		return
	}

	token, err := h.TokenManager.GenerateToken(user.UserID)
	if err != nil {
		log.Printf("handlers: login: generate token: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, authResponse{Token: token, UserID: user.UserID})
}