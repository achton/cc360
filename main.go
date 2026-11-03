package main

import (
	"fmt"
	"os"
	"runtime/debug"

	tea "charm.land/bubbletea/v2"
	"github.com/achton/cc360/internal/config"
	"github.com/achton/cc360/internal/db"
	"github.com/achton/cc360/internal/demo"
	"github.com/achton/cc360/internal/scanner"
	"github.com/achton/cc360/internal/tui"
)

// version is set via -ldflags by GoReleaser. For `go install` builds it stays
// "dev", so buildVersion falls back to the module version from build info.
var version = "dev"

// buildVersion returns the release version, preferring the ldflags value and
// falling back to the module version embedded by `go install`.
func buildVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return version
}

func main() {
	// Handle --version / -v flag
	if len(os.Args) == 2 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("cc360 %s\n", buildVersion())
		return
	}

	// Handle --demo flag
	if len(os.Args) == 2 && os.Args[1] == "--demo" {
		runDemo()
		return
	}

	// Recover terminal state on panic (raw mode, cursor, mouse mode)
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "cc360 panic: %v\n", r)
			os.Exit(1)
		}
	}()

	cfg, shouldExit, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if shouldExit {
		return
	}

	database, err := db.Open("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	sessions, err := tui.LoadSessions(database, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading sessions: %v\n", err)
		os.Exit(1)
	}

	// The first poll runs from Init, so startup does not wait on it.
	m := tui.New(database, cfg, sessions, nil).WithVersion(buildVersion())
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runDemo() {
	cfg := config.Config{
		ScanPaths: []string{"~/Code"},
		SortBy:    "modified",
		// Off, so polling never replaces the fixed states below.
		ShowActive: false,
	}
	sessions := demo.Sessions()
	// Show every indicator state.
	activeStates := map[string]scanner.Agent{
		tui.AgentKey(sessions[0]): {State: scanner.StateBusy},
		tui.AgentKey(sessions[1]): {State: scanner.StateWaiting, WaitingFor: "permission", Background: true, JobID: "a1b2c3d4"},
		tui.AgentKey(sessions[2]): {State: scanner.StateIdle},
	}
	m := tui.New(nil, cfg, sessions, activeStates)
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
