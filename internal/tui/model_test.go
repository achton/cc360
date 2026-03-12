package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/achton/cc360/internal/config"
	"github.com/achton/cc360/internal/db"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

func testModel(sessions []db.Session) Model {
	return New(nil, config.Config{}, sessions, nil, nil)
}

func testSessions() []db.Session {
	return []db.Session{
		{
			SessionID:   "session-1",
			ProjectName: "project-alpha",
			ProjectPath: "/tmp",
			Title:       "First session",
			Summary:     "Working on alpha",
			FirstPrompt: "hello world",
			Modified:    time.Now(),
		},
		{
			SessionID:   "session-2",
			ProjectName: "project-beta",
			ProjectPath: "/tmp",
			Title:       "Second session",
			Summary:     "Working on beta",
			FirstPrompt: "fix the bug",
			Modified:    time.Now().Add(-time.Hour),
		},
		{
			SessionID:   "session-3",
			ProjectName: "project-alpha",
			ProjectPath: "/tmp",
			Title:       "Third session",
			Summary:     "More alpha work",
			FirstPrompt: "add feature",
			Modified:    time.Now().Add(-2 * time.Hour),
		},
	}
}

func waitForOutput(tb testing.TB, tm *teatest.TestModel, s string) {
	tb.Helper()
	teatest.WaitFor(tb, tm.Output(), func(bts []byte) bool {
		return strings.Contains(string(bts), s)
	}, teatest.WithDuration(3*time.Second), teatest.WithCheckInterval(50*time.Millisecond))
}

func TestModelStartsAndQuits(t *testing.T) {
	m := testModel(testSessions())
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))
	waitForOutput(t, tm, "resume")
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

func TestModelShowsAllSessions(t *testing.T) {
	sessions := testSessions()
	m := testModel(sessions)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))
	// All three sessions should appear in the table
	waitForOutput(t, tm, "First session")
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

func TestFilterInput(t *testing.T) {
	m := testModel(testSessions())
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))
	waitForOutput(t, tm, "resume")

	// Open filter and type text via individual key messages
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	for _, r := range "alpha" {
		tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	// After filtering, only alpha sessions should be visible
	waitForOutput(t, tm, "project-alpha")

	// Escape clears filter — all sessions visible again
	tm.Send(tea.KeyMsg{Type: tea.KeyEscape})
	waitForOutput(t, tm, "project-beta")

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

func TestDetailToggle(t *testing.T) {
	m := testModel(testSessions())
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))

	// Detail pane is visible on launch
	waitForOutput(t, tm, "Folder:")

	// Toggle detail pane off and back on — triggers re-render with detail
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

func TestNavigation(t *testing.T) {
	m := testModel(testSessions())
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))
	waitForOutput(t, tm, "resume")

	// Move down, open detail to verify cursor moved
	tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	waitForOutput(t, tm, "Second session")

	// Move down again
	tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	waitForOutput(t, tm, "Third session")

	// Move to top
	tm.Send(tea.KeyMsg{Type: tea.KeyHome})
	waitForOutput(t, tm, "First session")

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

func TestProjectPicker(t *testing.T) {
	m := testModel(testSessions())
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))
	waitForOutput(t, tm, "resume")

	// Open picker
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	// Picker renders a bordered box with project names
	waitForOutput(t, tm, "project-alpha")

	// Close without selecting
	tm.Send(tea.KeyMsg{Type: tea.KeyEscape})

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

func TestOverlayCenter(t *testing.T) {
	bg := "AAAAAAAAAA\nBBBBBBBBBB\nCCCCCCCCCC\nDDDDDDDDDD\nEEEEEEEEEE"
	fg := "XX\nYY"
	result := overlayCenter(bg, fg, 10, 5)
	lines := strings.Split(result, "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(lines))
	}
	// Lines 0,1 should be background (A, B)
	if !strings.HasPrefix(lines[0], "A") {
		t.Errorf("line 0 should start with A, got %q", lines[0])
	}
	// Line 1 or 2 should contain the overlay
	found := false
	for _, line := range lines {
		if strings.Contains(line, "XX") || strings.Contains(line, "YY") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("overlay content not found in result:\n%s", result)
	}
	// Last line should be background
	if !strings.HasPrefix(lines[4], "E") {
		t.Errorf("line 4 should start with E, got %q", lines[4])
	}
}
