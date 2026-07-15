package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nd28/journal-tui/internal/store"
)

type historyState struct {
	sessions []store.SessionRecord
}

func (m Model) enterHistory() (tea.Model, tea.Cmd) {
	records, err := m.store.ListSessions(20)
	if err != nil {
		m.err = err
		return m, nil
	}
	m.history = historyState{sessions: records}
	m.screen = screenHistory
	return m, nil
}

func (m Model) updateHistory(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "esc", "enter":
		m.screen = screenHome
		m.homeCursor = 0
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) viewHistory() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("History") + "\n\n")
	if len(m.history.sessions) == 0 {
		b.WriteString(statStyle.Render("No sessions yet.") + "\n")
	}
	for _, s := range m.history.sessions {
		b.WriteString(statStyle.Render(fmt.Sprintf("%s   score %d   %d words", s.StartedAt, s.SessionScore, s.WordCount)) + "\n")
	}
	b.WriteString("\n" + statStyle.Render("esc: back to home") + "\n")
	return b.String()
}
