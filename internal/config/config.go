package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	ScanPaths []string `toml:"scan_paths"`
	// ClaudeHomes lists the Claude config dirs to read. Load always fills it
	// and owns the default, so callers use it as given.
	ClaudeHomes    []string `toml:"claude_homes"`
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

# Claude Code config dirs to read sessions from. Defaults to ~/.claude, plus
# $CLAUDE_CONFIG_DIR when it is set. Set this to read several configs at once,
# e.g. a work config and a separate personal one. An explicit list wins over
# $CLAUDE_CONFIG_DIR, so include every dir you want scanned.
# claude_homes = ["~/.claude", "~/.claude-personal"]

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

// claudeConfigDirEnv picks the config dir the claude binary reads. It only
// ever sees that one dir.
const claudeConfigDirEnv = "CLAUDE_CONFIG_DIR"

// DefaultClaudeHome is the config dir Claude Code uses when CLAUDE_CONFIG_DIR
// is unset.
func DefaultClaudeHome() string {
	return expandHome("~/.claude")
}

// ClaudeHome returns the config dir a session came from. Claude stores
// projects as <home>/projects/<project>, so the home is the grandparent. Any
// other shape returns "", and the caller then inherits the environment.
func ClaudeHome(claudeDir string) string {
	projects := filepath.Dir(claudeDir)
	if filepath.Base(projects) != "projects" {
		return ""
	}
	return filepath.Dir(projects)
}

// ClaudeEnv pins the config dir for one claude call. cc360 can itself run
// under a CLAUDE_CONFIG_DIR for another config, so a known home is always set.
// An unknown home returns nil and inherits the environment.
func ClaudeEnv(home string) []string {
	if home == "" {
		return nil
	}
	return append(os.Environ(), claudeConfigDirEnv+"="+home)
}

// ClaudeEnvPrefix is the same pin as a shell prefix. quote must shell-quote
// its argument. The shell the command is pasted into can select another
// config, so a known home is always pinned.
func ClaudeEnvPrefix(home string, quote func(string) string) string {
	if home == "" {
		return ""
	}
	return claudeConfigDirEnv + "=" + quote(home) + " "
}

// DefaultClaudeHomes returns the dirs to scan when claude_homes is absent:
// ~/.claude, plus $CLAUDE_CONFIG_DIR when the caller runs under a second
// config.
func DefaultClaudeHomes() []string {
	homes := []string{DefaultClaudeHome()}
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		homes = append(homes, d)
	}
	return normalizeHomes(homes)
}

// normalizeHomes expands ~, makes paths absolute, and drops duplicates. The
// pin is read from the session's project dir, so a relative home would resolve
// against the wrong dir.
func normalizeHomes(homes []string) []string {
	seen := make(map[string]bool, len(homes))
	out := make([]string, 0, len(homes))
	for _, h := range homes {
		if h == "" {
			continue
		}
		p := filepath.Clean(expandHome(h))
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
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
		fmt.Println("Welcome to cc360 - Claude Code 360")
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

	// An explicit claude_homes wins over $CLAUDE_CONFIG_DIR. Absent or empty
	// falls back to the default.
	if len(cfg.ClaudeHomes) == 0 {
		cfg.ClaudeHomes = DefaultClaudeHomes()
	} else {
		cfg.ClaudeHomes = normalizeHomes(cfg.ClaudeHomes)
	}

	// Apply defaults for zero values
	if cfg.SortBy == "" {
		cfg.SortBy = "modified"
	}

	return cfg, false, nil
}
