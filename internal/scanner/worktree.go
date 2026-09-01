package scanner

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// wtResult is the worktree classification for one project path.
type wtResult struct {
	resolved          bool   // false = could not inspect (dir gone / damaged): preserve stored
	isWorktree        bool   // true only for a proven worktree
	parentProjectName string // deriveProjectName of the main worktree root; "" if unknown
	worktreeName      string // leaf dir name of the worktree (badge text)
}

// claudeWorktreeMarker is the path segment Claude Code uses for the worktrees it
// creates by default: <repo-root>/.claude/worktrees/<name>. The parent repo root
// is therefore encoded in the path, which lets us classify such a worktree from
// the path alone even after its directory is deleted.
const claudeWorktreeMarker = "/.claude/worktrees/"

// maxGitPointerFileSize caps reads of git's pointer files. These files are
// small. The cap guards against a hostile repository with a huge file.
const maxGitPointerFileSize = 64 * 1024

// worktreeResolver classifies project paths as worktrees, caching filesystem
// probes so many sessions in the same repository share work within one scan.
type worktreeResolver struct {
	scanPaths []string
	results   map[string]wtResult // by project path
	probes    map[string]gitProbe // by directory
}

func newWorktreeResolver(scanPaths []string) *worktreeResolver {
	return &worktreeResolver{
		scanPaths: scanPaths,
		results:   make(map[string]wtResult),
		probes:    make(map[string]gitProbe),
	}
}

func (r *worktreeResolver) resolve(projectPath string) wtResult {
	if projectPath == "" {
		return wtResult{}
	}
	if cached, ok := r.results[projectPath]; ok {
		return cached
	}
	res := r.compute(projectPath)
	r.results[projectPath] = res
	return res
}

// compute applies detection precedence:
//  1. git metadata classifies the directory (worktree, or an inspected non-worktree);
//  2. else, only when the directory is absent, the Claude .claude/worktrees/<name>
//     path invariant recovers the grouping (main.go still hides gone paths);
//  3. else unresolved (present but damaged, or absent and not Claude-default) so
//     the DB preserves stored values.
func (r *worktreeResolver) compute(projectPath string) wtResult {
	res, absent := r.gitDetect(projectPath)
	if res.resolved {
		return res
	}
	// Only an absent directory falls back to the path invariant. A present but
	// damaged repo returns unresolved, so the DB preserves stored values.
	if absent {
		if wt, ok := claudeWorktreeFromPath(projectPath, r.scanPaths); ok {
			return wt
		}
	}
	return wtResult{}
}

// gitDetect resolves symlinks, then walks up to the nearest .git. Symlink
// resolution first is required: a worktree's recorded cwd may be a subdirectory
// of its root, and a lexical walk through a symlink would miss the real .git.
// It reports absent when the path is gone, which enables the path fallback.
func (r *worktreeResolver) gitDetect(projectPath string) (res wtResult, absent bool) {
	start, err := filepath.EvalSymlinks(projectPath)
	if err != nil {
		return wtResult{}, true // gone (or an unreadable ancestor)
	}
	dir := filepath.Clean(start)
	for {
		probe := r.probeGit(dir)
		switch probe.kind {
		case gitDir:
			return wtResult{resolved: true}, false
		case gitFileWorktree:
			res := wtResult{
				resolved:     true,
				isWorktree:   true,
				worktreeName: filepath.Base(dir),
			}
			// The main worktree root is the parent of a common dir named
			// ".git". A differently named common dir (bare repo, or a
			// separate git dir) leaves the main root unknown.
			if filepath.Base(probe.commonDir) == ".git" {
				res.parentProjectName = deriveProjectName(filepath.Dir(probe.commonDir), r.scanPaths)
			}
			return res, false
		case gitFileOther:
			return wtResult{resolved: true}, false
		case gitUnreadable:
			return wtResult{}, false // damaged: preserve stored
		default: // gitNone
			parent := filepath.Dir(dir)
			if parent == dir {
				return wtResult{resolved: true}, false // no git anywhere
			}
			dir = parent
		}
	}
}

type gitKind int

const (
	gitNone gitKind = iota
	gitDir
	gitFileWorktree
	gitFileOther
	gitUnreadable
)

type gitProbe struct {
	kind      gitKind
	commonDir string // absolute, for worktree/other
}

func (r *worktreeResolver) probeGit(dir string) gitProbe {
	if cached, ok := r.probes[dir]; ok {
		return cached
	}
	p := computeGitProbe(dir)
	r.probes[dir] = p
	return p
}

// computeGitProbe classifies dir/.git. A directory is a main checkout; a file is
// a gitfile that, once its backlink is validated, marks a linked worktree.
func computeGitProbe(dir string) gitProbe {
	gitPath := filepath.Join(dir, ".git")
	fi, err := os.Lstat(gitPath)
	if err != nil {
		if os.IsNotExist(err) {
			return gitProbe{kind: gitNone}
		}
		return gitProbe{kind: gitUnreadable}
	}
	if fi.IsDir() {
		return gitProbe{kind: gitDir}
	}

	// A gitfile: "gitdir: <git-dir>". The path can be relative.
	adminDir, ok := readGitfile(gitPath, dir)
	if !ok {
		return gitProbe{kind: gitUnreadable}
	}

	// Only a linked worktree's git dir is <common>/worktrees/<id> and holds a
	// commondir. A submodule or separate git dir has neither, so it is an
	// inspected non-worktree checkout, not damaged metadata.
	isWorktreeAdmin := filepath.Base(filepath.Dir(adminDir)) == "worktrees"
	commonDir, ok := readPointer(filepath.Join(adminDir, "commondir"), adminDir)
	if !ok {
		if isWorktreeAdmin {
			return gitProbe{kind: gitUnreadable} // damaged worktree admin: preserve
		}
		return gitProbe{kind: gitFileOther, commonDir: adminDir}
	}

	// The backlink is git's own proof that this admin dir owns this worktree. A
	// moved or damaged worktree fails it, so preserve stored values.
	backlink, ok := readPointer(filepath.Join(adminDir, "gitdir"), adminDir)
	if !ok || !samePath(backlink, gitPath) {
		return gitProbe{kind: gitUnreadable}
	}

	return gitProbe{kind: gitFileWorktree, commonDir: commonDir}
}

// readGitfile reads a ".git" gitfile and returns the referenced git dir as an
// absolute, cleaned path. A relative pointer resolves against base (the dir
// holding the gitfile).
func readGitfile(gitPath, base string) (string, bool) {
	line, ok := firstLine(gitPath)
	if !ok {
		return "", false
	}
	const prefix = "gitdir:"
	if !strings.HasPrefix(line, prefix) {
		return "", false
	}
	target := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if target == "" {
		return "", false
	}
	return resolveRelative(target, base), true
}

// readPointer reads a git pointer file (commondir / gitdir backlink) whose
// content is a single path, resolved against base when relative.
func readPointer(path, base string) (string, bool) {
	line, ok := firstLine(path)
	if !ok {
		return "", false
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return "", false
	}
	return resolveRelative(line, base), true
}

func resolveRelative(target, base string) string {
	if !filepath.IsAbs(target) {
		target = filepath.Join(base, target)
	}
	return filepath.Clean(target)
}

// firstLine reads at most the first line of a small, possibly untrusted file.
func firstLine(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	r := bufio.NewReader(io.LimitReader(f, maxGitPointerFileSize))
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return "", false
	}
	return strings.TrimRight(line, "\r\n"), true
}

// claudeWorktreeFromPath classifies a path under <repo>/.claude/worktrees/<name>
// using the path alone, so a deleted Claude-default worktree still groups under
// its parent. It is subordinate to git detection in compute().
func claudeWorktreeFromPath(projectPath string, scanPaths []string) (wtResult, bool) {
	idx := strings.Index(projectPath, claudeWorktreeMarker)
	if idx < 0 {
		return wtResult{}, false
	}
	repoRoot := projectPath[:idx]
	rest := projectPath[idx+len(claudeWorktreeMarker):]
	name := rest
	if slash := strings.IndexByte(rest, '/'); slash >= 0 {
		name = rest[:slash] // ignore any subdirectory below the worktree root
	}
	if name == "" || repoRoot == "" {
		return wtResult{}, false
	}
	return wtResult{
		resolved:          true,
		isWorktree:        true,
		parentProjectName: deriveProjectName(repoRoot, scanPaths),
		worktreeName:      name,
	}, true
}

// canonicalPath resolves symlinks for identity comparison, falling back to a
// lexical clean when the path cannot be resolved (e.g. it no longer exists).
func canonicalPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	if abs, err := filepath.Abs(path); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(path)
}

func samePath(a, b string) bool {
	return canonicalPath(a) == canonicalPath(b)
}
