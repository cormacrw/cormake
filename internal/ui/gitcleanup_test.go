package ui

import (
	"os"
	"os/exec"
	"testing"

	"cormake/internal/domain"
)

func TestTaskBranch(t *testing.T) {
	task := domain.Task{
		ID:        "a1b2c3d4-0000-0000-0000-000000000000",
		DisplayID: "ACME-7",
	}

	t.Run("prefers TargetBranch", func(t *testing.T) {
		task.TargetBranch = "feat/ACME-7"
		task.WorktreeName = "develop"
		if got, want := taskBranch(task), "feat/ACME-7"; got != want {
			t.Errorf("taskBranch() = %q, want %q", got, want)
		}
	})

	t.Run("falls back to WorktreeName", func(t *testing.T) {
		task.TargetBranch = ""
		task.WorktreeName = "develop"
		if got, want := taskBranch(task), "develop"; got != want {
			t.Errorf("taskBranch() = %q, want %q", got, want)
		}
	})

	t.Run("falls back to worktreeName", func(t *testing.T) {
		task.WorktreeName = ""
		if got, want := taskBranch(task), "acme-7"; got != want {
			t.Errorf("taskBranch() = %q, want %q", got, want)
		}
	})
}

func TestIsTaskOwnedBranch(t *testing.T) {
	task := domain.Task{
		ID:        "a1b2c3d4-0000-0000-0000-000000000000",
		DisplayID: "ACME-7",
	}

	if !isTaskOwnedBranch("feat/ACME-7", task) {
		t.Error("feat/ACME-7 should be task-owned")
	}
	if !isTaskOwnedBranch("acme-7", task) {
		t.Error("acme-7 should be task-owned")
	}
	if isTaskOwnedBranch("develop", task) {
		t.Error("develop should not be task-owned")
	}
}

func TestOtherTasksShareBranch(t *testing.T) {
	tasks := []domain.Task{
		{ID: "task-a", TargetBranch: "feat/ACME-7", WorktreePath: "/tmp/wt-a"},
		{ID: "task-b", TargetBranch: "feat/ACME-7", WorktreePath: "/tmp/wt-a"},
		{ID: "task-c", TargetBranch: "feat/OTHER-1", WorktreePath: "/tmp/wt-c"},
	}

	if !otherTasksShareBranch(tasks, "task-a", "feat/ACME-7", "/tmp/wt-a") {
		t.Error("expected shared branch/path with task-b")
	}
	if !otherTasksShareBranch(tasks, "task-b", "feat/ACME-7", "/tmp/wt-a") {
		t.Error("task-a should share branch/path with task-b when task-b is excluded")
	}
	if otherTasksShareBranch(tasks, "task-a", "feat/UNIQUE", "/tmp/wt-unique") {
		t.Error("no other task should share an exclusive branch/path")
	}
	if !otherTasksShareBranch(tasks, "task-a", "feat/OTHER-1", "/tmp/wt-c") {
		t.Error("task-c shares worktree path /tmp/wt-c")
	}
}

func TestDeleteTaskGitCleanup(t *testing.T) {
	repoPath := t.TempDir()
	runGitTest(t, repoPath, "init")
	runGitTest(t, repoPath, "commit", "--allow-empty", "-m", "init")

	branch := "feat/TEST-1"
	worktreePath, err := createWorktree(repoPath, branch)
	if err != nil {
		t.Fatalf("createWorktree: %v", err)
	}
	if _, err := os.Stat(worktreePath); err != nil {
		t.Fatalf("worktree path missing: %v", err)
	}
	if !branchExists(repoPath, branch) {
		t.Fatal("branch should exist before cleanup")
	}

	task := domain.Task{
		ID:           "task-1",
		DisplayID:    "TEST-1",
		TargetBranch: branch,
		WorktreePath: worktreePath,
	}

	msg := deleteTaskCmd(task, repoPath, []domain.Task{task})()
	finished, ok := msg.(deleteFinishedMsg)
	if !ok {
		t.Fatalf("unexpected msg type %T", msg)
	}
	if finished.err != nil {
		t.Fatalf("deleteTaskCmd: %v", finished.err)
	}

	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Errorf("worktree path still exists: %v", err)
	}
	if branchExists(repoPath, branch) {
		t.Error("branch should be deleted after cleanup")
	}
}

func runGitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func TestDeleteTaskGitCleanupSkipsUserBranch(t *testing.T) {
	repoPath := t.TempDir()
	runGitTest(t, repoPath, "init")
	runGitTest(t, repoPath, "commit", "--allow-empty", "-m", "init")

	branch := "develop"
	worktreePath, err := createWorktree(repoPath, branch)
	if err != nil {
		t.Fatalf("createWorktree: %v", err)
	}

	task := domain.Task{
		ID:           "task-1",
		DisplayID:    "TEST-1",
		TargetBranch: branch,
		WorktreePath: worktreePath,
	}

	msg := deleteTaskCmd(task, repoPath, []domain.Task{task})()
	finished := msg.(deleteFinishedMsg)
	if finished.err != nil {
		t.Fatalf("deleteTaskCmd: %v", finished.err)
	}

	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Errorf("worktree path should be removed: %v", err)
	}
	if !branchExists(repoPath, branch) {
		t.Error("user-picked branch develop should remain")
	}
}
