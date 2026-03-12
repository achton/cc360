package scanner

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ActiveSessionIDs returns session IDs currently in use by a running claude
// process. It reads /proc/*/cmdline for --resume args and matches fresh
// sessions (no --resume) by CWD to the most recently modified session in
// that directory.
func ActiveSessionIDs(sessions []Session) map[string]bool {
	active := make(map[string]bool)

	procs, err := filepath.Glob("/proc/[0-9]*/cmdline")
	if err != nil {
		return active
	}

	// Known session IDs for fast lookup
	knownIDs := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		knownIDs[s.SessionID] = true
	}

	// Map CWD -> most recently modified session ID
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

	myPID := os.Getpid()

	// Collect claude processes with their info
	type claudeProc struct {
		pid      string
		resumeID string // empty if fresh session
		cwd      string
	}
	var claudeProcs []claudeProc

	for _, cmdlineFile := range procs {
		dir := filepath.Dir(cmdlineFile)
		pidStr := filepath.Base(dir)

		// Skip our own process
		if pidStr == itoa(myPID) {
			continue
		}

		data, err := os.ReadFile(cmdlineFile)
		if err != nil {
			continue
		}

		args := strings.Split(string(data), "\x00")
		if len(args) == 0 {
			continue
		}

		// Check if this is a claude process (the main binary, not subprocesses)
		base := filepath.Base(args[0])
		if base != "claude" {
			continue
		}

		proc := claudeProc{pid: pidStr}

		// Look for --resume <session-id>
		for i, arg := range args {
			if arg == "--resume" && i+1 < len(args) {
				proc.resumeID = args[i+1]
				break
			}
		}

		// Read CWD
		if cwd, err := os.Readlink(filepath.Join(dir, "cwd")); err == nil {
			proc.cwd = cwd
		}

		claudeProcs = append(claudeProcs, proc)
	}

	// Process resumed sessions first (exact match)
	for _, p := range claudeProcs {
		if p.resumeID != "" && knownIDs[p.resumeID] {
			active[p.resumeID] = true
		}
	}

	// Process fresh sessions: match CWD to most recent session
	// Sort by PID descending so newer processes take precedence
	sort.Slice(claudeProcs, func(i, j int) bool {
		return claudeProcs[i].pid > claudeProcs[j].pid
	})
	for _, p := range claudeProcs {
		if p.resumeID != "" || p.cwd == "" {
			continue
		}
		if latest, ok := cwdToLatest[p.cwd]; ok {
			active[latest.id] = true
		}
	}

	return active
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
