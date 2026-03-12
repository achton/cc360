package tui

import (
	"fmt"
	"strings"

	"github.com/achton/cc360/internal/db"
	"github.com/charmbracelet/lipgloss"
)

const detailHeight = 8 // total lines including border

var (
	detailBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorSurface1).
				Padding(0, 1)

	detailTitleStyle = lipgloss.NewStyle().
				Foreground(colorMauve).
				Bold(true)

	detailPromptStyle = lipgloss.NewStyle().
				Foreground(colorSubtext1)

	detailLabelStyle = lipgloss.NewStyle().
				Foreground(colorSubtext0)

	detailMetaStyle = lipgloss.NewStyle().
			Foreground(colorOverlay0)

	detailActiveStyle = lipgloss.NewStyle().
				Foreground(colorGreen).
				Bold(true)
)

type detailPane struct {
	visible bool
}

func (d *detailPane) toggle() {
	d.visible = !d.visible
}

// view renders the detail pane for the given session.
// Returns exactly detailHeight lines (1 border + detailHeight-1 content).
func (d *detailPane) view(s *db.Session, width int, active bool) string {
	if s == nil {
		return d.empty(width)
	}

	contentLines := detailHeight - 1 // 7 lines of content
	var lines []string

	// Active indicator
	if active {
		lines = append(lines, detailActiveStyle.Render("● ACTIVE SESSION"))
	}

	// Title (bold, prominent) or folder if no title
	title := sanitize(s.Title)
	if title == "" {
		title = sanitize(s.ExistingSummary)
	}
	if title != "" {
		lines = append(lines, detailTitleStyle.Render(truncateRunes(title, width-2)))
	} else {
		lines = append(lines, detailTitleStyle.Render(simplifyProjectName(s.ProjectName)))
	}

	// Line 2: AI summary if available
	if s.Summary != "" {
		lines = append(lines, detailPromptStyle.Render(sanitize(s.Summary)))
	}

	// Remaining: First prompt (word-wrapped, fills available space)
	prompt := sanitize(s.FirstPrompt)
	if prompt != "" {
		wrapped := wordWrap(prompt, width-2)
		maxPromptLines := 3
		for i, line := range wrapped {
			if i >= maxPromptLines {
				break
			}
			lines = append(lines, detailPromptStyle.Render(line))
		}
	}

	// Line 5: Folder + path
	displayName := simplifyProjectName(s.ProjectName)
	folderLine := detailLabelStyle.Render("Folder: ") + detailPromptStyle.Render(displayName)
	if s.ProjectPath != "" && s.ProjectPath != s.ProjectName {
		folderLine += detailMetaStyle.Render("  (" + simplifyProjectName(s.ProjectPath) + ")")
	}
	lines = append(lines, folderLine)

	// Line 6: Metadata row — less important stuff, compact
	var meta []string
	if s.GitBranch != "" {
		meta = append(meta, "Branch: "+s.GitBranch)
	}
	if s.MessageCount > 0 {
		meta = append(meta, fmt.Sprintf("Msgs: %d", s.MessageCount))
	}
	if !s.Modified.IsZero() {
		meta = append(meta, "Modified: "+s.Modified.Format("2006-01-02 15:04"))
	} else if !s.Created.IsZero() {
		meta = append(meta, "Created: "+s.Created.Format("2006-01-02 15:04"))
	}
	idLen := len(s.SessionID)
	if idLen > 12 {
		idLen = 12
	}
	meta = append(meta, "ID: "+s.SessionID[:idLen])
	lines = append(lines, detailMetaStyle.Render(strings.Join(meta, "  ")))

	// Pad or truncate
	for len(lines) < contentLines {
		lines = append(lines, "")
	}
	if len(lines) > contentLines {
		lines = lines[:contentLines]
	}

	content := strings.Join(lines, "\n")
	return detailBorderStyle.Width(width).Render(content)
}

func (d *detailPane) empty(width int) string {
	lines := make([]string, detailHeight-1)
	content := strings.Join(lines, "\n")
	return detailBorderStyle.Width(width).Render(content)
}

// truncateRunes truncates by rune count.
func truncateRunes(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return "…"
	}
	return string(runes[:maxLen-1]) + "…"
}

// wordWrap breaks text into lines of at most width characters.
func wordWrap(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	var lines []string
	for len(s) > 0 {
		if len([]rune(s)) <= width {
			lines = append(lines, s)
			break
		}
		// Find last space within width
		runes := []rune(s)
		cut := width
		for cut > 0 && runes[cut] != ' ' {
			cut--
		}
		if cut == 0 {
			cut = width // no space found, hard break
		}
		lines = append(lines, string(runes[:cut]))
		s = strings.TrimLeft(string(runes[cut:]), " ")
	}
	return lines
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
