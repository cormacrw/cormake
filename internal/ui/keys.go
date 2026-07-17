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

	// Archive parks a TODO or READY_FOR_REVIEW task out of the active view
	// (or restores one already archived) — not a finished outcome, just set
	// aside. See Complete below for actually finishing a task's work.
	Archive key.Binding

	// Delete permanently removes the selected task, from any stage, behind
	// a confirmation modal (same gate as Plan/Execute, but no Status
	// eligibility check — delete works no matter what stage the task is in).
	Delete key.Binding

	// Complete finalizes a READY_FOR_REVIEW task that's been executed:
	// prompts for a feature branch name, commits the worktree's outstanding
	// changes onto it, removes the worktree, and moves the task to COMPLETE.
	Complete key.Binding

	Open       key.Binding // enters edit mode for the selected task's title/body
	Workspaces key.Binding // opens the workspace-picker modal

	// Plan spawns a read-only claude run (TODO -> PLANNING -> PLANNED).
	// Execute spawns a real claude run with edits enabled, inside a fresh
	// git worktree (TODO/PLANNED -> IN_PROGRESS -> READY_FOR_REVIEW). Both
	// are staged behind a confirmation modal.
	Plan    key.Binding
	Execute key.Binding

	// Review opens the selected task's artifact in revdiff for annotation:
	// its actual code changes (diffed against the worktree's base ref) once
	// it's been executed, otherwise its plan if it has one. Annotations get
	// sent back to claude as revision feedback automatically once revdiff
	// exits, resuming whichever session (plan or execute) produced them.
	Review key.Binding

	// Switch which tab the detail pane's content area shows. 1/2/3/4 are
	// fixed slots (Description/Plan/Summary/Log) regardless of whether
	// Plan/Summary is applicable to the current task, so they don't shift
	// task to task; [ and ] cycle through whichever tabs are visible.
	TabDescription key.Binding
	TabPlan        key.Binding
	TabSummary     key.Binding
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

	Archive:  key.NewBinding(key.WithKeys("a")),
	Delete:   key.NewBinding(key.WithKeys("d")),
	Complete: key.NewBinding(key.WithKeys("m")),

	Open:       key.NewBinding(key.WithKeys("enter")),
	Workspaces: key.NewBinding(key.WithKeys("w")),

	Plan:    key.NewBinding(key.WithKeys("p")),
	Execute: key.NewBinding(key.WithKeys("e")),
	Review:  key.NewBinding(key.WithKeys("r")),

	TabDescription: key.NewBinding(key.WithKeys("1")),
	TabPlan:        key.NewBinding(key.WithKeys("2")),
	TabSummary:     key.NewBinding(key.WithKeys("3")),
	TabLog:         key.NewBinding(key.WithKeys("4")),
	TabPrev:        key.NewBinding(key.WithKeys("[")),
	TabNext:        key.NewBinding(key.WithKeys("]")),

	NewTask: key.NewBinding(key.WithKeys("n")),
	Cancel:  key.NewBinding(key.WithKeys("c")),
	Help:    key.NewBinding(key.WithKeys("?")),
}

const footerHelp = " [n]ew [enter]edit [p]lan [e]xecute [r]eview [m]ark complete [a]rchive [d]elete [w]orkspaces tabs:1-4/[/] [?]help [q]uit"
