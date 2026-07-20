package logformat

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"cormake/internal/agent"
	"cormake/internal/domain"
)

func TestParsePersistedLogSplitsLegacyEntries(t *testing.T) {
	text := strings.Join([]string{
		"cormake: starting",
		"▸ session started — model sonnet",
		"  ⚙ Read internal/foo.go",
		"    line one\n    line two",
		"\n● claude  hello there",
	}, "\n")

	got := ParsePersistedLog(text)
	if len(got) != 5 {
		t.Fatalf("got %d entries, want 5: %#v", len(got), got)
	}
	if got[3] != "    line one\n    line two" {
		t.Fatalf("tool result entry = %q", got[3])
	}
}

func TestParsePersistedLogUsesRecordSeparator(t *testing.T) {
	text := "cormake: one" + LogRecordSep + "  ⚙ tool" + LogRecordSep + "    body"
	got := ParsePersistedLog(text)
	if len(got) != 3 {
		t.Fatalf("got %d entries, want 3: %#v", len(got), got)
	}
}

func TestRenderLogLineAppliesDistinctStyles(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)

	tests := []struct {
		line       string
		colorToken string
	}{
		{LogCormakeLine("starting"), "38;5;212"},
		{FormatAgentLogLine(agent.Event{Type: agent.EventInit, Text: "sonnet"}, domain.AgentBackendClaude), "38;5;240"},
		{FormatAgentLogLine(agent.Event{Type: agent.EventText, Text: "hello"}, domain.AgentBackendClaude), "38;5;255"},
		{FormatAgentLogLine(agent.Event{Type: agent.EventToolUse, ToolName: "Read", ToolInput: `{"file_path":"foo.go"}`}, domain.AgentBackendClaude), "38;5;111"},
		{FormatAgentLogLine(agent.Event{Type: agent.EventResult, ResultText: "done"}, domain.AgentBackendClaude), "38;5;42"},
	}

	for _, tt := range tests {
		out := RenderLogLine(tt.line, 0)
		if !strings.Contains(out, tt.colorToken) {
			t.Fatalf("line %q rendered without expected color %q: %q", tt.line, tt.colorToken, out)
		}
	}
}

func TestRenderLogLineKeepsStyleOnWrappedLines(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)

	plain := FormatAgentLogLine(agent.Event{
		Type: agent.EventText,
		Text: strings.Repeat("word ", 30),
	}, domain.AgentBackendClaude)

	out := RenderLogLine(plain, 24)
	for i, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.Contains(line, "38;5;255") {
			t.Fatalf("wrapped line %d lost assistant color: %q", i, ansi.Strip(line))
		}
	}
}
