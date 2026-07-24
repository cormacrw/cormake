// Package tasklist renders the left-hand pane: a compact, single-line list
// of tasks (truncated title + a right-aligned status glyph). It wraps
// bubbles/list with a custom delegate rather than the built-in two-line
// default style.
package tasklist

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"

	"cormake/internal/domain"
)

// filterBarStyle renders the "/" filter query line shown above the list
// while a filter is being typed or is applied.
var filterBarStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

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

	// tasks holds every task last given to SetTasks, before any filter
	// query narrows it down — re-filtered (see applyFilter) whenever the
	// query changes rather than filtering the already-filtered list.
	tasks []domain.Task

	// filterInput captures the "/" filter query, searched against a task's
	// title and display ID (see matchingTasks). filtering is true only
	// while the box is focused and capturing keystrokes — see Filtering,
	// read by the app shell to route keys straight here instead of treating
	// them as normal shortcuts (e.g. so typing "e" filters rather than
	// triggering Execute). The query itself stays applied to the list after
	// committing (enter) until cleared (esc), even once filtering goes
	// false again.
	filterInput textinput.Model
	filtering   bool

	// width/height are the last full size SetSize was given. The filter
	// bar, when shown, borrows one row of that for itself, so the
	// underlying list needs re-sizing whenever the bar appears or
	// disappears, not just when a new SetSize call comes in.
	width, height int
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

	fi := textinput.New()
	fi.Prompt = "/"
	fi.CharLimit = 100

	m := Model{List: l, spinner: s, frame: frame, tasks: tasks, filterInput: fi}
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

// toItems splits tasks into three sections — planned/ready-for-review at
// the top (in whatever order they were given, i.e. creation order),
// planning/in-progress next, then everything else — the latter two ordered
// by most-recently-updated first — separated by header rows wherever two
// non-empty sections meet. Headers are inert dividers (see skipHeader)
// rather than selectable rows.
func toItems(tasks []domain.Task) []list.Item {
	var top, active, bottom []domain.Task
	for _, t := range tasks {
		switch t.Status {
		case domain.StatusPlanned, domain.StatusReadyForReview, domain.StatusInReview:
			top = append(top, t)
		case domain.StatusPlanning, domain.StatusInProgress, domain.StatusOpeningPR:
			active = append(active, t)
		default:
			bottom = append(bottom, t)
		}
	}
	sort.SliceStable(active, func(i, j int) bool {
		return active[i].UpdatedAt.After(active[j].UpdatedAt)
	})
	sort.SliceStable(bottom, func(i, j int) bool {
		return bottom[i].UpdatedAt.After(bottom[j].UpdatedAt)
	})

	// Only label a section when there's more than one populated section
	// overall — a single-section list shouldn't grow a redundant header.
	populated := 0
	for _, s := range [][]domain.Task{top, active, bottom} {
		if len(s) > 0 {
			populated++
		}
	}
	showHeaders := populated > 1

	items := make([]list.Item, 0, len(tasks)+3)
	if showHeaders && len(top) > 0 {
		items = append(items, Item{Header: true, HeaderText: "PLANNED / READY FOR REVIEW / IN REVIEW"})
	}
	for _, t := range top {
		items = append(items, Item{Task: t})
	}
	if showHeaders && len(active) > 0 {
		items = append(items, Item{Header: true, HeaderText: "IN PROGRESS"})
	}
	for _, t := range active {
		items = append(items, Item{Task: t})
	}
	if showHeaders && len(bottom) > 0 {
		items = append(items, Item{Header: true, HeaderText: "OTHER"})
	}
	for _, t := range bottom {
		items = append(items, Item{Task: t})
	}
	return items
}

// SetTasks rebuilds the list's items, e.g. when the active workspace tab
// changes, and resets the selection to the top (the first real task, past
// any leading header — see toItems). Any active filter query stays applied,
// narrowing the new task set the same way it narrowed the old one.
func (m *Model) SetTasks(tasks []domain.Task) {
	m.tasks = tasks
	m.applyFilter()
	m.List.Select(0)
	m.skipHeader(0)
}

// applyFilter rebuilds the list's items from m.tasks, narrowed by the
// current filter query if one is set — the single place that keeps the
// list in sync with either changing.
func (m *Model) applyFilter() {
	m.List.SetItems(toItems(matchingTasks(m.tasks, m.filterInput.Value())))
}

// matchingTasks returns the tasks whose title or display ID contains query
// (case-insensitive) — all of tasks, unfiltered, if query is blank.
func matchingTasks(tasks []domain.Task, query string) []domain.Task {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return tasks
	}
	matched := make([]domain.Task, 0, len(tasks))
	for _, t := range tasks {
		if strings.Contains(strings.ToLower(t.Title), query) || strings.Contains(strings.ToLower(t.DisplayID), query) {
			matched = append(matched, t)
		}
	}
	return matched
}

// filterBarVisible reports whether a row needs to be reserved above the
// list for the filter bar: while actively typing a query, or with a
// committed query still narrowing the list.
func (m Model) filterBarVisible() bool {
	return m.filtering || m.filterInput.Value() != ""
}

// applySize resizes the underlying list to m.width/height minus the filter
// bar's row, if it's currently showing.
func (m *Model) applySize() {
	h := m.height
	if m.filterBarVisible() {
		h--
	}
	if h < 0 {
		h = 0
	}
	m.List.SetSize(m.width, h)
}

func (m *Model) SetSize(w, h int) {
	m.width, m.height = w, h
	promptWidth := lipgloss.Width(m.filterInput.Prompt)
	if fw := w - promptWidth; fw > 0 {
		m.filterInput.Width = fw
	}
	m.applySize()
}

// Filtering reports whether the "/" filter box is currently focused and
// capturing keystrokes — the app shell checks this to route key input
// straight to Update instead of treating it as a normal shortcut (see
// ui.Model.Update).
func (m Model) Filtering() bool { return m.filtering }

// HasActiveFilter reports whether a (possibly no-longer-being-typed) filter
// query is currently narrowing the list.
func (m Model) HasActiveFilter() bool { return strings.TrimSpace(m.filterInput.Value()) != "" }

// FilterQuery returns the current filter query, empty if none is set.
func (m Model) FilterQuery() string { return m.filterInput.Value() }

// startFilter opens the filter box for typing, preserving whatever query
// (if any) was already applied so it can be refined rather than retyped.
func (m *Model) startFilter() tea.Cmd {
	m.filtering = true
	m.filterInput.CursorEnd()
	m.applySize()
	return m.filterInput.Focus()
}

// updateFiltering handles input while the filter box is focused: esc clears
// the query entirely and exits, enter keeps whatever's typed applied and
// exits, anything else is forwarded to the text input and re-narrows the
// list live, on every keystroke.
func (m Model) updateFiltering(msg tea.Msg) (Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			m.filtering = false
			m.filterInput.Blur()
			m.filterInput.SetValue("")
			m.applyFilter()
			m.List.Select(0)
			m.skipHeader(0)
			m.applySize()
			return m, nil
		case "enter":
			m.filtering = false
			m.filterInput.Blur()
			m.applySize()
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	m.applyFilter()
	m.List.Select(0)
	m.skipHeader(0)
	return m, cmd
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
	if m.filtering {
		return m.updateFiltering(msg)
	}
	if km, ok := msg.(tea.KeyMsg); ok && km.String() == "/" {
		cmd := m.startFilter()
		return m, cmd
	}

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
	if !m.filterBarVisible() {
		return m.List.View()
	}
	return lipgloss.JoinVertical(lipgloss.Left, filterBarStyle.Render(m.filterInput.View()), m.List.View())
}
