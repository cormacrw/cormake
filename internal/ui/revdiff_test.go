package ui

import (
	"strings"
	"testing"

	"cormake/internal/domain"
)

func TestBuildRevisePromptClaudeIncludesPlanPath(t *testing.T) {
	const path = "/home/user/.claude/plans/my-plan.md"
	got := buildRevisePrompt("## note:3\nfix step 2", domain.AgentBackendClaude, path)
	if !strings.Contains(got, "The current plan file is at "+path) {
		t.Errorf("prompt = %q, want claude plan file path mentioned", got)
	}
}

func TestBuildRevisePromptCursorUsesCreatePlanToolCall(t *testing.T) {
	const path = "/home/user/.cormake/plans/task-1.md"
	got := buildRevisePrompt("## note:3\nfix step 2", domain.AgentBackendCursor, path)
	if strings.Contains(got, path) {
		t.Errorf("cursor prompt should not ask to write %q — plan mode cannot access it", path)
	}
	if !strings.Contains(got, "createPlanToolCall") {
		t.Errorf("prompt = %q, want createPlanToolCall instruction", got)
	}
}
