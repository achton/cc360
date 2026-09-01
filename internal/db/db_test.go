package db

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/achton/cc360/internal/scanner"
)

// legacySchema is the pre-v0.6.0 table, with the summary and summarized_at
// columns that were dropped. CREATE TABLE IF NOT EXISTS leaves them in place on
// existing caches, so queries must not depend on column count or order.
const legacySchema = `
CREATE TABLE sessions (
	session_id      TEXT PRIMARY KEY,
	project_name    TEXT NOT NULL,
	project_path    TEXT,
	claude_dir      TEXT NOT NULL,
	first_prompt    TEXT,
	existing_summary TEXT,
	title           TEXT,
	summary         TEXT,
	message_count   INTEGER,
	created         TEXT,
	modified        TEXT,
	git_branch      TEXT,
	is_sidechain    INTEGER DEFAULT 0,
	jsonl_path      TEXT,
	last_scanned    TEXT,
	summarized_at   TEXT
);`

func TestOpenLegacySchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")

	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := conn.Exec(legacySchema); err != nil {
		t.Fatalf("legacy schema: %v", err)
	}
	if _, err := conn.Exec(
		`INSERT INTO sessions (session_id, project_name, claude_dir, existing_summary,
		 title, summary, modified, summarized_at)
		 VALUES ('old-1', 'legacy', '/test', 'kept', 'Old Title', 'dropped', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open on legacy db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	all, err := db.AllSessions("modified", true)
	if err != nil {
		t.Fatalf("AllSessions: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("got %d sessions, want 1", len(all))
	}
	if all[0].Title != "Old Title" {
		t.Errorf("title = %q, want Old Title", all[0].Title)
	}
	if all[0].ExistingSummary != "kept" {
		t.Errorf("existing_summary = %q, want kept", all[0].ExistingSummary)
	}

	// Upsert must not fail against the wider table either.
	mustUpsert(t, db, []scanner.Session{{
		SessionID: "old-1", ProjectName: "legacy", ClaudeDir: "/test",
	}})

	res, err := db.Search("legacy")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res) != 1 {
		t.Errorf("Search hits = %d, want 1", len(res))
	}
}

func testDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func mustUpsert(t *testing.T, db *DB, sessions []scanner.Session) {
	t.Helper()
	if err := db.Upsert(sessions); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
}

func TestUpsertAndQuery(t *testing.T) {
	db := testDB(t)

	sessions := []scanner.Session{
		{
			SessionID:    "abc-123",
			ProjectName:  "Code/myproject",
			ProjectPath:  "/home/user/Code/myproject",
			ClaudeDir:    "/home/user/.claude/projects/test",
			FirstPrompt:  "fix the bug",
			MessageCount: 5,
			Created:      time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			Modified:     time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
			GitBranch:    "main",
			JSONLPath:    "/tmp/abc.jsonl",
		},
		{
			SessionID:    "def-456",
			ProjectName:  "Code/other",
			ProjectPath:  "/home/user/Code/other",
			ClaudeDir:    "/home/user/.claude/projects/test2",
			FirstPrompt:  "add feature",
			MessageCount: 10,
			Created:      time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC),
			Modified:     time.Date(2025, 1, 4, 0, 0, 0, 0, time.UTC),
		},
	}

	if err := db.Upsert(sessions); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	all, err := db.AllSessions("modified", true)
	if err != nil {
		t.Fatalf("AllSessions: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("got %d sessions, want 2", len(all))
	}
	// Most recent first
	if all[0].SessionID != "def-456" {
		t.Errorf("first session = %s, want def-456", all[0].SessionID)
	}
}

func TestUpsertOverwritesBranch(t *testing.T) {
	db := testDB(t)

	s1 := []scanner.Session{{
		SessionID:   "abc-123",
		ProjectName: "test",
		ClaudeDir:   "/test",
		GitBranch:   "feature-old",
	}}
	mustUpsert(t, db, s1)

	s2 := []scanner.Session{{
		SessionID:   "abc-123",
		ProjectName: "test",
		ClaudeDir:   "/test",
		GitBranch:   "feature-new",
	}}
	mustUpsert(t, db, s2)

	all, _ := db.AllSessions("modified", true)
	if all[0].GitBranch != "feature-new" {
		t.Errorf("git_branch = %q, want feature-new", all[0].GitBranch)
	}
}

func TestUpsertStoresTitle(t *testing.T) {
	db := testDB(t)

	mustUpsert(t, db, []scanner.Session{{
		SessionID: "abc-123", ProjectName: "test", ClaudeDir: "/test",
		Title: "Harvested title",
	}})

	all, _ := db.AllSessions("modified", true)
	if all[0].Title != "Harvested title" {
		t.Fatalf("title = %q, want Harvested title", all[0].Title)
	}
}

// A rescan after Claude Code deleted the transcript yields no ai-title. The
// previously harvested one must survive.
func TestUpsertEmptyTitleDoesNotClobber(t *testing.T) {
	db := testDB(t)

	mustUpsert(t, db, []scanner.Session{{
		SessionID: "abc-123", ProjectName: "test", ClaudeDir: "/test",
		Title: "Harvested title",
	}})
	mustUpsert(t, db, []scanner.Session{{
		SessionID: "abc-123", ProjectName: "test", ClaudeDir: "/test",
		Title: "",
	}})

	all, _ := db.AllSessions("modified", true)
	if all[0].Title != "Harvested title" {
		t.Errorf("title = %q, want it preserved as Harvested title", all[0].Title)
	}
}

// A newer title replaces the stored one, so /rename is picked up on rescan.
func TestUpsertNewTitleReplaces(t *testing.T) {
	db := testDB(t)

	mustUpsert(t, db, []scanner.Session{{
		SessionID: "abc-123", ProjectName: "test", ClaudeDir: "/test", Title: "Old",
	}})
	mustUpsert(t, db, []scanner.Session{{
		SessionID: "abc-123", ProjectName: "test", ClaudeDir: "/test", Title: "New",
	}})

	all, _ := db.AllSessions("modified", true)
	if all[0].Title != "New" {
		t.Errorf("title = %q, want New", all[0].Title)
	}
}

func TestSearch(t *testing.T) {
	db := testDB(t)

	mustUpsert(t, db, []scanner.Session{
		{SessionID: "a", ProjectName: "myproject", ClaudeDir: "/test", FirstPrompt: "fix the navbar"},
		{SessionID: "b", ProjectName: "other", ClaudeDir: "/test", FirstPrompt: "add login page"},
	})

	results, err := db.Search("navbar")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].SessionID != "a" {
		t.Errorf("expected 1 result for 'navbar', got %d", len(results))
	}

	results, _ = db.Search("myproject")
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'myproject', got %d", len(results))
	}
}

func TestAllSessionsSQLInjection(t *testing.T) {
	db := testDB(t)

	// Insert some test data
	mustUpsert(t, db, []scanner.Session{
		{SessionID: "a", ProjectName: "test", ClaudeDir: "/test", FirstPrompt: "hello"},
		{SessionID: "b", ProjectName: "test2", ClaudeDir: "/test2", FirstPrompt: "world"},
	})

	// Attempt SQL injection via sortBy parameter
	malicious := "modified; DROP TABLE sessions--"
	sessions, err := db.AllSessions(malicious, true)
	if err != nil {
		t.Fatalf("AllSessions with malicious sortBy should not error, got: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}

	// Verify the sessions table still exists by querying it again
	sessions2, err := db.AllSessions("modified", true)
	if err != nil {
		t.Fatalf("sessions table should still exist after injection attempt: %v", err)
	}
	if len(sessions2) != 2 {
		t.Errorf("expected 2 sessions after injection attempt, got %d", len(sessions2))
	}
}

func TestPruneUnseen(t *testing.T) {
	db := testDB(t)

	mustUpsert(t, db, []scanner.Session{
		{SessionID: "a", ProjectName: "test", ClaudeDir: "/test"},
		{SessionID: "b", ProjectName: "test", ClaudeDir: "/test"},
		{SessionID: "c", ProjectName: "test", ClaudeDir: "/test"},
	})

	// Only "a" and "c" were seen in current scan
	pruned, err := db.PruneUnseen([]string{"a", "c"}, nil)
	if err != nil {
		t.Fatalf("PruneUnseen: %v", err)
	}
	if pruned != 1 {
		t.Errorf("pruned = %d, want 1", pruned)
	}

	all, _ := db.AllSessions("modified", true)
	if len(all) != 2 {
		t.Fatalf("got %d sessions after prune, want 2", len(all))
	}
	for _, s := range all {
		if s.SessionID == "b" {
			t.Error("session 'b' should have been pruned")
		}
	}
}

// A titled session whose transcript Claude Code deleted stays in the cache, so
// the title is not lost. Untitled ones, and any project no longer scanned, go.
func TestPruneUnseenRetainsTitled(t *testing.T) {
	db := testDB(t)

	mustUpsert(t, db, []scanner.Session{
		{SessionID: "seen", ProjectName: "p", ClaudeDir: "/c", ProjectPath: "/code/p", Title: "Seen"},
		{SessionID: "titled", ProjectName: "p", ClaudeDir: "/c", ProjectPath: "/code/p", Title: "Kept"},
		{SessionID: "titled-nested", ProjectName: "p", ClaudeDir: "/c", ProjectPath: "/code/p/sub", Title: "Kept too"},
		{SessionID: "untitled", ProjectName: "p", ClaudeDir: "/c", ProjectPath: "/code/p"},
		{SessionID: "out-of-scope", ProjectName: "q", ClaudeDir: "/c", ProjectPath: "/elsewhere/q", Title: "Dropped"},
	})

	// Only "seen" is still scannable; /code is the sole scan path.
	if _, err := db.PruneUnseen([]string{"seen"}, []string{"/code"}); err != nil {
		t.Fatalf("PruneUnseen: %v", err)
	}

	got := map[string]bool{}
	all, _ := db.AllSessions("modified", true)
	for _, s := range all {
		got[s.SessionID] = true
	}
	for _, id := range []string{"seen", "titled", "titled-nested"} {
		if !got[id] {
			t.Errorf("session %q should have been retained", id)
		}
	}
	for _, id := range []string{"untitled", "out-of-scope"} {
		if got[id] {
			t.Errorf("session %q should have been pruned", id)
		}
	}

	for _, s := range all {
		if s.SessionID == "titled" && s.Title != "Kept" {
			t.Errorf("retained title = %q, want Kept", s.Title)
		}
	}
}

// A path prefix must not match a sibling directory with a longer name.
func TestPruneUnseenPathPrefixIsNotSubstring(t *testing.T) {
	db := testDB(t)

	mustUpsert(t, db, []scanner.Session{
		{SessionID: "keep", ProjectName: "p", ClaudeDir: "/c", ProjectPath: "/code/p", Title: "In scope"},
		{SessionID: "sibling", ProjectName: "p", ClaudeDir: "/c", ProjectPath: "/code-private/p", Title: "Different tree"},
	})

	if _, err := db.PruneUnseen([]string{"none"}, []string{"/code"}); err != nil {
		t.Fatalf("PruneUnseen: %v", err)
	}

	all, _ := db.AllSessions("modified", true)
	if len(all) != 1 || all[0].SessionID != "keep" {
		ids := []string{}
		for _, s := range all {
			ids = append(ids, s.SessionID)
		}
		t.Errorf("retained %v, want only [keep]", ids)
	}
}

// findSession returns the session with the given id, or fails.
func findSession(t *testing.T, db *DB, id string) Session {
	t.Helper()
	all, err := db.AllSessions("modified", true)
	if err != nil {
		t.Fatalf("AllSessions: %v", err)
	}
	for _, s := range all {
		if s.SessionID == id {
			return s
		}
	}
	t.Fatalf("session %q not found", id)
	return Session{}
}

// A resolved scan writes worktree metadata; a later unresolved scan (worktree
// dir gone) preserves it; a later resolved non-worktree scan clears it.
func TestWorktreePreserveAndClear(t *testing.T) {
	db := testDB(t)

	// 1. Resolved worktree: metadata is stored.
	mustUpsert(t, db, []scanner.Session{{
		SessionID: "wt", ProjectName: "Code/proj/.claude/worktrees/pr-1", ClaudeDir: "/c",
		ProjectPath:      "/code/proj/.claude/worktrees/pr-1",
		WorktreeResolved: true, IsWorktree: true,
		RepoKey: "/code/proj/.git", ParentProjectName: "Code/proj", WorktreeName: "pr-1",
	}})
	s := findSession(t, db, "wt")
	if !s.IsWorktree || s.ParentProjectName != "Code/proj" || s.WorktreeName != "pr-1" {
		t.Fatalf("after resolved worktree: %+v", s)
	}

	// 2. Unresolved scan (dir gone): stored values are preserved.
	mustUpsert(t, db, []scanner.Session{{
		SessionID: "wt", ProjectName: "Code/proj/.claude/worktrees/pr-1", ClaudeDir: "/c",
		ProjectPath:      "/code/proj/.claude/worktrees/pr-1",
		WorktreeResolved: false,
	}})
	s = findSession(t, db, "wt")
	if !s.IsWorktree || s.ParentProjectName != "Code/proj" || s.WorktreeName != "pr-1" {
		t.Fatalf("after unresolved scan, expected preserved metadata: %+v", s)
	}

	// 3. Resolved non-worktree: stale metadata is cleared.
	mustUpsert(t, db, []scanner.Session{{
		SessionID: "wt", ProjectName: "Code/proj", ClaudeDir: "/c",
		ProjectPath:      "/code/proj",
		WorktreeResolved: true, IsWorktree: false,
	}})
	s = findSession(t, db, "wt")
	if s.IsWorktree || s.ParentProjectName != "" || s.WorktreeName != "" {
		t.Fatalf("after resolved non-worktree, expected cleared metadata: %+v", s)
	}
}
