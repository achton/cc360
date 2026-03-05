package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/achton/cc360/internal/db"
	"github.com/charmbracelet/lipgloss"
)

type pickerSortMode int

const (
	pickerSortCount pickerSortMode = iota
	pickerSortAlpha
)

type projectEntry struct {
	name  string
	count int
}

type projectPicker struct {
	active   bool
	entries  []projectEntry
	cursor   int
	offset   int
	sortMode pickerSortMode
}

func (p *projectPicker) open(sessions []db.Session) {
	counts := make(map[string]int)
	for _, s := range sessions {
		counts[s.ProjectName]++
	}
	p.entries = make([]projectEntry, 0, len(counts))
	for name, count := range counts {
		p.entries = append(p.entries, projectEntry{name: name, count: count})
	}
	p.sortEntries()
	p.cursor = 0
	p.offset = 0
	p.active = true
}

func (p *projectPicker) close() {
	p.active = false
}

func (p *projectPicker) toggleSort() {
	if p.sortMode == pickerSortCount {
		p.sortMode = pickerSortAlpha
	} else {
		p.sortMode = pickerSortCount
	}
	p.sortEntries()
	p.cursor = 0
	p.offset = 0
}

func (p *projectPicker) sortEntries() {
	switch p.sortMode {
	case pickerSortAlpha:
		sort.Slice(p.entries, func(i, j int) bool {
			return p.entries[i].name < p.entries[j].name
		})
	default: // pickerSortCount
		sort.Slice(p.entries, func(i, j int) bool {
			if p.entries[i].count == p.entries[j].count {
				return p.entries[i].name < p.entries[j].name
			}
			return p.entries[i].count > p.entries[j].count
		})
	}
}

func (p *projectPicker) selected() string {
	if p.cursor >= 0 && p.cursor < len(p.entries) {
		return p.entries[p.cursor].name
	}
	return ""
}

func (p *projectPicker) moveUp()   { p.setCursor(p.cursor - 1) }
func (p *projectPicker) moveDown() { p.setCursor(p.cursor + 1) }

func (p *projectPicker) setCursor(n int) {
	if len(p.entries) == 0 {
		return
	}
	p.cursor = clamp(n, 0, len(p.entries)-1)
	// Keep cursor in view
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
}

var (
	pickerTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("15")).
				Background(lipgloss.Color("62")).
				Padding(0, 1)

	pickerSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("229")).
				Background(lipgloss.Color("57"))

	pickerNormalStyle = lipgloss.NewStyle()

	pickerCountStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("241"))
)

func (p *projectPicker) view(width, height int) string {
	var b strings.Builder

	sortLabel := "by count"
	if p.sortMode == pickerSortAlpha {
		sortLabel = "alphabetical"
	}
	title := pickerTitleStyle.Width(width).Render(
		fmt.Sprintf("Projects (%d) — %s  [s] toggle sort  [esc] cancel", len(p.entries), sortLabel),
	)
	b.WriteString(title)
	b.WriteByte('\n')

	// Available rows for entries
	visibleRows := height - 3 // title + status + help
	if visibleRows < 3 {
		visibleRows = 3
	}

	// Adjust offset for visible window
	if p.cursor >= p.offset+visibleRows {
		p.offset = p.cursor - visibleRows + 1
	}
	if p.offset < 0 {
		p.offset = 0
	}

	end := p.offset + visibleRows
	if end > len(p.entries) {
		end = len(p.entries)
	}

	for i := p.offset; i < end; i++ {
		e := p.entries[i]
		line := fmt.Sprintf("  %-*s", width-12, truncate(e.name, width-12))
		line += pickerCountStyle.Render(fmt.Sprintf("%4d", e.count))
		if i == p.cursor {
			line = pickerSelectedStyle.Width(width).Render(line)
		}
		b.WriteByte('\n')
		b.WriteString(line)
	}

	// Pad remaining
	rendered := end - p.offset
	for i := rendered; i < visibleRows; i++ {
		b.WriteByte('\n')
	}

	return b.String()
}
