package database

import (
	"fmt"
	"time"

	"operation-advertise/backend/internal/models"
)

// MaxBoardNotesReturned caps how many notes GetRecentBoardNotes will
// ever return in one call. The board is meant to be cycled through
// (see rpg-frontend's board.js), not paginated indefinitely — a fixed,
// generous cap keeps one query cheap forever without needing real
// pagination for what's a flavor feature, not a message archive.
const MaxBoardNotesReturned = 50

// InsertBoardNote adds one community board entry: `username` posted
// `message` at the current time. pinned marks it as a paid, permanent
// note (see handlers/board.go's PostNote) — GetPinnedBoardNotes below
// is how those get pulled back out separately from the free, cycling
// ones GetRecentBoardNotes returns. No editing/deletion path exists
// anywhere in this codebase — a note, once posted, is permanent for
// the lifetime of the database file either way, same as the game's
// design intent for a shared, append-only board.
func (d *DB) InsertBoardNote(username, message string, pinned bool) error {
	const query = `
		INSERT INTO board_notes (username, message, created_at, pinned)
		VALUES (?, ?, ?, ?)
	`
	stmt, err := d.Conn.Prepare(query)
	if err != nil {
		return fmt.Errorf("database: prepare InsertBoardNote: %w", err)
	}
	defer stmt.Close()

	if _, err := stmt.Exec(username, message, time.Now().Unix(), pinned); err != nil {
		return fmt.Errorf("database: exec InsertBoardNote: %w", err)
	}
	return nil
}

// GetRecentBoardNotes returns the most recent board notes, newest
// first, capped at MaxBoardNotesReturned regardless of how many exist
// in total. Includes BOTH pinned and unpinned notes — callers that
// want ONLY the free-form cycling ones should filter on !n.Pinned, or
// just use GetPinnedBoardNotes separately for the pinned set (which
// this call does NOT exclude, to keep this one query simple and let
// the caller decide how to split the two).
func (d *DB) GetRecentBoardNotes() ([]models.BoardNote, error) {
	const query = `
		SELECT username, message, created_at, pinned
		FROM board_notes
		ORDER BY created_at DESC, id DESC
		LIMIT ?
	`
	rows, err := d.Conn.Query(query, MaxBoardNotesReturned)
	if err != nil {
		return nil, fmt.Errorf("database: query GetRecentBoardNotes: %w", err)
	}
	defer rows.Close()

	notes := []models.BoardNote{}
	for rows.Next() {
		var n models.BoardNote
		var createdAtUnix int64
		if err := rows.Scan(&n.Username, &n.Message, &createdAtUnix, &n.Pinned); err != nil {
			return nil, fmt.Errorf("database: scan board note row: %w", err)
		}
		n.CreatedAt = time.Unix(createdAtUnix, 0).UTC()
		notes = append(notes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("database: iterate board note rows: %w", err)
	}

	return notes, nil
}

// GetPinnedBoardNotes returns EVERY pinned note, newest first, with no
// cap — unlike GetRecentBoardNotes's MaxBoardNotesReturned limit,
// pinned notes cost real gold to post (see handlers/board.go's
// PostNote), so none of them should ever silently fall off a limited
// query the way an old free note eventually would.
func (d *DB) GetPinnedBoardNotes() ([]models.BoardNote, error) {
	const query = `
		SELECT username, message, created_at, pinned
		FROM board_notes
		WHERE pinned = 1
		ORDER BY created_at DESC, id DESC
	`
	rows, err := d.Conn.Query(query)
	if err != nil {
		return nil, fmt.Errorf("database: query GetPinnedBoardNotes: %w", err)
	}
	defer rows.Close()

	notes := []models.BoardNote{}
	for rows.Next() {
		var n models.BoardNote
		var createdAtUnix int64
		if err := rows.Scan(&n.Username, &n.Message, &createdAtUnix, &n.Pinned); err != nil {
			return nil, fmt.Errorf("database: scan pinned board note row: %w", err)
		}
		n.CreatedAt = time.Unix(createdAtUnix, 0).UTC()
		notes = append(notes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("database: iterate pinned board note rows: %w", err)
	}

	return notes, nil
}
