package tui

import "testing"

func TestSanitize(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello world", "hello world"},
		{"line\none\ttwo", "line one two"},
		{"<b>bold</b> text", "bold text"},
		{"<teammate-message>content</teammate-message>", "content"},
		{"no tags here", "no tags here"},
		{"", ""},
	}
	for _, tt := range tests {
		got := sanitize(tt.input)
		if got != tt.want {
			t.Errorf("sanitize(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hell…"},
		{"hi", 2, "hi"},
		{"hello", 1, "…"},
		{"", 5, ""},
	}
	for _, tt := range tests {
		got := truncate(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}

func TestStripTags(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"<b>bold</b>", "bold"},
		{"no tags", "no tags"},
		{"<a href='x'>link</a> text", "link text"},
		{"<<nested>>", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := stripTags(tt.input)
		if got != tt.want {
			t.Errorf("stripTags(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBuildColumns(t *testing.T) {
	// Narrow: no branch, no msgs
	cols := buildColumns(80)
	if len(cols) != 3 {
		t.Errorf("at width 80: got %d columns, want 3", len(cols))
	}

	// Medium: branch visible
	cols = buildColumns(95)
	if len(cols) != 4 {
		t.Errorf("at width 95: got %d columns, want 4", len(cols))
	}
	if cols[3].Title != "Branch" {
		t.Errorf("4th column = %q, want Branch", cols[3].Title)
	}

	// Wide: branch + msgs
	cols = buildColumns(120)
	if len(cols) != 5 {
		t.Errorf("at width 120: got %d columns, want 5", len(cols))
	}
	if cols[4].Title != "Msgs" {
		t.Errorf("5th column = %q, want Msgs", cols[4].Title)
	}
}

func TestClamp(t *testing.T) {
	tests := []struct {
		v, lo, hi, want int
	}{
		{5, 0, 10, 5},
		{-1, 0, 10, 0},
		{15, 0, 10, 10},
		{0, 0, 0, 0},
	}
	for _, tt := range tests {
		got := clamp(tt.v, tt.lo, tt.hi)
		if got != tt.want {
			t.Errorf("clamp(%d, %d, %d) = %d, want %d", tt.v, tt.lo, tt.hi, got, tt.want)
		}
	}
}

func TestTableNavigation(t *testing.T) {
	table := sessionTable{
		rows:   make([][]string, 20),
		height: 10,
	}

	table.SetCursor(5)
	if table.Cursor() != 5 {
		t.Errorf("cursor = %d, want 5", table.Cursor())
	}

	table.GotoBottom()
	if table.Cursor() != 19 {
		t.Errorf("after GotoBottom: cursor = %d, want 19", table.Cursor())
	}

	table.GotoTop()
	if table.Cursor() != 0 {
		t.Errorf("after GotoTop: cursor = %d, want 0", table.Cursor())
	}

	// Can't go below 0
	table.MoveUp(5)
	if table.Cursor() != 0 {
		t.Errorf("after MoveUp past top: cursor = %d, want 0", table.Cursor())
	}

	// Can't go past end
	table.GotoBottom()
	table.MoveDown(5)
	if table.Cursor() != 19 {
		t.Errorf("after MoveDown past bottom: cursor = %d, want 19", table.Cursor())
	}
}
