package scanner

import (
	"os"
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
	scanPaths := []string{"/home/user/Code", "/home/user/Code-private"}

	tests := []struct {
		path string
		want string
	}{
		{"/home/user/Code/myproject", "Code/myproject"},
		{"/home/user/Code-private/homelab", "Code-private/homelab"},
		{"/home/user/Code/nested/deep", "Code/nested/deep"},
	}
	for _, tt := range tests {
		got := deriveProjectName(tt.path, scanPaths)
		if got != tt.want {
			t.Errorf("deriveProjectName(%q) = %q, want %q", tt.path, got, tt.want)
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
