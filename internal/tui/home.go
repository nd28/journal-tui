package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	statStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
)

var homeMenuItems = []string{"New Session", "History", "Quit"}

func (m Model) updateHome(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "up", "k":
		if m.homeCursor > 0 {
			m.homeCursor--
		}
	case "down", "j":
		if m.homeCursor < len(homeMenuItems)-1 {
			m.homeCursor++
		}
	case "enter":
		switch homeMenuItems[m.homeCursor] {
		case "New Session":
			return m.startWritingSession()
		case "History":
			return m.enterHistory()
		case "Quit":
			return m, tea.Quit
		}
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) viewHome() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Journal") + "\n\n")
	b.WriteString(statStyle.Render(fmt.Sprintf("Lifetime score: %d", m.stats.LifetimeScore)) + "\n")
	b.WriteString(statStyle.Render(fmt.Sprintf("Best session:   %d", m.stats.HighSessionScore)) + "\n")
	b.WriteString(statStyle.Render(fmt.Sprintf("Streak:         %d days", m.stats.CurrentStreak)) + "\n\n")

	for i, item := range homeMenuItems {
		cursor := "  "
		style := statStyle
		if i == m.homeCursor {
			cursor = "> "
			style = selectedStyle
		}
		b.WriteString(cursor + style.Render(item) + "\n")
	}
	return b.String()
}
