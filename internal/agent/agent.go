// Package agent defines a backend-agnostic boundary for running a coding
// agent and streaming back translated events. internal/agent/claude is the
// (currently only) implementation, wrapping the claude CLI; nothing outside
// this package should ever see raw claude JSON.
package agent

import "context"

// RunMode selects how a run is invoked: read-only research (plan) or actual
// execution inside a worktree (complete). This lives here, not on
// domain.Task, since it's a property of a single run, not a permanent
// attribute of the task itself — the same task can be planned, then later
// executed, as separate runs.
type RunMode string

const (
	RunModePlan     RunMode = "plan"
	RunModeComplete RunMode = "complete"
)

// RunSpec is everything needed to start one run.
type RunSpec struct {
	TaskID       string
	SessionID    string
	Prompt       string
	RepoPath     string
	Mode         RunMode
	WorktreeName string // only used for RunModeComplete
	SettingsPath string // optional; empty skips --settings (no hook server yet)

	// ResumeSessionID, when set, resumes an existing session (--resume)
	// instead of starting a new one — used to send follow-up feedback (e.g.
	// review annotations) back to the same conversation that produced a
	// plan, so claude has full context of what it already proposed. Takes
	// precedence over SessionID.
	ResumeSessionID string
}

type EventType int

const (
	EventInit EventType = iota
	EventText
	EventToolUse
	EventToolResult
	EventResult
	EventStderrLine
	EventProcessError
)

func (t EventType) String() string {
	switch t {
	case EventInit:
		return "init"
	case EventText:
		return "text"
	case EventToolUse:
		return "tool_use"
	case EventToolResult:
		return "tool_result"
	case EventResult:
		return "result"
	case EventStderrLine:
		return "stderr"
	case EventProcessError:
		return "process_error"
	default:
		return "unknown"
	}
}

// Event is a backend-neutral translation of one line of agent output (or a
// process-level occurrence like a stderr line or spawn failure).
type Event struct {
	TaskID string
	Type   EventType

	Text       string // assistant text, or tool_result content
	ToolName   string
	ToolInput  string // JSON-stringified; good enough for display in a POC
	IsSubagent bool

	ResultText string
	CostUSD    float64
	SessionID  string
	Cwd        string // set on EventInit; the process's actual working directory (a worktree path, for Complete-mode runs)

	Err error
}

// Handle is a running (or finished) agent invocation.
type Handle struct {
	Events <-chan Event
	Cancel func()
	Wait   func() error
}

// Runner starts an agent run and streams back translated events.
type Runner interface {
	Start(ctx context.Context, spec RunSpec) (*Handle, error)
}
