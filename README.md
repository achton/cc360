# CC360 — Claude Code 360

[![CI](https://github.com/achton/cc360/actions/workflows/ci.yml/badge.svg)](https://github.com/achton/cc360/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/achton/cc360)](https://github.com/achton/cc360/releases/latest)
[![Go Report Card](https://goreportcard.com/badge/github.com/achton/cc360)](https://goreportcard.com/report/github.com/achton/cc360)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A terminal UI for browsing, searching, filtering, and resuming [Claude Code](https://claude.ai/claude-code) sessions across multiple projects.

![CC360 demo](demo.gif)

## Why

After a reboot or across days of work, there's no easy way to see what Claude Code sessions existed, what they were about, or to resume them. Claude Code's `--resume` flag requires knowing the session ID. CC360 gives you a persistent, searchable overview of all sessions across your project directories.

## Install

Requires [Claude Code](https://docs.anthropic.com/en/docs/claude-code) installed.

### Homebrew (macOS)

```bash
brew install --cask achton/tap/cc360
```

Linux users: use the install script or a distro package below. Homebrew casks are
macOS-only.

Upgrading from a pre-v0.5.1 formula install: `brew uninstall cc360` first.

### Install script (macOS / Linux)

```bash
curl -fsSL https://raw.githubusercontent.com/achton/cc360/main/install.sh | sh
```

Installs the latest release to `/usr/local/bin` (override with `CC360_INSTALL_DIR`). Pin a version by passing it as an argument, e.g. `... | sh -s -- v0.3.0`.

### Linux packages (.deb / .rpm / .apk)

Download the package for your distro from [GitHub Releases](https://github.com/achton/cc360/releases/latest) and install it:

```bash
sudo dpkg -i cc360_*_linux_amd64.deb     # Debian / Ubuntu
sudo rpm -i cc360_*_linux_amd64.rpm       # Fedora / RHEL / openSUSE
sudo apk add --allow-untrusted cc360_*_linux_amd64.apk  # Alpine
```

### Go

```bash
go install github.com/achton/cc360@latest
```

### Binary

Download the latest release for your platform from [GitHub Releases](https://github.com/achton/cc360/releases/latest). Binaries are available for Linux and macOS (amd64 and arm64).

## First run

On first launch, CC360 creates a config file at `~/.config/cc360/config.toml` and exits with setup instructions. Edit the config to add your project directories:

```toml
scan_paths = ["~/Code", "~/Projects"]
```

Then run `cc360` again to launch the TUI.

## Configuration

Config file: `~/.config/cc360/config.toml`

| Setting | Default | Description |
|---------|---------|-------------|
| `scan_paths` | `[]` | Directories containing your projects. CC360 scans `~/.claude/projects/` for sessions matching these paths. **Required.** |
| `scan_orphans` | `true` | Include sessions found in `.jsonl` files that aren't listed in any session index. |
| `hide_sidechains` | `true` | Hide sidechain (branched conversation) sessions. |
| `sort_by` | `"modified"` | Default sort order. Options: `modified`, `created`, `messages`, `project`. |

## Keybindings

| Key | Action |
|-----|--------|
| `↑`/`k`, `↓`/`j` | Navigate up/down |
| `PgUp`, `PgDn` | Page up/down |
| `Home`/`g`, `End`/`G` | Jump to top/bottom |
| `Enter` | Resume the selected session (`claude --resume`) |
| `Tab` | Toggle detail pane (open by default) |
| `/` | Open text filter (live search across all fields) |
| `p` | Open project picker (tree view with multi-select) |
| `c` | Copy resume command to clipboard (via OSC 52) |
| `r` | Reload config and re-scan all sessions |
| `Esc` | Clear text filter |
| `q`, `Ctrl+C` | Quit |

### Filtering

Press `/` to open the text filter. Type to search across project names, titles, summaries, first prompts, and git branches. Press `Enter` to stop typing and navigate the filtered results — the filter stays visible. Press `/` again to edit the filter text. Press `Esc` to clear.

Press `p` to open the project picker, which shows a collapsible tree of projects grouped by directory. Use `Space` to toggle selection, `←`/`→` to collapse/expand groups, and `Enter` to apply. Multiple projects can be selected at once. Sessions in root directories (not subfolders) appear as a dimmed `(root)` entry.

Filters stack: pick projects with `p`, then refine with `/`.

## Active session detection

CC360 detects currently running Claude Code sessions by inspecting running `claude` processes — via `/proc` on Linux and `ps` + `lsof` on macOS. Active sessions are marked with a green `●` next to the date and cannot be resumed (to prevent conflicts). Detection refreshes every 15 seconds.

## How it works

### Data sources

CC360 reads Claude Code's own data files:

- **Session index**: `~/.claude/projects/{encoded-path}/sessions-index.json` contains metadata for each session (ID, timestamps, branch, message count, summary).
- **Orphan JSONL files**: Some sessions exist only as `.jsonl` transcript files without an index entry. CC360 parses the first 15 lines to extract metadata (cwd, branch, first prompt), then scans the full file for the last timestamp and message count.

The encoded path replaces `/` with `-` (e.g. `/home/user/Code/myproject` → `-home-user-Code-myproject`).

### Caching

Session metadata is cached in a SQLite database at `~/.cache/cc360/cc360.db`. On each launch, CC360 scans the disk and upserts into the cache.

### Session filtering

CC360 automatically filters out non-interactive sessions:

- **Hook/command sessions** — Sessions containing "Caveat: The messages below were generated by the user while running local commands." These are created by Claude Code's hook system and contain automated output, not interactive conversations.
- **Sub-agent sessions** — Sessions starting with `<teammate-message>`. These are spawned as background workers and aren't meaningful to resume independently.

### Display

The "Project summary" column shows (in priority order):

1. Claude's own session summary from the index file
2. The first user message as a fallback

The "Folder" column shows the project directory relative to the scan path (e.g. `Code/myproject`, `Code/lib/mylib`). Worktree sessions show a `⌥` indicator next to the folder name.

Columns are responsive: Branch appears at 90+ columns, message count at 100+.

## Project structure

```
main.go                         Entry point
internal/
  config/config.go              TOML config loading, first-run experience
  scanner/
    scanner.go                  Dual-source session discovery (index + orphan JSONL)
    active.go                   Active session detection (shared matching core)
    active_linux.go             Process discovery via /proc (Linux)
    active_darwin.go            Process discovery via ps + lsof (macOS)
  db/db.go                      SQLite cache (pure Go, no CGo)
  tui/
    model.go                    Bubbletea model, update loop, actions
    table.go                    Custom table rendering (columns, rows, scrolling)
    detail.go                   Togglable detail pane
    filter.go                   Text filter input
    picker.go                   Project picker overlay
    keys.go                     Key bindings
    styles.go                   Lipgloss styles
```

## Tech stack

- [Go](https://go.dev) 1.26+
- [Bubbletea](https://github.com/charmbracelet/bubbletea) — Elm-architecture TUI framework
- [Lipgloss](https://github.com/charmbracelet/lipgloss) — Terminal styling
- [Bubbles](https://github.com/charmbracelet/bubbles) — Spinner, text input components
- [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) — Pure Go SQLite (no CGo)
- [BurntSushi/toml](https://github.com/BurntSushi/toml) — Config parsing

## License

MIT
