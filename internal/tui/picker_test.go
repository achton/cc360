package tui

import (
	"sort"
	"strings"
	"testing"

	"github.com/achton/cc360/internal/db"
)

// pickerSessions holds the shapes that share a tree label: a repo with two
// worktrees, and a repo whose worktree never resolved a name.
func pickerSessions() []db.Session {
	wt := func(id, name, parent, label string) db.Session {
		return db.Session{
			SessionID: id, ProjectName: name, ProjectPath: "/tmp",
			IsWorktree: true, ParentProjectName: parent, WorktreeName: label,
		}
	}
	return []db.Session{
		{SessionID: "1", ProjectName: "Code/api-gateway", ProjectPath: "/tmp"},
		wt("2", "Code/api-gateway-a", "Code/api-gateway", "pr-847"),
		wt("3", "Code/api-gateway-b", "Code/api-gateway", "design-tokens"),
		{SessionID: "4", ProjectName: "Code/frontend", ProjectPath: "/tmp"},
		wt("5", "Code/frontend-a", "Code/frontend", ""),
		{SessionID: "6", ProjectName: "Code/infra", ProjectPath: "/tmp"},
	}
}

// layout renders the tree as a comparable string.
func layout(p *projectPicker) string {
	var b strings.Builder
	for _, root := range p.roots {
		b.WriteString(root.label + "|" + root.projectName + "\n")
		for _, c := range root.children {
			b.WriteString("  " + c.label + "|" + c.projectName + "\n")
		}
	}
	return b.String()
}

func TestPickerOrderingIsStableAcrossCalls(t *testing.T) {
	var first string
	// Go randomizes map iteration per range, so repeat enough to catch a
	// reintroduced dependency on map order.
	for i := 0; i < 200; i++ {
		p := &projectPicker{}
		p.open(pickerSessions(), nil)
		got := layout(p)
		if i == 0 {
			first = got
			continue
		}
		if got != first {
			t.Fatalf("tree changed on call %d\nfirst:\n%s\ngot:\n%s", i+1, first, got)
		}
	}
}

func TestPickerOrderingGroupsWorktreesUnderTheirRepo(t *testing.T) {
	p := &projectPicker{}
	p.open(pickerSessions(), nil)

	want := strings.Join([]string{
		"Code|",
		"  api-gateway|Code/api-gateway",
		"  api-gateway|Code/api-gateway-b",
		"  api-gateway|Code/api-gateway-a",
		"  frontend|Code/frontend",
		"  frontend|Code/frontend-a",
		"  infra|Code/infra",
		"",
	}, "\n")

	if got := layout(p); got != want {
		t.Errorf("tree layout:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// Two nodes that share a label and a worktree name must still have an order.
// Without one, the sort leaves them as it found them.
func TestLessTreeNodeIsATotalOrder(t *testing.T) {
	a := &treeNode{label: "repo", worktreeLabel: "", projectName: "Code/repo-a"}
	b := &treeNode{label: "repo", worktreeLabel: "", projectName: "Code/repo-b"}

	if !lessTreeNode(a, b) {
		t.Error("a must sort before b")
	}
	if lessTreeNode(b, a) {
		t.Error("b must not sort before a")
	}

	nodes := []*treeNode{b, a}
	sort.Slice(nodes, func(i, j int) bool { return lessTreeNode(nodes[i], nodes[j]) })
	if nodes[0] != a {
		t.Errorf("sorted first = %q, want %q", nodes[0].projectName, a.projectName)
	}
}

// Group counts must sum the children whatever order they were built in.
func TestPickerGroupCounts(t *testing.T) {
	p := &projectPicker{}
	p.open(pickerSessions(), nil)

	if len(p.roots) != 1 {
		t.Fatalf("got %d roots, want 1", len(p.roots))
	}
	if got := p.roots[0].count; got != 6 {
		t.Errorf("group count = %d, want 6", got)
	}
}
