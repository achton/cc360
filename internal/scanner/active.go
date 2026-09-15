package scanner

import (
	"context"
	"encoding/json"
	"os/exec"
	"sync"
	"time"

	"github.com/achton/cc360/internal/config"
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
// The command only reports agents of its own config dir, so every home is
// queried. The queries run concurrently because callers poll them from the UI
// loop. A failed home yields no results, so the indicators go quiet rather
// than the table breaking.
func ActiveSessions(homes []string) map[string]ActiveState {
	perHome := make([]map[string]ActiveState, len(homes))
	var wg sync.WaitGroup
	for i, home := range homes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			perHome[i] = agentsForHome(home)
		}()
	}
	wg.Wait()

	states := make(map[string]ActiveState)
	for _, m := range perHome {
		for id, state := range m {
			upsertState(states, id, state)
		}
	}
	if len(states) == 0 {
		return nil
	}
	return states
}

func agentsForHome(home string) map[string]ActiveState {
	ctx, cancel := context.WithTimeout(context.Background(), activeLookupTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude", "agents", "--json")
	cmd.Env = config.ClaudeEnv(home)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	return parseAgents(out)
}

// upsertState records a session's state, letting busy win. One session can be
// reported twice, both within a home and across two of them.
func upsertState(states map[string]ActiveState, id string, state ActiveState) {
	if states[id] != StateBusy {
		states[id] = state
	}
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
		upsertState(states, e.SessionID, state)
	}
	return states
}
