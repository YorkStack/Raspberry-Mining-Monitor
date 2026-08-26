// Package backup copies an evidence package to a target and verifies the copy
// byte-for-byte against the source. It also restores a package for a
// restore-test. A failed or missing backup is reported, so downstream steps
// (such as automatic printing) can refuse to proceed.
package backup

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Result summarises a backup or restore run.
type Result struct {
	FilesCopied int
	Verified    bool
	Bad         string
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// Copy copies every file under srcDir into dstDir (preserving relative paths)
// and verifies each copied file's hash against the source. Verified is false and
// Bad names the first file that did not match.
func Copy(srcDir, dstDir string) (Result, error) {
	var r Result
	r.Verified = true
	err := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(dstDir, rel)
		if err := copyFile(path, dst); err != nil {
			return err
		}
		srcHash, err := hashFile(path)
		if err != nil {
			return err
		}
		dstHash, err := hashFile(dst)
		if err != nil {
			return err
		}
		r.FilesCopied++
		if srcHash != dstHash && r.Bad == "" {
			r.Verified = false
			r.Bad = rel
		}
		return nil
	})
	return r, err
}

// Record stores a backup run.
func Record(db *sql.DB, reportID, target string, r Result, now time.Time) error {
	result := "ok"
	if !r.Verified {
		result = "verification failed at " + r.Bad
	}
	_, err := db.Exec(`INSERT INTO backup_runs (report_id, target, files_copied, verified, result, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		nullStr(reportID), target, r.FilesCopied, boolToInt(r.Verified), result, now.UTC().Format(time.RFC3339))
	return err
}

// RestoreTest copies a backed-up package to a scratch directory and verifies it,
// proving the backup can be restored intact.
func RestoreTest(backupDir, scratchDir string) (Result, error) {
	return Copy(backupDir, scratchDir)
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
