package scanner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func encodeDirName(path string) string {
	return strings.ReplaceAll(path, "/", "-")
}

func TestDecodeDirName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"-home-user-Code-myproject", "/home/user/Code/myproject"},
		{"-home-user", "/home/user"},
	}
	for _, tt := range tests {
		got := decodeDirName(tt.input)
		if got != tt.want {
			t.Errorf("decodeDirName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestShouldInclude(t *testing.T) {
	home, _ := os.UserHomeDir()
	scanPaths := []string{home + "/Code", home + "/Work"}

	tests := []struct {
		dirName string
		want    bool
	}{
		{encodeDirName(home + "/Code/myproject"), true},
		{encodeDirName(home + "/Work/stuff"), true},
		{encodeDirName(home + "/Other/project"), false},
		{encodeDirName(home), true}, // home dir itself
	}
	for _, tt := range tests {
		got := shouldInclude(tt.dirName, scanPaths)
		if got != tt.want {
			t.Errorf("shouldInclude(%q) = %v, want %v", tt.dirName, got, tt.want)
		}
	}
}

func TestDeriveProjectName(t *testing.T) {
	scanPaths := []string{"/home/user/Code", "/home/user/Projects"}

	tests := []struct {
		path string
		want string
	}{
		{"/home/user/Code/myproject", "Code/myproject"},
		{"/home/user/Projects/myproject", "Projects/myproject"},
		{"/home/user/Code/nested/deep", "Code/nested/deep"},
	}
	for _, tt := range tests {
		got := deriveProjectName(tt.path, scanPaths)
		if got != tt.want {
			t.Errorf("deriveProjectName(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestShouldIncludePathTraversal(t *testing.T) {
	home, _ := os.UserHomeDir()
	scanPaths := []string{home + "/Code"}

	tests := []struct {
		name    string
		dirName string
		want    bool
	}{
		{
			name:    "traversal to etc",
			dirName: encodeDirName(home + "/Code/../../../etc"),
			want:    false,
		},
		{
			name:    "traversal to parent",
			dirName: encodeDirName(home + "/Code/../Secret"),
			want:    false,
		},
		{
			name:    "double dot in middle",
			dirName: encodeDirName(home + "/Code/project/../../other"),
			want:    false,
		},
		{
			name:    "legitimate project",
			dirName: encodeDirName(home + "/Code/myproject"),
			want:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldInclude(tt.dirName, scanPaths)
			if got != tt.want {
				t.Errorf("shouldInclude(%q) = %v, want %v", tt.dirName, got, tt.want)
			}
		})
	}
}

func TestFieldTruncation(t *testing.T) {
	// Create a temporary JSONL file with an extremely long session ID
	tmpDir := t.TempDir()
	jsonlPath := filepath.Join(tmpDir, "test-session.jsonl")

	longSessionID := strings.Repeat("a", 10000)
	longCwd := "/home/user/" + strings.Repeat("x", 10000)
	longBranch := strings.Repeat("b", 500)
	longPrompt := strings.Repeat("c", 5000)

	// Write a JSONL file with oversized fields
	lines := []string{
		fmt.Sprintf(`{"type":"system","sessionId":"%s","cwd":"%s","gitBranch":"%s","timestamp":"2025-01-01T00:00:00Z"}`,
			longSessionID, longCwd, longBranch),
		fmt.Sprintf(`{"type":"message","message":{"role":"user","content":"%s"},"timestamp":"2025-01-01T00:00:01Z"}`,
			longPrompt),
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(jsonlPath, []byte(content), 0o644); err != nil {
		t.Fatalf("writing test JSONL: %v", err)
	}

	scanPaths := []string{"/home/user"}
	session := parseOrphanJSONL(jsonlPath, scanPaths)
	if session == nil {
		t.Fatal("parseOrphanJSONL returned nil")
	}

	if len(session.SessionID) > maxSessionIDLen {
		t.Errorf("SessionID length = %d, want <= %d", len(session.SessionID), maxSessionIDLen)
	}
	if len(session.ProjectPath) > maxCwdLen {
		t.Errorf("ProjectPath length = %d, want <= %d", len(session.ProjectPath), maxCwdLen)
	}
	if len(session.GitBranch) > maxGitBranchLen {
		t.Errorf("GitBranch length = %d, want <= %d", len(session.GitBranch), maxGitBranchLen)
	}
	if len(session.FirstPrompt) > maxFirstPromptLen {
		t.Errorf("FirstPrompt length = %d, want <= %d", len(session.FirstPrompt), maxFirstPromptLen)
	}
}

func TestTruncateField(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello"},
		{"", 5, ""},
		{strings.Repeat("a", 200), 100, strings.Repeat("a", 100)},
	}
	for _, tt := range tests {
		got := truncateField(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncateField(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}

func TestParseTime(t *testing.T) {
	// Valid RFC3339
	ts := parseTime("2025-01-15T10:30:00Z")
	if ts.IsZero() {
		t.Error("expected non-zero time")
	}
	if ts.Year() != 2025 || ts.Month() != 1 || ts.Day() != 15 {
		t.Errorf("unexpected date: %v", ts)
	}

	// Empty string
	ts = parseTime("")
	if !ts.IsZero() {
		t.Error("expected zero time for empty string")
	}

	// RFC3339Nano
	ts = parseTime("2025-01-15T10:30:00.123456789Z")
	if ts.IsZero() {
		t.Error("expected non-zero time for nano format")
	}
}

// writeJSONL builds a transcript from the given lines.
func writeJSONL(t *testing.T, lines []string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("writing JSONL: %v", err)
	}
	return path
}

func TestParseOrphanJSONLAITitle(t *testing.T) {
	head := `{"type":"system","sessionId":"s1","cwd":"/home/user/p","gitBranch":"main","timestamp":"2026-01-01T00:00:00Z"}`
	title := `{"type":"ai-title","aiTitle":"Fix the flaky test","sessionId":"s1"}`
	filler := `{"type":"assistant","message":{"role":"assistant","content":"x"},"timestamp":"2026-01-01T00:00:01Z"}`

	tests := []struct {
		name  string
		lines []string
		want  string
	}{
		{"early", []string{head, title, filler}, "Fix the flaky test"},
		{"absent", []string{head, filler}, ""},
		{"empty value", []string{head, `{"type":"ai-title","aiTitle":""}`, filler}, ""},
	}

	// Past the 15-line metadata pass, so the full-file loop has to catch it.
	late := []string{head}
	for i := 0; i < 25; i++ {
		late = append(late, filler)
	}
	late = append(late, title)
	tests = append(tests, struct {
		name  string
		lines []string
		want  string
	}{"after line 15", late, "Fix the flaky test"})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := parseOrphanJSONL(writeJSONL(t, tt.lines), []string{"/home/user"})
			if s == nil {
				t.Fatal("parseOrphanJSONL returned nil")
			}
			if s.Title != tt.want {
				t.Errorf("Title = %q, want %q", s.Title, tt.want)
			}
		})
	}
}

func TestParseOrphanJSONLTitleTruncated(t *testing.T) {
	head := `{"type":"system","sessionId":"s1","cwd":"/home/user/p","timestamp":"2026-01-01T00:00:00Z"}`
	long := fmt.Sprintf(`{"type":"ai-title","aiTitle":"%s"}`, strings.Repeat("t", 5000))

	s := parseOrphanJSONL(writeJSONL(t, []string{head, long}), []string{"/home/user"})
	if s == nil {
		t.Fatal("parseOrphanJSONL returned nil")
	}
	if len(s.Title) > maxTitleLen {
		t.Errorf("Title length = %d, want <= %d", len(s.Title), maxTitleLen)
	}
}
