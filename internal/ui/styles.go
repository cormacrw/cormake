package ui

import "github.com/charmbracelet/lipgloss"

var (
	paneBorderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240"))

	focusedPaneBorderStyle = paneBorderStyle.
				BorderForeground(lipgloss.Color("212"))

	activeTabStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("212")).
			Bold(true)

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("245"))

	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))
)

// paneOverhead is how many terminal columns/rows lipgloss's border adds on
// top of the content Width/Height passed to NewStyle().
const paneOverhead = 2
