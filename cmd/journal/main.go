package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/term"

	"github.com/nd28/journal-tui/internal/cli"
	"github.com/nd28/journal-tui/internal/store"
	"github.com/nd28/journal-tui/internal/tui"
)

func main() {
	os.Exit(run())
}

func run() int {
	app := cli.App{
		Version: tui.Version,
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Stderr:  os.Stderr,
	}
	if code, handled := app.Run(os.Args[1:]); handled {
		return code
	}
	return runTUI()
}

func runTUI() int {
	// Bubble Tea opens /dev/tty itself, so without a terminal it fails with
	// a message about a device that means nothing to whoever piped us. Say
	// what to do instead. Both streams are checked because redirecting just
	// one (`journal > log.txt` from a real terminal) is still interactive —
	// only losing both means nobody is at a keyboard.
	if !term.IsTerminal(os.Stdin.Fd()) && !term.IsTerminal(os.Stdout.Fd()) {
		fmt.Fprintln(os.Stderr, "journal: the interactive app needs a terminal.")
		fmt.Fprintln(os.Stderr, "Run `journal help` for the commands that work without one.")
		return 1
	}

	s, err := store.OpenDefault()
	if err != nil {
		fmt.Fprintln(os.Stderr, "journal: could not open database:", err)
		return 1
	}
	defer s.Close()

	m, err := tui.New(s)
	if err != nil {
		fmt.Fprintln(os.Stderr, "journal: could not initialize app:", err)
		return 1
	}

	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "journal: fatal error:", err)
		return 1
	}
	return 0
}
