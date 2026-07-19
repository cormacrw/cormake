package ui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// branchPickerKind distinguishes which of a task's two branches a
// standalone branch-picker modal (see keys.ChangeTargetBranch/
// ChangeSourceBranch) is changing.
type branchPickerKind int

const (
	branchPickerKindTarget branchPickerKind = iota
	branchPickerKindSource
)

// openBranchPickerModal opens a standalone picker to change the selected
// task's target or source branch after creation. Target is only eligible
// while no worktree has been created yet (t.WorktreePath == "") — that's
// what pins the branch actually in use, so changing it after the fact
// wouldn't do anything. Source has no such restriction: it's just
// bookkeeping about the eventual merge destination, changeable any time.
func (m *Model) openBranchPickerModal(kind branchPickerKind) tea.Cmd {
	t, ok := m.tasklist.Selected()
	if !ok {
		return nil
	}
	if kind == branchPickerKindTarget && t.WorktreePath != "" {
		return nil
	}

	repoPath, _ := m.repoPath(t.RepoID)
	branches := listLocalBranches(repoPath)

	var picker *branchPicker
	switch kind {
	case branchPickerKindTarget:
		suggested := t.TargetBranch
		if suggested == "" {
			suggested = suggestTargetBranchName(t.DisplayID)
		}
		picker = newBranchPicker("Target branch", branches, true,
			"+ create new branch ("+suggested+")", suggested, t.TargetBranch)
	case branchPickerKindSource:
		def := t.SourceBranch
		if def == "" {
			def = m.workspaces[m.activeWS].EffectiveDefaultTargetBranch()
		}
		picker = newBranchPicker("Source branch", branches, true,
			"other (type a branch name)", def, def)
	}

	m.branchPicker = picker
	m.branchPickerOpen = true
	m.branchPickerKind = kind
	m.branchPickerTaskID = t.ID
	return picker.Init()
}

// updateBranchPickerModal handles input while a standalone branch picker is
// open: esc cancels without changing anything (see updateNewTaskWizard for
// why esc, not huh's own ctrl+c, is what cancels here), everything else is
// forwarded to the picker itself. While the picker's own "/" search box is
// engaged, esc is forwarded instead of canceling outright — see
// branchPicker.Searching — so backing out of a search doesn't also close
// the whole modal.
func (m Model) updateBranchPickerModal(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok && km.String() == "esc" && !m.branchPicker.Searching() {
		m.branchPickerOpen = false
		m.branchPicker = nil
		return m, nil
	}

	cmd := m.branchPicker.Update(msg)
	if !m.branchPicker.Done() {
		return m, cmd
	}

	result := m.branchPicker.Result()
	kind := m.branchPickerKind
	taskID := m.branchPickerTaskID
	m.branchPickerOpen = false
	m.branchPicker = nil

	for i := range m.tasks {
		if m.tasks[i].ID != taskID {
			continue
		}
		if kind == branchPickerKindTarget {
			m.tasks[i].TargetBranch = result
		} else {
			m.tasks[i].SourceBranch = result
		}
		m.persistTask(m.tasks[i])
		break
	}
	// persistTask only updates m.tasks — the tasklist widget holds its own
	// copy of each task (see tasklist.Model.SetTasks), so refreshTaskList
	// (not the narrower syncDetail) is what's needed to make the change
	// show up immediately, same as every other in-place task edit (e.g.
	// handleCompleteFinished, archiveSelected).
	m.refreshTaskList()
	return m, cmd
}

// renderBranchPickerModal draws the standalone branch picker, centered over
// the full screen like every other modal.
func (m Model) renderBranchPickerModal() string {
	box := focusedPaneBorderStyle().Padding(1, 3).Render(m.branchPicker.View())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}
