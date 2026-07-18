// Package tasklist renders the left-hand pane: a compact, single-line list
// of tasks (truncated title + a right-aligned status glyph). It wraps
// bubbles/list with a custom delegate rather than the built-in two-line
// default style.
package tasklist

import (
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"

	"cormake/internal/domain"
)

// Item adapts a domain.Task to bubbles/list.Item. A Header item is an
// inert divider row (see toItems) rather than a real task.
type Item struct {
	Task domain.Task

	Header     bool
	HeaderText string
}

func (i Item) FilterValue() string {
	if i.Header {
		return ""
	}
	return i.Task.Title
}

type Model struct {
	List list.Model

	spinner spinner.Model

	// frame holds the shared spinner's current rendered frame, refreshed by
	// UpdateSpinner and read by Delegate.Render for PLANNING/IN_PROGRESS
	// rows — a pointer rather than a plain field so the Delegate value
	// handed to list.Model at construction time keeps seeing later updates.
	frame *string
}

func New(tasks []domain.Task) Model {
	s := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	frame := new(string)
	*frame = s.View()

	l := list.New(toItems(tasks), Delegate{Frame: frame}, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(false)
	l.DisableQuitKeybindings()
	m := Model{List: l, spinner: s, frame: frame}
	m.skipHeader(0)
	return m
}

// SpinnerTick starts (or resumes) the shared in-flight-agent spinner.
func (m Model) SpinnerTick() tea.Cmd {
	return m.spinner.Tick
}

// UpdateSpinner advances the shared spinner in response to a
// spinner.TickMsg and refreshes the frame Delegate.Render reads for
// PLANNING/IN_PROGRESS rows.
func (m *Model) UpdateSpinner(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	*m.frame = m.spinner.View()
	return cmd
}

// toItems splits tasks into two sections — planned/ready-for-review at the
// top (in whatever order they were given, i.e. creation order), everything
// else below ordered by most-recently-updated first — separated by a
// header row when both sections are non-empty. Headers are inert dividers
// (see skipHeader) rather than selectable rows.
func toItems(tasks []domain.Task) []list.Item {
	var top, bottom []domain.Task
	for _, t := range tasks {
		if t.Status == domain.StatusPlanned || t.Status == domain.StatusReadyForReview {
			top = append(top, t)
		} else {
			bottom = append(bottom, t)
		}
	}
	sort.SliceStable(bottom, func(i, j int) bool {
		return bottom[i].UpdatedAt.After(bottom[j].UpdatedAt)
	})

	// Only label the split when there's actually something on both sides of
	// it — a single-section list shouldn't grow a redundant header.
	showHeaders := len(top) > 0 && len(bottom) > 0

	items := make([]list.Item, 0, len(tasks)+2)
	if showHeaders {
		items = append(items, Item{Header: true, HeaderText: "PLANNED / READY FOR REVIEW"})
	}
	for _, t := range top {
		items = append(items, Item{Task: t})
	}
	if showHeaders {
		items = append(items, Item{Header: true, HeaderText: "OTHER"})
	}
	for _, t := range bottom {
		items = append(items, Item{Task: t})
	}
	return items
}

// SetTasks rebuilds the list's items, e.g. when the active workspace tab
// changes, and resets the selection to the top (the first real task, past
// any leading header — see toItems).
func (m *Model) SetTasks(tasks []domain.Task) {
	m.List.SetItems(toItems(tasks))
	m.List.Select(0)
	m.skipHeader(0)
}

func (m *Model) SetSize(w, h int) {
	m.List.SetSize(w, h)
}

// SelectByID moves the selection to the task with the given ID, if it's
// present in the current (filtered) item set. Used to land on a
// just-created task rather than wherever SetTasks reset the cursor to.
func (m *Model) SelectByID(id string) {
	for i, item := range m.List.Items() {
		if it, ok := item.(Item); ok && !it.Header && it.Task.ID == id {
			m.List.Select(i)
			return
		}
	}
}

// Selected returns the currently highlighted task, if any.
func (m Model) Selected() (domain.Task, bool) {
	item, ok := m.List.SelectedItem().(Item)
	if !ok || item.Header {
		return domain.Task{}, false
	}
	return item.Task, true
}

// AtTop reports whether the selection is already on the first selectable
// (non-header) row — used by the app shell to know when an "up" key press
// should move focus out of the list entirely (into the CORMAKE dashboard
// pane above it) rather than being swallowed here. An empty list counts as
// "at top" too, so focus can still move up with no tasks in view.
func (m Model) AtTop() bool {
	idx := m.List.Index()
	for i, item := range m.List.Items() {
		if it, ok := item.(Item); ok && !it.Header {
			return i == idx
		}
	}
	return true
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	before := m.List.Index()
	var cmd tea.Cmd
	m.List, cmd = m.List.Update(msg)
	m.skipHeader(before)
	return m, cmd
}

// skipHeader moves the selection off a header row (see toItems) onto the
// next real task in whichever direction the cursor was already moving, so
// headers behave as inert dividers rather than selectable rows.
func (m *Model) skipHeader(before int) {
	items := m.List.Items()
	idx := m.List.Index()
	if idx < 0 || idx >= len(items) {
		return
	}
	it, ok := items[idx].(Item)
	if !ok || !it.Header {
		return
	}
	dir := 1
	if idx < before {
		dir = -1
	}
	if next, found := nextSelectable(items, idx, dir); found {
		m.List.Select(next)
		return
	}
	if next, found := nextSelectable(items, idx, -dir); found {
		m.List.Select(next)
	}
}

// nextSelectable scans items from `from` in steps of `dir`, returning the
// index of the first non-header row it finds.
func nextSelectable(items []list.Item, from, dir int) (int, bool) {
	for i := from + dir; i >= 0 && i < len(items); i += dir {
		if it, ok := items[i].(Item); ok && !it.Header {
			return i, true
		}
	}
	return 0, false
}

func (m Model) View() string {
	return m.List.View()
}
