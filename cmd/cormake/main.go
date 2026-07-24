package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"cormake/internal/store"
	"cormake/internal/ui"
	"cormake/internal/version"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("cormake v" + version.Version)
		return
	}

	dir, err := store.AppDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cormake: could not resolve app directory:", err)
		os.Exit(1)
	}

	st, err := store.Open(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cormake: could not open local storage at", dir+":", err)
		os.Exit(1)
	}

	m, err := ui.New(st)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cormake: failed to load saved state:", err)
		os.Exit(1)
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error running cormake:", err)
		os.Exit(1)
	}
}
