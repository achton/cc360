package scanner

import (
	"encoding/json"
	"testing"
)

func TestTopLevelFields(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want map[string]string // key -> raw value
		ok   bool
	}{
		{"flat", `{"a":"x","b":true,"c":12}`, map[string]string{"a": `"x"`, "b": "true", "c": "12"}, true},
		{"spaces", ` { "a" : "x" , "b" : null } `, map[string]string{"a": `"x"`, "b": "null"}, true},
		{"nested skipped whole", `{"m":{"k":["}",{"x":"]"}]},"t":"after"}`, map[string]string{"m": `{"k":["}",{"x":"]"}]}`, "t": `"after"`}, true},
		{"escaped quotes", `{"a":"say \"hi\" \\","b":"y"}`, map[string]string{"a": `"say \"hi\" \\"`, "b": `"y"`}, true},
		{"empty", `{}`, map[string]string{}, true},
		{"not an object", `["a"]`, nil, false},
		{"truncated", `{"a":"unterminated`, nil, false},
		{"missing colon", `{"a" "x"}`, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := map[string]string{}
			ok := topLevelFields([]byte(tt.in), func(k, v []byte) { got[string(k)] = string(v) })
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if !tt.ok {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("%s = %s, want %s", k, got[k], v)
				}
			}
		})
	}
}

// decodeEntry must agree with encoding/json on the fields it reads.
func TestDecodeEntryMatchesEncodingJSON(t *testing.T) {
	line := `{"parentUuid":"p","isSidechain":true,"type":"user","message":{"role":"user","content":"a \"quoted\" \\ é prompt"},"isMeta":false,"sessionId":"s1","cwd":"/home/u/café","gitBranch":"main","timestamp":"2026-01-01T00:00:00Z","toolUseResult":{"stdout":"{not json"}}`

	var got jsonlEntry
	if !decodeEntry([]byte(line), &got) {
		t.Fatal("decodeEntry failed")
	}
	var want struct {
		Type, SessionID, Cwd, GitBranch, Timestamp string
		IsSidechain                                bool
		Message                                    json.RawMessage
	}
	if err := json.Unmarshal([]byte(line), &want); err != nil {
		t.Fatal(err)
	}
	if got.Type != want.Type || got.SessionID != want.SessionID || got.Cwd != want.Cwd ||
		got.GitBranch != want.GitBranch || got.Timestamp != want.Timestamp || got.IsSidechain != want.IsSidechain {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if string(got.Message) != string(want.Message) {
		t.Errorf("Message = %s, want %s", got.Message, want.Message)
	}
	if text, ok := extractUserMessage(got.Message); !ok || text != `a "quoted" \ é prompt` {
		t.Errorf("prompt = %q, %v", text, ok)
	}
}
