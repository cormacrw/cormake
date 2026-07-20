package cursor

import (
	"testing"

	"cormake/internal/agent"
)

// Lines below are the exact stream-json shapes captured by running
// `cursor-agent -p --output-format stream-json --stream-partial-output
// --trust "<prompt>"` directly (see this package's doc comments) — not
// invented from docs.

func TestTranslateLineInit(t *testing.T) {
	line := `{"type":"system","subtype":"init","session_id":"sess-1","model":"gpt-5","cwd":"/tmp/worktree"}`
	events := translateLine("task-1", []byte(line))
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	ev := events[0]
	if ev.Type != agent.EventInit {
		t.Errorf("Type = %v, want EventInit", ev.Type)
	}
	if ev.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want %q", ev.SessionID, "sess-1")
	}
	if ev.Text != "gpt-5" {
		t.Errorf("Text (model) = %q, want %q", ev.Text, "gpt-5")
	}
	if ev.Cwd != "/tmp/worktree" {
		t.Errorf("Cwd = %q, want %q", ev.Cwd, "/tmp/worktree")
	}
}

func TestTranslateLineAssistantText(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"hello there"}]}}`
	events := translateLine("task-1", []byte(line))
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	ev := events[0]
	if ev.Type != agent.EventText {
		t.Errorf("Type = %v, want EventText", ev.Type)
	}
	if ev.Text != "hello there" {
		t.Errorf("Text = %q, want %q", ev.Text, "hello there")
	}
	if ev.IsSubagent {
		t.Error("IsSubagent = true, want false (cursor never sets parent_tool_use_id)")
	}
}

func TestTranslateLineToolCallStarted(t *testing.T) {
	line := `{"type":"tool_call","subtype":"started","call_id":"call-1","tool_call":{"shellToolCall":{"args":{"command":"ls -la"}}}}`
	events := translateLine("task-1", []byte(line))
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	ev := events[0]
	if ev.Type != agent.EventToolUse {
		t.Errorf("Type = %v, want EventToolUse", ev.Type)
	}
	if ev.ToolName != "shellToolCall" {
		t.Errorf("ToolName = %q, want %q", ev.ToolName, "shellToolCall")
	}
	if ev.ToolInput != `{"command":"ls -la"}` {
		t.Errorf("ToolInput = %q, want %q", ev.ToolInput, `{"command":"ls -la"}`)
	}
}

func TestTranslateLineToolCallCompleted(t *testing.T) {
	line := `{"type":"tool_call","subtype":"completed","call_id":"call-1","tool_call":{"shellToolCall":{"args":{"command":"ls -la"},"result":{"success":{"stdout":"file1\nfile2","exitCode":0}}}}}`
	events := translateLine("task-1", []byte(line))
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	ev := events[0]
	if ev.Type != agent.EventToolResult {
		t.Errorf("Type = %v, want EventToolResult", ev.Type)
	}
	want := `{"success":{"stdout":"file1\nfile2","exitCode":0}}`
	if ev.Text != want {
		t.Errorf("Text = %q, want %q", ev.Text, want)
	}
}

func TestTranslateLineCreatePlanToolCallCompleted(t *testing.T) {
	line := `{"type":"tool_call","subtype":"completed","call_id":"call-1","tool_call":{"createPlanToolCall":{"args":{"plan":"# Revised\n"},"result":{"success":{},"planUri":""}}}}`
	events := translateLine("task-1", []byte(line))
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[0].Type != agent.EventToolUse || events[0].ToolName != "createPlanToolCall" {
		t.Fatalf("first event = %+v, want createPlanToolCall tool_use", events[0])
	}
	if events[0].ToolInput != `{"plan":"# Revised\n"}` {
		t.Errorf("ToolInput = %q, want plan args", events[0].ToolInput)
	}
	if events[1].Type != agent.EventToolResult {
		t.Errorf("second event Type = %v, want EventToolResult", events[1].Type)
	}
}

func TestTranslateLineThinkingDropped(t *testing.T) {
	for _, line := range []string{
		`{"type":"thinking","subtype":"delta","text":"reasoning..."}`,
		`{"type":"thinking","subtype":"completed","text":"reasoning done"}`,
	} {
		if events := translateLine("task-1", []byte(line)); events != nil {
			t.Errorf("translateLine(%q) = %v, want nil (thinking is dropped)", line, events)
		}
	}
}

func TestTranslateLineResult(t *testing.T) {
	line := `{"type":"result","subtype":"success","result":"done","session_id":"sess-1","is_error":false,"usage":{"inputTokens":100,"outputTokens":50,"cacheReadTokens":10,"cacheWriteTokens":5}}`
	events := translateLine("task-1", []byte(line))
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	ev := events[0]
	if ev.Type != agent.EventResult {
		t.Errorf("Type = %v, want EventResult", ev.Type)
	}
	if ev.ResultText != "done" {
		t.Errorf("ResultText = %q, want %q", ev.ResultText, "done")
	}
	if ev.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want %q", ev.SessionID, "sess-1")
	}
	if ev.CostUSD != 0 {
		t.Errorf("CostUSD = %v, want 0 (cursor-agent has no total_cost_usd field)", ev.CostUSD)
	}
	if ev.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", ev.InputTokens)
	}
	if ev.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", ev.OutputTokens)
	}
	if ev.CacheReadInputTokens != 10 {
		t.Errorf("CacheReadInputTokens = %d, want 10", ev.CacheReadInputTokens)
	}
	if ev.CacheCreationInputTokens != 5 {
		t.Errorf("CacheCreationInputTokens = %d, want 5", ev.CacheCreationInputTokens)
	}
}

func TestTranslateLineUnrecognizedDropped(t *testing.T) {
	line := `{"type":"stream_event","subtype":"whatever"}`
	if events := translateLine("task-1", []byte(line)); events != nil {
		t.Errorf("translateLine(%q) = %v, want nil", line, events)
	}
}
