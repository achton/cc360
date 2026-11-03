package scanner

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/achton/cc360/internal/config"
)

// Session holds metadata about a single Claude Code session.
type Session struct {
	SessionID       string
	ProjectName     string
	ProjectPath     string
	ClaudeDir       string
	FirstPrompt     string
	ExistingSummary string
	Title           string // latest ai-title
	CustomTitle     string // name given with /rename, or the agent name
	MessageCount    int
	Created         time.Time
	Modified        time.Time
	GitBranch       string
	IsSidechain     bool
	JSONLPath       string

	// PRLinks lists the PRs the session touched as "owner/repo#N", oldest
	// first. CostUSD and the line counts are the session's final totals.
	PRLinks      []string
	CostUSD      float64
	LinesAdded   int
	LinesRemoved int
	// Entrypoint is how the session was started: cli, claude-desktop,
	// claude-vscode, or sdk-* for `claude -p` and the Agent SDK.
	Entrypoint string

	// The transcript's size and mtime when it was parsed, so an unchanged file
	// is not parsed again. Zero for sessions that came from the index.
	FileSize    int64
	FileModNano int64

	// Worktree metadata, resolved from git after the session is parsed.
	// WorktreeResolved is true when resolution could inspect the project on disk.
	// When it is false, the DB layer keeps the stored values.
	WorktreeResolved  bool
	IsWorktree        bool
	ParentProjectName string // deriveProjectName of the main worktree root
	WorktreeName      string // leaf dir name of the worktree (badge text)
}

// ParserVersion changes whenever parseOrphanJSONL starts reading something
// new, so sessions cached by an older parser are parsed again.
const ParserVersion = 2

// Cache holds sessions from an earlier scan, keyed by transcript path.
type Cache map[string]Session

// indexEntry matches the JSON structure in sessions-index.json.
type indexEntry struct {
	SessionID   string `json:"sessionId"`
	FullPath    string `json:"fullPath"`
	FirstPrompt string `json:"firstPrompt"`
	Summary     string `json:"summary"`
	MsgCount    int    `json:"messageCount"`
	Created     string `json:"created"`
	Modified    string `json:"modified"`
	GitBranch   string `json:"gitBranch"`
	ProjectPath string `json:"projectPath"`
	IsSidechain bool   `json:"isSidechain"`
}

type indexFile struct {
	Entries []indexEntry `json:"entries"`
}

// jsonlEntry is the subset of a transcript record the scanner reads.
// decodeEntry maps the JSON keys.
type jsonlEntry struct {
	Type             string
	SessionID        string
	Cwd              string
	GitBranch        string
	IsSidechain      bool
	IsMeta           bool
	IsCompactSummary bool
	Timestamp        string
	AITitle          string
	CustomTitle      string
	AgentName        string
	Entrypoint       string
	PRNumber         int64
	PRRepository     string
	TotalCostUSD     float64
	LinesAdded       int64
	LinesRemoved     int64
	Message          json.RawMessage
}

type messageContent struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// decodeDirName converts an encoded Claude project dir name back to the
// original absolute path. Claude replaces / with - in dir names.
func decodeDirName(name string) string {
	return strings.ReplaceAll(name, "-", "/")
}

// shouldInclude checks if a Claude project dir falls under any scan path.
// It rejects paths containing ".." to prevent path traversal attacks.
func shouldInclude(dirName string, scanPaths []string) bool {
	decoded := decodeDirName(dirName)
	cleaned := filepath.Clean(decoded)
	// Reject paths that contain ".." traversal components
	if strings.Contains(cleaned, "..") {
		return false
	}
	home, _ := os.UserHomeDir()
	if cleaned == home {
		return true
	}
	for _, sp := range scanPaths {
		if strings.HasPrefix(cleaned, sp+"/") || cleaned == sp {
			return true
		}
	}
	return false
}

// deriveProjectName returns a short name that includes the scan path's
// own directory name. E.g. for path "/home/user/Code/private/myproject" with
// scan path "~/Code", returns "Code/private/myproject".
func deriveProjectName(projectPath string, scanPaths []string) string {
	for _, sp := range scanPaths {
		// Get the parent of the scan path so we keep its directory name
		parent := filepath.Dir(sp)
		if !strings.HasSuffix(parent, "/") {
			parent += "/"
		}
		if strings.HasPrefix(projectPath, parent) {
			return projectPath[len(parent):]
		}
	}
	home, _ := os.UserHomeDir()
	if strings.HasPrefix(projectPath, home+"/") {
		return "~/" + projectPath[len(home)+1:]
	}
	return projectPath
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, _ = time.Parse(time.RFC3339, s)
	}
	return t
}

// extractUserMessage tries to get the text content from a message field.
func extractUserMessage(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var msg messageContent
	if err := json.Unmarshal(raw, &msg); err != nil {
		return "", false
	}
	if msg.Role != "user" {
		return "", false
	}

	switch c := msg.Content.(type) {
	case string:
		if c != "" && c != "[Request interrupted by user]" {
			return c, true
		}
	case []any:
		var texts []string
		for _, item := range c {
			if m, ok := item.(map[string]any); ok {
				if m["type"] == "text" {
					if t, ok := m["text"].(string); ok && t != "" {
						texts = append(texts, t)
					}
				}
			}
		}
		joined := strings.Join(texts, " ")
		if joined != "" && joined != "[Request interrupted by user]" {
			return joined, true
		}
	}
	return "", false
}

// Field length limits for parsed JSONL metadata.
const (
	maxSessionIDLen   = 100
	maxCwdLen         = 4096
	maxGitBranchLen   = 256
	maxFirstPromptLen = 1000
	maxTitleLen       = 200
	maxSidecarLen     = 64 << 10
	maxPRLinks        = 20
)

// truncateField truncates a string to maxLen if it exceeds the limit.
func truncateField(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// maxLineLen caps how much of one transcript record is buffered. A longer
// record is skipped, and the scan carries on with the next one.
const maxLineLen = 64 << 20

// lineReader yields transcript lines of any length, unlike bufio.Scanner, which
// stops the whole scan at the first line over its limit.
type lineReader struct {
	r   *bufio.Reader
	max int
	buf []byte
}

// next returns the next line without its newline, or nil for a skipped
// oversized one. The slice is only valid until the following call.
func (l *lineReader) next() ([]byte, error) {
	l.buf = l.buf[:0]
	tooLong := false
	for {
		chunk, err := l.r.ReadSlice('\n')
		if !tooLong {
			if len(l.buf)+len(chunk) > l.max {
				tooLong = true
				l.buf = l.buf[:0]
			} else {
				l.buf = append(l.buf, chunk...)
			}
		}
		switch {
		case err == bufio.ErrBufferFull:
			continue
		case err == io.EOF && (len(l.buf) > 0 || tooLong):
			// A last line without a trailing newline.
		case err != nil:
			return nil, err
		}
		if tooLong {
			return nil, nil
		}
		return bytes.TrimRight(l.buf, "\r\n"), nil
	}
}

// skippedRecordPrefixes are bulky bookkeeping records that carry nothing the
// scanner reads. API request snapshots (Claude Code 2.1.300+) hold whole
// prompts, and their "message" would otherwise pass for a conversation turn.
var skippedRecordPrefixes = [][]byte{
	[]byte(`{"type":"api-request`),
	[]byte(`{"type":"file-history-`),
}

func skipRecord(line []byte) bool {
	for _, p := range skippedRecordPrefixes {
		if bytes.HasPrefix(line, p) {
			return true
		}
	}
	return false
}

// customTitleSidecar is written next to the transcript when a session is
// renamed, including from outside the CLI.
type customTitleSidecar struct {
	CustomTitle string `json:"customTitle"`
}

// readCustomTitleSidecar reads the sidecar of the transcript at path. The
// directory is named after the file, never after a sessionId field, so
// transcript content cannot point the read elsewhere.
func readCustomTitleSidecar(path string) string {
	f, err := os.Open(filepath.Join(strings.TrimSuffix(path, ".jsonl"), "custom-title.json"))
	if err != nil {
		return ""
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxSidecarLen))
	if err != nil {
		return ""
	}
	var sc customTitleSidecar
	if json.Unmarshal(data, &sc) != nil {
		return ""
	}
	return sc.CustomTitle
}

// parseOrphanJSONL reads a whole transcript for session metadata.
func parseOrphanJSONL(path string, scanPaths []string) *Session {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	sessionID := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	var (
		idFromRecord   bool
		cwd            string
		gitBranch      string
		firstTimestamp string
		lastTimestamp  string
		firstPrompt    string
		aiTitle        string
		customTitle    string
		agentName      string
		isSidechain    bool
		msgCount       int
		prLinks        []string
		cost           jsonlEntry
		entrypoint     string
	)

	lr := lineReader{r: bufio.NewReaderSize(f, 64<<10), max: maxLineLen}
	var entry jsonlEntry
	for {
		line, err := lr.next()
		if err != nil {
			break
		}
		if len(line) == 0 || skipRecord(line) {
			continue
		}
		if !decodeEntry(line, &entry) {
			continue
		}
		if entry.SessionID != "" && !idFromRecord {
			sessionID = entry.SessionID
			idFromRecord = true
		}
		if entry.Cwd != "" && cwd == "" {
			cwd = entry.Cwd
		}
		if entry.IsSidechain {
			isSidechain = true
		}
		if entry.Timestamp != "" {
			if firstTimestamp == "" {
				firstTimestamp = entry.Timestamp
			}
			lastTimestamp = entry.Timestamp
		}

		// A session started with -p and resumed interactively is interactive.
		if entry.Entrypoint != "" && (entrypoint == "" || IsSDKEntrypoint(entrypoint)) {
			entrypoint = entry.Entrypoint
		}

		switch entry.Type {
		case "pr-link":
			if entry.PRNumber > 0 && entry.PRRepository != "" {
				prLinks = appendPRLink(prLinks, fmt.Sprintf("%s#%d", entry.PRRepository, entry.PRNumber))
			}
		case "cost-state":
			// Each record holds the running totals; the last is final.
			cost = entry
		case "ai-title":
			// Claude Code regenerates the title; the last one is current.
			if entry.AITitle != "" {
				aiTitle = entry.AITitle
			}
		case "custom-title":
			if entry.CustomTitle != "" {
				customTitle = entry.CustomTitle
			}
		case "agent-name":
			if entry.AgentName != "" {
				agentName = entry.AgentName
			}
		case "user", "assistant":
			// The branch can change mid-session; show the latest.
			if entry.GitBranch != "" {
				gitBranch = entry.GitBranch
			}
			// Meta records are injected context (e.g. the local-command
			// caveat), not something the user said.
			if entry.IsMeta || entry.IsCompactSummary {
				continue
			}
			msgCount++
			if firstPrompt == "" && entry.Type == "user" {
				if text, ok := extractUserMessage(entry.Message); ok {
					firstPrompt = text
				}
			}
		}
	}

	if cwd == "" {
		return nil
	}

	dir := filepath.Dir(path)
	if t := readCustomTitleSidecar(path); t != "" {
		customTitle = t
	} else if customTitle == "" {
		customTitle = agentName
	}

	// Truncate fields to prevent unbounded memory usage from malicious JSONL
	sessionID = truncateField(sessionID, maxSessionIDLen)
	cwd = truncateField(cwd, maxCwdLen)
	gitBranch = truncateField(gitBranch, maxGitBranchLen)
	firstPrompt = truncateField(firstPrompt, maxFirstPromptLen)
	aiTitle = truncateField(aiTitle, maxTitleLen)
	customTitle = truncateField(customTitle, maxTitleLen)
	entrypoint = truncateField(entrypoint, maxGitBranchLen)

	return &Session{
		SessionID:    sessionID,
		ProjectName:  deriveProjectName(cwd, scanPaths),
		ProjectPath:  cwd,
		ClaudeDir:    dir,
		FirstPrompt:  firstPrompt,
		Title:        aiTitle,
		CustomTitle:  customTitle,
		MessageCount: msgCount,
		Created:      parseTime(firstTimestamp),
		Modified:     parseTime(lastTimestamp),
		GitBranch:    gitBranch,
		IsSidechain:  isSidechain,
		JSONLPath:    path,
		PRLinks:      prLinks,
		CostUSD:      cost.TotalCostUSD,
		LinesAdded:   int(cost.LinesAdded),
		LinesRemoved: int(cost.LinesRemoved),
		Entrypoint:   entrypoint,
	}
}

// IsSDKEntrypoint reports a programmatic session: `claude -p` or the Agent SDK.
func IsSDKEntrypoint(e string) bool {
	return strings.HasPrefix(e, "sdk-")
}

// appendPRLink adds a PR once, moving a repeat to the end so the last entry is
// the most recent one. The list is capped, dropping the oldest.
func appendPRLink(links []string, link string) []string {
	link = truncateField(link, maxGitBranchLen)
	for i, l := range links {
		if l == link {
			links = append(links[:i], links[i+1:]...)
			break
		}
	}
	links = append(links, link)
	if len(links) > maxPRLinks {
		links = links[len(links)-maxPRLinks:]
	}
	return links
}

// loadTranscript parses a transcript, or reuses the cached session when the
// file has not changed since. Any change parses the whole file again, because
// Claude Code also rewrites transcripts in place, not only appends to them.
func loadTranscript(path string, scanPaths []string, cache Cache) *Session {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	size, mod := info.Size(), info.ModTime().UnixNano()
	if c, ok := cache[path]; ok && c.FileSize == size && c.FileModNano == mod {
		c.ProjectName = deriveProjectName(c.ProjectPath, scanPaths)
		c.ClaudeDir = filepath.Dir(path)
		c.JSONLPath = path
		// A rename from outside the CLI rewrites only the sidecar.
		if t := readCustomTitleSidecar(path); t != "" {
			c.CustomTitle = truncateField(t, maxTitleLen)
		}
		return &c
	}
	s := parseOrphanJSONL(path, scanPaths)
	if s != nil {
		s.FileSize, s.FileModNano = size, mod
	}
	return s
}

// Scan discovers Claude Code sessions from disk, reading every dir in
// cfg.ClaudeHomes. An earlier home wins, so that list is the precedence order.
// cache may be nil, which parses every transcript.
func Scan(cfg config.Config, cache Cache) ([]Session, error) {
	sessions := make(map[string]Session)
	for _, home := range cfg.ClaudeHomes {
		if err := scanHome(home, cfg, cache, sessions); err != nil {
			return nil, err
		}
	}

	// Resolve worktree metadata once per session, sharing a cache across the
	// whole scan so sessions in the same repo reuse probe results.
	resolver := newWorktreeResolver(cfg.ScanPaths)
	result := make([]Session, 0, len(sessions))
	for _, s := range sessions {
		wt := resolver.resolve(s.ProjectPath)
		s.WorktreeResolved = wt.resolved
		s.IsWorktree = wt.isWorktree
		s.ParentProjectName = wt.parentProjectName
		s.WorktreeName = wt.worktreeName
		result = append(result, s)
	}
	return result, nil
}

// scanHome adds one Claude config dir's sessions to sessions, keyed by session
// ID. A missing projects dir is skipped, because a second config need not exist
// on every machine. Existing entries stay, so an earlier home wins.
func scanHome(home string, cfg config.Config, cache Cache, sessions map[string]Session) error {
	projectsDir := filepath.Join(home, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if !shouldInclude(entry.Name(), cfg.ScanPaths) {
			continue
		}

		dirPath := filepath.Join(projectsDir, entry.Name())

		// The transcript is the live record. The index stopped being written
		// in February 2026, so an indexed session resumed since then would
		// otherwise keep its old date and count.
		fromJSONL := make(map[string]bool)
		if cfg.ScanOrphans {
			jsonlFiles, _ := filepath.Glob(filepath.Join(dirPath, "*.jsonl"))
			for _, jf := range jsonlFiles {
				s := loadTranscript(jf, cfg.ScanPaths, cache)
				if s == nil {
					continue
				}
				if _, exists := sessions[s.SessionID]; exists {
					continue
				}
				sessions[s.SessionID] = *s
				fromJSONL[s.SessionID] = true
			}
		}

		// Legacy sessions-index.json: fills in sessions without a transcript,
		// and the summary it alone carries.
		indexPath := filepath.Join(dirPath, "sessions-index.json")
		data, err := os.ReadFile(indexPath)
		if err != nil {
			continue
		}
		var idx indexFile
		if json.Unmarshal(data, &idx) != nil {
			continue
		}
		for _, e := range idx.Entries {
			if e.SessionID == "" {
				continue
			}
			if fromJSONL[e.SessionID] {
				s := sessions[e.SessionID]
				if s.ExistingSummary == "" {
					s.ExistingSummary = e.Summary
				}
				sessions[e.SessionID] = s
				continue
			}
			if _, exists := sessions[e.SessionID]; exists {
				continue
			}
			sessions[e.SessionID] = Session{
				SessionID:       e.SessionID,
				ProjectName:     deriveProjectName(e.ProjectPath, cfg.ScanPaths),
				ProjectPath:     e.ProjectPath,
				ClaudeDir:       dirPath,
				FirstPrompt:     e.FirstPrompt,
				ExistingSummary: e.Summary,
				MessageCount:    e.MsgCount,
				Created:         parseTime(e.Created),
				Modified:        parseTime(e.Modified),
				GitBranch:       e.GitBranch,
				IsSidechain:     e.IsSidechain,
				JSONLPath:       e.FullPath,
			}
		}
	}
	return nil
}
