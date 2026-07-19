package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestBranchPickerSearching(t *testing.T) {
	p := newBranchPicker("Target branch", []string{"main", "develop", "feature/foo"}, true, "+ create new branch", "suggested", "")
	p.Init()

	if p.Searching() {
		t.Fatal("Searching() = true before any input, want false")
	}

	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if !p.Searching() {
		t.Fatal("Searching() = false after \"/\", want true")
	}

	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("dev")})
	if !p.Searching() {
		t.Fatal("Searching() = false while typing a filter query, want true")
	}

	// esc backs out of the search box, not out of the picker: the picker
	// should still be open and unresolved.
	p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if p.Searching() {
		t.Fatal("Searching() = true after esc, want false")
	}
	if p.Done() {
		t.Fatal("Done() = true after esc while searching, want false — esc should only dismiss the search box")
	}
}

func TestBranchPickerSearchingClearedOnEnter(t *testing.T) {
	p := newBranchPicker("Target branch", []string{"main", "develop"}, true, "+ create new branch", "suggested", "")
	p.Init()

	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if !p.Searching() {
		t.Fatal("Searching() = false after \"/\", want true")
	}

	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("main")})
	p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if p.Searching() {
		t.Fatal("Searching() = true after enter, want false — enter submits and exits the filter")
	}
}
