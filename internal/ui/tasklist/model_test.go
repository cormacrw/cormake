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
	todoNew := domain.Task{ID: "todo-new", Status: domain.StatusInProgress, UpdatedAt: now}
	planned := domain.Task{ID: "planned", Status: domain.StatusPlanned}
	review := domain.Task{ID: "review", Status: domain.StatusReadyForReview}

	items := toItems([]domain.Task{todoOld, planned, todoNew, review})

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

	wantHeaders := []string{"PLANNED / READY FOR REVIEW", "OTHER"}
	if len(headers) != len(wantHeaders) || headers[0] != wantHeaders[0] || headers[1] != wantHeaders[1] {
		t.Fatalf("headers = %v, want %v", headers, wantHeaders)
	}

	// Top section keeps input order; bottom section is most-recently-updated first.
	wantIDs := []string{"planned", "review", "todo-new", "todo-old"}
	if len(gotIDs) != len(wantIDs) {
		t.Fatalf("gotIDs = %v, want %v", gotIDs, wantIDs)
	}
	for i, id := range wantIDs {
		if gotIDs[i] != id {
			t.Errorf("gotIDs[%d] = %q, want %q", i, gotIDs[i], id)
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
