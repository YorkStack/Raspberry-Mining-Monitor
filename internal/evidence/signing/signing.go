// Package signing provides detached Ed25519 signatures for evidence manifests.
//
// The signing key is dedicated to evidence: it is generated here and is never a
// Bitcoin wallet key, seed, exchange API key or the monitor's auth secret. The
// private key lives in a single protected file (0600); the public keys are
// registered in the database so old keys remain available for verification
// after a rotation.
//
// Local signing provides tamper evidence. It is not automatically equivalent to
// a qualified electronic signature; that requires a separate legal process.
package signing

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"time"
)

// Key is an evidence-signing key pair.
type Key struct {
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey
}

// deriveKeyID is a short stable id for a public key.
func deriveKeyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return "ev-" + hex.EncodeToString(sum[:8])
}

// Generate creates a fresh dedicated signing key.
func Generate() (*Key, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Key{priv: priv, pub: pub}, nil
}

// KeyID returns the key's stable identifier.
func (k *Key) KeyID() string { return deriveKeyID(k.pub) }

// PublicBase64 returns the base64 public key.
func (k *Key) PublicBase64() string { return base64.StdEncoding.EncodeToString(k.pub) }

// Public returns the raw ed25519 public key, for verifying signatures.
func (k *Key) Public() ed25519.PublicKey { return k.pub }

// SavePrivate writes the private key to a file with restrictive permissions.
func (k *Key) SavePrivate(path string) error {
	b := base64.StdEncoding.EncodeToString(k.priv)
	return os.WriteFile(path, []byte(b+"\n"), 0o600)
}

// LoadPrivate reads a private key file.
func LoadPrivate(path string) (*Key, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(trim(string(b)))
	if err != nil {
		return nil, fmt.Errorf("signing: decode key: %w", err)
	}
	if len(raw) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("signing: bad private key length %d", len(raw))
	}
	priv := ed25519.PrivateKey(raw)
	return &Key{priv: priv, pub: priv.Public().(ed25519.PublicKey)}, nil
}

// Sign returns a detached signature over data.
func (k *Key) Sign(data []byte) []byte { return ed25519.Sign(k.priv, data) }

// Verify checks a detached signature.
func Verify(pub ed25519.PublicKey, data, sig []byte) bool { return ed25519.Verify(pub, data, sig) }

// ParsePublicBase64 decodes a base64 public key.
func ParsePublicBase64(s string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(trim(s))
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("signing: bad public key length %d", len(raw))
	}
	return ed25519.PublicKey(raw), nil
}

func trim(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}

// Store registers signing keys in the database (public keys only).
type Store struct{ db *sql.DB }

// NewStore creates a key store.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Register records a key as the active signing key, keeping any previous keys
// available for verification (rotation).
func (s *Store) Register(k *Key, now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("UPDATE signing_keys SET active = 0"); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO signing_keys (key_id, public_key, algorithm, active, created_at)
		VALUES (?, ?, 'ed25519', 1, ?)
		ON CONFLICT(key_id) DO UPDATE SET active = 1`,
		k.KeyID(), k.PublicBase64(), now.UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return tx.Commit()
}

// PublicKey returns the registered public key for a key id.
func (s *Store) PublicKey(keyID string) (ed25519.PublicKey, error) {
	var b64 string
	if err := s.db.QueryRow("SELECT public_key FROM signing_keys WHERE key_id = ?", keyID).Scan(&b64); err != nil {
		return nil, err
	}
	return ParsePublicBase64(b64)
}

// EnsureKey loads the private key from path, generating and registering a new
// one if the file does not exist. It returns the key and its id.
func EnsureKey(s *Store, path string, now time.Time) (*Key, error) {
	if k, err := LoadPrivate(path); err == nil {
		if err := s.Register(k, now); err != nil {
			return nil, err
		}
		return k, nil
	}
	k, err := Generate()
	if err != nil {
		return nil, err
	}
	if err := k.SavePrivate(path); err != nil {
		return nil, err
	}
	if err := s.Register(k, now); err != nil {
		return nil, err
	}
	return k, nil
}
