package scanner

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/achton/cc360/internal/config"
)

const (
	tHead = `{"type":"system","sessionId":"s1","cwd":"/home/user/p","timestamp":"2026-01-01T00:00:00Z"}`
	tUser = `{"parentUuid":null,"type":"user","sessionId":"s1","cwd":"/home/user/p","gitBranch":"main","message":{"role":"user","content":"real prompt"},"timestamp":"2026-01-01T00:00:01Z"}`
	tBot  = `{"parentUuid":"a","type":"assistant","sessionId":"s1","cwd":"/home/user/p","gitBranch":"main","message":{"role":"assistant","content":"ok"},"timestamp":"2026-01-01T00:00:02Z"}`
)

func parse(t *testing.T, lines ...string) *Session {
	t.Helper()
	s := parseOrphanJSONL(writeJSONL(t, lines), []string{"/home/user"})
	if s == nil {
		t.Fatal("parseOrphanJSONL returned nil")
	}
	return s
}

// API request snapshots (2.1.300+) carry a message with a role, but are not
// conversation turns and must not become the first prompt.
func TestParseSkipsAPIRequestRecords(t *testing.T) {
	blob := `{"type":"api-request-blob","sessionId":"s1","hash":"h","message":{"role":"user","content":"system prompt text"}}`
	req := `{"type":"api-request","sessionId":"s1","timestamp":"2026-01-01T09:00:00Z"}`

	s := parse(t, tHead, blob, tUser, blob, tBot, req)
	if s.MessageCount != 2 {
		t.Errorf("MessageCount = %d, want 2", s.MessageCount)
	}
	if s.FirstPrompt != "real prompt" {
		t.Errorf("FirstPrompt = %q, want real prompt", s.FirstPrompt)
	}
}

// Injected context such as the local-command caveat is marked isMeta.
func TestParseSkipsMetaRecords(t *testing.T) {
	meta := `{"parentUuid":null,"type":"user","isMeta":true,"cwd":"/home/user/p","message":{"role":"user","content":"<local-command-caveat>Caveat</local-command-caveat>"},"timestamp":"2026-01-01T00:00:00Z"}`
	compact := `{"parentUuid":"b","type":"user","isCompactSummary":true,"message":{"role":"user","content":"summary"},"timestamp":"2026-01-01T00:00:03Z"}`

	s := parse(t, meta, tUser, tBot, compact)
	if s.FirstPrompt != "real prompt" {
		t.Errorf("FirstPrompt = %q, want real prompt", s.FirstPrompt)
	}
	if s.MessageCount != 2 {
		t.Errorf("MessageCount = %d, want 2", s.MessageCount)
	}
}

// A record over the old 1 MiB scanner limit used to end the scan silently.
func TestParseReadsPastHugeRecord(t *testing.T) {
	huge := fmt.Sprintf(`{"parentUuid":"a","type":"user","message":{"role":"user","content":"%s"},"timestamp":"2026-01-01T00:00:05Z"}`,
		strings.Repeat("x", 3<<20))
	late := `{"parentUuid":"c","type":"assistant","message":{"role":"assistant","content":"done"},"timestamp":"2026-01-02T00:00:00Z"}`
	title := `{"type":"ai-title","aiTitle":"After the big one","sessionId":"s1"}`

	s := parse(t, tHead, tUser, huge, late, title)
	if s.MessageCount != 3 {
		t.Errorf("MessageCount = %d, want 3", s.MessageCount)
	}
	if got := s.Modified.Format("2006-01-02"); got != "2026-01-02" {
		t.Errorf("Modified = %s, want 2026-01-02", got)
	}
	if s.Title != "After the big one" {
		t.Errorf("Title = %q, want After the big one", s.Title)
	}
}

func TestLineReaderSkipsOversizedLine(t *testing.T) {
	in := "short\n" + strings.Repeat("y", 100) + "\nnext\nlast-no-newline"
	lr := lineReader{r: bufio.NewReaderSize(strings.NewReader(in), 16), max: 50}

	var got []string
	for {
		line, err := lr.next()
		if err != nil {
			break
		}
		got = append(got, string(line))
	}
	want := []string{"short", "", "next", "last-no-newline"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("lines = %q, want %q", got, want)
	}
}

func TestParseLatestTitleAndBranch(t *testing.T) {
	first := `{"type":"ai-title","aiTitle":"First","sessionId":"s1"}`
	second := `{"type":"ai-title","aiTitle":"Second","sessionId":"s1"}`
	onFeature := strings.Replace(tBot, `"gitBranch":"main"`, `"gitBranch":"feature"`, 1)

	s := parse(t, tHead, first, tUser, second, onFeature)
	if s.Title != "Second" {
		t.Errorf("Title = %q, want Second", s.Title)
	}
	if s.GitBranch != "feature" {
		t.Errorf("GitBranch = %q, want feature", s.GitBranch)
	}
}

func TestParseCustomTitle(t *testing.T) {
	custom := `{"type":"custom-title","customTitle":"my-name","sessionId":"s1"}`
	renamed := `{"type":"custom-title","customTitle":"renamed","sessionId":"s1"}`
	agent := `{"type":"agent-name","agentName":"bg-agent","sessionId":"s1"}`

	t.Run("last record wins", func(t *testing.T) {
		if got := parse(t, tHead, custom, tUser, renamed).CustomTitle; got != "renamed" {
			t.Errorf("CustomTitle = %q, want renamed", got)
		}
	})
	t.Run("agent name as fallback", func(t *testing.T) {
		if got := parse(t, tHead, agent, tUser).CustomTitle; got != "bg-agent" {
			t.Errorf("CustomTitle = %q, want bg-agent", got)
		}
	})
	t.Run("sidecar wins", func(t *testing.T) {
		path := writeJSONL(t, []string{tHead, custom, tUser})
		dir := filepath.Join(filepath.Dir(path), "s1")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "custom-title.json"), []byte(`{"customTitle":"from-sidecar"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		s := parseOrphanJSONL(path, []string{"/home/user"})
		if s == nil || s.CustomTitle != "from-sidecar" {
			t.Errorf("CustomTitle = %+v, want from-sidecar", s)
		}
	})
}

// A legacy index entry must not shadow a transcript that has moved on since.
func TestScanPrefersTranscriptOverIndex(t *testing.T) {
	cwd := "/tmp/scanroot/idx"
	home := writeHome(t, "indexed", cwd)
	dir := filepath.Join(home, "projects", encodeDirName(cwd))
	transcript := strings.Join([]string{
		`{"parentUuid":null,"type":"user","sessionId":"indexed","cwd":"/tmp/scanroot/idx","message":{"role":"user","content":"hi"},"timestamp":"2026-06-01T00:00:00Z"}`,
		`{"parentUuid":"a","type":"assistant","sessionId":"indexed","message":{"role":"assistant","content":"yo"},"timestamp":"2026-06-02T00:00:00Z"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "indexed.jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatal(err)
	}
	index := `{"entries":[
		{"sessionId":"indexed","projectPath":"/tmp/scanroot/idx","summary":"Old summary","messageCount":1,"modified":"2026-01-01T00:00:00Z"},
		{"sessionId":"index-only","projectPath":"/tmp/scanroot/idx","summary":"Gone","messageCount":4,"modified":"2026-01-01T00:00:00Z"}]}`
	if err := os.WriteFile(filepath.Join(dir, "sessions-index.json"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}

	sessions, err := Scan(config.Config{ScanPaths: []string{"/tmp/scanroot"}, ClaudeHomes: []string{home}, ScanOrphans: true}, nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	got := map[string]Session{}
	for _, s := range sessions {
		got[s.SessionID] = s
	}
	s := got["indexed"]
	if s.MessageCount != 2 || s.Modified.Format("2006-01-02") != "2026-06-02" {
		t.Errorf("indexed = %d msgs, modified %s; want the transcript's 2 and 2026-06-02", s.MessageCount, s.Modified)
	}
	if s.ExistingSummary != "Old summary" {
		t.Errorf("ExistingSummary = %q, want the index summary kept", s.ExistingSummary)
	}
	if got["index-only"].MessageCount != 4 {
		t.Errorf("index-only session missing or wrong: %+v", got["index-only"])
	}
}
