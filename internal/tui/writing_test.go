package tui

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nd28/journal-tui/internal/scoring"
	"github.com/nd28/journal-tui/internal/store"
)

// countSessionRows counts every session row, including unfinished ones that
// SearchSessions filters out — the only way to see rows left behind.
func countSessionRows(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	return n
}

func TestSyncWordCountAwardsPointsForCompletedWords(t *testing.T) {
	now := time.Now()
	sess := scoring.NewSession(now)
	words := syncWordCount(sess, 0, "hello world ", now)
	if words != 2 {
		t.Fatalf("expected 2 words, got %d", words)
	}
	if sess.RawScore() == 0 {
		t.Fatal("expected a non-zero score after completing words")
	}
}

func TestSyncWordCountIgnoresWordStillBeingTyped(t *testing.T) {
	now := time.Now()
	sess := scoring.NewSession(now)
	if got := syncWordCount(sess, 0, "hello wor", now); got != 1 {
		t.Fatalf("expected only the finished word to count, got %d", got)
	}
	if got := syncWordCount(sess, 1, "hello world", now); got != 1 {
		t.Fatalf("expected the trailing word to stay uncounted until terminated, got %d", got)
	}
	if got := syncWordCount(sess, 1, "hello world ", now); got != 2 {
		t.Fatalf("expected the word to count once terminated, got %d", got)
	}
}

func TestSyncWordCountIgnoresDeletedWords(t *testing.T) {
	now := time.Now()
	sess := scoring.NewSession(now)
	words := syncWordCount(sess, 0, "hello world ", now)
	scoreBefore := sess.RawScore()

	words = syncWordCount(sess, words, "hello ", now)
	if words != 2 {
		t.Fatalf("expected the high-water mark to hold at 2 after deletion, got %d", words)
	}
	if sess.RawScore() != scoreBefore {
		t.Fatalf("expected score to stay at %d after deletion, got %d", scoreBefore, sess.RawScore())
	}
}

func TestSyncWordCountDoesNotRescoreRetypedWords(t *testing.T) {
	now := time.Now()
	sess := scoring.NewSession(now)

	mark := syncWordCount(sess, 0, "one two three four ", now)
	scoreAfterTyping := sess.RawScore()

	// Delete the last two words, then retype them: the same words must not
	// be scored a second time.
	mark = syncWordCount(sess, mark, "one two ", now)
	mark = syncWordCount(sess, mark, "one two three four ", now)
	if got := sess.RawScore(); got != scoreAfterTyping {
		t.Fatalf("expected retyping deleted words to award nothing, score went %d -> %d", scoreAfterTyping, got)
	}

	// Writing genuinely new text still scores.
	mark = syncWordCount(sess, mark, "one two three four five ", now)
	if mark != 5 {
		t.Fatalf("expected the mark to advance to 5, got %d", mark)
	}
	if sess.RawScore() <= scoreAfterTyping {
		t.Fatal("expected new writing past the high-water mark to score")
	}
}

func TestSyncWordCountClampsBulkInsertion(t *testing.T) {
	now := time.Now()
	sess := scoring.NewSession(now)

	bulk := strings.Repeat("word ", 500)
	mark := syncWordCount(sess, 0, bulk, now)
	if mark != 500 {
		t.Fatalf("expected the mark to acknowledge all 500 words, got %d", mark)
	}
	if got := sess.TotalWords(); got != maxWordsPerUpdate {
		t.Fatalf("expected at most %d words scored from a bulk insert, got %d", maxWordsPerUpdate, got)
	}

	// The unscored remainder must not trickle in on the next keystroke.
	syncWordCount(sess, mark, bulk+"more ", now)
	if got := sess.TotalWords(); got != maxWordsPerUpdate+1 {
		t.Fatalf("expected only the one newly typed word to be added, got %d", got)
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

func TestViewWritingShowsWPMOnlyWithoutBaseline(t *testing.T) {
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

	m.writing.liveWPM = 42
	got := m.viewWriting()
	if !strings.Contains(got, "42 WPM") {
		t.Fatalf("expected WPM-only reading, got %q", got)
	}
	if strings.Contains(got, "WPM ·") {
		t.Fatalf("expected no ratio without a baseline, got %q", got)
	}
}

func TestViewWritingShowsRatioWithBaseline(t *testing.T) {
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
	m.writing.liveWPM = 42
	m.writing.intensityRatio = 1.4

	got := m.viewWriting()
	if !strings.Contains(got, "42 WPM · 1.4x") {
		t.Fatalf("expected WPM and ratio, got %q", got)
	}
	if tierIdx, paceIdx := strings.Index(got, "Focused"), strings.Index(got, "42 WPM"); tierIdx == -1 || tierIdx > paceIdx {
		t.Fatalf("expected the tier tag to precede the pace reading, got %q", got)
	}
}

func TestViewWritingShowsRatioWithoutTierAtNormalPace(t *testing.T) {
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
	m.writing.liveWPM = 28
	m.writing.intensityRatio = 0.9

	got := m.viewWriting()
	if !strings.Contains(got, "28 WPM · 0.9x") {
		t.Fatalf("expected WPM and ratio at normal pace, got %q", got)
	}
	if strings.Contains(got, "Focused") || strings.Contains(got, "Intense") || strings.Contains(got, "Frantic") {
		t.Fatalf("expected no tier tag at normal pace, got %q", got)
	}
}

func TestViewWritingShowsZeroWPMAtSessionStart(t *testing.T) {
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
	if !strings.Contains(got, "0 WPM") {
		t.Fatalf("expected 0 WPM before the first word, got %q", got)
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
	m.writing.textarea.SetValue("hello world ")
	m.writing.lastWordCount = syncWordCount(m.writing.session, m.writing.lastWordCount, m.writing.textarea.Value(), now)

	// A tick while the writer is still active is what samples the pace; the
	// recorded value is the median of those samples. 2 words share one
	// instant, so the 5s floor gives 2 / (5s in minutes) = 24 WPM.
	updated, _ = m.updateWriting(comboTickMsg(now))
	m = updated.(Model)

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
	if results[0].AvgPaceWPM != 24 {
		t.Fatalf("expected the recorded pace to be the median active reading (24), got %v", results[0].AvgPaceWPM)
	}
}

// A session's recorded pace must not be dragged down by idle stretches: only
// readings taken while actually typing are sampled. Before this, pace was
// total words over wall-clock duration, so a long pause made the session
// look glacially slow and every later comparison against it looked frantic.
func TestEndWritingSessionIgnoresIdleTicksWhenRecordingPace(t *testing.T) {
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

	now := time.Now()
	m.writing.textarea.SetValue("hello world ")
	m.writing.lastWordCount = syncWordCount(m.writing.session, m.writing.lastWordCount, m.writing.textarea.Value(), now)

	// One tick while typing, then a long silence's worth of idle ticks.
	updated, _ = m.updateWriting(comboTickMsg(now))
	m = updated.(Model)
	for i := 1; i <= 20; i++ {
		updated, _ = m.updateWriting(comboTickMsg(now.Add(time.Duration(i) * time.Minute)))
		m = updated.(Model)
	}

	updated, _ = m.endWritingSession()
	m = updated.(Model)

	results, _, err := s.SearchSessions("", 10, 0)
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if results[0].AvgPaceWPM != 24 {
		t.Fatalf("expected idle ticks to be excluded, leaving the typing pace of 24, got %v", results[0].AvgPaceWPM)
	}
}

func TestEndWritingSessionWithoutPaceSamplesRecordsNothing(t *testing.T) {
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

	// Words typed, but the session ended before any tick sampled the pace.
	m.writing.textarea.SetValue("hello world ")
	m.writing.lastWordCount = syncWordCount(m.writing.session, m.writing.lastWordCount, m.writing.textarea.Value(), time.Now())

	updated, _ = m.endWritingSession()
	m = updated.(Model)

	results, _, err := s.SearchSessions("", 10, 0)
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if results[0].AvgPaceWPM != 0 {
		t.Fatalf("expected no pace recorded without samples, got %v", results[0].AvgPaceWPM)
	}
	// Left NULL, so it must not count toward a future baseline.
	if _, ok, err := s.RecentAvgPace(10); err != nil || ok {
		t.Fatalf("expected an unmeasured session to be excluded from the baseline (ok=%v, err=%v)", ok, err)
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
	m.writing.textarea.SetValue("hello world this is a test ")
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
	path := filepath.Join(dir, "journal.db")
	s, err := store.Open(path)
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
	if got := countSessionRows(t, path); got != 1 {
		t.Fatalf("expected the started session to exist, got %d rows", got)
	}

	// No typing happens: end the session immediately.
	updated, _ = m.endWritingSession()
	m = updated.(Model)

	if m.screen != screenHome {
		t.Fatalf("expected screenHome for a zero-word session, got %v", m.screen)
	}
	if got := countSessionRows(t, path); got != 0 {
		t.Fatalf("expected the abandoned session row to be discarded, got %d rows", got)
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

// A single unterminated word scores nothing, but it's still writing — the
// discard path must not take it down with the empty session row.
func TestEndWritingSessionKeepsTextWithNoCompletedWords(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.db")
	s, err := store.Open(path)
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

	sessionID := m.writing.sessionID
	m.writing.textarea.SetValue("hello")
	m.writing.lastWordCount = syncWordCount(m.writing.session, m.writing.lastWordCount, m.writing.textarea.Value(), time.Now())

	updated, _ = m.endWritingSession()
	m = updated.(Model)

	if got := countSessionRows(t, path); got != 1 {
		t.Fatalf("expected the session to survive, got %d rows", got)
	}
	entries, err := s.GetEntries(sessionID)
	if err != nil {
		t.Fatalf("GetEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].Body != "hello" {
		t.Fatalf("expected the written text to be saved, got %+v", entries)
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

// bubbles/textarea binds ctrl+v to a clipboard read of its own, which comes
// back as an internal message rather than a key event — so it never reaches
// the tea.KeyMsg.Paste guard. Left enabled, it inserts the whole clipboard
// and scores every word of it.
func TestTextareaClipboardPasteBindingIsDisabled(t *testing.T) {
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

	if m.writing.textarea.KeyMap.Paste.Enabled() {
		t.Fatal("expected the textarea's clipboard paste binding to be disabled")
	}
}

func TestCtrlVWarnsInsteadOfPasting(t *testing.T) {
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

	updated, cmd := m.updateWriting(tea.KeyMsg{Type: tea.KeyCtrlV})
	m = updated.(Model)

	if cmd != nil {
		t.Fatal("expected ctrl+v to issue no command — a clipboard read would bypass the guard")
	}
	if m.writing.pasteWarning != pasteWarningText {
		t.Fatalf("expected the paste warning, got %q", m.writing.pasteWarning)
	}
	if m.writing.textarea.Value() != "" {
		t.Fatalf("expected nothing inserted, got %q", m.writing.textarea.Value())
	}
}

func TestViewWritingHeaderIsTruncatedToTerminalWidth(t *testing.T) {
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
	m.width, m.height = 40, 20

	updated, _ := m.startWritingSession()
	m = updated.(Model)
	m.writing.hasBaseline = true
	m.writing.liveWPM = 240
	m.writing.intensityRatio = 6.2

	header := strings.SplitN(m.viewWriting(), "\n", 2)[0]
	if got := lipgloss.Width(header); got > m.width {
		t.Fatalf("expected the header to fit %d columns, got %d: %q", m.width, got, header)
	}
}
