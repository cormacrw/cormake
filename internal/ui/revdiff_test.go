package ui

import (
	"strings"
	"testing"
)

func TestBuildRevisePromptIncludesPlanPath(t *testing.T) {
	const path = "/home/user/.cormake/plans/task-1.md"
	got := buildRevisePrompt("## note:3\nfix step 2", path)
	if !strings.Contains(got, "The current plan file is at "+path) {
		t.Errorf("prompt = %q, want plan file path mentioned", got)
	}
}
