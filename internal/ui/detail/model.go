// Package detail renders the right-hand pane: a metadata card for the
// currently selected task, followed by a scrollable viewport of its log.
package detail

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"cormake/internal/domain"
)

var cardStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color("240")).
	Padding(0, 1)

// cardHeight is the fixed number of terminal rows the metadata card
// occupies: 3 content lines + top/bottom border, no vertical padding.
const cardHeight = 5

type Model struct {
	Viewport viewport.Model
	task     domain.Task
	wsName   string
	repoName string
	logs     map[string][]string
	width    int
}

func New(logs map[string][]string) Model {
	return Model{
		Viewport: viewport.New(0, 0),
		logs:     logs,
	}
}

// SetSize sets the total content area available to this pane; the card
// takes a fixed slice of the height and the viewport gets the rest.
func (m *Model) SetSize(w, h int) {
	m.width = w
	vh := h - cardHeight
	if vh < 0 {
		vh = 0
	}
	m.Viewport.Width = w
	m.Viewport.Height = vh
}

// SetTask switches the displayed task, refreshing both the card and the
// log content, and scrolls back to the top of the new task's log.
// wsName/repoName are resolved display names, since Task only stores IDs.
func (m *Model) SetTask(t domain.Task, wsName, repoName string) {
	m.task = t
	m.wsName = wsName
	m.repoName = repoName
	m.Viewport.SetContent(strings.Join(m.logs[t.ID], "\n"))
	m.Viewport.SetYOffset(0)
}

// cardHorizontalPadding must match cardStyle's Padding(0, 1): lipgloss's
// Width() sizes the content+padding box together, so the text itself only
// gets (width - 2*padding) columns before it wraps.
const cardHorizontalPadding = 1

func (m Model) card() string {
	shortID := m.task.ID
	if len(shortID) > 4 {
		shortID = shortID[:4]
	}

	cardWidth := m.width - 2 // subtract left/right border; Width() sets content+padding width
	if cardWidth < 0 {
		cardWidth = 0
	}
	textWidth := cardWidth - 2*cardHorizontalPadding
	if textWidth < 0 {
		textWidth = 0
	}

	idPart := "#" + shortID
	titlePad := textWidth - lipgloss.Width(idPart)
	if titlePad < 0 {
		titlePad = 0
	}
	title := truncate(m.task.Title, titlePad)
	titleLine := fmt.Sprintf("%-*s%s", titlePad, title, idPart)

	metaLine := truncate(fmt.Sprintf("workspace: %s   repo: %s   mode: %s", m.wsName, m.repoName, m.task.Mode), textWidth)
	statusLine := truncate(fmt.Sprintf("status: %s   cost: $%.2f", m.task.Status, m.task.Cost), textWidth)

	return cardStyle.Width(cardWidth).Render(
		strings.Join([]string{titleLine, metaLine, statusLine}, "\n"),
	)
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

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.Viewport, cmd = m.Viewport.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	return lipgloss.JoinVertical(lipgloss.Left, m.card(), m.Viewport.View())
}
