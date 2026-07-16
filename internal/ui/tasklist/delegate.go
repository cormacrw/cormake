package tasklist

import (
	"fmt"
	"io"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"cormake/internal/domain"
)

var (
	selectedRowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	plainRowStyle    = lipgloss.NewStyle()
)

// Delegate renders each task as a single compact line:
// "<marker><glyph> <mode>  <title>", truncated to the pane's width.
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

	glyph := statusGlyph(ti.Task.Status)
	mode := string(ti.Task.Mode)

	// Reserve space for "<marker><glyph> <mode-8> " before truncating the title.
	prefix := fmt.Sprintf("%s%s %-8s ", marker, glyph, mode)
	titleWidth := m.Width() - lipgloss.Width(prefix)
	title := truncate(ti.Task.Title, titleWidth)

	line := prefix + title
	if index == m.Index() {
		line = selectedRowStyle.Render(line)
	} else {
		line = plainRowStyle.Render(line)
	}
	fmt.Fprint(w, line)
}

func statusGlyph(s domain.Status) string {
	switch s {
	case domain.StatusPending:
		return "○"
	case domain.StatusRunning:
		return "⏳"
	case domain.StatusAwaitingApproval:
		return "⚠"
	case domain.StatusCompleted:
		return "✔"
	case domain.StatusFailed:
		return "✖"
	case domain.StatusCancelled:
		return "⊘"
	default:
		return "?"
	}
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
