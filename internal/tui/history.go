package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nd28/journal-tui/internal/store"
)

const historyPageSize = 10

type historyState struct {
	query   string
	page    int
	cursor  int
	total   int
	results []store.SessionSearchResult
}

func (m Model) enterHistory() (tea.Model, tea.Cmd) {
	m.screen = screenHistory
	m.history = historyState{}
	return m.reloadHistory(0)
}

// reloadHistory re-queries the store for the given page under the current
// query, resetting the cursor to the top of the new result set.
func (m Model) reloadHistory(page int) (tea.Model, tea.Cmd) {
	m.history.page = page
	m.history.cursor = 0
	results, total, err := m.store.SearchSessions(m.history.query, historyPageSize, page*historyPageSize)
	if err != nil {
		m.err = err
		return m, nil
	}
	m.history.results = results
	m.history.total = total
	return m, nil
}

// changeHistoryPage moves by delta pages, clamped to [0, last page].
func (m Model) changeHistoryPage(delta int) (tea.Model, tea.Cmd) {
	totalPages := (m.history.total + historyPageSize - 1) / historyPageSize
	if totalPages == 0 {
		totalPages = 1
	}
	newPage := m.history.page + delta
	if newPage < 0 {
		newPage = 0
	}
	if newPage > totalPages-1 {
		newPage = totalPages - 1
	}
	return m.reloadHistory(newPage)
}

func (m Model) updateHistory(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch keyMsg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		m.screen = screenHome
		m.homeCursor = 0
		return m, nil
	case tea.KeyEnter:
		if len(m.history.results) == 0 {
			return m, nil
		}
		return m.enterRead(m.history.results[m.history.cursor])
	case tea.KeyUp:
		if m.history.cursor > 0 {
			m.history.cursor--
		}
		return m, nil
	case tea.KeyDown:
		if m.history.cursor < len(m.history.results)-1 {
			m.history.cursor++
		}
		return m, nil
	case tea.KeyPgUp:
		return m.changeHistoryPage(-1)
	case tea.KeyPgDown:
		return m.changeHistoryPage(1)
	case tea.KeyBackspace:
		if len(m.history.query) > 0 {
			r := []rune(m.history.query)
			m.history.query = string(r[:len(r)-1])
		}
		return m.reloadHistory(0)
	case tea.KeyCtrlU:
		m.history.query = ""
		return m.reloadHistory(0)
	case tea.KeySpace:
		m.history.query += " "
		return m.reloadHistory(0)
	case tea.KeyRunes:
		m.history.query += string(keyMsg.Runes)
		return m.reloadHistory(0)
	}
	return m, nil
}

func (m Model) viewHistory() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("History") + "\n\n")
	b.WriteString(statStyle.Render("search: "+m.history.query) + "\n\n")

	if len(m.history.results) == 0 {
		b.WriteString(statStyle.Render("No sessions found.") + "\n")
	}
	for i, r := range m.history.results {
		cursor := "  "
		style := statStyle
		if i == m.history.cursor {
			cursor = "> "
			style = selectedStyle
		}
		b.WriteString(cursor + style.Render(fmt.Sprintf(
			"%s   Score: %s   %s",
			formatSessionDate(r.StartedAt),
			formatNumber(r.SessionScore),
			formatCount(r.WordCount, "word", "words"),
		)) + "\n")
		if r.Snippet != "" {
			b.WriteString("    " + statStyle.Render(r.Snippet) + "\n")
		}
	}

	from, to := 0, 0
	if m.history.total > 0 {
		from = m.history.page*historyPageSize + 1
		to = from + len(m.history.results) - 1
	}
	b.WriteString("\n" + statStyle.Render(fmt.Sprintf("showing %d-%d of %d", from, to, m.history.total)) + "\n")
	b.WriteString(statStyle.Render("enter: read   pgup/pgdn: page   esc: back to home"))
	return b.String()
}
