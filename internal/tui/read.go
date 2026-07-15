package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/nd28/journal-tui/internal/store"
)

const (
	// readChromeLines is the Read screen's fixed vertical overhead: the
	// title line, a blank separator, the help line, and the version
	// footer appended by Model.View().
	readChromeLines = 4
	readMinHeight   = 3
)

type readState struct {
	viewport viewport.Model
	entries  []store.EntryRecord
}

// readViewportSize computes the viewport's width/height from the terminal
// size, reserving readChromeLines for the screen's fixed text and flooring
// the height so a tiny terminal never yields a non-positive viewport.
func readViewportSize(termWidth, termHeight int) (width, height int) {
	width = termWidth
	height = termHeight - readChromeLines
	if height < readMinHeight {
		height = readMinHeight
	}
	return width, height
}

// renderReadEntries renders a session's entries for the viewport. A single
// entry is shown with no extra header; multiple entries each get a dim
// "— entry N —" header.
func renderReadEntries(entries []store.EntryRecord) string {
	if len(entries) == 1 {
		return entries[0].Body
	}
	var b strings.Builder
	for i, e := range entries {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(statStyle.Render(fmt.Sprintf("— entry %d —", i+1)) + "\n")
		b.WriteString(e.Body)
	}
	return b.String()
}

func (m Model) enterRead(sessionID int64) (tea.Model, tea.Cmd) {
	entries, err := m.store.GetEntries(sessionID)
	if err != nil {
		m.err = err
		return m, nil
	}
	w, h := readViewportSize(m.width, m.height)
	vp := viewport.New(w, h)
	vp.SetContent(renderReadEntries(entries))
	m.read = readState{viewport: vp, entries: entries}
	m.screen = screenRead
	return m, nil
}

func (m Model) updateRead(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyEsc:
			m.screen = screenHistory
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.read.viewport, cmd = m.read.viewport.Update(msg)
	return m, cmd
}

func (m Model) viewRead() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Read") + "\n\n")
	b.WriteString(m.read.viewport.View() + "\n")
	b.WriteString(statStyle.Render("up/down/pgup/pgdn: scroll   esc: back to history"))
	return b.String()
}
