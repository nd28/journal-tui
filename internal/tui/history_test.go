package tui

import (
	"path/filepath"
	"testing"
	"time"

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
	if len(m.history.sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(m.history.sessions))
	}
	if m.history.sessions[0].SessionScore != 42 {
		t.Fatalf("expected score 42, got %d", m.history.sessions[0].SessionScore)
	}
}
