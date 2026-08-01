package models

import "time"

// BoardNote is one entry on the tavern's community board — a small,
// shared (not per-user) resource, unlike User.SaveData. See
// database/board.go for persistence and handlers/board.go for the
// HTTP surface.
type BoardNote struct {
	Username  string    `json:"username"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
	// Pinned notes cost 4 gold to post (see handlers/board.go's
	// PostNote) and are meant to stand out from the free, cycling
	// board — GetRecentBoardNotes still returns them mixed in with
	// everything else (ordered newest-first like normal), it's the
	// CALLER's job to separate/highlight pinned ones, same
	// "server stores, client decides how to render" split this
	// codebase already draws everywhere else.
	Pinned bool `json:"pinned"`
}
