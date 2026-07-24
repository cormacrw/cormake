// The CORMAKE dashboard: a small always-visible tile above the task list
// that, once highlighted (see paneFocus), swaps the right pane over from
// the selected task's detail view to a card-format overview of the whole
// workspace — open/in-flight/review/completed task counts, spend, and the
// currently open git worktrees.
package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	colorful "github.com/lucasb-eyer/go-colorful"

	"cormake/internal/domain"
	"cormake/internal/ui/theme"
)

// paneFocus is which of the two left-hand panes is currently highlighted —
// the CORMAKE dashboard tile, or the task list beneath it. It in turn
// decides what the right pane shows (see renderBody).
type paneFocus int

const (
	leftFocusTasks paneFocus = iota
	leftFocusDashboard
)

// cormakeGradient is the wordmark's fixed brand gradient (violet -> pink),
// deliberately independent of the active workspace's accent color (which
// changes per workspace, see theme.SetAccent) — CORMAKE is the app's own
// identity, not something that should shift when switching workspaces.
var cormakeGradient = []string{"#7c6cf0", "#c084fc", "#f472b6"}

// cormakeWordmark renders "CORMAKE" with cormakeGradient interpolated
// smoothly (in Luv space, which blends more evenly than plain RGB) across
// its letters.
func cormakeWordmark() string {
	return gradientText("CORMAKE", cormakeGradient)
}

// gradientText renders s one rune at a time, its foreground color
// interpolated across the given sequence of hex stops (evenly spaced along
// s). Falls back to a plain bold render of s if any stop fails to parse or
// there's nothing to interpolate between.
func gradientText(s string, hexStops []string) string {
	if len(hexStops) < 2 {
		return lipgloss.NewStyle().Bold(true).Render(s)
	}
	stops := make([]colorful.Color, len(hexStops))
	for i, hx := range hexStops {
		c, err := colorful.Hex(hx)
		if err != nil {
			return lipgloss.NewStyle().Bold(true).Render(s)
		}
		stops[i] = c
	}

	runes := []rune(s)
	segments := len(stops) - 1
	var b strings.Builder
	for i, r := range runes {
		t := 0.0
		if len(runes) > 1 {
			t = float64(i) / float64(len(runes)-1)
		}
		segF := t * float64(segments)
		seg := int(segF)
		if seg >= segments {
			seg = segments - 1
		}
		c := stops[seg].BlendLuv(stops[seg+1], segF-float64(seg))
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(c.Hex())).Render(string(r)))
	}
	return b.String()
}

// updateDashboardFocus handles input while the CORMAKE dashboard tile has
// focus — a deliberately small keymap, since there's no selected task to
// act on from here: "down" hands focus back to the task list, everything
// else that still makes sense with no task in view (quit, switch
// workspace, start a new task) is kept; anything task-specific is a silent
// no-op, same "ignore, don't pretend" pattern used elsewhere in this file.
func (m Model) updateDashboardFocus(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Down):
		m.leftFocus = leftFocusTasks
		return m, nil
	case key.Matches(msg, keys.Workspaces):
		m.openWorkspacePicker()
		return m, nil
	case key.Matches(msg, keys.NewTask):
		return m, m.openNewTaskWizard()
	}
	return m, nil
}

// dashboardCounts is the dashboard's stat-card data, computed fresh from
// m.tasks/m.active on every render — cheap in-memory aggregation, no I/O.
type dashboardCounts struct {
	OpenTasks      int
	InFlightAgents int
	ReadyForReview int
	CompletedToday int
	SessionCostUSD float64
	TotalCostUSD   float64
}

// computeDashboardCounts aggregates stats across every task in every
// workspace (not just the active one) — the dashboard is a bird's-eye view
// of all of cormake's work, unlike the task list which is scoped to
// whichever workspace tab is active.
func (m Model) computeDashboardCounts() dashboardCounts {
	var c dashboardCounts
	now := time.Now()
	for _, t := range m.tasks {
		c.TotalCostUSD += t.Cost
		switch t.Status {
		case domain.StatusTodo, domain.StatusPlanning, domain.StatusPlanned, domain.StatusInProgress, domain.StatusAwaitingApproval, domain.StatusOpeningPR:
			c.OpenTasks++
		case domain.StatusReadyForReview, domain.StatusInReview:
			c.ReadyForReview++
		case domain.StatusComplete:
			if sameDay(t.UpdatedAt, now) {
				c.CompletedToday++
			}
		}
	}
	c.InFlightAgents = len(m.active)
	c.SessionCostUSD = m.sessionCostUSD
	return c
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// renderDashboard draws the dashboard's stat-card grid plus the open
// worktrees list beneath it, clipped to width x height exactly like every
// other fixed-size block in this package (see detail.renderHeader).
func (m Model) renderDashboard(width, height int) string {
	counts := m.computeDashboardCounts()

	const gap = 1
	colWidth := (width - 2*gap) / 3
	if colWidth < 10 {
		colWidth = 10
	}
	const cardOuterHeight = 4

	card := func(label, value string) string {
		return renderStatCard(label, value, colWidth, cardOuterHeight)
	}
	spacer := strings.Repeat(" ", gap)

	row1 := lipgloss.JoinHorizontal(lipgloss.Top,
		card("Open Tasks", fmt.Sprintf("%d", counts.OpenTasks)), spacer,
		card("In-Flight Agents", fmt.Sprintf("%d", counts.InFlightAgents)), spacer,
		card("Ready for Review", fmt.Sprintf("%d", counts.ReadyForReview)),
	)
	row2 := lipgloss.JoinHorizontal(lipgloss.Top,
		card("Completed Today", fmt.Sprintf("%d", counts.CompletedToday)), spacer,
		card("Claude Usage (session)", fmt.Sprintf("$%.2f", counts.SessionCostUSD)), spacer,
		card("Model Cost (total)", fmt.Sprintf("$%.2f", counts.TotalCostUSD)),
	)

	sectionTitle := lipgloss.NewStyle().Bold(true).Render("Open Worktrees")
	divider := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(strings.Repeat("─", max0(width)))
	worktrees := m.renderWorktreeList(width)

	content := strings.Join([]string{row1, "", row2, "", sectionTitle, divider, worktrees}, "\n")
	return lipgloss.NewStyle().Width(width).MaxWidth(width).Height(height).MaxHeight(height).Render(content)
}

// renderStatCard draws one bordered stat tile: a bold accent-colored value
// over a muted label, both centered — a bare number/label pair, no chart,
// since a single instantaneous count needs nothing more.
func renderStatCard(label, value string, outerW, outerH int) string {
	innerW := outerW - paneOverhead
	innerH := outerH - paneOverhead
	if innerW < 1 {
		innerW = 1
	}
	if innerH < 1 {
		innerH = 1
	}
	valueStyle := lipgloss.NewStyle().Bold(true).Foreground(theme.Accent())
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	content := valueStyle.Render(value) + "\n" + labelStyle.Render(label)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Width(innerW).
		Height(innerH).
		Align(lipgloss.Center, lipgloss.Center).
		Render(content)
}

// worktreeRow is one open git worktree's display data, fetched via
// fetchWorktreesCmd.
type worktreeRow struct {
	Label   string // task display id + title
	Branch  string
	Commit  string // short hash; empty if the worktree has no commits yet
	Subject string
}

// dashboardWorktreesMsg reports fetchWorktreesCmd's result.
type dashboardWorktreesMsg struct {
	rows []worktreeRow
}

// fetchWorktreesCmd shells out to `git log` once per task with an open
// worktree to find its current commit — real filesystem/subprocess work,
// so it runs as a tea.Cmd (off the render path) rather than inline in
// renderDashboard, and only when the dashboard pane is actually entered
// (see the keys.Up case in Update) rather than on every frame.
func fetchWorktreesCmd(tasks []domain.Task) tea.Cmd {
	return func() tea.Msg {
		var rows []worktreeRow
		for _, t := range tasks {
			if t.WorktreePath == "" {
				continue
			}
			row := worktreeRow{
				Label:  worktreeTaskLabel(t),
				Branch: t.WorktreeName,
			}
			out, err := runGit(t.WorktreePath, "log", "-1", "--format=%h%x09%s")
			switch {
			case err != nil:
				row.Subject = "(no commits yet)"
			default:
				if hash, subject, ok := strings.Cut(out, "\t"); ok {
					row.Commit, row.Subject = hash, subject
				}
			}
			rows = append(rows, row)
		}
		return dashboardWorktreesMsg{rows: rows}
	}
}

func worktreeTaskLabel(t domain.Task) string {
	if t.DisplayID != "" {
		return t.DisplayID + " " + t.Title
	}
	return t.Title
}

// renderWorktreeList renders one line per open worktree: task label, branch
// name, and current commit — truncated to width like every other
// terminal-width-sensitive line in this package.
func (m Model) renderWorktreeList(width int) string {
	if len(m.dashboardWorktrees) == 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("No worktrees open.")
	}
	lines := make([]string, 0, len(m.dashboardWorktrees))
	for _, r := range m.dashboardWorktrees {
		commit := r.Commit
		if commit == "" {
			commit = "-------"
		}
		line := fmt.Sprintf("%-28s %-24s %s  %s",
			truncateStr(r.Label, 28), truncateStr(r.Branch, 24), commit, r.Subject)
		lines = append(lines, truncateStr(line, width))
	}
	return strings.Join(lines, "\n")
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// truncateStr shortens s to fit within maxWidth columns, appending an
// ellipsis when it doesn't — same behavior as detail.truncate, duplicated
// rather than exported across packages for one helper.
func truncateStr(s string, maxWidth int) string {
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
