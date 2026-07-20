package ui

import (
	"os"
	"path/filepath"
	"testing"

	"cormake/internal/domain"
	"cormake/internal/store"
)

func TestExtractClaudePlanFilePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(home, ".claude", "plans", "my-plan.md")

	got, ok := extractClaudePlanFilePath("Write", `{"file_path":"`+planPath+`"}`)
	if !ok {
		t.Fatal("extractClaudePlanFilePath = false, want true")
	}
	if got != planPath {
		t.Errorf("path = %q, want %q", got, planPath)
	}

	if _, ok := extractClaudePlanFilePath("writeToolCall", `{"path":"`+planPath+`"}`); ok {
		t.Error("writeToolCall should not match claude extractor")
	}
}

func TestExtractCursorPlanFilePath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(home, ".cursor", "plans", "my-plan.plan.md")

	got, ok := extractCursorPlanFilePath("editToolCall", `{"path":"`+planPath+`"}`)
	if !ok {
		t.Fatal("extractCursorPlanFilePath = false, want true")
	}
	if got != planPath {
		t.Errorf("path = %q, want %q", got, planPath)
	}
}

func TestExtractCursorPlanContent(t *testing.T) {
	content, ok := extractCursorPlanContent("createPlanToolCall", `{"plan":"# My plan\n\ndo the thing","name":"My plan"}`)
	if !ok {
		t.Fatal("extractCursorPlanContent = false, want true")
	}
	if content != "# My plan\n\ndo the thing" {
		t.Errorf("plan = %q, want markdown body", content)
	}
}

func TestHandlePlanToolUsePersistsCursorPlan(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	task := domain.Task{ID: "task-1", Status: domain.StatusPlanning}
	if err := st.SaveTask(task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}

	m := Model{
		store: st,
		tasks: []domain.Task{task},
	}

	m.handlePlanToolUse(task.ID, "createPlanToolCall", `{"plan":"# Cursor plan\n\nSteps here."}`)

	if got, want := m.tasks[0].PlanFilePath, st.PlanPath(task.ID); got != want {
		t.Errorf("PlanFilePath = %q, want %q", got, want)
	}
	data, err := os.ReadFile(st.PlanPath(task.ID))
	if err != nil {
		t.Fatalf("ReadFile plan: %v", err)
	}
	if string(data) != "# Cursor plan\n\nSteps here." {
		t.Errorf("plan file = %q, want markdown body", string(data))
	}
}
