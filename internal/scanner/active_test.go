package scanner

import "testing"

func TestParseAgents(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want map[string]ActiveState
	}{
		{
			name: "busy and idle",
			in: `[{"pid":1,"cwd":"/a","kind":"interactive","sessionId":"aaa","status":"busy"},
			      {"pid":2,"cwd":"/b","kind":"interactive","sessionId":"bbb","status":"idle"}]`,
			want: map[string]ActiveState{"aaa": StateBusy, "bbb": StateIdle},
		},
		{
			name: "no sessions running",
			in:   `[]`,
			want: map[string]ActiveState{},
		},
		{
			name: "unknown status counts as idle, not absent",
			in:   `[{"sessionId":"aaa","status":"something-new"}]`,
			want: map[string]ActiveState{"aaa": StateIdle},
		},
		{
			name: "missing status counts as idle",
			in:   `[{"sessionId":"aaa"}]`,
			want: map[string]ActiveState{"aaa": StateIdle},
		},
		{
			name: "entries without a session id are skipped",
			in:   `[{"pid":1,"status":"busy"},{"sessionId":"aaa","status":"busy"}]`,
			want: map[string]ActiveState{"aaa": StateBusy},
		},
		{
			name: "busy wins when a session reports twice",
			in:   `[{"sessionId":"aaa","status":"idle"},{"sessionId":"aaa","status":"busy"}]`,
			want: map[string]ActiveState{"aaa": StateBusy},
		},
		{
			name: "busy wins regardless of order",
			in:   `[{"sessionId":"aaa","status":"busy"},{"sessionId":"aaa","status":"idle"}]`,
			want: map[string]ActiveState{"aaa": StateBusy},
		},
		{name: "malformed json yields nothing", in: `not json`, want: nil},
		{name: "unexpected shape yields nothing", in: `{"sessions":[]}`, want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAgents([]byte(tt.in))
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for id, state := range tt.want {
				if got[id] != state {
					t.Errorf("state[%q] = %v, want %v", id, got[id], state)
				}
			}
		})
	}
}

// A session that is not running must read as StateNone via the zero value.
func TestParseAgentsAbsentIsStateNone(t *testing.T) {
	states := parseAgents([]byte(`[{"sessionId":"aaa","status":"busy"}]`))
	if states["not-running"] != StateNone {
		t.Errorf("absent session = %v, want StateNone", states["not-running"])
	}
}
