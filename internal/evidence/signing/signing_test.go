package signing

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/store"
)

func TestSignVerifyRoundTripAndTamper(t *testing.T) {
	k, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	data := []byte("final-manifest bytes")
	sig := k.Sign(data)
	pub, _ := ParsePublicBase64(k.PublicBase64())
	if !Verify(pub, data, sig) {
		t.Fatal("valid signature did not verify")
	}
	if Verify(pub, []byte("tampered"), sig) {
		t.Fatal("signature verified over tampered data")
	}
}

func TestKeyIsDedicatedEd25519(t *testing.T) {
	k, _ := Generate()
	// A fresh, dedicated key: id prefix marks it as an evidence key, and two
	// generations differ (not a fixed/derived wallet key).
	if k.KeyID()[:3] != "ev-" {
		t.Errorf("key id = %q, want ev- prefix", k.KeyID())
	}
	k2, _ := Generate()
	if k.KeyID() == k2.KeyID() {
		t.Error("two generated keys share an id; key is not independent")
	}
}

func TestSaveLoadPrivate(t *testing.T) {
	k, _ := Generate()
	path := filepath.Join(t.TempDir(), "key")
	if err := k.SavePrivate(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := LoadPrivate(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.KeyID() != k.KeyID() {
		t.Error("loaded key id differs")
	}
}

func TestRotationKeepsOldKeys(t *testing.T) {
	db, _ := store.Open(filepath.Join(t.TempDir(), "e.db"))
	defer db.Close()
	if err := db.Migrate(time.Now()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	s := NewStore(db.SQL())
	k1, _ := Generate()
	k2, _ := Generate()
	s.Register(k1, time.Now())
	s.Register(k2, time.Now())
	// Both public keys remain available for verification.
	if _, err := s.PublicKey(k1.KeyID()); err != nil {
		t.Errorf("old key not retained: %v", err)
	}
	if _, err := s.PublicKey(k2.KeyID()); err != nil {
		t.Errorf("new key missing: %v", err)
	}
	var active string
	db.SQL().QueryRow("SELECT key_id FROM signing_keys WHERE active = 1").Scan(&active)
	if active != k2.KeyID() {
		t.Errorf("active key = %q, want the latest %q", active, k2.KeyID())
	}
}
