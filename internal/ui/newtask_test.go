package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"cormake/internal/domain"
)

func testWorkspaceModel() Model {
	ws := domain.Workspace{
		ID:   "ws1",
		Name: "Acme",
		Repos: []domain.Repo{
			{ID: "r1", Name: "repo1", Path: "/nonexistent"},
		},
	}
	return Model{
		workspaces: []domain.Workspace{ws},
		repoNames:  map[string]string{"r1": "repo1"},
	}
}

// TestNewTaskWizardBackNavigation exercises shift+tab stepping back through
// every step of the wizard, verifying it lands on the right step and that
// whatever was already entered survives the round trip instead of resetting
// to that step's defaults.
func TestNewTaskWizardBackNavigation(t *testing.T) {
	t.Run("repo step back to title preserves the typed title", func(t *testing.T) {
		m := testWorkspaceModel()
		w := &newTaskWizard{step: wizardStepRepo, title: "fix the bug", repos: m.workspaces[0].Repos, repoID: "r1"}
		m.wizard = w

		mdl, _ := m.updateNewTaskWizard(tea.KeyMsg{Type: tea.KeyShiftTab})
		m2 := mdl.(Model)

		if m2.wizard.step != wizardStepTitle {
			t.Fatalf("step = %v, want wizardStepTitle", m2.wizard.step)
		}
		if m2.wizard.title != "fix the bug" {
			t.Errorf("title = %q, want %q", m2.wizard.title, "fix the bug")
		}
		if m2.wizard.titleForm.State != huh.StateNormal {
			t.Errorf("titleForm.State = %v, want StateNormal (rebuilt, not still completed)", m2.wizard.titleForm.State)
		}
	})

	t.Run("target step back to repo preserves the picked repo", func(t *testing.T) {
		m := testWorkspaceModel()
		targetPicker := newBranchPicker("Target branch", nil, true, "+ create new", "suggested", "")
		w := &newTaskWizard{step: wizardStepTarget, repos: m.workspaces[0].Repos, repoID: "r1", repoPath: "/nonexistent", targetPicker: targetPicker}
		m.wizard = w

		mdl, _ := m.updateNewTaskWizard(tea.KeyMsg{Type: tea.KeyShiftTab})
		m2 := mdl.(Model)

		if m2.wizard.step != wizardStepRepo {
			t.Fatalf("step = %v, want wizardStepRepo", m2.wizard.step)
		}
		if m2.wizard.repoID != "r1" {
			t.Errorf("repoID = %q, want %q", m2.wizard.repoID, "r1")
		}
		if m2.wizard.repoForm.State != huh.StateNormal {
			t.Errorf("repoForm.State = %v, want StateNormal", m2.wizard.repoForm.State)
		}
	})

	t.Run("source step back to target preserves a typed new-branch name", func(t *testing.T) {
		m := testWorkspaceModel()
		targetPicker := newBranchPicker("Target branch", nil, true, "+ create new (%q)", "suggested-name", "")
		targetPicker.mode = newBranchSentinel
		targetPicker.value = "my-custom-branch"
		targetPicker.done = true

		sourcePicker := newBranchPicker("Source branch", nil, true, "other", "main", "main")
		w := &newTaskWizard{step: wizardStepSource, repoPath: "/nonexistent", targetPicker: targetPicker, sourcePicker: sourcePicker}
		m.wizard = w

		mdl, _ := m.updateNewTaskWizard(tea.KeyMsg{Type: tea.KeyShiftTab})
		m2 := mdl.(Model)

		if m2.wizard.step != wizardStepTarget {
			t.Fatalf("step = %v, want wizardStepTarget", m2.wizard.step)
		}
		if m2.wizard.targetPicker.done {
			t.Error("targetPicker.Done() = true, want a fresh, not-yet-done picker")
		}
		if m2.wizard.targetPicker.mode != newBranchSentinel {
			t.Errorf("targetPicker.mode = %q, want newBranchSentinel (new-branch option preserved)", m2.wizard.targetPicker.mode)
		}
		if m2.wizard.targetPicker.value != "my-custom-branch" {
			t.Errorf("targetPicker.value = %q, want %q", m2.wizard.targetPicker.value, "my-custom-branch")
		}
	})

	t.Run("confirm step back to source preserves a typed new-branch name", func(t *testing.T) {
		m := testWorkspaceModel()
		sourcePicker := newBranchPicker("Source branch", nil, true, "other", "main", "main")
		sourcePicker.mode = newBranchSentinel
		sourcePicker.value = "release-42"
		sourcePicker.done = true

		w := &newTaskWizard{step: wizardStepConfirm, repoPath: "/nonexistent", sourcePicker: sourcePicker}
		m.wizard = w

		mdl, _ := m.updateNewTaskWizard(tea.KeyMsg{Type: tea.KeyShiftTab})
		m2 := mdl.(Model)

		if m2.wizard.step != wizardStepSource {
			t.Fatalf("step = %v, want wizardStepSource", m2.wizard.step)
		}
		if m2.wizard.sourcePicker.done {
			t.Error("sourcePicker.Done() = true, want a fresh, not-yet-done picker")
		}
		if m2.wizard.sourcePicker.mode != newBranchSentinel {
			t.Errorf("sourcePicker.mode = %q, want newBranchSentinel", m2.wizard.sourcePicker.mode)
		}
		if m2.wizard.sourcePicker.value != "release-42" {
			t.Errorf("sourcePicker.value = %q, want %q", m2.wizard.sourcePicker.value, "release-42")
		}
	})

	t.Run("shift+tab on the title step is a no-op — nothing precedes it", func(t *testing.T) {
		m := testWorkspaceModel()
		m.openNewTaskWizard()
		w := m.wizard

		mdl, _ := m.updateNewTaskWizard(tea.KeyMsg{Type: tea.KeyShiftTab})
		m2 := mdl.(Model)

		if m2.wizard != w || m2.wizard.step != wizardStepTitle {
			t.Fatalf("shift+tab on the title step should be a no-op")
		}
	})

	t.Run("shift+tab while a picker's search box is engaged backs out of search, not the wizard step", func(t *testing.T) {
		m := testWorkspaceModel()
		targetPicker := newBranchPicker("Target branch", []string{"main", "develop"}, true, "+ create new", "suggested", "")
		targetPicker.Init()
		targetPicker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
		if !targetPicker.Searching() {
			t.Fatal("setup: expected picker to be searching")
		}

		w := &newTaskWizard{step: wizardStepTarget, repoPath: "/nonexistent", targetPicker: targetPicker}
		m.wizard = w

		mdl, _ := m.updateNewTaskWizard(tea.KeyMsg{Type: tea.KeyShiftTab})
		m2 := mdl.(Model)

		if m2.wizard.step != wizardStepTarget {
			t.Fatalf("step = %v, want wizardStepTarget (shift+tab should forward into the search box, not step back)", m2.wizard.step)
		}
	})
}
