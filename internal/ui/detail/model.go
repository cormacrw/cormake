// Package detail renders the right-hand side of the app: a fixed header
// (title, metadata, tab bar) over a single scrollable content area that
// shows whichever tab is active — Description, Plan/Summary/PR tabs (only
// when applicable to the task), or Log. Editing happens in an external
// editor (see internal/ui/editor.go), not in this pane.
package detail

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"cormake/internal/domain"
	"cormake/internal/logformat"
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
	TabPRDescription
	TabPRComments
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
	renderedPRDesc      string
	renderedPRComments  string

	// prSnapshots holds the last-polled PR data per task ID (see
	// ui.handlePRQuery), independent of which task is currently displayed —
	// same "keep everything, render whichever is active" pattern as logs.
	prSnapshots map[string]domain.PRSnapshot

	activeTab Tab

	empty        bool
	emptyMessage string

	width, height int
}

func New(logs map[string][]string) Model {
	return Model{
		Viewport:    viewport.New(0, 0),
		logs:        logs,
		prSnapshots: make(map[string]domain.PRSnapshot),
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
// keeping whichever tab is currently active — e.g. scrolling through the
// task list while on the Log tab keeps showing each task's log — falling
// back to Description only if the new task doesn't have that tab (no plan
// or summary yet).
// repoName is the resolved display name, since Task only stores a RepoID.
// planContent is the plan file's content already read from disk, or empty
// if the task has no plan yet.
func (m *Model) SetTask(t domain.Task, repoName, planContent string) {
	m.empty = false
	m.task = t
	m.repoName = repoName
	m.planContent = planContent
	m.refreshRendered()
	if !m.activeTabVisible() {
		m.activeTab = TabDescription
	}
	m.syncViewportContent()
}

// activeTabVisible reports whether the currently active tab is still one of
// the tabs visible for the (possibly just-switched-to) task.
func (m Model) activeTabVisible() bool {
	for _, t := range m.visibleTabs() {
		if t == m.activeTab {
			return true
		}
	}
	return false
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

// HasPR reports whether the task has an associated PR to show — gated on
// Task.PRNumber (persisted) rather than on a snapshot actually being cached
// yet, so the tabs appear immediately once a PR is opened even before the
// first poll comes back (see refreshRendered's "loading" placeholder for
// that gap).
func (m Model) HasPR() bool {
	return m.task.PRNumber != 0
}

// SetPRSnapshot records the latest polled PR data for taskID (see
// ui.handlePRQuery) and, if that task is the one currently displayed,
// re-renders and refreshes the viewport immediately.
func (m *Model) SetPRSnapshot(taskID string, snap domain.PRSnapshot) {
	m.prSnapshots[taskID] = snap
	if m.task.ID != taskID {
		return
	}
	m.refreshRendered()
	m.syncViewportContent()
}

// PRSnapshot returns the last-polled PR data for taskID, if any — used by
// the app shell to judge whether a task's PR looks merged yet (see
// ui.openCompleteModal).
func (m Model) PRSnapshot(taskID string) (domain.PRSnapshot, bool) {
	snap, ok := m.prSnapshots[taskID]
	return snap, ok
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

func (m *Model) ShowPRDescription() {
	if !m.HasPR() {
		return
	}
	m.activeTab = TabPRDescription
	m.syncViewportContent()
}

func (m *Model) ShowPRComments() {
	if !m.HasPR() {
		return
	}
	m.activeTab = TabPRComments
	m.syncViewportContent()
}

// syncViewportContent loads whichever tab is active into the shared
// viewport. The Log tab opens scrolled to the bottom (tail -f style, so the
// most recent Claude output is visible immediately); other tabs scroll back
// to the top.
func (m *Model) syncViewportContent() {
	switch m.activeTab {
	case TabPlan:
		m.Viewport.SetContent(m.renderedPlan)
	case TabSummary:
		m.Viewport.SetContent(m.renderedSummary)
	case TabPRDescription:
		m.Viewport.SetContent(m.renderedPRDesc)
	case TabPRComments:
		m.Viewport.SetContent(m.renderedPRComments)
	case TabLog:
		m.Viewport.SetContent(m.renderLog(m.task.ID))
	default:
		m.Viewport.SetContent(m.renderedDescription)
	}
	if m.activeTab == TabLog {
		m.Viewport.GotoBottom()
	} else {
		m.Viewport.SetYOffset(0)
	}
}

// renderLog joins a task's stored log lines, styling and wrapping each
// entry to the pane's current width. Wrapping happens here, at render time,
// rather than baked into each line when it's stored — m.logs may include lines
// loaded back from a previous session (see AppendLogLine), including older
// entries that were persisted with inline ANSI styling; wrapping fresh on
// every render keeps width honest for the terminal size in front of the
// user now, and applying style after wrap keeps continuation lines colored.
func (m Model) renderLog(taskID string) string {
	entries := logformat.ExpandLogLines(m.logs[taskID])
	if len(entries) == 0 {
		return ""
	}
	wrapped := make([]string, len(entries))
	for i, line := range entries {
		wrapped[i] = logformat.RenderLogLine(line, m.width)
	}
	return strings.Join(wrapped, "\n")
}

// AppendLogLine adds a line to a task's log, live — used while a task is
// actively running. If that task's Log tab is the one currently on screen,
// the viewport updates immediately and stays scrolled to the bottom (tail
// -f style); otherwise the line just waits in m.logs for whenever the user
// switches to it. Persisting it to disk is the caller's job (see
// app.go's appendLogLine) — this package has no filesystem concerns.
func (m *Model) AppendLogLine(taskID, line string) {
	m.logs[taskID] = append(m.logs[taskID], line)
	if m.task.ID == taskID && m.activeTab == TabLog {
		m.Viewport.SetContent(m.renderLog(taskID))
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
	title := titleStyle.Render(truncate(m.task.Title, m.width-8)) + " #" + displayLabel(m.task)
	// Workspace isn't shown here — the app's top bar already names the
	// active workspace, so repeating it in every task's header is redundant.
	metaText := fmt.Sprintf("repo: %s   %s %s   created %s", m.repoName, glyph, stage, relativeTime(m.task.CreatedAt))
	// Older tasks predate the target/source-branch wizard and simply have
	// neither set — skip the segment entirely rather than showing a
	// misleading blank "→".
	if m.task.TargetBranch != "" {
		source := m.task.SourceBranch
		if source == "" {
			source = domain.DefaultSourceBranch
		}
		metaText += fmt.Sprintf("   %s → %s", m.task.TargetBranch, source)
	}
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
	if m.HasPR() {
		tabs = append(tabs, TabPRDescription, TabPRComments)
	}
	return append(tabs, TabLog)
}

func tabLabel(t Tab) string {
	switch t {
	case TabPlan:
		return "Plan"
	case TabSummary:
		return "Summary"
	case TabPRDescription:
		return "PR"
	case TabPRComments:
		return "PR Comments"
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
		usage := metaStyle.Render(formatUsageLine(m.task))
		divider := dividerStyle.Render(strings.Repeat("─", max0(m.width)))
		m.renderedSummary = strings.Join([]string{usage, divider, ""}, "\n") +
			renderMarkdown(m.task.ResultSummary, m.width, "_no summary_")
	} else {
		m.renderedSummary = ""
	}
	m.refreshRenderedPR()
}

// refreshRenderedPR renders the PR-description and PR-comments tabs from
// whatever snapshot is currently on hand for the active task — a "loading"
// placeholder if the task has a PR (Task.PRNumber set) but no poll has come
// back for it yet, e.g. immediately after opening one.
func (m *Model) refreshRenderedPR() {
	if !m.HasPR() {
		m.renderedPRDesc = ""
		m.renderedPRComments = ""
		return
	}
	snap, ok := m.prSnapshots[m.task.ID]
	if !ok {
		loading := metaStyle.Render("Loading PR info…")
		m.renderedPRDesc = loading
		m.renderedPRComments = loading
		return
	}

	divider := dividerStyle.Render(strings.Repeat("─", max0(m.width)))
	header := metaStyle.Render(fmt.Sprintf("PR #%d · %s · %s", snap.Number, snap.State, snap.URL))
	m.renderedPRDesc = strings.Join([]string{header, divider, ""}, "\n") +
		renderMarkdown(snap.Title+"\n\n"+snap.Body, m.width, "_no description_")
	m.renderedPRComments = renderPRComments(snap.Comments, m.width)
}

// renderPRComments joins a PR's comments/reviews (see domain.PRComment),
// already time-ordered by queryPR, into one scrollable feed — each entry
// its own little markdown block so authors/timestamps render distinctly
// from the surrounding prose.
func renderPRComments(comments []domain.PRComment, width int) string {
	if len(comments) == 0 {
		return metaStyle.Render("_no comments yet_")
	}
	divider := dividerStyle.Render(strings.Repeat("─", max0(width)))
	blocks := make([]string, len(comments))
	for i, c := range comments {
		head := titleStyle.Render(c.Author) + metaStyle.Render(fmt.Sprintf(" · %s · %s", c.Kind, relativeTime(c.CreatedAt)))
		blocks[i] = head + "\n\n" + renderMarkdown(c.Body, width, "_empty_")
	}
	return strings.Join(blocks, "\n"+divider+"\n")
}

// formatUsageLine renders the task's total cost and token usage — accrued
// across every run the task has had (plan, execute, and any resumed
// review-feedback round trips; see ui.handleAgentEvent) rather than just the
// run that produced the summary text below it, so it reads as the task's
// full spend rather than only its last leg.
func formatUsageLine(t domain.Task) string {
	total := t.InputTokens + t.OutputTokens + t.CacheReadInputTokens + t.CacheCreationInputTokens
	return fmt.Sprintf(
		"Cost: $%.4f   Tokens: %s total (%s in, %s out, %s cache write, %s cache read)",
		t.Cost, formatTokenCount(total), formatTokenCount(t.InputTokens), formatTokenCount(t.OutputTokens),
		formatTokenCount(t.CacheCreationInputTokens), formatTokenCount(t.CacheReadInputTokens),
	)
}

// formatTokenCount abbreviates n to a "1.2k"/"3.4M"-style count — token
// totals routinely run into the tens of thousands (cache reads especially),
// and a bare digit string that long is harder to scan than a short one.
func formatTokenCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
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

// displayLabel returns t's readable id if set, else falls back to the
// truncated UUID for tasks created before DisplayID existed.
func displayLabel(t domain.Task) string {
	if t.DisplayID != "" {
		return t.DisplayID
	}
	return shortID(t.ID)
}

func shortID(id string) string {
	if len(id) > 4 {
		return id[:4]
	}
	return id
}

// relativeTime renders t as a coarse "N unit(s) ago" string (or "just now"
// for anything under a minute), matching the header's terse style rather
// than a full timestamp.
func relativeTime(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := time.Since(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d/time.Minute), "minute") + " ago"
	case d < 24*time.Hour:
		return plural(int(d/time.Hour), "hour") + " ago"
	case d < 30*24*time.Hour:
		return plural(int(d/(24*time.Hour)), "day") + " ago"
	case d < 365*24*time.Hour:
		return plural(int(d/(30*24*time.Hour)), "month") + " ago"
	default:
		return plural(int(d/(365*24*time.Hour)), "year") + " ago"
	}
}

func plural(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", unit)
	}
	return fmt.Sprintf("%d %ss", n, unit)
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
