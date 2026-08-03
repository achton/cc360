package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, dir, body string) {
	t.Helper()
	cfgDir := filepath.Join(dir, "cc360")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	tests := []struct {
		input string
		want  string
	}{
		{"~/Code", filepath.Join(home, "Code")},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
	}
	for _, tt := range tests {
		got := expandHome(tt.input)
		if got != tt.want {
			t.Errorf("expandHome(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestLoadCreatesDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	_, shouldExit, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !shouldExit {
		t.Error("expected shouldExit=true on first run")
	}

	// Config file should exist now
	path := filepath.Join(dir, "cc360", "config.toml")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("config file not created at %s", path)
	}
}

func TestLoadEmptyScanPaths(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	writeConfig(t, dir, `scan_paths = []`)

	_, shouldExit, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !shouldExit {
		t.Error("expected shouldExit=true with empty scan_paths")
	}
}

func TestLoadValidConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	writeConfig(t, dir, `
scan_paths = ["/tmp/test"]
sort_by = "created"
`)

	cfg, shouldExit, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shouldExit {
		t.Error("expected shouldExit=false")
	}
	if len(cfg.ScanPaths) != 1 || cfg.ScanPaths[0] != "/tmp/test" {
		t.Errorf("unexpected scan_paths: %v", cfg.ScanPaths)
	}
	if cfg.SortBy != "created" {
		t.Errorf("sort_by = %q, want created", cfg.SortBy)
	}
}

func TestLoadSortByDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	writeConfig(t, dir, `
scan_paths = ["/tmp/test"]
`)

	cfg, shouldExit, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shouldExit {
		t.Error("expected shouldExit=false")
	}
	if cfg.SortBy != "modified" {
		t.Errorf("sort_by = %q, want modified (default)", cfg.SortBy)
	}
}
