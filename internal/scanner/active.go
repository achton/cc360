package scanner

import (
	"context"
	"encoding/json"
	"os/exec"
	"time"
)

// ActiveState describes what a running claude process is doing. The zero value
// means the session is not running.
type ActiveState int

const (
	StateNone ActiveState = iota
	StateIdle
	StateBusy
)

// agentEntry is the subset of `claude agents --json` we use.
type agentEntry struct {
	SessionID string `json:"sessionId"`
	Status    string `json:"status"`
}

const activeLookupTimeout = 5 * time.Second

// ActiveSessions maps the session IDs of running claude processes to their
// state. It asks Claude Code directly via `claude agents --json`, which reports
// the real session ID; inferring it from process arguments could only ever be a
// guess for sessions that were not started with --resume.
//
// Any failure yields no results, so the indicators go quiet rather than the
// table breaking.
func ActiveSessions() map[string]ActiveState {
	ctx, cancel := context.WithTimeout(context.Background(), activeLookupTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "claude", "agents", "--json").Output()
	if err != nil {
		return nil
	}
	return parseAgents(out)
}

func parseAgents(data []byte) map[string]ActiveState {
	var entries []agentEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil
	}

	states := make(map[string]ActiveState, len(entries))
	for _, e := range entries {
		if e.SessionID == "" {
			continue
		}
		state := StateIdle
		if e.Status == "busy" {
			state = StateBusy
		}
		// Several processes can report the same session; busy wins.
		if states[e.SessionID] != StateBusy {
			states[e.SessionID] = state
		}
	}
	return states
}
