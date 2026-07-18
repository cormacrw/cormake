package claude

import (
	"encoding/json"

	"cormake/internal/agent"
)

// Raw shapes below match real `claude -p --output-format stream-json`
// output, confirmed by running it directly rather than assumed from docs —
// notably total_cost_usd (not cost_usd), and parent_tool_use_id as a
// top-level sibling of "message", not nested inside it.

type rawEnvelope struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
}

type rawSystemInit struct {
	SessionID string `json:"session_id"`
	Model     string `json:"model"`
	Cwd       string `json:"cwd"`
}

type rawMessageLine struct {
	Message struct {
		Role    string            `json:"role"`
		Content []rawContentBlock `json:"content"`
	} `json:"message"`
	ParentToolUseID *string `json:"parent_tool_use_id"`
}

// rawContentBlock covers "text", "tool_use", and "tool_result" blocks.
// tool_result's Content is a json.RawMessage because it's sometimes a bare
// string and sometimes an array of blocks (e.g. tool_reference results) —
// confirmed both forms in real output.
type rawContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
}

type rawResult struct {
	Result       string  `json:"result"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	SessionID    string  `json:"session_id"`
	IsError      bool    `json:"is_error"`
	Usage        struct {
		InputTokens              int64 `json:"input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

// translateLine parses one stream-json line into zero or more
// backend-neutral events (a single line can carry multiple content
// blocks). Unrecognized lines (stream_event, rate_limit_event, system
// subtypes other than init, ...) are silently dropped rather than treated
// as errors — the wire format has more going on than this POC needs to
// track.
func translateLine(taskID string, line []byte) []agent.Event {
	var env rawEnvelope
	if err := json.Unmarshal(line, &env); err != nil {
		return nil
	}

	switch env.Type {
	case "system":
		if env.Subtype != "init" {
			return nil
		}
		var init rawSystemInit
		if err := json.Unmarshal(line, &init); err != nil {
			return nil
		}
		return []agent.Event{{
			TaskID:    taskID,
			Type:      agent.EventInit,
			SessionID: init.SessionID,
			Text:      init.Model,
			Cwd:       init.Cwd,
		}}

	case "assistant", "user":
		var msg rawMessageLine
		if err := json.Unmarshal(line, &msg); err != nil {
			return nil
		}
		isSubagent := msg.ParentToolUseID != nil

		var events []agent.Event
		for _, block := range msg.Message.Content {
			switch block.Type {
			case "text":
				events = append(events, agent.Event{
					TaskID: taskID, Type: agent.EventText,
					Text: block.Text, IsSubagent: isSubagent,
				})
			case "tool_use":
				events = append(events, agent.Event{
					TaskID: taskID, Type: agent.EventToolUse,
					ToolName: block.Name, ToolInput: string(block.Input), IsSubagent: isSubagent,
				})
			case "tool_result":
				events = append(events, agent.Event{
					TaskID: taskID, Type: agent.EventToolResult,
					Text: contentToString(block.Content), IsSubagent: isSubagent,
				})
				// "thinking" blocks exist too but aren't translated yet —
				// no UI surface for them at this stage.
			}
		}
		return events

	case "result":
		var res rawResult
		if err := json.Unmarshal(line, &res); err != nil {
			return nil
		}
		return []agent.Event{{
			TaskID: taskID, Type: agent.EventResult,
			ResultText: res.Result, CostUSD: res.TotalCostUSD, SessionID: res.SessionID,
			InputTokens:              res.Usage.InputTokens,
			OutputTokens:             res.Usage.OutputTokens,
			CacheReadInputTokens:     res.Usage.CacheReadInputTokens,
			CacheCreationInputTokens: res.Usage.CacheCreationInputTokens,
		}}

	default:
		return nil
	}
}

// contentToString handles tool_result's content being either a bare JSON
// string or something else (e.g. an array of blocks) — good enough for
// display in a POC; not trying to render structured content specially yet.
func contentToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}
