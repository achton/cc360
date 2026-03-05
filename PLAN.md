# CC360 -- Implementation Plan

Reference: `SPEC.md` in this repository.

## Overview

Eight implementation phases, designed so each phase produces a testable increment. Phases 1-4 deliver a usable tool. Phases 5-8 add polish and secondary features.

---

## Phase 1: Project skeleton + Config

**Goal**: Go module builds, config file loads or gets created on first run.

### Files
- `go.mod` -- module `github.com/achton/cc360`, Go 1.23+
- `main.go` -- entry point: load config, print status, exit
- `internal/config/config.go`

### Config implementation

```go
type Config struct {
    ScanPaths            []string `toml:"scan_paths"`
    ScanOrphans          bool     `toml:"scan_orphans"`
    HideSidechains       bool     `toml:"hide_sidechains"`
    AutoSummarize        int      `toml:"auto_summarize"`
    SummarizeConcurrency int      `toml:"summarize_concurrency"`
    SummarizeModel       string   `toml:"summarize_model"`
    SortBy               string   `toml:"sort_by"`
}
```

- `Load()` reads `~/.config/cc360/config.toml`
- If file missing: create directory + default file with `scan_paths = []`, print setup message, `os.Exit(0)`
- If `scan_paths` is empty: print message, `os.Exit(0)`
- Expand `~` in all paths

### Dependencies
- `github.com/BurntSushi/toml`

### Verification
```bash
go build -o cc360 . && ./cc360
# First run: creates config, prints setup instructions
# After editing config: prints "Config loaded, N scan paths"
```

---

## Phase 2: Scanner

**Goal**: Discover sessions from disk, print count.

### Files
- `internal/scanner/scanner.go`

### Types

```go
type Session struct {
    SessionID       string
    ProjectName     string    // derived short name
    ProjectPath     string
    ClaudeDir       string    // ~/.claude/projects/...
    FirstPrompt     string
    ExistingSummary string
    MessageCount    int
    Created         time.Time
    Modified        time.Time
    GitBranch       string
    IsSidechain     bool
    JSONLPath       string
}
```

### Logic

`Scan(cfg config.Config) ([]Session, error)`:

1. For each path in `cfg.ScanPaths`, expand `~`, derive the Claude project dir prefix (replace `/` with `-`).
2. Iterate `~/.claude/projects/*/`. For each dir, check if its decoded name starts with any scan path. Also include `~/.claude/projects/-home-{user}/` (the home dir itself).
3. **Index source**: Read `sessions-index.json`, parse entries into `Session` structs.
4. **Orphan source** (if `cfg.ScanOrphans`): Glob `*.jsonl`, skip those already in the index. Parse first ~15 lines for metadata.
5. Derive `ProjectName` by stripping the matching scan path prefix.
6. Return combined, deduplicated by `SessionID`.

### Verification
```bash
./cc360
# Prints "Scanned N sessions from M project directories"
```

---

## Phase 3: Database

**Goal**: Persist sessions to SQLite, support upsert and query.

### Files
- `internal/db/db.go`

### Implementation

Uses `modernc.org/sqlite` (pure Go, no CGo).

```go
type DB struct { conn *sql.DB }

func Open(path string) (*DB, error)           // ~/.cache/cc360/cc360.db
func (db *DB) Upsert(sessions []scanner.Session) error
func (db *DB) AllSessions(sortBy string, desc bool) ([]Session, error)
func (db *DB) Search(query string) ([]Session, error)
func (db *DB) Unsummarized(limit int) ([]Session, error)
func (db *DB) SetSummary(sessionID, title, summary string) error
func (db *DB) Close() error
```

- `Session` struct in `db` package mirrors scanner's but adds `Title`, `Summary`, `SummarizedAt` fields.
- Upsert: `INSERT ... ON CONFLICT(session_id) DO UPDATE SET ...` -- preserves `title`, `summary`, `summarized_at`.
- `AllSessions` supports sort by: `modified`, `created`, `message_count`, `project_name`.
- `Search` does case-insensitive `LIKE` across project_name, title, summary, first_prompt, git_branch.

### Integration in main.go

```
config.Load() -> scanner.Scan() -> db.Open() -> db.Upsert() -> print stats
```

### Verification
```bash
./cc360
# "Scanned 471 sessions, upserted to ~/.cache/cc360/cc360.db"
# Run again: same output, DB persists
```

---

## Phase 4: Basic TUI with table + actions

**Goal**: Interactive table, navigation, resume, shell, copy-to-clipboard. This is the MVP.

### Files
- `internal/tui/model.go` -- top-level model, Update, View
- `internal/tui/table.go` -- session table setup and rendering
- `internal/tui/keys.go` -- key bindings
- `internal/tui/styles.go` -- lipgloss styles

### Architecture

Top-level model:

```go
type Model struct {
    db          *db.DB
    cfg         config.Config
    sessions    []db.Session       // current filtered/sorted list
    table       table.Model        // bubbles table
    width       int
    height      int
    statusMsg   string
    keys        keyMap
}
```

**Bubbletea message flow**:
- `tea.WindowSizeMsg` -> store dimensions, recalculate column widths, resize table
- `tea.KeyPressMsg` -> dispatch based on key map

**Table columns** (responsive, computed in `recalcColumns(width)`):
- `width >= 100`: Date(10) + Project(flexible) + Branch(14) + Msgs(5) + Title(flexible)
- `width >= 90`: Drop Branch
- `width >= 80`: Drop Msgs
- Project and Title share remaining space roughly 1:2

**Key actions**:
- `j`/`k`/arrows: handled by `table.Model.Update()`
- `g`/`G`: set cursor to 0 / len-1
- `Enter`: `tea.ExecProcess(exec.Command("claude", "--resume", id), ...)` with `Dir` set to project path
- `o`: `tea.ExecProcess(exec.Command(shell), ...)` with `Dir` set to project path
- `c`: write `cd <path> && claude --resume <id>` to clipboard via OSC 52. Show confirmation in status bar.
- `q`: `tea.Quit`

**OSC 52 clipboard** (no external deps):
```go
func copyToClipboard(s string) {
    encoded := base64.StdEncoding.EncodeToString([]byte(s))
    fmt.Fprintf(os.Stderr, "\033]52;c;%s\007", encoded)
}
```

### main.go integration

```go
sessions := db.AllSessions(cfg.SortBy, true)
m := tui.New(database, cfg, sessions)
p := tea.NewProgram(m, tea.WithAltScreen())
p.Run()
```

### Verification
- Arrow keys navigate table
- `Enter` suspends TUI, runs `claude --resume`, returns
- `o` opens shell, returns
- `c` copies command, status bar shows confirmation
- Resize terminal: columns adapt, no horizontal overflow
- `q` exits

---

## Phase 5: Detail pane

**Goal**: Togglable bottom pane showing full session info.

### Files
- `internal/tui/detail.go`

### Implementation

A `viewport.Model` from bubbles, rendered below the table when toggled.

```go
type DetailPane struct {
    viewport viewport.Model
    visible  bool
}
```

- `Tab` toggles `visible`. When toggled, recalculate table height (table gets `height - detailHeight - chrome`).
- Detail content is rebuilt whenever the cursor moves (on `table.Model` cursor change).
- Renders using lipgloss:
  - **Title** (bold)
  - **Summary**
  - Session ID | Created | Modified
  - Project path | Git branch
  - First prompt (first 200 chars, word-wrapped)

### View layout

```go
func (m Model) View() string {
    header := renderHeader(m)
    tbl := m.table.View()
    detail := ""
    if m.detail.visible {
        detail = m.detail.View()
    }
    status := renderStatus(m)
    keys := renderKeyHints(m)
    return lipgloss.JoinVertical(lipgloss.Left, header, tbl, detail, status, keys)
}
```

### Verification
- `Tab` shows/hides detail pane
- Navigating updates detail content
- Table shrinks/grows to accommodate

---

## Phase 6: Summarizer

**Goal**: Generate titles and summaries via `claude --print`, with visible feedback.

### Files
- `internal/summarizer/summarizer.go`
- `internal/tui/model.go` (add summarization messages and commands)

### Summarizer implementation

```go
func Summarize(session db.Session, model string) (title string, summary string, err error)
```

1. Read JSONL, extract first 5 user messages (skip `[Request interrupted by user]`, cap each at 500 chars).
2. Build prompt requesting `TITLE:` (max 3 words) and `SUMMARY:` (max 60 chars).
3. `exec.Command("claude", "--print", "--no-session-persistence", "--model", model)` with prompt on stdin.
4. Parse `TITLE:` and `SUMMARY:` lines from stdout.
5. Return error on timeout (30s), non-zero exit, or parse failure.

### TUI integration -- messages

```go
type SummarizeStartMsg struct{ SessionID string }
type SummarizeResultMsg struct{ SessionID, Title, Summary string; Err error }
```

### Single summarize (`s`)

1. Dispatch `SummarizeStartMsg` -- set the row's title cell to spinner view, update status bar.
2. Return a `tea.Cmd` that calls `summarizer.Summarize()` in a goroutine and sends `SummarizeResultMsg`.
3. On `SummarizeResultMsg`: update DB, update row in table, update status bar.

### Spinner

Embed a `spinner.Model` in the top-level model. When any summarization is in-flight, tick the spinner. Show spinner in status bar: `[spinner] Summarizing myapp...`

### Verification
- Press `s` on a session -> spinner appears in status bar -> title/summary populate after ~5s
- Press `s` on already-summarized session -> re-summarizes

---

## Phase 7: Background auto-summarization + Summarize All

**Goal**: Concurrent background summarization on launch and via `S` key.

### Implementation

Worker pool using a channel + N goroutines:

```go
type summarizeQueue struct {
    pending chan db.Session
    results chan SummarizeResultMsg
}
```

- On launch: feed `db.Unsummarized(cfg.AutoSummarize)` into the channel.
- `S` key: feed `db.Unsummarized(0)` (all) into the channel.
- `cfg.SummarizeConcurrency` goroutines read from `pending`, call `summarizer.Summarize()`, send to `results`.
- A `tea.Cmd` listens on the `results` channel and emits `SummarizeResultMsg` messages to the TUI.

### Status bar progress

Track `completed`/`total` counters. Status bar shows: `Summarizing 3/25...` with spinner.

### Verification
- Launch with `auto_summarize = 5` -> status bar shows progress, rows update as summaries arrive
- Press `S` -> all unsummarized sessions get queued, progress updates
- Multiple summaries arrive concurrently (verify N > 1 workers)

---

## Phase 8: Filtering, sorting, and project picker

**Goal**: Live text filter, project picker, sort keys.

### Files
- `internal/tui/filter.go`

### Text filter (`/`)

- Toggle a `textinput.Model` at the top of the screen.
- On each keystroke (`textinput.Model` change), call `db.Search(query)` and rebuild the table rows.
- `Escape` clears the input, hides it, restores full session list.
- When filter is active, key bindings for navigation still work but single-letter bindings (`s`, `q`, `o`, etc.) are suppressed (input has focus).

### Project picker (`p`)

- Build a deduplicated, sorted list of project names with session counts from current data.
- Show as a simple `list.Model` overlay or replace the table temporarily.
- Selecting a project filters sessions to that project. Pressing `p` again or `Escape` clears.

### Sort keys (`1`-`4`)

- `1`: sort by `modified` DESC
- `2`: sort by `created` DESC
- `3`: sort by `message_count` DESC
- `4`: sort by `project_name` ASC, then `modified` DESC

Pressing the same key again toggles ASC/DESC. Update header to show current sort indicator.

Re-sorting re-queries the DB and rebuilds the table.

### Sidechain toggle (`x`)

Toggle `hide_sidechains` in runtime state. Re-query and rebuild table. Status bar: `Sidechains: visible` / `Sidechains: hidden`.

### Verification
- `/` opens filter, typing narrows results live, `Escape` clears
- `p` shows project list, selecting filters, `Escape` clears
- `1`-`4` change sort, pressing same key reverses
- `x` toggles sidechain visibility

---

## Dependency Summary

```
github.com/BurntSushi/toml       # config parsing
modernc.org/sqlite                # pure-Go SQLite
charm.land/bubbletea/v2           # TUI framework
charm.land/bubbles/v2/table       # table component
charm.land/bubbles/v2/viewport    # detail pane scrolling
charm.land/bubbles/v2/textinput   # filter input
charm.land/bubbles/v2/spinner     # loading indicator
charm.land/bubbles/v2/list        # project picker
charm.land/bubbles/v2/help        # key hint rendering
charm.land/bubbles/v2/key         # key binding definitions
charm.land/lipgloss/v2            # styling and layout
```

## Phase Summary

| Phase | Delivers | Testable? |
|-------|----------|-----------|
| 1 | Config loading, first-run experience | Yes: `./cc360` prints setup or confirms config |
| 2 | Session scanning from disk | Yes: prints session count |
| 3 | SQLite persistence | Yes: prints upsert stats, DB file created |
| 4 | **MVP TUI**: table, navigate, resume, shell, clipboard | Yes: full interactive testing |
| 5 | Detail pane | Yes: Tab toggles pane |
| 6 | Single-session summarization with feedback | Yes: `s` triggers summarize with spinner |
| 7 | Background + bulk summarization | Yes: auto-summarize on launch, `S` for all |
| 8 | Filter, sort, project picker | Yes: `/`, `p`, `1`-`4`, `x` all functional |
