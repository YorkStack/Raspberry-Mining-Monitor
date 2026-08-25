// Package config loads the evidence subsystem configuration. It is separate
// from the monitor's config: the evidence binary is its own service.
//
// Only the fields the foundation needs are parsed here; later phases extend the
// struct (telemetry, valuation, integrity, backup, printing) following the
// documented evidence: block.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the evidence subsystem configuration (the evidence: block).
type Config struct {
	Enabled        bool   `yaml:"enabled"`
	DataDirectory  string `yaml:"data_directory"`
	Timezone       string `yaml:"timezone"`
	ReportLanguage string `yaml:"report_language"`
}

type file struct {
	Evidence Config `yaml:"evidence"`
}

// Default returns the built-in defaults.
func Default() Config {
	return Config{
		Enabled:        true,
		DataDirectory:  "mining-evidence",
		Timezone:       "Europe/Berlin",
		ReportLanguage: "de",
	}
}

// Load reads the evidence: block from a YAML file and applies defaults for any
// omitted field.
func Load(path string) (Config, error) {
	c := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("evidence config: %w", err)
	}
	var f file
	if err := yaml.Unmarshal(data, &f); err != nil {
		return Config{}, fmt.Errorf("evidence config: %w", err)
	}
	if f.Evidence.DataDirectory != "" {
		c.DataDirectory = f.Evidence.DataDirectory
	}
	if f.Evidence.Timezone != "" {
		c.Timezone = f.Evidence.Timezone
	}
	if f.Evidence.ReportLanguage != "" {
		c.ReportLanguage = f.Evidence.ReportLanguage
	}
	c.Enabled = f.Evidence.Enabled
	if err := c.validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

func (c Config) validate() error {
	if _, err := c.Location(); err != nil {
		return fmt.Errorf("evidence config: timezone %q: %w", c.Timezone, err)
	}
	return nil
}

// Location resolves the configured timezone.
func (c Config) Location() (*time.Location, error) {
	return time.LoadLocation(c.Timezone)
}

// DBPath is the SQLite file path inside the data directory.
func (c Config) DBPath() string { return filepath.Join(c.DataDirectory, "evidence.db") }
