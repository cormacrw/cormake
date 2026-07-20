package ui

import (
	"fmt"

	"cormake/internal/domain"

	tea "github.com/charmbracelet/bubbletea"
)

// deleteFinishedMsg reports the outcome of git cleanup before a task record
// is removed from memory and disk.
type deleteFinishedMsg struct {
	taskID string
	err    error
}

// taskBranch resolves the git branch associated with a task, using the same
// fallback chain as startCompleteTask and resolveTaskWorktree.
func taskBranch(t domain.Task) string {
	branch := t.TargetBranch
	if branch == "" {
		branch = t.WorktreeName
	}
	if branch == "" {
		branch = worktreeName(t)
	}
	return branch
}

// isTaskOwnedBranch reports whether branch is one cormake auto-created for
// this task (feat/<DisplayID> or the legacy worktreeName), as opposed to a
// branch the user picked from an existing list like develop or main.
func isTaskOwnedBranch(branch string, t domain.Task) bool {
	if t.DisplayID != "" && branch == suggestTargetBranchName(t.DisplayID) {
		return true
	}
	return branch == worktreeName(t)
}

// otherTasksShareBranch reports whether any task other than excludeID still
// references the same worktree path or resolved branch name.
func otherTasksShareBranch(tasks []domain.Task, excludeID, branch, worktreePath string) bool {
	for _, t := range tasks {
		if t.ID == excludeID {
			continue
		}
		if worktreePath != "" && t.WorktreePath == worktreePath {
			return true
		}
		if branch != "" && taskBranch(t) == branch {
			return true
		}
	}
	return false
}

// removeWorktree unlocks (best-effort) and removes a worktree. When force is
// true, uncommitted changes are discarded — appropriate when abandoning a
// task via delete rather than finalizing via complete.
func removeWorktree(repoPath, worktreePath string, force bool) error {
	_, _ = runGit(repoPath, "worktree", "unlock", worktreePath)
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, worktreePath)
	if out, err := runGit(repoPath, args...); err != nil {
		return fmt.Errorf("remove worktree: %w: %s", err, out)
	}
	return nil
}

// removeWorktreeForce is removeWorktree with --force, discarding uncommitted
// changes in the worktree.
func removeWorktreeForce(repoPath, worktreePath string) error {
	return removeWorktree(repoPath, worktreePath, true)
}

// deleteLocalBranch force-deletes a local branch if it exists.
func deleteLocalBranch(repoPath, branch string) error {
	if !branchExists(repoPath, branch) {
		return nil
	}
	if out, err := runGit(repoPath, "branch", "-D", branch); err != nil {
		return fmt.Errorf("delete branch: %w: %s", err, out)
	}
	return nil
}

// deleteTaskCmd removes the task's worktree and task-owned branch when no
// other task still references them. Failures are returned in
// deleteFinishedMsg so the caller can log them while still deleting the
// task record.
func deleteTaskCmd(task domain.Task, repoPath string, allTasks []domain.Task) tea.Cmd {
	return func() tea.Msg {
		branch := taskBranch(task)
		worktreePath := task.WorktreePath
		if worktreePath == "" && branch != "" {
			worktreePath, _ = findWorktreeForBranch(repoPath, branch)
		}
		shared := otherTasksShareBranch(allTasks, task.ID, branch, worktreePath)
		var err error
		if worktreePath != "" && !shared {
			err = removeWorktreeForce(repoPath, worktreePath)
		}
		if err == nil && branch != "" && isTaskOwnedBranch(branch, task) && !shared {
			if branchErr := deleteLocalBranch(repoPath, branch); branchErr != nil {
				err = branchErr
			}
		}
		return deleteFinishedMsg{taskID: task.ID, err: err}
	}
}
