package tui

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"time"

	"github.com/achton/cc360/internal/config"
	"github.com/achton/cc360/internal/db"
	"github.com/achton/cc360/internal/scanner"
	"github.com/achton/cc360/internal/summarizer"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// Model is the top-level Bubbletea model.
type Model struct {
	db              *db.DB
	cfg             config.Config
	allSessions     []db.Session // full unfiltered list
	sessions        []db.Session // currently displayed (filtered)
	scannerSessions []scanner.Session
	activeIDs       map[string]bool
	table           sessionTable
	detail          detailPane
	filter          filterInput
	picker          projectPicker
	projectFilter   map[string]bool // if non-empty, only show sessions from these projects
	spinner         spinner.Model
	tableInit       bool
	keys            keyMap
	width           int
	height          int
	statusMsg       string

	// Background summarization state
	summarizing    bool              // true while any summarization is in-flight
	summaryDone    int               // completed count
	summaryTotal   int               // total queued
	summaryFailed  int               // failed count
	summaryCh      <-chan db.Session  // channel for auto-summarize workers
}

type execFinishedMsg struct{ err error }

type summarizeResultMsg struct {
	sessionID string
	title     string
	summary   string
	err       error
}

type autoSummarizeMsg struct{}
type activeTickMsg struct{}

type reloadResultMsg struct {
	cfg      config.Config
	sessions []db.Session
	scanSess []scanner.Session
	err      error
}

const activePollInterval = 15 * time.Second

func activeTickCmd() tea.Cmd {
	return tea.Tick(activePollInterval, func(time.Time) tea.Msg {
		return activeTickMsg{}
	})
}

// New creates the initial TUI model.
func New(database *db.DB, cfg config.Config, sessions []db.Session, scannerSessions []scanner.Session, activeIDs map[string]bool) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	return Model{
		db:              database,
		cfg:             cfg,
		allSessions:     sessions,
		sessions:        sessions,
		scannerSessions: scannerSessions,
		activeIDs:       activeIDs,
		spinner:         s,
		detail:          detailPane{visible: true},
		filter:          newFilterInput(),
		keys:            newKeyMap(),
		tableInit:       false,
	}
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{activeTickCmd()}

	// Trigger auto-summarize via message so it runs in Update (pointer receiver)
	if m.cfg.AutoSummarize > 0 {
		cmds = append(cmds, func() tea.Msg { return autoSummarizeMsg{} })
	}

	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if !m.tableInit {
			m.table = newSessionTable(m.sessions, m.width, m.tableHeight(), m.activeIDs)
			m.tableInit = true
		} else {
			m.table.resize(m.sessions, m.width, m.tableHeight())
		}
		return m, nil

	case tea.KeyMsg:
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

		case key.Matches(msg, m.keys.Summarize):
			return m, m.startSingleSummarize()

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

		case key.Matches(msg, m.keys.Reload):
			m.statusMsg = "Reloading..."
			return m, m.reloadCmd()

		case key.Matches(msg, m.keys.Escape):
			m.clearFilters()
			return m, nil
		}

	case spinner.TickMsg:
		if m.summarizing {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}

	case summarizeResultMsg:
		return m, m.handleSummarizeResult(msg)

	case autoSummarizeMsg:
		return m, m.startAutoSummarize()

	case reloadResultMsg:
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Reload error: %v", msg.err)
			return m, nil
		}
		m.cfg = msg.cfg
		m.scannerSessions = msg.scanSess
		m.allSessions = msg.sessions
		m.activeIDs = scanner.ActiveSessionIDs(m.scannerSessions)
		m.table.activeIDs = m.activeIDs
		// Clear filters and rebuild
		m.filter.close()
		m.projectFilter = nil
		m.sessions = m.allSessions
		m.rebuildTable()
		m.statusMsg = fmt.Sprintf("Reloaded — %d sessions", len(m.allSessions))
		return m, nil

	case activeTickMsg:
		m.activeIDs = scanner.ActiveSessionIDs(m.scannerSessions)
		m.table.activeIDs = m.activeIDs
		m.table.rows = buildRows(m.sessions, m.width, m.table.columns, m.activeIDs)
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

func (m *Model) updatePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

	case msg.Type == tea.KeySpace:
		m.picker.toggleSelect()
		return m, nil

	case msg.Type == tea.KeyRight:
		m.picker.expand()
		return m, nil

	case msg.Type == tea.KeyLeft:
		m.picker.collapse()
		return m, nil
	}

	return m, nil
}

func (m *Model) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Escape):
		m.filter.close()
		m.applyFilters()
		return m, nil

	case key.Matches(msg, m.keys.Resume):
		// Enter: stop typing but keep filter visible
		m.filter.input.Blur()
		return m, nil

	case msg.Type == tea.KeyUp:
		m.table.MoveUp(1)
		return m, nil

	case msg.Type == tea.KeyDown:
		m.table.MoveDown(1)
		return m, nil
	}

	// Pass to text input
	prevValue := m.filter.value()
	var cmd tea.Cmd
	m.filter.input, cmd = m.filter.input.Update(msg)

	// If value changed, re-filter
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
			text := strings.ToLower(s.ProjectName + " " + s.Title + " " + s.Summary +
				" " + s.FirstPrompt + " " + s.GitBranch + " " + s.ExistingSummary)
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
	m.table.rows = buildRows(m.sessions, m.width, m.table.columns, m.activeIDs)
	m.table.SetCursor(0)
	m.table.setHeight(m.tableHeight())
}

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	headerText := headerAppStyle.Render("CC360") + headerTagStyle.Render(" // built by @achton")
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
		detail = "\n" + m.detail.view(s, m.width, m.isActive(s))
	}

	statusText := m.statusMsg
	if m.summarizing {
		statusText = m.spinner.View() + " " + m.statusMsg
	}
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

	return base
}

func (m Model) renderHelp() string {
	pairs := []struct{ key, desc string }{
		{"enter", "resume"},
		{"tab", "detail"},
		{"/", "filter"},
		{"p", "projects"},
		{"s", "summarize"},
		{"c", "copy cmd"},
		{"r", "reload"},
		{"q", "quit"},
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
		h -= lipgloss.Height(m.detail.view(m.selectedSession(), m.width, false))
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

func (m *Model) isActive(s *db.Session) bool {
	return s != nil && m.activeIDs[s.SessionID]
}

// startAutoSummarize queues unsummarized sessions for background processing.
func (m *Model) startAutoSummarize() tea.Cmd {
	unsummarized, err := m.db.Unsummarized(m.cfg.AutoSummarize)
	if err != nil || len(unsummarized) == 0 {
		return nil
	}

	m.summarizing = true
	m.summaryDone = 0
	m.summaryFailed = 0
	m.summaryTotal = len(unsummarized)
	m.statusMsg = fmt.Sprintf("Summarizing 0/%d...", m.summaryTotal)

	// Launch concurrent workers
	concurrency := m.cfg.SummarizeConcurrency
	if concurrency <= 0 {
		concurrency = 3
	}

	// Feed sessions through a channel
	ch := make(chan db.Session, len(unsummarized))
	for _, s := range unsummarized {
		ch <- s
	}
	close(ch)
	m.summaryCh = ch

	model := m.cfg.SummarizeModel
	var cmds []tea.Cmd
	cmds = append(cmds, m.spinner.Tick)

	for i := 0; i < concurrency && i < len(unsummarized); i++ {
		cmds = append(cmds, summarizeWorker(ch, model))
	}

	return tea.Batch(cmds...)
}

// summarizeWorker reads from the channel and summarizes one session at a time.
func summarizeWorker(ch <-chan db.Session, model string) tea.Cmd {
	return func() tea.Msg {
		s, ok := <-ch
		if !ok {
			return nil
		}
		title, summary, err := summarizer.Summarize(s, model)
		return summarizeResultMsg{
			sessionID: s.SessionID,
			title:     title,
			summary:   summary,
			err:       err,
		}
	}
}

func (m *Model) startSingleSummarize() tea.Cmd {
	s := m.selectedSession()
	if s == nil {
		m.statusMsg = "No session selected"
		return nil
	}
	if s.JSONLPath == "" {
		m.statusMsg = "Cannot summarize: no JSONL file for this session"
		return nil
	}
	if _, err := os.Stat(s.JSONLPath); err != nil {
		m.statusMsg = "Cannot summarize: JSONL file no longer exists"
		return nil
	}
	if s.Title != "" && !s.SummarizedAt.IsZero() && !s.Modified.After(s.SummarizedAt) {
		m.statusMsg = "Already summarized (not modified since)"
		return nil
	}
	if m.activeIDs[s.SessionID] {
		m.statusMsg = "Cannot summarize: session is active (data not flushed to disk yet)"
		return nil
	}

	if !m.summarizing {
		m.summarizing = true
		m.summaryDone = 0
		m.summaryFailed = 0
		m.summaryTotal = 1
	} else {
		m.summaryTotal++
	}
	m.statusMsg = fmt.Sprintf("Summarizing %d/%d...", m.summaryDone, m.summaryTotal)

	session := *s
	model := m.cfg.SummarizeModel
	return tea.Batch(
		m.spinner.Tick,
		func() tea.Msg {
			title, summary, err := summarizer.Summarize(session, model)
			return summarizeResultMsg{
				sessionID: session.SessionID,
				title:     title,
				summary:   summary,
				err:       err,
			}
		},
	)
}

func (m *Model) handleSummarizeResult(msg summarizeResultMsg) tea.Cmd {
	m.summaryDone++

	if msg.err != nil {
		m.summaryFailed++
	} else {
		// Update DB
		if err := m.db.SetSummary(msg.sessionID, msg.title, msg.summary); err == nil {
			// Update in-memory sessions (both allSessions and filtered sessions)
			for i := range m.allSessions {
				if m.allSessions[i].SessionID == msg.sessionID {
					m.allSessions[i].Title = msg.title
					m.allSessions[i].Summary = msg.summary
					break
				}
			}
			for i := range m.sessions {
				if m.sessions[i].SessionID == msg.sessionID {
					m.sessions[i].Title = msg.title
					m.sessions[i].Summary = msg.summary
					break
				}
			}
			m.table.rows = buildRows(m.sessions, m.width, m.table.columns, m.activeIDs)
		} else {
			m.summaryFailed++
		}
	}

	if m.summaryDone >= m.summaryTotal {
		m.summarizing = false
		m.summaryCh = nil
		succeeded := m.summaryTotal - m.summaryFailed
		switch {
		case m.summaryFailed > 0 && succeeded == 0:
			m.statusMsg = fmt.Sprintf("Summarization failed: %v", msg.err)
		case m.summaryFailed > 0:
			m.statusMsg = fmt.Sprintf("Summarized %d sessions (%d failed)", succeeded, m.summaryFailed)
		default:
			m.statusMsg = fmt.Sprintf("Summarized %d sessions", succeeded)
		}
		return nil
	}

	m.statusMsg = fmt.Sprintf("Summarizing %d/%d...", m.summaryDone, m.summaryTotal)

	// Re-spawn worker to pick up next item from channel
	if m.summaryCh != nil {
		return summarizeWorker(m.summaryCh, m.cfg.SummarizeModel)
	}
	return nil
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

		scanned, err := scanner.Scan(cfg)
		if err != nil {
			return reloadResultMsg{err: err}
		}

		if err := database.Upsert(scanned); err != nil {
			return reloadResultMsg{err: err}
		}

		ids := make([]string, len(scanned))
		for i, s := range scanned {
			ids[i] = s.SessionID
		}
		database.PruneUnseen(ids)

		all, err := database.AllSessions(cfg.SortBy, true)
		if err != nil {
			return reloadResultMsg{err: err}
		}

		// Filter non-interactive sessions
		filtered := make([]db.Session, 0, len(all))
		for _, s := range all {
			text := s.ExistingSummary + s.FirstPrompt
			if strings.Contains(text, "Caveat: The messages below were generated by the user while running") {
				continue
			}
			if strings.HasPrefix(s.FirstPrompt, "<teammate-message") {
				continue
			}
			if s.ProjectPath != "" {
				if _, err := os.Stat(s.ProjectPath); err != nil {
					continue
				}
			}
			filtered = append(filtered, s)
		}

		return reloadResultMsg{
			cfg:      cfg,
			sessions: filtered,
			scanSess: scanned,
		}
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
	s := m.selectedSession()
	if s == nil {
		m.statusMsg = "No session selected"
		return nil
	}
	if m.isActive(s) {
		m.statusMsg = "Session is currently active"
		return nil
	}
	if !m.checkProjectPath(s) {
		return nil
	}
	c := exec.Command("claude", "--resume", s.SessionID)
	c.Dir = s.ProjectPath
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return execFinishedMsg{err: err}
	})
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

func (m *Model) copyResumeCommand() {
	s := m.selectedSession()
	if s == nil {
		m.statusMsg = "No session selected"
		return
	}
	if m.isActive(s) {
		m.statusMsg = "Session is currently active"
		return
	}
	if !m.checkProjectPath(s) {
		return
	}
	cmd := fmt.Sprintf("cd %s && claude --resume %s", s.ProjectPath, s.SessionID)
	encoded := base64.StdEncoding.EncodeToString([]byte(cmd))
	fmt.Fprintf(os.Stderr, "\033]52;c;%s\007", encoded)
	m.statusMsg = "Copied resume command to clipboard"
}
