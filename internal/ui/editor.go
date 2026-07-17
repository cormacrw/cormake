package ui

import (
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"cormake/internal/domain"
)

// editorFinishedMsg reports the outcome of an external-editor session
// started by openInEditorCmd. path is empty if the temp file was never
// successfully created.
type editorFinishedMsg struct {
	taskID string
	path   string
	err    error
}

// editorCommand picks the external editor: $VISUAL, then $EDITOR, then
// nvim as the default cormake was asked to support.
func editorCommand() string {
	if e := os.Getenv("VISUAL"); e != "" {
		return e
	}
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	return "nvim"
}

// taskToEditorContent formats a task as a single markdown file: an H1
// title line, a blank line, then the description body.
func taskToEditorContent(t domain.Task) string {
	return "# " + t.Title + "\n\n" + t.Description
}

// parseEditorContent reverses taskToEditorContent: the first line becomes
// the title (its leading "# " is stripped if present), everything after
// the first blank-line separator becomes the description.
func parseEditorContent(content string) (title, description string) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	firstLine, rest, _ := strings.Cut(content, "\n")
	title = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(firstLine), "#"))
	description = strings.TrimSpace(strings.TrimPrefix(rest, "\n"))
	return title, description
}

// openInEditorCmd writes t to a temp markdown file and suspends the TUI to
// run the user's editor on it via tea.ExecProcess, which hands the terminal
// over completely and resumes bubbletea's renderer once the editor exits.
func openInEditorCmd(t domain.Task) tea.Cmd {
	f, err := os.CreateTemp("", "cormake-task-*.md")
	if err != nil {
		return func() tea.Msg { return editorFinishedMsg{taskID: t.ID, err: err} }
	}
	path := f.Name()

	_, writeErr := f.WriteString(taskToEditorContent(t))
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		os.Remove(path)
		err := writeErr
		if err == nil {
			err = closeErr
		}
		return func() tea.Msg { return editorFinishedMsg{taskID: t.ID, err: err} }
	}

	cmd := exec.Command(editorCommand(), path)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorFinishedMsg{taskID: t.ID, path: path, err: err}
	})
}

// fileEditFinishedMsg reports the outcome of an external-editor session
// started by openFileInEditorCmd — editing an existing file directly, with
// no temp file and nothing to parse back.
type fileEditFinishedMsg struct {
	path string
	err  error
}

// openFileInEditorCmd suspends the TUI to edit an existing file in place
// (e.g. workspaces.json, for manual repo management) via tea.ExecProcess.
func openFileInEditorCmd(path string) tea.Cmd {
	cmd := exec.Command(editorCommand(), path)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return fileEditFinishedMsg{path: path, err: err}
	})
}
