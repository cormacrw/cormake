package ui

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"cormake/internal/domain"
)

// wizardStep is which page of the new-task wizard is currently active. The
// steps always run in this order; there's no back-navigation (see
// updateNewTaskWizard) — correcting a choice after the fact goes through
// the standalone keys.ChangeTargetBranch/ChangeSourceBranch shortcuts
// instead.
type wizardStep int

const (
	wizardStepTitle wizardStep = iota
	wizardStepRepo
	wizardStepTarget
	wizardStepSource
	wizardStepConfirm
)

// newTaskWizard drives the multi-step New Task flow: task name, repo,
// target branch (the branch this task's work is committed to), source
// branch (the branch that work will eventually be merged into), then a
// recap confirmation. See keys.NewTask and Model.openNewTaskWizard.
type newTaskWizard struct {
	step wizardStep

	titleForm *huh.Form
	title     string

	repoForm *huh.Form
	repoID   string
	repos    []domain.Repo // the active workspace's repos, snapshotted at open time
	repoPath string        // resolved once repoID is picked, used to list branches

	targetPicker *branchPicker
	sourcePicker *branchPicker

	confirmForm *huh.Form
	confirmed   bool
}

// activePickerSearching reports whether the wizard's current step is one of
// the two branch-picker steps and that picker's "/" search box is currently
// engaged — see updateNewTaskWizard's esc handling.
func (w *newTaskWizard) activePickerSearching() bool {
	switch w.step {
	case wizardStepTarget:
		return w.targetPicker.Searching()
	case wizardStepSource:
		return w.sourcePicker.Searching()
	default:
		return false
	}
}

func requireTaskTitle(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("a task name is required")
	}
	return nil
}

// openNewTaskWizard starts the New Task wizard on its title step.
func (m *Model) openNewTaskWizard() tea.Cmd {
	w := &newTaskWizard{}
	w.titleForm = huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Task name").Value(&w.title).CharLimit(200).Validate(requireTaskTitle),
	)).WithWidth(56).WithShowHelp(true)
	m.wizard = w
	return w.titleForm.Init()
}

// createTaskQuick creates a task straight from the wizard's title step
// using every default — the active workspace's first repo (if any), a
// fresh auto-named target branch, and the default source branch — for
// keys bound to "just get on with it" (see updateNewTaskWizard's ctrl+j
// handling).
func (m *Model) createTaskQuick(title string) {
	if len(m.workspaces) == 0 {
		m.createTask(title, "", "", "")
		return
	}
	ws := m.workspaces[m.activeWS]
	repoID := ""
	if len(ws.Repos) > 0 {
		repoID = ws.Repos[0].ID
	}
	m.createTask(title, repoID, suggestTargetBranchName(ws.PeekNextDisplayID()), ws.EffectiveDefaultTargetBranch())
}

// updateNewTaskWizard forwards a key to whichever step is currently active,
// advancing to the next step once that step reports itself complete.
//
// esc cancels the whole wizard outright rather than being forwarded into
// huh: huh's own abort binding is ctrl+c (see its KeyMap), which this app
// intercepts globally for tea.Quit before any modal ever sees it (see
// Update), so relying on huh's own StateAborted would never fire. The one
// exception is while the active step's branch picker has its "/" search box
// engaged (see branchPicker.Searching) — there esc is forwarded into the
// picker instead, so it backs out of the search rather than closing the
// whole wizard.
func (m Model) updateNewTaskWizard(msg tea.Msg) (tea.Model, tea.Cmd) {
	w := m.wizard
	if km, ok := msg.(tea.KeyMsg); ok {
		if km.String() == "esc" && !w.activePickerSearching() {
			m.wizard = nil
			return m, nil
		}

		// Quick task: on the title step, ctrl+enter jumps straight to
		// creating the task with every default, skipping
		// repo/branch/confirm entirely. Bound to ctrl+j too since a
		// terminal's Enter key is carriage-return with no distinct
		// ctrl-modified encoding — many terminals report Ctrl+Enter as
		// ctrl+j instead (see keys.go).
		if w.step == wizardStepTitle && (km.String() == "ctrl+enter" || km.String() == "ctrl+j") {
			title := strings.TrimSpace(w.title)
			if title == "" {
				return m, nil
			}
			m.wizard = nil
			m.createTaskQuick(title)
			return m, nil
		}
	}

	switch w.step {
	case wizardStepTitle:
		f, cmd := w.titleForm.Update(msg)
		w.titleForm = f.(*huh.Form)
		if w.titleForm.State == huh.StateCompleted {
			return m.advanceWizardFromTitle()
		}
		return m, cmd

	case wizardStepRepo:
		f, cmd := w.repoForm.Update(msg)
		w.repoForm = f.(*huh.Form)
		if w.repoForm.State == huh.StateCompleted {
			return m.advanceWizardFromRepo()
		}
		return m, cmd

	case wizardStepTarget:
		cmd := w.targetPicker.Update(msg)
		if w.targetPicker.Done() {
			return m.advanceWizardFromTarget()
		}
		return m, cmd

	case wizardStepSource:
		cmd := w.sourcePicker.Update(msg)
		if w.sourcePicker.Done() {
			return m.advanceWizardFromSource()
		}
		return m, cmd

	case wizardStepConfirm:
		f, cmd := w.confirmForm.Update(msg)
		w.confirmForm = f.(*huh.Form)
		if w.confirmForm.State == huh.StateCompleted {
			m.wizard = nil
			if w.confirmed {
				m.createTask(strings.TrimSpace(w.title), w.repoID, w.targetPicker.Result(), w.sourcePicker.Result())
			}
			return m, nil
		}
		return m, cmd
	}
	return m, nil
}

// advanceWizardFromTitle moves from the title step to the repo step — or,
// if the active workspace has no repos configured, there's nothing to
// branch off of, so it falls back to creating a bare repo-less task
// outright (the pre-wizard behavior).
func (m Model) advanceWizardFromTitle() (tea.Model, tea.Cmd) {
	w := m.wizard
	title := strings.TrimSpace(w.title)
	w.repos = m.workspaces[m.activeWS].Repos
	if len(w.repos) == 0 {
		m.wizard = nil
		m.createTask(title, "", "", "")
		return m, nil
	}

	options := make([]huh.Option[string], len(w.repos))
	for i, r := range w.repos {
		options[i] = huh.NewOption(r.Name, r.ID)
	}
	w.repoID = w.repos[0].ID
	w.step = wizardStepRepo
	w.repoForm = huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title("Repo").Options(options...).Value(&w.repoID),
	)).WithWidth(56).WithShowHelp(true)
	return m, w.repoForm.Init()
}

// advanceWizardFromRepo moves from the repo step to the target-branch
// step, resolving the picked repo's filesystem path once so the target and
// source steps can both list its branches without re-resolving it.
func (m Model) advanceWizardFromRepo() (tea.Model, tea.Cmd) {
	w := m.wizard
	w.repoPath, _ = m.repoPath(w.repoID)
	branches := listLocalBranches(w.repoPath)
	ws := m.workspaces[m.activeWS]
	suggested := suggestTargetBranchName(ws.PeekNextDisplayID())

	w.step = wizardStepTarget
	w.targetPicker = newBranchPicker(
		"Target branch — where this task's work will be committed",
		branches, true, fmt.Sprintf("+ create new branch (%q)", suggested), suggested, "",
	)
	return m, w.targetPicker.Init()
}

// advanceWizardFromTarget moves from the target-branch step to the
// source-branch step, defaulting to and preselecting the active workspace's
// EffectiveDefaultTargetBranch.
func (m Model) advanceWizardFromTarget() (tea.Model, tea.Cmd) {
	w := m.wizard
	branches := listLocalBranches(w.repoPath)
	def := m.workspaces[m.activeWS].EffectiveDefaultTargetBranch()

	w.step = wizardStepSource
	w.sourcePicker = newBranchPicker(
		"Source branch — where this work will eventually be merged",
		branches, true, "other (type a branch name)", def, def,
	)
	return m, w.sourcePicker.Init()
}

// advanceWizardFromSource moves from the source-branch step to the final
// confirmation, recapping every choice made so far.
func (m Model) advanceWizardFromSource() (tea.Model, tea.Cmd) {
	w := m.wizard
	recap := fmt.Sprintf(
		"Task:   %s\nRepo:   %s\nTarget: %s\nSource: %s",
		strings.TrimSpace(w.title), m.repoNames[w.repoID], w.targetPicker.Result(), w.sourcePicker.Result(),
	)
	w.step = wizardStepConfirm
	w.confirmed = true // huh.Confirm defaults to whichever its bound value already holds — pre-select "Create" rather than "Cancel"
	w.confirmForm = huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title("Create task?").Description(recap).
			Affirmative("Create").Negative("Cancel").Value(&w.confirmed),
	)).WithWidth(56).WithShowHelp(true)
	return m, w.confirmForm.Init()
}

// renderNewTaskWizard draws whichever step is currently active, centered
// over the full screen like every other modal.
func (m Model) renderNewTaskWizard() string {
	w := m.wizard
	var content string
	switch w.step {
	case wizardStepTitle:
		content = w.titleForm.View() + "\n" + tabInfoStyle.Render("[enter] next   [ctrl+enter] quick create   [esc] cancel")
	case wizardStepRepo:
		content = w.repoForm.View()
	case wizardStepTarget:
		content = w.targetPicker.View()
	case wizardStepSource:
		content = w.sourcePicker.View()
	case wizardStepConfirm:
		content = w.confirmForm.View()
	}
	box := focusedPaneBorderStyle().Padding(1, 3).Render("New task\n\n" + content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}
