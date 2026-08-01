package database

import (
	"fmt"
)

// schema is the full, current `users` table definition. CREATE TABLE
// IF NOT EXISTS makes this safe to run against a brand-new database
// file — it's the only place a fresh install learns the users table
// shape.
//
// death_marks / locked_until back the death-cycle feature: every
// defeat in combat adds one death_marks (see database/users.go's
// RecordDeath, the only place that column is ever written). Reaching
// 5 resets death_marks to 0 and sets locked_until to a 24-hour-out
// Unix timestamp; both login (handlers/auth.go) and every protected
// game route (handlers/game.go's loadUserAndState) refuse to proceed
// while locked_until is still in the future. locked_until = 0 means
// "not locked" — never NULL, so callers can always compare it
// directly against time.Now().Unix() without a null check.
const schema = `
CREATE TABLE IF NOT EXISTS users (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id           TEXT    NOT NULL UNIQUE,
    username          TEXT    NOT NULL UNIQUE,
    password_hash     TEXT    NOT NULL,
    easter_egg_found  BOOLEAN NOT NULL DEFAULT 0,
    level             INTEGER NOT NULL DEFAULT 1,
    story_completed   BOOLEAN NOT NULL DEFAULT 0,
    save_data         TEXT    NOT NULL DEFAULT '',
    death_marks       INTEGER NOT NULL DEFAULT 0,
    locked_until      INTEGER NOT NULL DEFAULT 0
)`

// boardSchema is the tavern community board's table — a separate,
// shared (not per-user) resource, unlike everything else in `users`.
// Notes are never updated or deleted once posted (no UPDATE/DELETE
// path anywhere in database/board.go), so there's no need for a
// separate "edited_at" or soft-delete column — created_at is the only
// timestamp this table will ever need.
const boardSchema = `
CREATE TABLE IF NOT EXISTS board_notes (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    username    TEXT    NOT NULL,
    message     TEXT    NOT NULL,
    created_at  INTEGER NOT NULL,
    pinned      BOOLEAN NOT NULL DEFAULT 0
)`

// boardAddedColumns mirrors addedColumns below, but for board_notes —
// `pinned` was added after board_notes itself first shipped, so
// anyone who already ran that first migration needs the same
// check-then-ALTER upgrade path users' death_marks/locked_until went
// through, not just a CREATE TABLE IF NOT EXISTS that a pre-existing
// table would never re-run.
var boardAddedColumns = []struct {
	name       string
	definition string
}{
	{"pinned", "BOOLEAN NOT NULL DEFAULT 0"},
}

// legacySchema is the Hall of Legacies' table — a separate, shared
// (not per-user) resource, same category as board_notes. Entries are
// never updated or deleted once inserted (see database/legacy.go's
// doc comment), so like board_notes, completed_at is the only
// timestamp this table will ever need.
const legacySchema = `
CREATE TABLE IF NOT EXISTS legacies (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    username        TEXT    NOT NULL,
    character_name  TEXT    NOT NULL,
    class           TEXT    NOT NULL,
    path            TEXT    NOT NULL,
    completed_at    INTEGER NOT NULL
)`

// addedColumns lists every column that must exist on `users` beyond
// what CREATE TABLE IF NOT EXISTS guarantees for a database file that
// predates that column. Each entry is applied via ALTER TABLE ADD
// COLUMN, but only after confirming (via PRAGMA table_info) that the
// column isn't already there — SQLite errors on adding a duplicate
// column, and a database created fresh from `schema` above will
// already have all of these, so this step needs to be a genuine no-op
// in that case, not a hard failure.
var addedColumns = []struct {
	name       string
	definition string
}{
	{"death_marks", "INTEGER NOT NULL DEFAULT 0"},
	{"locked_until", "INTEGER NOT NULL DEFAULT 0"},
}

// Migrate brings the database schema up to date. Called once at
// startup (see cmd/server/main.go), before the router is wired up, so
// no handler ever runs against a table that's missing a column it
// expects.
func Migrate(d *DB) error {
	if _, err := d.Conn.Exec(schema); err != nil {
		return fmt.Errorf("database: migrate: create users table: %w", err)
	}

	if _, err := d.Conn.Exec(boardSchema); err != nil {
		return fmt.Errorf("database: migrate: create board_notes table: %w", err)
	}

	if _, err := d.Conn.Exec(legacySchema); err != nil {
		return fmt.Errorf("database: migrate: create legacies table: %w", err)
	}

	for _, col := range addedColumns {
		exists, err := d.hasColumn("users", col.name)
		if err != nil {
			return fmt.Errorf("database: migrate: check column %q: %w", col.name, err)
		}
		if exists {
			continue
		}

		alter := fmt.Sprintf("ALTER TABLE users ADD COLUMN %s %s", col.name, col.definition)
		if _, err := d.Conn.Exec(alter); err != nil {
			return fmt.Errorf("database: migrate: add column %q: %w", col.name, err)
		}
	}

	for _, col := range boardAddedColumns {
		exists, err := d.hasColumn("board_notes", col.name)
		if err != nil {
			return fmt.Errorf("database: migrate: check board_notes column %q: %w", col.name, err)
		}
		if exists {
			continue
		}

		alter := fmt.Sprintf("ALTER TABLE board_notes ADD COLUMN %s %s", col.name, col.definition)
		if _, err := d.Conn.Exec(alter); err != nil {
			return fmt.Errorf("database: migrate: add board_notes column %q: %w", col.name, err)
		}
	}

	return nil
}

// hasColumn reports whether the given table already has a column with
// the given name, via SQLite's PRAGMA table_info. This is the only
// reliable, driver-agnostic way to make an ALTER TABLE ADD COLUMN
// idempotent — SQLite has no "ADD COLUMN IF NOT EXISTS" syntax that
// mattn/go-sqlite3's bundled SQLite version can be relied on to
// support, so we check first rather than trying the ALTER and
// swallowing a "duplicate column" error string (too fragile, same
// class of problem users.go's containsUsernameConstraint already
// works around for a different error).
func (d *DB) hasColumn(table, column string) (bool, error) {
	rows, err := d.Conn.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, fmt.Errorf("database: pragma table_info(%s): %w", table, err)
	}
	defer rows.Close()

	// PRAGMA table_info columns: cid, name, type, notnull, dflt_value, pk
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dfltValue interface{}
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			return false, fmt.Errorf("database: scan table_info row: %w", err)
		}
		if name == column {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("database: iterate table_info rows: %w", err)
	}

	return false, nil
}
