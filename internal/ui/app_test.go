package ui

import (
	"testing"

	"cormake/internal/domain"
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
