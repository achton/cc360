package tui

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/achton/cc360/internal/config"
	"github.com/achton/cc360/internal/db"
	"github.com/achton/cc360/internal/scanner"
	"github.com/achton/cc360/internal/update"
	"github.com/charmbracelet/x/ansi"
)

// Model is the top-level Bubbletea model.
type Model struct {
	db            *db.DB
	cfg           config.Config
	allSessions   []db.Session // full unfiltered list
	sessions      []db.Session // currently displayed (filtered)
	activeStates  map[string]scanner.Agent
	table         sessionTable
	detail        detailPane
	filter        filterInput
	picker        projectPicker
	projectFilter map[string]bool // if non-empty, only show sessions from these projects
	tableInit     bool
	keys          keyMap
	width         int
	height        int
	statusMsg     string

	version        string         // this build, for the update check
	update         update.Release // a newer release, when one was found
	upgradeCommand string
}

type execFinishedMsg struct{ err error }

type updateFoundMsg struct {
	release update.Release
	command string
}

type activeTickMsg struct{}

type activeResultMsg struct {
	states map[string]scanner.Agent
}

type reloadResultMsg struct {
	cfg      config.Config
	sessions []db.Session
	err      error
}

const activePollInterval = 15 * time.Second

func activeTickCmd() tea.Cmd {
	return tea.Tick(activePollInterval, func(time.Time) tea.Msg {
		return activeTickMsg{}
	})
}

// New creates the initial TUI model.
func New(database *db.DB, cfg config.Config, sessions []db.Session, activeStates map[string]scanner.Agent) Model {
	return Model{
		db:           database,
		cfg:          cfg,
		allSessions:  sessions,
		sessions:     sessions,
		activeStates: activeStates,
		detail:       detailPane{visible: true},
		filter:       newFilterInput(),
		keys:         newKeyMap(),
		tableInit:    false,
	}
}

// WithVersion sets this build's version, which the update check compares.
func (m Model) WithVersion(v string) Model {
	m.version = v
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.pollActiveCmd(), m.updateCheckCmd())
}

const updateCheckTimeout = 10 * time.Second

// updateCheckCmd looks for a newer release off the UI loop. The answer is
// cached for a day, so most launches do not touch the network.
func (m Model) updateCheckCmd() tea.Cmd {
	if !m.cfg.CheckUpdates || m.version == "" {
		return nil
	}
	current := m.version
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
		defer cancel()
		rel, ok := update.NewChecker(config.CacheDir()).Newer(ctx, current)
		if !ok {
			return nil
		}
		exe, err := os.Executable()
		if err != nil {
			return nil
		}
		return updateFoundMsg{release: rel, command: update.Command(exe, rel.Version)}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if !m.tableInit {
			m.table = newSessionTable(m.sessions, m.width, m.tableHeight(), m.activeStates)
			m.tableInit = true
		} else {
			m.table.resize(m.sessions, m.width, m.tableHeight())
		}
		return m, nil

	case tea.PasteMsg:
		// v2 delivers bracketed paste as its own message, not as a key press.
		if m.filter.focused() {
			return m.sendToFilter(msg)
		}
		return m, nil

	case tea.KeyPressMsg:
		// When picker is active, handle picker keys
		if m.picker.active {
			return m.updatePicker(msg)
		}

		// When filter input is focused, route most keys to it
		if m.filter.focused() {
			return m.updateFilter(msg)
		}

		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, m.keys.Up):
			m.table.MoveUp(1)
			return m, nil

		case key.Matches(msg, m.keys.Down):
			m.table.MoveDown(1)
			return m, nil

		case key.Matches(msg, m.keys.PageUp):
			m.table.PageUp()
			return m, nil

		case key.Matches(msg, m.keys.PageDown):
			m.table.PageDown()
			return m, nil

		case key.Matches(msg, m.keys.Top):
			m.table.GotoTop()
			return m, nil

		case key.Matches(msg, m.keys.Bottom):
			m.table.GotoBottom()
			return m, nil

		case key.Matches(msg, m.keys.Detail):
			m.detail.toggle()
			m.table.setHeight(m.tableHeight())
			return m, nil

		case key.Matches(msg, m.keys.Resume):
			return m, m.resumeSession()

		case key.Matches(msg, m.keys.Copy):
			m.copyResumeCommand()
			return m, nil

		case key.Matches(msg, m.keys.Filter):
			if !m.filter.visible() {
				m.filter.open()
				m.table.setHeight(m.tableHeight())
			}
			return m, m.filter.input.Focus()

		case key.Matches(msg, m.keys.Picker):
			m.picker.open(m.allSessions, m.projectFilter)
			return m, nil

		case key.Matches(msg, m.keys.Upgrade):
			m.copyUpgradeCommand()
			return m, nil

		case key.Matches(msg, m.keys.Reload):
			m.statusMsg = "Reloading..."
			return m, m.reloadCmd()

		case key.Matches(msg, m.keys.Escape):
			m.clearFilters()
			return m, nil
		}

	case reloadResultMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Reload error: %v", msg.err)
			return m, nil
		}
		m.cfg = msg.cfg
		m.allSessions = msg.sessions
		// Clear filters and rebuild
		m.filter.close()
		m.projectFilter = nil
		m.sessions = m.allSessions
		m.rebuildTable()
		m.statusMsg = fmt.Sprintf("Reloaded — %d sessions", len(m.allSessions))
		return m, m.pollActiveCmd()

	case updateFoundMsg:
		m.update = msg.release
		m.upgradeCommand = msg.command
		return m, nil

	case activeTickMsg:
		return m, m.pollActiveCmd()

	case activeResultMsg:
		m.activeStates = msg.states
		m.table.activeStates = msg.states
		m.table.rows = buildRows(m.sessions, m.width, m.table.columns, msg.states)
		// Space the next poll from this result, so two never overlap.
		return m, activeTickCmd()

	case execFinishedMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Process error: %v", msg.err)
		} else {
			m.statusMsg = "Returned to cc360"
		}
		return m, nil
	}

	return m, nil
}

func (m *Model) updatePicker(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Escape), key.Matches(msg, m.keys.Quit), key.Matches(msg, m.keys.Picker):
		m.picker.close()
		return m, nil

	case key.Matches(msg, m.keys.Up):
		m.picker.moveUp()
		return m, nil

	case key.Matches(msg, m.keys.Down):
		m.picker.moveDown()
		return m, nil

	case key.Matches(msg, m.keys.Resume): // Enter — apply selection
		m.picker.close()
		selected := m.picker.selectedProjects()
		if len(selected) == 0 {
			m.projectFilter = nil
		} else {
			m.projectFilter = make(map[string]bool, len(selected))
			for _, p := range selected {
				m.projectFilter[p] = true
			}
		}
		m.applyFilters()
		return m, nil

	case msg.String() == "space":
		m.picker.toggleSelect()
		return m, nil

	case msg.String() == "right":
		m.picker.expand()
		return m, nil

	case msg.String() == "left":
		m.picker.collapse()
		return m, nil
	}

	return m, nil
}

func (m *Model) updateFilter(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Escape):
		m.filter.close()
		m.applyFilters()
		return m, nil

	case key.Matches(msg, m.keys.Resume):
		// Enter: stop typing but keep filter visible
		m.filter.input.Blur()
		return m, nil

	case msg.String() == "up":
		m.table.MoveUp(1)
		return m, nil

	case msg.String() == "down":
		m.table.MoveDown(1)
		return m, nil
	}

	return m.sendToFilter(msg)
}

func (m *Model) sendToFilter(msg tea.Msg) (tea.Model, tea.Cmd) {
	prevValue := m.filter.value()
	var cmd tea.Cmd
	m.filter.input, cmd = m.filter.input.Update(msg)

	if m.filter.value() != prevValue {
		m.applyFilters()
	}

	return m, cmd
}

// applyFilters rebuilds m.sessions from allSessions applying project filter + text filter.
func (m *Model) applyFilters() {
	result := m.allSessions

	// Apply project filter first
	if len(m.projectFilter) > 0 {
		filtered := make([]db.Session, 0)
		for _, s := range result {
			if m.projectFilter[s.ProjectName] {
				filtered = append(filtered, s)
			}
		}
		result = filtered
	}

	// Apply text filter
	query := strings.ToLower(m.filter.value())
	if query != "" {
		filtered := make([]db.Session, 0)
		for _, s := range result {
			text := strings.ToLower(s.ProjectName + " " + s.ParentProjectName +
				" " + s.WorktreeName + " " + s.CustomTitle + " " + s.Title +
				" " + s.FirstPrompt + " " + s.GitBranch + " " + s.ExistingSummary +
				" " + strings.Join(s.PRLinks, " "))
			if strings.Contains(text, query) {
				filtered = append(filtered, s)
			}
		}
		result = filtered
	}

	m.sessions = result
	m.rebuildTable()
	m.updateStatusFromFilters()
}

// clearFilters clears the text filter only. Project filter is managed via the picker.
func (m *Model) clearFilters() {
	if m.filter.value() != "" {
		m.filter.close()
		m.applyFilters()
		return
	}
	m.statusMsg = ""
}

func (m *Model) updateStatusFromFilters() {
	var parts []string
	if len(m.projectFilter) > 0 {
		if len(m.projectFilter) <= 3 {
			names := make([]string, 0, len(m.projectFilter))
			for name := range m.projectFilter {
				names = append(names, name)
			}
			sort.Strings(names)
			parts = append(parts, strings.Join(names, ", "))
		} else {
			parts = append(parts, fmt.Sprintf("%d projects", len(m.projectFilter)))
		}
	}
	if m.filter.value() != "" {
		parts = append(parts, "\""+m.filter.value()+"\"")
	}
	if len(parts) > 0 {
		m.statusMsg = fmt.Sprintf("%d/%d sessions — %s (esc to clear)",
			len(m.sessions), len(m.allSessions), strings.Join(parts, " + "))
	} else {
		m.statusMsg = ""
	}
}

func (m *Model) rebuildTable() {
	m.table.rows = buildRows(m.sessions, m.width, m.table.columns, m.activeStates)
	m.table.SetCursor(0)
	m.table.setHeight(m.tableHeight())
}

// View builds the frame. v2 declares terminal state such as AltScreen on the
// view, not as a program option.
func (m Model) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true

	if m.width == 0 {
		v.SetContent("Loading...")
		return v
	}

	headerText := headerAppStyle.Render("cc360") + headerTagStyle.Render(" // built by @achton")
	if m.update.Version != "" {
		headerText += headerUpdateStyle.Render("  ↑ v" + m.update.Version + " available · u to upgrade")
	}
	// A wrapped header would push the table down a line.
	headerText = ansi.Truncate(headerText, m.width-headerStyle.GetHorizontalFrameSize(), "…")
	header := headerStyle.Width(m.width).Render(headerText)

	if len(m.sessions) != len(m.allSessions) {
		m.table.sessionInfo = fmt.Sprintf("%d/%d sessions", len(m.sessions), len(m.allSessions))
	} else {
		m.table.sessionInfo = fmt.Sprintf("%d sessions", len(m.allSessions))
	}

	var filterLine string
	if m.filter.visible() {
		filterLine = m.filter.view(m.width) + "\n"
	}

	tbl := m.table.View()

	var detail string
	if m.detail.visible {
		s := m.selectedSession()
		detail = "\n" + m.detail.view(s, m.width, m.agent(s))
	}

	statusText := m.statusMsg
	// Truncate to prevent wrapping (padding takes 2 chars of horizontal space)
	if maxW := m.width - 2; maxW > 0 {
		statusText = ansi.Truncate(statusText, maxW, "…")
	}
	status := statusStyle.Width(m.width).Render(statusText)

	help := m.renderHelp()

	base := fmt.Sprintf("%s\n%s%s%s\n%s\n%s", header, filterLine, tbl, detail, status, help)

	if m.picker.active {
		pickerHeight := m.height * 2 / 3
		if pickerHeight < 8 {
			pickerHeight = 8
		}
		pickerWidth := m.width * 3 / 4
		if pickerWidth < 40 {
			pickerWidth = 40
		}
		if pickerWidth > m.width {
			pickerWidth = m.width
		}
		box := m.picker.view(pickerWidth, pickerHeight)
		base = overlayCenter(base, box, m.width, m.height)
	}

	v.SetContent(base)
	return v
}

func (m Model) renderHelp() string {
	pairs := []struct{ key, desc string }{
		{"enter", "resume"},
		{"tab", "detail"},
		{"/", "filter"},
		{"p", "projects"},
		{"c", "copy cmd"},
		{"r", "reload"},
		{"q", "quit"},
	}
	if m.update.Version != "" {
		pairs = append(pairs[:len(pairs)-1], struct{ key, desc string }{"u", "upgrade"}, pairs[len(pairs)-1])
	}
	sep := helpSepStyle.Render(" · ")
	var s string
	for i, p := range pairs {
		if i > 0 {
			s += sep
		}
		s += helpKeyStyle.Render(p.key) + " " + helpDescStyle.Render(p.desc)
	}
	return helpStyle.Width(m.width).Render(s)
}

func (m Model) tableHeight() int {
	// Chrome: header(1) + status(1) + help(1) = 3 fixed lines
	h := m.height - 3
	if m.detail.visible {
		// detail pane rendered height includes border
		h -= lipgloss.Height(m.detail.view(m.selectedSession(), m.width, scanner.Agent{}))
	}
	if m.filter.visible() {
		h -= lipgloss.Height(m.filter.view(m.width))
	}
	if h < 4 {
		h = 4
	}
	return h
}

func (m *Model) selectedSession() *db.Session {
	cursor := m.table.Cursor()
	if cursor >= 0 && cursor < len(m.sessions) {
		return &m.sessions[cursor]
	}
	return nil
}

func (m *Model) agent(s *db.Session) scanner.Agent {
	if s == nil {
		return scanner.Agent{}
	}
	return m.activeStates[s.SessionID]
}

// openArgs returns the claude arguments that open s. A running background job
// is attached to; an interactive session running in another terminal cannot
// be taken over, so that returns a reason instead.
func openArgs(s *db.Session, a scanner.Agent) ([]string, string) {
	switch {
	case a.State == scanner.StateNone:
		return []string{"--resume", s.SessionID}, ""
	case a.Background && a.JobID != "" && isValidSessionID(a.JobID):
		return []string{"attach", a.JobID}, ""
	case a.Background:
		// Since Claude Code 2.1.285 --resume opens a running background session.
		return []string{"--resume", s.SessionID}, ""
	case a.PID > 0:
		return nil, fmt.Sprintf("Session is running in another terminal (pid %d)", a.PID)
	default:
		return nil, "Session is running in another terminal"
	}
}

// openableSession returns the selected session and the arguments that open
// it, or sets the status line and returns nil.
func (m *Model) openableSession() (*db.Session, []string) {
	s := m.selectedSession()
	if s == nil {
		m.statusMsg = "No session selected"
		return nil, nil
	}
	args, reason := openArgs(s, m.agent(s))
	if reason != "" {
		m.statusMsg = reason
		return nil, nil
	}
	if !m.checkProjectPath(s) {
		return nil, nil
	}
	if !isValidSessionID(s.SessionID) {
		m.statusMsg = "Invalid session ID"
		return nil, nil
	}
	return s, args
}

// pollActiveCmd queries running sessions off the UI loop. Each query spawns a
// claude process per home, which would otherwise freeze the table for as long
// as they take. Returns nil when the user turned the indicators off.
func (m *Model) pollActiveCmd() tea.Cmd {
	if !m.cfg.ShowActive {
		return nil
	}
	homes := m.cfg.ClaudeHomes
	return func() tea.Msg {
		return activeResultMsg{states: scanner.ActiveSessions(homes)}
	}
}

func (m *Model) reloadCmd() tea.Cmd {
	database := m.db
	return func() tea.Msg {
		cfg, shouldExit, err := config.Load()
		if err != nil || shouldExit {
			if err == nil {
				err = fmt.Errorf("invalid config")
			}
			return reloadResultMsg{err: err}
		}

		sessions, err := LoadSessions(database, cfg)
		if err != nil {
			return reloadResultMsg{err: err}
		}
		return reloadResultMsg{cfg: cfg, sessions: sessions}
	}
}

func (m *Model) checkProjectPath(s *db.Session) bool {
	if s.ProjectPath == "" {
		m.statusMsg = "No project path for this session"
		return false
	}
	if _, err := os.Stat(s.ProjectPath); err != nil {
		m.statusMsg = fmt.Sprintf("Project path no longer exists: %s", s.ProjectPath)
		return false
	}
	return true
}

func (m *Model) resumeSession() tea.Cmd {
	s, args := m.openableSession()
	if s == nil {
		return nil
	}
	c := exec.Command("claude", args...)
	c.Dir = s.ProjectPath
	// Pin the session's config dir. claude only resumes sessions of the config
	// it runs under.
	c.Env = config.ClaudeEnv(config.ClaudeHome(s.ClaudeDir))
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return execFinishedMsg{err: err}
	})
}

// copyUpgradeCommand puts the command that installs the newer release on the
// clipboard and shows it, so it can also be read off the screen.
func (m *Model) copyUpgradeCommand() {
	if m.upgradeCommand == "" {
		m.statusMsg = "No newer cc360 release found"
		return
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(m.upgradeCommand))
	fmt.Fprintf(os.Stderr, "\033]52;c;%s\007", encoded)
	m.statusMsg = "Copied: " + m.upgradeCommand
}

// overlayCenter composites the overlay box centered on top of the base view,
// preserving the background content around the overlay with ANSI-aware truncation.
func overlayCenter(base, overlay string, width, height int) string {
	fgLines := strings.Split(overlay, "\n")
	bgLines := strings.Split(base, "\n")

	fgW, fgH := lipgloss.Size(overlay)
	bgH := len(bgLines)

	// Center the overlay
	x := (width - fgW) / 2
	y := (bgH - fgH) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	var sb strings.Builder
	for i, bgLine := range bgLines {
		if i > 0 {
			sb.WriteByte('\n')
		}
		if i < y || i >= y+fgH {
			sb.WriteString(bgLine)
			continue
		}

		pos := 0
		if x > 0 {
			left := ansi.Truncate(bgLine, x, "")
			pos = ansi.StringWidth(left)
			sb.WriteString(left)
			if pos < x {
				sb.WriteString(strings.Repeat(" ", x-pos))
				pos = x
			}
		}

		fgLine := fgLines[i-y]
		sb.WriteString(fgLine)
		pos += ansi.StringWidth(fgLine)

		right := ansi.TruncateLeft(bgLine, pos, "")
		bgW := ansi.StringWidth(bgLine)
		rightW := ansi.StringWidth(right)
		if rightW <= bgW-pos {
			sb.WriteString(strings.Repeat(" ", bgW-rightW-pos))
		}
		sb.WriteString(right)
	}
	return sb.String()
}

// shellCommand builds the clipboard one-liner. The prefix must sit on the
// claude call, not the cd, or the env never reaches the right command.
func shellCommand(s *db.Session, args []string) string {
	prefix := config.ClaudeEnvPrefix(config.ClaudeHome(s.ClaudeDir), shellQuote)
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = a
		if strings.HasPrefix(a, "-") || a == "attach" {
			continue
		}
		quoted[i] = shellQuote(a)
	}
	return fmt.Sprintf("cd %s && %sclaude %s",
		shellQuote(s.ProjectPath), prefix, strings.Join(quoted, " "))
}

func (m *Model) copyResumeCommand() {
	s, args := m.openableSession()
	if s == nil {
		return
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(shellCommand(s, args)))
	fmt.Fprintf(os.Stderr, "\033]52;c;%s\007", encoded)
	m.statusMsg = "Copied command to clipboard"
}
