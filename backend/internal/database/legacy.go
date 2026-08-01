package database

import (
	"fmt"
	"time"

	"operation-advertise/backend/internal/models"
)

// MaxLegaciesReturned caps how many Hall of Legacies entries
// GetRecentLegacies will ever return in one call — mirrors board.go's
// MaxBoardNotesReturned for the same reason: this is meant to be a
// scrollable "recent history" view (see rpg-frontend's hall.js), not
// paginated indefinitely.
const MaxLegaciesReturned = 20

// InsertLegacy permanently records one completed run: `username`
// finished the game as `characterName` the `class` and chose `path`
// (game.LegacyPath's ID, not its display name — resolved back to a
// name by the caller, same as InsertBoardNote stores raw strings and
// leaves formatting to its caller). No editing/deletion path exists
// anywhere in this codebase, same as board_notes — a legacy, once
// earned, is permanent for the lifetime of the database file.
func (d *DB) InsertLegacy(username, characterName, class, path string) error {
	const query = `
		INSERT INTO legacies (username, character_name, class, path, completed_at)
		VALUES (?, ?, ?, ?, ?)
	`
	stmt, err := d.Conn.Prepare(query)
	if err != nil {
		return fmt.Errorf("database: prepare InsertLegacy: %w", err)
	}
	defer stmt.Close()

	if _, err := stmt.Exec(username, characterName, class, path, time.Now().Unix()); err != nil {
		return fmt.Errorf("database: exec InsertLegacy: %w", err)
	}
	return nil
}

// GetRecentLegacies returns the most recent Hall of Legacies entries,
// newest first, capped at MaxLegaciesReturned — this is what the
// king's chambers shows a player about those who came before them
// (see handlers/game.go's handleLegacyHall).
func (d *DB) GetRecentLegacies() ([]models.Legacy, error) {
	const query = `
		SELECT username, character_name, class, path, completed_at
		FROM legacies
		ORDER BY completed_at DESC, id DESC
		LIMIT ?
	`
	rows, err := d.Conn.Query(query, MaxLegaciesReturned)
	if err != nil {
		return nil, fmt.Errorf("database: query GetRecentLegacies: %w", err)
	}
	defer rows.Close()

	legacies := []models.Legacy{}
	for rows.Next() {
		var l models.Legacy
		var completedAtUnix int64
		if err := rows.Scan(&l.Username, &l.CharacterName, &l.Class, &l.Path, &completedAtUnix); err != nil {
			return nil, fmt.Errorf("database: scan legacy row: %w", err)
		}
		l.CompletedAt = time.Unix(completedAtUnix, 0).UTC()
		legacies = append(legacies, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("database: iterate legacy rows: %w", err)
	}

	return legacies, nil
}
