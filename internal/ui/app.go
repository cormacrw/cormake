// Package ui implements the bubbletea root model for cormake's two-pane
// TUI shell: a task list on the left and a task detail/log view on the
// right, under a workspace tab bar and above a keybinding footer.
package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"

	"cormake/internal/agent"
	"cormake/internal/agent/claude"
	"cormake/internal/agent/cursor"
	"cormake/internal/domain"
	"cormake/internal/logformat"
	"cormake/internal/store"
	"cormake/internal/ui/detail"
	"cormake/internal/ui/tasklist"
	"cormake/internal/ui/theme"
)

const (
	topBarHeight = 1
	footerHeight = 1
	listWidthPct = 0.30
)

// taskTab selects which of the task list's three tabs is showing: the
// active TODO pipeline, manually-parked Archived tasks, or automatically
// terminal Completed tasks (see domain.Task.IsArchived/IsCompleted).
type taskTab int

const (
	taskTabTodo taskTab = iota
	taskTabArchived
	taskTabCompleted
)

type Model struct {
	store      *store.Store
	workspaces []domain.Workspace
	activeWS   int
	tasks      []domain.Task
	activeTab  taskTab

	workspaceModalOpen bool
	workspaceCursor    int

	// wizard is non-nil while the New Task wizard (see newtask.go) is open —
	// title, repo, target branch, source branch, then a recap confirmation.
	wizard *newTaskWizard

	// branchPickerOpen/branchPicker/branchPickerKind/branchPickerTaskID
	// stage the standalone change-target/change-source-branch shortcuts
	// (see branchmodal.go), independent of the wizard above.
	branchPickerOpen   bool
	branchPicker       *branchPicker
	branchPickerKind   branchPickerKind
	branchPickerTaskID string

	// inputModalOpen stages a free-form message to send to a PLANNED or
	// READY_FOR_REVIEW task's claude session (see keys.Input) — a lighter
	// alternative to the revdiff-based Review flow when there's nothing to
	// annotate on the artifact itself, just something to say.
	inputModalOpen bool
	inputTextarea  textarea.Model
	inputTaskID    string

	confirmModalOpen bool
	confirmMessage   string
	confirmKind      confirmKind
	confirmTo        domain.Status
	confirmFrom      []domain.Status
	confirmTaskID    string
	// confirmBackend is the agent backend a pending Plan/Execute
	// confirmation (confirmKindTransition only) will use — initialized in
	// openConfirm from the task's workspace default, toggleable in the
	// modal itself before confirming (see renderConfirmModal/
	// updateConfirmModal).
	confirmBackend domain.AgentBackend

	repoNames map[string]string

	tasklist tasklist.Model
	detail   detail.Model

	// leftFocus tracks which of the two left-hand panes — the CORMAKE
	// dashboard tile or the task list below it — is highlighted; that in
	// turn decides whether the right pane shows the dashboard or the
	// selected task's detail view. See updateDashboardFocus/renderBody.
	leftFocus paneFocus

	// sessionCostUSD accumulates ev.CostUSD across every EventResult seen
	// since this cormake process started — there's no local way to query
	// Anthropic's actual account-level usage/rate-limit remaining, so the
	// dashboard's "Claude usage" card shows this real, locally-known number
	// (spend by agents this process has run) rather than a fabricated quota.
	sessionCostUSD float64

	// dashboardWorktrees is the last-fetched snapshot of open worktrees and
	// their current commit, refreshed (see fetchWorktreesCmd) each time
	// leftFocus moves onto the dashboard pane rather than on every render,
	// since it shells out to git per worktree.
	dashboardWorktrees []worktreeRow

	// runners maps each domain.AgentBackend to the agent.Runner that
	// actually implements it — see runnerFor, which every call site goes
	// through instead of touching this map directly, so unrecognized/empty
	// backend values still fall back to claude rather than panicking.
	// eventsCh is the shared fan-in channel every running task's forwarding
	// goroutine writes to; active tracks running handles by task ID. A
	// running task's process is never killed just because cormake quits —
	// see the removal of the old cancelAllActive — so active is purely
	// bookkeeping for forwardEvents/deleteTask; no manual per-task Cancel
	// key wired up yet, that's still a future piece.
	runners  map[domain.AgentBackend]agent.Runner
	eventsCh chan tea.Msg
	active   map[string]*agent.Handle

	// spinnerTicking tracks whether the shared in-flight-agent spinner (see
	// tasklist.Model.SpinnerTick) is currently animating — the
	// spinner.TickMsg chain is self-perpetuating once started (see the
	// spinner package), so this flag is what lets it stop cleanly once
	// active is empty and restart on demand rather than ticking forever.
	spinnerTicking bool

	// resultSeen tracks, per task ID, whether an EventResult has been
	// observed for its current run — the primary signal handleTaskFinished
	// uses to decide success vs. failure, since an Attach-produced Handle's
	// Wait() has no real exit code to report (see agent.Handle.Wait).
	resultSeen map[string]bool

	width, height int
}

// agentEventMsg/taskFinishedMsg are what a running task's forwarding
// goroutine (see forwardEvents) sends over eventsCh.
type agentEventMsg struct{ Event agent.Event }
type taskFinishedMsg struct {
	TaskID string
	Err    error
}

// reconnectTaskMsg signals that a task loaded at startup was left mid-run
// (see Init) and should be reconnected — routed through Update rather than
// handled directly in Init, since Init has a value receiver and can't
// mutate the model it returns.
type reconnectTaskMsg struct{ TaskID string }

func New(st *store.Store) (Model, error) {
	workspaces, err := st.LoadWorkspaces()
	if err != nil {
		return Model{}, err
	}
	if len(workspaces) == 0 {
		now := time.Now()
		defaultWS := domain.Workspace{
			ID:        uuid.NewString(),
			Name:      "default",
			CreatedAt: now,
			UpdatedAt: now,
		}
		workspaces = []domain.Workspace{defaultWS}
		if err := st.SaveWorkspaces(workspaces); err != nil {
			return Model{}, err
		}
	}

	tasks, err := st.LoadTasks()
	if err != nil {
		return Model{}, err
	}

	// Eager-load every task's persisted log up front — a small personal
	// task list, not thousands of entries, so this is simpler than lazily
	// loading on first display and risking a task whose log never gets
	// loaded at all.
	logs := make(map[string][]string)
	for _, t := range tasks {
		lines, err := st.LoadLog(t.ID)
		if err != nil {
			fmt.Fprintln(os.Stderr, "cormake: failed to load log for", t.ID+":", err)
			continue
		}
		if lines != nil {
			logs[t.ID] = lines
		}
	}

	repoNames := make(map[string]string)
	for _, w := range workspaces {
		for _, r := range w.Repos {
			repoNames[r.ID] = r.Name
		}
	}

	ta := textarea.New()
	ta.Placeholder = "Type a message to send to claude..."
	ta.CharLimit = 4000
	ta.SetWidth(60)
	ta.SetHeight(8)
	ta.ShowLineNumbers = false

	m := Model{
		store:         st,
		workspaces:    workspaces,
		tasks:         tasks,
		repoNames:     repoNames,
		tasklist:      tasklist.New(nil),
		detail:        detail.New(logs),
		inputTextarea: ta,
		runners: map[domain.AgentBackend]agent.Runner{
			domain.AgentBackendClaude: claude.Client{},
			domain.AgentBackendCursor: cursor.Client{},
		},
		eventsCh:   make(chan tea.Msg, 64),
		active:     make(map[string]*agent.Handle),
		resultSeen: make(map[string]bool),
	}
	m.refreshTaskList()
	return m, nil
}

// runnerFor resolves backend to the agent.Runner that implements it,
// falling back to claude for an empty or unrecognized value — this is what
// lets an empty Task.AgentBackend (every task persisted before this field
// existed) keep behaving exactly as before, with no migration needed.
func (m Model) runnerFor(backend domain.AgentBackend) agent.Runner {
	if r, ok := m.runners[backend]; ok {
		return r
	}
	return m.runners[domain.AgentBackendClaude]
}

// taskAgentBackend looks up taskID's own AgentBackend — used by
// handleAgentEvent to label log lines with the backend that actually
// produced them (see logformat.FormatAgentLogLine), since agent.Event itself is
// deliberately backend-neutral and carries no such field.
func (m Model) taskAgentBackend(taskID string) domain.AgentBackend {
	for _, t := range m.tasks {
		if t.ID == taskID {
			return t.AgentBackend
		}
	}
	return domain.AgentBackendClaude
}

// replayFileFor resolves backend to the ReplayFile func that knows how to
// parse that backend's own stream-json dialect (see reconnectTask) — unlike
// ProcessAlive (a dialect-free PID check, so claude.ProcessAlive is reused
// directly regardless of backend), replaying raw stdout has to go through
// the same dialect parser the run was originally produced by. Falls back to
// claude for an empty/unrecognized value, same convention as runnerFor.
func replayFileFor(backend domain.AgentBackend) func(taskID, path string, fromOffset int64) ([]agent.Event, int64) {
	if backend == domain.AgentBackendCursor {
		return cursor.ReplayFile
	}
	return claude.ReplayFile
}

// Init kicks off listening for agent events plus, for every task loaded
// from disk still showing PLANNING or IN_PROGRESS, a reconnectTaskMsg —
// there's no live goroutine watching such a task yet in this cormake
// process, whether its run is still genuinely alive (most likely, since
// quitting no longer kills it — see reconnectTask), already finished while
// cormake was away, or actually died; reconnectTask (triggered by that
// message, in Update) sorts out which and reconnects accordingly.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{waitForEvent(m.eventsCh)}
	for _, t := range m.tasks {
		if t.Status == domain.StatusPlanning || t.Status == domain.StatusInProgress {
			taskID := t.ID
			cmds = append(cmds, func() tea.Msg { return reconnectTaskMsg{TaskID: taskID} })
		}
	}
	return tea.Batch(cmds...)
}

// waitForEvent is the standard bubbletea "drain a channel" command: it
// blocks for exactly one message, and callers re-append this same command
// to whatever they return so the loop keeps listening.
func waitForEvent(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

// ensureSpinnerTicking (re)starts the shared in-flight-agent spinner if it
// isn't already animating — called from every place that adds to m.active
// (runPlanAgent, runExecuteAgent, reconnectTask). The spinner.TickMsg case
// in Update is what stops the chain once active empties out, so this is
// the only place that needs to kick it back off.
func (m *Model) ensureSpinnerTicking() tea.Cmd {
	if m.spinnerTicking || len(m.active) == 0 {
		return nil
	}
	m.spinnerTicking = true
	return m.tasklist.SpinnerTick()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.recalcLayout()
		return m, nil

	case editorFinishedMsg:
		m.applyEditorResult(msg)
		return m, nil

	case fileEditFinishedMsg:
		m.reloadWorkspaces()
		return m, nil

	case revdiffFinishedMsg:
		cmd := m.handleRevdiffFinished(msg)
		return m, cmd

	case completeFinishedMsg:
		m.handleCompleteFinished(msg)
		return m, nil

	case deleteFinishedMsg:
		m.handleDeleteFinished(msg)
		return m, nil

	case agentEventMsg:
		m.handleAgentEvent(msg.Event)
		return m, waitForEvent(m.eventsCh)

	case taskFinishedMsg:
		m.handleTaskFinished(msg)
		return m, waitForEvent(m.eventsCh)

	case reconnectTaskMsg:
		return m, m.reconnectTask(msg.TaskID)

	case spinner.TickMsg:
		cmd := m.tasklist.UpdateSpinner(msg)
		if len(m.active) == 0 {
			// Nothing left in flight — drop the tick chain instead of
			// feeding it another cmd, so it stops rather than spinning
			// forever in the background.
			m.spinnerTicking = false
			return m, nil
		}
		return m, cmd

	case dashboardWorktreesMsg:
		m.dashboardWorktrees = msg.rows
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}

	// The New Task wizard and the standalone branch-picker modals wrap huh
	// forms, which advance fields/groups via their own internal follow-up
	// messages (nextFieldMsg, nextGroupMsg, etc.) returned as tea.Cmds from
	// a field's Update — not delivered synchronously, but as a plain
	// tea.Msg on some later call to this function. Unlike every other
	// modal below (all hand-rolled against bubbles widgets that resolve
	// within a single tea.KeyMsg), these two need every message type
	// routed to them while open, not just key presses, or that follow-up
	// message would fall through to the tasklist update at the bottom of
	// this function and the form would silently stop advancing.
	if m.wizard != nil {
		return m.updateNewTaskWizard(msg)
	}
	if m.branchPickerOpen {
		return m.updateBranchPickerModal(msg)
	}

	if msg, ok := msg.(tea.KeyMsg); ok {
		if m.workspaceModalOpen {
			return m.updateWorkspaceModal(msg)
		}

		if m.inputModalOpen {
			return m.updateInputModal(msg)
		}

		if m.confirmModalOpen {
			return m.updateConfirmModal(msg)
		}

		// While the "/" filter box is focused, every keystroke belongs to
		// it — letters like "e" or "p" must narrow the filter, not trigger
		// Execute/Plan — so route straight to the tasklist rather than
		// falling into the shortcut switch below.
		if m.tasklist.Filtering() {
			var cmd tea.Cmd
			m.tasklist, cmd = m.tasklist.Update(msg)
			m.syncDetail()
			return m, cmd
		}

		if m.leftFocus == leftFocusDashboard {
			return m.updateDashboardFocus(msg)
		}

		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, keys.PgUp, keys.PgDown, keys.Scroll):
			cmd := m.detail.Scroll(msg)
			return m, cmd

		case key.Matches(msg, keys.Up):
			if m.tasklist.AtTop() {
				m.leftFocus = leftFocusDashboard
				return m, fetchWorktreesCmd(m.tasks)
			}
			var cmd tea.Cmd
			m.tasklist, cmd = m.tasklist.Update(msg)
			m.syncDetail()
			return m, cmd

		case key.Matches(msg, keys.Left):
			if m.activeTab > taskTabTodo {
				m.activeTab--
			}
			m.refreshTaskList()
			return m, nil

		case key.Matches(msg, keys.Right):
			if m.activeTab < taskTabCompleted {
				m.activeTab++
			}
			m.refreshTaskList()
			return m, nil

		case key.Matches(msg, keys.Archive):
			m.archiveSelected()
			return m, nil

		case key.Matches(msg, keys.Delete):
			m.openDeleteConfirm()
			return m, nil

		case key.Matches(msg, keys.Complete):
			m.openCompleteModal()
			return m, nil

		case key.Matches(msg, keys.Open):
			if t, ok := m.tasklist.Selected(); ok {
				return m, openInEditorCmd(t)
			}
			return m, nil

		case key.Matches(msg, keys.Workspaces):
			m.openWorkspacePicker()
			return m, nil

		case key.Matches(msg, keys.NewTask):
			return m, m.openNewTaskWizard()

		case key.Matches(msg, keys.ChangeTargetBranch):
			return m, m.openBranchPickerModal(branchPickerKindTarget)

		case key.Matches(msg, keys.ChangeSourceBranch):
			return m, m.openBranchPickerModal(branchPickerKindSource)

		case key.Matches(msg, keys.Plan):
			m.openConfirm("Start planning", domain.StatusPlanning, domain.StatusTodo)
			return m, nil

		case key.Matches(msg, keys.Execute):
			m.openConfirm("Start executing", domain.StatusInProgress, domain.StatusTodo, domain.StatusPlanned)
			return m, nil

		case key.Matches(msg, keys.Input):
			return m, m.openInputModal()

		case key.Matches(msg, keys.Review):
			if t, ok := m.tasklist.Selected(); ok {
				// Once a task has been executed, review the actual code
				// changes rather than the plan that preceded it — the diff
				// is the more concrete, more current artifact. Planning and
				// Planned are excluded even though those tasks now have a
				// worktree too (see resolveTaskWorktree): plan-mode runs
				// read-only, writing no diff into it — claude's plan-mode
				// only ever writes the plan doc itself, and always to
				// ~/.claude/plans/ (see domain.Task.PlanFilePath) — so
				// there's nothing there yet worth diffing.
				if t.WorktreePath != "" && t.Status != domain.StatusPlanning && t.Status != domain.StatusPlanned {
					return m, openRevdiffDiffCmd(t.ID, t.WorktreePath, t.WorktreeBaseRef, t.ResultSummary)
				}
				if plan := m.readPlanFile(t); plan != "" {
					return m, openRevdiffCmd(t.ID, plan)
				}
			}
			return m, nil

		case key.Matches(msg, keys.TabDescription):
			m.detail.ShowDescription()
			return m, nil

		case key.Matches(msg, keys.TabPlan):
			m.detail.ShowPlan()
			return m, nil

		case key.Matches(msg, keys.TabSummary):
			m.detail.ShowSummary()
			return m, nil

		case key.Matches(msg, keys.TabLog):
			m.detail.ShowLog()
			return m, nil

		case key.Matches(msg, keys.TabPrev):
			m.detail.CycleTab(-1)
			return m, nil

		case key.Matches(msg, keys.TabNext):
			m.detail.CycleTab(1)
			return m, nil

		case key.Matches(msg, keys.Cancel, keys.Help):
			// Reserved: needs real storage/orchestrator wiring that doesn't
			// exist yet. Intentionally a no-op rather than pretending to work.
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.tasklist, cmd = m.tasklist.Update(msg)
	m.syncDetail()
	return m, cmd
}

// updateWorkspaceModal handles input while the workspace picker is open:
// up/down move the cursor, enter or a digit key confirms a selection, esc
// or q cancels without changing the active workspace.
func (m Model) updateWorkspaceModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.workspaceCursor > 0 {
			m.workspaceCursor--
		}
		return m, nil
	case "down", "j":
		if m.workspaceCursor < len(m.workspaces)-1 {
			m.workspaceCursor++
		}
		return m, nil
	case "enter":
		m.setWorkspace(m.workspaceCursor)
		m.workspaceModalOpen = false
		return m, nil
	case "esc", "q":
		m.workspaceModalOpen = false
		return m, nil
	case "e":
		// Manual repo management, for now: hand-edit workspaces.json
		// directly in an external editor rather than a dedicated form.
		m.workspaceModalOpen = false
		if m.store == nil {
			return m, nil
		}
		return m, openFileInEditorCmd(m.store.WorkspacesPath())
	}
	if idx, ok := digitIndex(msg.String()); ok && idx < len(m.workspaces) {
		m.setWorkspace(idx)
		m.workspaceModalOpen = false
	}
	return m, nil
}

// reloadWorkspaces re-reads workspaces.json after a manual edit (see the
// workspace modal's "e" action) and rebuilds the name lookup maps. A parse
// error or an edit that empties the file out entirely is logged and
// otherwise ignored, leaving the in-memory state as it was — better than
// bricking the app on a bad hand-edit.
func (m *Model) reloadWorkspaces() {
	if m.store == nil {
		return
	}
	workspaces, err := m.store.LoadWorkspaces()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cormake: failed to reload workspaces.json:", err)
		return
	}
	if len(workspaces) == 0 {
		fmt.Fprintln(os.Stderr, "cormake: workspaces.json has no workspaces, ignoring")
		return
	}

	m.workspaces = workspaces
	if m.activeWS >= len(m.workspaces) {
		m.activeWS = 0
	}

	repoNames := make(map[string]string)
	for _, w := range workspaces {
		for _, r := range w.Repos {
			repoNames[r.ID] = r.Name
		}
	}
	m.repoNames = repoNames
	m.refreshTaskList()
}

// confirmKind distinguishes what a pending confirmation modal will do once
// confirmed — a Status transition (Plan/Execute), an unconditional delete,
// or finalizing a task's worktree (Complete). Kept as plain data on Model
// rather than a stored closure: Update has a value receiver, so a new Model
// copy exists by the time "y" is handled, and a closure captured over the
// old *Model would be acting on a stale snapshot.
type confirmKind int

const (
	confirmKindTransition confirmKind = iota
	confirmKindDelete
	confirmKindComplete
)

// openConfirm stages a Plan/Execute status change behind a confirmation
// modal — but only if the selected task is actually eligible (in one of
// allowedFrom); an ineligible task stays a silent no-op, same as before
// this confirmation step existed.
func (m *Model) openConfirm(verb string, to domain.Status, allowedFrom ...domain.Status) {
	t, ok := m.tasklist.Selected()
	if !ok {
		return
	}
	eligible := false
	for _, s := range allowedFrom {
		if t.Status == s {
			eligible = true
			break
		}
	}
	if !eligible {
		return
	}
	m.confirmModalOpen = true
	m.confirmMessage = fmt.Sprintf("%s %q?", verb, t.Title)
	m.confirmKind = confirmKindTransition
	m.confirmTo = to
	m.confirmFrom = allowedFrom
	m.confirmBackend = m.workspaces[m.activeWS].EffectiveDefaultAgentBackend()
}

// openWorkspacePicker opens the workspace-picker modal, resetting its cursor
// onto the currently active workspace. Shared by the normal keymap and
// updateDashboardFocus, since switching workspaces makes just as much sense
// while the dashboard pane is focused as while the task list is.
func (m *Model) openWorkspacePicker() {
	m.workspaceCursor = m.activeWS
	m.workspaceModalOpen = true
}

// openDeleteConfirm stages deleting the selected task behind a confirmation
// modal. Unlike Plan/Execute there's no Status eligibility check — a task
// can be deleted no matter what stage it's in, the confirmation itself is
// the only gate.
func (m *Model) openDeleteConfirm() {
	t, ok := m.tasklist.Selected()
	if !ok {
		return
	}
	m.confirmModalOpen = true
	m.confirmMessage = fmt.Sprintf("Delete %q? This removes the task and any associated worktree/branch. This cannot be undone.", t.Title)
	m.confirmKind = confirmKindDelete
	m.confirmTaskID = t.ID
}

// openCompleteModal stages finalizing the selected task behind a
// confirmation modal — only eligible once it's actually been executed (has
// a worktree) and is ready for review. Its work is already committed onto
// TargetBranch throughout execution (see handleTaskFinished), so there's no
// branch name left to ask for — just a confirmation that it's really done.
func (m *Model) openCompleteModal() {
	t, ok := m.tasklist.Selected()
	if !ok || t.Status != domain.StatusReadyForReview || t.WorktreePath == "" {
		return
	}
	m.confirmModalOpen = true
	m.confirmMessage = fmt.Sprintf("Mark %q complete? This commits any outstanding changes and removes the worktree.", t.Title)
	m.confirmKind = confirmKindComplete
	m.confirmTaskID = t.ID
}

// openInputModal stages a free-form message to send to the selected task's
// existing agent session — only eligible for PLANNED or READY_FOR_REVIEW
// tasks, the two "stopped and waiting on a person" states outside of
// Review's revdiff annotation flow. The textarea's placeholder is
// re-labeled per task (t.AgentBackend), since a message sent here resumes
// whichever backend actually ran this task, not necessarily claude.
func (m *Model) openInputModal() tea.Cmd {
	t, ok := m.tasklist.Selected()
	if !ok || (t.Status != domain.StatusPlanned && t.Status != domain.StatusReadyForReview) {
		return nil
	}
	m.inputTaskID = t.ID
	m.inputTextarea.Reset()
	m.inputTextarea.Placeholder = "Type a message to send to " + logformat.AgentBackendLabel(t.AgentBackend) + "..."
	m.inputModalOpen = true
	return m.inputTextarea.Focus()
}

// updateInputModal handles input while the message textarea is open: esc
// cancels without sending anything, ctrl+s sends the message (resuming the
// task's session) and closes the modal, everything else is forwarded to the
// textarea itself — enter included, so it inserts a newline rather than
// submitting, since a prompt is often more than one line.
func (m Model) updateInputModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.inputModalOpen = false
		m.inputTextarea.Blur()
		return m, nil
	case tea.KeyCtrlS:
		message := strings.TrimSpace(m.inputTextarea.Value())
		m.inputModalOpen = false
		m.inputTextarea.Blur()
		if message == "" {
			return m, nil
		}
		cmd := m.sendInputPrompt(message)
		return m, cmd
	}
	var cmd tea.Cmd
	m.inputTextarea, cmd = m.inputTextarea.Update(msg)
	return m, cmd
}

// sendInputPrompt resumes the task openInputModal staged (by ID, same
// resolve-once pattern as startCompleteTask) with a free-form user message —
// a Plan-mode resume for a PLANNED task, or a Complete-mode resume in its
// existing worktree for a READY_FOR_REVIEW task.
func (m *Model) sendInputPrompt(message string) tea.Cmd {
	for _, t := range m.tasks {
		if t.ID != m.inputTaskID {
			continue
		}
		m.appendLogLine(t.ID, logformat.LogCormakeLine("sending message to "+logformat.AgentBackendLabel(t.AgentBackend)))
		if t.Status == domain.StatusReadyForReview {
			return m.runExecuteAgent(t, message+executeSummaryInstruction, t.SessionID)
		}
		return m.runPlanAgent(t, message, t.SessionID)
	}
	return nil
}

// startCompleteTask kicks off finalizing the task openCompleteModal staged
// (by ID, not the current selection — the modal blocks all other input so
// it can't have changed, but this matches openDeleteConfirm's pattern of
// resolving the target once, up front). The branch it lands on is always
// the worktree's own branch — target.TargetBranch, already committed to
// throughout execution (see handleTaskFinished) — never a separately typed
// name, so there's nothing left to prompt for here.
func (m *Model) startCompleteTask() tea.Cmd {
	var target domain.Task
	found := false
	for _, t := range m.tasks {
		if t.ID == m.confirmTaskID {
			target = t
			found = true
			break
		}
	}
	if !found || target.WorktreePath == "" {
		return nil
	}
	repoPath, ok := m.repoPath(target.RepoID)
	if !ok || repoPath == "" {
		m.appendLogLine(target.ID, logformat.LogCormakeLine("cannot complete — task has no repo assigned"))
		return nil
	}
	branch := target.TargetBranch
	if branch == "" {
		branch = target.WorktreeName
	}
	m.appendLogLine(target.ID, logformat.LogCormakeLine("finalizing onto branch "+branch))
	return completeTaskCmd(target.ID, repoPath, target.WorktreePath, branch, "cormake: "+target.Title)
}

// startDeleteTask kills any live agent for the staged task, then runs async
// git cleanup (worktree removal and branch deletion) before the task record
// is removed. When the task has no repo assigned, it skips git cleanup and
// finishes immediately.
func (m *Model) startDeleteTask() tea.Cmd {
	var target domain.Task
	found := false
	for _, t := range m.tasks {
		if t.ID == m.confirmTaskID {
			target = t
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	if h, ok := m.active[target.ID]; ok {
		h.Kill()
		delete(m.active, target.ID)
	}
	repoPath, ok := m.repoPath(target.RepoID)
	if !ok || repoPath == "" || target.RepoID == "" {
		return func() tea.Msg {
			return deleteFinishedMsg{taskID: target.ID}
		}
	}
	return deleteTaskCmd(target, repoPath, m.tasks)
}

// handleDeleteFinished removes the task from memory and disk after git
// cleanup finishes. Git failures are logged but do not block deletion.
func (m *Model) handleDeleteFinished(msg deleteFinishedMsg) {
	if msg.err != nil {
		label := msg.taskID
		for _, t := range m.tasks {
			if t.ID == msg.taskID {
				if t.DisplayID != "" {
					label = t.DisplayID
				} else {
					label = t.Title
				}
				break
			}
		}
		fmt.Fprintln(os.Stderr, "cormake: failed to clean up git for task", label+":", msg.err)
	}
	m.deleteTask(msg.taskID)
}

// handleCompleteFinished reacts to a finalize sequence ending: success moves
// the task to Complete and records the branch it landed on, clearing
// WorktreePath since that directory no longer exists (also keeps Review
// from later trying to diff-review a worktree that's gone); a failure is
// just logged, leaving the task exactly as it was — ReadyForReview with its
// worktree intact — so nothing is lost and completing can be retried.
func (m *Model) handleCompleteFinished(msg completeFinishedMsg) {
	if msg.err != nil {
		m.appendLogLine(msg.taskID, logformat.LogCormakeLine("failed to complete: "+msg.err.Error()))
		return
	}
	for i := range m.tasks {
		if m.tasks[i].ID != msg.taskID {
			continue
		}
		m.tasks[i].Status = domain.StatusComplete
		m.tasks[i].Branch = msg.branch
		m.tasks[i].WorktreePath = ""
		m.persistTask(m.tasks[i])
		break
	}
	m.refreshTaskList()
	m.syncDetail()
}

// updateConfirmModal handles input while the confirmation prompt is open:
// y/enter carries out whatever's staged (a status change, a delete, or a
// complete), anything else (n/esc/q) cancels without changing anything.
func (m Model) updateConfirmModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		m.confirmModalOpen = false
		if m.confirmKind == confirmKindDelete {
			cmd := m.startDeleteTask()
			return m, cmd
		}
		if m.confirmKind == confirmKindComplete {
			cmd := m.startCompleteTask()
			return m, cmd
		}
		if m.confirmTo == domain.StatusPlanning {
			cmd := m.startPlanRun()
			return m, cmd
		}
		if m.confirmTo == domain.StatusInProgress {
			cmd := m.startExecuteRun()
			return m, cmd
		}
		m.advanceSelected(m.confirmTo, m.confirmFrom...)
		return m, nil
	case "left", "right", "tab":
		// Only the two status-transition confirms (Plan/Execute) show the
		// backend selector — delete/complete confirms have no agent run to
		// pick a backend for, so left/right/tab there falls through to the
		// default cancel-on-anything-else behavior below, same as before
		// this selector existed.
		if m.confirmKind == confirmKindTransition {
			m.confirmBackend = toggleAgentBackend(m.confirmBackend)
			return m, nil
		}
		m.confirmModalOpen = false
		return m, nil
	default:
		m.confirmModalOpen = false
		return m, nil
	}
}

// toggleAgentBackend flips between the two agent backends the confirmation
// modal's selector cycles through (see updateConfirmModal/
// renderConfirmModal).
func toggleAgentBackend(b domain.AgentBackend) domain.AgentBackend {
	if b == domain.AgentBackendCursor {
		return domain.AgentBackendClaude
	}
	return domain.AgentBackendCursor
}

// createTask adds a new TODO task to the active workspace. repoID,
// targetBranch, and sourceBranch come from the new-task wizard (see
// newtask.go) — any of them may be empty (e.g. a workspace with no repos
// configured yet skips straight past those wizard steps). The description
// gets filled in later via the [enter] edit-in-editor flow.
func (m *Model) createTask(title, repoID, targetBranch, sourceBranch string) {
	if len(m.workspaces) == 0 {
		return
	}
	ws := &m.workspaces[m.activeWS]
	t := domain.Task{
		ID:           uuid.NewString(),
		DisplayID:    ws.NextDisplayID(),
		WorkspaceID:  ws.ID,
		Title:        title,
		Status:       domain.StatusTodo,
		Source:       "manual",
		RepoID:       repoID,
		TargetBranch: targetBranch,
		SourceBranch: sourceBranch,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if ws.TaskTemplate != "" {
		if content, err := m.store.ReadTemplate(ws.TaskTemplate); err == nil {
			t.Description = content
		}
	}
	m.tasks = append(m.tasks, t)
	m.persistTask(t)
	m.persistWorkspaces()
	m.activeTab = taskTabTodo // a fresh TODO task belongs in the TODO view
	m.refreshTaskList()
	m.tasklist.SelectByID(t.ID)
	m.syncDetail()
}

// persistTask saves t to disk, best-effort: there's no toast/error-banner
// UI yet (future polish), so a failed save is logged to stderr rather than
// crashing the TUI — a deliberate, temporary simplification. It also bumps
// t.UpdatedAt to now and reflects that onto m.tasks, since every mutation
// site routes through here — the task list's "everything else" section
// sorts on this field (see refreshTaskList).
func (m *Model) persistTask(t domain.Task) {
	t.UpdatedAt = time.Now()
	for i := range m.tasks {
		if m.tasks[i].ID == t.ID {
			m.tasks[i].UpdatedAt = t.UpdatedAt
			break
		}
	}
	if m.store == nil {
		return
	}
	if err := m.store.SaveTask(t); err != nil {
		fmt.Fprintln(os.Stderr, "cormake: failed to save task", t.ID+":", err)
	}
}

// persistWorkspaces saves the full workspace set to disk, best-effort, same
// pattern as persistTask — used after mutating a workspace's
// NextTaskNumber (see NextDisplayID) since SaveWorkspaces overwrites the
// whole file.
func (m *Model) persistWorkspaces() {
	if m.store == nil {
		return
	}
	if err := m.store.SaveWorkspaces(m.workspaces); err != nil {
		fmt.Fprintln(os.Stderr, "cormake: failed to save workspaces:", err)
	}
}

// appendLogLine is the single choke point for adding a line to a task's Log
// tab: it updates the in-memory detail view (so it shows up immediately)
// and persists it to disk (so it survives quitting and relaunching cormake
// — see internal/store/logs.go), best-effort like persistTask.
func (m *Model) appendLogLine(taskID, line string) {
	m.detail.AppendLogLine(taskID, line)
	if m.store == nil {
		return
	}
	if err := m.store.AppendLogLine(taskID, line); err != nil {
		fmt.Fprintln(os.Stderr, "cormake: failed to persist log line for", taskID+":", err)
	}
}

// digitIndex parses a single-digit '1'-'9' key into a zero-based index.
func digitIndex(s string) (int, bool) {
	if len(s) != 1 || s[0] < '1' || s[0] > '9' {
		return 0, false
	}
	return int(s[0] - '1'), true
}

// applyEditorResult reads back the temp file an external editor session
// left behind (if the session succeeded) and commits the parsed
// title/description into the edited task, then cleans up the temp file.
func (m *Model) applyEditorResult(msg editorFinishedMsg) {
	if msg.path == "" {
		return
	}
	defer os.Remove(msg.path)

	if msg.err != nil {
		return
	}
	data, readErr := os.ReadFile(msg.path)
	if readErr != nil {
		return
	}

	title, description := parseEditorContent(string(data))
	for i, t := range m.tasks {
		if t.ID == msg.taskID {
			m.tasks[i].Title = title
			m.tasks[i].Description = description
			m.persistTask(m.tasks[i])
			break
		}
	}
	m.refreshTaskList()
}

// archiveSelected toggles the selected task in or out of the archive. Only
// a TODO or READY_FOR_REVIEW task can be archived (see Task.CanArchive) —
// anything else is either actively in an agent's hands or already a
// terminal outcome, and archiving is a no-op there. Unarchiving restores
// whichever of those two statuses the task was archived from.
func (m *Model) archiveSelected() {
	t, ok := m.tasklist.Selected()
	if !ok {
		return
	}
	for i := range m.tasks {
		if m.tasks[i].ID != t.ID {
			continue
		}
		changed := true
		switch {
		case m.tasks[i].Status == domain.StatusArchived:
			m.tasks[i].Status = m.tasks[i].PreviousStatus
			m.tasks[i].PreviousStatus = ""
		case m.tasks[i].CanArchive():
			m.tasks[i].PreviousStatus = m.tasks[i].Status
			m.tasks[i].Status = domain.StatusArchived
		default:
			changed = false
		}
		if changed {
			m.persistTask(m.tasks[i])
		}
		break
	}
	m.refreshTaskList()
}

// deleteTask removes a task from disk and from the in-memory list.
// Unconditional by design — the confirmation modal is the only gate, there's
// no Status restriction like archiveSelected's CanArchive check.
func (m *Model) deleteTask(id string) {
	// A deleted task's live process (if any) shouldn't keep running against
	// a worktree that's about to disappear.
	if h, ok := m.active[id]; ok {
		h.Kill()
		delete(m.active, id)
	}
	for i, t := range m.tasks {
		if t.ID == id {
			m.tasks = append(m.tasks[:i], m.tasks[i+1:]...)
			break
		}
	}
	if m.store != nil {
		if err := m.store.DeleteTask(id); err != nil {
			fmt.Fprintln(os.Stderr, "cormake: failed to delete task", id+":", err)
		}
	}
	m.refreshTaskList()
}

// advanceSelected moves the selected task to a new status, but only if it's
// currently in one of allowedFrom — e.g. Execute makes sense from TODO or
// PLANNED, but not from anything else. This does not spawn any agent yet;
// it just advances Status so the pipeline is visible and testable in the UI.
func (m *Model) advanceSelected(to domain.Status, allowedFrom ...domain.Status) {
	t, ok := m.tasklist.Selected()
	if !ok {
		return
	}
	ok = false
	for _, s := range allowedFrom {
		if t.Status == s {
			ok = true
			break
		}
	}
	if !ok {
		return
	}
	for i := range m.tasks {
		if m.tasks[i].ID == t.ID {
			m.tasks[i].Status = to
			m.persistTask(m.tasks[i])
			break
		}
	}
	m.refreshTaskList()
}

// startPlanRun spawns a real plan-mode agent run for the selected task, on
// whichever backend the confirmation modal's selector settled on
// (m.confirmBackend) — a fresh Plan run always starts a brand new session
// (resumeSessionID ""), so it's always free to pick its own backend. See
// runPlanAgent's persistence of t.AgentBackend for how later resumes (e.g.
// revise-after-review) end up reusing this same choice automatically.
// Eligibility was already checked once in openConfirm before the
// confirmation modal appeared; nothing else could change the task's status
// in between (the modal blocks all other input), so this doesn't
// re-validate against confirmFrom.
func (m *Model) startPlanRun() tea.Cmd {
	t, ok := m.tasklist.Selected()
	if !ok {
		return nil
	}
	t.AgentBackend = m.confirmBackend
	return m.runPlanAgent(t, buildPrompt(t), "")
}

// activeAgentCount returns how many of workspaceID's tasks currently have a
// running agent handle — the enforcement point for each workspace's
// EffectiveMaxConcurrentAgents cap, checked by runPlanAgent/runExecuteAgent
// before spawning another one.
func (m *Model) activeAgentCount(workspaceID string) int {
	n := 0
	for _, t := range m.tasks {
		if t.WorkspaceID != workspaceID {
			continue
		}
		if _, ok := m.active[t.ID]; ok {
			n++
		}
	}
	return n
}

// resolveTaskWorktree determines the worktree a Plan or Execute run should
// operate in, shared by runPlanAgent/runExecuteAgent since both now need
// one: a task's work may target a specific branch, and both researching and
// implementing it should happen against that branch rather than whatever's
// currently checked out in the repo's actual working copy.
//
// When resumeSessionID is set, this just returns t's existing
// worktree/name/baseRef unchanged — a resumed run continues wherever its
// session already left off. Otherwise it resolves t.TargetBranch (falling
// back to worktreeName(t) for tasks predating the target-branch wizard) and
// either reuses a worktree already open on that branch (e.g. one a prior
// Plan run opened, or one opened for another task on the same branch) or
// creates a fresh one.
//
// The bool return is false when the run can't proceed (worktree creation
// failed, or a resume has no worktree on record) — the caller's own
// appendLogLine already explains why, so callers just bail on false rather
// than logging again.
func (m *Model) resolveTaskWorktree(t domain.Task, repoPath, resumeSessionID string) (worktreePath, wtName, baseRef string, ok bool) {
	wtName = t.WorktreeName
	baseRef = t.WorktreeBaseRef
	worktreePath = t.WorktreePath

	if resumeSessionID == "" {
		targetBranch := t.TargetBranch
		if targetBranch == "" {
			// Back-compat: tasks created before the target-branch wizard
			// existed have no TargetBranch on record, so fall back to the
			// old scheme of naming the branch after the task itself.
			targetBranch = worktreeName(t)
		}
		wtName = targetBranch
		baseRef = gitHeadRef(repoPath)

		if existing, found := findWorktreeForBranch(repoPath, targetBranch); found {
			m.appendLogLine(t.ID, logformat.LogCormakeLine("reusing existing worktree already open on branch "+targetBranch))
			worktreePath = existing
		} else {
			path, err := createWorktree(repoPath, targetBranch)
			if err != nil {
				m.appendLogLine(t.ID, logformat.LogCormakeLine("failed to create worktree: "+err.Error()))
				return "", "", "", false
			}
			worktreePath = path
		}
	}
	if worktreePath == "" {
		m.appendLogLine(t.ID, logformat.LogCormakeLine("cannot resume — task has no worktree"))
		return "", "", "", false
	}
	return worktreePath, wtName, baseRef, true
}

// runPlanAgent spawns a plan-mode claude run for t with the given prompt,
// shared by the initial Plan action and the revise-after-review-feedback
// flow (see revdiff.go). resumeSessionID, when non-empty, continues t's
// existing session instead of starting a fresh one — used so claude has
// full context of the plan it already proposed when revising it.
//
// Plan runs open a worktree the same way Execute runs do (see
// resolveTaskWorktree) rather than working directly in repoPath — a task's
// work may target a specific branch, and planning should research and write
// its plan against that branch too, not whatever happens to be checked out
// in the repo's actual working copy. A fresh Plan run's worktree is keyed by
// TargetBranch, so a later Execute run on the same task reuses it (see
// resolveTaskWorktree/findWorktreeForBranch) rather than opening a second
// one.
func (m *Model) runPlanAgent(t domain.Task, prompt, resumeSessionID string) tea.Cmd {
	repoPath, ok := m.repoPath(t.RepoID)
	if !ok || repoPath == "" {
		m.appendLogLine(t.ID, logformat.LogCormakeLine("cannot start — task has no repo assigned"))
		return nil
	}
	if limit := m.workspaces[m.activeWS].EffectiveMaxConcurrentAgents(); m.activeAgentCount(t.WorkspaceID) >= limit {
		m.appendLogLine(t.ID, logformat.LogCormakeLine(fmt.Sprintf("cannot start — workspace agent limit reached (%d running)", limit)))
		return nil
	}

	worktreePath, wtName, baseRef, ok := m.resolveTaskWorktree(t, repoPath, resumeSessionID)
	if !ok {
		return nil
	}

	spec := agent.RunSpec{
		TaskID:          t.ID,
		Prompt:          prompt,
		RepoPath:        worktreePath,
		Mode:            agent.RunModePlan,
		ResumeSessionID: resumeSessionID,
		RawStdoutPath:   m.store.RawStdoutPath(t.ID),
		RawStderrPath:   m.store.RawStderrPath(t.ID),
	}
	sessionID := resumeSessionID
	if sessionID == "" {
		sessionID = uuid.NewString()
		spec.SessionID = sessionID
	}

	handle, err := m.runnerFor(t.AgentBackend).Start(context.Background(), spec)
	if err != nil {
		m.appendLogLine(t.ID, logformat.LogCormakeLine("failed to start: "+err.Error()))
		return nil
	}

	// Start truncates the raw stdout file, so any offset left over from a
	// previous run must be reset — otherwise a later reconnect would read
	// from a stale position past the end of this shorter, freshly-truncated
	// file and silently miss this run's output entirely.
	if err := m.store.SaveOffset(t.ID, 0); err != nil {
		fmt.Fprintln(os.Stderr, "cormake: failed to reset offset for", t.ID+":", err)
	}

	for i := range m.tasks {
		if m.tasks[i].ID == t.ID {
			m.tasks[i].Status = domain.StatusPlanning
			m.tasks[i].SessionID = sessionID
			m.tasks[i].WorktreeName = wtName
			m.tasks[i].WorktreeBaseRef = baseRef
			m.tasks[i].WorktreePath = worktreePath
			m.tasks[i].PID = handle.PID
			m.tasks[i].AgentBackend = t.AgentBackend
			m.persistTask(m.tasks[i])
			break
		}
	}
	m.refreshTaskList()

	m.active[t.ID] = handle
	go forwardEvents(m.eventsCh, t.ID, handle)

	return m.ensureSpinnerTicking()
}

// startExecuteRun spawns a fresh real Complete-mode agent run for the
// selected task, on whichever backend the confirmation modal's selector
// settled on (m.confirmBackend). Execute always starts a fresh session
// regardless of what backend a prior Plan run used (resumeSessionID is
// always "" here — see runExecuteAgent), so it's always free to pick its
// own backend independent of Plan's. Eligibility (TODO/PLANNED) was already
// checked once in openConfirm before the confirmation modal appeared.
func (m *Model) startExecuteRun() tea.Cmd {
	t, ok := m.tasklist.Selected()
	if !ok {
		return nil
	}
	t.AgentBackend = m.confirmBackend
	return m.runExecuteAgent(t, buildExecutePrompt(t), "")
}

// runExecuteAgent spawns a Complete-mode claude run for t with the given
// prompt, shared by the initial Execute action and the
// revise-after-code-review flow (see revdiff.go). resumeSessionID, when
// non-empty, continues t's existing session in its existing worktree
// instead of starting a fresh one.
//
// A fresh run creates the worktree itself (see resolveTaskWorktree, which
// calls createWorktree in complete.go) rather than asking claude to via
// -w/--worktree: confirmed directly that -w forks from the repo's
// remote-tracking default branch when one is configured, not local HEAD,
// silently dropping local-only commits. Creating it ourselves and pointing
// RepoPath (-> cmd.Dir) straight at it sidesteps that entirely, and also
// means WorktreePath is known synchronously here rather than waited on from
// the run's first event. If the task was already planned, this reuses the
// same worktree the plan run opened (see resolveTaskWorktree).
func (m *Model) runExecuteAgent(t domain.Task, prompt, resumeSessionID string) tea.Cmd {
	repoPath, ok := m.repoPath(t.RepoID)
	if !ok || repoPath == "" {
		m.appendLogLine(t.ID, logformat.LogCormakeLine("cannot start — task has no repo assigned"))
		return nil
	}
	if limit := m.workspaces[m.activeWS].EffectiveMaxConcurrentAgents(); m.activeAgentCount(t.WorkspaceID) >= limit {
		m.appendLogLine(t.ID, logformat.LogCormakeLine(fmt.Sprintf("cannot start — workspace agent limit reached (%d running)", limit)))
		return nil
	}

	worktreePath, wtName, baseRef, ok := m.resolveTaskWorktree(t, repoPath, resumeSessionID)
	if !ok {
		return nil
	}

	sessionID := resumeSessionID
	if sessionID == "" {
		sessionID = uuid.NewString()
	}

	spec := agent.RunSpec{
		TaskID:          t.ID,
		Prompt:          prompt,
		RepoPath:        worktreePath,
		Mode:            agent.RunModeComplete,
		ResumeSessionID: resumeSessionID,
		RawStdoutPath:   m.store.RawStdoutPath(t.ID),
		RawStderrPath:   m.store.RawStderrPath(t.ID),
	}
	if resumeSessionID == "" {
		spec.SessionID = sessionID
	}

	handle, err := m.runnerFor(t.AgentBackend).Start(context.Background(), spec)
	if err != nil {
		m.appendLogLine(t.ID, logformat.LogCormakeLine("failed to start: "+err.Error()))
		return nil
	}

	// See runPlanAgent's identical reset: Start truncates the raw stdout
	// file, so a stale offset from a previous run must not linger.
	if err := m.store.SaveOffset(t.ID, 0); err != nil {
		fmt.Fprintln(os.Stderr, "cormake: failed to reset offset for", t.ID+":", err)
	}

	for i := range m.tasks {
		if m.tasks[i].ID == t.ID {
			m.tasks[i].Status = domain.StatusInProgress
			m.tasks[i].SessionID = sessionID
			m.tasks[i].WorktreeName = wtName
			m.tasks[i].WorktreeBaseRef = baseRef
			m.tasks[i].WorktreePath = worktreePath
			m.tasks[i].PID = handle.PID
			m.tasks[i].AgentBackend = t.AgentBackend
			m.persistTask(m.tasks[i])
			break
		}
	}
	m.refreshTaskList()

	m.active[t.ID] = handle
	go forwardEvents(m.eventsCh, t.ID, handle)

	return m.ensureSpinnerTicking()
}

// gitHeadRef resolves repoPath's current HEAD commit — best-effort, since
// this is only used to pin a review's diff base; a failure just means a
// later Review falls back to whatever revdiff defaults to without a
// baseRef, same degrade as "no repo assigned" already gets elsewhere.
func gitHeadRef(repoPath string) string {
	out, err := exec.Command("git", "-C", repoPath, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// worktreeName derives a git-branch-safe worktree name from a task, e.g.
// "acme-7" for a task with DisplayID "ACME-7". Falls back to the
// pre-readable-ID scheme ("task-a1b2c3d4") for tasks created before
// DisplayID existed.
func worktreeName(t domain.Task) string {
	if t.DisplayID != "" {
		return strings.ToLower(t.DisplayID)
	}
	return "task-" + shortTaskID(t.ID)
}

// shortTaskID returns a task ID's UUID first segment (8 hex chars) — short
// enough to use in generated branch/worktree names.
func shortTaskID(id string) string {
	if i := strings.IndexByte(id, '-'); i > 0 {
		return id[:i]
	}
	return id
}

// handleRevdiffFinished reacts to a revdiff annotation session ending: a
// failure gets logged, a clean quit with no annotations is a silent no-op,
// and actual annotations kick off a revise run — resuming the task's
// existing session (plan or execute, per msg.kind) so claude has full
// context of what it already did — with no separate confirmation step,
// since this reads as one continuous action (annotate, quit, feedback sent)
// rather than two.
func (m *Model) handleRevdiffFinished(msg revdiffFinishedMsg) tea.Cmd {
	if msg.err != nil {
		m.appendLogLine(msg.taskID, logformat.LogCormakeLine("revdiff failed: "+msg.err.Error()))
		return nil
	}
	if msg.annotations == "" {
		return nil
	}

	for _, t := range m.tasks {
		if t.ID != msg.taskID {
			continue
		}
		m.appendLogLine(t.ID, logformat.LogCormakeLine("sending review feedback to "+logformat.AgentBackendLabel(t.AgentBackend)+" for revision"))
		if msg.kind == reviewKindExecute {
			return m.runExecuteAgent(t, buildExecuteRevisePrompt(msg.annotations), t.SessionID)
		}
		return m.runPlanAgent(t, buildRevisePrompt(msg.annotations), t.SessionID)
	}
	return nil
}

// repoPath looks up a repo's filesystem path by ID across all workspaces —
// a linear scan is fine since it's only used at run-start time, not a hot
// path, and there's no dedicated repo-path map yet.
func (m Model) repoPath(repoID string) (string, bool) {
	for _, w := range m.workspaces {
		for _, r := range w.Repos {
			if r.ID == repoID {
				return expandHome(r.Path), true
			}
		}
	}
	return "", false
}

// expandHome expands a leading "~" or "~/..." to the user's home
// directory. The OS has no concept of "~" — that's purely a shell
// convenience — so a repo path hand-typed with one (very natural to type,
// since that's how you'd normally reference it in a terminal) would
// otherwise be passed to exec.Cmd.Dir completely literally and fail with a
// misleading fork/exec error rather than a clear "path doesn't exist" one.
func expandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}

// buildPrompt turns a task's title/description into the prompt sent to
// claude — intentionally minimal for now, no template beyond concatenation.
func buildPrompt(t domain.Task) string {
	task := t.Title
	if strings.TrimSpace(t.Description) != "" {
		task += "\n\n" + t.Description
	}
	// Explicit "write a plan" framing matters: plan mode judges case by
	// case whether a request warrants a written plan.md versus just
	// answering directly (confirmed directly — a bare "summarize X in one
	// sentence" prompt got answered inline with no plan file at all). Since
	// cormake's Plan tab depends on that file existing, ask for one plainly
	// rather than leaving it to judgment call.
	return "Investigate the following task and write up a plan for how to approach it:\n\n" + task
}

// executeSummaryInstruction asks claude to end a Complete-mode run on a
// proper summary of the actual work done plus a fenced commit-description
// block. The prose becomes ev.ResultText -> Task.ResultSummary (see
// handleAgentEvent's EventResult case) — cormake's Summary tab and the
// description shown alongside a code review (see openRevdiffDiffCmd)
// display that text — while the fenced block is parsed into
// Task.CommitDescription for slim git commit bodies (see
// commitBodyFromTask).
const executeSummaryInstruction = "\n\nWhen you are finished, your final message must be a concise summary " +
	"of what you actually implemented (not a question, not a list of possible next steps) — it's stored " +
	"and shown to the user as this task's result summary.\n\n" +
	"End your message with a commit description block in this exact format (short bullet points only):\n\n" +
	"```cormake-commit\n" +
	"- first change\n" +
	"- second change\n" +
	"```"

// buildExecutePrompt turns a task's title/description (and its plan, if it
// has one) into the prompt sent to claude for a real Complete-mode run.
// Unlike buildPrompt, this asks for the work to actually be implemented —
// Complete mode runs with edits enabled in a disposable worktree, so there's
// no reason to hold back.
func buildExecutePrompt(t domain.Task) string {
	task := t.Title
	if strings.TrimSpace(t.Description) != "" {
		task += "\n\n" + t.Description
	}
	prompt := "Implement the following task — make the necessary code changes to complete it:\n\n" + task
	if t.PlanFilePath != "" {
		prompt += fmt.Sprintf("\n\nA plan for this task was written earlier at %s — read it first and follow it.", t.PlanFilePath)
	}
	return prompt + executeSummaryInstruction
}

// forwardEvents drains a running task's event channel onto the shared
// eventsCh as tea.Msgs, then reports completion. It's a free function, not
// a Model method, deliberately: bubbletea's Update returns a fresh Model
// value on every call, so a goroutine must never hold a pointer into one
// particular snapshot — it may only touch data reached through a channel,
// which is safe to share by value across goroutines.
func forwardEvents(eventsCh chan<- tea.Msg, taskID string, h *agent.Handle) {
	for ev := range h.Events {
		eventsCh <- agentEventMsg{Event: ev}
	}
	err := h.Wait()
	eventsCh <- taskFinishedMsg{TaskID: taskID, Err: err}
}

// handleAgentEvent appends a formatted line to the task's live log for
// every event type, and additionally captures the final result text/cost
// onto the task itself when the run reports one.
// handleAgentEvent appends a formatted line to the task's live log for
// every event type (see logformat.go for how each kind renders), and
// separately handles whatever side effect that event type carries — none of
// which affect how the line reads, so they're kept out of logformat.FormatAgentLogLine
// entirely.
func (m *Model) handleAgentEvent(ev agent.Event) {
	m.appendLogLine(ev.TaskID, logformat.FormatAgentLogLine(ev, m.taskAgentBackend(ev.TaskID)))

	switch ev.Type {
	case agent.EventToolUse:
		if path, ok := extractPlanFilePath(ev.ToolName, ev.ToolInput); ok {
			m.setPlanFilePath(ev.TaskID, path)
		}
	case agent.EventInit:
		m.updateTaskSessionID(ev.TaskID, ev.SessionID)
	case agent.EventResult:
		m.resultSeen[ev.TaskID] = true
		m.sessionCostUSD += ev.CostUSD
		for i := range m.tasks {
			if m.tasks[i].ID == ev.TaskID {
				summary, commitDesc := parseAgentResult(ev.ResultText)
				m.tasks[i].ResultSummary = summary
				m.tasks[i].CommitDescription = commitDesc
				// Accumulate rather than overwrite: a task can go through
				// several runs (plan, execute, resumed review-feedback
				// rounds), each reporting its own EventResult, and the
				// summary should reflect the task's total spend across all
				// of them, not just whichever run finished last.
				m.tasks[i].Cost += ev.CostUSD
				m.tasks[i].InputTokens += ev.InputTokens
				m.tasks[i].OutputTokens += ev.OutputTokens
				m.tasks[i].CacheReadInputTokens += ev.CacheReadInputTokens
				m.tasks[i].CacheCreationInputTokens += ev.CacheCreationInputTokens
				break
			}
		}
		m.updateTaskSessionID(ev.TaskID, ev.SessionID)
	}

	// Persist how far into the raw stdout file this task has been
	// translated (unset/0 for stderr-derived events, which aren't
	// offset-tracked — see agent.AttachSpec) so a later Attach/replay
	// resumes from here instead of re-emitting duplicate display-log lines.
	if m.store != nil && ev.Type != agent.EventStderrLine && ev.Type != agent.EventProcessError {
		if err := m.store.SaveOffset(ev.TaskID, ev.Offset); err != nil {
			fmt.Fprintln(os.Stderr, "cormake: failed to save offset for", ev.TaskID+":", err)
		}
	}
}

// extractPlanFilePath checks whether a Write/Edit tool call targeted
// claude's plan directory (~/.claude/plans/ — its own hardcoded, always
// read-only-mode-permitted scratch space for plan-mode, confirmed
// directly, not assumed). toolInputJSON is the tool's JSON-stringified
// input; only its file_path field is used.
func extractPlanFilePath(toolName, toolInputJSON string) (string, bool) {
	if toolName != "Write" && toolName != "Edit" {
		return "", false
	}
	var input struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal([]byte(toolInputJSON), &input); err != nil || input.FilePath == "" {
		return "", false
	}
	plansDir, err := claudePlansDir()
	if err != nil || !strings.HasPrefix(input.FilePath, plansDir) {
		return "", false
	}
	return input.FilePath, true
}

// claudePlansDir returns ~/.claude/plans/ (trailing separator, so it's
// ready for a strings.HasPrefix path check).
func claudePlansDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "plans") + string(filepath.Separator), nil
}

// updateTaskSessionID persists the agent-reported session ID onto the task
// when it differs from what's stored — Cursor generates its own session on
// fresh runs (cormake's pre-assigned UUID is ignored), so the init/result
// events are what make --resume work on later feedback/input/review runs.
func (m *Model) updateTaskSessionID(taskID, sessionID string) {
	if sessionID == "" {
		return
	}
	for i := range m.tasks {
		if m.tasks[i].ID == taskID && m.tasks[i].SessionID != sessionID {
			m.tasks[i].SessionID = sessionID
			m.persistTask(m.tasks[i])
			break
		}
	}
}

// setPlanFilePath records where a task's plan landed (see
// extractPlanFilePath) and refreshes the detail pane immediately, so the
// Plan tab can appear mid-run rather than only once the whole run finishes.
func (m *Model) setPlanFilePath(taskID, path string) {
	for i := range m.tasks {
		if m.tasks[i].ID != taskID {
			continue
		}
		if m.tasks[i].PlanFilePath == path {
			return
		}
		m.tasks[i].PlanFilePath = path
		m.persistTask(m.tasks[i])
		break
	}
	m.syncDetail()
}

// handleTaskFinished reacts to a run being over: a successful plan run
// moves Planning -> Planned, a successful execute run moves InProgress ->
// ReadyForReview, and anything else moves it to Failed. "Successful" is
// judged primarily by resultSeen (did an EventResult ever come through for
// this task's current run) rather than msg.Err alone — an Attach-produced
// Handle has no real exit code to report (see agent.Handle.Wait), so its
// Err is always nil, and resultSeen is the only way to tell "it finished
// cleanly" apart from "the process just vanished". A real Err (from a
// Start-produced Handle's actual process exit) still wins when present.
// This doesn't yet distinguish "genuinely failed" from "user cancelled"
// (both look the same here) since there's no way to trigger Cancel from
// the UI yet — that's still a future piece.
//
// Every Complete-mode run (WorktreePath set, whether it's the initial
// execute or a later review-feedback revise) also gets committed here,
// success or not — one commit per attempt rather than everything staying
// squashed until the eventual completeTaskCmd commit, so a task's worktree
// history is easy to step through attempt by attempt during review. Plan
// runs are excluded even though they now have a worktree too (see
	// resolveTaskWorktree) — plan-mode never writes into it. The commit body is
	// CommitDescription when the agent supplied a ```cormake-commit block,
	// otherwise ResultSummary (see executeSummaryInstruction,
	// parseAgentResult, and handleAgentEvent's EventResult case) — freshly
	// overwritten by this run's own EventResult before taskFinishedMsg ever
	// arrives here, so it describes this attempt specifically rather than
	// some earlier one.
func (m *Model) handleTaskFinished(msg taskFinishedMsg) {
	delete(m.active, msg.TaskID)
	sawResult := m.resultSeen[msg.TaskID]
	delete(m.resultSeen, msg.TaskID)

	for i := range m.tasks {
		if m.tasks[i].ID != msg.TaskID {
			continue
		}
		m.tasks[i].PID = 0
		// Captured before the switch below overwrites Status: a plan-mode
		// run never touches the worktree's tracked files (claude's plan-mode
		// only ever writes the plan doc itself, and always to
		// ~/.claude/plans/, never into the worktree — see
		// domain.Task.PlanFilePath), so it's excluded from the
		// commit-per-attempt block below regardless of outcome — there's
		// nothing to commit, and counting it would leave a gap in the
		// attempt numbering with no matching commit.
		wasPlanning := m.tasks[i].Status == domain.StatusPlanning
		switch {
		case msg.Err != nil:
			m.tasks[i].Status = domain.StatusFailed
			m.tasks[i].ErrorMessage = msg.Err.Error()
		case !sawResult:
			m.tasks[i].Status = domain.StatusFailed
			m.tasks[i].ErrorMessage = "process ended without completing"
		case m.tasks[i].Status == domain.StatusPlanning:
			m.tasks[i].Status = domain.StatusPlanned
		case m.tasks[i].Status == domain.StatusInProgress:
			m.tasks[i].Status = domain.StatusReadyForReview
		}
		if !wasPlanning && m.tasks[i].WorktreePath != "" {
			m.tasks[i].ExecutionAttempts++
			commitMsg := fmt.Sprintf("cormake: %s (attempt %d)", m.tasks[i].Title, m.tasks[i].ExecutionAttempts)
			if body := commitBodyFromTask(m.tasks[i].ResultSummary, m.tasks[i].CommitDescription); body != "" {
				commitMsg += "\n\n" + body
			}
			if err := commitWorktreeChanges(m.tasks[i].WorktreePath, commitMsg); err != nil {
				m.appendLogLine(m.tasks[i].ID, logformat.LogCormakeLine("failed to commit execution attempt: "+err.Error()))
			}
		}
		m.persistTask(m.tasks[i])
		break
	}
	m.refreshTaskList()
	m.syncDetail()
}

// reconnectPlanPrompt/reconnectExecutePrompt are sent instead of the
// original buildPrompt/buildExecutePrompt when reconnectTask falls back to
// respawning a task left mid-run by a previous cormake process (its
// recorded process genuinely didn't survive — see reconnectTask) — worded
// to handle both real possibilities honestly, since there's no way to know
// from here whether the old run actually finished before it died or
// genuinely still has work left.
const reconnectPlanPrompt = "cormake was restarted and is reconnecting to this in-progress planning session. " +
	"If the plan is already written, just confirm it's in place; otherwise continue researching and write it up."

var reconnectExecutePrompt = "cormake was restarted and is reconnecting to this in-progress session. " +
	"If the task is already complete, just summarize what was implemented; otherwise continue and finish it." +
	executeSummaryInstruction

// reconnectTask handles a task Init found still PLANNING or IN_PROGRESS at
// startup. Quitting cormake no longer kills a task's process (see the
// removed cancelAllActive), so there are three real possibilities, checked
// in order:
//
//  1. The recorded PID is still alive → reattach a tailer to it (via
//     m.runnerFor(t.AgentBackend).Attach) from the stored offset. No new
//     process is spawned;
//     this is the common case for "closed cormake, or switched to another
//     tool, and it kept running".
//  2. The PID is dead, but replaying the raw stdout file from the stored
//     offset turns up a result → the run already finished while cormake
//     was away. Feed the replayed events through the normal handling (so
//     any trailing display-log lines/plan-file detection still happen),
//     then finalize via handleTaskFinished directly — nothing to spawn.
//  3. The PID is dead and no result was ever produced → genuinely
//     interrupted (a real crash, or an old run from before this feature
//     existed with no PID on record at all). Fall back to the previous
//     behavior: respawn via runPlanAgent/runExecuteAgent with --resume,
//     picking the session back up from claude's own persisted transcript.
func (m *Model) reconnectTask(taskID string) tea.Cmd {
	var t domain.Task
	found := false
	for _, task := range m.tasks {
		if task.ID == taskID {
			t = task
			found = true
			break
		}
	}
	if !found || t.SessionID == "" {
		return nil
	}

	if t.PID != 0 && claude.ProcessAlive(t.PID) {
		offset, err := m.store.LoadOffset(t.ID)
		if err != nil {
			fmt.Fprintln(os.Stderr, "cormake: failed to load offset for", t.ID+":", err)
		}
		handle, err := m.runnerFor(t.AgentBackend).Attach(context.Background(), agent.AttachSpec{
			TaskID:        t.ID,
			PID:           t.PID,
			RawStdoutPath: m.store.RawStdoutPath(t.ID),
			RawStderrPath: m.store.RawStderrPath(t.ID),
			Offset:        offset,
		})
		if err == nil {
			m.appendLogLine(t.ID, logformat.LogCormakeLine(fmt.Sprintf("cormake restarted — reattaching to still-running session (pid %d)", t.PID)))
			m.active[t.ID] = handle
			go forwardEvents(m.eventsCh, t.ID, handle)
			return m.ensureSpinnerTicking()
		}
		fmt.Fprintln(os.Stderr, "cormake: failed to attach to pid", t.PID, "for", t.ID+":", err)
	}

	offset, err := m.store.LoadOffset(t.ID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cormake: failed to load offset for", t.ID+":", err)
	}
	events, newOffset := replayFileFor(t.AgentBackend)(t.ID, m.store.RawStdoutPath(t.ID), offset)
	for _, ev := range events {
		m.handleAgentEvent(ev)
	}
	if err := m.store.SaveOffset(t.ID, newOffset); err != nil {
		fmt.Fprintln(os.Stderr, "cormake: failed to save offset for", t.ID+":", err)
	}
	if m.resultSeen[t.ID] {
		m.appendLogLine(t.ID, logformat.LogCormakeLine("cormake restarted — the run had already finished"))
		m.handleTaskFinished(taskFinishedMsg{TaskID: t.ID})
		return nil
	}

	m.appendLogLine(t.ID, logformat.LogCormakeLine("cormake restarted — reconnecting to interrupted session"))

	switch t.Status {
	case domain.StatusPlanning:
		return m.runPlanAgent(t, reconnectPlanPrompt, t.SessionID)
	case domain.StatusInProgress:
		if t.WorktreePath == "" {
			m.appendLogLine(t.ID, logformat.LogCormakeLine("cannot reconnect — task has no worktree on record"))
			return nil
		}
		return m.runExecuteAgent(t, reconnectExecutePrompt, t.SessionID)
	}
	return nil
}

// setWorkspace sets the active workspace by index and refreshes the task
// list; idx is expected to already be in range (callers check against
// len(m.workspaces)).
func (m *Model) setWorkspace(idx int) {
	if idx < 0 || idx >= len(m.workspaces) {
		return
	}
	m.activeWS = idx
	m.refreshTaskList()
}

// refreshTaskList rebuilds the visible task list from the active workspace
// and the active tab: TODO holds everything still actionable (todo,
// planning, in progress, awaiting input, ready for review), Archived holds
// manually-parked tasks (domain.Task.IsArchived), and Completed holds tasks
// that reached a terminal outcome on their own (domain.Task.IsCompleted).
func (m *Model) refreshTaskList() {
	if len(m.workspaces) == 0 {
		return
	}
	theme.SetAccent(m.workspaces[m.activeWS].PrimaryColor)
	activeID := m.workspaces[m.activeWS].ID
	filtered := make([]domain.Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		if t.WorkspaceID != activeID {
			continue
		}
		if !m.taskBelongsToActiveTab(t) {
			continue
		}
		filtered = append(filtered, t)
	}
	m.tasklist.SetTasks(filtered)
	m.syncDetail()
}

// taskBelongsToActiveTab reports whether t should be shown under the
// currently selected tab (see taskTab).
func (m *Model) taskBelongsToActiveTab(t domain.Task) bool {
	switch m.activeTab {
	case taskTabArchived:
		return t.IsArchived()
	case taskTabCompleted:
		return t.IsCompleted()
	default:
		return !t.IsArchived() && !t.IsCompleted()
	}
}

func (m *Model) syncDetail() {
	t, ok := m.tasklist.Selected()
	if !ok {
		msg := "No tasks yet.\n\nPress n to create one."
		switch {
		case m.tasklist.HasActiveFilter():
			msg = fmt.Sprintf("No tasks match %q.", m.tasklist.FilterQuery())
		case m.activeTab == taskTabArchived:
			msg = "No archived tasks."
		case m.activeTab == taskTabCompleted:
			msg = "No completed tasks."
		}
		m.detail.SetEmpty(msg)
		return
	}
	m.detail.SetTask(t, m.repoNames[t.RepoID], m.readPlanFile(t))
}

// readPlanFile reads a task's plan (see domain.Task.PlanFilePath) from
// disk, if it has one. A missing/unreadable file is treated the same as
// "no plan yet" rather than surfaced as an error — it's just not written
// (or not written *yet*, mid-run) rather than something having gone wrong.
func (m Model) readPlanFile(t domain.Task) string {
	if t.PlanFilePath == "" {
		return ""
	}
	data, err := os.ReadFile(t.PlanFilePath)
	if err != nil {
		return ""
	}
	return string(data)
}

func (m Model) currentWorkspaceName() string {
	if len(m.workspaces) == 0 {
		return ""
	}
	return m.workspaces[m.activeWS].Name
}

// paneDims returns each pane's total rendered width (border included) and
// the shared content height available inside either pane's border.
func (m Model) paneDims() (leftTotal, rightTotal, contentHeight int) {
	bodyHeight := m.height - topBarHeight - footerHeight
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	contentHeight = bodyHeight - paneOverhead
	if contentHeight < 1 {
		contentHeight = 1
	}

	leftTotal = int(float64(m.width) * listWidthPct)
	if leftTotal < paneOverhead+4 {
		leftTotal = paneOverhead + 4
	}
	rightTotal = m.width - leftTotal
	if rightTotal < paneOverhead+4 {
		rightTotal = paneOverhead + 4
	}
	return leftTotal, rightTotal, contentHeight
}

// cormakePaneOuterHeight is the CORMAKE dashboard tile's fixed total
// rendered height (border included) — just enough for one centered line of
// wordmark text.
const cormakePaneOuterHeight = 3

// taskTabsHeight is the fixed single row the TODO/Archived/Completed tabs occupy,
// unbordered, between the CORMAKE tile and the task list (see
// renderTaskTabs/renderBody).
const taskTabsHeight = 1

// leftPaneHeights splits the left column's total height (which otherwise
// matches the right pane's single box exactly) between the CORMAKE tile,
// the task tabs row, and the task list beneath them, returning the CORMAKE
// tile's and task list's inner content heights (border excluded, ready for
// Style.Height) — the tabs row itself is a fixed, unbordered single line.
func (m Model) leftPaneHeights() (cormakeContent, taskListContent int) {
	_, _, rightContentHeight := m.paneDims()
	totalOuter := rightContentHeight + paneOverhead

	taskListOuter := totalOuter - cormakePaneOuterHeight - taskTabsHeight
	if taskListOuter < paneOverhead+1 {
		taskListOuter = paneOverhead + 1
	}
	return cormakePaneOuterHeight - paneOverhead, taskListOuter - paneOverhead
}

func (m *Model) recalcLayout() {
	leftTotal, rightTotal, contentHeight := m.paneDims()
	_, taskListContent := m.leftPaneHeights()
	m.tasklist.SetSize(leftTotal-paneOverhead, taskListContent)
	m.detail.SetSize(rightTotal-paneOverhead, contentHeight)
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	if m.workspaceModalOpen {
		return m.renderWorkspaceModal()
	}

	if m.wizard != nil {
		return m.renderNewTaskWizard()
	}

	if m.branchPickerOpen {
		return m.renderBranchPickerModal()
	}

	if m.inputModalOpen {
		return m.renderInputModal()
	}

	if m.confirmModalOpen {
		return m.renderConfirmModal()
	}

	top := m.renderTopBar()
	body := m.renderBody()
	// Width/MaxWidth alone isn't enough: content wider than that still
	// *wraps* onto a second line instead of getting cut off (confirmed by
	// testing directly — MaxWidth only bounds the wrap column, not the line
	// count), which silently pushes the tab bar off-screen. Height/MaxHeight
	// pinned to 1 is what actually forces truncation. Bit us on this exact
	// line once already from an incomplete version of this fix.
	footer := footerStyle.Width(m.width).MaxWidth(m.width).Height(1).MaxHeight(1).Render(footerHelp)

	return lipgloss.JoinVertical(lipgloss.Left, top, body, footer)
}

// renderTopBar renders the current workspace name, right-aligned across the
// full width — the TODO/Archived/Completed task tabs used to live here too, but now
// sit under the CORMAKE tile instead (see renderTaskTabs), since they only
// ever affect the task list beneath them, not the whole app.
func (m Model) renderTopBar() string {
	wsInfo := tabInfoStyle.Render("workspace: " + m.currentWorkspaceName())
	gap := m.width - lipgloss.Width(wsInfo)
	if gap < 0 {
		gap = 0
	}
	bar := strings.Repeat(" ", gap) + wsInfo
	return lipgloss.NewStyle().Width(m.width).MaxWidth(m.width).Height(1).MaxHeight(1).Render(bar)
}

// renderTaskTabs renders the TODO/Archived/Completed tabs scoped to the
// left column's width — placed between the CORMAKE tile and the task list
// (see renderBody) since they only switch which tasks that list below them
// shows.
func (m Model) renderTaskTabs(width int) string {
	labels := []string{" TODO ", " Archived ", " Completed "}
	labels[m.activeTab] = "[" + strings.TrimSpace(labels[m.activeTab]) + "]"

	rendered := make([]string, len(labels))
	for i, label := range labels {
		if taskTab(i) == m.activeTab {
			rendered[i] = activeTabStyle().Render(label)
		} else {
			rendered[i] = inactiveTabStyle.Render(label)
		}
	}
	tabs := " " + strings.Join(rendered, "  ")
	return lipgloss.NewStyle().Width(width).MaxWidth(width).Height(1).MaxHeight(1).Render(tabs)
}

// renderWorkspaceModal draws the workspace picker, centered over the full
// screen (replacing the dashboard rather than overlaying it, for now).
func (m Model) renderWorkspaceModal() string {
	lines := []string{"Select workspace:", ""}
	for i, w := range m.workspaces {
		marker := "  "
		name := w.Name
		if i == m.workspaceCursor {
			marker = "▸ "
			name = activeTabStyle().Render(name)
		}
		swatch := lipgloss.NewStyle().Foreground(lipgloss.Color(swatchColor(w.PrimaryColor))).Render("■")
		lines = append(lines, marker+swatch+" "+name)
	}
	lines = append(lines, "", tabInfoStyle.Render("[enter] select   [e]dit repos/color   [esc] cancel"))

	box := focusedPaneBorderStyle().Padding(1, 3).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// swatchColor mirrors theme.SetAccent's empty-string fallback so an
// unset PrimaryColor previews as today's default pink rather than black.
func swatchColor(c string) string {
	if c == "" {
		return theme.DefaultAccent
	}
	return c
}

// renderInputModal draws the free-form message textarea, centered over the
// full screen.
func (m Model) renderInputModal() string {
	lines := []string{
		"Send a message to " + logformat.AgentBackendLabel(m.taskAgentBackend(m.inputTaskID)), "",
		m.inputTextarea.View(), "",
		tabInfoStyle.Render("[ctrl+s] send   [esc] cancel"),
	}
	box := focusedPaneBorderStyle().Padding(1, 3).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// renderConfirmModal draws the Plan/Execute confirmation prompt, centered
// over the full screen. The status-transition confirms (Plan/Execute) also
// show a two-option agent-backend toggle beneath the message; delete/
// complete confirms have no agent run to pick a backend for, so it's
// omitted there.
func (m Model) renderConfirmModal() string {
	lines := []string{m.confirmMessage}
	if m.confirmKind == confirmKindTransition {
		lines = append(lines, "", "Agent:  "+renderAgentBackendToggle(m.confirmBackend))
	}
	lines = append(lines, "", tabInfoStyle.Render("[y]es   [n]o   [tab] switch agent"))
	box := focusedPaneBorderStyle().Padding(1, 3).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// renderAgentBackendToggle draws the manual two-option backend picker used
// by renderConfirmModal — a plain highlighted-label toggle rather than a
// full huh.Select, matching this modal's existing plain-text style (it's
// not a huh.Form today, unlike e.g. branchPicker).
func renderAgentBackendToggle(selected domain.AgentBackend) string {
	label := func(name string, backend domain.AgentBackend) string {
		if selected == backend || (selected == "" && backend == domain.AgentBackendClaude) {
			return activeTabStyle().Render("(*) " + name)
		}
		return tabInfoStyle.Render("( ) " + name)
	}
	return label("claude", domain.AgentBackendClaude) + "   " + label("cursor", domain.AgentBackendCursor)
}

// renderBody renders the three panes: the CORMAKE dashboard tile and the
// task list stacked on the left, and — depending on which of those two has
// focus (see leftFocus) — either the selected task's detail view or the
// dashboard on the right.
func (m Model) renderBody() string {
	cormakeStyle := paneBorderStyle
	taskListStyle := focusedPaneBorderStyle()
	if m.leftFocus == leftFocusDashboard {
		cormakeStyle = focusedPaneBorderStyle()
		taskListStyle = paneBorderStyle
	}

	leftTotal, rightTotal, contentHeight := m.paneDims()
	cormakeContent, taskListContent := m.leftPaneHeights()

	cormakePane := cormakeStyle.Width(leftTotal-paneOverhead).Height(cormakeContent).
		Align(lipgloss.Center, lipgloss.Center).Render(cormakeWordmark())
	taskTabs := m.renderTaskTabs(leftTotal)
	taskListPane := taskListStyle.Width(leftTotal - paneOverhead).Height(taskListContent).Render(m.tasklist.View())
	left := lipgloss.JoinVertical(lipgloss.Left, cormakePane, taskTabs, taskListPane)

	rightContent := m.detail.View()
	if m.leftFocus == leftFocusDashboard {
		rightContent = m.renderDashboard(rightTotal-paneOverhead, contentHeight)
	}
	right := paneBorderStyle.Width(rightTotal - paneOverhead).Height(contentHeight).Render(rightContent)

	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}
