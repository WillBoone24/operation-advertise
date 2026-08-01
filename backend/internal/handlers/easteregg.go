package handlers

import (
	"errors"
	"log"
	"net/http"

	"operation-advertise/backend/internal/auth"
	"operation-advertise/backend/internal/database"
)

// EasterEggHandler bundles the dependencies needed to serve the easter
// egg route. Split into its own handler struct — rather than folding
// this single method onto ProfileHandler — for the same reason
// ProfileHandler is split from AuthHandler: this route touches a
// single, narrow slice of state (easter_egg_found) and has no business
// reason to hold a *database.DB with broader intent than that. As this
// grows (e.g. multiple easter eggs, a leaderboard of who found it
// first), it has its own home to grow into without crowding
// ProfileHandler.
type EasterEggHandler struct {
	DB *database.DB
}

// NewEasterEggHandler constructs an EasterEggHandler.
func NewEasterEggHandler(db *database.DB) *EasterEggHandler {
	return &EasterEggHandler{DB: db}
}

// ---------------------------------------------------------------------
// POST /api/easteregg
// ---------------------------------------------------------------------

// easterEggResponse is the JSON body returned by POST /api/easteregg.
//
// FirstDiscovery distinguishes "you just found it for the first time"
// from "you already found this earlier" — both return 200 with
// EasterEggFound: true (the persisted fact is the same either way),
// but the frontend needs FirstDiscovery to decide whether to play a
// one-time reveal/celebration animation versus a quieter
// acknowledgment on repeat calls. Without this field the handler would
// have no way to tell the client which case occurred, since
// database.SetEasterEggFound is intentionally idempotent and silent
// about it.
type easterEggResponse struct {
	EasterEggFound bool `json:"easter_egg_found"`
	FirstDiscovery bool `json:"first_discovery"`
}

// Found handles POST /api/easteregg. It must be mounted behind
// auth.Middleware, same as Me in profile.go — it trusts a validated
// user_id is already present in the request context.
//
// Trust boundary, stated explicitly: this endpoint takes the caller's
// word for it that they found the egg. There is no server-side
// verification that the request actually originated from wherever the
// egg is hidden in the RPG frontend (e.g. no secret code in the
// request body checked against a server-held value). For a hidden
// terminal MMORPG easter egg on a portfolio project, "anyone who can
// authenticate and knows this endpoint exists can mark it found" is an
// acceptable risk — the egg is a fun flourish, not a security boundary
// or a scored achievement with stakes. If that changes later (e.g. it
// gates something meaningful), the fix is to add a required "code"
// field to the request body, checked against a constant the frontend
// only reveals once the player actually finds the hidden trigger. That
// is a deliberate non-goal for now, not an oversight.
func (h *EasterEggHandler) Found(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		// Unreachable in practice under correct routing — see the
		// identical comment in profile.go's Me handler for why we
		// still check explicitly rather than trusting the invariant.
		log.Printf("handlers: easteregg: no user_id in context (route mounted outside auth middleware?)")
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Look up the user's current state before writing, purely so we
	// can compute FirstDiscovery below. This costs one extra query
	// compared to blindly calling SetEasterEggFound, but this route is
	// not a hot path — trading a cheap read for a client-friendly
	// response field is a easy call here.
	user, err := h.DB.GetUserByUserID(userID)
	if err != nil {
		if errors.Is(err, database.ErrUserNotFound) {
			// Valid token for a user_id no longer in the database.
			// Same reasoning as profile.go: a 401, not a 404, since
			// this isn't the client's fault and keeps the response
			// shape consistent with other auth-failure cases.
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		log.Printf("handlers: easteregg: get user by user_id: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	alreadyFound := user.EasterEggFound

	if !alreadyFound {
		if err := h.DB.SetEasterEggFound(userID); err != nil {
			if errors.Is(err, database.ErrUserNotFound) {
				// Vanishingly unlikely race (deleted between the read
				// above and this write) but handled the same way as
				// any other "no longer exists" case rather than
				// surfaced as a 500.
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			log.Printf("handlers: easteregg: set easter egg found: %v", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
	}

	writeJSON(w, http.StatusOK, easterEggResponse{
		EasterEggFound: true,
		FirstDiscovery: !alreadyFound,
	})
}
