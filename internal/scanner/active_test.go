package scanner

import (
	"testing"
	"time"
)

func TestMatchActiveEmpty(t *testing.T) {
	sessions := []Session{{SessionID: "a", ProjectPath: "/home/u/proj"}}
	if got := matchActive(sessions, nil); len(got) != 0 {
		t.Errorf("expected no active sessions, got %v", got)
	}
}

func TestMatchActiveResume(t *testing.T) {
	sessions := []Session{
		{SessionID: "known-1", ProjectPath: "/home/u/proj"},
		{SessionID: "known-2", ProjectPath: "/home/u/other"},
	}
	procs := []claudeProc{
		{pid: 100, resumeID: "known-1"},
		{pid: 101, resumeID: "ghost"}, // unknown ID must be ignored
	}

	got := matchActive(sessions, procs)
	if !got["known-1"] {
		t.Error("expected known-1 to be active via --resume")
	}
	if got["ghost"] {
		t.Error("unknown resume ID should not be active")
	}
	if len(got) != 1 {
		t.Errorf("expected exactly 1 active session, got %v", got)
	}
}

func TestMatchActiveFreshCwd(t *testing.T) {
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	sessions := []Session{
		{SessionID: "old", ProjectPath: "/home/u/proj", Modified: older},
		{SessionID: "new", ProjectPath: "/home/u/proj", Modified: newer},
	}
	// Fresh process (no --resume) in that directory: matches the most recently
	// modified session.
	procs := []claudeProc{{pid: 200, cwd: "/home/u/proj"}}

	got := matchActive(sessions, procs)
	if !got["new"] {
		t.Error("expected the most recently modified session to be active")
	}
	if got["old"] {
		t.Error("did not expect the older session to be active")
	}
}

func TestMatchActiveFreshNoCwdMatch(t *testing.T) {
	sessions := []Session{{SessionID: "a", ProjectPath: "/home/u/proj", Modified: time.Now()}}
	procs := []claudeProc{
		{pid: 300, cwd: "/somewhere/else"}, // no matching session dir
		{pid: 301, cwd: ""},                // cwd unavailable (e.g. lsof failed)
	}
	if got := matchActive(sessions, procs); len(got) != 0 {
		t.Errorf("expected no matches, got %v", got)
	}
}

func TestMatchActiveResumeAndFresh(t *testing.T) {
	now := time.Now()
	sessions := []Session{
		{SessionID: "resumed", ProjectPath: "/home/u/a", Modified: now},
		{SessionID: "fresh", ProjectPath: "/home/u/b", Modified: now},
	}
	procs := []claudeProc{
		{pid: 400, resumeID: "resumed", cwd: "/home/u/a"},
		{pid: 401, cwd: "/home/u/b"},
	}

	got := matchActive(sessions, procs)
	if !got["resumed"] || !got["fresh"] {
		t.Errorf("expected both resumed and fresh sessions active, got %v", got)
	}
}
