package tasklist

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	selectedRowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	plainRowStyle    = lipgloss.NewStyle()
)

// Delegate renders each task as a single compact line:
// "<marker><mode>  <title>" on the left, "<glyph> <STAGE>" right-aligned.
type Delegate struct{}

func (d Delegate) Height() int                               { return 1 }
func (d Delegate) Spacing() int                              { return 0 }
func (d Delegate) Update(msg tea.Msg, m *list.Model) tea.Cmd { return nil }

func (d Delegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	ti, ok := item.(Item)
	if !ok {
		return
	}

	marker := "  "
	if index == m.Index() {
		marker = "▸ "
	}

	stage, glyph := ti.Task.DisplayStage()
	right := fmt.Sprintf("%s %s", glyph, stage)

	prefix := fmt.Sprintf("%s%-8s ", marker, ti.Task.Mode)
	titleWidth := m.Width() - lipgloss.Width(prefix) - lipgloss.Width(right) - 1
	title := truncate(ti.Task.Title, titleWidth)

	gap := m.Width() - lipgloss.Width(prefix) - lipgloss.Width(title) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}

	line := prefix + title + repeatSpace(gap) + right
	if index == m.Index() {
		line = selectedRowStyle.Render(line)
	} else {
		line = plainRowStyle.Render(line)
	}
	fmt.Fprint(w, line)
}

func repeatSpace(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
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
	// Trim rune-by-rune (rather than assuming 1 rune == 1 cell) until the
	// truncated string plus the ellipsis fits within maxWidth.
	for len(runes) > 0 && lipgloss.Width(string(runes)+"…") > maxWidth {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}
