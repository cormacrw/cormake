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

// RunSpec is everything needed to start one run. RepoPath is the caller's
// responsibility to point at a disposable git worktree rather than the
// actual checkout, for both RunModePlan and RunModeComplete — this package
// has no notion of worktrees itself, it just runs claude wherever RepoPath
// says.
type RunSpec struct {
	TaskID       string
	SessionID    string
	Prompt       string
	RepoPath     string
	Mode         RunMode
	SettingsPath string // optional; empty skips --settings (no hook server yet)

	// ResumeSessionID, when set, resumes an existing session (--resume)
	// instead of starting a new one — used to send follow-up feedback (e.g.
	// review annotations) back to the same conversation that produced a
	// plan, so claude has full context of what it already proposed. Takes
	// precedence over SessionID.
	ResumeSessionID string

	// RawStdoutPath/RawStderrPath are where Start redirects the detached
	// process's stdout/stderr — a real file, not a pipe, so a full buffer
	// never blocks the child on write() regardless of whether anything in
	// cormake is currently reading (the TUI paused via tea.ExecProcess, or
	// not running at all). Computed by the caller (see store.RawStdoutPath/
	// RawStderrPath) so this package stays storage-agnostic.
	RawStdoutPath string
	RawStderrPath string
}

// AttachSpec is everything needed to reattach to a process a *previous*
// cormake run already started (see AttachSpec's use in Runner.Attach) —
// distinct from RunSpec because no process is spawned here, just tailed.
type AttachSpec struct {
	TaskID string
	// PID is the OS process ID recorded when the run was originally
	// started. Liveness is checked via signal 0 (see claude.ProcessAlive),
	// not a real parent/child relationship — cormake may not be, and after
	// a restart never is, the process's actual OS parent.
	PID int

	RawStdoutPath string
	RawStderrPath string

	// Offset is how far into RawStdoutPath has already been translated and
	// appended to the task's persisted display log (see store.LoadOffset) —
	// tailing resumes from here so reattaching doesn't re-emit duplicate
	// log lines for content already shown before this cormake process
	// started.
	Offset int64
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

	// Token counts from the result's usage block, set only on EventResult.
	// CacheReadInputTokens/CacheCreationInputTokens are broken out from
	// InputTokens (rather than folded in) since they're billed at different
	// rates and cache reads dominate turn count on a resumed session —
	// collapsing them would make cache-heavy runs look far pricier than
	// their total_cost_usd actually reflects.
	InputTokens              int64
	OutputTokens             int64
	CacheReadInputTokens     int64
	CacheCreationInputTokens int64

	SessionID string
	Cwd       string // set on EventInit; the process's actual working directory (a worktree path, for Complete-mode runs)

	// Offset is set only on events derived from the raw stdout tail (0/unused
	// for EventStderrLine/EventProcessError): the byte position in the raw
	// stdout file consumed through the end of the line this event came from.
	// Persisted (see store.SaveOffset) so a later Attach knows where to
	// resume tailing without re-translating already-seen content.
	Offset int64

	Err error
}

// Handle is a running (or finished) agent invocation.
type Handle struct {
	Events <-chan Event
	// PID is the OS process ID of the underlying claude process, set
	// identically whether this Handle came from Start or Attach.
	PID int
	// Cancel asks the process to stop gracefully: SIGTERM now, SIGKILL after
	// a grace period if it's still alive — appropriate when the caller
	// itself keeps running long enough to enforce that grace period.
	Cancel func()
	// Kill terminates the process immediately (SIGKILL, no grace period) —
	// for a caller that's about to exit itself and so can't rely on a
	// delayed follow-up (see Cancel) ever actually firing.
	Kill func()
	// Wait blocks until the run is over and reports how it ended. For a
	// Start-produced Handle this is a real process exit error (or nil). For
	// an Attach-produced Handle there's no real child-process relationship
	// to report an exit code from, so this always returns nil — completion
	// vs. failure for that case is instead judged by whether an EventResult
	// was ever observed (see ui.handleTaskFinished's resultSeen check).
	Wait func() error
}

// Runner starts an agent run and streams back translated events.
type Runner interface {
	Start(ctx context.Context, spec RunSpec) (*Handle, error)
	// Attach resumes tailing a process a previous Start call already
	// launched (identified by AttachSpec.PID) instead of spawning a new
	// one — used when cormake restarts and finds a task's process still
	// alive (see ui.reconnectTask).
	Attach(ctx context.Context, spec AttachSpec) (*Handle, error)
}
