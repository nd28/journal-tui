package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nd28/journal-tui/internal/store"
)

func TestReadViewportSizeClampsToMaxWidth(t *testing.T) {
	w, h := readViewportSize(200, 30)
	if w != readMaxWidth-readWidthMargin {
		t.Fatalf("expected width %d, got %d", readMaxWidth-readWidthMargin, w)
	}
	if h != 30-readChromeLines {
		t.Fatalf("expected height %d, got %d", 30-readChromeLines, h)
	}
}

func TestReadViewportSizeUsesSmallerTerminal(t *testing.T) {
	w, _ := readViewportSize(60, 30)
	if w != 60-readWidthMargin {
		t.Fatalf("expected width %d, got %d", 60-readWidthMargin, w)
	}
}

func TestReadViewportSizeFloorsOnTinyTerminal(t *testing.T) {
	w, h := readViewportSize(10, 5)
	if w != readMinWidth {
		t.Fatalf("expected width floor %d, got %d", readMinWidth, w)
	}
	if h != readMinHeight {
		t.Fatalf("expected height floor %d, got %d", readMinHeight, h)
	}
}

func TestRenderReadEntriesSingleEntryNoHeader(t *testing.T) {
	entries := []store.EntryRecord{{ID: 1, CreatedAt: "2026-07-15T10:00:00Z", Body: "just one entry", WordCount: 3}}
	// lipgloss's width-based wrapping also right-pads short lines to the
	// full width (invisible in a terminal) — trim that before comparing.
	got := strings.TrimRight(renderReadEntries(entries, 80), " ")
	if got != "just one entry" {
		t.Fatalf("expected raw body with no header, got %q", got)
	}
}

func TestRenderReadEntriesMultipleEntriesGetHeaders(t *testing.T) {
	entries := []store.EntryRecord{
		{ID: 1, CreatedAt: "2026-07-15T10:00:00Z", Body: "alpha", WordCount: 1},
		{ID: 2, CreatedAt: "2026-07-15T10:01:00Z", Body: "beta", WordCount: 1},
	}
	got := renderReadEntries(entries, 80)
	if !strings.Contains(got, "— entry 1 —") || !strings.Contains(got, "— entry 2 —") {
		t.Fatalf("expected numbered headers, got %q", got)
	}
	if !strings.Contains(got, "alpha") || !strings.Contains(got, "beta") {
		t.Fatalf("expected both bodies present, got %q", got)
	}
}

func TestRenderReadEntriesWrapsLongLinesToWidth(t *testing.T) {
	long := strings.TrimSpace(strings.Repeat("word ", 40))
	entries := []store.EntryRecord{{ID: 1, CreatedAt: "2026-07-15T10:00:00Z", Body: long, WordCount: 40}}

	got := renderReadEntries(entries, 20)

	lines := strings.Split(got, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected long text to wrap across multiple lines, got %d line(s): %q", len(lines), got)
	}
	for _, line := range lines {
		if len(line) > 20 {
			t.Fatalf("expected no line wider than 20 chars, got %d: %q", len(line), line)
		}
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

	session := store.SessionSearchResult{SessionRecord: store.SessionRecord{ID: id, StartedAt: "2026-07-15T10:00:00Z", SessionScore: 10, WordCount: 6}}
	updated, _ := m.enterRead(session)
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

func TestViewReadIncludesSessionStats(t *testing.T) {
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
	if err := s.SaveEntry(id, now, "some text", 2); err != nil {
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

	session := store.SessionSearchResult{SessionRecord: store.SessionRecord{ID: id, StartedAt: "2026-07-15T10:00:00Z", SessionScore: 42, WordCount: 7}}
	updated, _ := m.enterRead(session)
	m = updated.(Model)

	got := m.viewRead()
	if !strings.Contains(got, "Jul 15, 2026 · 10:00 AM") {
		t.Fatalf("expected view to show a human-readable date, got %q", got)
	}
	if !strings.Contains(got, "Score: 42") {
		t.Fatalf("expected view to show the session's score, got %q", got)
	}
	if !strings.Contains(got, "7 words") {
		t.Fatalf("expected view to show the session's word count, got %q", got)
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
