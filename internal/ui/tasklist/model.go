// Package tasklist renders the left-hand pane: a compact, single-line list
// of tasks (truncated title + a right-aligned status glyph). It wraps
// bubbles/list with a custom delegate rather than the built-in two-line
// default style.
package tasklist

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/bubbles/list"

	"cormake/internal/domain"
)

// Item adapts a domain.Task to bubbles/list.Item.
type Item struct {
	Task domain.Task
}

func (i Item) FilterValue() string { return i.Task.Title }

type Model struct {
	List list.Model
}

func New(tasks []domain.Task) Model {
	l := list.New(toItems(tasks), Delegate{}, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(false)
	l.DisableQuitKeybindings()
	return Model{List: l}
}

func toItems(tasks []domain.Task) []list.Item {
	items := make([]list.Item, len(tasks))
	for i, t := range tasks {
		items[i] = Item{Task: t}
	}
	return items
}

// SetTasks rebuilds the list's items, e.g. when the active workspace tab
// changes, and resets the selection to the top.
func (m *Model) SetTasks(tasks []domain.Task) {
	m.List.SetItems(toItems(tasks))
	m.List.Select(0)
}

func (m *Model) SetSize(w, h int) {
	m.List.SetSize(w, h)
}

// SelectByID moves the selection to the task with the given ID, if it's
// present in the current (filtered) item set. Used to land on a
// just-created task rather than wherever SetTasks reset the cursor to.
func (m *Model) SelectByID(id string) {
	for i, item := range m.List.Items() {
		if it, ok := item.(Item); ok && it.Task.ID == id {
			m.List.Select(i)
			return
		}
	}
}

// Selected returns the currently highlighted task, if any.
func (m Model) Selected() (domain.Task, bool) {
	item, ok := m.List.SelectedItem().(Item)
	if !ok {
		return domain.Task{}, false
	}
	return item.Task, true
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.List, cmd = m.List.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	return m.List.View()
}
