package config

import (
	"os"
	"path/filepath"
	"testing"
)

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
	orig := os.Getenv("XDG_CONFIG_HOME")
	t.Setenv("XDG_CONFIG_HOME", dir)
	defer os.Setenv("XDG_CONFIG_HOME", orig)

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

	// Create config with empty scan_paths
	cfgDir := filepath.Join(dir, "cc360")
	os.MkdirAll(cfgDir, 0o755)
	os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(`scan_paths = []`), 0o644)

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

	cfgDir := filepath.Join(dir, "cc360")
	os.MkdirAll(cfgDir, 0o755)
	os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(`
scan_paths = ["/tmp/test"]
auto_summarize = 10
summarize_model = "haiku"
`), 0o644)

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
	if cfg.AutoSummarize != 10 {
		t.Errorf("auto_summarize = %d, want 10", cfg.AutoSummarize)
	}
	if cfg.SummarizeModel != "haiku" {
		t.Errorf("summarize_model = %q, want haiku", cfg.SummarizeModel)
	}
	// Defaults
	if cfg.SummarizeConcurrency != 3 {
		t.Errorf("summarize_concurrency = %d, want 3", cfg.SummarizeConcurrency)
	}
	if cfg.SortBy != "modified" {
		t.Errorf("sort_by = %q, want modified", cfg.SortBy)
	}
}

func TestLoadAutoSummarizeZero(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfgDir := filepath.Join(dir, "cc360")
	os.MkdirAll(cfgDir, 0o755)
	os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(`
scan_paths = ["/tmp/test"]
auto_summarize = 0
`), 0o644)

	cfg, shouldExit, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shouldExit {
		t.Error("expected shouldExit=false")
	}
	if cfg.AutoSummarize != 0 {
		t.Errorf("auto_summarize = %d, want 0 (explicitly disabled)", cfg.AutoSummarize)
	}
}
