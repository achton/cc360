package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/achton/cc360/internal/config"
	"github.com/achton/cc360/internal/db"
	"github.com/achton/cc360/internal/scanner"
)

func TestVisibleSessionsHidesSidechains(t *testing.T) {
	dir := t.TempDir()
	all := []db.Session{
		{SessionID: "main", ProjectPath: dir},
		{SessionID: "side", ProjectPath: dir, IsSidechain: true},
	}

	if got := VisibleSessions(all, config.Config{HideSidechains: true}); len(got) != 1 || got[0].SessionID != "main" {
		t.Errorf("hidden: got %+v, want only main", got)
	}
	if got := VisibleSessions(all, config.Config{HideSidechains: false}); len(got) != 2 {
		t.Errorf("shown: got %d sessions, want 2", len(got))
	}
}

// A second load must give the same result from the cache as the first parse.
func TestLoadSessionsTwice(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "proj")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	dir := filepath.Join(home, "projects", strings.ReplaceAll(project, "/", "-"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"parentUuid":null,"type":"user","sessionId":"s1","cwd":"` + project + `","message":{"role":"user","content":"hello"},"timestamp":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, "s1.jsonl"), []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	database, err := db.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	cfg := config.Config{ScanPaths: []string{root}, ClaudeHomes: []string{home}, ScanOrphans: true, SortBy: "modified"}

	for i := range 2 {
		got, err := LoadSessions(database, cfg)
		if err != nil {
			t.Fatalf("load %d: %v", i, err)
		}
		if len(got) != 1 || got[0].FirstPrompt != "hello" || got[0].MessageCount != 1 {
			t.Fatalf("load %d: got %+v", i, got)
		}
	}
	if cache, _ := database.ScanCache(scanner.ParserVersion); len(cache) != 1 {
		t.Errorf("cache has %d entries after a load, want 1", len(cache))
	}
}

func TestVisibleSessionsHidesSDKRuns(t *testing.T) {
	dir := t.TempDir()
	all := []db.Session{
		{SessionID: "cli", ProjectPath: dir, Entrypoint: "cli"},
		{SessionID: "desktop", ProjectPath: dir, Entrypoint: "claude-desktop"},
		{SessionID: "print", ProjectPath: dir, Entrypoint: "sdk-cli"},
		{SessionID: "legacy", ProjectPath: dir},
	}
	got := VisibleSessions(all, config.Config{})
	if len(got) != 3 {
		t.Fatalf("got %d sessions, want the sdk-cli run hidden", len(got))
	}
	for _, s := range got {
		if s.SessionID == "print" {
			t.Error("sdk-cli session is visible")
		}
	}
}

func TestActivityLine(t *testing.T) {
	s := &db.Session{PRLinks: []string{"o/a#1", "o/b#2"}, CostUSD: 1.234, LinesAdded: 5, Entrypoint: "claude-vscode"}
	want := "PR: o/b#2 (+1)  Cost: $1.23  Lines: +5 -0  Origin: VS Code"
	if got := activityLine(s); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := activityLine(&db.Session{Entrypoint: "cli"}); got != "" {
		t.Errorf("plain CLI session: got %q, want no line", got)
	}
}
