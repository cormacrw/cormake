package domain

import (
	"fmt"
	"strings"
)

// fallbackPrefix is used when a workspace has neither an explicit Prefix
// nor a Name that yields any usable characters.
const fallbackPrefix = "TASK"

// maxPrefixLen caps a sanitized prefix, keeping generated worktree/branch
// names reasonably short regardless of how long a workspace name is.
const maxPrefixLen = 10

// sanitizePrefix normalizes a raw prefix (whether hand-typed or derived
// from a workspace name) into something safe to use in a git branch/
// directory name: uppercase letters and digits only, capped at
// maxPrefixLen.
func sanitizePrefix(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		}
		if b.Len() >= maxPrefixLen {
			break
		}
	}
	return b.String()
}

// effectivePrefix returns the prefix NextDisplayID should use: w.Prefix if
// it sanitizes to something usable, else one derived from w.Name, else
// fallbackPrefix.
func (w *Workspace) effectivePrefix() string {
	if p := sanitizePrefix(w.Prefix); p != "" {
		return p
	}
	if p := sanitizePrefix(w.Name); p != "" {
		return p
	}
	return fallbackPrefix
}

// NextDisplayID returns this workspace's next readable task id
// (PREFIX-N) and advances NextTaskNumber. Falls back to a prefix derived
// from Name if Prefix is unset, and to "TASK" if that's also empty.
func (w *Workspace) NextDisplayID() string {
	w.NextTaskNumber++
	return fmt.Sprintf("%s-%d", w.effectivePrefix(), w.NextTaskNumber)
}

// PeekNextDisplayID returns what NextDisplayID would return, without
// advancing NextTaskNumber — used to preview a not-yet-created task's id
// (e.g. suggesting its default branch name in the new-task wizard, before
// the task itself, and its real id, exist).
func (w Workspace) PeekNextDisplayID() string {
	return fmt.Sprintf("%s-%d", w.effectivePrefix(), w.NextTaskNumber+1)
}
