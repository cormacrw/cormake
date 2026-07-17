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
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"

	"cormake/internal/agent"
	"cormake/internal/agent/claude"
	"cormake/internal/domain"
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

type Model struct {
	store       *store.Store
	workspaces  []domain.Workspace
	activeWS    int
	tasks       []domain.Task
	showArchive bool

	workspaceModalOpen bool
	workspaceCursor    int

	newTaskModalOpen bool
	newTaskInput     textinput.Model

	completeModalOpen bool
	completeInput     textinput.Model
	completeTaskID    string

	confirmModalOpen bool
	confirmMessage   string
	confirmKind      confirmKind
	confirmTo        domain.Status
	confirmFrom      []domain.Status
	confirmTaskID    string

	repoNames map[string]string

	tasklist tasklist.Model
	detail   detail.Model

	// runner is the agent backend — claude.Client today, but kept behind
	// the agent.Runner interface so nothing here is claude-specific.
	// eventsCh is the shared fan-in channel every running task's forwarding
	// goroutine writes to; active tracks running handles by task ID. A
	// running task's process is never killed just because cormake quits —
	// see the removal of the old cancelAllActive — so active is purely
	// bookkeeping for forwardEvents/deleteTask; no manual per-task Cancel
	// key wired up yet, that's still a future piece.
	runner   agent.Runner
	eventsCh chan tea.Msg
	active   map[string]*agent.Handle

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

	ti := textinput.New()
	ti.Placeholder = "task title"
	ti.CharLimit = 200
	ti.Width = 40

	ci := textinput.New()
	ci.Placeholder = "feature branch name"
	ci.CharLimit = 200
	ci.Width = 40

	m := Model{
		store:         st,
		workspaces:    workspaces,
		tasks:         tasks,
		repoNames:     repoNames,
		tasklist:      tasklist.New(nil),
		detail:        detail.New(logs),
		newTaskInput:  ti,
		completeInput: ci,
		runner:        claude.Client{},
		eventsCh:      make(chan tea.Msg, 64),
		active:        make(map[string]*agent.Handle),
		resultSeen:    make(map[string]bool),
	}
	m.refreshTaskList()
	return m, nil
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

	case agentEventMsg:
		m.handleAgentEvent(msg.Event)
		return m, waitForEvent(m.eventsCh)

	case taskFinishedMsg:
		m.handleTaskFinished(msg)
		return m, waitForEvent(m.eventsCh)

	case reconnectTaskMsg:
		m.reconnectTask(msg.TaskID)
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		if m.workspaceModalOpen {
			return m.updateWorkspaceModal(msg)
		}

		if m.newTaskModalOpen {
			return m.updateNewTaskModal(msg)
		}

		if m.completeModalOpen {
			return m.updateCompleteModal(msg)
		}

		if m.confirmModalOpen {
			return m.updateConfirmModal(msg)
		}

		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, keys.PgUp, keys.PgDown):
			cmd := m.detail.Scroll(msg)
			return m, cmd

		case key.Matches(msg, keys.Left):
			m.showArchive = false
			m.refreshTaskList()
			return m, nil

		case key.Matches(msg, keys.Right):
			m.showArchive = true
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
			if m.completeModalOpen {
				return m, m.completeInput.Focus()
			}
			return m, nil

		case key.Matches(msg, keys.Open):
			if t, ok := m.tasklist.Selected(); ok {
				return m, openInEditorCmd(t)
			}
			return m, nil

		case key.Matches(msg, keys.Workspaces):
			m.workspaceCursor = m.activeWS
			m.workspaceModalOpen = true
			return m, nil

		case key.Matches(msg, keys.NewTask):
			m.newTaskInput.SetValue("")
			m.newTaskModalOpen = true
			return m, m.newTaskInput.Focus()

		case key.Matches(msg, keys.Plan):
			m.openConfirm("Start planning", domain.StatusPlanning, domain.StatusTodo)
			return m, nil

		case key.Matches(msg, keys.Execute):
			m.openConfirm("Start executing", domain.StatusInProgress, domain.StatusTodo, domain.StatusPlanned)
			return m, nil

		case key.Matches(msg, keys.Review):
			if t, ok := m.tasklist.Selected(); ok {
				// Once a task has been executed, review the actual code
				// changes rather than the plan that preceded it — the diff
				// is the more concrete, more current artifact.
				if t.WorktreePath != "" {
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
// confirmed — a Status transition (Plan/Execute) or an unconditional delete.
// Kept as plain data on Model rather than a stored closure: Update has a
// value receiver, so a new Model copy exists by the time "y" is handled, and
// a closure captured over the old *Model would be acting on a stale snapshot.
type confirmKind int

const (
	confirmKindTransition confirmKind = iota
	confirmKindDelete
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
	m.confirmMessage = fmt.Sprintf("Delete %q? This cannot be undone.", t.Title)
	m.confirmKind = confirmKindDelete
	m.confirmTaskID = t.ID
}

// openCompleteModal stages finalizing the selected task — only eligible once
// it's actually been executed (has a worktree) and is ready for review — and
// prompts for the feature branch name the committed work should land on.
func (m *Model) openCompleteModal() {
	t, ok := m.tasklist.Selected()
	if !ok || t.Status != domain.StatusReadyForReview || t.WorktreePath == "" {
		return
	}
	m.completeTaskID = t.ID
	m.completeInput.SetValue(suggestBranchName(t))
	m.completeInput.CursorEnd()
	m.completeModalOpen = true
}

// updateCompleteModal handles input while the branch-name prompt is open:
// enter commits the worktree's changes onto the given branch and removes
// the worktree (see complete.go), esc cancels without touching anything.
func (m Model) updateCompleteModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.completeModalOpen = false
		m.completeInput.Blur()
		return m, nil
	case "enter":
		branch := strings.TrimSpace(m.completeInput.Value())
		m.completeModalOpen = false
		m.completeInput.Blur()
		if branch == "" {
			return m, nil
		}
		cmd := m.startCompleteTask(branch)
		return m, cmd
	}
	var cmd tea.Cmd
	m.completeInput, cmd = m.completeInput.Update(msg)
	return m, cmd
}

// startCompleteTask kicks off finalizing the task openCompleteModal staged
// (by ID, not the current selection — the modal blocks all other input so
// it can't have changed, but this matches openDeleteConfirm's pattern of
// resolving the target once, up front).
func (m *Model) startCompleteTask(branch string) tea.Cmd {
	var target domain.Task
	found := false
	for _, t := range m.tasks {
		if t.ID == m.completeTaskID {
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
		m.appendLogLine(target.ID, logCormakeLine("cannot complete — task has no repo assigned"))
		return nil
	}
	m.appendLogLine(target.ID, logCormakeLine("finalizing onto branch "+branch))
	return completeTaskCmd(target.ID, repoPath, target.WorktreePath, branch, "cormake: "+target.Title)
}

// handleCompleteFinished reacts to a finalize sequence ending: success moves
// the task to Complete and records the branch it landed on, clearing
// WorktreePath since that directory no longer exists (also keeps Review
// from later trying to diff-review a worktree that's gone); a failure is
// just logged, leaving the task exactly as it was — ReadyForReview with its
// worktree intact — so nothing is lost and completing can be retried.
func (m *Model) handleCompleteFinished(msg completeFinishedMsg) {
	if msg.err != nil {
		m.appendLogLine(msg.taskID, logCormakeLine("failed to complete: "+msg.err.Error()))
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

// suggestBranchName derives a starting-point feature-branch name from a
// task's title — lowercased, non-alphanumeric runs collapsed to a single
// "-". Just a default shown in the complete modal's input; the user can
// freely edit it before confirming.
func suggestBranchName(t domain.Task) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(t.Title) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	name := strings.TrimRight(b.String(), "-")
	if name == "" {
		name = shortTaskID(t.ID)
	}
	if len(name) > 50 {
		name = strings.TrimRight(name[:50], "-")
	}
	return "feature/" + name
}

// updateConfirmModal handles input while the confirmation prompt is open:
// y/enter carries out whatever's staged (a status change or a delete),
// anything else (n/esc/q) cancels without changing anything.
func (m Model) updateConfirmModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		m.confirmModalOpen = false
		if m.confirmKind == confirmKindDelete {
			m.deleteTask(m.confirmTaskID)
			return m, nil
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
	default:
		m.confirmModalOpen = false
		return m, nil
	}
}

// updateNewTaskModal handles input while the new-task title prompt is open:
// enter creates the task (if the title isn't blank) and closes the modal,
// esc closes it without creating anything, everything else is forwarded to
// the text input.
func (m Model) updateNewTaskModal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.newTaskModalOpen = false
		m.newTaskInput.Blur()
		return m, nil
	case "enter":
		title := strings.TrimSpace(m.newTaskInput.Value())
		m.newTaskModalOpen = false
		m.newTaskInput.Blur()
		if title != "" {
			m.createTask(title)
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.newTaskInput, cmd = m.newTaskInput.Update(msg)
	return m, cmd
}

// createTask adds a new TODO task to the active workspace with just a
// title, defaulting to the workspace's first repo if it has one (there's
// no repo picker yet). The description gets filled in later via the
// [enter] edit-in-editor flow.
func (m *Model) createTask(title string) {
	if len(m.workspaces) == 0 {
		return
	}
	ws := &m.workspaces[m.activeWS]
	t := domain.Task{
		ID:          uuid.NewString(),
		DisplayID:   ws.NextDisplayID(),
		WorkspaceID: ws.ID,
		Title:       title,
		Status:      domain.StatusTodo,
		Source:      "manual",
		CreatedAt:   time.Now(),
	}
	if len(ws.Repos) > 0 {
		t.RepoID = ws.Repos[0].ID
	}
	m.tasks = append(m.tasks, t)
	m.persistTask(t)
	m.persistWorkspaces()
	m.showArchive = false // a fresh TODO task belongs in the Open view
	m.refreshTaskList()
	m.tasklist.SelectByID(t.ID)
	m.syncDetail()
}

// persistTask saves t to disk, best-effort: there's no toast/error-banner
// UI yet (future polish), so a failed save is logged to stderr rather than
// crashing the TUI — a deliberate, temporary simplification.
func (m *Model) persistTask(t domain.Task) {
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

// startPlanRun spawns a real claude plan-mode run for the selected task.
// Eligibility was already checked once in openConfirm before the
// confirmation modal appeared; nothing else could change the task's status
// in between (the modal blocks all other input), so this doesn't
// re-validate against confirmFrom.
func (m *Model) startPlanRun() tea.Cmd {
	t, ok := m.tasklist.Selected()
	if !ok {
		return nil
	}
	return m.runPlanAgent(t, buildPrompt(t), "")
}

// runPlanAgent spawns a plan-mode claude run for t with the given prompt,
// shared by the initial Plan action and the revise-after-review-feedback
// flow (see revdiff.go). resumeSessionID, when non-empty, continues t's
// existing session instead of starting a fresh one — used so claude has
// full context of the plan it already proposed when revising it.
func (m *Model) runPlanAgent(t domain.Task, prompt, resumeSessionID string) tea.Cmd {
	repoPath, ok := m.repoPath(t.RepoID)
	if !ok || repoPath == "" {
		m.appendLogLine(t.ID, logCormakeLine("cannot start — task has no repo assigned"))
		return nil
	}

	spec := agent.RunSpec{
		TaskID:          t.ID,
		Prompt:          prompt,
		RepoPath:        repoPath,
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

	handle, err := m.runner.Start(context.Background(), spec)
	if err != nil {
		m.appendLogLine(t.ID, logCormakeLine("failed to start: "+err.Error()))
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
			m.tasks[i].PID = handle.PID
			m.persistTask(m.tasks[i])
			break
		}
	}
	m.refreshTaskList()

	m.active[t.ID] = handle
	go forwardEvents(m.eventsCh, t.ID, handle)

	return nil
}

// startExecuteRun spawns a fresh real claude Complete-mode run for the
// selected task. Eligibility (TODO/PLANNED) was already checked once in
// openConfirm before the confirmation modal appeared.
func (m *Model) startExecuteRun() tea.Cmd {
	t, ok := m.tasklist.Selected()
	if !ok {
		return nil
	}
	return m.runExecuteAgent(t, buildExecutePrompt(t), "")
}

// runExecuteAgent spawns a Complete-mode claude run for t with the given
// prompt, shared by the initial Execute action and the
// revise-after-code-review flow (see revdiff.go). resumeSessionID, when
// non-empty, continues t's existing session in its existing worktree
// instead of starting a fresh one.
//
// A fresh run creates the worktree itself (see createWorktree in
// complete.go) rather than asking claude to via -w/--worktree: confirmed
// directly that -w forks from the repo's remote-tracking default branch
// when one is configured, not local HEAD, silently dropping local-only
// commits. Creating it ourselves and pointing RepoPath (-> cmd.Dir) straight
// at it sidesteps that entirely, and also means WorktreePath is known
// synchronously here rather than waited on from the run's first event.
func (m *Model) runExecuteAgent(t domain.Task, prompt, resumeSessionID string) tea.Cmd {
	repoPath, ok := m.repoPath(t.RepoID)
	if !ok || repoPath == "" {
		m.appendLogLine(t.ID, logCormakeLine("cannot start — task has no repo assigned"))
		return nil
	}

	sessionID := resumeSessionID
	wtName := t.WorktreeName
	baseRef := t.WorktreeBaseRef
	worktreePath := t.WorktreePath

	if sessionID == "" {
		sessionID = uuid.NewString()
		wtName = worktreeName(t)
		baseRef = gitHeadRef(repoPath)
		path, err := createWorktree(repoPath, wtName)
		if err != nil {
			m.appendLogLine(t.ID, logCormakeLine("failed to create worktree: "+err.Error()))
			return nil
		}
		worktreePath = path
	}
	if worktreePath == "" {
		m.appendLogLine(t.ID, logCormakeLine("cannot resume — task has no worktree"))
		return nil
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

	handle, err := m.runner.Start(context.Background(), spec)
	if err != nil {
		m.appendLogLine(t.ID, logCormakeLine("failed to start: "+err.Error()))
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
			m.persistTask(m.tasks[i])
			break
		}
	}
	m.refreshTaskList()

	m.active[t.ID] = handle
	go forwardEvents(m.eventsCh, t.ID, handle)

	return nil
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
		m.appendLogLine(msg.taskID, logCormakeLine("revdiff failed: "+msg.err.Error()))
		return nil
	}
	if msg.annotations == "" {
		return nil
	}

	for _, t := range m.tasks {
		if t.ID != msg.taskID {
			continue
		}
		m.appendLogLine(t.ID, logCormakeLine("sending review feedback to claude for revision"))
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
// proper summary of the actual work done. It matters because the run's
// final message becomes ev.ResultText -> Task.ResultSummary (see
// handleAgentEvent's EventResult case) — cormake's Summary tab and the
// description shown alongside a code review (see openRevdiffDiffCmd)
// display that text verbatim, so without this it could just as easily end
// on a trailing question or a bare "done".
const executeSummaryInstruction = "\n\nWhen you are finished, your final message must be a concise summary " +
	"of what you actually implemented (not a question, not a list of possible next steps) — it's stored " +
	"and shown to the user as this task's result summary."

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
// which affect how the line reads, so they're kept out of formatAgentLogLine
// entirely.
func (m *Model) handleAgentEvent(ev agent.Event) {
	m.appendLogLine(ev.TaskID, formatAgentLogLine(ev))

	switch ev.Type {
	case agent.EventToolUse:
		if path, ok := extractPlanFilePath(ev.ToolName, ev.ToolInput); ok {
			m.setPlanFilePath(ev.TaskID, path)
		}
	case agent.EventResult:
		m.resultSeen[ev.TaskID] = true
		for i := range m.tasks {
			if m.tasks[i].ID == ev.TaskID {
				m.tasks[i].ResultSummary = ev.ResultText
				m.tasks[i].Cost = ev.CostUSD
				break
			}
		}
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
func (m *Model) handleTaskFinished(msg taskFinishedMsg) {
	delete(m.active, msg.TaskID)
	sawResult := m.resultSeen[msg.TaskID]
	delete(m.resultSeen, msg.TaskID)

	for i := range m.tasks {
		if m.tasks[i].ID != msg.TaskID {
			continue
		}
		m.tasks[i].PID = 0
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
//     m.runner.Attach) from the stored offset. No new process is spawned;
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
func (m *Model) reconnectTask(taskID string) {
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
		return
	}

	if t.PID != 0 && claude.ProcessAlive(t.PID) {
		offset, err := m.store.LoadOffset(t.ID)
		if err != nil {
			fmt.Fprintln(os.Stderr, "cormake: failed to load offset for", t.ID+":", err)
		}
		handle, err := m.runner.Attach(context.Background(), agent.AttachSpec{
			TaskID:        t.ID,
			PID:           t.PID,
			RawStdoutPath: m.store.RawStdoutPath(t.ID),
			RawStderrPath: m.store.RawStderrPath(t.ID),
			Offset:        offset,
		})
		if err == nil {
			m.appendLogLine(t.ID, logCormakeLine(fmt.Sprintf("cormake restarted — reattaching to still-running session (pid %d)", t.PID)))
			m.active[t.ID] = handle
			go forwardEvents(m.eventsCh, t.ID, handle)
			return
		}
		fmt.Fprintln(os.Stderr, "cormake: failed to attach to pid", t.PID, "for", t.ID+":", err)
	}

	offset, err := m.store.LoadOffset(t.ID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cormake: failed to load offset for", t.ID+":", err)
	}
	events, newOffset := claude.ReplayFile(t.ID, m.store.RawStdoutPath(t.ID), offset)
	for _, ev := range events {
		m.handleAgentEvent(ev)
	}
	if err := m.store.SaveOffset(t.ID, newOffset); err != nil {
		fmt.Fprintln(os.Stderr, "cormake: failed to save offset for", t.ID+":", err)
	}
	if m.resultSeen[t.ID] {
		m.appendLogLine(t.ID, logCormakeLine("cormake restarted — the run had already finished"))
		m.handleTaskFinished(taskFinishedMsg{TaskID: t.ID})
		return
	}

	m.appendLogLine(t.ID, logCormakeLine("cormake restarted — reconnecting to interrupted session"))

	switch t.Status {
	case domain.StatusPlanning:
		m.runPlanAgent(t, reconnectPlanPrompt, t.SessionID)
	case domain.StatusInProgress:
		if t.WorktreePath == "" {
			m.appendLogLine(t.ID, logCormakeLine("cannot reconnect — task has no worktree on record"))
			return
		}
		m.runExecuteAgent(t, reconnectExecutePrompt, t.SessionID)
	}
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
// and the active/archive view toggle: the default view is everything still
// actionable (todo, planning, in progress, awaiting input, ready for
// review), while the archive view holds tasks that reached a terminal
// outcome (complete, failed, or cancelled).
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
		if t.IsArchived() != m.showArchive {
			continue
		}
		filtered = append(filtered, t)
	}
	m.tasklist.SetTasks(filtered)
	m.syncDetail()
}

func (m *Model) syncDetail() {
	t, ok := m.tasklist.Selected()
	if !ok {
		msg := "No tasks yet.\n\nPress n to create one."
		if m.showArchive {
			msg = "No archived tasks."
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

func (m *Model) recalcLayout() {
	leftTotal, rightTotal, contentHeight := m.paneDims()
	m.tasklist.SetSize(leftTotal-paneOverhead, contentHeight)
	m.detail.SetSize(rightTotal-paneOverhead, contentHeight)
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	if m.workspaceModalOpen {
		return m.renderWorkspaceModal()
	}

	if m.newTaskModalOpen {
		return m.renderNewTaskModal()
	}

	if m.completeModalOpen {
		return m.renderCompleteModal()
	}

	if m.confirmModalOpen {
		return m.renderConfirmModal()
	}

	top := m.renderTabBar()
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

// renderTabBar renders the Open/Archived tabs on the left and the current
// workspace name on the right; switching workspaces happens through the
// [w] picker modal, not these tabs.
func (m Model) renderTabBar() string {
	open, archived := " Open ", " Archived "
	if !m.showArchive {
		open = activeTabStyle().Render("[Open]")
		archived = inactiveTabStyle.Render(archived)
	} else {
		open = inactiveTabStyle.Render(open)
		archived = activeTabStyle().Render("[Archived]")
	}
	tabs := " " + open + "  " + archived

	wsInfo := tabInfoStyle.Render("workspace: " + m.currentWorkspaceName())

	gap := m.width - lipgloss.Width(tabs) - lipgloss.Width(wsInfo)
	if gap < 1 {
		gap = 1
	}
	bar := tabs + strings.Repeat(" ", gap) + wsInfo
	return lipgloss.NewStyle().Width(m.width).MaxWidth(m.width).Height(1).MaxHeight(1).Render(bar)
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

// renderNewTaskModal draws the new-task title prompt, centered over the
// full screen.
func (m Model) renderNewTaskModal() string {
	lines := []string{
		"New task", "",
		m.newTaskInput.View(), "",
		tabInfoStyle.Render("[enter] create   [esc] cancel"),
	}
	box := focusedPaneBorderStyle().Padding(1, 3).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// renderCompleteModal draws the mark-complete branch-name prompt, centered
// over the full screen.
func (m Model) renderCompleteModal() string {
	lines := []string{
		"Mark task complete", "",
		"Commit changes and finalize onto branch:", "",
		m.completeInput.View(), "",
		tabInfoStyle.Render("[enter] complete   [esc] cancel"),
	}
	box := focusedPaneBorderStyle().Padding(1, 3).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// renderConfirmModal draws the Plan/Execute confirmation prompt, centered
// over the full screen.
func (m Model) renderConfirmModal() string {
	lines := []string{
		m.confirmMessage, "",
		tabInfoStyle.Render("[y]es   [n]o"),
	}
	box := focusedPaneBorderStyle().Padding(1, 3).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) renderBody() string {
	leftStyle := focusedPaneBorderStyle()
	rightStyle := paneBorderStyle

	leftTotal, rightTotal, contentHeight := m.paneDims()

	left := leftStyle.Width(leftTotal - paneOverhead).Height(contentHeight).Render(m.tasklist.View())
	right := rightStyle.Width(rightTotal - paneOverhead).Height(contentHeight).Render(m.detail.View())

	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}
