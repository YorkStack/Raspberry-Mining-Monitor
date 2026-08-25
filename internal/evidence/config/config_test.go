package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "evidence.yaml")
	if err := os.WriteFile(path, []byte("evidence:\n  enabled: true\n  data_directory: /data/ev\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.DataDirectory != "/data/ev" {
		t.Errorf("DataDirectory = %q", c.DataDirectory)
	}
	if c.Timezone != "Europe/Berlin" {
		t.Errorf("Timezone default = %q, want Europe/Berlin", c.Timezone)
	}
	if _, err := c.Location(); err != nil {
		t.Errorf("Location: %v", err)
	}
	if c.DBPath() != filepath.Join("/data/ev", "evidence.db") {
		t.Errorf("DBPath = %q", c.DBPath())
	}
}

func TestLoadRejectsBadTimezone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "e.yaml")
	os.WriteFile(path, []byte("evidence:\n  timezone: Nowhere/Nope\n"), 0o600)
	if _, err := Load(path); err == nil {
		t.Error("bad timezone accepted")
	}
}
