package summarizer

import (
	"strings"
	"testing"
)

func TestParseResponse(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantTitle   string
		wantSummary string
		wantErr     bool
	}{
		{
			name:        "valid response",
			input:       "TITLE: Fix navbar styling\nSUMMARY: Fixed CSS flexbox layout issues in the main navigation bar",
			wantTitle:   "Fix navbar styling",
			wantSummary: "Fixed CSS flexbox layout issues in the main navigation bar",
		},
		{
			name:        "extra whitespace",
			input:       "  TITLE:   Build Docker setup  \n  SUMMARY:   Created multi-stage Dockerfile  \n",
			wantTitle:   "Build Docker setup",
			wantSummary: "Created multi-stage Dockerfile",
		},
		{
			name:        "with preamble",
			input:       "Here is the summary:\n\nTITLE: Refactor auth module\nSUMMARY: Extracted authentication logic into separate middleware",
			wantTitle:   "Refactor auth module",
			wantSummary: "Extracted authentication logic into separate middleware",
		},
		{
			name:    "no title or summary",
			input:   "This is just some random text without the expected format",
			wantErr: true,
		},
		{
			name:        "title only",
			input:       "TITLE: Something\nNo summary line here",
			wantTitle:   "Something",
			wantSummary: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title, summary, err := parseResponse(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if title != tt.wantTitle {
				t.Errorf("title = %q, want %q", title, tt.wantTitle)
			}
			if summary != tt.wantSummary {
				t.Errorf("summary = %q, want %q", summary, tt.wantSummary)
			}
		})
	}
}

func TestValidateModelName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"simple name", "sonnet", false},
		{"name with hyphens and dots", "claude-3.5-sonnet", false},
		{"name with underscores", "my_model_v2", false},
		{"empty string", "", true},
		{"with semicolons", "model;rm -rf /", true},
		{"with spaces", "model name", true},
		{"with backticks", "model`whoami`", true},
		{"with dollar sign", "model$(cmd)", true},
		{"with slashes", "path/to/model", true},
		{"very long string", strings.Repeat("a", 51), true},
		{"exactly 50 chars", strings.Repeat("a", 50), false},
		{"with newline", "model\nname", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateModelName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateModelName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestExtractText(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  string
	}{
		{"string content", "hello world", "hello world"},
		{"nil content", nil, ""},
		{
			"array with text blocks",
			[]any{
				map[string]any{"type": "text", "text": "first"},
				map[string]any{"type": "text", "text": "second"},
			},
			"first second",
		},
		{
			"array with mixed types",
			[]any{
				map[string]any{"type": "text", "text": "hello"},
				map[string]any{"type": "image", "url": "http://example.com"},
			},
			"hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractText(tt.input)
			if got != tt.want {
				t.Errorf("extractText() = %q, want %q", got, tt.want)
			}
		})
	}
}
