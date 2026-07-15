package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nd28/journal-tui/internal/store"
)

func TestReadViewportSizeUsesTerminalHeightMinusChrome(t *testing.T) {
	w, h := readViewportSize(100, 30)
	if w != 100 {
		t.Fatalf("expected width 100, got %d", w)
	}
	if h != 30-readChromeLines {
		t.Fatalf("expected height %d, got %d", 30-readChromeLines, h)
	}
}

func TestReadViewportSizeFloorsOnTinyTerminal(t *testing.T) {
	_, h := readViewportSize(20, 5)
	if h != readMinHeight {
		t.Fatalf("expected height floor %d, got %d", readMinHeight, h)
	}
}

func TestRenderReadEntriesSingleEntryNoHeader(t *testing.T) {
	entries := []store.EntryRecord{{ID: 1, CreatedAt: "2026-07-15T10:00:00Z", Body: "just one entry", WordCount: 3}}
	got := renderReadEntries(entries)
	if got != "just one entry" {
		t.Fatalf("expected raw body with no header, got %q", got)
	}
}

func TestRenderReadEntriesMultipleEntriesGetHeaders(t *testing.T) {
	entries := []store.EntryRecord{
		{ID: 1, CreatedAt: "2026-07-15T10:00:00Z", Body: "alpha", WordCount: 1},
		{ID: 2, CreatedAt: "2026-07-15T10:01:00Z", Body: "beta", WordCount: 1},
	}
	got := renderReadEntries(entries)
	if !strings.Contains(got, "— entry 1 —") || !strings.Contains(got, "— entry 2 —") {
		t.Fatalf("expected numbered headers, got %q", got)
	}
	if !strings.Contains(got, "alpha") || !strings.Contains(got, "beta") {
		t.Fatalf("expected both bodies present, got %q", got)
	}
}

func TestEnterReadLoadsEntries(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	now := time.Now()
	id, err := s.StartSession(now)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id, now, "first entry text", 3); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if err := s.SaveEntry(id, now.Add(time.Minute), "second entry text", 3); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if _, _, err := s.FinishSession(id, now, 10, 1.0, 1, now.Format("2006-01-02")); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}

	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.width, m.height = 100, 30

	updated, _ := m.enterRead(id)
	m = updated.(Model)

	if m.screen != screenRead {
		t.Fatalf("expected screenRead, got %v", m.screen)
	}
	if len(m.read.entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(m.read.entries))
	}
	view := m.read.viewport.View()
	if !strings.Contains(view, "first entry text") || !strings.Contains(view, "second entry text") {
		t.Fatalf("expected viewport to contain both entries, got %q", view)
	}
	if !strings.Contains(view, "— entry 1 —") || !strings.Contains(view, "— entry 2 —") {
		t.Fatalf("expected entry headers for multi-entry session, got %q", view)
	}
}

func TestHistoryEnterOpensReadForSelectedSessionAndEscReturnsWithStateIntact(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	today := base.Format("2006-01-02")

	id1, err := s.StartSession(base)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id1, base, "apple pie recipe", 3); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if _, _, err := s.FinishSession(id1, base, 10, 1.0, 1, today); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}

	later := base.Add(time.Hour)
	id2, err := s.StartSession(later)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id2, later, "banana bread notes", 3); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if _, _, err := s.FinishSession(id2, later, 20, 1.0, 1, today); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}

	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.width, m.height = 100, 30

	updated, _ := m.enterHistory()
	m = updated.(Model)

	for _, r := range []rune("apple") {
		updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}
	if len(m.history.results) != 1 {
		t.Fatalf("expected 1 filtered result, got %d", len(m.history.results))
	}
	wantQuery, wantPage, wantCursor := m.history.query, m.history.page, m.history.cursor

	updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.screen != screenRead {
		t.Fatalf("expected screenRead after enter, got %v", m.screen)
	}
	if len(m.read.entries) != 1 || m.read.entries[0].Body != "apple pie recipe" {
		t.Fatalf("expected the filtered session's entry, got %+v", m.read.entries)
	}

	updated, _ = m.updateRead(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.screen != screenHistory {
		t.Fatalf("expected screenHistory after esc, got %v", m.screen)
	}
	if m.history.query != wantQuery || m.history.page != wantPage || m.history.cursor != wantCursor {
		t.Fatalf("expected history state intact, got query=%q page=%d cursor=%d", m.history.query, m.history.page, m.history.cursor)
	}
	if len(m.history.results) != 1 {
		t.Fatalf("expected results still intact without refetch, got %d", len(m.history.results))
	}
}
