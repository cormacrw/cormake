package claude

import "cormake/internal/agent"

// BuildArgs turns a RunSpec into claude CLI arguments. Verified directly
// against real `claude -p --output-format stream-json` output (see
// translate.go) rather than assumed from docs alone.
func BuildArgs(spec agent.RunSpec) []string {
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--include-partial-messages",
		"--verbose",
	}
	// ResumeSessionID takes precedence: continuing an existing conversation
	// and starting a brand new one with a specific ID are mutually exclusive.
	if spec.ResumeSessionID != "" {
		args = append(args, "--resume", spec.ResumeSessionID)
	} else if spec.SessionID != "" {
		args = append(args, "--session-id", spec.SessionID)
	}
	if spec.SettingsPath != "" {
		args = append(args, "--settings", spec.SettingsPath)
	}
	switch spec.Mode {
	case agent.RunModePlan:
		args = append(args, "--permission-mode", "plan")
	case agent.RunModeComplete:
		// bypassPermissions is required for a headless (-p) run to make any
		// real progress: any tool needing a permission prompt would just
		// hang forever otherwise, since there's no TTY to answer it (verified
		// directly — acceptEdits alone stalls the first time it needs Bash).
		// That's made acceptable by RepoPath pointing at a disposable
		// worktree rather than the actual checkout — created by cormake
		// itself (see createWorktree), not via claude's own -w/--worktree:
		// confirmed directly that -w forks from the repo's remote-tracking
		// branch instead of local HEAD whenever one is configured.
		args = append(args, "--permission-mode", "bypassPermissions")
	}
	return append(args, spec.Prompt)
}
