package ui

import (
	"strings"
)

// listLocalBranches returns repoPath's local and remote-tracking branches,
// most-recently-committed first — a reasonable default order for a branch
// picker, since the user's own recent work is likely near the top. Remote
// branches (e.g. "origin/main") are reduced to their plain name so a branch
// that's only ever been fetched, never checked out locally, still shows up
// as a pickable option — common for a source/merge-target branch in a fresh
// clone or worktree. A branch present both locally and on a remote is only
// listed once. Best-effort: an empty/unreadable repo (e.g. no commits yet)
// just yields no branches rather than an error, leaving a branch picker's
// "create new" option as the only choice.
func listLocalBranches(repoPath string) []string {
	out, err := runGit(repoPath, "branch", "--all", "--format=%(refname)", "--sort=-committerdate")
	if err != nil || strings.TrimSpace(out) == "" {
		return nil
	}

	seen := make(map[string]bool)
	var branches []string
	for _, ref := range strings.Split(out, "\n") {
		var name string
		switch {
		case strings.HasPrefix(ref, "refs/heads/"):
			name = strings.TrimPrefix(ref, "refs/heads/")
		case strings.HasPrefix(ref, "refs/remotes/"):
			rest := strings.TrimPrefix(ref, "refs/remotes/")
			idx := strings.Index(rest, "/")
			if idx == -1 {
				continue
			}
			name = rest[idx+1:]
			if name == "HEAD" {
				continue // origin/HEAD is a symbolic pointer, not a branch
			}
		default:
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		branches = append(branches, name)
	}
	return branches
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
