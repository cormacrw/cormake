// Package ui implements the bubbletea root model for cormake's two-pane
// TUI shell: a task list on the left and a task detail/log view on the
// right, under a workspace tab bar and above a keybinding footer.
package ui

import (
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"cormake/internal/domain"
	"cormake/internal/ui/detail"
	"cormake/internal/ui/tasklist"
)

const (
	topBarHeight = 1
	footerHeight = 1
	listWidthPct = 0.30
)

type Model struct {
	workspaces  []domain.Workspace
	activeWS    int
	tasks       []domain.Task
	showArchive bool

	workspaceModalOpen bool
	workspaceCursor    int

	workspaceNames map[string]string
	repoNames      map[string]string

	tasklist tasklist.Model
	detail   detail.Model

	width, height int
}

func New() Model {
	data := newSampleData()

	wsNames := make(map[string]string, len(data.Workspaces))
	repoNames := make(map[string]string)
	for _, w := range data.Workspaces {
		wsNames[w.ID] = w.Name
		for _, r := range w.Repos {
			repoNames[r.ID] = r.Name
		}
	}

	m := Model{
		workspaces:     data.Workspaces,
		tasks:          data.Tasks,
		workspaceNames: wsNames,
		repoNames:      repoNames,
		tasklist:       tasklist.New(nil),
		detail:         detail.New(data.Logs),
	}
	m.refreshTaskList()
	return m
}

func (m Model) Init() tea.Cmd {
	return nil
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

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		if m.workspaceModalOpen {
			return m.updateWorkspaceModal(msg)
		}

		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, keys.PgUp, keys.PgDown):
			cmd := m.detail.ScrollLog(msg)
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

		case key.Matches(msg, keys.Open):
			if t, ok := m.tasklist.Selected(); ok {
				return m, openInEditorCmd(t)
			}
			return m, nil

		case key.Matches(msg, keys.Workspaces):
			m.workspaceCursor = m.activeWS
			m.workspaceModalOpen = true
			return m, nil

		case key.Matches(msg, keys.Plan):
			m.advanceSelected(domain.StatusPlanning, domain.StatusTodo)
			return m, nil

		case key.Matches(msg, keys.Execute):
			m.advanceSelected(domain.StatusInProgress, domain.StatusTodo, domain.StatusPlanned)
			return m, nil

		case key.Matches(msg, keys.NewTask, keys.Cancel, keys.Help):
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
	}
	if idx, ok := digitIndex(msg.String()); ok && idx < len(m.workspaces) {
		m.setWorkspace(idx)
		m.workspaceModalOpen = false
	}
	return m, nil
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
		switch {
		case m.tasks[i].Status == domain.StatusArchived:
			m.tasks[i].Status = m.tasks[i].PreviousStatus
			m.tasks[i].PreviousStatus = ""
		case m.tasks[i].CanArchive():
			m.tasks[i].PreviousStatus = m.tasks[i].Status
			m.tasks[i].Status = domain.StatusArchived
		}
		break
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
			break
		}
	}
	m.refreshTaskList()
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
		placeholder := "no tasks in this workspace"
		if m.showArchive {
			placeholder = "no archived tasks in this workspace"
		}
		m.detail.SetTask(domain.Task{Title: placeholder}, m.currentWorkspaceName(), "")
		return
	}
	m.detail.SetTask(t, m.workspaceNames[t.WorkspaceID], m.repoNames[t.RepoID])
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

	top := m.renderTabBar()
	body := m.renderBody()
	// MaxWidth, not just Width: Width alone pads short content but wraps
	// (rather than truncates) content wider than it, which silently adds an
	// extra line and pushes the tab bar off-screen — bit us once already.
	footer := footerStyle.Width(m.width).MaxWidth(m.width).Render(footerHelp)

	return lipgloss.JoinVertical(lipgloss.Left, top, body, footer)
}

// renderTabBar renders the Open/Archived tabs on the left and the current
// workspace name on the right; switching workspaces happens through the
// [w] picker modal, not these tabs.
func (m Model) renderTabBar() string {
	open, archived := " Open ", " Archived "
	if !m.showArchive {
		open = activeTabStyle.Render("[Open]")
		archived = inactiveTabStyle.Render(archived)
	} else {
		open = inactiveTabStyle.Render(open)
		archived = activeTabStyle.Render("[Archived]")
	}
	tabs := " " + open + "  " + archived

	wsInfo := tabInfoStyle.Render("workspace: " + m.currentWorkspaceName())

	gap := m.width - lipgloss.Width(tabs) - lipgloss.Width(wsInfo)
	if gap < 1 {
		gap = 1
	}
	bar := tabs + strings.Repeat(" ", gap) + wsInfo
	return lipgloss.NewStyle().Width(m.width).MaxWidth(m.width).Render(bar)
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
			name = activeTabStyle.Render(name)
		}
		lines = append(lines, marker+name)
	}
	lines = append(lines, "", tabInfoStyle.Render("[enter] select   [esc] cancel"))

	box := focusedPaneBorderStyle.Padding(1, 3).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) renderBody() string {
	leftStyle := focusedPaneBorderStyle
	rightStyle := paneBorderStyle

	leftTotal, rightTotal, contentHeight := m.paneDims()

	left := leftStyle.Width(leftTotal - paneOverhead).Height(contentHeight).Render(m.tasklist.View())
	right := rightStyle.Width(rightTotal - paneOverhead).Height(contentHeight).Render(m.detail.View())

	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}
