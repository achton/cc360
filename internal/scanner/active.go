package scanner

import (
	"context"
	"encoding/json"
	"os/exec"
	"sync"
	"time"

	"github.com/achton/cc360/internal/config"
)

// ActiveState describes what a running session is doing. The zero value means
// it is not running. Higher values win when a session is reported twice.
type ActiveState int

const (
	StateNone ActiveState = iota
	StateIdle
	StateWaiting // blocked on the user: a permission prompt, a question
	StateBusy
)

// Agent is one running session as `claude agents --json` reports it.
type Agent struct {
	State      ActiveState
	Background bool   // a `claude --bg` job, which `claude attach` can open
	JobID      string // the background job id that `claude attach` takes
	Name       string
	WaitingFor string
	PID        int
}

// agentEntry is the subset of `claude agents --json` we use. Status is the live
// process (idle, waiting, busy). State is the background job's lifecycle,
// present without a status when the job has no process right now.
type agentEntry struct {
	ID         string          `json:"id"`
	PID        int             `json:"pid"`
	Kind       string          `json:"kind"`
	SessionID  string          `json:"sessionId"`
	Name       string          `json:"name"`
	Status     string          `json:"status"`
	State      string          `json:"state"`
	WaitingFor json.RawMessage `json:"waitingFor"`
}

const activeLookupTimeout = 5 * time.Second

// ActiveSessions maps the session IDs of running sessions to what they are
// doing. It asks Claude Code directly via `claude agents --json`, which reports
// the real session ID; inferring it from process arguments could only ever be a
// guess for sessions that were not started with --resume.
//
// The command only reports agents of its own config dir, so every home is
// queried. The queries run concurrently because callers poll them from the UI
// loop. A failed home yields no results, so the indicators go quiet rather
// than the table breaking.
func ActiveSessions(homes []string) map[string]Agent {
	perHome := make([]map[string]Agent, len(homes))
	var wg sync.WaitGroup
	for i, home := range homes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			perHome[i] = agentsForHome(home)
		}()
	}
	wg.Wait()

	agents := make(map[string]Agent)
	for _, m := range perHome {
		for id, a := range m {
			upsertAgent(agents, id, a)
		}
	}
	if len(agents) == 0 {
		return nil
	}
	return agents
}

func agentsForHome(home string) map[string]Agent {
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

// upsertAgent records a session, keeping the more urgent state. One session
// can be reported twice, both within a home and across two of them. On a tie
// a background job wins, because only that can be attached to.
func upsertAgent(agents map[string]Agent, id string, a Agent) {
	if prev, ok := agents[id]; ok {
		if prev.State > a.State || (prev.State == a.State && (prev.Background || !a.Background)) {
			return
		}
	}
	agents[id] = a
}

// stateOf prefers the live process status. A background job without one falls
// back to its lifecycle state, and a finished job is not running at all.
func stateOf(e agentEntry) ActiveState {
	switch e.Status {
	case "busy":
		return StateBusy
	case "waiting":
		return StateWaiting
	case "idle":
		return StateIdle
	}
	switch e.State {
	case "working":
		return StateBusy
	case "blocked":
		return StateWaiting
	case "done", "failed", "stopped":
		return StateNone
	}
	return StateIdle
}

func parseAgents(data []byte) map[string]Agent {
	var entries []agentEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil
	}

	agents := make(map[string]Agent, len(entries))
	for _, e := range entries {
		if e.SessionID == "" {
			continue
		}
		state := stateOf(e)
		if state == StateNone {
			continue
		}
		a := Agent{
			State:      state,
			Background: e.Kind == "background",
			Name:       e.Name,
			PID:        e.PID,
		}
		if a.Background {
			a.JobID = e.ID
		}
		if state == StateWaiting {
			// A string in current releases; anything else is ignored.
			_ = json.Unmarshal(e.WaitingFor, &a.WaitingFor)
		}
		upsertAgent(agents, e.SessionID, a)
	}
	return agents
}
