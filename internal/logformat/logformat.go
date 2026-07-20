package logformat

import (
	"encoding/json"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"cormake/internal/agent"
	"cormake/internal/domain"
	"cormake/internal/ui/theme"
)

// Log line styles, one per kind of thing that shows up in a task's Log tab.
// Colors follow the same 256-color palette already used elsewhere in the UI
// (212 pink accent, 245/240 muted grays) so the log doesn't look like it
// belongs to a different app.
var (
	logMetaStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	logAssistantStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	logSubagentStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true)
	logToolStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("111"))
	logToolResultStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	logResultStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	logErrorStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
)

func logCormakeStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Accent())
}

// AgentBackendLabel returns the human-facing name for backend — "claude" or
// "cursor" — used everywhere the UI needs to say which agent it's talking
// to (log lines, the input modal, its textarea placeholder). Empty/
// unrecognized values read as "claude", the same fallback convention as
// Model.runnerFor/Workspace.EffectiveDefaultAgentBackend.
func AgentBackendLabel(backend domain.AgentBackend) string {
	if backend == domain.AgentBackendCursor {
		return "cursor"
	}
	return "claude"
}

// Long tool_result blobs (a full file's contents, a huge `ls -la`, ...)
// would otherwise flood the log with more text than anyone reads; these cap
// how much of one shows before it's cut off with a marker.
const (
	maxToolResultLines = 12
	maxToolResultChars = 1200
)

// FormatAgentLogLine turns a raw agent.Event into the plain, human-readable
// text appended to a task's Log tab. Styling and width wrapping happen at
// render time via RenderLogLine so wrapped continuation lines keep their
// color. backend picks the "● claude"/"● cursor" label on assistant text
// lines — it's the task's own AgentBackend, not derivable from ev itself
// (agent.Event is deliberately backend-neutral).
func FormatAgentLogLine(ev agent.Event, backend domain.AgentBackend) string {
	switch ev.Type {
	case agent.EventInit:
		return "▸ session started — model " + ev.Text

	case agent.EventText:
		label := "● " + AgentBackendLabel(backend)
		if ev.IsSubagent {
			label = "◦ subagent"
		}
		// A leading blank line visually separates each new turn from
		// whatever tool activity preceded it, instead of one unbroken wall
		// of text.
		return "\n" + label + "  " + ev.Text

	case agent.EventToolUse:
		return "  ⚙ " + describeToolUse(ev.ToolName, ev.ToolInput)

	case agent.EventToolResult:
		body := truncateBlock(ev.Text, maxToolResultLines, maxToolResultChars)
		return indentBlock("    ", body)

	case agent.EventResult:
		return "\n" + "✔ " + ev.ResultText

	case agent.EventStderrLine:
		return "  ⚠ " + ev.Text

	case agent.EventProcessError:
		return "✖ " + ev.Err.Error()

	default:
		return ev.Text
	}
}

// LogCormakeLine returns one of cormake's own status lines (distinct from
// anything claude said). Styling is applied at render time via RenderLogLine.
func LogCormakeLine(msg string) string {
	return "cormake: " + msg
}

// LogRecordSep delimits persisted log entries. Each AppendLogLine write ends
// with this byte so LoadLog can split entries even when an entry itself
// contains embedded newlines (multi-line tool results).
const LogRecordSep = "\x1e"

// ParsePersistedLog splits one persisted log blob into individual entries.
// New-format files use LogRecordSep; older files used bare newlines between
// entries and are split heuristically from their visible text. Legacy entries
// that were saved with inline ANSI codes are stripped before splitting.
func ParsePersistedLog(text string) []string {
	if text == "" {
		return nil
	}
	if strings.Contains(text, LogRecordSep) {
		return filterEmpty(strings.Split(text, LogRecordSep))
	}
	plain := text
	if strings.Contains(text, "\x1b") || strings.Contains(text, "\x9b") {
		plain = ansi.Strip(text)
	}
	return splitLogEntries(plain)
}

// ExpandLogLines flattens a task's in-memory log slice into one element per
// logical entry. Slice elements loaded before entry splitting was added may
// still be whole-file blobs; live appends are already one entry each.
func ExpandLogLines(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, ParsePersistedLog(line)...)
	}
	return out
}

func splitLogEntries(text string) []string {
	if text == "" {
		return nil
	}
	if !strings.Contains(text, "\n") {
		return []string{text}
	}

	lines := strings.Split(text, "\n")
	entries := make([]string, 0, len(lines))
	var cur strings.Builder

	flush := func() {
		if cur.Len() == 0 {
			return
		}
		entries = append(entries, cur.String())
		cur.Reset()
	}

	for _, line := range lines {
		if line == "" {
			if cur.Len() > 0 {
				flush()
			}
			continue
		}
		if cur.Len() == 0 {
			cur.WriteString(line)
			continue
		}
		if isNewLogEntryLine(line, cur.String()) {
			flush()
			cur.WriteString(line)
			continue
		}
		cur.WriteByte('\n')
		cur.WriteString(line)
	}
	flush()
	return entries
}

func isNewLogEntryLine(line, current string) bool {
	if strings.HasPrefix(line, "    ") {
		return !strings.HasPrefix(strings.TrimLeft(current, "\n"), "    ")
	}
	return strings.HasPrefix(line, "cormake: ") ||
		strings.HasPrefix(line, "▸ session started") ||
		strings.HasPrefix(line, "  ⚙ ") ||
		strings.HasPrefix(line, "  ⚠ ") ||
		strings.HasPrefix(line, "✖ ") ||
		strings.HasPrefix(line, "✔ ") ||
		strings.HasPrefix(line, "● ") ||
		strings.HasPrefix(line, "◦ subagent")
}

func filterEmpty(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// RenderLogLine styles and optionally word-wraps one stored log entry for
// display. Lines are stored as plain text; older persisted logs that still
// contain ANSI codes are stripped and re-styled from their visible prefix.
func RenderLogLine(line string, width int) string {
	plain := line
	if strings.Contains(line, "\x1b") || strings.Contains(line, "\x9b") {
		plain = ansi.Strip(line)
	}
	style := styleForLine(plain)
	if width > 0 {
		return style.Width(width).Render(plain)
	}
	return style.Render(plain)
}

func styleForLine(plain string) lipgloss.Style {
	body := strings.TrimLeft(plain, "\n")
	switch {
	case strings.HasPrefix(body, "cormake: "):
		return logCormakeStyle()
	case strings.HasPrefix(body, "✖ "):
		return logErrorStyle
	case strings.HasPrefix(body, "✔ "):
		return logResultStyle
	case strings.HasPrefix(body, "▸ session started"):
		return logMetaStyle
	case strings.HasPrefix(body, "◦ subagent"):
		return logSubagentStyle
	case strings.HasPrefix(body, "● "):
		return logAssistantStyle
	case strings.HasPrefix(body, "  ⚙ "):
		return logToolStyle
	case strings.HasPrefix(body, "  ⚠ "):
		return logErrorStyle
	case strings.HasPrefix(body, "    "):
		return logToolResultStyle
	default:
		return lipgloss.NewStyle()
	}
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

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const max = 160
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

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

func indentBlock(prefix, s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}
