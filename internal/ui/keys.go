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

	// Delete permanently removes the selected task, from any stage, behind
	// a confirmation modal (same gate as Plan/Execute, but no Status
	// eligibility check — delete works no matter what stage the task is in).
	Delete key.Binding

	Open       key.Binding // enters edit mode for the selected task's title/body
	Workspaces key.Binding // opens the workspace-picker modal

	// Plan/Execute only act on a TODO task: Plan moves it to PLANNING,
	// Execute skips planning and moves it straight to IN_PROGRESS. Neither
	// spawns a real agent yet — this just advances Status for now, and both
	// are staged behind a confirmation modal.
	Plan    key.Binding
	Execute key.Binding

	// Review opens the selected task's plan in revdiff for annotation, if
	// it has one; annotations get sent back to claude as revision feedback
	// automatically once revdiff exits.
	Review key.Binding

	// Switch which tab the detail pane's content area shows. 1/2/3 are fixed
	// slots regardless of whether Plan is applicable, so they don't shift
	// task to task; [ and ] cycle through whichever tabs are visible.
	TabDescription key.Binding
	TabPlan        key.Binding
	TabLog         key.Binding
	TabPrev        key.Binding
	TabNext        key.Binding

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
	Delete:  key.NewBinding(key.WithKeys("d")),

	Open:       key.NewBinding(key.WithKeys("enter")),
	Workspaces: key.NewBinding(key.WithKeys("w")),

	Plan:    key.NewBinding(key.WithKeys("p")),
	Execute: key.NewBinding(key.WithKeys("e")),
	Review:  key.NewBinding(key.WithKeys("r")),

	TabDescription: key.NewBinding(key.WithKeys("1")),
	TabPlan:        key.NewBinding(key.WithKeys("2")),
	TabLog:         key.NewBinding(key.WithKeys("3")),
	TabPrev:        key.NewBinding(key.WithKeys("[")),
	TabNext:        key.NewBinding(key.WithKeys("]")),

	NewTask: key.NewBinding(key.WithKeys("n")),
	Cancel:  key.NewBinding(key.WithKeys("c")),
	Help:    key.NewBinding(key.WithKeys("?")),
}

const footerHelp = " [n]ew [enter]edit [p]lan [e]xecute [r]eview [a]rchive [d]elete [w]orkspaces tabs:1-3/[/] [?]help [q]uit"
