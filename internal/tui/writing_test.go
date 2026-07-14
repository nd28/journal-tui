package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"journal/internal/scoring"
	"journal/internal/store"
)

func TestSyncWordCountAwardsPointsForNewWords(t *testing.T) {
	now := time.Now()
	sess := scoring.NewSession(now)
	words := syncWordCount(sess, 0, "hello world", now)
	if words != 2 {
		t.Fatalf("expected 2 words, got %d", words)
	}
	if sess.RawScore() == 0 {
		t.Fatal("expected a non-zero score after completing words")
	}
}

func TestSyncWordCountIgnoresDeletedWords(t *testing.T) {
	now := time.Now()
	sess := scoring.NewSession(now)
	words := syncWordCount(sess, 0, "hello world", now)
	scoreBefore := sess.RawScore()

	words = syncWordCount(sess, words, "hello", now)
	if words != 1 {
		t.Fatalf("expected 1 word after deletion, got %d", words)
	}
	if sess.RawScore() != scoreBefore {
		t.Fatalf("expected score to stay at %d after deletion, got %d", scoreBefore, sess.RawScore())
	}
}

func TestRenderComboBarAtFloorIsEmpty(t *testing.T) {
	bar := renderComboBar(scoring.ComboFloor, 10)
	if strings.Contains(bar, "█") {
		t.Fatalf("expected no filled blocks at floor multiplier, got %q", bar)
	}
}

func TestRenderComboBarAtCapIsFull(t *testing.T) {
	bar := renderComboBar(scoring.ComboCap, 10)
	if strings.Count(bar, "█") != 10 {
		t.Fatalf("expected 10 filled blocks at cap multiplier, got %q", bar)
	}
}

func TestEndWritingSessionPersistsAndUpdatesStats(t *testing.T) {
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

	updated, _ := m.startWritingSession()
	m = updated.(Model)
	if m.screen != screenWriting {
		t.Fatalf("expected screenWriting, got %v", m.screen)
	}

	now := time.Now()
	m.writing.textarea.SetValue("hello world this is a test")
	m.writing.lastWordCount = syncWordCount(m.writing.session, m.writing.lastWordCount, m.writing.textarea.Value(), now)

	updated, _ = m.endWritingSession()
	m = updated.(Model)

	if m.screen != screenSummary {
		t.Fatalf("expected screenSummary, got %v", m.screen)
	}
	if m.summary.finalScore == 0 {
		t.Fatal("expected a non-zero final score")
	}
	if m.summary.totalWords != 6 {
		t.Fatalf("expected 6 words, got %d", m.summary.totalWords)
	}
	if m.stats.LifetimeScore != m.summary.finalScore {
		t.Fatalf("expected lifetime score %d to equal session score %d on the first session", m.stats.LifetimeScore, m.summary.finalScore)
	}
	if !m.summary.isNewHigh {
		t.Fatal("expected the first session to be a new high score")
	}
}
