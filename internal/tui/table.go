package tui

import (
	"fmt"
	"strings"
	"time"

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
	columns     []column
	rows        [][]string
	activeIDs   map[string]bool
	sessionInfo string // e.g. "42 sessions" — shown on the indicator line
	cursor      int
	offset      int // first visible row index
	height      int // number of visible data rows
	width       int
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

// setHeight sets visible data rows. availHeight is the space available for the
// entire table view (header + separator + data rows + scroll indicator).
func (t *sessionTable) setHeight(availHeight int) {
	// Table chrome: column header(1) + separator(1) + scroll indicator(1) = 3
	h := availHeight - 3
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

// colRole identifies the semantic role of a column for styling.
type colRole int

const (
	colNormal colRole = iota
	colTitle
	colBranch
)

// columnRoles returns the role for each column index.
func columnRoles(numCols int) []colRole {
	// 0=Date, 1=Title, 2=Folder, 3=Branch (if present), 4=Msgs (if present)
	roles := make([]colRole, numCols)
	roles[1] = colTitle
	if numCols >= 4 {
		roles[3] = colBranch
	}
	return roles
}

// renderCell renders a single cell padded/truncated to the column width.
// role controls per-column styling; selected composes with selection background.
func renderCell(value string, width int, first bool, role colRole, selected bool) string {
	base := lipgloss.NewStyle().Width(width).MaxWidth(width).Inline(true)

	switch role {
	case colTitle:
		// Split "title: summary" and style title portion bold+colored
		// The raw value uses " — " as separator (set in buildRows)
		if parts := strings.SplitN(value, " — ", 2); len(parts) == 2 {
			titleStyle := titleBoldStyle
			if selected {
				titleStyle = titleStyle.Background(colorSurface0)
			}
			summaryStyle := lipgloss.NewStyle()
			if selected {
				summaryStyle = summaryStyle.Background(colorSurface0)
			}
			value = titleStyle.Render(parts[0]) + summaryStyle.Render(": "+parts[1])
		} else if value != "" {
			titleStyle := titleBoldStyle
			if selected {
				titleStyle = titleStyle.Background(colorSurface0)
			}
			value = titleStyle.Render(value)
		}
	case colBranch:
		style := dimStyle
		if selected {
			style = style.Background(colorSurface0)
		}
		value = style.Render(value)
	}

	if selected {
		base = base.Background(colorSurface0)
	}

	s := base.Render(value)
	if !first {
		gap := strings.Repeat(" ", colGap)
		if selected {
			gap = lipgloss.NewStyle().Background(colorSurface0).Render(gap)
		}
		s = gap + s
	}
	return s
}

// View renders column header + separator + visible rows + scroll indicator.
// Always returns exactly height+3 lines for stable layout.
func (t *sessionTable) View() string {
	var b strings.Builder

	// Column header with indent to align with accent bar
	b.WriteString("  ") // align with "▎ " on data rows
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
	b.WriteString(sepStyle.Render(strings.Repeat("╌", t.width)))
	b.WriteByte('\n')

	// Data rows
	end := t.offset + t.height
	if end > len(t.rows) {
		end = len(t.rows)
	}
	roles := columnRoles(len(t.columns))
	for i := t.offset; i < end; i++ {
		isSel := i == t.cursor
		var line string
		for j, col := range t.columns {
			cell := ""
			if j < len(t.rows[i]) {
				cell = t.rows[i][j]
			}
			line += renderCell(cell, col.Width, j == 0, roles[j], isSel)
		}
		if isSel {
			line = selectedBarStyle.Render("▎") + selectedStyle.Render(" ") + line
		} else {
			line = "  " + line
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

	// Info line (always rendered to keep height stable)
	b.WriteByte('\n')
	info := t.sessionInfo
	if end < len(t.rows) {
		remaining := len(t.rows) - end
		if info != "" {
			info += fmt.Sprintf(" · ↓ %d more", remaining)
		} else {
			info = fmt.Sprintf("↓ %d more", remaining)
		}
	}
	if info != "" {
		b.WriteString(mutedStyle.Render("  " + info))
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

// relativeDate formats a time as a human-friendly relative date.
func relativeDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())

	diff := today.Sub(day)
	switch {
	case diff < 0:
		return t.Format("15:04")
	case diff == 0:
		return "Today " + t.Format("15:04")
	case diff < 24*time.Hour:
		return "Yesterday"
	case diff < 7*24*time.Hour:
		days := int(diff.Hours() / 24)
		return fmt.Sprintf("%dd ago", days)
	case t.Year() == now.Year():
		return t.Format("Jan _2")
	default:
		return t.Format("2006-01-02")
	}
}

// buildRows converts sessions into pre-formatted table rows.
func buildRows(sessions []db.Session, width int, cols []column, activeIDs map[string]bool) [][]string {
	showBranch := width >= 90
	showMsgs := width >= 100

	rows := make([][]string, len(sessions))
	for i, s := range sessions {
		ts := s.Modified
		if ts.IsZero() {
			ts = s.Created
		}
		date := relativeDate(ts)
		if activeIDs[s.SessionID] {
			date += " " + activeStyle.Render("●")
		}

		// Title: use " — " as separator so renderCell can style parts independently
		title := sanitize(s.Title)
		summary := sanitize(s.Summary)
		if title != "" && summary != "" {
			title = title + " — " + summary
		} else if title != "" {
			// Has title but no summary yet — show title with first prompt as context
			fp := sanitize(s.FirstPrompt)
			if fp != "" {
				title = title + " — " + fp
			}
		} else {
			// No title — use existing summary or first prompt as fallback
			title = sanitize(s.ExistingSummary)
			if title == "" {
				title = sanitize(s.FirstPrompt)
			}
			if title == "" {
				title = sanitize(s.ProjectName)
			}
		}

		folder := sanitize(simplifyProjectName(s.ProjectName))
		if strings.Contains(s.ProjectName, "/.claude/worktrees/") {
			folder += " " + pickerWorktreeStyle.Render("⌥")
		}
		row := []string{date, title, folder}
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
