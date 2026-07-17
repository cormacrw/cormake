// Package detail renders the right-hand side of the app: a fixed header
// (title, metadata, tab bar) over a single scrollable content area that
// shows whichever tab is active — Description, Plan (only when the task
// has one), or Log. Editing happens in an external editor (see
// internal/ui/editor.go), not in this pane.
package detail

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"cormake/internal/domain"
	"cormake/internal/ui/theme"
)

var (
	titleStyle       = lipgloss.NewStyle().Bold(true)
	metaStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	dividerStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	inactiveTabStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

func activeTabStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Accent()).Bold(true)
}

// Tab is which content the pane's single scrollable area is showing.
type Tab int

const (
	TabDescription Tab = iota
	TabPlan
	TabSummary
	TabLog
)

// headerHeight is the fixed number of rows the title/meta/tab-bar/divider
// block occupies; everything else goes to the active tab's content.
const headerHeight = 4

type Model struct {
	Viewport viewport.Model // content area, shared across tabs (only one is visible at a time)

	task     domain.Task
	repoName string
	logs     map[string][]string

	// planContent is the plan file's content, read from disk by the caller
	// (claude writes it to ~/.claude/plans/, not anywhere this package
	// knows about) and handed in via SetTask — this package just renders it.
	planContent string

	renderedDescription string
	renderedPlan        string
	renderedSummary     string

	activeTab Tab

	empty        bool
	emptyMessage string

	width, height int
}

func New(logs map[string][]string) Model {
	return Model{
		Viewport: viewport.New(0, 0),
		logs:     logs,
	}
}

func (m *Model) SetSize(w, h int) {
	m.width = w
	m.height = h

	contentHeight := h - headerHeight
	if contentHeight < 1 {
		contentHeight = 1
	}
	m.Viewport.Width = w
	m.Viewport.Height = contentHeight

	m.refreshRendered()
	m.syncViewportContent()
}

// SetTask switches the displayed task, refreshing all tab content and
// resetting to the Description tab — a predictable default rather than
// carrying over whatever tab happened to be active for the last task.
// repoName is the resolved display name, since Task only stores a RepoID.
// planContent is the plan file's content already read from disk, or empty
// if the task has no plan yet.
func (m *Model) SetTask(t domain.Task, repoName, planContent string) {
	m.empty = false
	m.task = t
	m.repoName = repoName
	m.planContent = planContent
	m.activeTab = TabDescription
	m.refreshRendered()
	m.syncViewportContent()
}

// SetEmpty replaces the normal task view with a centered message — used
// when there's no task to show at all (e.g. an empty workspace/view),
// rather than rendering a fake task with a placeholder title.
func (m *Model) SetEmpty(msg string) {
	m.empty = true
	m.emptyMessage = msg
}

// HasPlan reports whether the task has plan content to show.
func (m Model) HasPlan() bool {
	return strings.TrimSpace(m.planContent) != ""
}

// HasSummary reports whether the task has a final result message (the last
// thing claude said when it finished a run) to show.
func (m Model) HasSummary() bool {
	return strings.TrimSpace(m.task.ResultSummary) != ""
}

// ShowDescription, ShowPlan, ShowSummary, and ShowLog switch the active tab.
// ShowPlan/ShowSummary are no-ops when the task has no plan/summary yet —
// same "ignore, don't pretend" pattern as the Plan/Execute keys.
func (m *Model) ShowDescription() {
	m.activeTab = TabDescription
	m.syncViewportContent()
}

func (m *Model) ShowPlan() {
	if !m.HasPlan() {
		return
	}
	m.activeTab = TabPlan
	m.syncViewportContent()
}

func (m *Model) ShowSummary() {
	if !m.HasSummary() {
		return
	}
	m.activeTab = TabSummary
	m.syncViewportContent()
}

func (m *Model) ShowLog() {
	m.activeTab = TabLog
	m.syncViewportContent()
}

// syncViewportContent loads whichever tab is active into the shared
// viewport and scrolls back to the top.
func (m *Model) syncViewportContent() {
	switch m.activeTab {
	case TabPlan:
		m.Viewport.SetContent(m.renderedPlan)
	case TabSummary:
		m.Viewport.SetContent(m.renderedSummary)
	case TabLog:
		m.Viewport.SetContent(strings.Join(m.logs[m.task.ID], "\n"))
	default:
		m.Viewport.SetContent(m.renderedDescription)
	}
	m.Viewport.SetYOffset(0)
}

// AppendLogLine adds a line to a task's log, live — used while a task is
// actively running. If that task's Log tab is the one currently on screen,
// the viewport updates immediately and stays scrolled to the bottom (tail
// -f style); otherwise the line just waits in m.logs for whenever the user
// switches to it.
//
// The viewport itself doesn't wrap long lines — anything wider than it just
// overflows and gets hard-clipped/wrapped by the terminal at whatever
// column it happens to hit, mid-word, even slicing into the pane's own
// border character (confirmed directly). Wrapping here, at append time
// while m.width is known, keeps every line's visible width honest instead.
func (m *Model) AppendLogLine(taskID, line string) {
	if m.width > 0 {
		line = lipgloss.NewStyle().Width(m.width).Render(line)
	}
	m.logs[taskID] = append(m.logs[taskID], line)
	if m.task.ID == taskID && m.activeTab == TabLog {
		m.Viewport.SetContent(strings.Join(m.logs[taskID], "\n"))
		m.Viewport.GotoBottom()
	}
}

// Scroll forwards a paging key straight to the content viewport, whichever
// tab it's currently showing.
func (m *Model) Scroll(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.Viewport, cmd = m.Viewport.Update(msg)
	return cmd
}

func (m Model) View() string {
	if m.empty {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			metaStyle.Render(m.emptyMessage))
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.renderHeader(), m.Viewport.View())
}

// renderHeader always produces headerHeight lines (title, meta, tab bar,
// divider) — pinning Height/MaxHeight to that forces truncation instead of
// letting an over-length line wrap the block taller, which would silently
// eat into the content viewport's height below it.
func (m Model) renderHeader() string {
	stage, glyph := m.task.DisplayStage()
	title := titleStyle.Render(truncate(m.task.Title, m.width-8)) + " #" + shortID(m.task.ID)
	// Workspace isn't shown here — the app's top bar already names the
	// active workspace, so repeating it in every task's header is redundant.
	metaText := fmt.Sprintf("repo: %s   %s %s", m.repoName, glyph, stage)
	meta := metaStyle.Render(truncate(metaText, m.width))
	tabBar := m.renderTabBar()
	divider := dividerStyle.Render(strings.Repeat("─", max0(m.width)))

	content := strings.Join([]string{title, meta, tabBar, divider}, "\n")
	return lipgloss.NewStyle().Width(m.width).MaxWidth(m.width).Height(headerHeight).MaxHeight(headerHeight).Render(content)
}

// visibleTabs is the ordered set of tabs currently shown, respecting
// whether the task has a plan and/or a result summary to show.
func (m Model) visibleTabs() []Tab {
	tabs := []Tab{TabDescription}
	if m.HasPlan() {
		tabs = append(tabs, TabPlan)
	}
	if m.HasSummary() {
		tabs = append(tabs, TabSummary)
	}
	return append(tabs, TabLog)
}

func tabLabel(t Tab) string {
	switch t {
	case TabPlan:
		return "Plan"
	case TabSummary:
		return "Summary"
	case TabLog:
		return "Log"
	default:
		return "Description"
	}
}

// CycleTab moves to the next (delta > 0) or previous (delta < 0) visible
// tab, wrapping around.
func (m *Model) CycleTab(delta int) {
	tabs := m.visibleTabs()
	idx := 0
	for i, t := range tabs {
		if t == m.activeTab {
			idx = i
			break
		}
	}
	idx = ((idx+delta)%len(tabs) + len(tabs)) % len(tabs)
	m.activeTab = tabs[idx]
	m.syncViewportContent()
}

func (m Model) renderTabBar() string {
	tabs := m.visibleTabs()
	rendered := make([]string, len(tabs))
	for i, t := range tabs {
		label := tabLabel(t)
		if t == m.activeTab {
			rendered[i] = activeTabStyle().Render("[" + label + "]")
		} else {
			rendered[i] = inactiveTabStyle.Render(" " + label + " ")
		}
	}
	return strings.Join(rendered, " ")
}

// refreshRendered re-runs glamour for the description and (if present) plan
// and result summary, which is cheap enough per task/resize but not worth
// doing on every keystroke, so it's only called from SetSize and SetTask.
func (m *Model) refreshRendered() {
	m.renderedDescription = renderMarkdown(m.task.Description, m.width, "_no description_")
	if m.HasPlan() {
		m.renderedPlan = renderMarkdown(m.planContent, m.width, "_no plan_")
	} else {
		m.renderedPlan = ""
	}
	if m.HasSummary() {
		m.renderedSummary = renderMarkdown(m.task.ResultSummary, m.width, "_no summary_")
	} else {
		m.renderedSummary = ""
	}
}

func renderMarkdown(md string, width int, emptyText string) string {
	if strings.TrimSpace(md) == "" {
		return metaStyle.Render(emptyText)
	}
	if width < 1 {
		width = 1
	}
	// WithStandardStyle, not WithAutoStyle: auto-detection queries the
	// terminal's background color over stdin, which races bubbletea's own
	// input reader on the same file descriptor and intermittently freezes
	// the whole program. A fixed style avoids that terminal round-trip.
	r, err := glamour.NewTermRenderer(glamour.WithStandardStyle("dark"), glamour.WithWordWrap(width))
	if err != nil {
		return md
	}
	out, err := r.Render(md)
	if err != nil {
		return md
	}
	return strings.TrimRight(out, "\n")
}

func shortID(id string) string {
	if len(id) > 4 {
		return id[:4]
	}
	return id
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

func truncate(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	if maxWidth == 1 {
		return "…"
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes)+"…") > maxWidth {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}
