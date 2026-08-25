package document

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/audit"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/store"
)

func setup(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "e.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(time.Now()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	log := audit.New(db.SQL(), time.UTC)
	return New(db.SQL(), log, dir), dir
}

func TestAddHashesAndStoresFile(t *testing.T) {
	s, dir := setup(t)
	content := []byte("%PDF-1.4 invoice contents")
	d, err := s.Add("miner_invoice", "invoice-nerd.pdf", content, Meta{Description: "NerdOctaxe"}, "york", time.Now())
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	sum := sha256.Sum256(content)
	if d.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("hash mismatch")
	}
	if d.Version != 1 {
		t.Errorf("version = %d, want 1", d.Version)
	}
	if _, err := os.Stat(filepath.Join(dir, "documents", d.SafeFilename)); err != nil {
		t.Errorf("file not written: %v", err)
	}
	if d.MimeType == "" {
		t.Error("mime not detected")
	}
}

func TestReuploadCreatesVersionAndPreservesOriginal(t *testing.T) {
	s, dir := setup(t)
	v1, err := s.Add("miner_invoice", "invoice.pdf", []byte("original v1"), Meta{}, "york", time.Now())
	if err != nil {
		t.Fatalf("v1: %v", err)
	}
	v2, err := s.Add("miner_invoice", "invoice.pdf", []byte("replacement v2"), Meta{}, "york", time.Now())
	if err != nil {
		t.Fatalf("v2: %v", err)
	}
	if v2.Version != 2 {
		t.Errorf("second version = %d, want 2", v2.Version)
	}
	// Both files preserved on disk.
	for _, d := range []Doc{v1, v2} {
		if _, err := os.Stat(filepath.Join(dir, "documents", d.SafeFilename)); err != nil {
			t.Errorf("version %d file missing: %v", d.Version, err)
		}
	}
	all, err := s.Versions("invoice")
	if err != nil || len(all) != 2 {
		t.Fatalf("Versions len = %d, err=%v; want 2", len(all), err)
	}
}

func TestPathTraversalRejected(t *testing.T) {
	s, _ := setup(t)
	for _, name := range []string{"../evil.pdf", "a/b.pdf", "..\\x.pdf", "no\x00null.pdf"} {
		if _, err := s.Add("x", name, []byte("data"), Meta{}, "york", time.Now()); err == nil {
			t.Errorf("unsafe filename accepted: %q", name)
		}
	}
}

func TestExecutableRejected(t *testing.T) {
	s, _ := setup(t)
	elf := append([]byte{0x7f, 'E', 'L', 'F'}, make([]byte, 20)...)
	if _, err := s.Add("x", "payload.pdf", elf, Meta{}, "york", time.Now()); err == nil {
		t.Error("ELF executable accepted despite .pdf name (MIME spoof)")
	}
	if _, err := s.Add("x", "script.txt", []byte("#!/bin/sh\nrm -rf /"), Meta{}, "york", time.Now()); err == nil {
		t.Error("shell script accepted")
	}
}
