// Package theme holds the currently active accent color, shared mutable
// state so ui, ui/tasklist, and ui/detail can all read the active
// workspace's accent without needing direct access to the app Model.
package theme

import "github.com/charmbracelet/lipgloss"

// DefaultAccent is the original hardcoded pink accent, now the fallback for
// workspaces with no PrimaryColor set.
const DefaultAccent = "212"

var accent = DefaultAccent

// SetAccent sets the active accent color; an empty string resets to
// DefaultAccent.
func SetAccent(c string) {
	if c == "" {
		c = DefaultAccent
	}
	accent = c
}

// Accent returns the currently active accent color.
func Accent() lipgloss.Color {
	return lipgloss.Color(accent)
}
