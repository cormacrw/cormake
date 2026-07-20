package cursor

import "cormake/internal/agent"

// BuildArgs turns a RunSpec into cursor-agent CLI arguments. Verified
// directly against real `cursor-agent -p --output-format stream-json
// --stream-partial-output --trust` output (see translate.go) rather than
// assumed from docs alone.
func BuildArgs(spec agent.RunSpec) []string {
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--stream-partial-output",
		// A directory must be explicitly trusted for headless use, or the
		// process just prints a trust prompt and produces no stream-json at
		// all (confirmed directly). Safe unconditionally since RepoPath
		// always points at a disposable cormake-created worktree, never the
		// user's real checkout.
		"--trust",
	}
	// Unlike claude, cursor-agent has no "start a new session with this
	// specific ID" flag — a fresh run just gets whatever session id it
	// generates, read back off the init event (see translate.go). Only a
	// resume names an existing session explicitly.
	if spec.ResumeSessionID != "" {
		args = append(args, "--resume", spec.ResumeSessionID)
	}
	// SettingsPath has no cursor-agent equivalent (no hook-server concept)
	// — ignored for this backend.
	switch spec.Mode {
	case agent.RunModePlan:
		args = append(args, "--mode", "plan")
	case agent.RunModeComplete:
		// --force is cursor-agent's bypass-permissions equivalent, required
		// for a headless run to make any real progress (see claude's
		// identical bypassPermissions rationale) — made acceptable by
		// RepoPath pointing at a disposable worktree, not the actual
		// checkout.
		args = append(args, "--force")
	}
	return append(args, spec.Prompt)
}
