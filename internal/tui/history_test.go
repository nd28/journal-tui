package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nd28/journal-tui/internal/store"
)

func TestEnterHistoryLoadsSessions(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	id, err := s.StartSession(time.Now())
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id, time.Now(), "hello world", 2); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if _, _, err := s.FinishSession(id, time.Now(), 42, 1.0, 1, time.Now().Format("2006-01-02")); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}

	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	updated, _ := m.enterHistory()
	m = updated.(Model)

	if m.screen != screenHistory {
		t.Fatalf("expected screenHistory, got %v", m.screen)
	}
	if len(m.history.results) != 1 {
		t.Fatalf("expected 1 session, got %d", len(m.history.results))
	}
	if m.history.results[0].SessionScore != 42 {
		t.Fatalf("expected score 42, got %d", m.history.results[0].SessionScore)
	}
	if m.history.results[0].Snippet != "" {
		t.Fatalf("expected empty snippet with no query, got %q", m.history.results[0].Snippet)
	}
}

func TestViewHistoryShowsIntensityTagForElevatedSessions(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	id, err := s.StartSession(time.Now())
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id, time.Now(), "hello world", 2); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if _, _, err := s.FinishSession(id, time.Now(), 42, 1.0, 1, time.Now().Format("2006-01-02")); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}
	if err := s.RecordSessionPace(id, 50, 2.1); err != nil {
		t.Fatalf("RecordSessionPace: %v", err)
	}

	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	updated, _ := m.enterHistory()
	m = updated.(Model)

	if got := m.viewHistory(); !strings.Contains(got, "· Intense") {
		t.Fatalf("expected the history line to show the Intense tag, got %q", got)
	}
}

func TestHistoryQueryTypingFiltersResults(t *testing.T) {
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

	updated, _ := m.enterHistory()
	m = updated.(Model)
	if len(m.history.results) != 2 {
		t.Fatalf("expected 2 results before filtering, got %d", len(m.history.results))
	}

	for _, r := range []rune("banana") {
		updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}

	if m.history.query != "banana" {
		t.Fatalf("expected query %q, got %q", "banana", m.history.query)
	}
	if len(m.history.results) != 1 {
		t.Fatalf("expected 1 filtered result, got %d", len(m.history.results))
	}
	if m.history.results[0].ID != id2 {
		t.Fatalf("expected session %d, got %d", id2, m.history.results[0].ID)
	}
	if m.history.results[0].Snippet != "banana bread notes" {
		t.Fatalf("expected snippet %q, got %q", "banana bread notes", m.history.results[0].Snippet)
	}
}

func TestHistoryBackspaceRemovesLastQueryChar(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	updated, _ := m.enterHistory()
	m = updated.(Model)

	for _, r := range []rune("abc") {
		updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}
	updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(Model)

	if m.history.query != "ab" {
		t.Fatalf("expected query %q after backspace, got %q", "ab", m.history.query)
	}
}

func TestHistoryCtrlUClearsQuery(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	updated, _ := m.enterHistory()
	m = updated.(Model)

	for _, r := range []rune("abc") {
		updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}
	updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = updated.(Model)

	if m.history.query != "" {
		t.Fatalf("expected empty query after ctrl+u, got %q", m.history.query)
	}
}

func TestHistoryPageNavigationClamps(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	today := base.Format("2006-01-02")
	for i := 0; i < 15; i++ {
		startedAt := base.Add(time.Duration(i) * time.Hour)
		id, err := s.StartSession(startedAt)
		if err != nil {
			t.Fatalf("StartSession: %v", err)
		}
		if err := s.SaveEntry(id, startedAt, "day entry", 2); err != nil {
			t.Fatalf("SaveEntry: %v", err)
		}
		if _, _, err := s.FinishSession(id, startedAt, 10, 1.0, 1, today); err != nil {
			t.Fatalf("FinishSession: %v", err)
		}
	}

	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	updated, _ := m.enterHistory()
	m = updated.(Model)

	if len(m.history.results) != 10 || m.history.total != 15 {
		t.Fatalf("expected 10 results of 15 total on page 0, got %d of %d", len(m.history.results), m.history.total)
	}

	updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyPgDown})
	m = updated.(Model)
	if m.history.page != 1 || len(m.history.results) != 5 {
		t.Fatalf("expected page 1 with 5 results, got page %d with %d results", m.history.page, len(m.history.results))
	}

	updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyPgDown})
	m = updated.(Model)
	if m.history.page != 1 {
		t.Fatalf("expected page to stay clamped at 1, got %d", m.history.page)
	}

	updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyPgUp})
	m = updated.(Model)
	if m.history.page != 0 || len(m.history.results) != 10 {
		t.Fatalf("expected page 0 with 10 results, got page %d with %d results", m.history.page, len(m.history.results))
	}

	updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyPgUp})
	m = updated.(Model)
	if m.history.page != 0 {
		t.Fatalf("expected page to stay clamped at 0, got %d", m.history.page)
	}
}

func TestHistoryUpDownMovesCursorWithinPageAndClamps(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	today := base.Format("2006-01-02")
	for i := 0; i < 3; i++ {
		startedAt := base.Add(time.Duration(i) * time.Hour)
		id, err := s.StartSession(startedAt)
		if err != nil {
			t.Fatalf("StartSession: %v", err)
		}
		if err := s.SaveEntry(id, startedAt, "note", 1); err != nil {
			t.Fatalf("SaveEntry: %v", err)
		}
		if _, _, err := s.FinishSession(id, startedAt, 10, 1.0, 1, today); err != nil {
			t.Fatalf("FinishSession: %v", err)
		}
	}

	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	updated, _ := m.enterHistory()
	m = updated.(Model)

	if m.history.cursor != 0 {
		t.Fatalf("expected cursor to start at 0, got %d", m.history.cursor)
	}

	updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.history.cursor != 2 {
		t.Fatalf("expected cursor 2, got %d", m.history.cursor)
	}

	updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.history.cursor != 2 {
		t.Fatalf("expected cursor clamped at 2, got %d", m.history.cursor)
	}

	updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	if m.history.cursor != 1 {
		t.Fatalf("expected cursor 1, got %d", m.history.cursor)
	}
}
