package tui

import (
	"fmt"
	"strings"

	"github.com/achton/cc360/internal/db"
	"github.com/charmbracelet/lipgloss"
)

// column defines a table column.
type column struct {
	Title string
	Width int
}

// sessionTable renders a fixed header + scrollable rows with a highlighted cursor row.
type sessionTable struct {
	columns   []column
	rows      [][]string
	activeIDs map[string]bool
	cursor    int
	offset    int // first visible row index
	height    int // number of visible data rows
	width     int
}

func newSessionTable(sessions []db.Session, width, availHeight int, activeIDs map[string]bool) sessionTable {
	t := sessionTable{width: width, activeIDs: activeIDs}
	t.columns = buildColumns(width)
	t.rows = buildRows(sessions, width, t.columns, activeIDs)
	t.setHeight(availHeight)
	return t
}

func (t *sessionTable) resize(sessions []db.Session, width, availHeight int) {
	cursor := t.cursor
	t.width = width
	t.columns = buildColumns(width)
	t.rows = buildRows(sessions, width, t.columns, t.activeIDs)
	t.setHeight(availHeight)
	t.SetCursor(cursor)
}

// setHeight sets visible data rows. availHeight is total terminal height
// minus any detail pane, but still including chrome.
func (t *sessionTable) setHeight(availHeight int) {
	// Chrome: app header(1) + column header(1) + separator(1) + status(1) + help(1) = 5
	h := availHeight - 5
	if h < 2 {
		h = 2
	}
	t.height = h
	t.clampView()
}

func (t *sessionTable) Cursor() int    { return t.cursor }
func (t *sessionTable) MoveUp(n int)   { t.SetCursor(t.cursor - n) }
func (t *sessionTable) MoveDown(n int) { t.SetCursor(t.cursor + n) }
func (t *sessionTable) GotoTop()       { t.SetCursor(0) }
func (t *sessionTable) GotoBottom()    { t.SetCursor(len(t.rows) - 1) }
func (t *sessionTable) PageUp()        { t.MoveUp(t.height) }
func (t *sessionTable) PageDown()      { t.MoveDown(t.height) }

func (t *sessionTable) SetCursor(n int) {
	if len(t.rows) == 0 {
		t.cursor = 0
		return
	}
	t.cursor = clamp(n, 0, len(t.rows)-1)
	t.clampView()
}

func (t *sessionTable) clampView() {
	if t.cursor < t.offset {
		t.offset = t.cursor
	}
	if t.cursor >= t.offset+t.height {
		t.offset = t.cursor - t.height + 1
	}
	maxOffset := len(t.rows) - t.height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if t.offset > maxOffset {
		t.offset = maxOffset
	}
	if t.offset < 0 {
		t.offset = 0
	}
}

// colGap is the number of spaces prepended before each column (except the first).
const colGap = 2

// renderCell renders a single cell padded/truncated to the column width.
// If first is false, colGap spaces are prepended.
func renderCell(value string, width int, first bool) string {
	s := lipgloss.NewStyle().Width(width).MaxWidth(width).Inline(true).Render(value)
	if !first {
		s = strings.Repeat(" ", colGap) + s
	}
	return s
}

// View renders column header + separator + visible rows.
// Returns exactly height+2 lines.
func (t *sessionTable) View() string {
	var b strings.Builder

	// Column header
	for i, col := range t.columns {
		hdr := colHdrStyle.Width(col.Width).MaxWidth(col.Width).Render(
			truncate(col.Title, col.Width),
		)
		if i > 0 {
			b.WriteString(strings.Repeat(" ", colGap))
		}
		b.WriteString(hdr)
	}
	b.WriteByte('\n')

	// Separator
	b.WriteString(sepStyle.Width(t.width).Render(strings.Repeat("─", t.width)))
	b.WriteByte('\n')

	// Data rows
	end := t.offset + t.height
	if end > len(t.rows) {
		end = len(t.rows)
	}
	for i := t.offset; i < end; i++ {
		var line string
		for j, col := range t.columns {
			cell := ""
			if j < len(t.rows[i]) {
				cell = t.rows[i][j]
			}
			line += renderCell(cell, col.Width, j == 0)
		}
		// Apply selection style to entire line (NOT per-cell)
		if i == t.cursor {
			line = selectedStyle.Render(line)
		}
		b.WriteString(line)
		if i < end-1 {
			b.WriteByte('\n')
		}
	}

	// Pad with empty lines if fewer rows than height
	rendered := end - t.offset
	for i := rendered; i < t.height; i++ {
		if i > 0 || rendered > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.Repeat(" ", t.width))
	}

	return b.String()
}

// buildColumns computes table columns based on terminal width.
func buildColumns(width int) []column {
	dateW := 12
	msgsW := 6
	branchW := 16

	showBranch := width >= 90
	showMsgs := width >= 100

	// Count columns to compute total gap space (colGap between each pair)
	numCols := 3 // date, summary, folder (always present)
	if showBranch {
		numCols++
	}
	if showMsgs {
		numCols++
	}
	gaps := (numCols - 1) * colGap

	used := dateW + gaps
	if showMsgs {
		used += msgsW
	}
	if showBranch {
		used += branchW
	}

	remaining := width - used
	if remaining < 20 {
		remaining = 20
	}

	projectW := remaining / 3
	if projectW < 10 {
		projectW = 10
	}
	titleW := remaining - projectW

	cols := []column{
		{Title: "Date", Width: dateW},
		{Title: "Project summary", Width: titleW},
		{Title: "Folder", Width: projectW},
	}
	if showBranch {
		cols = append(cols, column{Title: "Branch", Width: branchW})
	}
	if showMsgs {
		cols = append(cols, column{Title: "Msgs", Width: msgsW})
	}

	return cols
}

// sanitize replaces control characters with spaces and strips XML/HTML markup.
func sanitize(s string) string {
	s = stripTags(s)
	return strings.Map(func(r rune) rune {
		if r < ' ' {
			return ' '
		}
		return r
	}, s)
}

// stripTags removes XML/HTML-style tags from a string.
func stripTags(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// truncate shortens a string to maxLen with an ellipsis.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen || maxLen <= 0 {
		return s
	}
	if maxLen <= 1 {
		return "…"
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-1]) + "…"
}

// buildRows converts sessions into pre-formatted table rows.
func buildRows(sessions []db.Session, width int, cols []column, activeIDs map[string]bool) [][]string {
	showBranch := width >= 90
	showMsgs := width >= 100

	rows := make([][]string, len(sessions))
	for i, s := range sessions {
		date := ""
		if !s.Modified.IsZero() {
			date = s.Modified.Format("2006-01-02")
		} else if !s.Created.IsZero() {
			date = s.Created.Format("2006-01-02")
		}
		if activeIDs[s.SessionID] {
			date += " *"
		}

		title := sanitize(s.Title)
		if title != "" && s.Summary != "" {
			title += " — " + sanitize(s.Summary)
		} else if title == "" {
			title = sanitize(s.ExistingSummary)
		}
		if title == "" {
			title = sanitize(s.FirstPrompt)
		}

		row := []string{date, title, sanitize(s.ProjectName)}
		if showBranch {
			row = append(row, sanitize(s.GitBranch))
		}
		if showMsgs {
			msgs := ""
			if s.MessageCount > 0 {
				msgs = fmt.Sprintf("%d", s.MessageCount)
			}
			row = append(row, msgs)
		}
		rows[i] = row
	}
	return rows
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
