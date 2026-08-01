package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"operation-advertise/backend/internal/database"
)

// maxBoardNoteLength caps a single note's length. Generous enough for
// a real message, small enough that the board (and board.js's cycling
// display) can't be turned into a place to paste arbitrary walls of
// text.
const maxBoardNoteLength = 200

// pinnedNoteGoldCost is what a permanent, pinned board note costs —
// see game/tavern.go's doc comment convention for why this lives here
// rather than in game/tavern.go itself: it's a board_notes concern,
// not a SaveState/Inventory purchase the way TavernPotions/ScrollPrices
// are, even though it's still gold leaving state.Gold.
const pinnedNoteGoldCost = 4

// BoardHandler bundles the dependencies needed to serve the tavern
// community board routes. Embeds *GameHandler (rather than duplicating
// its auth/state-loading boilerplate) purely to reuse loadUserAndState
// — posting/listing notes both need "authenticated user_id -> user row
// -> parsed SaveState, with AtTavern true" before doing anything else,
// exactly the same precondition every game/action tavern_* branch
// enforces.
type BoardHandler struct {
	*GameHandler
}

// NewBoardHandler constructs a BoardHandler.
func NewBoardHandler(db *database.DB) *BoardHandler {
	return &BoardHandler{GameHandler: NewGameHandler(db)}
}

// postNoteRequest is the expected JSON body for POST /api/game/board.
type postNoteRequest struct {
	Message string `json:"message"`
	// Pinned requests the paid, permanent variant (pinnedNoteGoldCost
	// gold) instead of the default free note — see PostNote.
	Pinned bool `json:"pinned,omitempty"`
}

// boardNoteResponse is the public shape of one board note.
type boardNoteResponse struct {
	Username  string `json:"username"`
	Message   string `json:"message"`
	CreatedAt string `json:"created_at"` // RFC3339
	Pinned    bool   `json:"pinned"`
}

// PostNote handles POST /api/game/board: leaves a note on the
// community board. Requires the poster to currently be AtTavern — see
// SaveState.AtTavern's doc comment on why that's the location the
// community board lives in, mirroring every tavern_* game action's
// same gate. A pinned note additionally costs pinnedNoteGoldCost gold
// (deducted from the SAME SaveState this request already loaded via
// loadUserAndState, then saved back — this is the one place board.go
// actually mutates a character rather than just the shared board).
func (h *BoardHandler) PostNote(w http.ResponseWriter, r *http.Request) {
	userID, state, _, ok := h.loadUserAndState(w, r)
	if !ok {
		return
	}
	if !state.AtTavern {
		writeError(w, http.StatusConflict, "you need to be in the tavern to leave a note")
		return
	}

	var req postNoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	message := strings.TrimSpace(req.Message)
	if message == "" {
		writeError(w, http.StatusBadRequest, "message is required")
		return
	}
	if len(message) > maxBoardNoteLength {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("message must be %d characters or fewer", maxBoardNoteLength))
		return
	}

	if req.Pinned {
		if state.Gold < pinnedNoteGoldCost {
			writeError(w, http.StatusConflict, fmt.Sprintf("not enough gold to pin a note (need %d, have %d)", pinnedNoteGoldCost, state.Gold))
			return
		}
		state.Gold -= pinnedNoteGoldCost
		if err := h.saveState(userID, &state); err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
	}

	user, err := h.DB.GetUserByUserID(userID)
	if err != nil {
		log.Printf("handlers: board: post note: get user %s: %v", userID, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if err := h.DB.InsertBoardNote(user.Username, message, req.Pinned); err != nil {
		log.Printf("handlers: board: post note: insert for %s: %v", userID, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "posted"})
}

// ListNotes handles GET /api/game/board: reads the community board.
// Also gated on AtTavern — the board is a tavern fixture, not
// something visible from mid-dungeon, same as every other tavern_*
// service. Returns pinned and unpinned notes as two separate arrays —
// see database.GetPinnedBoardNotes's doc comment on why pinned notes
// need their own uncapped query rather than trusting them to always
// still be within GetRecentBoardNotes's 50-note window.
func (h *BoardHandler) ListNotes(w http.ResponseWriter, r *http.Request) {
	_, state, _, ok := h.loadUserAndState(w, r)
	if !ok {
		return
	}
	if !state.AtTavern {
		writeError(w, http.StatusConflict, "you need to be in the tavern to read the board")
		return
	}

	pinned, err := h.DB.GetPinnedBoardNotes()
	if err != nil {
		log.Printf("handlers: board: list pinned notes: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	recent, err := h.DB.GetRecentBoardNotes()
	if err != nil {
		log.Printf("handlers: board: list notes: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	pinnedResp := make([]boardNoteResponse, 0, len(pinned))
	for _, n := range pinned {
		pinnedResp = append(pinnedResp, boardNoteResponse{
			Username:  n.Username,
			Message:   n.Message,
			CreatedAt: n.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			Pinned:    true,
		})
	}

	// recent excludes anything already surfaced in pinnedResp, so the
	// frontend never has to de-duplicate a note that's both "recent"
	// and "pinned" — it just always shows everything in `pinned`, and
	// cycles through everything in `notes`.
	notesResp := make([]boardNoteResponse, 0, len(recent))
	for _, n := range recent {
		if n.Pinned {
			continue
		}
		notesResp = append(notesResp, boardNoteResponse{
			Username:  n.Username,
			Message:   n.Message,
			CreatedAt: n.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			Pinned:    false,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"pinned": pinnedResp, "notes": notesResp})
}
