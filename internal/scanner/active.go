package scanner

import "sort"

// claudeProc describes a running `claude` process discovered by a
// platform-specific backend (see active_linux.go / active_darwin.go).
type claudeProc struct {
	pid      int
	resumeID string // session ID from --resume, empty for a fresh session
	cwd      string // working directory, empty if unavailable
}

// ActiveSessionIDs returns session IDs currently in use by a running claude
// process. Process discovery is platform-specific (claudeProcesses); the
// matching logic is shared across platforms.
func ActiveSessionIDs(sessions []Session) map[string]bool {
	return matchActive(sessions, claudeProcesses())
}

// matchActive maps running claude processes to known session IDs. Resumed
// sessions (--resume <id>) match exactly; fresh sessions are matched by CWD to
// the most recently modified session in that directory, with newer processes
// (higher PID) taking precedence.
func matchActive(sessions []Session, procs []claudeProc) map[string]bool {
	active := make(map[string]bool)

	// Known session IDs for fast lookup.
	knownIDs := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		knownIDs[s.SessionID] = true
	}

	// Map CWD -> most recently modified session ID.
	type cwdSession struct {
		id       string
		modified int64
	}
	cwdToLatest := make(map[string]cwdSession)
	for _, s := range sessions {
		if s.ProjectPath == "" {
			continue
		}
		mod := s.Modified.Unix()
		if existing, ok := cwdToLatest[s.ProjectPath]; !ok || mod > existing.modified {
			cwdToLatest[s.ProjectPath] = cwdSession{id: s.SessionID, modified: mod}
		}
	}

	// Resumed sessions first (exact match).
	for _, p := range procs {
		if p.resumeID != "" && knownIDs[p.resumeID] {
			active[p.resumeID] = true
		}
	}

	// Fresh sessions: match CWD to the most recent session in that directory.
	// Sort by PID descending so newer processes take precedence.
	sort.Slice(procs, func(i, j int) bool {
		return procs[i].pid > procs[j].pid
	})
	for _, p := range procs {
		if p.resumeID != "" || p.cwd == "" {
			continue
		}
		if latest, ok := cwdToLatest[p.cwd]; ok {
			active[latest.id] = true
		}
	}

	return active
}
