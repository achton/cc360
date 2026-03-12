package db

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/achton/cc360/internal/scanner"
)

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

func TestUpsertAndQuery(t *testing.T) {
	db := testDB(t)

	sessions := []scanner.Session{
		{
			SessionID:   "abc-123",
			ProjectName: "Code/myproject",
			ProjectPath: "/home/user/Code/myproject",
			ClaudeDir:   "/home/user/.claude/projects/test",
			FirstPrompt: "fix the bug",
			MessageCount: 5,
			Created:     time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			Modified:    time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
			GitBranch:   "main",
			JSONLPath:   "/tmp/abc.jsonl",
		},
		{
			SessionID:   "def-456",
			ProjectName: "Code/other",
			ProjectPath: "/home/user/Code/other",
			ClaudeDir:   "/home/user/.claude/projects/test2",
			FirstPrompt: "add feature",
			MessageCount: 10,
			Created:     time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC),
			Modified:    time.Date(2025, 1, 4, 0, 0, 0, 0, time.UTC),
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
	db.Upsert(s1)

	s2 := []scanner.Session{{
		SessionID:   "abc-123",
		ProjectName: "test",
		ClaudeDir:   "/test",
		GitBranch:   "feature-new",
	}}
	db.Upsert(s2)

	all, _ := db.AllSessions("modified", true)
	if all[0].GitBranch != "feature-new" {
		t.Errorf("git_branch = %q, want feature-new", all[0].GitBranch)
	}
}

func TestUpsertPreservesSummary(t *testing.T) {
	db := testDB(t)

	s := []scanner.Session{{
		SessionID:   "abc-123",
		ProjectName: "test",
		ClaudeDir:   "/test",
	}}
	db.Upsert(s)
	db.SetSummary("abc-123", "My Title", "My summary")

	// Re-upsert
	db.Upsert(s)

	all, _ := db.AllSessions("modified", true)
	if all[0].Title != "My Title" {
		t.Errorf("title = %q, want My Title", all[0].Title)
	}
	if all[0].Summary != "My summary" {
		t.Errorf("summary = %q, want My summary", all[0].Summary)
	}
}

func TestSearch(t *testing.T) {
	db := testDB(t)

	db.Upsert([]scanner.Session{
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

func TestUnsummarized(t *testing.T) {
	db := testDB(t)

	db.Upsert([]scanner.Session{
		{SessionID: "a", ProjectName: "test", ClaudeDir: "/test", JSONLPath: "/tmp/a.jsonl"},
		{SessionID: "b", ProjectName: "test", ClaudeDir: "/test", JSONLPath: "/tmp/b.jsonl"},
		{SessionID: "c", ProjectName: "test", ClaudeDir: "/test"}, // no JSONL
	})
	db.SetSummary("a", "Title A", "Summary A")

	unsummarized, err := db.Unsummarized(10)
	if err != nil {
		t.Fatalf("Unsummarized: %v", err)
	}
	if len(unsummarized) != 1 || unsummarized[0].SessionID != "b" {
		t.Errorf("expected [b], got %v", unsummarized)
	}
}

func TestAllSessionsSQLInjection(t *testing.T) {
	db := testDB(t)

	// Insert some test data
	db.Upsert([]scanner.Session{
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

func TestUnsummarizedWithLimit(t *testing.T) {
	db := testDB(t)

	db.Upsert([]scanner.Session{
		{SessionID: "a", ProjectName: "test", ClaudeDir: "/test", JSONLPath: "/tmp/a.jsonl",
			Modified: time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC)},
		{SessionID: "b", ProjectName: "test", ClaudeDir: "/test", JSONLPath: "/tmp/b.jsonl",
			Modified: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)},
		{SessionID: "c", ProjectName: "test", ClaudeDir: "/test", JSONLPath: "/tmp/c.jsonl",
			Modified: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},
	})

	// Limit to 2 results
	unsummarized, err := db.Unsummarized(2)
	if err != nil {
		t.Fatalf("Unsummarized: %v", err)
	}
	if len(unsummarized) != 2 {
		t.Errorf("expected 2 unsummarized sessions with limit 2, got %d", len(unsummarized))
	}

	// No limit (0)
	all, err := db.Unsummarized(0)
	if err != nil {
		t.Fatalf("Unsummarized(0): %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 unsummarized sessions with no limit, got %d", len(all))
	}
}

func TestPruneUnseen(t *testing.T) {
	db := testDB(t)

	db.Upsert([]scanner.Session{
		{SessionID: "a", ProjectName: "test", ClaudeDir: "/test"},
		{SessionID: "b", ProjectName: "test", ClaudeDir: "/test"},
		{SessionID: "c", ProjectName: "test", ClaudeDir: "/test"},
	})

	// Only "a" and "c" were seen in current scan
	pruned, err := db.PruneUnseen([]string{"a", "c"})
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
