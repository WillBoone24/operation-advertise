package models

// User represents a registered account in the system.
//
// This struct serves two roles today:
//   1. It is the in-memory representation of a row in the `users` table.
//   2. It is (partially) serialized back to the frontend via JSON.
//
// IMPORTANT: PasswordHash must NEVER be serialized to JSON. It is tagged
// with `json:"-"` to guarantee it cannot leak through an API response,
// even if a handler accidentally does something like `json.Marshal(user)`
// on the full struct instead of a sanitized DTO.
//
// SaveData is a placeholder for future RPG functionality. It stores
// opaque, serialized JSON representing a player's save state. The backend
// treats it as an opaque string today — it does not parse or validate
// its contents. That parsing will live in a future `internal/game`
// package, not here. This file must remain ignorant of game logic.
type User struct {
	ID              int64  `json:"-"`                 // internal DB primary key, never exposed
	UserID          string `json:"user_id"`            // public-facing random 8-char ID
	Username        string `json:"username"`
	PasswordHash    string `json:"-"`                  // never serialize
	EasterEggFound  bool   `json:"easter_egg_found"`
	Level           int    `json:"level"`
	StoryCompleted  bool   `json:"story_completed"`
	SaveData        string `json:"-"`                  // opaque for now, excluded until RPG phase

	// DeathMarks and LockedUntil back the 5-mark death cycle (see
	// database.RecordDeath, the only place these are written).
	// Neither is serialized directly — DeathMarks is surfaced to the
	// frontend via the game-state response (handlers/game.go), which
	// stays fresher across a session than this profile snapshot
	// would; LockedUntil is a raw Unix timestamp used only for the
	// login/Me lockout check and is never handed to the client as-is.
	DeathMarks  int   `json:"-"`
	LockedUntil int64 `json:"-"` // unix seconds; 0 = not currently locked
}

// PublicProfile returns a sanitized view of the user suitable for
// returning from GET /api/me. Keeping this as an explicit method (rather
// than relying on struct json tags alone) makes it obvious at the call
// site in handlers/profile.go exactly what data is being sent back,
// and gives us one place to extend later (e.g. adding "created_at").
type PublicProfile struct {
	UserID         string `json:"user_id"`
	Username       string `json:"username"`
	EasterEggFound bool   `json:"easter_egg_found"`
	Level          int    `json:"level"`
	StoryCompleted bool   `json:"story_completed"`
}

func (u *User) ToPublicProfile() PublicProfile {
	return PublicProfile{
		UserID:         u.UserID,
		Username:       u.Username,
		EasterEggFound: u.EasterEggFound,
		Level:          u.Level,
		StoryCompleted: u.StoryCompleted,
	}
}