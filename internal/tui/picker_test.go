package tui

import (
	"strings"
	"testing"

	"github.com/achton/cc360/internal/db"
)

// pickerSessions covers the case that used to shuffle: a project and its
// worktree, which simplify to the same tree label.
func pickerSessions() []db.Session {
	return []db.Session{
		{SessionID: "1", ProjectName: "Code/api-gateway"},
		{SessionID: "2", ProjectName: "Code/api-gateway"},
		{SessionID: "3", ProjectName: "Code/api-gateway/.claude/worktrees/pr-847"},
		{SessionID: "4", ProjectName: "Code/frontend"},
		{SessionID: "5", ProjectName: "Code/frontend/.claude/worktrees/design-tokens"},
		{SessionID: "6", ProjectName: "Code/infra"},
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
	// Go randomises map iteration per range, so repeat enough to catch a
	// reintroduced map-order dependency.
	for i := 0; i < 50; i++ {
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

func TestPickerOrderingTiebreaksOnProjectName(t *testing.T) {
	p := &projectPicker{}
	p.open(pickerSessions(), nil)

	want := strings.Join([]string{
		"Code|",
		"  api-gateway|Code/api-gateway",
		"  api-gateway|Code/api-gateway/.claude/worktrees/pr-847",
		"  frontend|Code/frontend",
		"  frontend|Code/frontend/.claude/worktrees/design-tokens",
		"  infra|Code/infra",
		"",
	}, "\n")

	if got := layout(p); got != want {
		t.Errorf("tree layout:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// Group counts must sum the children regardless of build order.
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
