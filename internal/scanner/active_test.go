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
			name: "waiting on the user",
			in:   `[{"pid":1,"kind":"interactive","sessionId":"aaa","status":"waiting","waitingFor":"permission"}]`,
			want: map[string]ActiveState{"aaa": StateWaiting},
		},
		{
			name: "background job without a process uses its state",
			in: `[{"id":"j1","kind":"background","sessionId":"aaa","state":"blocked"},
			      {"id":"j2","kind":"background","sessionId":"bbb","state":"working"}]`,
			want: map[string]ActiveState{"aaa": StateWaiting, "bbb": StateBusy},
		},
		{
			name: "process status wins over job state",
			in:   `[{"id":"j1","pid":3,"kind":"background","sessionId":"aaa","status":"idle","state":"working"}]`,
			want: map[string]ActiveState{"aaa": StateIdle},
		},
		{
			name: "finished background jobs are not running",
			in: `[{"kind":"background","sessionId":"aaa","state":"done"},
			      {"kind":"background","sessionId":"bbb","state":"failed"},
			      {"kind":"background","sessionId":"ccc","state":"stopped"}]`,
			want: map[string]ActiveState{},
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
			in:   `[{"sessionId":"aaa","status":"busy"},{"sessionId":"aaa","status":"waiting"}]`,
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
				if got[id].State != state {
					t.Errorf("state[%q] = %v, want %v", id, got[id].State, state)
				}
			}
		})
	}
}

func TestParseAgentsDetails(t *testing.T) {
	got := parseAgents([]byte(`[
		{"id":"job1","pid":7,"kind":"background","sessionId":"bg","name":"nightly","status":"waiting","waitingFor":"permission"},
		{"id":"ignored","pid":8,"kind":"interactive","sessionId":"fg","status":"waiting","waitingFor":{"agents":[]}}]`))

	bg := got["bg"]
	if !bg.Background || bg.JobID != "job1" || bg.Name != "nightly" || bg.WaitingFor != "permission" || bg.PID != 7 {
		t.Errorf("background agent = %+v", bg)
	}
	fg := got["fg"]
	if fg.Background || fg.JobID != "" || fg.WaitingFor != "" {
		t.Errorf("interactive agent = %+v, want no job id and the non-string waitingFor ignored", fg)
	}
}

// A session that is not running must read as StateNone via the zero value.
func TestParseAgentsAbsentIsStateNone(t *testing.T) {
	agents := parseAgents([]byte(`[{"sessionId":"aaa","status":"busy"}]`))
	if agents["not-running"].State != StateNone {
		t.Errorf("absent session = %v, want StateNone", agents["not-running"].State)
	}
}
