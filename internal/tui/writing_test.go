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

func TestStartWritingSessionFetchesBaseline(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	today := base.Format("2006-01-02")
	for i, pace := range []float64{20, 30, 40} {
		startedAt := base.Add(time.Duration(i) * time.Hour)
		id, err := s.StartSession(startedAt)
		if err != nil {
			t.Fatalf("StartSession: %v", err)
		}
		if _, _, err := s.FinishSession(id, startedAt, 10, 1.0, 1, today); err != nil {
			t.Fatalf("FinishSession: %v", err)
		}
		if err := s.RecordSessionPace(id, pace, 0); err != nil {
			t.Fatalf("RecordSessionPace: %v", err)
		}
	}

	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	updated, _ := m.startWritingSession()
	m = updated.(Model)

	if !m.writing.hasBaseline {
		t.Fatal("expected a baseline after 3 recorded sessions")
	}
	if want := (20.0 + 30.0 + 40.0) / 3.0; m.writing.baselineWPM != want {
		t.Fatalf("expected baseline %v, got %v", want, m.writing.baselineWPM)
	}
}

func TestComboTickUpdatesIntensityAndTracksPeak(t *testing.T) {
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
	m.writing.hasBaseline = true
	m.writing.baselineWPM = 10

	now := time.Now()
	m.writing.session.CompleteWord(now)
	m.writing.session.CompleteWord(now.Add(1 * time.Second))

	// 2 words 1s apart floors to a 5s window: 24 WPM / 10 baseline = 2.4x.
	tickTime := now.Add(1 * time.Second)
	updated, _ = m.updateWriting(comboTickMsg(tickTime))
	m = updated.(Model)

	if got := m.writing.intensityRatio; got != 2.4 {
		t.Fatalf("expected intensity ratio 2.4, got %v", got)
	}
	if got := m.writing.peakIntensityRatio; got != 2.4 {
		t.Fatalf("expected peak ratio 2.4, got %v", got)
	}

	// A later tick, with no new words typed, must show the live ratio
	// dropping (pace has cooled since the last word) while the tracked
	// peak stays at its high-water mark. 40s after the last word: WPM =
	// 2 words / (40s in minutes) = 3.0 WPM; ratio = 3.0 / 10 = 0.3 — well
	// below the Focused threshold, so intensityRatio must fall, but
	// peakIntensityRatio must not.
	laterTick := now.Add(40 * time.Second)
	updated, _ = m.updateWriting(comboTickMsg(laterTick))
	m = updated.(Model)
	if got := m.writing.intensityRatio; got != 0.3 {
		t.Fatalf("expected intensity ratio to drop to 0.3, got %v", got)
	}
	if got := m.writing.peakIntensityRatio; got != 2.4 {
		t.Fatalf("expected peak ratio to remain 2.4 after a later, slower tick, got %v", got)
	}
}

func TestViewWritingShowsTierTagWhenElevated(t *testing.T) {
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

	m.writing.intensityRatio = scoring.IntensityIntenseRatio
	if got := m.viewWriting(); !strings.Contains(got, "Intense") {
		t.Fatalf("expected view to show the Intense tier tag, got %q", got)
	}
}

func TestViewWritingHidesTierTagAtNormalPace(t *testing.T) {
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

	got := m.viewWriting()
	if strings.Contains(got, "Focused") || strings.Contains(got, "Intense") || strings.Contains(got, "Frantic") {
		t.Fatalf("expected no tier tag at normal pace, got %q", got)
	}
}

func TestComboTickUpdatesLiveWPMWithoutBaseline(t *testing.T) {
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
	if m.writing.hasBaseline {
		t.Fatal("expected no baseline for a fresh store")
	}

	now := time.Now()
	m.writing.session.CompleteWord(now)
	m.writing.session.CompleteWord(now.Add(1 * time.Second))

	// 2 words 1s apart floors to a 5s window: 2 / (5s in minutes) = 24 WPM.
	tickTime := now.Add(1 * time.Second)
	updated, _ = m.updateWriting(comboTickMsg(tickTime))
	m = updated.(Model)

	if got := m.writing.liveWPM; got != 24 {
		t.Fatalf("expected live WPM 24 without a baseline, got %v", got)
	}
	if got := m.writing.intensityRatio; got != 0 {
		t.Fatalf("expected intensity ratio to stay 0 without a baseline, got %v", got)
	}
}

func TestEndWritingSessionRecordsPace(t *testing.T) {
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
	m.writing.peakIntensityRatio = 2.1

	now := time.Now()
	m.writing.textarea.SetValue("hello world")
	m.writing.lastWordCount = syncWordCount(m.writing.session, m.writing.lastWordCount, m.writing.textarea.Value(), now)

	updated, _ = m.endWritingSession()
	m = updated.(Model)

	if m.summary.peakIntensityRatio != 2.1 {
		t.Fatalf("expected summary peak ratio 2.1, got %v", m.summary.peakIntensityRatio)
	}

	results, _, err := s.SearchSessions("", 10, 0)
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 finished session, got %d", len(results))
	}
	if results[0].PeakIntensityRatio != 2.1 {
		t.Fatalf("expected persisted peak ratio 2.1, got %v", results[0].PeakIntensityRatio)
	}
	if results[0].AvgPaceWPM <= 0 {
		t.Fatalf("expected a positive recorded avg pace, got %v", results[0].AvgPaceWPM)
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

func TestPasteIsBlockedAndWarns(t *testing.T) {
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

	updated, _ = m.updateWriting(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("pasted text"), Paste: true})
	m = updated.(Model)

	if m.writing.textarea.Value() != "" {
		t.Fatalf("expected pasted text to be blocked, got textarea value %q", m.writing.textarea.Value())
	}
	if m.writing.pasteWarning == "" {
		t.Fatal("expected a paste warning to be set")
	}
	if !strings.Contains(m.viewWriting(), m.writing.pasteWarning) {
		t.Fatalf("expected viewWriting to render the paste warning %q", m.writing.pasteWarning)
	}
}

func TestPasteWarningClearsOnNextNormalKey(t *testing.T) {
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

	updated, _ = m.updateWriting(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x"), Paste: true})
	m = updated.(Model)
	if m.writing.pasteWarning == "" {
		t.Fatal("expected a paste warning to be set")
	}

	updated, _ = m.updateWriting(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = updated.(Model)
	if m.writing.pasteWarning != "" {
		t.Fatalf("expected paste warning to clear after a normal key, got %q", m.writing.pasteWarning)
	}
	if m.writing.textarea.Value() != "h" {
		t.Fatalf("expected normal key to reach the textarea, got %q", m.writing.textarea.Value())
	}
}
