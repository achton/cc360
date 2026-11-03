package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/achton/cc360/internal/db"
	"github.com/achton/cc360/internal/scanner"
	"github.com/charmbracelet/x/ansi"
)

// The border adds two more lines.
const detailContentLines = 7

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

// view renders the detail pane. Its height is fixed, so every line is cut to
// the content width. A wider line wraps and grows the pane.
func (d *detailPane) view(s *db.Session, width int, agent scanner.Agent) string {
	if s == nil {
		return d.empty(width)
	}

	inner := detailInnerWidth(width)
	var lines []string

	if line := agentLine(agent); line != "" {
		lines = append(lines, ansi.Truncate(line, inner, "…"))
	}

	// Title (bold, prominent) or folder if no title
	title := firstNonEmpty(sanitize(s.CustomTitle), sanitize(s.Title), sanitize(s.ExistingSummary))
	if title != "" {
		lines = append(lines, detailTitleStyle.Render(truncateRunes(title, inner)))
	} else {
		lines = append(lines, detailTitleStyle.Render(displayProjectName(*s)))
	}

	// Remaining: First prompt (word-wrapped, fills available space)
	prompt := sanitize(s.FirstPrompt)
	if prompt != "" {
		wrapped := wordWrap(prompt, inner)
		maxPromptLines := 3
		for i, line := range wrapped {
			if i >= maxPromptLines {
				break
			}
			lines = append(lines, detailPromptStyle.Render(line))
		}
	}

	// Line 5: Folder + worktree indicator + path
	displayName := displayProjectName(*s)
	folderLine := detailLabelStyle.Render("Folder: ") + detailPromptStyle.Render(displayName)
	if s.IsWorktree {
		folderLine += worktreeBadge(s.WorktreeName)
	}
	// Show the real working directory (the worktree's own path when a worktree).
	if s.ProjectPath != "" && s.ProjectPath != displayName {
		folderLine += detailMetaStyle.Render("  (" + s.ProjectPath + ")")
	}
	lines = append(lines, ansi.Truncate(folderLine, inner, "…"))

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
	lines = append(lines, ansi.Truncate(detailMetaStyle.Render(strings.Join(meta, "  ")), inner, "…"))

	// Pad or truncate
	for len(lines) < detailContentLines {
		lines = append(lines, "")
	}
	if len(lines) > detailContentLines {
		lines = lines[:detailContentLines]
	}

	content := strings.Join(lines, "\n")
	return detailBorderStyle.Width(width).Render(content)
}

// agentLine describes a running session in one line, or "" when it is not.
func agentLine(a scanner.Agent) string {
	var head string
	switch a.State {
	case scanner.StateBusy:
		head = detailActiveStyle.Render("● WORKING")
	case scanner.StateWaiting:
		head = waitingStyle.Render("◆ NEEDS INPUT")
		if a.WaitingFor != "" {
			head += waitingStyle.Render(": " + sanitize(a.WaitingFor))
		}
	case scanner.StateIdle:
		head = idleStyle.Render("○ IDLE")
	default:
		return ""
	}
	var extra []string
	if a.Background {
		extra = append(extra, "background")
	}
	if a.Name != "" {
		extra = append(extra, sanitize(a.Name))
	}
	if len(extra) > 0 {
		head += detailMetaStyle.Render("  " + strings.Join(extra, " · "))
	}
	return head
}

// Lip Gloss v2 counts the border and the padding inside Width.
func detailInnerWidth(width int) int {
	inner := width - detailBorderStyle.GetHorizontalFrameSize()
	if inner < 1 {
		inner = 1
	}
	return inner
}

func (d *detailPane) empty(width int) string {
	lines := make([]string, detailContentLines)
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
