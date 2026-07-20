package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// handlePlanToolUse records where a task's plan landed when an agent tool
// call writes or declares one. Claude plan-mode runs Write/Edit into
// ~/.claude/plans/; cursor plan-mode runs createPlanToolCall with inline
// markdown (planUri is empty headless — confirmed directly), which cormake
// persists under store.PlanPath. Cursor file writes to ~/.cursor/plans/
// or this task's existing PlanFilePath are also recognized.
func (m *Model) handlePlanToolUse(taskID, toolName, toolInput string) {
	if path, ok := extractClaudePlanFilePath(toolName, toolInput); ok {
		m.setPlanFilePath(taskID, path)
		return
	}
	if path, ok := extractCursorPlanFilePath(toolName, toolInput); ok {
		m.setPlanFilePath(taskID, path)
		return
	}
	if path, ok := extractKnownPlanFilePath(toolName, toolInput, m.taskPlanFilePath(taskID)); ok {
		m.setPlanFilePath(taskID, path)
		return
	}
	content, ok := extractCursorPlanContent(toolName, toolInput)
	if !ok || m.store == nil {
		return
	}
	path := m.store.PlanPath(taskID)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return
	}
	m.setPlanFilePath(taskID, path)
}

func (m Model) taskPlanFilePath(taskID string) string {
	for _, t := range m.tasks {
		if t.ID == taskID {
			return t.PlanFilePath
		}
	}
	return ""
}

// extractClaudePlanFilePath checks whether a Write/Edit tool call targeted
// claude's plan directory (~/.claude/plans/ — its own hardcoded, always
// read-only-mode-permitted scratch space for plan-mode, confirmed
// directly, not assumed). toolInputJSON is the tool's JSON-stringified
// input; only its file_path field is used.
func extractClaudePlanFilePath(toolName, toolInputJSON string) (string, bool) {
	if toolName != "Write" && toolName != "Edit" {
		return "", false
	}
	var input struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal([]byte(toolInputJSON), &input); err != nil || input.FilePath == "" {
		return "", false
	}
	plansDir, err := claudePlansDir()
	if err != nil || !strings.HasPrefix(input.FilePath, plansDir) {
		return "", false
	}
	return input.FilePath, true
}

// extractCursorPlanFilePath checks whether a cursor writeToolCall or
// editToolCall targeted ~/.cursor/plans/ (cursor's plan scratch space,
// analogous to claude's ~/.claude/plans/).
func extractCursorPlanFilePath(toolName, toolInputJSON string) (string, bool) {
	if toolName != "writeToolCall" && toolName != "editToolCall" {
		return "", false
	}
	var input struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(toolInputJSON), &input); err != nil || input.Path == "" {
		return "", false
	}
	plansDir, err := cursorPlansDir()
	if err != nil || !strings.HasPrefix(input.Path, plansDir) {
		return "", false
	}
	return input.Path, true
}

// extractCursorPlanContent pulls markdown from a createPlanToolCall's args
// — cursor plan-mode's primary delivery mechanism in headless runs.
func extractCursorPlanContent(toolName, toolInputJSON string) (string, bool) {
	if toolName != "createPlanToolCall" {
		return "", false
	}
	var input struct {
		Plan string `json:"plan"`
	}
	if err := json.Unmarshal([]byte(toolInputJSON), &input); err != nil || strings.TrimSpace(input.Plan) == "" {
		return "", false
	}
	return input.Plan, true
}

// extractKnownPlanFilePath checks whether a cursor write/edit tool call
// targeted this task's already-recorded plan file — used when a revise run
// edits ~/.cormake/plans/{taskID}.md in place instead of createPlanToolCall.
func extractKnownPlanFilePath(toolName, toolInputJSON, knownPath string) (string, bool) {
	if knownPath == "" {
		return "", false
	}
	if toolName != "writeToolCall" && toolName != "editToolCall" {
		return "", false
	}
	var input struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(toolInputJSON), &input); err != nil || input.Path != knownPath {
		return "", false
	}
	return knownPath, true
}

func claudePlansDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "plans") + string(filepath.Separator), nil
}

func cursorPlansDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cursor", "plans") + string(filepath.Separator), nil
}
