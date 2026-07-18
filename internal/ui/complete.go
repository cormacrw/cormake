package ui

import (
	"fmt"
	"os/exec"
	"path/filepath"
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
// "done with this worktree" sequence. The unlock call is best-effort: a
// worktree cormake created itself (see createWorktree) is never locked, but
// this also has to handle worktrees created by older cormake builds via
// claude's own -w, which does lock them while its session is active —
// `git worktree remove` refuses those outright otherwise (confirmed
// directly: "cannot remove a locked working tree").
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

// createWorktree creates a new git worktree at
// <repoPath>/.claude/worktrees/<dir-safe branch name> for branch. If branch
// already exists in repoPath, the worktree checks it out as-is (this is how
// "allow task on existing branch" is honored — see findWorktreeForBranch
// for the case where that branch already has a worktree open elsewhere,
// which this function is never called for). Otherwise branch is created
// fresh, forking from repoPath's actual local HEAD.
//
// Forking new branches from local HEAD (rather than handing worktree
// creation to claude's own -w/--worktree flag) is deliberate: confirmed
// directly that -w instead forks from the repo's remote-tracking default
// branch whenever one is configured — regardless of which local branch is
// checked out or how far local HEAD has diverged from it — silently
// dropping any local-only commits. Doing it ourselves guarantees the
// worktree actually reflects what's on disk.
func createWorktree(repoPath, branch string) (string, error) {
	dirName := strings.ReplaceAll(branch, "/", "-")
	path := filepath.Join(repoPath, ".claude", "worktrees", dirName)

	var out string
	var err error
	if branchExists(repoPath, branch) {
		out, err = runGit(repoPath, "worktree", "add", path, branch)
	} else {
		out, err = runGit(repoPath, "worktree", "add", "-b", branch, path, "HEAD")
	}
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, out)
	}
	return path, nil
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
