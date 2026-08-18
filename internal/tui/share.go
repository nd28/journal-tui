package tui

import (
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/nd28/journal-tui/internal/store"
)

// copyToClipboard is a variable rather than a direct call so tests can swap
// the machine's real clipboard out.
var copyToClipboard = clipboard.WriteAll

// renderShareText renders a session as plain text for pasting elsewhere: the
// same stat header the Read screen shows, a blank line, then the entry
// bodies. Unlike the Read screen's rendering it carries no styling and no
// wrapping — whatever the text is pasted into does its own wrapping, and
// hard-wrapped text pasted into a chat window reads badly.
func renderShareText(session store.SessionSearchResult, entries []store.EntryRecord) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf(
		"%s   Score: %s   %s%s",
		formatSessionDate(session.StartedAt),
		formatNumber(session.SessionScore),
		formatCount(session.WordCount, "word", "words"),
		formatIntensityTag(session.PeakIntensityRatio),
	))
	b.WriteString("\n\n")
	if len(entries) == 1 {
		b.WriteString(entries[0].Body)
		return b.String()
	}
	for i, e := range entries {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(fmt.Sprintf("— entry %d —\n", i+1))
		b.WriteString(e.Body)
	}
	return b.String()
}

// shareSession copies a session's text to the clipboard and reports the
// outcome on the status line. A clipboard failure — no xclip or wl-copy
// installed, a headless machine — surfaces as an ordinary error rather than
// silently copying nothing.
func (m Model) shareSession(session store.SessionSearchResult, entries []store.EntryRecord) (tea.Model, tea.Cmd) {
	if err := copyToClipboard(renderShareText(session, entries)); err != nil {
		m.err = err
		return m, nil
	}
	m.shareStatus = "copied " + formatCount(len(entries), "entry", "entries") + " to the clipboard"
	return m, nil
}
