package domain

import "time"

type Mode string

const (
	ModePlan     Mode = "plan"
	ModeComplete Mode = "complete"
)

type Status string

const (
	StatusPending          Status = "pending"
	StatusRunning          Status = "running"
	StatusAwaitingApproval Status = "awaiting_approval"
	StatusCompleted        Status = "completed"
	StatusFailed           Status = "failed"
	StatusCancelled        Status = "cancelled"
)

type Task struct {
	ID          string
	WorkspaceID string
	RepoID      string
	Title       string
	Description string

	Mode   Mode
	Status Status

	SessionID    string
	WorktreeName string

	ResultSummary string
	Cost          float64
	ErrorMessage  string

	// Reserved for a future MCP-based import from tools like ClickUp/Jira.
	// Defaults to "manual" and stays unpopulated until that importer exists.
	Source      string
	ExternalID  string
	ExternalURL string

	CreatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
}

// Stage is the coarse, user-facing lifecycle label shown in the task list,
// as opposed to the finer-grained Status used internally.
type Stage string

const (
	StageTodo       Stage = "TODO"
	StageInProgress Stage = "IN PROGRESS"
	StagePlanned    Stage = "PLANNED"
	StageComplete   Stage = "COMPLETE"
)

// DisplayStage maps (Mode, Status) onto one of the four lifecycle stages
// plus a glyph carrying outcome nuance. PLANNED and COMPLETE are
// mode-specific terminal states: a plan-mode task's finish line is a
// proposed plan, a complete-mode task's finish line is shipped work. A
// failed or cancelled run still reports the stage it reached, distinguished
// only by glyph.
func (t Task) DisplayStage() (stage Stage, glyph string) {
	switch t.Status {
	case StatusPending:
		return StageTodo, "○"
	case StatusRunning:
		return StageInProgress, "⏳"
	case StatusAwaitingApproval:
		return StageInProgress, "⚠"
	case StatusCompleted:
		return t.terminalStage(), "✔"
	case StatusFailed:
		return t.terminalStage(), "✖"
	case StatusCancelled:
		return t.terminalStage(), "⊘"
	default:
		return StageTodo, "?"
	}
}

func (t Task) terminalStage() Stage {
	if t.Mode == ModePlan {
		return StagePlanned
	}
	return StageComplete
}

// IsArchived reports whether the task reached a terminal outcome
// (succeeded, failed, or was cancelled) and belongs in the archive view
// rather than the active task list.
func (t Task) IsArchived() bool {
	switch t.Status {
	case StatusCompleted, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}
