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

	CreatedAt time.Time
	UpdatedAt time.Time
}

// DefaultMaxConcurrentAgents is the cap applied when MaxConcurrentAgents is
// unset. Kept low by default since each running agent is a full claude
// subprocess plus, for Execute runs, its own git worktree.
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
