package cursor

import (
	"encoding/json"

	"cormake/internal/agent"
)

// Raw shapes below match real `cursor-agent -p --output-format stream-json
// --stream-partial-output --trust` output, confirmed by running it directly
// rather than assumed from docs — notably tool calls are a separate event
// type entirely ("tool_call"), not content blocks the way claude's are, and
// there is no parent_tool_use_id field anywhere, so subagent detection
// doesn't apply to this backend (Event.IsSubagent is always left false).

type rawEnvelope struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
}

type rawSystemInit struct {
	SessionID string `json:"session_id"`
	Model     string `json:"model"`
	Cwd       string `json:"cwd"`
}

type rawAssistantMessage struct {
	Message struct {
		Content []rawContentBlock `json:"content"`
	} `json:"message"`
}

type rawContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// rawToolCallEnvelope's ToolCall has exactly one key — the tool kind (e.g.
// "shellToolCall") — rather than a flat "name" field, matched across
// started/completed via CallID.
type rawToolCallEnvelope struct {
	CallID   string                     `json:"call_id"`
	ToolCall map[string]json.RawMessage `json:"tool_call"`
}

// rawToolCallDetail is the value under tool_call's single dynamic key.
// Result is only present once the call reaches "completed"; its shape
// varies per tool kind (e.g. shellToolCall's is
// result.success.{stdout,exitCode}, presumably a result.error variant on
// failure) — treated generically as opaque JSON rather than special-cased
// per tool kind, matching claude's own translator's stated bar of "not
// trying to render structured content specially yet".
type rawToolCallDetail struct {
	Args   json.RawMessage `json:"args"`
	Result json.RawMessage `json:"result"`
}

type rawResult struct {
	Result    string `json:"result"`
	SessionID string `json:"session_id"`
	IsError   bool   `json:"is_error"`
	Usage     struct {
		InputTokens      int64 `json:"inputTokens"`
		OutputTokens     int64 `json:"outputTokens"`
		CacheReadTokens  int64 `json:"cacheReadTokens"`
		CacheWriteTokens int64 `json:"cacheWriteTokens"`
	} `json:"usage"`
}

// translateLine parses one stream-json line into zero or more
// backend-neutral events. Unrecognized lines ("thinking", and anything else
// not handled below) are silently dropped rather than treated as errors —
// same policy as claude's translator.
//
// CostUSD is always left at its zero value: cursor-agent's result event has
// no total_cost_usd field at all (confirmed directly), so cursor-backed
// runs simply show $0/no cost contribution. Estimating cost from tokens via
// a model-price table is out of scope for this pass.
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

	case "assistant":
		var msg rawAssistantMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			return nil
		}
		var events []agent.Event
		for _, block := range msg.Message.Content {
			if block.Type == "text" {
				events = append(events, agent.Event{
					TaskID: taskID, Type: agent.EventText,
					Text: block.Text,
				})
			}
		}
		return events

	case "tool_call":
		var tc rawToolCallEnvelope
		if err := json.Unmarshal(line, &tc); err != nil {
			return nil
		}
		var toolName string
		var raw json.RawMessage
		for k, v := range tc.ToolCall {
			toolName, raw = k, v
			break
		}
		if toolName == "" {
			return nil
		}
		var detail rawToolCallDetail
		if err := json.Unmarshal(raw, &detail); err != nil {
			return nil
		}
		switch env.Subtype {
		case "started":
			return []agent.Event{{
				TaskID: taskID, Type: agent.EventToolUse,
				ToolName: toolName, ToolInput: string(detail.Args),
			}}
		case "completed":
			events := []agent.Event{{
				TaskID: taskID, Type: agent.EventToolResult,
				Text: string(detail.Result),
			}}
			// createPlanToolCall sometimes arrives with empty args on
			// "started" but the full revised plan only on "completed"
			// (confirmed on plan-feedback resume runs) — emit a second
			// tool_use so cormake can persist the updated markdown.
			if toolName == "createPlanToolCall" && len(detail.Args) > 0 && string(detail.Args) != "null" {
				events = append([]agent.Event{{
					TaskID: taskID, Type: agent.EventToolUse,
					ToolName: toolName, ToolInput: string(detail.Args),
				}}, events...)
			}
			return events
		default:
			return nil
		}

	case "result":
		var res rawResult
		if err := json.Unmarshal(line, &res); err != nil {
			return nil
		}
		return []agent.Event{{
			TaskID: taskID, Type: agent.EventResult,
			ResultText:               res.Result,
			SessionID:                res.SessionID,
			InputTokens:              res.Usage.InputTokens,
			OutputTokens:             res.Usage.OutputTokens,
			CacheReadInputTokens:     res.Usage.CacheReadTokens,
			CacheCreationInputTokens: res.Usage.CacheWriteTokens,
		}}

	default:
		return nil
	}
}
