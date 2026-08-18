package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// typedRun is what bubbletea delivers when several runes arrive in one read:
// a single KeyMsg carrying the whole run, whose String() is the run itself.
func typedRun(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// A typed run that happens to spell a key name must be written, not obeyed.
// Every name here is one bubbles binds in the textarea, so before the fix
// each of these moved the cursor or scrolled instead of appearing on screen.
func TestTypedWordsThatSpellKeyNamesAreWritten(t *testing.T) {
	for _, word := range []string{"up", "down", "left", "right", "home", "end", "tab", "space", "delete"} {
		t.Run(word, func(t *testing.T) {
			s, _ := openStore(t)
			m, err := New(s)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			updated, _ := m.startWritingSession()
			m = updated.(Model)

			updated, _ = m.updateWriting(typedRun(word))
			m = updated.(Model)

			if got := m.writing.textarea.Value(); got != word {
				t.Fatalf("typing %q produced %q", word, got)
			}
		})
	}
}

// The worst case: "esc" is three ordinary letters, and it used to end the
// session out from under whoever was typing them.
func TestTypingEscDoesNotEndTheSession(t *testing.T) {
	s, _ := openStore(t)
	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	updated, _ := m.startWritingSession()
	m = updated.(Model)

	updated, _ = m.updateWriting(typedRun("describe the escape hatch "))
	m = updated.(Model)

	if m.screen != screenWriting {
		t.Fatalf("expected to still be writing, got screen %v", m.screen)
	}
	if !strings.Contains(m.writing.textarea.Value(), "escape hatch") {
		t.Fatalf("expected the text to be written, got %q", m.writing.textarea.Value())
	}
}

func TestTypedRunIsScored(t *testing.T) {
	s, _ := openStore(t)
	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	updated, _ := m.startWritingSession()
	m = updated.(Model)

	updated, _ = m.updateWriting(typedRun("three whole words "))
	m = updated.(Model)

	if got := m.writing.session.TotalWords(); got != 3 {
		t.Fatalf("expected a typed run to score its words, got %d", got)
	}
}

// A run inserts at the cursor like any other typing, rather than being
// appended to the end of the document.
func TestTypedRunInsertsAtTheCursor(t *testing.T) {
	s, _ := openStore(t)
	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	updated, _ := m.startWritingSession()
	m = updated.(Model)

	updated, _ = m.updateWriting(typedRun("start end"))
	m = updated.(Model)
	for i := 0; i < 3; i++ {
		updated, _ = m.updateWriting(tea.KeyMsg{Type: tea.KeyLeft})
		m = updated.(Model)
	}
	updated, _ = m.updateWriting(typedRun("mid "))
	m = updated.(Model)

	if got := m.writing.textarea.Value(); got != "start mid end" {
		t.Fatalf("expected the run inserted at the cursor, got %q", got)
	}
}

// The real keys must keep working now that they match on Type.
func TestRealControlKeysStillWork(t *testing.T) {
	s, _ := openStore(t)
	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	updated, _ := m.startWritingSession()
	m = updated.(Model)

	compact := m.compactMode
	updated, _ = m.updateWriting(tea.KeyMsg{Type: tea.KeyCtrlT})
	m = updated.(Model)
	if m.compactMode == compact {
		t.Fatal("expected ctrl+t to toggle compact mode")
	}

	updated, _ = m.updateWriting(tea.KeyMsg{Type: tea.KeyCtrlV})
	m = updated.(Model)
	if m.writing.pasteWarning != pasteWarningText {
		t.Fatalf("expected ctrl+v to warn, got %q", m.writing.pasteWarning)
	}

	m.writing.textarea.SetValue("something written ")
	m.writing.lastWordCount = syncWordCount(m.writing.session, m.writing.lastWordCount, m.writing.textarea.Value(), time.Now())
	updated, _ = m.updateWriting(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.screen != screenSummary {
		t.Fatalf("expected esc to end the session, got screen %v", m.screen)
	}
}

// A terminal paste still arrives with Paste set and must still be refused,
// even though it also carries a multi-rune run.
func TestBracketedPasteIsStillBlockedDespiteBeingARun(t *testing.T) {
	s, _ := openStore(t)
	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	updated, _ := m.startWritingSession()
	m = updated.(Model)

	pasted := typedRun("a whole paragraph someone else wrote ")
	pasted.Paste = true
	updated, _ = m.updateWriting(pasted)
	m = updated.(Model)

	if m.writing.textarea.Value() != "" {
		t.Fatalf("expected the paste to be refused, got %q", m.writing.textarea.Value())
	}
	if m.writing.pasteWarning != pasteWarningText {
		t.Fatalf("expected the paste warning, got %q", m.writing.pasteWarning)
	}
}
