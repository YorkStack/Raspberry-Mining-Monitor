package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyAndRestoreReproduceFiles(t *testing.T) {
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "data"), 0o750)
	os.WriteFile(filepath.Join(src, "data", "a.csv"), []byte("hello"), 0o640)
	os.WriteFile(filepath.Join(src, "manifest.json"), []byte("{}"), 0o640)

	target := t.TempDir()
	r, err := Copy(src, target)
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if !r.Verified || r.FilesCopied != 2 {
		t.Errorf("copy result = %+v, want verified 2 files", r)
	}
	if b, _ := os.ReadFile(filepath.Join(target, "data", "a.csv")); string(b) != "hello" {
		t.Error("copied content differs")
	}

	// Restore-test to a scratch dir reproduces the files.
	scratch := t.TempDir()
	rr, err := RestoreTest(target, scratch)
	if err != nil || !rr.Verified || rr.FilesCopied != 2 {
		t.Errorf("restore = %+v err=%v", rr, err)
	}
}
