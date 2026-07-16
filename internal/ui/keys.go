package ui

import "github.com/charmbracelet/bubbles/key"

// KeyMap holds every binding the app shell recognizes. Bindings marked
// "stub" below are wired to a harmless placeholder, not real behavior yet.
type KeyMap struct {
	Up    key.Binding
	Down  key.Binding
	Tab   key.Binding
	Left  key.Binding
	Right key.Binding
	Quit  key.Binding

	// Stubs: shown in the footer, need real backend pieces that don't exist yet.
	NewTask    key.Binding
	Open       key.Binding
	Cancel     key.Binding
	Workspaces key.Binding
	Help       key.Binding
}

var keys = KeyMap{
	Up:    key.NewBinding(key.WithKeys("up", "k")),
	Down:  key.NewBinding(key.WithKeys("down", "j")),
	Tab:   key.NewBinding(key.WithKeys("tab")),
	Left:  key.NewBinding(key.WithKeys("left", "h")),
	Right: key.NewBinding(key.WithKeys("right", "l")),
	Quit:  key.NewBinding(key.WithKeys("q", "ctrl+c")),

	NewTask:    key.NewBinding(key.WithKeys("n")),
	Open:       key.NewBinding(key.WithKeys("enter")),
	Cancel:     key.NewBinding(key.WithKeys("c")),
	Workspaces: key.NewBinding(key.WithKeys("w")),
	Help:       key.NewBinding(key.WithKeys("?")),
}

const footerHelp = " [n]ew  [enter]open  [c]ancel  [w]orkspaces  [tab]switch pane  [?]help  [q]uit"
