package ui

import (
	"testing"
	"time"

	"cormake/internal/domain"
	"cormake/internal/store"
)

func TestWorktreeName(t *testing.T) {
	t.Run("readable id", func(t *testing.T) {
		task := domain.Task{ID: "a1b2c3d4-0000-0000-0000-000000000000", DisplayID: "ACME-7"}
		if got, want := worktreeName(task), "acme-7"; got != want {
			t.Errorf("worktreeName() = %q, want %q", got, want)
		}
	})

	t.Run("falls back to uuid-based name when DisplayID unset", func(t *testing.T) {
		task := domain.Task{ID: "a1b2c3d4-0000-0000-0000-000000000000"}
		if got, want := worktreeName(task), "task-a1b2c3d4"; got != want {
			t.Errorf("worktreeName() = %q, want %q", got, want)
		}
	})
}

func TestHandleAgentEventPersistsSessionID(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	const (
		taskID   = "task-123"
		staleID  = "cormake-preassigned-uuid"
		cursorID = "cursor-agent-session-id"
	)

	now := time.Now()
	task := domain.Task{
		ID:        taskID,
		SessionID: staleID,
		Status:    domain.StatusPlanning,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := st.SaveTask(task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}

	m := Model{
		store: st,
		tasks: []domain.Task{task},
	}

	m.updateTaskSessionID(taskID, cursorID)

	if got, want := m.tasks[0].SessionID, cursorID; got != want {
		t.Errorf("in-memory SessionID = %q, want %q", got, want)
	}

	loaded, err := st.LoadTasks()
	if err != nil {
		t.Fatalf("LoadTasks: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("LoadTasks len = %d, want 1", len(loaded))
	}
	if got, want := loaded[0].SessionID, cursorID; got != want {
		t.Errorf("persisted SessionID = %q, want %q", got, want)
	}
}
