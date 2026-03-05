package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Up        key.Binding
	Down      key.Binding
	PageUp    key.Binding
	PageDown  key.Binding
	Top       key.Binding
	Bottom    key.Binding
	Detail    key.Binding
	Summarize key.Binding
	Resume    key.Binding
	Shell     key.Binding
	Copy      key.Binding
	Filter    key.Binding
	Picker    key.Binding
	Escape    key.Binding
	Quit      key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup"),
			key.WithHelp("pgup", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown"),
			key.WithHelp("pgdn", "page down"),
		),
		Top: key.NewBinding(
			key.WithKeys("home", "g"),
			key.WithHelp("home/g", "top"),
		),
		Bottom: key.NewBinding(
			key.WithKeys("end", "G"),
			key.WithHelp("end/G", "bottom"),
		),
		Detail: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "detail"),
		),
		Summarize: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "summarize"),
		),
		Resume: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "resume"),
		),
		Shell: key.NewBinding(
			key.WithKeys("o"),
			key.WithHelp("o", "shell"),
		),
		Copy: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "copy cmd"),
		),
		Filter: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "filter"),
		),
		Picker: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "projects"),
		),
		Escape: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "clear"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
	}
}
