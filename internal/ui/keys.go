package ui

import "github.com/charmbracelet/bubbles/key"

// KeyMap holds every binding the app shell recognizes. Bindings marked
// "stub" below are wired to a harmless placeholder, not real behavior yet.
type KeyMap struct {
	Up      key.Binding
	Down    key.Binding
	Left    key.Binding
	Right   key.Binding
	PgUp    key.Binding
	PgDown  key.Binding
	Quit    key.Binding
	Archive key.Binding

	Open key.Binding // enters edit mode for the selected task's title/body

	// Stubs: shown in the footer, need real backend pieces that don't exist yet.
	NewTask    key.Binding
	Cancel     key.Binding
	Workspaces key.Binding
	Help       key.Binding
}

var keys = KeyMap{
	Up:      key.NewBinding(key.WithKeys("up", "k")),
	Down:    key.NewBinding(key.WithKeys("down", "j")),
	Left:    key.NewBinding(key.WithKeys("left", "h")),
	Right:   key.NewBinding(key.WithKeys("right", "l")),
	PgUp:    key.NewBinding(key.WithKeys("pgup", "ctrl+u")),
	PgDown:  key.NewBinding(key.WithKeys("pgdown", "ctrl+d")),
	Quit:    key.NewBinding(key.WithKeys("q")),
	Archive: key.NewBinding(key.WithKeys("a")),

	Open: key.NewBinding(key.WithKeys("enter")),

	NewTask:    key.NewBinding(key.WithKeys("n")),
	Cancel:     key.NewBinding(key.WithKeys("c")),
	Workspaces: key.NewBinding(key.WithKeys("w")),
	Help:       key.NewBinding(key.WithKeys("?")),
}

const footerHelp = " [n]ew  [enter]edit  [c]ancel  [w]orkspaces  [a]rchive  [?]help  [q]uit"
