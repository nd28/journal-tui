package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nd28/journal-tui/internal/store"
)

const (
	// readChromeLines is the Read screen's fixed vertical overhead: the
	// title line, a blank separator, the session stat line, another blank
	// separator, the help line, and the version footer appended by
	// Model.View().
	readChromeLines = 6
	readMinHeight   = 3

	// Reading width is capped well short of a wide terminal's full width —
	// long unbroken lines are hard to read. Same cap as the writing screen's
	// Full mode (writingMaxWidth/writingWidthMargin/writingMinWidth).
	readMaxWidth    = 100
	readWidthMargin = 4
	readMinWidth    = 20
)

type readState struct {
	viewport viewport.Model
	entries  []store.EntryRecord
	session  store.SessionSearchResult
}

// readViewportSize computes the viewport's width/height from the terminal
// size. Width is capped at readMaxWidth (minus a margin) so reading stays
// comfortable on a wide terminal; height reserves readChromeLines for the
// screen's fixed text. Both floor so a tiny terminal never yields a
// non-positive viewport.
func readViewportSize(termWidth, termHeight int) (width, height int) {
	width = termWidth
	if width > readMaxWidth {
		width = readMaxWidth
	}
	width -= readWidthMargin
	if width < readMinWidth {
		width = readMinWidth
	}

	height = termHeight - readChromeLines
	if height < readMinHeight {
		height = readMinHeight
	}
	return width, height
}

// renderReadEntries renders a session's entries for the viewport, word-
// wrapping each entry's body to width (bubbles/viewport clips rather than
// wraps overly long lines). A single entry is shown with no extra header;
// multiple entries each get a dim "— entry N —" header.
func renderReadEntries(entries []store.EntryRecord, width int) string {
	wrap := lipgloss.NewStyle().Width(width)
	if len(entries) == 1 {
		return wrap.Render(entries[0].Body)
	}
	var b strings.Builder
	for i, e := range entries {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(statStyle.Render(fmt.Sprintf("— entry %d —", i+1)) + "\n")
		b.WriteString(wrap.Render(e.Body))
	}
	return b.String()
}

func (m Model) enterRead(session store.SessionSearchResult) (tea.Model, tea.Cmd) {
	entries, err := m.store.GetEntries(session.ID)
	if err != nil {
		m.err = err
		return m, nil
	}
	w, h := readViewportSize(m.width, m.height)
	vp := viewport.New(w, h)
	vp.SetContent(renderReadEntries(entries, w))
	m.read = readState{viewport: vp, entries: entries, session: session}
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
	b.WriteString(statStyle.Render(fmt.Sprintf(
		"%s   Score: %s   %s%s",
		formatSessionDate(m.read.session.StartedAt),
		formatNumber(m.read.session.SessionScore),
		formatCount(m.read.session.WordCount, "word", "words"),
		formatIntensityTag(m.read.session.PeakIntensityRatio),
	)) + "\n\n")
	b.WriteString(m.read.viewport.View() + "\n")
	b.WriteString(statStyle.Render("up/down/pgup/pgdn: scroll   esc: back to history"))
	return b.String()
}
