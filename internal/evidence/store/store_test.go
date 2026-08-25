package store

import (
	"path/filepath"
	"testing"
)

func TestOpenAppliesPragmasAndWorks(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "evidence.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	var fk int
	if err := db.SQL().QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("query foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, want 1", fk)
	}

	var mode string
	if err := db.SQL().QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}

	if _, err := db.SQL().Exec("CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)"); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.SQL().Exec("INSERT INTO t (v) VALUES ('hello')"); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var v string
	if err := db.SQL().QueryRow("SELECT v FROM t WHERE id = 1").Scan(&v); err != nil {
		t.Fatalf("select: %v", err)
	}
	if v != "hello" {
		t.Errorf("v = %q, want hello", v)
	}
}
