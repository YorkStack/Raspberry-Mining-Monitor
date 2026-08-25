package store

import (
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// SchemaVersion returns the highest migration version currently applied.
func (d *DB) SchemaVersion() (int, error) {
	if _, err := d.sql.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		name    TEXT NOT NULL,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return 0, err
	}
	var v *int
	if err := d.sql.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&v); err != nil {
		return 0, err
	}
	if v == nil {
		return 0, nil
	}
	return *v, nil
}

// Migrate applies every embedded migration whose version is newer than the
// current schema version, each in its own transaction. Migrations are ordered
// by their numeric filename prefix (0001_, 0002_, ...) and are never re-run, so
// applying twice is a no-op — existing data, including the live monitor's, is
// preserved.
func (d *DB) Migrate(now time.Time) error {
	current, err := d.SchemaVersion()
	if err != nil {
		return fmt.Errorf("evidence: schema version: %w", err)
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	type mig struct {
		version int
		name    string
	}
	var migs []mig
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		prefix := strings.SplitN(e.Name(), "_", 2)[0]
		version, err := strconv.Atoi(prefix)
		if err != nil {
			return fmt.Errorf("evidence: migration %q has no numeric prefix", e.Name())
		}
		migs = append(migs, mig{version: version, name: e.Name()})
	}
	sort.Slice(migs, func(i, j int) bool { return migs[i].version < migs[j].version })

	for _, m := range migs {
		if m.version <= current {
			continue
		}
		sqlBytes, err := migrationFS.ReadFile("migrations/" + m.name)
		if err != nil {
			return err
		}
		tx, err := d.sql.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(sqlBytes)); err != nil {
			tx.Rollback()
			return fmt.Errorf("evidence: migration %s: %w", m.name, err)
		}
		if _, err := tx.Exec("INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)",
			m.version, m.name, now.UTC().Format(time.RFC3339)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
