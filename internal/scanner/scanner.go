package scanner

import (
	"bufio"
	"encoding/json"
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
	MessageCount    int
	Created         time.Time
	Modified        time.Time
	GitBranch       string
	IsSidechain     bool
	JSONLPath       string
}

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

// jsonlEntry is a minimal representation of a JSONL line for orphan parsing.
type jsonlEntry struct {
	Type        string          `json:"type"`
	SessionID   string          `json:"sessionId"`
	Cwd         string          `json:"cwd"`
	GitBranch   string          `json:"gitBranch"`
	IsSidechain bool            `json:"isSidechain"`
	Timestamp   string          `json:"timestamp"`
	Message     json.RawMessage `json:"message"`
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
)

// truncateField truncates a string to maxLen if it exceeds the limit.
func truncateField(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// parseOrphanJSONL reads the first ~15 lines of a JSONL file to extract session metadata.
func parseOrphanJSONL(path string, scanPaths []string) *Session {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	sessionID := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	var (
		cwd            string
		gitBranch      string
		firstTimestamp string
		lastTimestamp  string
		firstPrompt    string
		isSidechain    bool
		msgCount       int
	)

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer for long lines

	// First pass: read first 15 lines for metadata (cwd, branch, firstPrompt, etc.)
	for i := 0; i < 15 && sc.Scan(); i++ {
		var entry jsonlEntry
		if err := json.Unmarshal(sc.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type == "file-history-snapshot" {
			continue
		}
		if entry.SessionID != "" {
			sessionID = entry.SessionID
		}
		if entry.Cwd != "" && cwd == "" {
			cwd = entry.Cwd
		}
		if entry.GitBranch != "" && gitBranch == "" {
			gitBranch = entry.GitBranch
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
		if text, ok := extractUserMessage(entry.Message); ok && firstPrompt == "" {
			firstPrompt = text
		}
		// Count user/assistant messages
		if len(entry.Message) > 0 {
			var mc messageContent
			if json.Unmarshal(entry.Message, &mc) == nil {
				if mc.Role == "user" || mc.Role == "assistant" {
					msgCount++
				}
			}
		}
	}

	// Continue scanning remaining lines for lastTimestamp and msgCount
	for sc.Scan() {
		var entry jsonlEntry
		if err := json.Unmarshal(sc.Bytes(), &entry); err != nil {
			continue
		}
		if entry.Type == "file-history-snapshot" {
			continue
		}
		if entry.Timestamp != "" {
			lastTimestamp = entry.Timestamp
		}
		if len(entry.Message) > 0 {
			var mc messageContent
			if json.Unmarshal(entry.Message, &mc) == nil {
				if mc.Role == "user" || mc.Role == "assistant" {
					msgCount++
				}
			}
		}
	}

	if cwd == "" {
		return nil
	}

	// Truncate fields to prevent unbounded memory usage from malicious JSONL
	sessionID = truncateField(sessionID, maxSessionIDLen)
	cwd = truncateField(cwd, maxCwdLen)
	gitBranch = truncateField(gitBranch, maxGitBranchLen)
	firstPrompt = truncateField(firstPrompt, maxFirstPromptLen)

	return &Session{
		SessionID:   sessionID,
		ProjectName: deriveProjectName(cwd, scanPaths),
		ProjectPath: cwd,
		ClaudeDir:   filepath.Dir(path),
		FirstPrompt: firstPrompt,
		MessageCount: msgCount,
		Created:     parseTime(firstTimestamp),
		Modified:    parseTime(lastTimestamp),
		GitBranch:   gitBranch,
		IsSidechain: isSidechain,
		JSONLPath:   path,
	}
}

// Scan discovers Claude Code sessions from disk.
func Scan(cfg config.Config) ([]Session, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	projectsDir := filepath.Join(home, ".claude", "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	sessions := make(map[string]Session)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if !shouldInclude(entry.Name(), cfg.ScanPaths) {
			continue
		}

		dirPath := filepath.Join(projectsDir, entry.Name())
		indexedIDs := make(map[string]bool)

		// Source 1: sessions-index.json
		indexPath := filepath.Join(dirPath, "sessions-index.json")
		if data, err := os.ReadFile(indexPath); err == nil {
			var idx indexFile
			if json.Unmarshal(data, &idx) == nil {
				for _, e := range idx.Entries {
					if e.SessionID == "" {
						continue
					}
					indexedIDs[e.SessionID] = true
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
		}

		// Source 2: orphan JSONL files
		if cfg.ScanOrphans {
			jsonlFiles, _ := filepath.Glob(filepath.Join(dirPath, "*.jsonl"))
			for _, jf := range jsonlFiles {
				stem := strings.TrimSuffix(filepath.Base(jf), ".jsonl")
				if indexedIDs[stem] {
					continue
				}
				if s := parseOrphanJSONL(jf, cfg.ScanPaths); s != nil {
					if _, exists := sessions[s.SessionID]; !exists {
						sessions[s.SessionID] = *s
					}
				}
			}
		}
	}

	result := make([]Session, 0, len(sessions))
	for _, s := range sessions {
		result = append(result, s)
	}
	return result, nil
}
