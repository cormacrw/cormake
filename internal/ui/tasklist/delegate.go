package tasklist

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"cormake/internal/ui/theme"
)

var plainRowStyle = lipgloss.NewStyle()

var headerRowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Bold(true)

func selectedRowStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Accent()).Bold(true)
}

// Delegate renders each task as a single compact line: "<marker><title>" on
// the left, its status glyph right-aligned (full stage name is left for the
// detail pane; the list stays compact).
type Delegate struct{}

func (d Delegate) Height() int                               { return 1 }
func (d Delegate) Spacing() int                              { return 0 }
func (d Delegate) Update(msg tea.Msg, m *list.Model) tea.Cmd { return nil }

func (d Delegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	ti, ok := item.(Item)
	if !ok {
		return
	}

	if ti.Header {
		fmt.Fprint(w, headerRowStyle.Render(truncate(ti.HeaderText, m.Width())))
		return
	}

	marker := "  "
	if index == m.Index() {
		marker = "▸ "
	}

	_, glyph := ti.Task.DisplayStage()

	titleWidth := m.Width() - lipgloss.Width(marker) - lipgloss.Width(glyph) - 1
	title := truncate(ti.Task.Title, titleWidth)

	gap := m.Width() - lipgloss.Width(marker) - lipgloss.Width(title) - lipgloss.Width(glyph)
	if gap < 1 {
		gap = 1
	}

	line := marker + title + repeatSpace(gap) + glyph
	if index == m.Index() {
		line = selectedRowStyle().Render(line)
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
