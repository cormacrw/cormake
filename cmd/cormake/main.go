package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"cormake/internal/ui"
)

func main() {
	p := tea.NewProgram(ui.New(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error running cormake:", err)
		os.Exit(1)
	}
}
