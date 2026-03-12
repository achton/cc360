package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

var filterStyle = lipgloss.NewStyle().
	Foreground(colorYellow).
	Padding(0, 1)

type filterInput struct {
	input  textinput.Model
	active bool
}

func newFilterInput() filterInput {
	ti := textinput.New()
	ti.Prompt = "/ "
	ti.CharLimit = 100
	return filterInput{input: ti}
}

func (f *filterInput) open() {
	f.active = true
	f.input.Focus()
}

func (f *filterInput) close() {
	f.active = false
	f.input.SetValue("")
	f.input.Blur()
}

// visible returns true if the filter line should be shown (active or has text).
func (f *filterInput) visible() bool {
	return f.active || f.input.Value() != ""
}

// focused returns true if the text input is capturing keystrokes.
func (f *filterInput) focused() bool {
	return f.input.Focused()
}

func (f *filterInput) value() string {
	return f.input.Value()
}

func (f *filterInput) view(width int) string {
	return filterStyle.Width(width).Render(f.input.View())
}
