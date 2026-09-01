package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/achton/cc360/internal/db"
	"github.com/charmbracelet/lipgloss"
)

// treeNode represents a node in the project tree.
type treeNode struct {
	label         string // display label
	projectName   string // full project name for leaves (filter key)
	count         int    // session count (leaf) or sum of children (group)
	children      []*treeNode
	selected      bool
	expanded      bool
	worktree      bool   // true if this project is a worktree
	worktreeLabel string // worktree name for the badge
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

// displayProjectName is the name a session is shown and grouped under: a
// worktree adopts its parent repo's name so it sits with the parent, otherwise
// its own name. The parent name is empty when the main root is unknown (e.g. a
// bare repo), so such a worktree groups under its own name but still badges.
func displayProjectName(s db.Session) string {
	if s.IsWorktree && s.ParentProjectName != "" {
		return s.ParentProjectName
	}
	return s.ProjectName
}

// worktreeBadge renders the ⌥ worktree indicator, or "" when there is no name.
// It truncates a long name rune-safely and includes its own leading space.
func worktreeBadge(name string) string {
	if name == "" {
		return ""
	}
	return " " + pickerWorktreeStyle.Render("⌥ "+truncateRunes(name, 21))
}

// lessTreeNode orders nodes by label, then worktree name, so a worktree sorts
// next to its parent repo, which shares its label.
func lessTreeNode(a, b *treeNode) bool {
	if a.label != b.label {
		return a.label < b.label
	}
	return a.worktreeLabel < b.worktreeLabel
}

// projInfo aggregates the sessions that share one ProjectName.
type projInfo struct {
	count         int
	worktree      bool
	displayName   string
	worktreeLabel string
	lastScanned   time.Time
}

func (p *projectPicker) open(sessions []db.Session, activeFilter map[string]bool) {
	// Aggregate per ProjectName, taking worktree metadata from the most recently
	// scanned session: retained older sessions can carry stale metadata.
	infos := make(map[string]*projInfo)
	for _, s := range sessions {
		pi := infos[s.ProjectName]
		if pi == nil {
			pi = &projInfo{}
			infos[s.ProjectName] = pi
		}
		pi.count++
		if pi.lastScanned.IsZero() || s.LastScanned.After(pi.lastScanned) {
			pi.lastScanned = s.LastScanned
			pi.worktree = s.IsWorktree
			pi.displayName = displayProjectName(s)
			pi.worktreeLabel = s.WorktreeName
		}
	}

	// Group by the first path component of the display name, so worktrees join
	// their parent repo's group even when their own path lives elsewhere.
	groups := make(map[string]*treeNode)
	var standalones []*treeNode

	for name, pi := range infos {
		parts := strings.SplitN(pi.displayName, "/", 2)
		if len(parts) == 1 {
			// No slash — standalone
			standalones = append(standalones, &treeNode{
				label:         pi.displayName,
				projectName:   name,
				count:         pi.count,
				worktree:      pi.worktree,
				worktreeLabel: pi.worktreeLabel,
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
			label:         strings.TrimPrefix(pi.displayName, groupKey+"/"),
			projectName:   name,
			count:         pi.count,
			worktree:      pi.worktree,
			worktreeLabel: pi.worktreeLabel,
		})
	}

	// Merge standalones that match a group key into that group as a "(root)" child
	var merged []*treeNode
	for _, s := range standalones {
		if g, ok := groups[s.label]; ok {
			g.children = append([]*treeNode{{
				label:         "(root)",
				projectName:   s.projectName,
				count:         s.count,
				worktree:      s.worktree,
				worktreeLabel: s.worktreeLabel,
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

	// Sort children and compute group counts. A worktree shares its parent's
	// label, so the empty worktreeLabel of the parent sorts it ahead of its
	// worktrees, and worktrees order among themselves by name.
	for _, g := range groups {
		sort.Slice(g.children, func(i, j int) bool {
			return lessTreeNode(g.children[i], g.children[j])
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
		return lessTreeNode(standalones[i], standalones[j])
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
			wtTag = worktreeBadge(node.worktreeLabel)
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
