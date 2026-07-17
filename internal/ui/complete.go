package ui

import (
	"fmt"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// completeFinishedMsg reports the outcome of finalizing a task: committing
// its worktree's outstanding changes onto a named feature branch and
// removing the worktree.
type completeFinishedMsg struct {
	taskID string
	branch string
	err    error
}

// completeTaskCmd commits any outstanding changes in the task's worktree,
// renames its branch to branch, and removes the worktree — the standard
// "done with this worktree" sequence for a git worktree claude created (via
// -w). Such a worktree is locked while its session is active; unlocking is
// required before `git worktree remove` will touch it (confirmed directly —
// remove fails outright otherwise with "cannot remove a locked working
// tree").
func completeTaskCmd(taskID, repoPath, worktreePath, branch, commitMessage string) tea.Cmd {
	return func() tea.Msg {
		if err := commitWorktreeChanges(worktreePath, commitMessage); err != nil {
			return completeFinishedMsg{taskID: taskID, err: fmt.Errorf("commit: %w", err)}
		}
		if out, err := runGit(worktreePath, "branch", "-m", branch); err != nil {
			return completeFinishedMsg{taskID: taskID, err: fmt.Errorf("rename branch: %w: %s", err, out)}
		}
		// Best-effort: a worktree that was never locked (or got unlocked
		// some other way) errors here too, and remove below still works —
		// only remove's own failure is worth surfacing.
		_, _ = runGit(repoPath, "worktree", "unlock", worktreePath)
		if out, err := runGit(repoPath, "worktree", "remove", worktreePath); err != nil {
			return completeFinishedMsg{taskID: taskID, branch: branch, err: fmt.Errorf("remove worktree: %w: %s", err, out)}
		}
		return completeFinishedMsg{taskID: taskID, branch: branch}
	}
}

// commitWorktreeChanges stages and commits everything in worktreePath, but
// only if there's actually something to commit — a clean worktree (e.g. the
// agent already committed its own work along the way) is left alone rather
// than erroring on an empty commit.
func commitWorktreeChanges(worktreePath, message string) error {
	statusOut, err := runGit(worktreePath, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(statusOut) == "" {
		return nil
	}
	if out, err := runGit(worktreePath, "add", "-A"); err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	if out, err := runGit(worktreePath, "commit", "-m", message); err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}

// runGit runs `git -C dir <args...>` and returns its combined output
// (trimmed), for both the happy path (e.g. `status --porcelain`'s stdout)
// and error reporting (git's own error text is more useful than Go's bare
// exit-status error).
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
