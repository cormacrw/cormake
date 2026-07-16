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
