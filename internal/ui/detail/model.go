// Package detail renders the right-hand side of the app: a top panel with
// the selected task's title, metadata, and markdown description (rendered
// via glamour), and a log viewport underneath. Editing happens in an
// external editor (see internal/ui/editor.go), not in this pane.
package detail

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"cormake/internal/domain"
)

var (
	titleStyle   = lipgloss.NewStyle().Bold(true)
	metaStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	dividerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

// topHeightPct is the share of the pane's height given to the
// title/info/description panel; the rest goes to the log viewport below it.
const topHeightPct = 0.45

type Model struct {
	Viewport viewport.Model // the log, bottom panel

	task     domain.Task
	wsName   string
	repoName string
	logs     map[string][]string

	renderedBody string

	empty        bool
	emptyMessage string

	width, height int
	topHeight     int
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

	m.topHeight = int(float64(h) * topHeightPct)
	if m.topHeight < 4 {
		m.topHeight = 4
	}
	logHeight := h - m.topHeight
	if logHeight < 1 {
		logHeight = 1
	}

	m.Viewport.Width = w
	m.Viewport.Height = logHeight

	m.refreshRenderedBody()
}

// SetTask switches the displayed task, refreshing the info panel and log
// content, and scrolls the log back to the top of the new task's log.
// wsName/repoName are resolved display names, since Task only stores IDs.
func (m *Model) SetTask(t domain.Task, wsName, repoName string) {
	m.empty = false
	m.task = t
	m.wsName = wsName
	m.repoName = repoName
	m.Viewport.SetContent(strings.Join(m.logs[t.ID], "\n"))
	m.Viewport.SetYOffset(0)
	m.refreshRenderedBody()
}

// SetEmpty replaces the normal task view with a centered message — used
// when there's no task to show at all (e.g. an empty workspace/view),
// rather than rendering a fake task with a placeholder title.
func (m *Model) SetEmpty(msg string) {
	m.empty = true
	m.emptyMessage = msg
}

// ScrollLog forwards a paging key straight to the log viewport, independent
// of whatever's shown in the top panel.
func (m *Model) ScrollLog(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.Viewport, cmd = m.Viewport.Update(msg)
	return cmd
}

func (m Model) View() string {
	if m.empty {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			metaStyle.Render(m.emptyMessage))
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.renderTop(), m.Viewport.View())
}

func (m Model) renderTop() string {
	stage, glyph := m.task.DisplayStage()
	title := titleStyle.Render(truncate(m.task.Title, m.width-8)) + " #" + shortID(m.task.ID)
	meta := metaStyle.Render(fmt.Sprintf("workspace: %s   repo: %s   %s %s",
		m.wsName, m.repoName, glyph, stage))
	divider := dividerStyle.Render(strings.Repeat("─", max0(m.width)))

	content := strings.Join([]string{title, meta, divider, m.renderedBody}, "\n")
	return lipgloss.NewStyle().Width(m.width).Height(m.topHeight).MaxHeight(m.topHeight).MaxWidth(m.width).Render(content)
}

// refreshRenderedBody re-runs glamour, which is cheap enough for task
// descriptions but not worth doing on every keystroke, so it's only called
// when the task or the available width changes.
func (m *Model) refreshRenderedBody() {
	m.renderedBody = renderMarkdown(m.task.Description, m.width)
}

func renderMarkdown(md string, width int) string {
	if strings.TrimSpace(md) == "" {
		return metaStyle.Render("_no description_")
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
