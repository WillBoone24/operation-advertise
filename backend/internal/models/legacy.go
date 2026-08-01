package models

import "time"

// Legacy is one permanent Hall of Legacies entry — the record left
// behind by a character who finished the FULL run (the dungeon AND
// the Journey) and chose their path before the king. Unlike
// User.SaveData, this is a small, shared (not per-user), append-only
// resource — same category as BoardNote, and modeled after it for the
// same reason: nothing here is ever edited or deleted once written.
//
// See database/legacy.go for persistence and handlers/game.go's
// handleChoosePath, the only place one of these ever gets created.
type Legacy struct {
	Username      string `json:"username"`
	CharacterName string `json:"character_name"`
	Class         string `json:"class"`
	// Path is the game.LegacyPath ID ("lords" | "commons" | "heroes"),
	// not the display name — handlers/game.go resolves it to a name
	// via game.GetLegacyPath before it ever reaches the wire, same
	// "server stores the ID, resolves the display fields itself"
	// convention as Familiar/SecondFamiliar in gameStateResponse.
	Path        string    `json:"path"`
	CompletedAt time.Time `json:"completed_at"`
}
