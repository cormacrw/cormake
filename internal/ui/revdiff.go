package ui

import (
	"errors"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// revdiffFinishedMsg reports the outcome of a revdiff annotation session.
// Annotations is empty when the user quit without adding any (a clean,
// nothing-to-do outcome, not an error).
type revdiffFinishedMsg struct {
	taskID      string
	annotations string
	err         error
}

// openRevdiffCmd suspends the TUI to let the user annotate a task's plan
// text in revdiff (an external diff/document review TUI — see
// https://github.com/umputun/revdiff), then reports back whatever
// annotations it produced.
//
// The plan text is fed via stdin rather than a temp file: tea.ExecProcess's
// underlying wiring (see charmbracelet/bubbletea's exec.go) only sets
// Stdin/Stdout/Stderr when they're nil, so pre-setting cmd.Stdin here is
// respected, while leaving Stdout/Stderr untouched lets bubbletea wire them
// to the real terminal — required for revdiff's interactive UI to render at
// all (confirmed directly: redirecting its stdout away from a real tty
// breaks rendering). Annotations come back via -o to a temp file rather
// than captured from stdout, sidestepping that same constraint entirely.
func openRevdiffCmd(taskID, planText string) tea.Cmd {
	outFile, err := os.CreateTemp("", "cormake-plan-annotations-*.md")
	if err != nil {
		return func() tea.Msg { return revdiffFinishedMsg{taskID: taskID, err: err} }
	}
	outPath := outFile.Name()
	outFile.Close()

	cmd := exec.Command("revdiff",
		"--stdin",
		"--stdin-name", "plan.md",
		"-o", outPath,
		"--exit-code-on-annotations",
	)
	cmd.Stdin = strings.NewReader(planText)

	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		defer os.Remove(outPath)

		// Exit code 10 means "annotations were produced" per
		// --exit-code-on-annotations — a successful outcome, not an error.
		var exitErr *exec.ExitError
		hasAnnotations := errors.As(err, &exitErr) && exitErr.ExitCode() == 10
		if hasAnnotations {
			err = nil
		}
		if err != nil {
			return revdiffFinishedMsg{taskID: taskID, err: err}
		}
		if !hasAnnotations {
			return revdiffFinishedMsg{taskID: taskID} // clean quit, nothing to send
		}

		data, readErr := os.ReadFile(outPath)
		if readErr != nil {
			return revdiffFinishedMsg{taskID: taskID, err: readErr}
		}
		return revdiffFinishedMsg{taskID: taskID, annotations: strings.TrimSpace(string(data))}
	})
}

// buildRevisePrompt turns raw revdiff annotation output (format: "##
// <label>:<line>" followed by the note on the next line, one block per
// annotation) into a follow-up prompt asking claude to revise its plan.
func buildRevisePrompt(annotations string) string {
	return "Here is feedback on the plan you just proposed, as inline review annotations " +
		"(each is a \"## <label>:<line>\" heading followed by the note):\n\n" +
		annotations +
		"\n\nPlease revise the plan to address this feedback."
}
