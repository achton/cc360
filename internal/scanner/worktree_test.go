package scanner

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// git runs a git command in dir, skipping the test if git is unavailable and
// failing it on any git error.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// newRepo creates a git repository with one commit and returns its root.
func newRepo(t *testing.T, parent, name string) string {
	t.Helper()
	root := filepath.Join(parent, name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, root, "init", "-q")
	git(t, root, "commit", "-q", "--allow-empty", "-m", "init")
	return root
}

func TestResolveMainCheckout(t *testing.T) {
	tmp := t.TempDir()
	repo := newRepo(t, tmp, "myrepo")

	r := newWorktreeResolver([]string{tmp})
	got := r.resolve(repo)

	if !got.resolved || got.isWorktree {
		t.Fatalf("main checkout: got resolved=%v isWorktree=%v, want resolved=true isWorktree=false", got.resolved, got.isWorktree)
	}
}

func TestResolveLinkedWorktreeSibling(t *testing.T) {
	tmp := t.TempDir()
	repo := newRepo(t, tmp, "myrepo")
	wt := filepath.Join(tmp, "myrepo-worktrees", "feature-x")
	git(t, repo, "worktree", "add", "-q", "-b", "feature-x", wt)

	r := newWorktreeResolver([]string{tmp})
	got := r.resolve(wt)

	if !got.resolved || !got.isWorktree {
		t.Fatalf("sibling worktree: got resolved=%v isWorktree=%v, want both true", got.resolved, got.isWorktree)
	}
	if got.worktreeName != "feature-x" {
		t.Errorf("worktreeName = %q, want %q", got.worktreeName, "feature-x")
	}
	if got.parentProjectName == "" {
		t.Errorf("parentProjectName is empty, want the main repo name")
	}
	if got.repoKey == "" || filepath.Base(got.repoKey) != ".git" {
		t.Errorf("repoKey = %q, want the main repo's .git dir", got.repoKey)
	}
}

func TestResolveWorktreeUnderClaudeDir(t *testing.T) {
	tmp := t.TempDir()
	repo := newRepo(t, tmp, "myrepo")
	wt := filepath.Join(repo, ".claude", "worktrees", "pr-42")
	git(t, repo, "worktree", "add", "-q", "-b", "pr-42", wt)

	r := newWorktreeResolver([]string{tmp})
	got := r.resolve(wt)

	if !got.resolved || !got.isWorktree || got.worktreeName != "pr-42" {
		t.Fatalf("claude worktree: got %+v, want isWorktree with name pr-42", got)
	}
}

func TestResolveSubdirOfWorktree(t *testing.T) {
	tmp := t.TempDir()
	repo := newRepo(t, tmp, "myrepo")
	wt := filepath.Join(tmp, "wt", "feat")
	git(t, repo, "worktree", "add", "-q", "-b", "feat", wt)
	sub := filepath.Join(wt, "pkg", "inner")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	r := newWorktreeResolver([]string{tmp})
	got := r.resolve(sub)

	if !got.isWorktree || got.worktreeName != "feat" {
		t.Fatalf("subdir of worktree: got %+v, want isWorktree with name feat (the worktree root)", got)
	}
}

func TestResolveNonGitDir(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "plain")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	r := newWorktreeResolver([]string{tmp})
	got := r.resolve(dir)

	if !got.resolved || got.isWorktree {
		t.Fatalf("non-git dir: got resolved=%v isWorktree=%v, want resolved=true isWorktree=false", got.resolved, got.isWorktree)
	}
}

func TestResolveDeletedDirIsUnresolved(t *testing.T) {
	tmp := t.TempDir()
	gone := filepath.Join(tmp, "does-not-exist")

	r := newWorktreeResolver([]string{tmp})
	got := r.resolve(gone)

	if got.resolved || got.isWorktree {
		t.Fatalf("deleted dir: got resolved=%v isWorktree=%v, want both false (unresolved, preserve stored)", got.resolved, got.isWorktree)
	}
}

// A deleted worktree under .claude/worktrees/ is still classified from its path
// alone, so it keeps its parent grouping without the directory on disk.
func TestClaudeWorktreePathFallback(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		wantName string
	}{
		{"root", "/home/u/Code/proj/.claude/worktrees/pr-9", "pr-9"},
		{"subdir below worktree", "/home/u/Code/proj/.claude/worktrees/pr-9/src/app", "pr-9"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := claudeWorktreeFromPath(tt.path, []string{"/home/u/Code"})
			if !ok || !got.isWorktree || got.worktreeName != tt.wantName {
				t.Fatalf("got %+v ok=%v, want isWorktree name=%q", got, ok, tt.wantName)
			}
			if got.parentProjectName == "" {
				t.Errorf("parentProjectName empty, want the repo name from the stripped path")
			}
		})
	}
}

// A .git file whose admin dir backlink does not point back to it (a moved or
// damaged worktree, or a submodule) must not be treated as a worktree.
func TestBacklinkMismatchRejected(t *testing.T) {
	tmp := t.TempDir()
	wt := filepath.Join(tmp, "wt")
	admin := filepath.Join(tmp, "main", ".git", "worktrees", "wt")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(admin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(wt, ".git"), "gitdir: "+admin+"\n")
	writeFile(t, filepath.Join(admin, "commondir"), "../..\n")
	// Backlink points somewhere else, not wt/.git.
	writeFile(t, filepath.Join(admin, "gitdir"), filepath.Join(tmp, "other", ".git")+"\n")

	r := newWorktreeResolver([]string{tmp})
	got := r.resolve(wt)

	if got.isWorktree {
		t.Fatalf("backlink mismatch: got isWorktree=true, want false")
	}
}

// Relative pointer files must resolve against the correct base directory.
func TestRelativePointerResolution(t *testing.T) {
	tmp := t.TempDir()
	base := filepath.Join(tmp, "a", "b")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(base, "commondir"), "../../shared/.git\n")

	got, ok := readPointer(filepath.Join(base, "commondir"), base)
	want := filepath.Join(tmp, "shared", ".git")
	if !ok || got != want {
		t.Fatalf("readPointer relative = %q ok=%v, want %q", got, ok, want)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A submodule (a .git file pointing at .git/modules/<name>, with no commondir)
// is an inspected non-worktree, not damaged metadata: resolved, not a worktree.
func TestResolveSubmoduleIsNonWorktree(t *testing.T) {
	tmp := t.TempDir()
	sub := filepath.Join(tmp, "super", "sub")
	mod := filepath.Join(tmp, "super", ".git", "modules", "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(mod, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sub, ".git"), "gitdir: "+mod+"\n")

	got := newWorktreeResolver([]string{tmp}).resolve(sub)
	if !got.resolved || got.isWorktree {
		t.Fatalf("submodule: got resolved=%v isWorktree=%v, want resolved=true isWorktree=false", got.resolved, got.isWorktree)
	}
}

// An existing plain repository under .claude/worktrees/ must not be badged by
// the path fallback: git metadata is authoritative when the directory exists.
func TestClaudeMarkerPathNotBadgedWhenRealRepo(t *testing.T) {
	tmp := t.TempDir()
	parent := filepath.Join(tmp, "outer", ".claude", "worktrees")
	repo := newRepo(t, parent, "foo")

	got := newWorktreeResolver([]string{tmp}).resolve(repo)
	if got.isWorktree {
		t.Fatalf("real repo under .claude/worktrees: got isWorktree=true, want false")
	}
}

// The path fallback still recovers an absent Claude-default worktree.
func TestClaudeMarkerPathFallbackWhenAbsent(t *testing.T) {
	got := newWorktreeResolver([]string{"/nope"}).resolve("/nope/proj/.claude/worktrees/gone")
	if !got.isWorktree || got.worktreeName != "gone" {
		t.Fatalf("absent Claude worktree: got %+v, want isWorktree name=gone", got)
	}
}
