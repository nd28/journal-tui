package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nd28/journal-tui/internal/scoring"
	"github.com/nd28/journal-tui/internal/store"
)

func openStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "journal.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

// startWriting opens a fresh session and types text into it, mirroring what
// the update loop does on every keystroke.
func startWriting(t *testing.T, m Model, text string) Model {
	t.Helper()
	updated, _ := m.startWritingSession()
	m = updated.(Model)
	m.writing.textarea.SetValue(text)
	m.writing.lastWordCount = syncWordCount(m.writing.session, m.writing.lastWordCount, text, time.Now())
	return m
}

func TestAutosaveTickPersistsTheBuffer(t *testing.T) {
	s, _ := openStore(t)
	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m = startWriting(t, m, "hello world ")

	updated, cmd := m.updateWriting(autosaveTickMsg{sessionID: m.writing.sessionID})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("expected the autosave ticker to re-arm itself")
	}

	d, ok, err := s.GetDraft(m.writing.sessionID)
	if err != nil {
		t.Fatalf("GetDraft: %v", err)
	}
	if !ok {
		t.Fatal("expected an autosave tick to write a draft")
	}
	if d.Body != "hello world " {
		t.Fatalf("expected the buffer to be mirrored, got %q", d.Body)
	}
	if d.RawScore != m.writing.session.RawScore() || d.TotalWords != m.writing.session.TotalWords() {
		t.Fatalf("expected the running score to be saved alongside the text, got score=%d words=%d",
			d.RawScore, d.TotalWords)
	}
	if m.writing.savedDraft != "hello world " {
		t.Fatalf("expected savedDraft to track what was written, got %q", m.writing.savedDraft)
	}
}

// A writer who stops to think shouldn't cause a database write every five
// seconds for the rest of the session.
func TestAutosaveTickSkipsAnUnchangedBuffer(t *testing.T) {
	s, _ := openStore(t)
	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m = startWriting(t, m, "first version ")
	m.saveDraft()

	// The buffer moves on, but savedDraft already claims to match it: the
	// tick must trust the flag and leave the stored row alone.
	m.writing.textarea.SetValue("second version ")
	m.writing.savedDraft = "second version "

	updated, _ := m.updateWriting(autosaveTickMsg{sessionID: m.writing.sessionID})
	m = updated.(Model)

	d, _, err := s.GetDraft(m.writing.sessionID)
	if err != nil {
		t.Fatalf("GetDraft: %v", err)
	}
	if d.Body != "first version " {
		t.Fatalf("expected no write for an unchanged buffer, got %q", d.Body)
	}
}

func TestEndWritingSessionClearsTheDraft(t *testing.T) {
	s, _ := openStore(t)
	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m = startWriting(t, m, "some words to keep ")
	m.saveDraft()
	sessionID := m.writing.sessionID

	updated, _ := m.endWritingSession()
	m = updated.(Model)

	if _, ok, err := s.GetDraft(sessionID); err != nil || ok {
		t.Fatalf("expected the draft to be dropped once the text became an entry, got ok=%v err=%v", ok, err)
	}
	if m.recovery != nil {
		t.Fatalf("expected no session left to recover, got %+v", m.recovery)
	}
}

// ctrl+c used to quit outright: the buffer was dropped and the session row
// stayed unfinished, so its writing never reached History.
func TestCtrlCEndsTheSessionInsteadOfDroppingIt(t *testing.T) {
	s, _ := openStore(t)
	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m = startWriting(t, m, "words that must survive a panic exit ")

	updated, cmd := m.updateWriting(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(Model)

	if cmd == nil {
		t.Fatal("expected ctrl+c to still quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", cmd())
	}

	results, total, err := s.SearchSessions("", 10, 0)
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if total != 1 || len(results) != 1 {
		t.Fatalf("expected the session to be finished and visible in History, got total=%d", total)
	}

	entries, err := s.GetEntries(results[0].ID)
	if err != nil {
		t.Fatalf("GetEntries: %v", err)
	}
	if len(entries) != 1 || !strings.Contains(entries[0].Body, "must survive") {
		t.Fatalf("expected the buffer to be saved on ctrl+c, got %+v", entries)
	}

	stats, err := s.GetStats()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.LifetimeScore == 0 {
		t.Fatal("expected the session's score to count toward the lifetime total")
	}
}

func TestNewSweepsEmptySessionsAndOffersTheOneWithWriting(t *testing.T) {
	s, _ := openStore(t)
	base := time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)

	if _, err := s.StartSession(base); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	withText, err := s.StartSession(base.Add(time.Hour))
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveDraft(withText, base.Add(time.Hour), "a day of notes ", 90, 4); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	unfinished, err := s.UnfinishedSessions()
	if err != nil {
		t.Fatalf("UnfinishedSessions: %v", err)
	}
	if len(unfinished) != 1 || unfinished[0].ID != withText {
		t.Fatalf("expected only the session holding writing to survive the sweep, got %+v", unfinished)
	}

	if m.recovery == nil || m.recovery.ID != withText {
		t.Fatalf("expected the session with writing to be offered, got %+v", m.recovery)
	}
	if m.recoveryCount != 1 {
		t.Fatalf("expected recoveryCount 1, got %d", m.recoveryCount)
	}

	items := m.menuItems()
	if items[0] != resumeMenuItem {
		t.Fatalf("expected the resume entry first on the menu, got %q", items[0])
	}
	view := m.viewHome()
	if !strings.Contains(view, "Unfinished session") {
		t.Fatalf("expected the home screen to say a session is waiting, got %q", view)
	}
	if !strings.Contains(view, "4 words") {
		t.Fatalf("expected the notice to report how much writing is waiting, got %q", view)
	}
}

func TestHomeMenuHidesResumeWhenNothingIsWaiting(t *testing.T) {
	s, _ := openStore(t)
	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := m.menuItems(); len(got) != len(homeMenuItems) || got[0] != "New Session" {
		t.Fatalf("expected the plain menu with nothing to recover, got %v", got)
	}
	if strings.Contains(m.viewHome(), "Unfinished") {
		t.Fatalf("expected no recovery notice, got %q", m.viewHome())
	}
}

func TestResumeRestoresDraftTextAndRunningScore(t *testing.T) {
	s, _ := openStore(t)
	base := time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)

	id, err := s.StartSession(base)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	const draft = "notes from before the battery died "
	if err := s.SaveDraft(id, base, draft, 350, 6); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// The cursor starts on the resume entry, so plain enter picks it.
	updated, _ := m.updateHome(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if m.screen != screenWriting {
		t.Fatalf("expected the writing screen, got %v", m.screen)
	}
	if m.writing.sessionID != id {
		t.Fatalf("expected to reopen session %d, got %d", id, m.writing.sessionID)
	}
	if m.writing.textarea.Value() != draft {
		t.Fatalf("expected the draft text back in the editor, got %q", m.writing.textarea.Value())
	}
	if got := m.writing.session.RawScore(); got != 350 {
		t.Fatalf("expected the running score restored, got %d", got)
	}
	if got := m.writing.session.TotalWords(); got != 6 {
		t.Fatalf("expected the running word count restored, got %d", got)
	}
	if m.recovery != nil {
		t.Fatalf("expected the resumed session to leave the menu, got %+v", m.recovery)
	}

	// The recovered words were paid for during the first run: reconciling
	// the unchanged buffer must not pay for them again.
	before := m.writing.session.RawScore()
	m.writing.lastWordCount = syncWordCount(m.writing.session, m.writing.lastWordCount, m.writing.textarea.Value(), time.Now())
	if got := m.writing.session.RawScore(); got != before {
		t.Fatalf("expected recovered words not to score twice, went from %d to %d", before, got)
	}
}

func TestResumedSessionSavesRecoveredTextToHistory(t *testing.T) {
	s, _ := openStore(t)
	base := time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)

	id, err := s.StartSession(base)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveDraft(id, base, "recovered writing ", 60, 2); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	updated, _ := m.resumeWritingSession(*m.recovery)
	m = updated.(Model)

	updated, _ = m.endWritingSession()
	m = updated.(Model)

	if m.screen != screenSummary {
		t.Fatalf("expected the summary screen, got %v", m.screen)
	}
	results, total, err := s.SearchSessions("recovered", 10, 0)
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if total != 1 || len(results) != 1 || results[0].ID != id {
		t.Fatalf("expected the resumed session to appear in History, got total=%d results=%+v", total, results)
	}
	if _, ok, _ := s.GetDraft(id); ok {
		t.Fatal("expected the draft to be cleared once the session finished")
	}
}

// Sessions interrupted before autosave existed have entries but no recorded
// score. Resuming one must not report writing worth zero points.
func TestResumeCreditsEntriesFromBeforeAutosave(t *testing.T) {
	s, _ := openStore(t)
	base := time.Date(2026, 7, 28, 9, 0, 0, 0, time.UTC)

	id, err := s.StartSession(base)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id, base, "fifty words of older writing", 50); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}

	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	updated, _ := m.resumeWritingSession(*m.recovery)
	m = updated.(Model)

	if m.writing.textarea.Value() != "" {
		t.Fatalf("expected an empty editor with no draft to restore, got %q", m.writing.textarea.Value())
	}
	if got := m.writing.session.TotalWords(); got != 50 {
		t.Fatalf("expected the committed words to be carried over, got %d", got)
	}
	if want := 50 * scoring.BasePointsPerWord; m.writing.session.RawScore() != want {
		t.Fatalf("expected %d points credited at the base rate, got %d", want, m.writing.session.RawScore())
	}
}

// ctrl+n empties the buffer but not the score. The draft has to follow, or a
// crash right afterwards restores a stale buffer on top of a saved entry.
func TestNewEntryKeyRewritesTheDraft(t *testing.T) {
	s, _ := openStore(t)
	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m = startWriting(t, m, "first entry text ")
	m.saveDraft()

	updated, _ := m.updateWriting(tea.KeyMsg{Type: tea.KeyCtrlN})
	m = updated.(Model)

	d, ok, err := s.GetDraft(m.writing.sessionID)
	if err != nil {
		t.Fatalf("GetDraft: %v", err)
	}
	if !ok {
		t.Fatal("expected the draft to be rewritten, not deleted")
	}
	if d.Body != "" {
		t.Fatalf("expected an empty draft body after the entry was committed, got %q", d.Body)
	}
	if d.TotalWords != m.writing.session.TotalWords() || d.RawScore != m.writing.session.RawScore() {
		t.Fatalf("expected the running totals to survive the new entry, got score=%d words=%d",
			d.RawScore, d.TotalWords)
	}
}

// bubbles caps a textarea at 99 lines by silently ignoring enter. A session
// used for a day of notes runs straight into that wall.
func TestWritingTextareaAcceptsMoreThan99Lines(t *testing.T) {
	ta := newWritingTextarea(80, 24, false)
	ta.Focus()
	for i := 0; i < 150; i++ {
		ta, _ = ta.Update(tea.KeyMsg{Type: tea.KeyEnter})
	}
	if got := ta.LineCount(); got <= 99 {
		t.Fatalf("expected the line cap lifted, stopped at %d lines", got)
	}
}

func TestWritingScreenReportsWhetherTheBufferIsSaved(t *testing.T) {
	s, _ := openStore(t)
	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.width = 120
	m.height = 40
	m = startWriting(t, m, "words not yet on disk ")

	if !strings.Contains(m.viewWriting(), "unsaved") {
		t.Fatalf("expected the writing screen to flag unsaved text, got %q", m.viewWriting())
	}

	updated, _ := m.updateWriting(autosaveTickMsg{sessionID: m.writing.sessionID})
	m = updated.(Model)

	view := m.viewWriting()
	if !strings.Contains(view, "saved") || strings.Contains(view, "unsaved") {
		t.Fatalf("expected the screen to report saved after an autosave, got %q", view)
	}
}

// A tick armed by a session that has since ended must die where it lands
// rather than re-arming against whatever session is open now.
func TestAutosaveTickFromAnEndedSessionDoesNotRearm(t *testing.T) {
	s, _ := openStore(t)
	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m = startWriting(t, m, "current session text ")

	stale := autosaveTickMsg{sessionID: m.writing.sessionID + 1000}
	updated, cmd := m.updateWriting(stale)
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("expected a stale tick not to re-arm the ticker")
	}
	if _, ok, err := s.GetDraft(m.writing.sessionID); err != nil || ok {
		t.Fatalf("expected a stale tick not to write a draft, got ok=%v err=%v", ok, err)
	}
}
