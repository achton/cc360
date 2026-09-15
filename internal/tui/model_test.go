package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/achton/cc360/internal/config"
	"github.com/achton/cc360/internal/db"
	"github.com/achton/cc360/internal/scanner"
	"github.com/charmbracelet/x/exp/teatest/v2"
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
			FirstPrompt: "hello world",
			Modified:    time.Now(),
		},
		{
			SessionID:   "session-2",
			ProjectName: "project-beta",
			ProjectPath: "/tmp",
			Title:       "Second session",
			FirstPrompt: "fix the bug",
			Modified:    time.Now().Add(-time.Hour),
		},
		{
			SessionID:   "session-3",
			ProjectName: "project-alpha",
			ProjectPath: "/tmp",
			Title:       "Third session",
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

// keyPress builds a printable key press. v2 replaced KeyMsg's Type/Runes with
// an interface plus Code/Text on KeyPressMsg.
func keyPress(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

// keyCode builds a non-printable key press, e.g. tea.KeyEscape.
func keyCode(c rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: c}
}

func TestModelStartsAndQuits(t *testing.T) {
	m := testModel(testSessions())
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))
	waitForOutput(t, tm, "resume")
	tm.Send(keyPress('q'))
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

func TestModelShowsAllSessions(t *testing.T) {
	sessions := testSessions()
	m := testModel(sessions)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))
	// All three sessions should appear in the table
	waitForOutput(t, tm, "First session")
	tm.Send(keyPress('q'))
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

func TestFilterInput(t *testing.T) {
	m := testModel(testSessions())
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))
	waitForOutput(t, tm, "resume")

	// Open filter and type text via individual key messages
	tm.Send(keyPress('/'))
	for _, r := range "alpha" {
		tm.Send(keyPress(r))
	}
	// Assert on the session count, not on rows. v2's renderer emits only what
	// changed, and the alpha rows were already painted, so they are never
	// re-sent. The count line does change.
	waitForOutput(t, tm, "2/3 sessions")

	// Escape clears the filter, which repaints the beta row.
	tm.Send(keyCode(tea.KeyEscape))
	waitForOutput(t, tm, "project-beta")

	tm.Send(keyPress('q'))
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

func TestDetailToggle(t *testing.T) {
	m := testModel(testSessions())
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))

	// Detail pane is visible on launch
	waitForOutput(t, tm, "Folder:")

	// Toggle detail pane off and back on — triggers re-render with detail
	tm.Send(keyCode(tea.KeyTab))
	tm.Send(keyCode(tea.KeyTab))

	tm.Send(keyPress('q'))
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

func TestNavigation(t *testing.T) {
	m := testModel(testSessions())
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))
	waitForOutput(t, tm, "resume")

	// Move down, open detail to verify cursor moved
	tm.Send(keyCode(tea.KeyDown))
	tm.Send(keyCode(tea.KeyTab))
	waitForOutput(t, tm, "Second session")

	// Move down again
	tm.Send(keyCode(tea.KeyDown))
	waitForOutput(t, tm, "Third session")

	// Move to top
	tm.Send(keyCode(tea.KeyHome))
	waitForOutput(t, tm, "First session")

	tm.Send(keyPress('q'))
	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))
}

func TestProjectPicker(t *testing.T) {
	m := testModel(testSessions())
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 40))
	waitForOutput(t, tm, "resume")

	// Open picker
	tm.Send(keyPress('p'))
	// Picker renders a bordered box with project names
	waitForOutput(t, tm, "project-alpha")

	// Close without selecting
	tm.Send(keyCode(tea.KeyEscape))

	tm.Send(keyPress('q'))
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

// Which sessions survive a text filter, asserted on model state so it does not
// depend on what the renderer chose to repaint.
func TestApplyFiltersSelectsMatchingSessions(t *testing.T) {
	tests := []struct {
		query string
		want  []string
	}{
		{"", []string{"session-1", "session-2", "session-3"}},
		{"alpha", []string{"session-1", "session-3"}},
		{"beta", []string{"session-2"}},
		{"Second session", []string{"session-2"}}, // matches Title
		{"fix the bug", []string{"session-2"}},    // matches FirstPrompt
		{"nothing matches this", nil},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			m := testModel(testSessions())
			m.filter.open()
			m.filter.input.SetValue(tt.query)
			m.applyFilters()

			var got []string
			for _, s := range m.sessions {
				got = append(got, s.SessionID)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("query %q selected %v, want %v", tt.query, got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("query %q selected %v, want %v", tt.query, got, tt.want)
					break
				}
			}
		})
	}
}

// The picker overlay must land exactly on the requested width, or it pushes
// past the terminal edge. Under lipgloss v1 the border was added on top of
// Width, making the box 2 columns too wide.
func TestPickerViewRespectsWidth(t *testing.T) {
	p := projectPicker{}
	p.open(testSessions(), nil)

	for _, want := range []int{40, 60, 100} {
		out := p.view(want, 20)
		if got := lipgloss.Width(out); got != want {
			t.Errorf("picker width = %d, want %d", got, want)
		}
	}
}

// cc360 does not control the CLAUDE_CONFIG_DIR of the shell the command is
// pasted into, so a known home is always pinned.
func TestResumeShellCommand(t *testing.T) {
	tests := []struct {
		name      string
		claudeDir string
		want      string
	}{
		{
			name:      "default home",
			claudeDir: "/home/u/.claude/projects/-home-u-proj",
			want:      "cd '/home/u/proj' && CLAUDE_CONFIG_DIR='/home/u/.claude' claude --resume 'abc'",
		},
		{
			name:      "second home",
			claudeDir: "/home/u/.claude-personal/projects/-home-u-proj",
			want:      "cd '/home/u/proj' && CLAUDE_CONFIG_DIR='/home/u/.claude-personal' claude --resume 'abc'",
		},
		{
			name:      "unrecognisable claude dir inherits the environment",
			claudeDir: "/test",
			want:      "cd '/home/u/proj' && claude --resume 'abc'",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &db.Session{SessionID: "abc", ProjectPath: "/home/u/proj", ClaudeDir: tt.claudeDir}
			if got := resumeShellCommand(s); got != tt.want {
				t.Errorf("resumeShellCommand() = %q, want %q", got, tt.want)
			}
		})
	}
}

// The poll spawns a claude process per Claude home. Update must hand that to a
// command instead of running it, or the table freezes until every one returns.
func TestActiveTickDoesNotPollInline(t *testing.T) {
	m := testModel(testSessions())
	m.cfg.ShowActive = true
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)

	updated, cmd := m.Update(activeTickMsg{})
	if cmd == nil {
		t.Fatal("activeTickMsg returned no command, so no poll was scheduled")
	}
	if got := updated.(Model).activeStates; got != nil {
		t.Errorf("activeStates = %v, want nil until the result arrives", got)
	}

	m.cfg.ShowActive = false
	if _, cmd := m.Update(activeTickMsg{}); cmd != nil {
		t.Error("indicators are off, so no poll must be scheduled")
	}
}

func TestActiveResultAppliesStates(t *testing.T) {
	m := testModel(testSessions())
	m.cfg.ShowActive = true
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)

	want := map[string]scanner.ActiveState{m.sessions[0].SessionID: scanner.StateBusy}
	updated, cmd := m.Update(activeResultMsg{states: want})
	if cmd == nil {
		t.Error("no command returned, so the poll loop stopped")
	}
	if got := updated.(Model).activeStates[m.sessions[0].SessionID]; got != scanner.StateBusy {
		t.Errorf("state = %v, want StateBusy", got)
	}
}

// v2 delivers paste as tea.PasteMsg, which no longer reaches the filter on its own.
func TestPasteReachesFilter(t *testing.T) {
	m := testModel(testSessions())
	m.filter.open()

	updated, _ := m.Update(tea.PasteMsg{Content: "alpha"})
	got := updated.(*Model)

	if got.filter.value() != "alpha" {
		t.Errorf("filter value = %q, want %q", got.filter.value(), "alpha")
	}
	if len(got.sessions) != 2 {
		t.Errorf("paste selected %d sessions, want 2", len(got.sessions))
	}
}

// The pane sits in a fixed height budget, so long text must not grow it.
func TestDetailPaneKeepsItsSize(t *testing.T) {
	long := &db.Session{
		SessionID:   "abcdefgh-1234-5678-9012-abcdefabcdef",
		ProjectName: "-home-user-Code-a-very-long-project-name-that-keeps-going",
		ProjectPath: "/home/user/Code/a-very-long-project-name-that-keeps-going",
		Title:       strings.Repeat("title ", 40),
		FirstPrompt: strings.Repeat("prompt ", 60),
		GitBranch:   strings.Repeat("branch-", 20),
		Modified:    time.Now(),
	}
	d := detailPane{visible: true}

	for _, width := range []int{40, 80, 120} {
		for _, s := range []*db.Session{nil, long} {
			out := d.view(s, width, scanner.StateBusy)
			if got := lipgloss.Width(out); got != width {
				t.Errorf("width %d: rendered %d columns", width, got)
			}
			if got, want := lipgloss.Height(out), detailContentLines+2; got != want {
				t.Errorf("width %d: rendered %d lines, want %d", width, got, want)
			}
		}
	}
}

// These keys are matched by name now, and a wrong name is silently inert.
func TestPickerKeysAreNamedCorrectly(t *testing.T) {
	// A slash in the project name creates a group row to expand and collapse.
	grouped := []db.Session{
		{SessionID: "s1", ProjectName: "repo/one", ProjectPath: "/tmp"},
		{SessionID: "s2", ProjectName: "repo/two", ProjectPath: "/tmp"},
	}
	m := testModel(grouped)
	m.picker.open(grouped, nil)
	expanded := len(m.picker.flat)

	updated, _ := m.Update(keyCode(tea.KeyLeft))
	m = *updated.(*Model)
	if len(m.picker.flat) >= expanded {
		t.Errorf("left did not collapse the group: %d rows, was %d", len(m.picker.flat), expanded)
	}

	updated, _ = m.Update(keyCode(tea.KeyRight))
	m = *updated.(*Model)
	if len(m.picker.flat) != expanded {
		t.Errorf("right did not expand the group: %d rows, want %d", len(m.picker.flat), expanded)
	}

	updated, _ = m.Update(keyCode(tea.KeySpace))
	m = *updated.(*Model)
	if len(m.picker.selectedProjects()) == 0 {
		t.Error("space selected nothing")
	}
}

// Matched by name too, while the filter holds focus.
func TestFilterArrowsMoveTheTable(t *testing.T) {
	m := testModel(testSessions())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)
	m.filter.open()

	updated, _ = m.Update(keyCode(tea.KeyDown))
	if got := updated.(*Model).table.Cursor(); got != 1 {
		t.Errorf("down moved the cursor to %d, want 1", got)
	}

	m = *updated.(*Model)
	updated, _ = m.Update(keyCode(tea.KeyUp))
	if got := updated.(*Model).table.Cursor(); got != 0 {
		t.Errorf("up moved the cursor to %d, want 0", got)
	}
}
