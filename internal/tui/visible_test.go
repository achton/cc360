package tui

import (
	"testing"

	"github.com/achton/cc360/internal/config"
	"github.com/achton/cc360/internal/db"
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
