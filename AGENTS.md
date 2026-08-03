# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Test

```bash
go build -o cc360 .                              # Build binary
go test ./... -v -race                            # Full test suite
go test -run TestModelStartsAndQuits ./internal/tui -v  # Single test
go vet ./...                                      # Lint
```

## Architecture

CC360 is a Go/Bubbletea TUI for browsing and resuming Claude Code sessions. Data flows linearly:

```
Disk scan → SQLite cache → TUI
```

1. **Scanner** (`internal/scanner/`) reads `~/.claude/projects/*/sessions-index.json` and orphan `.jsonl` files to discover sessions. Claude Code stopped writing `sessions-index.json` in February 2026, so in practice every current session arrives via the orphan JSONL path; the index only supplies older entries that predate that.
2. **DB** (`internal/db/`) upserts scan results into SQLite (`~/.cache/cc360/cc360.db`). `title` holds the `ai-title` harvested from the transcript, preserved when a later scan finds none. Titled sessions also survive `PruneUnseen`, so a title outlives Claude Code's 30-day transcript cleanup; they are still dropped once their project falls outside the configured scan paths.
3. **TUI** (`internal/tui/`) renders everything using Bubbletea's Elm architecture (Model → Update → View).

### TUI Model Composition

`Model` in `model.go` owns four subcomponents, each with their own state and `view()`:
- `sessionTable` — scrollable table with cursor, responsive column layout
- `detailPane` — togglable session details (8-line fixed height)
- `filterInput` — live text search bar
- `projectPicker` — tree-based multi-select overlay

### Concurrency

Active session detection polls `claude agents --json` every 15 seconds via `activeTickMsg`, which reports each running session's real ID and a busy/idle status. That replaced per-platform process scanning, which could only infer the ID by matching a process CWD against the most recently modified session in that directory. Any failure returns no results, so the indicators go quiet rather than breaking the table. Skipped entirely when `show_active = false`.

### Styling

All styles in `styles.go` use the Catppuccin Mocha palette. Styles are module-level vars, not created in render methods. Table columns have semantic roles (`colNormal`, `colTitle`, `colBranch`) for targeted styling in `renderCell()`.

## Layout Height Budget

The table's `setHeight()` reserves 4 lines of chrome (top separator + column header + separator + info line). The model's `tableHeight()` subtracts header(1) + status(1) + help(1) from terminal height, plus detail pane and filter bar when visible. The info line is always rendered (even when empty) to prevent layout jumps.

## Commit Messages

Use conventional commit prefixes for changelog generation via GoReleaser:
- `feat:` / `fix:` / `perf:` → appear in release changelog
- `docs:` / `test:` / `ci:` / `chore:` / `style:` → excluded from changelog

Scopes are excluded too (`chore(deps):`), but a breaking-change marker keeps the
commit in the changelog (`chore!:`).

## Key Conventions

- Non-interactive sessions (hook outputs, sub-agents) are filtered out in `main.go`, not in the DB or scanner.
- The scanner reads JSONL metadata from the first 15 lines, then scans the full file for accurate timestamps and message counts.
- Clipboard uses OSC 52 escape sequences (no external tools needed).
- Session resumption suspends the TUI via `tea.ExecProcess`, runs `claude --resume`, then restores.
- Worktree sessions are detected by `/.claude/worktrees/` in the project path.
