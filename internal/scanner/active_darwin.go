//go:build darwin

package scanner

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// claudeProcesses discovers running claude processes on macOS, which has no
// /proc. Command lines come from `ps`; working directories come from `lsof`
// (libproc would require cgo, which would break the cross-compiled release).
func claudeProcesses() []claudeProc {
	// pid + full command line, one process per line, untruncated (-ww).
	out, err := exec.Command("ps", "-axww", "-o", "pid=", "-o", "args=").Output()
	if err != nil {
		return nil
	}

	myPID := os.Getpid()
	var procs []claudeProc

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Split off the leading PID; the remainder is the command line.
		pidStr, cmdline, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		pid, err := strconv.Atoi(pidStr)
		if err != nil || pid == myPID {
			continue
		}

		args := strings.Fields(cmdline)
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
		// CWD is only needed to match fresh (non-resumed) sessions.
		if p.resumeID == "" {
			p.cwd = processCwd(pid)
		}

		procs = append(procs, p)
	}

	return procs
}

// processCwd returns the working directory of a PID via lsof, or "" if it can't
// be determined. Output is field mode (-F n): a line like "n/path/to/cwd".
func processCwd(pid int) string {
	cmd := exec.Command("lsof", "-a", "-p", strconv.Itoa(pid), "-d", "cwd", "-F", "n")
	out, err := cmd.Output() // stderr discarded; lsof warns about inaccessible fds
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "n") {
			return line[1:]
		}
	}
	return ""
}
