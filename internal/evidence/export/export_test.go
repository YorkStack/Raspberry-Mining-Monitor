package export

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/store"
)

func seed(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "e.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrate(time.Now()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	now := time.Now().Format(time.RFC3339)
	db.SQL().Exec("INSERT INTO miners (internal_id, created_at) VALUES ('M1', ?)", now)
	db.SQL().Exec(`INSERT INTO miner_versions (miner_id, version, valid_from, model, serial_number, created_at)
		VALUES (1, 1, ?, 'NerdOctaxe', 'SN1', ?)`, now, now)
	return db
}

func TestWriteAndVerifyPackage(t *testing.T) {
	db := seed(t)
	defer db.Close()
	dir := t.TempDir()

	bundle, manifest, err := WriteEvidencePackage(db.SQL(), dir)
	if err != nil {
		t.Fatalf("WriteEvidencePackage: %v", err)
	}
	if bundle == "" {
		t.Fatal("empty bundle hash")
	}
	// Every dataset produced a file listed in the manifest.
	if len(manifest.Files) != len(Datasets) {
		t.Errorf("manifest files = %d, want %d", len(manifest.Files), len(Datasets))
	}
	for _, d := range Datasets {
		if _, err := os.Stat(filepath.Join(dir, "data", d.File)); err != nil {
			t.Errorf("missing %s: %v", d.File, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "evidence-manifest.json")); err != nil {
		t.Errorf("manifest not written: %v", err)
	}

	ok, bad, gotBundle, err := VerifyEvidencePackage(dir)
	if err != nil || !ok {
		t.Fatalf("Verify = ok:%v bad:%q err:%v", ok, bad, err)
	}
	if gotBundle != bundle {
		t.Errorf("bundle hash mismatch on verify")
	}
}

func TestTamperingFailsVerification(t *testing.T) {
	db := seed(t)
	defer db.Close()
	dir := t.TempDir()
	if _, _, err := WriteEvidencePackage(db.SQL(), dir); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Flip one byte of a data file.
	p := filepath.Join(dir, "data", "miners.csv")
	b, _ := os.ReadFile(p)
	b[0] ^= 0xff
	os.WriteFile(p, b, 0o640)

	ok, bad, _, _ := VerifyEvidencePackage(dir)
	if ok {
		t.Fatal("verification passed on a tampered file")
	}
	if bad != "data/miners.csv" {
		t.Errorf("bad file = %q, want data/miners.csv", bad)
	}
}

func TestManifestTamperingDetected(t *testing.T) {
	db := seed(t)
	defer db.Close()
	dir := t.TempDir()
	WriteEvidencePackage(db.SQL(), dir)
	mp := filepath.Join(dir, "evidence-manifest.json")
	b, _ := os.ReadFile(mp)
	os.WriteFile(mp, append(b, ' '), 0o640) // whitespace change
	ok, bad, _, _ := VerifyEvidencePackage(dir)
	if ok {
		t.Fatal("verification passed on a tampered manifest")
	}
	if bad != "evidence-manifest.json" {
		t.Errorf("bad = %q, want evidence-manifest.json", bad)
	}
}
