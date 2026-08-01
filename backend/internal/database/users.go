package database

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"operation-advertise/backend/internal/models"
)

// deathMarksBeforeLockout and deathLockoutDuration define the death
// cycle: every combat defeat adds one mark (see RecordDeath below);
// on the mark that brings the count to deathMarksBeforeLockout, the
// count resets to 0 and the account is locked out for
// deathLockoutDuration. This is the single place those two numbers
// are defined — handlers/game.go and handlers/auth.go both react to
// what RecordDeath returns rather than re-deriving the threshold
// themselves.
const (
	deathMarksBeforeLockout = 5
	deathLockoutDuration    = 24 * time.Hour
)

// Sentinel errors returned by this package. Handlers check against these
// with errors.Is rather than inspecting sql.ErrNoRows or SQLite-specific
// error strings directly. This keeps handlers decoupled from the fact
// that SQLite is the underlying store at all.
var (
	// ErrUserNotFound is returned when a lookup finds no matching row.
	ErrUserNotFound = errors.New("database: user not found")

	// ErrUsernameTaken is returned when CreateUser violates the
	// username UNIQUE constraint.
	ErrUsernameTaken = errors.New("database: username already taken")

	// ErrUserIDCollision is returned when CreateUser violates the
	// user_id UNIQUE constraint. This should be exceedingly rare (it
	// means the random ID generator produced a duplicate) but callers
	// need a way to detect it and retry with a freshly generated ID.
	ErrUserIDCollision = errors.New("database: user_id collision")
)

// CreateUser inserts a new user row. The caller is responsible for
// having already hashed the password (see internal/auth/hash.go) and
// generated the random user_id (see internal/utils/random.go) — this
// method does no hashing or generation itself, keeping it a pure
// persistence operation.
//
// On success, it returns the newly created user's internal ID (the
// auto-increment primary key), useful for logging but not for exposing
// externally.
func (d *DB) CreateUser(username, passwordHash, userID string) (int64, error) {
	const query = `
		INSERT INTO users (user_id, username, password_hash, easter_egg_found, level, story_completed, save_data)
		VALUES (?, ?, ?, 0, 1, 0, '')
	`

	stmt, err := d.Conn.Prepare(query)
	if err != nil {
		return 0, fmt.Errorf("database: prepare CreateUser: %w", err)
	}
	defer stmt.Close()

	result, err := stmt.Exec(userID, username, passwordHash)
	if err != nil {
		// SQLite returns a generic "UNIQUE constraint failed: users.<col>"
		// error string — there's no typed constraint-violation error in
		// mattn/go-sqlite3 the way pq or pgx provide for Postgres. We
		// inspect the message to distinguish which UNIQUE constraint
		// fired, so callers can react appropriately (e.g. retry ID
		// generation vs. tell the user to pick a different username).
		msg := err.Error()
		switch {
		case containsUsernameConstraint(msg):
			return 0, ErrUsernameTaken
		case containsUserIDConstraint(msg):
			return 0, ErrUserIDCollision
		default:
			return 0, fmt.Errorf("database: exec CreateUser: %w", err)
		}
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("database: retrieve last insert id: %w", err)
	}

	return id, nil
}

// GetUserByUsername looks up a full user row by username. Used by the
// login handler to fetch the password hash for comparison.
func (d *DB) GetUserByUsername(username string) (*models.User, error) {
	const query = `
		SELECT id, user_id, username, password_hash, easter_egg_found, level, story_completed, save_data, death_marks, locked_until
		FROM users
		WHERE username = ?
	`
	return d.queryUser(query, username)
}

// GetUserByUserID looks up a full user row by the public-facing user_id.
// This is the primary lookup used by authenticated routes (GET /api/me,
// POST /api/easteregg) once a JWT has been validated and its subject
// claim (the user_id) extracted.
func (d *DB) GetUserByUserID(userID string) (*models.User, error) {
	const query = `
		SELECT id, user_id, username, password_hash, easter_egg_found, level, story_completed, save_data, death_marks, locked_until
		FROM users
		WHERE user_id = ?
	`
	return d.queryUser(query, userID)
}

// queryUser is a shared helper for the two lookup methods above. Both
// need identical scan logic; centralizing it avoids the two queries
// drifting out of sync if a column is added later.
func (d *DB) queryUser(query, arg string) (*models.User, error) {
	stmt, err := d.Conn.Prepare(query)
	if err != nil {
		return nil, fmt.Errorf("database: prepare queryUser: %w", err)
	}
	defer stmt.Close()

	row := stmt.QueryRow(arg)

	var u models.User
	err = row.Scan(
		&u.ID,
		&u.UserID,
		&u.Username,
		&u.PasswordHash,
		&u.EasterEggFound,
		&u.Level,
		&u.StoryCompleted,
		&u.SaveData,
		&u.DeathMarks,
		&u.LockedUntil,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("database: scan user row: %w", err)
	}

	return &u, nil
}

// SetEasterEggFound marks the easter egg as found for a given user_id.
// It's a targeted, single-column update rather than a generic "UpdateUser"
// method — deliberately narrow so the easter egg handler can't
// accidentally overwrite unrelated fields (like save_data) by passing a
// half-populated struct.
//
// Idempotent by design: calling this on a user who already found the
// egg is a harmless no-op from the caller's perspective (still returns
// nil error), since the handler layer is responsible for deciding
// whether to treat "already found" as notable.
func (d *DB) SetEasterEggFound(userID string) error {
	const query = `
		UPDATE users
		SET easter_egg_found = 1
		WHERE user_id = ?
	`

	stmt, err := d.Conn.Prepare(query)
	if err != nil {
		return fmt.Errorf("database: prepare SetEasterEggFound: %w", err)
	}
	defer stmt.Close()

	result, err := stmt.Exec(userID)
	if err != nil {
		return fmt.Errorf("database: exec SetEasterEggFound: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("database: rows affected SetEasterEggFound: %w", err)
	}
	if rows == 0 {
		return ErrUserNotFound
	}

	return nil
}

// containsUsernameConstraint and containsUserIDConstraint inspect a
// SQLite driver error message to determine which UNIQUE constraint was
// violated. This string-matching approach is fragile in the abstract,
// but mattn/go-sqlite3's error messages are stable and well-established
// ("UNIQUE constraint failed: <table>.<column>"), so this is a pragmatic
// and commonly-used pattern for this driver rather than a hack. If we
// ever swap drivers, these two functions are the only place that needs
// to change.
func containsUsernameConstraint(msg string) bool {
	return containsAll(msg, "UNIQUE", "users.username")
}

func containsUserIDConstraint(msg string) bool {
	return containsAll(msg, "UNIQUE", "users.user_id")
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !stringContains(s, sub) {
			return false
		}
	}
	return true
}

// stringContains is a tiny local wrapper around strings.Contains.
// Pulled out as its own function only to keep the constraint-checking
// helpers above readable as a small composable chain; feel free to
// inline strings.Contains directly if you'd prefer one less indirection.
func stringContains(s, substr string) bool {
	return len(substr) == 0 || (len(s) >= len(substr) && indexOf(s, substr) >= 0)
}

func indexOf(s, substr string) int {
	n := len(s)
	m := len(substr)
	for i := 0; i+m <= n; i++ {
		if s[i:i+m] == substr {
			return i
		}
	}
	return -1
}


// UpdateGameState persists a character's save data alongside the two
// coarse-grained public fields (level, story_completed) that mirror
// it. Bundled into ONE update — never written independently, or
// /api/me could report stale progress.
func (d *DB) UpdateGameState(userID, saveData string, level int, storyCompleted bool) error {
	const query = `
		UPDATE users
		SET save_data = ?, level = ?, story_completed = ?
		WHERE user_id = ?
	`
	stmt, err := d.Conn.Prepare(query)
	if err != nil {
		return fmt.Errorf("database: prepare UpdateGameState: %w", err)
	}
	defer stmt.Close()

	storyCompletedInt := 0
	if storyCompleted {
		storyCompletedInt = 1
	}

	result, err := stmt.Exec(saveData, level, storyCompletedInt, userID)
	if err != nil {
		return fmt.Errorf("database: exec UpdateGameState: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("database: rows affected UpdateGameState: %w", err)
	}
	if rows == 0 {
		return ErrUserNotFound
	}
	return nil
}

// ClearDeathMarks resets death_marks to 0 for the given user, leaving
// locked_until untouched. Backs the tavern's "cleanse death marks for
// gold" purchase (see game.ClearDeathMarksItemID and
// handlers/game.go's handleTavernBuy, the only caller) — a player can
// only ever reach the tavern with locked_until already in the past
// (loadUserAndState blocks a locked account before any handler runs),
// so there is never a live lockout for this to need to also clear.
func (d *DB) ClearDeathMarks(userID string) error {
	const query = `UPDATE users SET death_marks = 0 WHERE user_id = ?`

	stmt, err := d.Conn.Prepare(query)
	if err != nil {
		return fmt.Errorf("database: prepare ClearDeathMarks: %w", err)
	}
	defer stmt.Close()

	result, err := stmt.Exec(userID)
	if err != nil {
		return fmt.Errorf("database: exec ClearDeathMarks: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("database: rows affected ClearDeathMarks: %w", err)
	}
	if rows == 0 {
		return ErrUserNotFound
	}
	return nil
}

// RecordDeath is called exactly once per combat defeat (see
// handlers/game.go's handleAttack) and implements the whole death
// cycle in one atomic read-modify-write: increment death_marks, and
// if that increment just reached deathMarksBeforeLockout, reset the
// count to 0 and set locked_until to deathLockoutDuration from now.
//
// Wrapped in a transaction rather than a bare UPDATE ... SET
// death_marks = death_marks + 1 because the "did we just cross the
// threshold" decision needs to see the post-increment value before
// deciding whether to also set locked_until — that's two dependent
// writes derived from one read, which needs the read and both writes
// to be atomic against a concurrent death for the same user (SQLite's
// single-writer connection pool already serializes this in practice,
// per database.go's MaxOpenConns(1) comment, but the transaction
// makes that guarantee explicit rather than relying on it silently).
//
// Returns the resulting death_marks count and locked_until (unix
// seconds, 0 if not currently locked) after the update, so callers
// never need a second query to learn what state the cycle landed in.
func (d *DB) RecordDeath(userID string) (deathMarks int, lockedUntil int64, err error) {
	tx, err := d.Conn.Begin()
	if err != nil {
		return 0, 0, fmt.Errorf("database: record death: begin tx: %w", err)
	}
	defer tx.Rollback() // no-op once Commit succeeds below

	var marks int
	row := tx.QueryRow(`SELECT death_marks FROM users WHERE user_id = ?`, userID)
	if err := row.Scan(&marks); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, ErrUserNotFound
		}
		return 0, 0, fmt.Errorf("database: record death: select death_marks: %w", err)
	}

	marks++
	var newLockedUntil int64
	if marks >= deathMarksBeforeLockout {
		marks = 0
		newLockedUntil = time.Now().Add(deathLockoutDuration).Unix()
	}

	result, err := tx.Exec(
		`UPDATE users SET death_marks = ?, locked_until = ? WHERE user_id = ?`,
		marks, newLockedUntil, userID,
	)
	if err != nil {
		return 0, 0, fmt.Errorf("database: record death: update: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, 0, fmt.Errorf("database: record death: rows affected: %w", err)
	}
	if rows == 0 {
		return 0, 0, ErrUserNotFound
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("database: record death: commit: %w", err)
	}

	return marks, newLockedUntil, nil
}