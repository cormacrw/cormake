package ui

import (
	"github.com/charmbracelet/lipgloss"

	"cormake/internal/ui/theme"
)

var (
	paneBorderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240"))

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("245"))

	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	tabInfoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))
)

func focusedPaneBorderStyle() lipgloss.Style {
	return paneBorderStyle.BorderForeground(theme.Accent())
}

func activeTabStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(theme.Accent()).
		Bold(true)
}

// paneOverhead is how many terminal columns/rows lipgloss's border adds on
// top of the content Width/Height passed to NewStyle().
const paneOverhead = 2
