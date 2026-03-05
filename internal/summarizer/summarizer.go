package summarizer

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/achton/cc360/internal/db"
)

type messageContent struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type jsonlEntry struct {
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message"`
}

// extractUserMessages reads a JSONL file and returns up to maxMsgs user messages.
func extractUserMessages(path string, maxMsgs int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var messages []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		if len(messages) >= maxMsgs {
			break
		}
		var entry jsonlEntry
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}
		if len(entry.Message) == 0 {
			continue
		}
		var msg messageContent
		if json.Unmarshal(entry.Message, &msg) != nil {
			continue
		}
		if msg.Role != "user" {
			continue
		}
		text := extractText(msg.Content)
		if text == "" || text == "[Request interrupted by user]" {
			continue
		}
		// Cap each message
		if len([]rune(text)) > 500 {
			text = string([]rune(text)[:500])
		}
		messages = append(messages, text)
	}
	return messages
}

func extractText(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case []any:
		var parts []string
		for _, item := range c {
			if m, ok := item.(map[string]any); ok {
				if m["type"] == "text" {
					if t, ok := m["text"].(string); ok {
						parts = append(parts, t)
					}
				}
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

const prompt = `You are summarizing a Claude Code session. Given the user messages below, produce:
1. A TITLE (max 6 words, no quotes, describing what the session is about)
2. A SUMMARY (max 80 chars, one sentence describing what was done or discussed)

Format your response EXACTLY as:
TITLE: <title here>
SUMMARY: <summary here>

User messages:
`

// Summarize generates a title and summary for a session by calling claude --print.
func Summarize(session db.Session, model string) (title string, summary string, err error) {
	if session.JSONLPath == "" {
		return "", "", fmt.Errorf("no JSONL path for session %s", session.SessionID)
	}

	messages := extractUserMessages(session.JSONLPath, 5)
	if len(messages) == 0 {
		return "", "", fmt.Errorf("no user messages found in %s", session.JSONLPath)
	}

	// Build the full prompt
	var sb strings.Builder
	sb.WriteString(prompt)
	for i, msg := range messages {
		fmt.Fprintf(&sb, "\n--- Message %d ---\n%s\n", i+1, msg)
	}
	// Add context
	if session.ProjectName != "" {
		fmt.Fprintf(&sb, "\nProject: %s\n", session.ProjectName)
	}
	if session.GitBranch != "" {
		fmt.Fprintf(&sb, "Branch: %s\n", session.GitBranch)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude", "--print", "--no-session-persistence", "--model", model)
	cmd.Stdin = strings.NewReader(sb.String())

	output, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("claude --print failed: %w", err)
	}

	return parseResponse(string(output))
}

func parseResponse(output string) (title, summary string, err error) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "TITLE:") {
			title = strings.TrimSpace(strings.TrimPrefix(line, "TITLE:"))
		}
		if strings.HasPrefix(line, "SUMMARY:") {
			summary = strings.TrimSpace(strings.TrimPrefix(line, "SUMMARY:"))
		}
	}
	if title == "" && summary == "" {
		return "", "", fmt.Errorf("could not parse TITLE/SUMMARY from output: %s", output[:min(len(output), 200)])
	}
	return title, summary, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
