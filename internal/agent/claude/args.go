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
		if spec.WorktreeName != "" {
			args = append(args, "-w", spec.WorktreeName)
		}
	}
	return append(args, spec.Prompt)
}
