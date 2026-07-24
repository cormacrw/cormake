package ui

import "github.com/charmbracelet/bubbles/key"

// KeyMap holds every binding the app shell recognizes. Bindings marked
// "stub" below are wired to a harmless placeholder, not real behavior yet.
type KeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Left   key.Binding // moves to the previous task tab (TODO/Archived/Completed)
	Right  key.Binding // moves to the next task tab (TODO/Archived/Completed)
	PgUp   key.Binding
	PgDown key.Binding
	Quit   key.Binding

	// Scroll covers the bare arrow keys, forwarded straight to the detail
	// pane's content viewport. Arrow keys are reserved for scrolling only —
	// list navigation and the tab switch use their vim-key equivalents
	// (k/j/h/l) instead, so a key never does double duty.
	Scroll key.Binding

	// Archive parks a TODO or READY_FOR_REVIEW task out of the active view
	// and into the Archived tab (or restores one already archived) — not a
	// finished outcome, just set aside. See Complete below for actually
	// finishing a task's work.
	Archive key.Binding

	// Delete permanently removes the selected task, from any stage, behind
	// a confirmation modal (same gate as Plan/Execute, but no Status
	// eligibility check — delete works no matter what stage the task is in).
	Delete key.Binding

	// Complete finalizes a READY_FOR_REVIEW or IN_REVIEW task that's been
	// executed: behind a confirmation, commits the worktree's outstanding
	// changes onto its target branch, removes the worktree, and moves the
	// task to COMPLETE.
	Complete key.Binding

	// OpenPR stages opening a pull request for a READY_FOR_REVIEW task
	// behind a confirmation modal — a claude run that pushes the branch and
	// creates the PR with gh (see startOpenPR), moving the task through
	// OPENING_PR to IN_REVIEW once that's confirmed to have landed.
	OpenPR key.Binding

	// OpenPRBrowser opens the selected task's PR (if it has one) in the
	// system's default browser.
	OpenPRBrowser key.Binding

	Open       key.Binding // enters edit mode for the selected task's title/body
	Workspaces key.Binding // opens the workspace-picker modal

	// ChangeTargetBranch/ChangeSourceBranch open a standalone branch-picker
	// modal (see branchmodal.go) to change the selected task's target or
	// source branch after it was already set in the new-task wizard.
	// ChangeTargetBranch only takes effect before a worktree exists yet
	// (see openBranchPickerModal); ChangeSourceBranch is unrestricted,
	// since it's just bookkeeping about the eventual merge destination.
	ChangeTargetBranch key.Binding
	ChangeSourceBranch key.Binding

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

	// Input opens a free-form textarea prompt for a PLANNED, READY_FOR_REVIEW,
	// or IN_REVIEW task, sent to claude as a follow-up message that resumes
	// whichever session (plan or execute) produced that state — a
	// lighter-weight alternative to Review for when there's nothing to
	// annotate on the artifact itself, just something to say (e.g. pasting
	// in a GitHub PR review comment to address).
	Input key.Binding

	// Switch which tab the detail pane's content area shows. 1-6 are fixed
	// slots (Description/Plan/Summary/PR/PR Comments/Log) regardless of
	// whether a given tab is applicable to the current task, so they don't
	// shift task to task; [ and ] cycle through whichever tabs are visible.
	TabDescription key.Binding
	TabPlan        key.Binding
	TabSummary     key.Binding
	TabPRDesc      key.Binding
	TabPRComments  key.Binding
	TabLog         key.Binding
	TabPrev        key.Binding
	TabNext        key.Binding

	// Stubs: shown in the footer, need real backend pieces that don't exist yet.
	NewTask key.Binding
	Cancel  key.Binding
	Help    key.Binding
}

var keys = KeyMap{
	Up:     key.NewBinding(key.WithKeys("k")),
	Down:   key.NewBinding(key.WithKeys("j")),
	Left:   key.NewBinding(key.WithKeys("h")),
	Right:  key.NewBinding(key.WithKeys("l")),
	PgUp:   key.NewBinding(key.WithKeys("pgup", "ctrl+u")),
	PgDown: key.NewBinding(key.WithKeys("pgdown", "ctrl+d")),
	Quit:   key.NewBinding(key.WithKeys("q")),

	Scroll: key.NewBinding(key.WithKeys("up", "down", "left", "right")),

	Archive:  key.NewBinding(key.WithKeys("a")),
	Delete:   key.NewBinding(key.WithKeys("d")),
	Complete: key.NewBinding(key.WithKeys("m")),

	OpenPR:        key.NewBinding(key.WithKeys("P")),
	OpenPRBrowser: key.NewBinding(key.WithKeys("b")),

	Open:       key.NewBinding(key.WithKeys("enter")),
	Workspaces: key.NewBinding(key.WithKeys("w")),

	ChangeTargetBranch: key.NewBinding(key.WithKeys("t")),
	ChangeSourceBranch: key.NewBinding(key.WithKeys("s")),

	Plan:    key.NewBinding(key.WithKeys("p")),
	Execute: key.NewBinding(key.WithKeys("e")),
	Review:  key.NewBinding(key.WithKeys("r")),
	Input:   key.NewBinding(key.WithKeys("i")),

	TabDescription: key.NewBinding(key.WithKeys("1")),
	TabPlan:        key.NewBinding(key.WithKeys("2")),
	TabSummary:     key.NewBinding(key.WithKeys("3")),
	TabLog:         key.NewBinding(key.WithKeys("4")),
	TabPRDesc:      key.NewBinding(key.WithKeys("5")),
	TabPRComments:  key.NewBinding(key.WithKeys("6")),
	TabPrev:        key.NewBinding(key.WithKeys("[")),
	TabNext:        key.NewBinding(key.WithKeys("]")),

	NewTask: key.NewBinding(key.WithKeys("n")),
	Cancel:  key.NewBinding(key.WithKeys("c")),
	Help:    key.NewBinding(key.WithKeys("?")),
}

const footerHelp = " [n]ew [/]filter [enter]edit [p]lan [e]xecute [r]eview [i]nput [P]R [b]rowser [m]ark complete [t]arget-branch [s]ource-branch [a]rchive [d]elete [w]orkspaces tabs:1-6/[/] arrows:scroll [?]help [q]uit"
