package ui

import (
	"strings"
)

// listLocalBranches returns repoPath's local branches, most-recently-committed
// first — a reasonable default order for a branch picker, since the user's
// own recent work is likely near the top. Best-effort: an empty/unreadable
// repo (e.g. no commits yet) just yields no branches rather than an error,
// leaving a branch picker's "create new" option as the only choice.
func listLocalBranches(repoPath string) []string {
	out, err := runGit(repoPath, "branch", "--format=%(refname:short)", "--sort=-committerdate")
	if err != nil || strings.TrimSpace(out) == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// branchExists reports whether branch is a known local branch in repoPath.
func branchExists(repoPath, branch string) bool {
	_, err := runGit(repoPath, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// findWorktreeForBranch scans repoPath's existing worktrees (via `git
// worktree list --porcelain`) for one already checked out onto branch,
// returning its path. A task whose target branch is already open elsewhere
// reuses that worktree instead of creating a second one for the same
// branch — git itself refuses two worktrees on one branch anyway.
func findWorktreeForBranch(repoPath, branch string) (string, bool) {
	out, err := runGit(repoPath, "worktree", "list", "--porcelain")
	if err != nil {
		return "", false
	}
	want := "refs/heads/" + branch
	var currentPath string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			currentPath = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch "):
			if strings.TrimPrefix(line, "branch ") == want {
				return currentPath, true
			}
		}
	}
	return "", false
}

// suggestTargetBranchName derives a default new-branch name from a task's
// human-readable id, shown as the default option in the new-task wizard's
// target-branch step (and the standalone change-target-branch shortcut) —
// this is also the branch a task's work ultimately lands on at Complete
// time, since execution commits straight onto it throughout (see
// ui.handleTaskFinished).
func suggestTargetBranchName(displayID string) string {
	return "feat/" + displayID
}
