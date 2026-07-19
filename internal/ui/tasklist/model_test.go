package tasklist

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"cormake/internal/domain"
)

func TestToItemsSplitsAndOrdersSections(t *testing.T) {
	now := time.Now()
	todoOld := domain.Task{ID: "todo-old", Status: domain.StatusTodo, UpdatedAt: now.Add(-2 * time.Hour)}
	inProgressOld := domain.Task{ID: "in-progress-old", Status: domain.StatusInProgress, UpdatedAt: now.Add(-1 * time.Hour)}
	inProgressNew := domain.Task{ID: "in-progress-new", Status: domain.StatusPlanning, UpdatedAt: now}
	planned := domain.Task{ID: "planned", Status: domain.StatusPlanned}
	review := domain.Task{ID: "review", Status: domain.StatusReadyForReview}

	items := toItems([]domain.Task{todoOld, planned, inProgressOld, inProgressNew, review})

	var gotIDs []string
	var headers []string
	for _, it := range items {
		i := it.(Item)
		if i.Header {
			headers = append(headers, i.HeaderText)
			continue
		}
		gotIDs = append(gotIDs, i.Task.ID)
	}

	wantHeaders := []string{"PLANNED / READY FOR REVIEW", "IN PROGRESS", "OTHER"}
	if len(headers) != len(wantHeaders) {
		t.Fatalf("headers = %v, want %v", headers, wantHeaders)
	}
	for i, h := range wantHeaders {
		if headers[i] != h {
			t.Errorf("headers[%d] = %q, want %q", i, headers[i], h)
		}
	}

	// Top section keeps input order; the other two sections are
	// most-recently-updated first.
	wantIDs := []string{"planned", "review", "in-progress-new", "in-progress-old", "todo-old"}
	if len(gotIDs) != len(wantIDs) {
		t.Fatalf("gotIDs = %v, want %v", gotIDs, wantIDs)
	}
	for i, id := range wantIDs {
		if gotIDs[i] != id {
			t.Errorf("gotIDs[%d] = %q, want %q", i, gotIDs[i], id)
		}
	}
}

func TestToItemsOmitsEmptySectionHeaderWhenOnlyTwoPopulated(t *testing.T) {
	now := time.Now()
	planned := domain.Task{ID: "planned", Status: domain.StatusPlanned}
	inProgress := domain.Task{ID: "in-progress", Status: domain.StatusInProgress, UpdatedAt: now}

	items := toItems([]domain.Task{planned, inProgress})

	var headers []string
	for _, it := range items {
		if i := it.(Item); i.Header {
			headers = append(headers, i.HeaderText)
		}
	}

	wantHeaders := []string{"PLANNED / READY FOR REVIEW", "IN PROGRESS"}
	if len(headers) != len(wantHeaders) {
		t.Fatalf("headers = %v, want %v", headers, wantHeaders)
	}
	for i, h := range wantHeaders {
		if headers[i] != h {
			t.Errorf("headers[%d] = %q, want %q", i, headers[i], h)
		}
	}
}

func TestToItemsOmitsHeaderWhenOneSectionEmpty(t *testing.T) {
	items := toItems([]domain.Task{{ID: "a", Status: domain.StatusTodo}})
	for _, it := range items {
		if it.(Item).Header {
			t.Fatalf("expected no header row when only one section is populated, got items %+v", items)
		}
	}
}

func TestSetTasksSelectsFirstRealTask(t *testing.T) {
	m := New([]domain.Task{{ID: "planned", Status: domain.StatusPlanned}})
	task, ok := m.Selected()
	if !ok {
		t.Fatal("expected a selected task, got none")
	}
	if task.ID != "planned" {
		t.Fatalf("selected task = %q, want %q", task.ID, "planned")
	}
}

func TestSpinnerTickAdvancesSharedFrame(t *testing.T) {
	m := New(nil)
	before := *m.frame

	cmd := m.SpinnerTick()
	if cmd == nil {
		t.Fatal("SpinnerTick() returned a nil cmd")
	}
	msg := cmd()

	if updateCmd := m.UpdateSpinner(msg); updateCmd == nil {
		t.Fatal("UpdateSpinner returned a nil cmd, expected the chain to continue")
	}

	if *m.frame == before {
		t.Fatalf("frame did not change after a spinner tick: got %q both before and after", before)
	}
}

func TestUpdateSkipsHeaderRowOnCursorDown(t *testing.T) {
	m := New([]domain.Task{
		{ID: "planned", Status: domain.StatusPlanned},
		{ID: "todo", Status: domain.StatusTodo},
	})
	m.SetSize(40, 10)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})

	task, ok := m.Selected()
	if !ok {
		t.Fatal("expected a selected task after moving down, got none (landed on header)")
	}
	if task.ID != "todo" {
		t.Fatalf("selected task = %q, want %q", task.ID, "todo")
	}
}
