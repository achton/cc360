# CC360 -- Claude Code 360 Specification

## Overview

CC360 is a terminal UI tool for browsing, searching, filtering, and resuming Claude Code sessions. It scans Claude Code's on-disk session data, caches metadata and AI-generated summaries in a local database, and presents everything in a responsive, keyboard-driven TUI built with Go and Bubbletea.

Binary name: `cc360`
Full name: Claude Code 360

## Problem

After a reboot or across days of work, there is no easy way to see what Claude Code sessions existed, what they were about, which project they belonged to, or to resume them. Claude Code's own `--resume` flag requires knowing the session ID. CC360 solves this by providing a persistent, searchable overview of all sessions across configured project directories.

## Data Sources

### Session Index Files

Claude Code stores session metadata at:
```
~/.claude/projects/{encoded-path}/sessions-index.json
```

The encoded path replaces `/` with `-` (e.g. `/home/user/Code/myproject` becomes `-home-user-Code-myproject`).

Each index file contains an `entries` array. Each entry has:

| Field | Type | Notes |
|-------|------|-------|
| `sessionId` | string | UUID |
| `fullPath` | string | Absolute path to the `.jsonl` transcript |
| `firstPrompt` | string | First user message (may be truncated with `...`) |
| `summary` | string | Claude-generated summary |
| `messageCount` | int | Total messages in session |
| `created` | ISO 8601 | Session start time |
| `modified` | ISO 8601 | Last activity time |
| `gitBranch` | string | Git branch at time of session |
| `projectPath` | string | Absolute path to the project directory |
| `isSidechain` | bool | Whether this is a branched/alternative conversation |

### Orphan JSONL Files

Some sessions exist only as `.jsonl` files with no corresponding index entry. These are discovered by scanning `*.jsonl` files in each project directory and checking whether their stem (filename without extension) appears in the index.

Orphan JSONL parsing extracts metadata from the first 15 lines (`sessionId`, `cwd`, `gitBranch`, first user message, `isSidechain`), then scans the full file for `lastTimestamp` and `msgCount` to ensure accurate modified time and message count.

Orphan scanning is enabled by default but can be disabled via config (`scan_orphans = false`).

### Derived Fields

- **Project name**: Derived by stripping the configured allowlist prefix from `projectPath`. E.g. if the allowlist contains `~/Code`, then `/home/user/Code/myapp` becomes `myapp`. Nested paths are preserved: `/home/user/Code/org/myservice` becomes `org/myservice`.
- **Allowlist group**: Which allowlist entry a session falls under (for filtering).

## Persistence / Cache

CC360 maintains a local database at `~/.cache/cc360/cc360.db` (SQLite).

### Schema

```sql
CREATE TABLE sessions (
    session_id      TEXT PRIMARY KEY,
    project_name    TEXT NOT NULL,
    project_path    TEXT,
    claude_dir      TEXT NOT NULL,       -- e.g. ~/.claude/projects/-home-...
    first_prompt    TEXT,
    existing_summary TEXT,               -- from sessions-index.json
    title           TEXT,                -- AI-generated, max 30 chars
    summary         TEXT,                -- AI-generated, max 60 chars
    message_count   INTEGER,
    created         TEXT,                -- ISO 8601
    modified        TEXT,                -- ISO 8601
    git_branch      TEXT,
    is_sidechain    INTEGER DEFAULT 0,
    jsonl_path      TEXT,
    last_scanned    TEXT,                -- ISO 8601, when this row was last updated from disk
    summarized_at   TEXT                 -- ISO 8601, when AI summary was generated
);
```

### Upsert Behavior

On scan, sessions are upserted. The upsert preserves existing `title` and `summary` (AI-generated fields) -- these are never overwritten by a scan. All other fields are updated from the latest disk data.

### Freshness Detection

On launch, CC360 re-scans all configured directories. It uses filesystem `mtime` on `sessions-index.json` and `*.jsonl` files to skip unchanged project directories since `last_scanned`. This keeps launch fast even with hundreds of sessions.

## Configuration

Config file: `~/.config/cc360/config.toml`

```toml
# Directories to scan. Sessions outside these paths are ignored.
# Paths are expanded (~ works). Required -- cc360 will not start without
# at least one entry.
scan_paths = [
    "~/Code",
    "~/Code-private",
]

# Scan orphan JSONL files (sessions not in any index). Default: true.
scan_orphans = true

# Hide sidechain sessions. Default: true.
hide_sidechains = true

# Number of sessions to auto-summarize on launch. 0 to disable. Default: 25.
auto_summarize = 25

# Maximum concurrent summarization calls. Default: 3.
summarize_concurrency = 3

# Model to use for summarization (passed to claude --model). Default: "sonnet".
summarize_model = "sonnet"

# Default sort order. Options: "modified", "created", "messages", "project".
# Default: "modified".
sort_by = "modified"
```

### First Run

If no config file exists, CC360 creates a default one with `scan_paths = []` and prints a message:

```
No scan paths configured. Edit ~/.config/cc360/config.toml and add at least
one path under scan_paths, then run cc360 again.
```

## TUI Layout

### Responsive Design

The layout adapts to terminal width. Minimum supported width: 80 columns. No horizontal scrollbar under any circumstances. Content is truncated with ellipsis rather than overflowing.

### Main View (default)

```
+------------------------------------------------------------------+
| CC360 - 471 sessions                          [sorted: modified] |
+------------------------------------------------------------------+
| Date       | Project        | Branch       | Msgs | Title        |
|------------|----------------|--------------|------|--------------|
| Yesterday  | myapp          | develop      |   14 | TUI fix S... |
| Yesterday  | org/myservice  | main         |   13 | Security a.. |
| 2d ago     | frontend       | feature/..   |   12 | Dep review.. |
| Mar  4     | website        | HEAD         |   11 | Drupal ski.. |
| > Mar  4   | backend        | HEAD         |    7 | Project st.. |
|            |                |              |      |              |
+------------------------------------------------------------------+
| Backend status check                                             |
| User asked about work done and progress so far.                  |
| Reviewed codebase and summarized progress.                       |
|                                                                  |
| Folder: Code/backend  |  Branch: HEAD  |  Msgs: 7               |
| Modified: 2026-03-04  |  ID: 9d80cfd6                           |
+------------------------------------------------------------------+
| enter resume · tab detail · / filter · p projects · s summarize  |
+------------------------------------------------------------------+
```

### Layout Regions

1. **Header bar** (1 line): App name with solid background and author tagline.
2. **Session table** (upper portion, scrollable): Columns adapt to terminal width. Catppuccin Mocha color theme with role-based column styling. Selected row has a blue accent bar. Info line below the table shows session count and scroll indicator.
3. **Detail pane** (lower portion, visible by default, toggled with Tab): Shows full info for highlighted session.
4. **Status bar** (1 line): Contextual feedback (summarization progress, errors). Truncated to prevent wrapping.
5. **Key hints** (1 line): Available actions.

### Column Priority (responsive)

When terminal width is limited, columns are hidden in this order (rightmost dropped first):

| Priority | Column | Min width | Notes |
|----------|--------|-----------|-------|
| 1 | Date | 10 | Always shown. `YYYY-MM-DD` |
| 2 | Project | 12-20 | Always shown. Truncated with ellipsis |
| 3 | Title | 10-30 | Always shown. Truncated with ellipsis |
| 4 | Msgs | 5 | Hidden below 100 cols |
| 5 | Branch | 8-15 | Hidden below 90 cols |

### Detail Pane

Toggled with `Tab`. When visible, it occupies the bottom ~8 lines and shows:

- **Title** (bold, full text -- not truncated)
- **Summary** (full text)
- **Session ID** (for manual `--resume` use)
- **Created / Modified** timestamps
- **Project path**
- **Git branch**
- **First prompt** (first ~200 chars)

### Sidechain Sessions

Hidden by default (`hide_sidechains = true`). When shown (toggled or configured), displayed with a dim style and a `[s]` marker in the title column.

## Summarization

### Generation

Summaries are generated by shelling out to `claude`:

```
claude --print --no-session-persistence --model <configured-model>
```

The prompt is piped to stdin. It includes:
- Project name and git branch
- Existing summary from the index (if any)
- First ~5 user messages from the JSONL (each truncated to 500 chars)

The prompt asks for:
- **TITLE**: Max 3 words, no quotes (stored max 30 chars)
- **SUMMARY**: One sentence, max 60 chars, no quotes

### Feedback

While summarization is running:
- Status bar shows: `Summarizing 3/25...` (with count progress)
- The row being summarized shows a spinner or `[...]` in the title column
- On completion, the row updates in-place with the new title

### Concurrency

Multiple `claude --print` calls run concurrently (default 3, configurable). A worker pool processes the queue.

### Triggers

- **On launch**: Auto-summarize up to N unsummarized sessions (configurable, default 25)
- **`s` key**: Summarize the currently selected session (re-summarize if already done)
- **`S` key**: Summarize all unsummarized sessions

## Keybindings

| Key | Action |
|-----|--------|
| `j` / `k` / Up / Down | Navigate session list |
| `g` / `G` | Jump to top / bottom |
| `Tab` | Toggle detail pane |
| `Enter` | Resume Claude session (suspend TUI, run `claude --resume`, return) |
| `c` | Copy resume command to clipboard (`cd <path> && claude --resume <id>`) |
| `s` | Summarize selected session |
| `/` | Open filter input |
| `Escape` | Clear text filter |
| `p` | Filter by project (tree picker with multi-select) |
| `r` | Reload config and re-scan sessions |
| `q` | Quit |

## Filtering & Sorting

### Text Filter (`/`)

Opens a text input at the top. Filters live as the user types. Matches against: project name, title, summary, first prompt, git branch. Case-insensitive substring match.

### Project Filter (`p`)

Opens a tree-based picker overlay showing projects grouped by directory. Use `Space` to toggle selection, `←`/`→` to collapse/expand groups, `Enter` to apply. Multiple projects can be selected. Sessions in root directories appear as a dimmed `(root)` entry within their group. Filters stack with the text filter.

## Actions

### Resume Session (`Enter`)

1. Suspend the TUI (release terminal)
2. Run `claude --resume <session-id>` with `cwd` set to the session's project path
3. When `claude` exits, restore the TUI

If the project path no longer exists, show an error in the status bar.

### Copy Resume Command (`c`)

Copies a shell command to the system clipboard:
```
cd /home/user/Code/myapp && claude --resume 9d80cfd6-...
```

Uses OSC 52 escape sequence for clipboard access (works in most modern terminals without external dependencies). Falls back to `xclip`/`xsel` if available.

Status bar confirms: `Copied resume command to clipboard`

### Reload (`r`)

Re-reads the config file, re-scans all session directories, and refreshes the session list in place without restarting.

## Technology Stack

- **Language**: Go (latest stable)
- **TUI framework**: [Bubbletea](https://github.com/charmbracelet/bubbletea) (Elm-architecture TUI framework)
- **Styling**: [Lipgloss](https://github.com/charmbracelet/lipgloss) (terminal styling/layout)
- **Components**: [Bubbles](https://github.com/charmbracelet/bubbles) (table, text input, viewport, spinner, help, key)
- **Database**: SQLite via `modernc.org/sqlite` (pure Go, no CGo dependency -- enables easy cross-compilation and static binary)
- **Config**: TOML via `github.com/BurntSushi/toml`
- **Build**: `go build` producing a single static binary

### Why Bubbletea

- Single static binary, no runtime dependencies
- Elm architecture (Model-Update-View) gives clean separation
- Rich ecosystem: `bubbles` provides table, list, text input, spinner, viewport, help components out of the box
- `lipgloss` handles responsive layout with flex-like width calculations
- Native terminal suspend/restore via `tea.ExecProcess`
- Concurrent operations via Go's goroutines and `tea.Cmd`

## Project Structure

```
cc360/
├── go.mod
├── go.sum
├── main.go                 # Entry point, config loading, scan, launch TUI
├── internal/
│   ├── config/
│   │   └── config.go       # TOML config parsing, defaults, first-run
│   ├── scanner/
│   │   └── scanner.go      # Discover sessions from disk (index + orphan JSONL)
│   ├── db/
│   │   └── db.go           # SQLite operations (upsert, query, search)
│   ├── summarizer/
│   │   └── summarizer.go   # Shell out to claude --print, parse response
│   └── tui/
│       ├── model.go         # Top-level Bubbletea model, Update, View
│       ├── table.go         # Session table component
│       ├── detail.go        # Detail pane component
│       ├── filter.go        # Filter input
│       ├── picker.go        # Project picker overlay (tree view)
│       ├── keys.go          # Key bindings
│       └── styles.go        # Lipgloss style definitions
```

## Build & Install

```bash
# Build
go build -o cc360 .

# Install to GOPATH/bin (typically in $PATH)
go install .

# Or copy to system path
sudo cp cc360 /usr/local/bin/

# Or symlink
ln -s $(pwd)/cc360 ~/.local/bin/cc360
```

## First Run Experience

```
$ cc360

Welcome to CC360 -- Claude Code 360

No configuration found. Creating default config at:
  ~/.config/cc360/config.toml

You must configure at least one scan path. For example:

  scan_paths = ["~/Code"]

Edit the config file, then run cc360 again.
```

After configuring:

```
$ cc360

Scanning 2 paths... found 471 sessions (3 new since last scan).
Auto-summarizing 25 sessions...

[TUI launches]
```

## Verification Checklist

1. `cc360` with no config -- prints setup instructions, creates default config
2. `cc360` with config -- scans, caches, launches TUI with populated table and detail pane
3. Arrow keys navigate, detail pane toggles with Tab
4. `Enter` suspends TUI, runs `claude --resume`, returns to TUI
5. `s` on a session -- spinner appears, summary populates after ~5s
6. Auto-summarization runs on launch with progress in status bar
7. `/` opens filter, typing filters live, Escape clears
8. `p` opens tree-based project picker, multi-select with Space, apply with Enter
9. `r` reloads config and re-scans sessions
10. Responsive: resize terminal, columns adapt, no horizontal overflow
11. 80-column terminal: Date + Project summary + Folder visible, Branch/Msgs hidden
12. Active sessions show green `●`, worktree sessions show `⌥` indicator
