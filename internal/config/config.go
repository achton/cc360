package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	ScanPaths      []string `toml:"scan_paths"`
	ScanOrphans    bool     `toml:"scan_orphans"`
	HideSidechains bool     `toml:"hide_sidechains"`
	SortBy         string   `toml:"sort_by"`
	ShowActive     bool     `toml:"show_active"`
}

// defaults are applied before decoding, so a key absent from the file keeps its
// default rather than the Go zero value.
func defaults() Config {
	return Config{ShowActive: true}
}

const defaultConfig = `# Directories to scan for Claude Code sessions.
# cc360 will not start without at least one entry.
scan_paths = []

# Scan orphan JSONL files (sessions not in any index).
scan_orphans = true

# Hide sidechain (branched conversation) sessions.
hide_sidechains = true

# Default sort order: "modified", "created", "messages", "project".
sort_by = "modified"

# Mark sessions that are currently running: a filled dot when Claude is
# working, a hollow one when it is waiting. Polls "claude agents" every 15s.
show_active = true
`

func configDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "cc360")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "cc360")
}

func configPath() string {
	return filepath.Join(configDir(), "config.toml")
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

// Load reads the config file, creating a default one if it doesn't exist.
// Returns the config and a boolean indicating whether the program should exit
// (e.g. first run or empty scan_paths).
func Load() (Config, bool, error) {
	path := configPath()

	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(configDir(), 0o755); err != nil {
			return Config{}, true, fmt.Errorf("creating config dir: %w", err)
		}
		if err := os.WriteFile(path, []byte(defaultConfig), 0o644); err != nil {
			return Config{}, true, fmt.Errorf("writing default config: %w", err)
		}
		fmt.Println("Welcome to CC360 -- Claude Code 360")
		fmt.Println()
		fmt.Printf("No configuration found. Created default config at:\n  %s\n\n", path)
		fmt.Println("You must configure at least one scan path. For example:")
		fmt.Println()
		fmt.Println("  scan_paths = [\"~/Code\"]")
		fmt.Println()
		fmt.Println("Edit the config file, then run cc360 again.")
		return Config{}, true, nil
	}

	cfg := defaults()
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return Config{}, true, fmt.Errorf("parsing config: %w", err)
	}

	if len(cfg.ScanPaths) == 0 {
		fmt.Printf("No scan paths configured in %s\n\n", path)
		fmt.Println("Add at least one path under scan_paths, for example:")
		fmt.Println()
		fmt.Println("  scan_paths = [\"~/Code\"]")
		fmt.Println()
		fmt.Println("Then run cc360 again.")
		return cfg, true, nil
	}

	// Expand ~ in scan paths
	for i, p := range cfg.ScanPaths {
		cfg.ScanPaths[i] = expandHome(p)
	}

	// Apply defaults for zero values
	if cfg.SortBy == "" {
		cfg.SortBy = "modified"
	}

	return cfg, false, nil
}
