package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/achton/cc360/internal/db"
	"github.com/charmbracelet/lipgloss"
)

// treeNode represents a node in the project tree.
type treeNode struct {
	label       string // display label
	projectName string // full project name for leaves
	count       int    // session count (leaf) or sum of children (group)
	children    []*treeNode
	selected    bool
	expanded    bool
	worktree    bool // true if this project is a worktree
}

func (n *treeNode) isGroup() bool { return len(n.children) > 0 }

// flatRow is a flattened tree row for display/navigation.
type flatRow struct {
	node  *treeNode
	depth int
}

type projectPicker struct {
	active bool
	roots  []*treeNode
	flat   []flatRow
	cursor int
	offset int
}

// simplifyProjectName strips the .claude/worktrees/ segment from worktree paths,
// returning just the base project path (e.g. "Code/lb/myproject").
func simplifyProjectName(name string) string {
	if idx := strings.Index(name, "/.claude/worktrees/"); idx >= 0 {
		return name[:idx]
	}
	return name
}

// isWorktreePath returns true if the project name contains a worktree path segment.
func isWorktreePath(name string) bool {
	return strings.Contains(name, "/.claude/worktrees/")
}

// worktreeName extracts the worktree name from a worktree path
// (e.g. "Code/lb/myproject/.claude/worktrees/pr-123" → "pr-123").
func worktreeName(name string) string {
	if idx := strings.Index(name, "/.claude/worktrees/"); idx >= 0 {
		return name[idx+len("/.claude/worktrees/"):]
	}
	return ""
}

// childLabel returns the part of the project name to show as a leaf label
// after simplifying worktree paths and stripping the group prefix.
func childLabel(projectName, groupPrefix string) string {
	simplified := simplifyProjectName(projectName)
	return strings.TrimPrefix(simplified, groupPrefix+"/")
}

func (p *projectPicker) open(sessions []db.Session, activeFilter map[string]bool) {
	counts := make(map[string]int)
	for _, s := range sessions {
		counts[s.ProjectName]++
	}

	// Group by first path component
	groups := make(map[string]*treeNode)
	var standalones []*treeNode

	for name, count := range counts {
		isWorktree := strings.Contains(name, "/.claude/worktrees/")

		parts := strings.SplitN(name, "/", 2)
		if len(parts) == 1 {
			// No slash — standalone
			standalones = append(standalones, &treeNode{
				label:       name,
				projectName: name,
				count:       count,
				worktree:    isWorktree,
			})
			continue
		}

		groupKey := parts[0]
		if groups[groupKey] == nil {
			groups[groupKey] = &treeNode{
				label:    groupKey,
				expanded: true,
			}
		}
		groups[groupKey].children = append(groups[groupKey].children, &treeNode{
			label:       childLabel(name, groupKey),
			projectName: name,
			count:       count,
			worktree:    isWorktree,
		})
	}

	// Merge standalones that match a group key into that group as a "(root)" child
	var merged []*treeNode
	for _, s := range standalones {
		if g, ok := groups[s.label]; ok {
			g.children = append([]*treeNode{{
				label:       "(root)",
				projectName: s.projectName,
				count:       s.count,
				worktree:    s.worktree,
			}}, g.children...)
			merged = append(merged, s)
		}
	}
	if len(merged) > 0 {
		remaining := make([]*treeNode, 0, len(standalones)-len(merged))
		mergedSet := make(map[string]bool, len(merged))
		for _, m := range merged {
			mergedSet[m.projectName] = true
		}
		for _, s := range standalones {
			if !mergedSet[s.projectName] {
				remaining = append(remaining, s)
			}
		}
		standalones = remaining
	}

	// Sort children and compute group counts
	for _, g := range groups {
		sort.Slice(g.children, func(i, j int) bool {
			return g.children[i].label < g.children[j].label
		})
		total := 0
		for _, c := range g.children {
			total += c.count
		}
		g.count = total
	}

	// If a group has only one child with the same name as the group,
	// promote it to a standalone
	for key, g := range groups {
		if len(g.children) == 1 && g.children[0].projectName == key {
			standalones = append(standalones, g.children[0])
			delete(groups, key)
		}
	}

	// Collect roots
	p.roots = make([]*treeNode, 0, len(groups)+len(standalones))
	groupKeys := make([]string, 0, len(groups))
	for k := range groups {
		groupKeys = append(groupKeys, k)
	}
	sort.Strings(groupKeys)
	for _, k := range groupKeys {
		p.roots = append(p.roots, groups[k])
	}
	sort.Slice(standalones, func(i, j int) bool {
		return standalones[i].label < standalones[j].label
	})
	p.roots = append(p.roots, standalones...)

	// Restore selections from active filter
	if len(activeFilter) > 0 {
		for _, root := range p.roots {
			if root.isGroup() {
				for _, c := range root.children {
					c.selected = activeFilter[c.projectName]
				}
			} else {
				root.selected = activeFilter[root.projectName]
			}
		}
		p.syncGroupSelection()
	}

	p.flatten()
	p.cursor = 0
	p.offset = 0
	p.active = true
}

func (p *projectPicker) close() {
	p.active = false
}

func (p *projectPicker) flatten() {
	p.flat = nil
	for _, root := range p.roots {
		p.flat = append(p.flat, flatRow{node: root, depth: 0})
		if root.isGroup() && root.expanded {
			for _, child := range root.children {
				p.flat = append(p.flat, flatRow{node: child, depth: 1})
			}
		}
	}
}

func (p *projectPicker) moveUp()   { p.setCursor(p.cursor - 1) }
func (p *projectPicker) moveDown() { p.setCursor(p.cursor + 1) }

func (p *projectPicker) setCursor(n int) {
	if len(p.flat) == 0 {
		return
	}
	p.cursor = clamp(n, 0, len(p.flat)-1)
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
}

func (p *projectPicker) expand() {
	if p.cursor >= len(p.flat) {
		return
	}
	node := p.flat[p.cursor].node
	if !node.isGroup() || node.expanded {
		return
	}
	node.expanded = true
	p.flatten()
}

func (p *projectPicker) collapse() {
	if p.cursor >= len(p.flat) {
		return
	}
	node := p.flat[p.cursor].node
	if node.isGroup() {
		if node.expanded {
			node.expanded = false
			p.flatten()
			p.cursor = clamp(p.cursor, 0, len(p.flat)-1)
		}
	} else {
		// On a child: collapse the parent group and move cursor to it
		for i, row := range p.flat {
			if row.node.isGroup() {
				for _, c := range row.node.children {
					if c == node {
						row.node.expanded = false
						p.flatten()
						p.cursor = clamp(i, 0, len(p.flat)-1)
						return
					}
				}
			}
		}
	}
}

func (p *projectPicker) toggleSelect() {
	if p.cursor >= len(p.flat) {
		return
	}
	node := p.flat[p.cursor].node
	if node.isGroup() {
		allSelected := true
		for _, c := range node.children {
			if !c.selected {
				allSelected = false
				break
			}
		}
		newState := !allSelected
		node.selected = newState
		for _, c := range node.children {
			c.selected = newState
		}
	} else {
		node.selected = !node.selected
		p.syncGroupSelection()
	}
}

func (p *projectPicker) syncGroupSelection() {
	for _, root := range p.roots {
		if !root.isGroup() {
			continue
		}
		allSelected := true
		for _, c := range root.children {
			if !c.selected {
				allSelected = false
				break
			}
		}
		root.selected = allSelected
	}
}

func (p *projectPicker) selectedProjects() []string {
	var result []string
	for _, root := range p.roots {
		if root.isGroup() {
			for _, c := range root.children {
				if c.selected {
					result = append(result, c.projectName)
				}
			}
		} else if root.selected {
			result = append(result, root.projectName)
		}
	}
	return result
}

func (p *projectPicker) hasSelection() bool {
	for _, root := range p.roots {
		if root.isGroup() {
			for _, c := range root.children {
				if c.selected {
					return true
				}
			}
		} else if root.selected {
			return true
		}
	}
	return false
}

var (
	pickerTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorBlue).
				Padding(0, 1)

	pickerSelectedStyle = lipgloss.NewStyle().
				Foreground(colorText).
				Background(colorSurface0)

	pickerCheckStyle = lipgloss.NewStyle().
				Foreground(colorGreen)

	pickerPartialStyle = lipgloss.NewStyle().
				Foreground(colorYellow)

	pickerGroupStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorSubtext1)

	pickerCountStyle = lipgloss.NewStyle().
				Foreground(colorOverlay0)

	pickerBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorSurface1)

	pickerWorktreeStyle = lipgloss.NewStyle().
				Foreground(colorPeach)
)

func (p *projectPicker) view(width, height int) string {
	var b strings.Builder

	borderH := pickerBorderStyle.GetHorizontalBorderSize() + pickerBorderStyle.GetHorizontalPadding()
	innerWidth := width - borderH
	if innerWidth < 20 {
		innerWidth = 20
	}

	title := pickerTitleStyle.Width(innerWidth).Render(
		"Projects — space select  ←/→ expand/collapse  enter apply  esc cancel",
	)
	b.WriteString(title)
	b.WriteByte('\n')

	titleHeight := lipgloss.Height(title)
	borderV := pickerBorderStyle.GetVerticalBorderSize() + pickerBorderStyle.GetVerticalPadding()
	visibleRows := height - titleHeight - borderV
	if visibleRows < 3 {
		visibleRows = 3
	}

	if p.cursor >= p.offset+visibleRows {
		p.offset = p.cursor - visibleRows + 1
	}
	if p.offset < 0 {
		p.offset = 0
	}

	end := p.offset + visibleRows
	if end > len(p.flat) {
		end = len(p.flat)
	}

	for i := p.offset; i < end; i++ {
		row := p.flat[i]
		node := row.node

		// Checkbox
		check := mutedStyle.Render("○") + " "
		if node.selected {
			check = pickerCheckStyle.Render("◉") + " "
		} else if node.isGroup() {
			anySelected := false
			for _, c := range node.children {
				if c.selected {
					anySelected = true
					break
				}
			}
			if anySelected {
				check = pickerPartialStyle.Render("◎") + " "
			}
		}

		indent := strings.Repeat("  ", row.depth)

		prefix := ""
		if node.isGroup() {
			if node.expanded {
				prefix = "▼ "
			} else {
				prefix = "▶ "
			}
		}

		label := node.label
		if node.isGroup() {
			label = pickerGroupStyle.Render(label)
		} else if label == "(root)" {
			label = dimStyle.Render(label)
		}

		countStr := pickerCountStyle.Render(fmt.Sprintf(" (%d)", node.count))

		wtTag := ""
		if node.worktree {
			wt := worktreeName(node.projectName)
			if len(wt) > 20 {
				wt = wt[:20] + "…"
			}
			wtTag = " " + pickerWorktreeStyle.Render("⌥ "+wt)
		}

		line := indent + check + prefix + label + countStr + wtTag

		// Truncate to inner width
		if len(line) > innerWidth {
			line = string([]rune(line)[:innerWidth-1]) + "…"
		}

		if i == p.cursor {
			visW := lipgloss.Width(line)
			if pad := innerWidth - visW; pad > 0 {
				line = line + strings.Repeat(" ", pad)
			}
			line = pickerSelectedStyle.Render(line)
		}

		b.WriteByte('\n')
		b.WriteString(line)
	}

	rendered := end - p.offset
	for i := rendered; i < visibleRows; i++ {
		b.WriteByte('\n')
	}

	return pickerBorderStyle.Width(innerWidth + 2).Render(b.String())
}
