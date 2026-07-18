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

// slugify lowercases s and collapses every run of non-alphanumeric
// characters into a single "-", trimming any trailing one — used to name a
// task's eventual completed-feature branch (see suggestBranchName).
func slugify(s string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	name := strings.TrimRight(b.String(), "-")
	if len(name) > 50 {
		name = strings.TrimRight(name[:50], "-")
	}
	return name
}

// suggestTargetBranchName derives a default new-branch name from a task's
// human-readable id, shown as the default option in the new-task wizard's
// target-branch step (and the standalone change-target-branch shortcut).
// Distinct from suggestBranchName's "feature/" prefix (the completed-work
// branch chosen at Complete time) since this names the in-progress working
// branch instead.
func suggestTargetBranchName(displayID string) string {
	return "feat/" + displayID
}
