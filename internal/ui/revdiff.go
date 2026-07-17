package ui

import (
	"errors"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// reviewKind distinguishes what a revdiff session was reviewing, so
// handleRevdiffFinished knows how to act on the resulting annotations —
// revise a plan (resuming a Plan-mode session) or address code-review
// feedback (resuming a Complete-mode session in its worktree).
type reviewKind int

const (
	reviewKindPlan reviewKind = iota
	reviewKindExecute
)

// revdiffFinishedMsg reports the outcome of a revdiff annotation session.
// Annotations is empty when the user quit without adding any (a clean,
// nothing-to-do outcome, not an error).
type revdiffFinishedMsg struct {
	taskID      string
	kind        reviewKind
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

// openRevdiffDiffCmd suspends the TUI to let the user review an execute-mode
// task's actual code changes in revdiff — the worktree's working tree
// (including untracked new files, via --untracked) diffed against baseRef,
// the repo's HEAD at the moment the worktree was created. Unlike
// openRevdiffCmd (a plan reviewed as a stdin scratch buffer), this points
// revdiff straight at the worktree's real git state; no stdin involved, so
// Stdin is left nil like Stdout/Stderr, same tty-wiring reasoning as above.
//
// resultSummary, if the task has one (the last thing claude said when the
// run finished), is appended to what the reviewer sees via revdiff's own
// --description-file — prose context shown in its info popup (confirmed
// directly: press 'i' in revdiff) — alongside the diff, rather than folded
// into the diff content itself.
func openRevdiffDiffCmd(taskID, worktreePath, baseRef, resultSummary string) tea.Cmd {
	outFile, err := os.CreateTemp("", "cormake-diff-annotations-*.md")
	if err != nil {
		return func() tea.Msg { return revdiffFinishedMsg{taskID: taskID, kind: reviewKindExecute, err: err} }
	}
	outPath := outFile.Name()
	outFile.Close()

	args := []string{"--untracked", "-o", outPath, "--exit-code-on-annotations"}
	if baseRef != "" {
		args = append(args, baseRef)
	}

	var descPath string
	if strings.TrimSpace(resultSummary) != "" {
		if f, err := os.CreateTemp("", "cormake-diff-description-*.md"); err == nil {
			descPath = f.Name()
			f.Close()
			if writeErr := os.WriteFile(descPath, []byte(resultSummary), 0o644); writeErr != nil {
				os.Remove(descPath)
				descPath = ""
			}
		}
	}
	if descPath != "" {
		args = append(args, "--description-file", descPath)
	}

	cmd := exec.Command("revdiff", args...)
	cmd.Dir = worktreePath

	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		defer os.Remove(outPath)
		if descPath != "" {
			defer os.Remove(descPath)
		}

		var exitErr *exec.ExitError
		hasAnnotations := errors.As(err, &exitErr) && exitErr.ExitCode() == 10
		if hasAnnotations {
			err = nil
		}
		if err != nil {
			return revdiffFinishedMsg{taskID: taskID, kind: reviewKindExecute, err: err}
		}
		if !hasAnnotations {
			return revdiffFinishedMsg{taskID: taskID, kind: reviewKindExecute}
		}

		data, readErr := os.ReadFile(outPath)
		if readErr != nil {
			return revdiffFinishedMsg{taskID: taskID, kind: reviewKindExecute, err: readErr}
		}
		return revdiffFinishedMsg{taskID: taskID, kind: reviewKindExecute, annotations: strings.TrimSpace(string(data))}
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

// buildExecuteRevisePrompt turns revdiff annotations left on the actual code
// diff into a follow-up prompt asking claude to address them with further
// changes in the same session/worktree. Ends with the same summary
// instruction as buildExecutePrompt (see executeSummaryInstruction) since
// this also ends a Complete-mode run whose final message becomes the task's
// stored ResultSummary.
func buildExecuteRevisePrompt(annotations string) string {
	return "Here is feedback on the code changes you just made, as inline review annotations " +
		"(each is a \"## <label>:<line>\" heading followed by the note):\n\n" +
		annotations +
		"\n\nPlease address this feedback with further changes." +
		executeSummaryInstruction
}
