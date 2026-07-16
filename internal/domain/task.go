package domain

import "time"

// Status is a task's position in its lifecycle pipeline:
//
//	TODO -> (optionally) PLANNING -> PLANNED -> IN_PROGRESS -> AWAITING_APPROVAL* -> READY_FOR_REVIEW -> COMPLETE
//
// Planning is optional: from TODO you can kick off a planning agent
// (-> PLANNING -> PLANNED, once it's done) or skip straight to execution
// (-> IN_PROGRESS). Execute is available from either TODO or PLANNED.
// *Either PLANNING or IN_PROGRESS can pause on AWAITING_APPROVAL when the
// agent needs a permission decision or has a clarifying question.
//
// ARCHIVED is a separate, manual side-branch: only a TODO or
// READY_FOR_REVIEW task can be archived (parking work that isn't actively
// in an agent's hands), and unarchiving restores whichever of those two
// statuses it was archived from. COMPLETE/FAILED/CANCELLED tasks land in
// the archive view automatically, without needing to be archived manually
// or being able to be unarchived — they're already a finished outcome, not
// parked work.
type Status string

const (
	StatusTodo             Status = "todo"
	StatusPlanning         Status = "planning"
	StatusPlanned          Status = "planned"
	StatusInProgress       Status = "in_progress"
	StatusAwaitingApproval Status = "awaiting_approval"
	StatusReadyForReview   Status = "ready_for_review"
	StatusComplete         Status = "complete"
	StatusFailed           Status = "failed"
	StatusCancelled        Status = "cancelled"
	StatusArchived         Status = "archived"
)

type Task struct {
	ID          string
	WorkspaceID string
	RepoID      string
	Title       string
	Description string

	Status Status

	// PreviousStatus is only meaningful while Status == StatusArchived: it's
	// what to restore on unarchive (always StatusTodo or
	// StatusReadyForReview, since those are the only archivable statuses).
	PreviousStatus Status

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

// Stage is the coarse, user-facing lifecycle label shown in the task list;
// it's a 1:1 relabeling of Status for display purposes.
type Stage string

const (
	StageTodo           Stage = "TODO"
	StagePlanning       Stage = "PLANNING"
	StagePlanned        Stage = "PLANNED"
	StageInProgress     Stage = "IN PROGRESS"
	StageAwaitingInput  Stage = "AWAITING INPUT"
	StageReadyForReview Stage = "READY FOR REVIEW"
	StageComplete       Stage = "COMPLETE"
	StageFailed         Stage = "FAILED"
	StageCancelled      Stage = "CANCELLED"
	StageArchived       Stage = "ARCHIVED"
)

// DisplayStage maps Status onto its display label and glyph.
func (t Task) DisplayStage() (stage Stage, glyph string) {
	switch t.Status {
	case StatusTodo:
		return StageTodo, "○"
	case StatusPlanning:
		return StagePlanning, "✎"
	case StatusPlanned:
		return StagePlanned, "📋"
	case StatusInProgress:
		return StageInProgress, "⏳"
	case StatusAwaitingApproval:
		return StageAwaitingInput, "⚠"
	case StatusReadyForReview:
		return StageReadyForReview, "👀"
	case StatusComplete:
		return StageComplete, "✔"
	case StatusFailed:
		return StageFailed, "✖"
	case StatusCancelled:
		return StageCancelled, "⊘"
	case StatusArchived:
		return StageArchived, "📦"
	default:
		return StageTodo, "?"
	}
}

// IsArchived reports whether the task belongs in the archive view: either
// it was archived manually, or it reached a terminal outcome (complete,
// failed, or cancelled) on its own. Only the manual case can be undone —
// see CanArchive.
func (t Task) IsArchived() bool {
	switch t.Status {
	case StatusArchived, StatusComplete, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}

// CanArchive reports whether the task is currently eligible to be
// archived: only TODO and READY_FOR_REVIEW tasks are, since anything else
// is either actively in an agent's hands or already a terminal outcome.
func (t Task) CanArchive() bool {
	return t.Status == StatusTodo || t.Status == StatusReadyForReview
}
