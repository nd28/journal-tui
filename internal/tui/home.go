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

const resumeMenuItem = "Resume Session"

// menuItems is the home menu for the current state: a resume entry joins the
// front whenever an interrupted session is waiting. Built per call rather
// than stored so the menu and the cursor can never disagree about what is on
// screen.
func (m Model) menuItems() []string {
	if m.recovery == nil {
		return homeMenuItems
	}
	return append([]string{resumeMenuItem}, homeMenuItems...)
}

func (m Model) updateHome(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	items := m.menuItems()
	switch keyMsg.String() {
	case "up", "k":
		if m.homeCursor > 0 {
			m.homeCursor--
		}
	case "down", "j":
		if m.homeCursor < len(items)-1 {
			m.homeCursor++
		}
	case "enter":
		// The cursor can outlive the menu it was placed on — resuming the
		// last interrupted session shortens the menu by one — so clamp
		// rather than index blind.
		if m.homeCursor >= len(items) {
			m.homeCursor = len(items) - 1
		}
		switch items[m.homeCursor] {
		case resumeMenuItem:
			return m.resumeWritingSession(*m.recovery)
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
	b.WriteString(statStyle.Render(fmt.Sprintf("Lifetime score: %s", formatNumber(m.stats.LifetimeScore))) + "\n")
	b.WriteString(statStyle.Render(fmt.Sprintf("Best session:   %s", formatNumber(m.stats.HighSessionScore))) + "\n")
	b.WriteString(statStyle.Render(fmt.Sprintf("Streak:         %s", formatCount(m.stats.CurrentStreak, "day", "days"))) + "\n\n")

	if m.recovery != nil {
		b.WriteString(errorStyle.Render(formatRecoveryNotice(*m.recovery, m.recoveryCount)) + "\n\n")
	}

	for i, item := range m.menuItems() {
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
