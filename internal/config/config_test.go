package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
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

// show_active defaults to true, so an existing config that predates the key
// keeps the indicators rather than silently losing them to Go's zero value.
func TestShowActiveDefaultsTrue(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeConfig(t, dir, `scan_paths = ["/tmp/test"]`)

	cfg, _, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.ShowActive {
		t.Error("ShowActive = false, want true when the key is absent")
	}
}

func TestShowActiveExplicitFalse(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeConfig(t, dir, `
scan_paths = ["/tmp/test"]
show_active = false
`)

	cfg, _, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ShowActive {
		t.Error("ShowActive = true, want false when explicitly disabled")
	}
}

// The generated default config must parse back to the intended defaults.
func TestDefaultConfigParsesToDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	body := strings.Replace(defaultConfig, `scan_paths = []`, `scan_paths = ["/tmp/test"]`, 1)
	writeConfig(t, dir, body)

	cfg, shouldExit, err := Load()
	if err != nil || shouldExit {
		t.Fatalf("Load: err=%v shouldExit=%v", err, shouldExit)
	}
	if !cfg.ShowActive {
		t.Error("ShowActive = false, want true from the default config")
	}
}

// Most configs omit claude_homes, so the default must cover both one config
// and a shell running under a second one.
func TestClaudeHomesDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	writeConfig(t, dir, `scan_paths = ["/tmp/test"]`)

	cfg, _, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{DefaultClaudeHome()}
	if !slices.Equal(cfg.ClaudeHomes, want) {
		t.Errorf("ClaudeHomes = %v, want %v", cfg.ClaudeHomes, want)
	}
}

func TestClaudeHomesDefaultIncludesEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("CLAUDE_CONFIG_DIR", "/tmp/personal-config")
	writeConfig(t, dir, `scan_paths = ["/tmp/test"]`)

	cfg, _, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{DefaultClaudeHome(), "/tmp/personal-config"}
	if !slices.Equal(cfg.ClaudeHomes, want) {
		t.Errorf("ClaudeHomes = %v, want %v", cfg.ClaudeHomes, want)
	}
}

// The env var only feeds the default. An explicit list is the whole list.
func TestClaudeHomesExplicitWinsOverEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("CLAUDE_CONFIG_DIR", "/tmp/from-env")
	writeConfig(t, dir, `
scan_paths = ["/tmp/test"]
claude_homes = ["~/.claude", "~/.claude-personal"]
`)

	cfg, _, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := []string{filepath.Join(home, ".claude"), filepath.Join(home, ".claude-personal")}
	if !slices.Equal(cfg.ClaudeHomes, want) {
		t.Errorf("ClaudeHomes = %v, want %v", cfg.ClaudeHomes, want)
	}
}

// A home named twice (directly, or once via the env) must not be scanned twice.
func TestClaudeHomesDedupe(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	writeConfig(t, dir, `
scan_paths = ["/tmp/test"]
claude_homes = ["~/.claude", "~/.claude/", "/tmp/other", "/tmp/other"]
`)

	cfg, _, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{DefaultClaudeHome(), "/tmp/other"}
	if !slices.Equal(cfg.ClaudeHomes, want) {
		t.Errorf("ClaudeHomes = %v, want %v", cfg.ClaudeHomes, want)
	}
}

// Resume derives the config dir from the project dir, so pin down the shape.
func TestClaudeHome(t *testing.T) {
	tests := []struct {
		name      string
		claudeDir string
		want      string
	}{
		{"standard layout", "/home/u/.claude/projects/-home-u-code-app", "/home/u/.claude"},
		{"second config", "/home/u/.claude-personal/projects/-home-u-priv", "/home/u/.claude-personal"},
		{"not a projects dir", "/home/u/.claude/sessions/-home-u-app", ""},
		{"degenerate value", "/test", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClaudeHome(tt.claudeDir); got != tt.want {
				t.Errorf("ClaudeHome(%q) = %q, want %q", tt.claudeDir, got, tt.want)
			}
		})
	}
}

// A relative home reaches claude from the session's project dir, which
// resolves against the wrong dir. Load must make it absolute.
func TestClaudeHomesAreAbsolute(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	writeConfig(t, dir, `
scan_paths = ["/tmp/test"]
claude_homes = ["relative/claude"]
`)

	cfg, _, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.ClaudeHomes) != 1 {
		t.Fatalf("ClaudeHomes = %v, want one entry", cfg.ClaudeHomes)
	}
	if !filepath.IsAbs(cfg.ClaudeHomes[0]) {
		t.Errorf("ClaudeHomes[0] = %q, want an absolute path", cfg.ClaudeHomes[0])
	}
}

// One configured home need not be the default one, so the pin cannot depend
// on how many homes exist.
func TestClaudeEnvPrefixPinsAnyKnownHome(t *testing.T) {
	quote := func(s string) string { return "'" + s + "'" }
	tests := []struct {
		home string
		want string
	}{
		{DefaultClaudeHome(), "CLAUDE_CONFIG_DIR='" + DefaultClaudeHome() + "' "},
		{"/home/u/.claude-personal", "CLAUDE_CONFIG_DIR='/home/u/.claude-personal' "},
		{"", ""},
	}
	for _, tt := range tests {
		if got := ClaudeEnvPrefix(tt.home, quote); got != tt.want {
			t.Errorf("ClaudeEnvPrefix(%q) = %q, want %q", tt.home, got, tt.want)
		}
	}
}
