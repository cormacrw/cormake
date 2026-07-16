package ui

import "github.com/charmbracelet/bubbles/key"

// KeyMap holds every binding the app shell recognizes. Bindings marked
// "stub" below are wired to a harmless placeholder, not real behavior yet.
type KeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Left   key.Binding // switches to the Open tab
	Right  key.Binding // switches to the Archived tab
	PgUp   key.Binding
	PgDown key.Binding
	Quit   key.Binding

	// Archive sets the selected task's Status to Complete, from any stage —
	// you can always archive a task, whatever it's doing.
	Archive key.Binding

	Open       key.Binding // enters edit mode for the selected task's title/body
	Workspaces key.Binding // opens the workspace-picker modal

	// Plan/Execute only act on a TODO task: Plan moves it to PLANNING,
	// Execute skips planning and moves it straight to IN_PROGRESS. Neither
	// spawns a real agent yet — this just advances Status for now.
	Plan    key.Binding
	Execute key.Binding

	// Stubs: shown in the footer, need real backend pieces that don't exist yet.
	NewTask key.Binding
	Cancel  key.Binding
	Help    key.Binding
}

var keys = KeyMap{
	Up:     key.NewBinding(key.WithKeys("up", "k")),
	Down:   key.NewBinding(key.WithKeys("down", "j")),
	Left:   key.NewBinding(key.WithKeys("left", "h")),
	Right:  key.NewBinding(key.WithKeys("right", "l")),
	PgUp:   key.NewBinding(key.WithKeys("pgup", "ctrl+u")),
	PgDown: key.NewBinding(key.WithKeys("pgdown", "ctrl+d")),
	Quit:   key.NewBinding(key.WithKeys("q")),

	Archive: key.NewBinding(key.WithKeys("a")),

	Open:       key.NewBinding(key.WithKeys("enter")),
	Workspaces: key.NewBinding(key.WithKeys("w")),

	Plan:    key.NewBinding(key.WithKeys("p")),
	Execute: key.NewBinding(key.WithKeys("e")),

	NewTask: key.NewBinding(key.WithKeys("n")),
	Cancel:  key.NewBinding(key.WithKeys("c")),
	Help:    key.NewBinding(key.WithKeys("?")),
}

const footerHelp = " [n]ew  [enter]edit  [p]lan  [e]xecute  [a]rchive  [w]orkspaces  [c]ancel  [?]help  [q]uit"
