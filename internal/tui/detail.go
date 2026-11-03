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
	var head, tail []string

	if line := agentLine(agent); line != "" {
		head = append(head, ansi.Truncate(line, inner, "…"))
	}

	// Title (bold, prominent) or folder if no title
	title := firstNonEmpty(sanitize(s.CustomTitle), sanitize(s.Title), sanitize(s.ExistingSummary))
	displayName := sanitize(displayProjectName(*s))
	// Truncated by cells, not runes, so wide text cannot wrap and grow the pane.
	head = append(head, detailTitleStyle.Render(ansi.Truncate(firstNonEmpty(title, displayName), inner, "…")))

	// Line 5: Folder + worktree indicator + path
	folderLine := detailLabelStyle.Render("Folder: ") + detailPromptStyle.Render(displayName)
	if s.IsWorktree {
		folderLine += worktreeBadge(s.WorktreeName)
	}
	// Show the real working directory (the worktree's own path when a worktree).
	if s.ProjectPath != "" && s.ProjectPath != displayName {
		folderLine += detailMetaStyle.Render("  (" + sanitize(s.ProjectPath) + ")")
	}
	tail = append(tail, ansi.Truncate(folderLine, inner, "…"))

	// Line 6: Metadata row — less important stuff, compact
	var meta []string
	if s.GitBranch != "" {
		meta = append(meta, "Branch: "+sanitize(s.GitBranch))
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
	tail = append(tail, ansi.Truncate(detailMetaStyle.Render(strings.Join(meta, "  ")), inner, "…"))
	if extra := activityLine(s); extra != "" {
		tail = append(tail, ansi.Truncate(detailMetaStyle.Render(extra), inner, "…"))
	}

	// The first prompt fills what the fixed lines leave, up to three lines.
	lines := head
	if prompt := sanitize(s.FirstPrompt); prompt != "" {
		room := min(detailContentLines-len(head)-len(tail), 3)
		for i, line := range strings.Split(ansi.Wrap(prompt, inner, ""), "\n") {
			if i >= room {
				break
			}
			lines = append(lines, detailPromptStyle.Render(line))
		}
	}
	lines = append(lines, tail...)

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

// activityLine sums up what a session did: its latest PR, cost and line
// changes, and where it was started when that was not the CLI.
func activityLine(s *db.Session) string {
	var parts []string
	if n := len(s.PRLinks); n > 0 {
		pr := "PR: " + sanitize(s.PRLinks[n-1])
		if n > 1 {
			pr += fmt.Sprintf(" (+%d)", n-1)
		}
		parts = append(parts, pr)
	}
	if s.CostUSD > 0 {
		parts = append(parts, fmt.Sprintf("Cost: $%.2f", s.CostUSD))
	}
	if s.LinesAdded > 0 || s.LinesRemoved > 0 {
		parts = append(parts, fmt.Sprintf("Lines: +%d -%d", s.LinesAdded, s.LinesRemoved))
	}
	if origin := originName(s.Entrypoint); origin != "" {
		parts = append(parts, "Origin: "+origin)
	}
	return strings.Join(parts, "  ")
}

// originName names where a session was started, or "" for the CLI.
func originName(entrypoint string) string {
	switch {
	case entrypoint == "" || entrypoint == "cli":
		return ""
	case entrypoint == "claude-desktop":
		return "Desktop"
	case entrypoint == "claude-vscode":
		return "VS Code"
	case scanner.IsSDKEntrypoint(entrypoint):
		return "SDK"
	}
	return sanitize(entrypoint)
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
