package database

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

// DB wraps the underlying *sql.DB connection pool.
//
// We wrap rather than passing around *sql.DB directly for two reasons:
//  1. It gives us a stable seam to attach future methods (health checks,
//     transaction helpers, metrics) without touching every call site.
//  2. It keeps the database/sql driver import contained to this package.
//     Nothing outside `internal/database` needs to know we're using
//     SQLite specifically — handlers and auth code depend only on this
//     type, not on database/sql or the sqlite3 driver.
type DB struct {
	Conn *sql.DB
}

// Config holds the settings needed to open a database connection.
// Kept minimal today; will grow if we add connection pool tuning,
// read replicas, etc.
type Config struct {
	// Path is the filesystem path to the SQLite database file,
	// e.g. "storage/users.db".
	Path string
}

// New opens a SQLite database at the given path, verifies connectivity,
// and returns a ready-to-use *DB. It does NOT run migrations — that is
// a distinct, explicit step (see migrate.go) so callers can control
// exactly when schema changes are applied.
func New(cfg Config) (*DB, error) {
	if cfg.Path == "" {
		return nil, fmt.Errorf("database: config.Path must not be empty")
	}

	// foreign_keys=on is set now even though we have a single table today.
	// The RPG phase will very likely introduce related tables (e.g.
	// player_saves, sessions) with foreign keys back to users, and SQLite
	// requires this pragma to be set per-connection. Setting it from day
	// one avoids a silent "why aren't my foreign keys enforced" bug later.
	dsn := fmt.Sprintf("file:%s?_foreign_keys=on", cfg.Path)

	conn, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("database: failed to open sqlite connection: %w", err)
	}

	// SQLite only supports one writer at a time. Capping MaxOpenConns to 1
	// avoids "database is locked" errors under concurrent writes by
	// serializing access at the Go level rather than fighting SQLite's
	// own locking. Reads are still fast since SQLite uses WAL-friendly
	// locking internally for short transactions.
	//
	// This is a deliberate tradeoff for simplicity in Phase 1. If the RPG
	// engine later demands higher write throughput, this is the first
	// place to revisit (e.g. moving to a proper client-server DB).
	conn.SetMaxOpenConns(1)

	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("database: failed to ping sqlite connection: %w", err)
	}

	log.Printf("database: connected to %s", cfg.Path)

	return &DB{Conn: conn}, nil
}

// Close closes the underlying connection pool. Should be deferred by
// the caller in main.go immediately after New() succeeds.
func (d *DB) Close() error {
	if d == nil || d.Conn == nil {
		return nil
	}
	return d.Conn.Close()
}