package domain

import "time"

type Repo struct {
	ID      string
	Name    string
	Path    string
	AddedAt time.Time
}

type Workspace struct {
	ID           string
	Name         string
	Repos        []Repo
	PrimaryColor string

	// Prefix is the user-set readable-ID prefix for this workspace's tasks,
	// e.g. "ACME" -> task IDs like "ACME-7". Hand-edited via workspaces.json
	// (there's no dedicated settings UI yet, same as PrimaryColor). When
	// unset, NextDisplayID derives one from Name instead.
	Prefix string

	// NextTaskNumber is the next sequence number NextDisplayID will hand
	// out for Prefix. It does not reset when Prefix is edited by hand.
	NextTaskNumber int

	// MaxConcurrentAgents caps how many Plan/Execute agent runs may be
	// active at once across this workspace's tasks. Hand-edited via
	// workspaces.json, same as PrimaryColor/Prefix (no dedicated settings
	// UI yet). Zero means "unset", not "unlimited" — see
	// EffectiveMaxConcurrentAgents — so existing workspaces.json files
	// written before this field existed stay capped at the low default
	// instead of suddenly allowing unbounded concurrent agent processes.
	MaxConcurrentAgents int

	// TaskTemplate names a markdown file, stored alongside workspaces.json
	// in the store directory, whose contents prefill the Description of
	// every new task created in this workspace (see Model.createTask).
	// Hand-edited via workspaces.json, same as PrimaryColor/Prefix — empty
	// means no template.
	TaskTemplate string

	// DefaultTargetBranch is the branch a new task's work should merge
	// into by default — named "target" to match how it's presented in the
	// new-task wizard (a PR's target branch), even though it fills
	// Task.SourceBranch, this codebase's name for that same concept (see
	// Task.SourceBranch's doc comment). Hand-edited via workspaces.json,
	// same as TaskTemplate/Prefix — empty means unset, see
	// EffectiveDefaultTargetBranch.
	DefaultTargetBranch string

	// PRSkill, when set, names a Claude Code skill that opening a PR (see
	// ui.buildOpenPRPrompt) should be told to use for writing the PR
	// description, taking precedence over the repo's own pull request
	// template — useful when a repo's skills already encode richer
	// PR-writing conventions than a static template can. Hand-edited via
	// workspaces.json, same as TaskTemplate/Prefix — empty means unset,
	// falling back to the repo's template.
	PRSkill string

	// DefaultAgentBackend is which agent CLI (see AgentBackend) Plan/Execute
	// runs in this workspace use by default, overridable per-run from the
	// confirmation modal. Hand-edited via workspaces.json, same as
	// TaskTemplate/Prefix — empty means unset, see
	// EffectiveDefaultAgentBackend.
	DefaultAgentBackend AgentBackend

	CreatedAt time.Time
	UpdatedAt time.Time
}

// AgentBackend selects which coding-agent CLI a Plan/Execute run actually
// shells out to (see agent.Runner) — claude and cursor-agent today.
type AgentBackend string

const (
	AgentBackendClaude AgentBackend = "claude"
	AgentBackendCursor AgentBackend = "cursor"
)

// DefaultMaxConcurrentAgents is the cap applied when MaxConcurrentAgents is
// unset. Kept low by default since each running agent is a full claude
// subprocess plus its own git worktree.
const DefaultMaxConcurrentAgents = 1

// EffectiveMaxConcurrentAgents returns the cap on simultaneously running
// agents for this workspace: MaxConcurrentAgents if set to a positive
// value, else DefaultMaxConcurrentAgents.
func (w Workspace) EffectiveMaxConcurrentAgents() int {
	if w.MaxConcurrentAgents > 0 {
		return w.MaxConcurrentAgents
	}
	return DefaultMaxConcurrentAgents
}

// EffectiveDefaultTargetBranch returns the branch a new task's source
// branch should default to in the new-task wizard: DefaultTargetBranch if
// set, else DefaultSourceBranch.
func (w Workspace) EffectiveDefaultTargetBranch() string {
	if w.DefaultTargetBranch != "" {
		return w.DefaultTargetBranch
	}
	return DefaultSourceBranch
}

// EffectiveDefaultAgentBackend returns the agent backend a fresh Plan/Execute
// run in this workspace should default to: DefaultAgentBackend if set, else
// AgentBackendClaude — so workspaces.json files written before this field
// existed keep behaving exactly as before.
func (w Workspace) EffectiveDefaultAgentBackend() AgentBackend {
	if w.DefaultAgentBackend != "" {
		return w.DefaultAgentBackend
	}
	return AgentBackendClaude
}
