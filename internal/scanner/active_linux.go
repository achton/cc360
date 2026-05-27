//go:build linux

package scanner

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// claudeProcesses discovers running claude processes by reading /proc.
func claudeProcesses() []claudeProc {
	matches, err := filepath.Glob("/proc/[0-9]*/cmdline")
	if err != nil {
		return nil
	}

	myPID := os.Getpid()
	var procs []claudeProc

	for _, cmdlineFile := range matches {
		dir := filepath.Dir(cmdlineFile)
		pid, err := strconv.Atoi(filepath.Base(dir))
		if err != nil || pid == myPID {
			continue
		}

		data, err := os.ReadFile(cmdlineFile)
		if err != nil {
			continue
		}

		// /proc cmdline is NUL-separated.
		args := strings.Split(string(data), "\x00")
		if len(args) == 0 || filepath.Base(args[0]) != "claude" {
			continue
		}

		p := claudeProc{pid: pid}
		for i, arg := range args {
			if arg == "--resume" && i+1 < len(args) {
				p.resumeID = args[i+1]
				break
			}
		}
		if cwd, err := os.Readlink(filepath.Join(dir, "cwd")); err == nil {
			p.cwd = cwd
		}

		procs = append(procs, p)
	}

	return procs
}
