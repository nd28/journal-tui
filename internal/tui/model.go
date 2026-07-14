package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"journal/internal/store"
)

type screen int

const (
	screenHome screen = iota
	screenWriting
	screenSummary
	screenHistory
)

// Model is the root Bubble Tea model. It holds the current screen plus
// per-screen state, and dispatches Update/View to the active screen.
type Model struct {
	screen screen
	store  *store.Store
	stats  store.Stats

	homeCursor int

	writing writingState
	summary summaryState
	history historyState

	err error
}

func New(s *store.Store) (Model, error) {
	stats, err := s.GetStats()
	if err != nil {
		return Model{}, err
	}
	return Model{screen: screenHome, store: s, stats: stats}, nil
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenHome:
		return m.updateHome(msg)
	case screenWriting:
		return m.updateWriting(msg)
	case screenSummary:
		return m.updateSummary(msg)
	case screenHistory:
		return m.updateHistory(msg)
	}
	return m, nil
}

func (m Model) View() string {
	var body string
	switch m.screen {
	case screenHome:
		body = m.viewHome()
	case screenWriting:
		body = m.viewWriting()
	case screenSummary:
		body = m.viewSummary()
	case screenHistory:
		body = m.viewHistory()
	}
	if m.err != nil {
		body += "\n" + errorStyle.Render("Error: "+m.err.Error())
	}
	return body
}
