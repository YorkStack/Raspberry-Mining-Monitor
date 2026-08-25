// Package store opens the evidence SQLite database and runs migrations.
//
// The evidence subsystem uses a real relational store, unlike the live monitor,
// because the tax/evidence records are versioned, append-only and relational.
// SQLite is pure Go (modernc.org/sqlite, no cgo), so the binary stays a single
// static file and cross-compiles for the Pi.
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver
)

// DB wraps the sql.DB with the evidence schema.
type DB struct {
	sql *sql.DB
}

// Open opens (creating if needed) the database at path and applies the
// integrity-critical PRAGMAs: foreign keys on, WAL journalling for crash safety
// and fewer SD-card writes, and a busy timeout so concurrent writers wait
// rather than fail.
func Open(path string) (*DB, error) {
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("evidence: open %s: %w", path, err)
	}
	// One writer at a time keeps SQLite happy and the WAL simple on the Pi.
	sqlDB.SetMaxOpenConns(1)

	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := sqlDB.Exec(pragma); err != nil {
			sqlDB.Close()
			return nil, fmt.Errorf("evidence: %q: %w", pragma, err)
		}
	}
	return &DB{sql: sqlDB}, nil
}

// SQL exposes the underlying handle for the domain packages.
func (d *DB) SQL() *sql.DB { return d.sql }

// Close closes the database.
func (d *DB) Close() error { return d.sql.Close() }
