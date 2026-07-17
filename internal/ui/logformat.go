package ui

import (
	"encoding/json"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"cormake/internal/agent"
)

// Log line styles, one per kind of thing that shows up in a task's Log tab.
// Colors follow the same 256-color palette already used elsewhere in this
// package (212 pink accent, 245/240 muted grays) so the log doesn't look
// like it belongs to a different app.
var (
	logMetaStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	logAssistantStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	logSubagentStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true)
	logToolStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("111"))
	logToolResultStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	logResultStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	logErrorStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
	logCormakeStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
)

// Long tool_result blobs (a full file's contents, a huge `ls -la`, ...)
// would otherwise flood the log with more text than anyone reads; these cap
// how much of one shows before it's cut off with a marker.
const (
	maxToolResultLines = 12
	maxToolResultChars = 1200
)

// formatAgentLogLine turns a raw agent.Event into the styled, human-readable
// text appended to a task's Log tab. Kept separate from handleAgentEvent's
// side effects (capturing PlanFilePath, ResultSummary, WorktreePath, ...) —
// this only decides how the event reads, not what it means.
func formatAgentLogLine(ev agent.Event) string {
	switch ev.Type {
	case agent.EventInit:
		return logMetaStyle.Render("▸ session started — model " + ev.Text)

	case agent.EventText:
		style, label := logAssistantStyle, "● claude"
		if ev.IsSubagent {
			style, label = logSubagentStyle, "◦ subagent"
		}
		// A leading blank line visually separates each new turn from
		// whatever tool activity preceded it, instead of one unbroken wall
		// of text.
		return "\n" + style.Render(label+"  "+ev.Text)

	case agent.EventToolUse:
		return logToolStyle.Render("  ⚙ " + describeToolUse(ev.ToolName, ev.ToolInput))

	case agent.EventToolResult:
		body := truncateBlock(ev.Text, maxToolResultLines, maxToolResultChars)
		return logToolResultStyle.Render(indentBlock("    ", body))

	case agent.EventResult:
		return "\n" + logResultStyle.Render("✔ "+ev.ResultText)

	case agent.EventStderrLine:
		return logErrorStyle.Render("  ⚠ " + ev.Text)

	case agent.EventProcessError:
		return logErrorStyle.Render("✖ " + ev.Err.Error())

	default:
		return ev.Text
	}
}

// logCormakeLine styles one of cormake's own status lines (distinct from
// anything claude said) in the same accent color used for the active tab
// elsewhere in the app, so it reads as "the app talking" rather than another
// tool_result.
func logCormakeLine(msg string) string {
	return logCormakeStyle.Render("cormake: " + msg)
}

// describeToolUse renders a tool_use call the way a human would want to
// read it — the command actually run, the file actually touched — instead
// of its raw JSON input. Falls back to the tool name plus a trimmed
// one-line version of the JSON for anything not specifically handled.
func describeToolUse(name, inputJSON string) string {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(inputJSON), &raw); err != nil {
		return name
	}
	str := func(key string) (string, bool) {
		v, ok := raw[key]
		if !ok {
			return "", false
		}
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			return "", false
		}
		return s, true
	}

	switch name {
	case "Bash":
		if cmd, ok := str("command"); ok {
			return "$ " + oneLine(cmd)
		}
	case "Read", "Write", "Edit", "NotebookEdit":
		if fp, ok := str("file_path"); ok {
			return name + " " + fp
		}
	case "Glob":
		if pattern, ok := str("pattern"); ok {
			return "Glob " + pattern
		}
	case "Grep":
		if pattern, ok := str("pattern"); ok {
			return "Grep " + pattern
		}
	case "WebFetch":
		if url, ok := str("url"); ok {
			return "Fetch " + url
		}
	case "WebSearch":
		if q, ok := str("query"); ok {
			return "Search " + q
		}
	case "Task":
		if desc, ok := str("description"); ok {
			return "Task: " + desc
		}
	}
	return name + " " + oneLine(inputJSON)
}

// oneLine collapses a string to a single line (in case a raw JSON blob or
// command string has embedded newlines) and caps its length — a fallback
// display, not meant to be read in full.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const max = 160
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

// truncateBlock caps a multi-line blob to at most maxLines lines and
// maxChars characters, whichever comes first, marking the result as cut off
// when either limit was hit.
func truncateBlock(s string, maxLines, maxChars int) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return "(empty)"
	}

	lines := strings.Split(s, "\n")
	cut := false
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		cut = true
	}
	out := strings.Join(lines, "\n")
	if len(out) > maxChars {
		out = out[:maxChars]
		cut = true
	}
	if cut {
		out += "\n… (truncated)"
	}
	return out
}

// indentBlock prefixes every line of s with prefix, for nesting a
// tool_result visually under the tool_use line that produced it.
func indentBlock(prefix, s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}
