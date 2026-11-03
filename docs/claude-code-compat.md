# Claude Code compatibility

cc360 reads files and runs commands that belong to Claude Code. Claude Code does not document most of them as a stable interface, so they change without notice. This file lists what cc360 depends on, and which Claude Code version this list was last checked against.

**Last reviewed:** Claude Code **2.1.317** (released 2026-11-02), on 2026-11-03, for cc360 v0.9.0.

## How to review a new range of releases

1. Get the changelog: `curl -fsSL https://raw.githubusercontent.com/anthropics/claude-code/main/CHANGELOG.md`. Release dates come from `npm view @anthropic-ai/claude-code time --json`.
2. Read every entry newer than the version above. Search first for: `transcript`, `jsonl`, `session`, `resume`, `attach`, `agents --json`, `title`, `rename`, `fork`, `worktree`, `CLAUDE_CONFIG_DIR`, `cleanupPeriodDays`, `projects/`.
3. Check the real data, not only the changelog. Count the record `type` values and their keys in recent transcripts, and compare them with the tables below. Run `claude agents --json` and `claude agents --json --all`, and compare the keys.
4. Update this file: the version above, the tables, and the timeline. Put work that is not done now in GitHub issues.

## Files

| Path | Use |
|---|---|
| `<home>/projects/<encoded cwd>/<session id>.jsonl` | The transcript. The source of every current session. |
| `<home>/projects/<encoded cwd>/<session id>/custom-title.json` | `{"customTitle": …}`, written on a rename, also from outside the CLI. It wins over the records. |
| `<home>/projects/<encoded cwd>/sessions-index.json` | Legacy index. Claude Code stopped writing it in February 2026. Read only for sessions without a transcript, and for its `summary`. |

`<home>` is `~/.claude` or a `CLAUDE_CONFIG_DIR`. The encoded directory name replaces `/` with `-`. It cannot be decoded reliably, and `CLAUDE_CODE_PROJECT_DIR_NAME` (2.1.234) can replace it with any name ([#44](https://github.com/achton/cc360/issues/44)).

Claude Code deletes transcripts older than `cleanupPeriodDays` (30 by default). Desktop sessions have their own rule (2.1.248).

## Transcript records

A transcript has one JSON object per line. A conversation record starts with `{"parentUuid":`. A metadata record starts with `{"type":"…"`. cc360 skips some records by this prefix without decoding them. If the key order changes, the records are decoded in full, which is correct but slower.

| `type` | Keys cc360 reads | Rule |
|---|---|---|
| `user`, `assistant` | `cwd`, `gitBranch`, `timestamp`, `sessionId`, `isSidechain`, `isMeta`, `isCompactSummary`, `entrypoint`, `message` | A message is a record that is not `isMeta` or `isCompactSummary`. The first prompt is the first such `user` record. The project is the first `cwd`. The branch is the last `gitBranch`. |
| `ai-title` | `aiTitle` | Last wins. Written many times per session. |
| `custom-title` | `customTitle` | Last wins. Set by `/rename`. |
| `agent-name` | `agentName` | Last wins. The same as the custom title after a rename. A background job can have only this. |
| `pr-link` | `prNumber`, `prRepository` | Every PR the session touched. Kept as `owner/repo#N`. |
| `cost-state` | `totalCostUSD`, `totalLinesAdded`, `totalLinesRemoved` | Running totals. Last wins. `totalCostUSD` can be an integer. |
| `api-request`, `api-request-blob`, `api-request-shape` | none, skipped | Since 2.1.300. A blob has a `message` with a role, which is not a conversation turn. |
| `file-history-snapshot`, `file-history-delta` | none, skipped | Large. |

`entrypoint` values seen: `cli`, `claude-desktop`, `claude-vscode`, `sdk-cli`. cc360 hides `sdk-*` sessions (`claude -p`, Agent SDK), unless a later record has another entrypoint.

Records that exist but that cc360 does not read yet: `relocated` (`relocatedCwd`, after `/cd`, [#40](https://github.com/achton/cc360/issues/40)), `worktree-state`, `continued-in` (`continuedInSessionId`, after `/fork`, [#45](https://github.com/achton/cc360/issues/45)), `frame-link`, `bridge-session`, `last-prompt`, `mode`, `permission-mode`, `tools-used`, `queue-operation`, `attachment`, `system`.

A single record can be larger than 1 MiB.

## Commands

All commands only see the config dir they run under, so cc360 sets `CLAUDE_CONFIG_DIR` to the home of the session.

| Command | Use | Since |
|---|---|---|
| `claude agents --json` | Running sessions, polled every 15 s per home. | 2.1.257 for a safe call from a TUI: earlier versions reset the terminal mode. |
| `claude --resume <session id>` | Resume a session that is not running. Run from the project directory. | |
| `claude attach <job id>` | Open a running background job. | 2.1.251 |
| `claude --resume <session id>` on a running background session | Opens it instead of refusing. cc360 uses this when a job has no `id`. | 2.1.285 |

`claude agents --json` returns an array. The keys cc360 reads:

| Key | Values |
|---|---|
| `sessionId` | The session ID. |
| `kind` | `interactive`, or `background` for `claude --bg` and `/fork`. |
| `id` | The job id that `claude attach` takes. Background only. |
| `pid` | The process, when one runs. |
| `name` | The session name. |
| `status` | `busy`, `waiting` or `idle`. Absent when a background job has no process. |
| `waitingFor` | A string that says why the session waits. Only with `waiting`. |
| `state` | Background lifecycle: `working`, `blocked`, `idle`, `done`, `failed`, `stopped`. cc360 uses it only when `status` is absent. |

## Timeline of changes that affected cc360

| Version | Change | cc360 |
|---|---|---|
| Feb 2026 | `sessions-index.json` no longer written. | Transcripts are the main source. |
| 2.1.212 | `agents --json` reports "needs input". `/fork` creates a background session. | v0.9.0: waiting state. |
| 2.1.234 | `CLAUDE_CODE_PROJECT_DIR_NAME`. Shorter AI titles. | [#44](https://github.com/achton/cc360/issues/44) |
| 2.1.251 | `claude attach`. `/cd` moves a session. | v0.9.0: attach. [#40](https://github.com/achton/cc360/issues/40) |
| 2.1.257 | `agents --json` no longer resets the terminal mode. | Minimum version for `show_active`. |
| 2.1.285 | `--resume` opens a running background session. `claude --desktop`. | v0.9.0: fallback. [#43](https://github.com/achton/cc360/issues/43) |
| 2.1.300 | Transcripts store API request snapshots. | v0.9.0: skipped, they inflated counts. |
| 2.1.314 | The `/resume` picker no longer uses injected text as a title. | v0.9.0: skips `isMeta` records. |
| 2.1.317 | `claude -p` inside a session gets its own session ID. Cleanup fixed where it was skipped. | `sdk-*` filter. More old transcripts deleted. |
