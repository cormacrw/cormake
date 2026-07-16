// Package ui implements the bubbletea root model for cormake's two-pane
// TUI shell: a task list on the left and a task detail/log view on the
// right, under a workspace tab bar and above a keybinding footer.
package ui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"cormake/internal/domain"
	"cormake/internal/ui/detail"
	"cormake/internal/ui/tasklist"
)

type focusZone int

const (
	focusList focusZone = iota
	focusDetail
)

const (
	topBarHeight = 1
	footerHeight = 1
	listWidthPct = 0.30
)

type Model struct {
	workspaces []domain.Workspace
	activeWS   int
	tasks      []domain.Task

	workspaceNames map[string]string
	repoNames      map[string]string

	tasklist tasklist.Model
	detail   detail.Model

	focus         focusZone
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
		focus:          focusList,
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

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, keys.Tab):
			m.toggleFocus()
			return m, nil

		case key.Matches(msg, keys.Left):
			m.switchWorkspace(-1)
			return m, nil

		case key.Matches(msg, keys.Right):
			m.switchWorkspace(1)
			return m, nil

		case isDigitOneToThree(msg.String()):
			m.setWorkspace(int(msg.String()[0] - '1'))
			return m, nil

		case key.Matches(msg, keys.NewTask, keys.Open, keys.Cancel, keys.Workspaces, keys.Help):
			// Reserved: needs real storage/orchestrator wiring that doesn't
			// exist yet. Intentionally a no-op rather than pretending to work.
			return m, nil
		}
	}

	var cmd tea.Cmd
	switch m.focus {
	case focusList:
		m.tasklist, cmd = m.tasklist.Update(msg)
		m.syncDetail()
	case focusDetail:
		m.detail, cmd = m.detail.Update(msg)
	}
	return m, cmd
}

func isDigitOneToThree(s string) bool {
	return len(s) == 1 && s[0] >= '1' && s[0] <= '3'
}

func (m *Model) toggleFocus() {
	if m.focus == focusList {
		m.focus = focusDetail
	} else {
		m.focus = focusList
	}
}

func (m *Model) switchWorkspace(delta int) {
	m.setWorkspace(m.activeWS + delta)
}

func (m *Model) setWorkspace(idx int) {
	n := len(m.workspaces)
	if n == 0 {
		return
	}
	m.activeWS = ((idx % n) + n) % n
	m.refreshTaskList()
}

func (m *Model) refreshTaskList() {
	if len(m.workspaces) == 0 {
		return
	}
	activeID := m.workspaces[m.activeWS].ID
	filtered := make([]domain.Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		if t.WorkspaceID == activeID {
			filtered = append(filtered, t)
		}
	}
	m.tasklist.SetTasks(filtered)
	m.syncDetail()
}

func (m *Model) syncDetail() {
	t, ok := m.tasklist.Selected()
	if !ok {
		m.detail.SetTask(domain.Task{Title: "no tasks in this workspace"}, m.currentWorkspaceName(), "")
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

	top := m.renderTabBar()
	body := m.renderBody()
	footer := footerStyle.Width(m.width).Render(footerHelp)

	return lipgloss.JoinVertical(lipgloss.Left, top, body, footer)
}

func (m Model) renderTabBar() string {
	var rendered []string
	for i, w := range m.workspaces {
		label := " " + w.Name + " "
		if i == m.activeWS {
			label = "[" + w.Name + "]"
			rendered = append(rendered, activeTabStyle.Render(label))
		} else {
			rendered = append(rendered, inactiveTabStyle.Render(label))
		}
	}
	bar := " workspaces:  " + lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
	return lipgloss.NewStyle().Width(m.width).Render(bar)
}

func (m Model) renderBody() string {
	leftStyle := paneBorderStyle
	rightStyle := paneBorderStyle
	if m.focus == focusList {
		leftStyle = focusedPaneBorderStyle
	} else {
		rightStyle = focusedPaneBorderStyle
	}

	leftTotal, rightTotal, contentHeight := m.paneDims()

	left := leftStyle.Width(leftTotal - paneOverhead).Height(contentHeight).Render(m.tasklist.View())
	right := rightStyle.Width(rightTotal - paneOverhead).Height(contentHeight).Render(m.detail.View())

	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}
