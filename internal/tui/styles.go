package tui

import "github.com/charmbracelet/lipgloss"

// Catppuccin Mocha palette — slightly boosted for vibrancy
var (
	colorBase     = lipgloss.Color("#1e1e2e")
	colorMantle   = lipgloss.Color("#181825")
	colorSurface0 = lipgloss.Color("#3b3b52")
	colorSurface1 = lipgloss.Color("#525270")
	colorOverlay0 = lipgloss.Color("#8888a4")
	colorSubtext0 = lipgloss.Color("#b0b8d1")
	colorSubtext1 = lipgloss.Color("#d0d6e8")
	colorText     = lipgloss.Color("#e0e4f0")
	colorBlue     = lipgloss.Color("#96bfff")
	colorMauve    = lipgloss.Color("#d4b0ff")
	colorGreen    = lipgloss.Color("#b5f0b0")
	colorYellow   = lipgloss.Color("#ffe5a0")
	colorRed      = lipgloss.Color("#ff9bb5")
	colorPeach    = lipgloss.Color("#ffc49a")
	colorDim      = lipgloss.Color("#706f87")
)

var (
	// Header bar: solid background
	headerAppStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorBlue).
			Background(colorSurface0)

	headerTagStyle = lipgloss.NewStyle().
				Foreground(colorDim).
				Background(colorSurface0)

	headerStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Background(colorSurface0)

	// Status bar
	statusStyle = lipgloss.NewStyle().
			Foreground(colorSubtext0).
			Padding(0, 1)

	// Help bar
	helpStyle = lipgloss.NewStyle().
			Foreground(colorOverlay0).
			Padding(0, 1)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(colorBlue).
			Bold(true)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(colorOverlay0)

	helpSepStyle = lipgloss.NewStyle().
			Foreground(colorSurface1)

	// Table column header
	colHdrStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorSubtext1)

	// Table separator
	sepStyle = lipgloss.NewStyle().
			Foreground(colorSurface1)

	// Table selected row: left accent bar + subtle background
	selectedStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Background(colorSurface0)

	selectedBarStyle = lipgloss.NewStyle().
				Foreground(colorBlue).
				Background(colorSurface0)

	// Active session indicator
	activeStyle = lipgloss.NewStyle().
			Foreground(colorGreen)

	// Muted text (inactive indicator alignment)
	mutedStyle = lipgloss.NewStyle().
			Foreground(colorOverlay0)

	// Table title (bold project name prefix)
	titleBoldStyle = lipgloss.NewStyle().
			Foreground(colorMauve).
			Bold(true)

	// Dim style for branch column
	dimStyle = lipgloss.NewStyle().
			Foreground(colorDim)
)
