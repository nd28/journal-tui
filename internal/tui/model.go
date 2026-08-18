package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/nd28/journal-tui/internal/store"
)

const Version = "0.6.0"

type screen int

const (
	screenHome screen = iota
	screenWriting
	screenSummary
	screenHistory
	screenRead
)

// Model is the root Bubble Tea model. It holds the current screen plus
// per-screen state, and dispatches Update/View to the active screen.
type Model struct {
	screen screen
	store  *store.Store
	stats  store.Stats

	width       int
	height      int
	compactMode bool

	homeCursor int

	// recovery is the most recent session the app was killed in the middle
	// of, offered on the home menu; recoveryCount is how many are waiting in
	// total. Nil when there is nothing to pick up.
	recovery      *store.UnfinishedSession
	recoveryCount int

	writing writingState
	summary summaryState
	history historyState
	read    readState

	err error
}

func New(s *store.Store) (Model, error) {
	stats, err := s.GetStats()
	if err != nil {
		return Model{}, err
	}
	m := Model{screen: screenHome, store: s, stats: stats}
	// Sessions opened and walked away from hold nothing worth recovering and
	// would otherwise pile up forever, since nothing at runtime revisits an
	// unfinished row. Sweep them first so what's left is only real writing.
	if _, err := s.DiscardEmptyUnfinishedSessions(); err != nil {
		return Model{}, err
	}
	m.refreshRecovery()
	if m.err != nil {
		return Model{}, m.err
	}
	return m, nil
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if sizeMsg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = sizeMsg.Width
		m.height = sizeMsg.Height
	}

	// Any keypress dismisses a stale error. Screen updates run after this,
	// so an error raised by the very keypress that cleared the last one
	// still shows.
	if _, ok := msg.(tea.KeyMsg); ok {
		m.err = nil
	}

	switch m.screen {
	case screenHome:
		return m.updateHome(msg)
	case screenWriting:
		return m.updateWriting(msg)
	case screenSummary:
		return m.updateSummary(msg)
	case screenHistory:
		return m.updateHistory(msg)
	case screenRead:
		return m.updateRead(msg)
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
	case screenRead:
		body = m.viewRead()
	}
	if m.err != nil {
		body += "\n" + errorStyle.Render("Error: "+m.err.Error())
	}
	body += "\n" + statStyle.Render("journal v"+Version)
	return body
}
