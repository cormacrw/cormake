package ui

import (
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// newBranchSentinel is the branchPicker select's special option value
// meaning "not an existing branch — type one in", always listed first
// (huh starts the cursor on the first option) so a fresh picker defaults
// to it rather than requiring the user to scroll past every existing
// branch to reach it.
const newBranchSentinel = "\x00new-branch"

// branchPicker is a small two-step huh flow: pick an existing branch (or
// the sentinel) from a Select, then — only if the sentinel was picked —
// type a name in a follow-up Input pre-filled with a suggested default.
// Shared by the new-task wizard's target/source steps (see newtask.go) and
// the standalone change-target/change-source-branch shortcuts (see
// keys.ChangeTargetBranch/ChangeSourceBranch in app.go).
type branchPicker struct {
	selectForm *huh.Form
	inputForm  *huh.Form

	mode  string // bound to the select field: an existing branch name, or newBranchSentinel
	value string // bound to the input field once shown, pre-filled with suggested

	done bool

	// searching tracks whether the select step's built-in "/" filter (huh's
	// Select supports typing to search its options out of the box) is
	// currently engaged, so callers embedding this picker in a larger esc-
	// cancels-everything flow (see Update, and updateBranchPickerModal /
	// updateNewTaskWizard) can let a lone esc back out of the search box
	// first instead of immediately closing the whole picker.
	searching bool
}

// newBranchPicker builds a picker offering branches plus, if allowNew, a
// sentinel option labeled sentinelLabel that opens a follow-up text input
// pre-filled with suggested. defaultBranch preselects that branch if it's
// present in branches; otherwise the picker defaults to the sentinel (or,
// if allowNew is false, to branches[0]).
func newBranchPicker(title string, branches []string, allowNew bool, sentinelLabel, suggested, defaultBranch string) *branchPicker {
	p := &branchPicker{value: suggested}

	branches = branchesWithDefaultFirst(branches, defaultBranch)

	options := make([]huh.Option[string], 0, len(branches)+1)
	if allowNew {
		options = append(options, huh.NewOption(sentinelLabel, newBranchSentinel))
		p.mode = newBranchSentinel
	}
	for _, b := range branches {
		options = append(options, huh.NewOption(b, b))
		if !allowNew && p.mode == "" {
			p.mode = b
		}
		if b == defaultBranch {
			p.mode = b
		}
	}

	sel := huh.NewSelect[string]().Title(title).Options(options...).Value(&p.mode)
	p.selectForm = huh.NewForm(huh.NewGroup(sel)).WithWidth(56).WithShowHelp(true)
	return p
}

func (p *branchPicker) Init() tea.Cmd {
	return p.selectForm.Init()
}

// branchesWithDefaultFirst returns branches with defaultBranch moved to the
// front, if present, so the option a picker preselects is also the first
// one listed rather than wherever git's most-recently-committed ordering
// happened to place it.
func branchesWithDefaultFirst(branches []string, defaultBranch string) []string {
	if defaultBranch == "" {
		return branches
	}
	idx := -1
	for i, b := range branches {
		if b == defaultBranch {
			idx = i
			break
		}
	}
	if idx <= 0 {
		return branches
	}
	out := make([]string, 0, len(branches))
	out = append(out, defaultBranch)
	out = append(out, branches[:idx]...)
	out = append(out, branches[idx+1:]...)
	return out
}

func requireBranchName(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("a branch name is required")
	}
	return nil
}

// Update forwards msg to whichever internal form is active. The select
// form completing either finishes the picker outright (an existing branch
// was chosen) or, if the "new branch" sentinel was picked, opens the
// follow-up name input — whose own completion finishes the picker.
func (p *branchPicker) Update(msg tea.Msg) tea.Cmd {
	if p.inputForm != nil {
		f, cmd := p.inputForm.Update(msg)
		p.inputForm = f.(*huh.Form)
		if p.inputForm.State == huh.StateCompleted {
			p.done = true
		}
		return cmd
	}

	// Mirror huh's own filtering state machine (see its Select.setFiltering)
	// just enough to know whether esc should be treated as "back out of the
	// search box" rather than "cancel the picker": "/" opens the filter,
	// and huh unconditionally closes it again on enter/tab (it submits the
	// highlighted, possibly-filtered option) or esc (huh's own filter-step
	// esc binding, forwarded through once the caller sees searching go
	// false here) — so a single esc always resolves back to "not
	// searching," even though huh itself needs a second esc to fully clear
	// the typed filter text.
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "/":
			p.searching = true
		case "enter", "tab", "esc":
			p.searching = false
		}
	}

	f, cmd := p.selectForm.Update(msg)
	p.selectForm = f.(*huh.Form)
	if p.selectForm.State == huh.StateCompleted {
		if p.mode != newBranchSentinel {
			p.value = p.mode
			p.done = true
			return cmd
		}
		input := huh.NewInput().Title("Branch name").Value(&p.value).CharLimit(200).Validate(requireBranchName)
		p.inputForm = huh.NewForm(huh.NewGroup(input)).WithWidth(56).WithShowHelp(true)
		return tea.Batch(cmd, p.inputForm.Init())
	}
	return cmd
}

func (p *branchPicker) View() string {
	if p.inputForm != nil {
		return p.inputForm.View()
	}
	return p.selectForm.View()
}

// Done reports whether the picker has a final value ready (see Result).
func (p *branchPicker) Done() bool { return p.done }

// Searching reports whether the select step's "/" filter is currently
// engaged (see the searching field), so an enclosing esc-cancels-everything
// flow can let esc dismiss the search first instead of closing the picker.
func (p *branchPicker) Searching() bool { return p.searching }

// Result returns the picker's resolved branch name, valid once Done.
func (p *branchPicker) Result() string { return strings.TrimSpace(p.value) }
