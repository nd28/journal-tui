package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"journal/internal/store"
	"journal/internal/tui"
)

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "journal: could not determine home directory:", err)
		os.Exit(1)
	}

	dbDir := filepath.Join(home, ".journal")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "journal: could not create data directory:", err)
		os.Exit(1)
	}

	s, err := store.Open(filepath.Join(dbDir, "journal.db"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "journal: could not open database:", err)
		os.Exit(1)
	}
	defer s.Close()

	m, err := tui.New(s)
	if err != nil {
		fmt.Fprintln(os.Stderr, "journal: could not initialize app:", err)
		os.Exit(1)
	}

	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "journal: fatal error:", err)
		os.Exit(1)
	}
}
