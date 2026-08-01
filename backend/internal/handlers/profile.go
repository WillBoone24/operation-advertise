package handlers

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"operation-advertise/backend/internal/auth"
	"operation-advertise/backend/internal/database"
)

// ProfileHandler bundles the dependencies needed to serve profile
// routes. Kept as its own struct (rather than folding into
// AuthHandler) because profile routes are conceptually distinct from
// registration/login: they require an already-established identity
// (via auth.Middleware) rather than establishing one. Splitting them
// also means ProfileHandler never needs a *auth.TokenManager — it has
// no business issuing tokens, only reading who the request already
// claims to be.
type ProfileHandler struct {
	DB *database.DB
}

// NewProfileHandler constructs a ProfileHandler.
func NewProfileHandler(db *database.DB) *ProfileHandler {
	return &ProfileHandler{DB: db}
}

// ---------------------------------------------------------------------
// GET /api/me
// ---------------------------------------------------------------------

// Me handles GET /api/me. It must be mounted behind auth.Middleware —
// it trusts that a valid user_id is already present in the request
// context and does no token parsing itself (see the doc comment on
// auth.UserIDFromContext: handlers must never parse the Authorization
// header directly).
//
// Returns the requesting user's own sanitized profile. There is no
// path parameter for "which user" — by design, Phase 1 has no concept
// of viewing another user's profile, only your own. That's a
// deliberate scope boundary, not an oversight: a public profile-lookup
// endpoint is a different feature with its own privacy considerations
// (e.g. should level/story progress be visible to strangers?) that
// hasn't been decided yet.
func (h *ProfileHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		// This should be unreachable in practice — auth.Middleware
		// guarantees a user_id is set before next.ServeHTTP is called.
		// We still check explicitly rather than trusting that invariant
		// silently, because a future refactor of the routing in main.go
		// (e.g. accidentally mounting this handler outside the
		// protected subrouter) would otherwise fail in a confusing way
		// — a nil-pointer panic or a lookup against an empty string —
		// instead of a clear, logged 401.
		log.Printf("handlers: me: no user_id in context (route mounted outside auth middleware?)")
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	user, err := h.DB.GetUserByUserID(userID)
	if err != nil {
		if errors.Is(err, database.ErrUserNotFound) {
			// A valid, unexpired JWT for a user_id that no longer exists
			// in the database. This can legitimately happen (e.g. an
			// account deleted after the token was issued, once account
			// deletion exists) — it is not the client's fault, so this
			// stays a 401 "your session is no longer valid" rather than
			// a 404, keeping the response shape consistent with other
			// auth-failure cases in this API.
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		log.Printf("handlers: me: get user by user_id: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// A JWT issued before a death-cycle lockout kicked in (see
	// database.RecordDeath) is still cryptographically valid for up
	// to 24h (auth/jwt.go's tokenLifetime) — the token itself can't
	// be revoked, so this check is what actually enforces "forced
	// logout" for a session that was already open when the 5th death
	// landed. api/client.js treats this 423 the same as a 401 and
	// clears the stored session.
	if user.LockedUntil > time.Now().Unix() {
		unlockAt := time.Unix(user.LockedUntil, 0).UTC()
		writeError(w, http.StatusLocked, fmt.Sprintf(
			"account locked until %s after five deaths",
			unlockAt.Format(time.RFC3339),
		))
		return
	}

	writeJSON(w, http.StatusOK, user.ToPublicProfile())
}
