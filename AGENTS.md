# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Test

```bash
make check                                        # fmt + vet + build + test: run before every push
go test -run TestModelStartsAndQuits ./internal/tui -v  # Single test
```

`make check` runs the same gates as CI. The format check (`gofmt`) fails the build, so run `make check`, not a bare `go build`, before you push.

## Architecture

`cc360` is a Go/Bubbletea TUI for browsing and resuming Claude Code sessions. Data flows linearly:

```
Disk scan → SQLite cache → TUI
```

1. **Scanner** (`internal/scanner/`) reads `<claude home>/projects/*/sessions-index.json` and orphan `.jsonl` files to discover sessions. Claude Code stopped writing `sessions-index.json` in February 2026, so in practice every current session arrives via the orphan JSONL path; the index only supplies older entries that predate that.
   Homes come from `claude_homes`, default `~/.claude` plus `$CLAUDE_CONFIG_DIR` when set, so one table can span a work and a personal config. `config.Load` owns that default. `Scan` uses `cfg.ClaudeHomes` as given. `scanHome` runs once per home into a shared session-ID map, and earlier homes win. A missing home is skipped, not an error.
2. **DB** (`internal/db/`) upserts scan results into SQLite (`~/.cache/cc360/cc360.db`). `title` holds the `ai-title` harvested from the transcript, preserved when a later scan finds none. Titled sessions also survive `PruneUnseen`, so a title outlives Claude Code's 30-day transcript cleanup; they are still dropped once their project falls outside the configured scan paths.
3. **TUI** (`internal/tui/`) renders everything using Bubbletea's Elm architecture (Model → Update → View).

### TUI Model Composition

`Model` in `model.go` owns four subcomponents, each with their own state and `view()`:
- `sessionTable` — scrollable table with cursor, responsive column layout
- `detailPane` - togglable session details (fixed height: `detailContentLines` plus a border)
- `filterInput` — live text search bar
- `projectPicker` — tree-based multi-select overlay

### Concurrency

Active session detection polls `claude agents --json` every 15 seconds via `activeTickMsg`, which reports each running session's real ID and a busy/idle status. That replaced per-platform process scanning, which could only infer the ID by matching a process CWD against the most recently modified session in that directory. One query runs per Claude home, concurrently, because `pollActive` runs inline in `Update`. Any failure returns no results, so the indicators go quiet rather than breaking the table. Skipped entirely when `show_active = false`.

### Styling

All styles in `styles.go` use the Catppuccin Mocha palette. Styles are module-level vars, not created in render methods. Table columns have semantic roles (`colNormal`, `colTitle`, `colBranch`) for targeted styling in `renderCell()`.

In Lip Gloss v2, `Width()` counts the border and the padding, unlike v1 which added the
border on top. A bordered box sized to a budget must pass the full width, and the content
inside it must be built to `width - GetHorizontalFrameSize()`. The picker overlay and the
detail pane both do this. A line wider than that wraps, which makes the box taller than the
budget allows.

### Charm v2 notes

`View()` returns a `tea.View`, and terminal state such as `AltScreen` is declared
on it rather than passed to `tea.NewProgram`. Key handling matches on
`tea.KeyPressMsg`, whose `Code`/`Text` replaced v1's `Type`/`Runes`. Bracketed paste is
a separate `tea.PasteMsg`, so it has to be routed to the focused input by hand.

The v2 renderer emits only the cells that changed. `teatest` assertions must
therefore wait on content that actually updates: text painted once and never
touched again is consumed by the first read and never re-sent.

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
- Session resumption suspends the TUI via `tea.ExecProcess`, runs `claude --resume`, then restores. Both `claude agents` and `claude --resume` only see the config dir they run under, so the call pins `CLAUDE_CONFIG_DIR` to the session's home (`config.ClaudeEnv`). The home is not stored. `config.ClaudeHome` derives it from `claude_dir`, which is always `<home>/projects/<project>`, and returns `""` for any other shape so the caller inherits the environment. The copied command (`c`) carries the same pin as a shell prefix (`config.ClaudeEnvPrefix`) on the `claude` call, not the `cd`. `config.normalizeHomes` makes homes absolute, because that pin is read from the session's project dir.
- Worktree detection uses git metadata, in `internal/scanner/worktree.go`. The resolver walks from a session's path to the nearest `.git`. A `.git` file is a linked worktree when its admin-dir backlink points back to it. The parent repo comes from `commondir`, and the session groups under that parent. This works for any layout, not only `/.claude/worktrees/`.
- If the directory is gone, cc360 reads a `<repo>/.claude/worktrees/<name>` path directly. This keeps the parent grouping for a deleted Claude default-layout worktree.
- Worktree fields are stored per session in SQLite: `is_worktree`, `repo_key`, `parent_project_name`, `worktree_name`. A scan overwrites them when it can read the directory. It keeps them when the directory is gone. `is_worktree` is nullable, and NULL means never resolved. So a worktree keeps its grouping after `git worktree remove`, which erases the on-disk git metadata.
