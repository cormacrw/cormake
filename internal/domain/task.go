package domain

import "time"

// DefaultSourceBranch is the branch a new task's SourceBranch defaults to
// before the user picks a different one in the new-task wizard — the
// branch this task's work will eventually be merged into.
const DefaultSourceBranch = "develop"

// Status is a task's position in its lifecycle pipeline:
//
//	TODO -> (optionally) PLANNING -> PLANNED -> IN_PROGRESS -> AWAITING_APPROVAL* -> READY_FOR_REVIEW -> [OPENING_PR -> IN_REVIEW ->] COMPLETE
//
// Planning is optional: from TODO you can kick off a planning agent
// (-> PLANNING -> PLANNED, once it's done) or skip straight to execution
// (-> IN_PROGRESS). Execute is available from either TODO or PLANNED.
// *Either PLANNING or IN_PROGRESS can pause on AWAITING_APPROVAL when the
// agent needs a permission decision or has a clarifying question.
//
// The PR sub-branch is optional and manual: from READY_FOR_REVIEW, opening
// a PR moves through OPENING_PR (an agent pushing the branch and running
// `gh pr create`, see ui.startOpenPR) to IN_REVIEW once that's confirmed to
// have actually landed (see ui.handlePRQuery). IN_REVIEW behaves like
// READY_FOR_REVIEW — Review/Input both still work, resuming the same
// execute session to address PR feedback (see ui.sendInputPrompt) — plus
// its own PR-comments/PR-description panes and periodic polling (see
// ui.ensurePRPollTicking). A task can also skip the PR flow entirely and go
// straight from READY_FOR_REVIEW to COMPLETE, same as before this existed.
//
// ARCHIVED is a separate, manual side-branch: only a TODO or
// READY_FOR_REVIEW task can be archived (parking work that isn't actively
// in an agent's hands), and unarchiving restores whichever of those two
// statuses it was archived from. COMPLETE/FAILED/CANCELLED tasks land in
// the Completed tab automatically instead, without needing to be archived
// manually or being able to be unarchived — they're already a finished
// outcome, not parked work.
type Status string

const (
	StatusTodo             Status = "todo"
	StatusPlanning         Status = "planning"
	StatusPlanned          Status = "planned"
	StatusInProgress       Status = "in_progress"
	StatusAwaitingApproval Status = "awaiting_approval"
	StatusReadyForReview   Status = "ready_for_review"
	StatusOpeningPR        Status = "opening_pr"
	StatusInReview         Status = "in_review"
	StatusComplete         Status = "complete"
	StatusFailed           Status = "failed"
	StatusCancelled        Status = "cancelled"
	StatusArchived         Status = "archived"
)

type Task struct {
	ID string

	// DisplayID is the workspace-scoped human-readable id (e.g. "ACME-7"),
	// set once at creation via Workspace.NextDisplayID and immutable after.
	// ID (the UUID) remains the on-disk filename/lookup key; DisplayID is
	// purely an additional human-facing label.
	DisplayID string

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

	// AgentBackend is which agent CLI (see AgentBackend) this task's active
	// session actually belongs to — set once when a fresh Plan or Execute
	// run is kicked off from the confirmation modal, then reused by every
	// later resume of that same task (revise-after-review, a free-form
	// message). Zero-value (empty) on already-persisted tasks is treated as
	// AgentBackendClaude, matching this codebase's existing
	// zero-value-means-legacy-default convention (see e.g.
	// Workspace.EffectiveMaxConcurrentAgents).
	AgentBackend AgentBackend

	// PID is the OS process ID of this task's live (or most-recently-run)
	// claude process, if any — used to detect whether a task left
	// Planning/InProgress by a previous cormake process actually survived a
	// restart (see ui.reconnectTask), rather than assuming it didn't. 0 once
	// the run has finished (see ui.handleTaskFinished).
	PID int

	// WorktreePath is the worktree's actual absolute path on disk — set by
	// cormake itself when it creates (or reuses) the worktree for a task's
	// first Plan or Execute run (see ui.resolveTaskWorktree), not reported
	// back by claude.
	WorktreePath string

	// WorktreeBaseRef is the repo's HEAD commit at the moment the worktree
	// was created — the base a code review diff should compare against.
	// Captured directly rather than assumed (e.g. "master") since the
	// worktree's branch name and the repo's default branch name are
	// independent of each other.
	WorktreeBaseRef string

	// Branch is the branch the task's work landed on once marked complete
	// (see the Complete-task flow) — the same branch as TargetBranch, the
	// worktree's own branch throughout execution, left behind in the repo
	// after the worktree itself is removed.
	Branch string

	// ExecutionAttempts counts how many Complete-mode runs (the initial
	// execute plus every review-feedback revise) have finished in this
	// task's worktree — each one gets its own commit (see
	// ui.handleTaskFinished) rather than squashing everything together at
	// Complete time, so the worktree's history stays one commit per attempt
	// and is easier to step through in review. Plan-mode runs don't count,
	// even though they share the same worktree (see ui.resolveTaskWorktree)
	// — plan-mode never writes into it, so there'd be nothing to commit.
	ExecutionAttempts int

	// TargetBranch is the branch this task's work is committed to — chosen
	// in the new-task wizard (see ui.newTaskWizard), defaulting to a fresh
	// branch auto-named from the task's title unless the user picks an
	// existing one instead. If that branch already has a worktree open
	// elsewhere, Plan/Execute reuse it rather than creating a second one
	// (see ui.resolveTaskWorktree/ui.findWorktreeForBranch) — git itself
	// would refuse a second worktree on the same branch anyway.
	TargetBranch string

	// SourceBranch is the branch this task's work will eventually be merged
	// into — independent of TargetBranch (the branch actually committed to)
	// and of the repo's own git HEAD, since neither necessarily matches
	// where the work should land once reviewed. Defaults to the workspace's
	// EffectiveDefaultTargetBranch, overridable in the new-task wizard.
	SourceBranch string

	// PlanFilePath points at the plan an agent wrote during a plan-mode run.
	// Claude plan-mode writes to ~/.claude/plans/ (confirmed directly); cursor
	// plan-mode uses createPlanToolCall with inline markdown, which cormake
	// persists under store.PlanPath. cormake watches tool calls and remembers
	// where the plan landed rather than owning the file itself.
	PlanFilePath string

	ResultSummary string

	// CommitDescription is short bullet points parsed from the agent's final
	// ```cormake-commit fenced block (see ui.parseAgentResult) — used as the
	// git commit body for each execution attempt rather than the full
	// ResultSummary prose.
	CommitDescription string

	// Cost and the token counts below accumulate across every agent run
	// this task has had — planning, execution, and any resumed
	// review-feedback round trips (see ui.handleAgentEvent) — rather than
	// holding just the most recent run's numbers, so the summary reflects
	// the task's full spend, not only its last leg.
	Cost                     float64
	InputTokens              int64
	OutputTokens             int64
	CacheReadInputTokens     int64
	CacheCreationInputTokens int64

	ErrorMessage string

	// PRNumber/PRURL identify the pull request opened for this task's work
	// (see ui.startOpenPR/ui.handlePRQuery) — the only PR-related state
	// that's actually persisted; everything else about it (title, body,
	// comments, merge state) is fetched live via `gh pr view` and kept
	// in-memory only (see domain.PRSnapshot), since it's cheap to re-fetch
	// and would otherwise just go stale on disk between polls. PRNumber
	// zero means no PR has been opened (or the attempt failed) — used by
	// detail.Model.HasPR to gate the PR-description/PR-comments tabs.
	PRNumber int
	PRURL    string

	// Reserved for a future MCP-based import from tools like ClickUp/Jira.
	// Defaults to "manual" and stays unpopulated until that importer exists.
	Source      string
	ExternalID  string
	ExternalURL string

	CreatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time

	// UpdatedAt is bumped on every persisted change (see ui.persistTask) and
	// used to order the task list's "everything else" section by
	// most-recently-touched.
	UpdatedAt time.Time
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
	StageOpeningPR      Stage = "OPENING PR"
	StageInReview       Stage = "IN REVIEW"
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
	case StatusOpeningPR:
		return StageOpeningPR, "📤"
	case StatusInReview:
		return StageInReview, "🔍"
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

// IsArchived reports whether the task belongs in the Archived tab: it was
// manually archived, parking it out of the active TODO view without it
// being a finished outcome. See CanArchive for how a task gets here, and
// IsCompleted for the separate, automatic terminal-outcome case.
func (t Task) IsArchived() bool {
	return t.Status == StatusArchived
}

// IsCompleted reports whether the task belongs in the Completed tab: it
// reached a terminal outcome (complete, failed, or cancelled) on its own,
// rather than being manually archived. Unlike an archived task, this can't
// be undone — it's already a finished outcome, not parked work.
func (t Task) IsCompleted() bool {
	switch t.Status {
	case StatusComplete, StatusFailed, StatusCancelled:
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
