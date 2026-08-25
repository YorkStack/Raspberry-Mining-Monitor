// Package document stores evidence files (invoices, PDFs, supporting docs)
// with a content hash and versioning. Re-adding the same logical document never
// overwrites the original: it creates a new version. Uploads are validated
// against path traversal, filename injection, executables and MIME spoofing.
package document

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/evidence/audit"
)

// Meta is the optional metadata for a document.
type Meta struct {
	DocDate     string
	Description string
	MinerID     *int64
}

// Doc is a stored document version.
type Doc struct {
	DocUID           string
	Version          int
	DocType          string
	OriginalFilename string
	SafeFilename     string
	DocDate          string
	Description      string
	MinerID          *int64
	MimeType         string
	SizeBytes        int64
	SHA256           string
	UploadedAt       time.Time
	UploadedBy       string
	StorageLocation  string
}

// Store persists documents under baseDir.
type Store struct {
	db      *sql.DB
	log     *audit.Log
	baseDir string
}

// New creates a store. baseDir/documents holds the files.
func New(db *sql.DB, log *audit.Log, baseDir string) *Store {
	return &Store{db: db, log: log, baseDir: baseDir}
}

var (
	unsafeName = regexp.MustCompile(`[^\w.\- ]+`)
	slugStrip  = regexp.MustCompile(`[^a-z0-9]+`)
)

// validateFilename rejects path traversal, separators, and control characters.
func validateFilename(name string) error {
	if name == "" {
		return fmt.Errorf("document: empty filename")
	}
	if strings.ContainsAny(name, "/\\\x00") || strings.Contains(name, "..") {
		return fmt.Errorf("document: unsafe filename %q", name)
	}
	for _, r := range name {
		if r < 0x20 {
			return fmt.Errorf("document: control character in filename")
		}
	}
	return nil
}

// looksExecutable rejects common executable magic bytes.
func looksExecutable(content []byte) bool {
	if len(content) >= 4 {
		switch {
		case content[0] == 0x7f && string(content[1:4]) == "ELF": // Linux ELF
			return true
		case content[0] == 'M' && content[1] == 'Z': // Windows PE
			return true
		case string(content[:4]) == "\xcf\xfa\xed\xfe", string(content[:4]) == "\xfe\xed\xfa\xcf",
			string(content[:4]) == "\xca\xfe\xba\xbe": // Mach-O / universal
			return true
		}
	}
	if strings.HasPrefix(string(content), "#!") { // shebang script
		return true
	}
	return false
}

func slug(s string) string {
	s = strings.ToLower(s)
	s = slugStrip.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "doc"
	}
	return s
}

// Add stores a new document (or a new version of an existing logical document,
// keyed by its original filename). It writes the file, hashes it, records the
// metadata, and appends an audit event, all atomically.
func (s *Store) Add(docType, originalFilename string, content []byte, meta Meta, actor string, now time.Time) (Doc, error) {
	if err := validateFilename(originalFilename); err != nil {
		return Doc{}, err
	}
	if len(content) == 0 {
		return Doc{}, fmt.Errorf("document: empty content")
	}
	if looksExecutable(content) {
		return Doc{}, fmt.Errorf("document: executable uploads are not allowed")
	}

	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	// MIME is detected from content, not trusted from the name, defeating a
	// spoofed extension.
	mime := http.DetectContentType(content)

	docUID := slug(strings.TrimSuffix(originalFilename, filepath.Ext(originalFilename)))

	tx, err := s.db.Begin()
	if err != nil {
		return Doc{}, err
	}
	defer tx.Rollback()

	var maxVersion sql.NullInt64
	if err := tx.QueryRow("SELECT MAX(version) FROM evidence_documents WHERE doc_uid = ?", docUID).Scan(&maxVersion); err != nil {
		return Doc{}, err
	}
	version := 1
	if maxVersion.Valid {
		version = int(maxVersion.Int64) + 1
	}

	safeName := fmt.Sprintf("%s__v%03d__%s", docUID, version, unsafeName.ReplaceAllString(originalFilename, "_"))
	docsDir := filepath.Join(s.baseDir, "documents")
	if err := os.MkdirAll(docsDir, 0o750); err != nil {
		return Doc{}, err
	}
	fullPath := filepath.Join(docsDir, safeName)
	// O_EXCL: never silently overwrite an existing file.
	f, err := os.OpenFile(fullPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return Doc{}, fmt.Errorf("document: write %s: %w", safeName, err)
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		return Doc{}, err
	}
	if err := f.Close(); err != nil {
		return Doc{}, err
	}

	doc := Doc{
		DocUID: docUID, Version: version, DocType: docType, OriginalFilename: originalFilename,
		SafeFilename: safeName, DocDate: meta.DocDate, Description: meta.Description, MinerID: meta.MinerID,
		MimeType: mime, SizeBytes: int64(len(content)), SHA256: hash, UploadedAt: now,
		UploadedBy: actor, StorageLocation: fullPath,
	}

	var minerID any
	if meta.MinerID != nil {
		minerID = *meta.MinerID
	}
	if _, err := tx.Exec(`INSERT INTO evidence_documents
		(doc_uid, version, doc_type, original_filename, safe_filename, doc_date, description,
		 miner_id, mime_type, size_bytes, sha256, uploaded_at, uploaded_by, storage_location)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		docUID, version, docType, originalFilename, safeName, meta.DocDate, meta.Description,
		minerID, mime, doc.SizeBytes, hash, now.UTC().Format(time.RFC3339), actor, fullPath); err != nil {
		os.Remove(fullPath)
		return Doc{}, err
	}

	evType := "document.uploaded"
	if version > 1 {
		evType = "document.version_added"
	}
	if _, err := s.log.AppendTx(tx, audit.Event{
		EventUID: "doc-" + docUID + "-v" + fmt.Sprint(version) + "-" + now.UTC().Format(time.RFC3339Nano),
		TsUTC:    now, Actor: actor, Type: evType, Entity: "document", EntityID: docUID,
		NewValueHash: hash,
	}); err != nil {
		os.Remove(fullPath)
		return Doc{}, err
	}
	if err := tx.Commit(); err != nil {
		os.Remove(fullPath)
		return Doc{}, err
	}
	return doc, nil
}

// Versions returns every version of a logical document, oldest first.
func (s *Store) Versions(docUID string) ([]Doc, error) {
	rows, err := s.db.Query(`SELECT doc_uid, version, doc_type, original_filename, safe_filename,
		doc_date, description, miner_id, mime_type, size_bytes, sha256, uploaded_at, uploaded_by, storage_location
		FROM evidence_documents WHERE doc_uid = ? ORDER BY version ASC`, docUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Doc
	for rows.Next() {
		var (
			d          Doc
			uploadedAt string
			minerID    sql.NullInt64
		)
		if err := rows.Scan(&d.DocUID, &d.Version, &d.DocType, &d.OriginalFilename, &d.SafeFilename,
			&d.DocDate, &d.Description, &minerID, &d.MimeType, &d.SizeBytes, &d.SHA256,
			&uploadedAt, &d.UploadedBy, &d.StorageLocation); err != nil {
			return nil, err
		}
		d.UploadedAt, _ = time.Parse(time.RFC3339, uploadedAt)
		if minerID.Valid {
			d.MinerID = &minerID.Int64
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
