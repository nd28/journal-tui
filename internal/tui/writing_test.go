package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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

func TestEndWritingSessionWithNoWordsSkipsPersistence(t *testing.T) {
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

	// No typing happens: end the session immediately.
	updated, _ = m.endWritingSession()
	m = updated.(Model)

	if m.screen != screenHome {
		t.Fatalf("expected screenHome for a zero-word session, got %v", m.screen)
	}

	stats, err := m.store.GetStats()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.LifetimeScore != 0 {
		t.Fatalf("expected LifetimeScore to remain 0, got %d", stats.LifetimeScore)
	}
	if stats.CurrentStreak != 0 {
		t.Fatalf("expected CurrentStreak to remain 0, got %d", stats.CurrentStreak)
	}
}

func TestWritingDimensionsFullModeClampsToMaxWidth(t *testing.T) {
	w, h := writingDimensions(200, 50, false)
	if w != writingMaxWidth-writingWidthMargin {
		t.Fatalf("expected width %d, got %d", writingMaxWidth-writingWidthMargin, w)
	}
	if h != 50-writingChromeLines-writingHeightMargin {
		t.Fatalf("expected height %d, got %d", 50-writingChromeLines-writingHeightMargin, h)
	}
}

func TestWritingDimensionsFullModeUsesSmallerTerminal(t *testing.T) {
	w, h := writingDimensions(60, 20, false)
	if w != 60-writingWidthMargin {
		t.Fatalf("expected width %d, got %d", 60-writingWidthMargin, w)
	}
	if h != 20-writingChromeLines-writingHeightMargin {
		t.Fatalf("expected height %d, got %d", 20-writingChromeLines-writingHeightMargin, h)
	}
}

func TestWritingDimensionsFullModeFloorsOnTinyTerminal(t *testing.T) {
	w, h := writingDimensions(10, 5, false)
	if w != writingMinWidth {
		t.Fatalf("expected width floor %d, got %d", writingMinWidth, w)
	}
	if h != writingMinHeight {
		t.Fatalf("expected height floor %d, got %d", writingMinHeight, h)
	}
}

func TestWritingDimensionsCompactModeIgnoresTerminalSize(t *testing.T) {
	w, h := writingDimensions(200, 80, true)
	if w != compactWidth || h != compactHeight {
		t.Fatalf("expected compact %dx%d, got %dx%d", compactWidth, compactHeight, w, h)
	}
}

func TestCtrlTTogglesCompactMode(t *testing.T) {
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
	m.width, m.height = 120, 40

	updated, _ := m.startWritingSession()
	m = updated.(Model)
	if m.compactMode {
		t.Fatal("expected to start in Full mode")
	}
	if got := m.writing.textarea.Width(); got != writingMaxWidth-writingWidthMargin {
		t.Fatalf("expected initial full width %d, got %d", writingMaxWidth-writingWidthMargin, got)
	}

	updated, _ = m.updateWriting(tea.KeyMsg{Type: tea.KeyCtrlT})
	m = updated.(Model)
	if !m.compactMode {
		t.Fatal("expected compact mode after ctrl+t")
	}
	if got := m.writing.textarea.Width(); got != compactWidth {
		t.Fatalf("expected compact width %d after toggle, got %d", compactWidth, got)
	}

	updated, _ = m.updateWriting(tea.KeyMsg{Type: tea.KeyCtrlT})
	m = updated.(Model)
	if m.compactMode {
		t.Fatal("expected full mode after second ctrl+t")
	}
	if got := m.writing.textarea.Width(); got != writingMaxWidth-writingWidthMargin {
		t.Fatalf("expected full width %d after second toggle, got %d", writingMaxWidth-writingWidthMargin, got)
	}
}

func TestWindowSizeMsgResizesActiveTextarea(t *testing.T) {
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
	m.width, m.height = 120, 40

	updated, _ := m.startWritingSession()
	m = updated.(Model)

	updated, _ = m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m = updated.(Model)

	if got := m.writing.textarea.Width(); got != 60-writingWidthMargin {
		t.Fatalf("expected resized width %d, got %d", 60-writingWidthMargin, got)
	}
	if got := m.writing.textarea.Height(); got != 20-writingChromeLines-writingHeightMargin {
		t.Fatalf("expected resized height %d, got %d", 20-writingChromeLines-writingHeightMargin, got)
	}
}
