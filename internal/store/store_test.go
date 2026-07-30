package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenInitializesZeroStats(t *testing.T) {
	s := openTestStore(t)
	stats, err := s.GetStats()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.LifetimeScore != 0 || stats.HighSessionScore != 0 || stats.CurrentStreak != 0 || stats.LastEntryDate != "" {
		t.Fatalf("expected zero-value stats on a fresh store, got %+v", stats)
	}
}

func TestStartSessionAndSaveEntry(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()

	id, err := s.StartSession(now)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if id == 0 {
		t.Fatal("expected a non-zero session id")
	}

	if err := s.SaveEntry(id, now, "hello world", 2); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
}

func TestFinishSessionUpdatesStatsAndReportsNewHigh(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	today := now.Format("2006-01-02")

	id, err := s.StartSession(now)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	stats, isNewHigh, err := s.FinishSession(id, now, 100, 1.0, 1, today)
	if err != nil {
		t.Fatalf("FinishSession: %v", err)
	}
	if !isNewHigh {
		t.Fatal("expected the first session to be a new high score")
	}
	if stats.LifetimeScore != 100 || stats.HighSessionScore != 100 || stats.CurrentStreak != 1 || stats.LastEntryDate != today {
		t.Fatalf("unexpected stats after first session: %+v", stats)
	}

	id2, err := s.StartSession(now)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	stats, isNewHigh, err = s.FinishSession(id2, now, 50, 1.0, 1, today)
	if err != nil {
		t.Fatalf("FinishSession: %v", err)
	}
	if isNewHigh {
		t.Fatal("expected the second, lower-scoring session not to be a new high score")
	}
	if stats.LifetimeScore != 150 || stats.HighSessionScore != 100 {
		t.Fatalf("unexpected stats after second session: %+v", stats)
	}
}

func TestComputeStreakFirstEverEntry(t *testing.T) {
	if got := ComputeStreak("", "2026-07-14", 0); got != 1 {
		t.Fatalf("ComputeStreak first entry = %d, want 1", got)
	}
}

func TestComputeStreakSameDayReturnsCurrentStreak(t *testing.T) {
	if got := ComputeStreak("2026-07-14", "2026-07-14", 3); got != 3 {
		t.Fatalf("ComputeStreak same day = %d, want 3", got)
	}
}

func TestComputeStreakConsecutiveDayIncrements(t *testing.T) {
	if got := ComputeStreak("2026-07-13", "2026-07-14", 3); got != 4 {
		t.Fatalf("ComputeStreak consecutive day = %d, want 4", got)
	}
}

func TestComputeStreakGapResetsToOne(t *testing.T) {
	if got := ComputeStreak("2026-07-10", "2026-07-14", 5); got != 1 {
		t.Fatalf("ComputeStreak after gap = %d, want 1", got)
	}
}

func TestSearchSessionsEmptyQueryReturnsAllPaginated(t *testing.T) {
	s := openTestStore(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	today := base.Format("2006-01-02")

	id1, err := s.StartSession(base)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id1, base, "hello world", 2); err != nil {
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
	if err := s.SaveEntry(id2, later, "a b c", 3); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if _, _, err := s.FinishSession(id2, later, 20, 1.0, 1, today); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}

	results, total, err := s.SearchSessions("", 10, 0)
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected total 2, got %d", total)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].ID != id2 {
		t.Fatalf("expected most recent session (%d) first, got %d", id2, results[0].ID)
	}
	if results[0].WordCount != 3 {
		t.Fatalf("expected 3 words for the latest session, got %d", results[0].WordCount)
	}
	if results[0].Snippet != "" || results[1].Snippet != "" {
		t.Fatalf("expected empty snippets for an empty query, got %q and %q", results[0].Snippet, results[1].Snippet)
	}
}

func TestSearchSessionsFiltersByEntryTextAndReturnsSnippet(t *testing.T) {
	s := openTestStore(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	today := base.Format("2006-01-02")

	id1, err := s.StartSession(base)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id1, base, "the quick brown fox", 4); err != nil {
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
	if err := s.SaveEntry(id2, later, "hello world", 2); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if _, _, err := s.FinishSession(id2, later, 20, 1.0, 1, today); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}

	results, total, err := s.SearchSessions("fox", 10, 0)
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected total 1, got %d", total)
	}
	if len(results) != 1 || results[0].ID != id1 {
		t.Fatalf("expected only session %d to match, got %+v", id1, results)
	}
	if results[0].Snippet != "the quick brown fox" {
		t.Fatalf("expected snippet %q, got %q", "the quick brown fox", results[0].Snippet)
	}
}

func TestSearchSessionsIsCaseInsensitive(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	id, err := s.StartSession(now)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id, now, "The Quick Brown Fox", 4); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if _, _, err := s.FinishSession(id, now, 10, 1.0, 1, now.Format("2006-01-02")); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}

	results, total, err := s.SearchSessions("FOX", 10, 0)
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if total != 1 || len(results) != 1 {
		t.Fatalf("expected a case-insensitive match, got total=%d len=%d", total, len(results))
	}
}

func TestSearchSessionsSnippetTruncatesLongEntries(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	long := "start of entry " + strings.Repeat("padding ", 10) + "needle at the end"

	id, err := s.StartSession(now)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id, now, long, 20); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if _, _, err := s.FinishSession(id, now, 10, 1.0, 1, now.Format("2006-01-02")); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}

	results, _, err := s.SearchSessions("needle", 10, 0)
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !strings.HasSuffix(results[0].Snippet, "...") {
		t.Fatalf("expected truncated snippet to end with '...', got %q", results[0].Snippet)
	}
	if len([]rune(results[0].Snippet)) != 63 {
		t.Fatalf("expected a 60-rune snippet plus '...' (63 runes), got %d: %q", len([]rune(results[0].Snippet)), results[0].Snippet)
	}
}

func TestSearchSessionsPaginationTotalAcrossPages(t *testing.T) {
	s := openTestStore(t)
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

	page0, total0, err := s.SearchSessions("", 10, 0)
	if err != nil {
		t.Fatalf("SearchSessions page 0: %v", err)
	}
	if total0 != 15 || len(page0) != 10 {
		t.Fatalf("expected 10 of 15 on page 0, got %d of %d", len(page0), total0)
	}

	page1, total1, err := s.SearchSessions("", 10, 10)
	if err != nil {
		t.Fatalf("SearchSessions page 1: %v", err)
	}
	if total1 != 15 || len(page1) != 5 {
		t.Fatalf("expected 5 of 15 on page 1, got %d of %d", len(page1), total1)
	}
}

func TestGetEntriesReturnsEntriesInWriteOrder(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	id, err := s.StartSession(now)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id, now, "first", 1); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if err := s.SaveEntry(id, now.Add(time.Minute), "second", 1); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if err := s.SaveEntry(id, now.Add(2*time.Minute), "third", 1); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}

	entries, err := s.GetEntries(id)
	if err != nil {
		t.Fatalf("GetEntries: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Body != "first" || entries[1].Body != "second" || entries[2].Body != "third" {
		t.Fatalf("expected entries in write order, got %+v", entries)
	}
}

func TestOpenTwiceOnSameFileToleratesRepeatMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.db")

	s1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	s1.Close()

	if _, err := Open(path); err != nil {
		t.Fatalf("second Open on the same file: %v", err)
	}
}

func TestRecordSessionPacePersistsAndSearchSessionsReturnsIt(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	today := now.Format("2006-01-02")

	id, err := s.StartSession(now)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id, now, "hello world", 2); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if _, _, err := s.FinishSession(id, now, 10, 1.0, 1, today); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}
	if err := s.RecordSessionPace(id, 42.5, 2.1); err != nil {
		t.Fatalf("RecordSessionPace: %v", err)
	}

	results, _, err := s.SearchSessions("", 10, 0)
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].AvgPaceWPM != 42.5 {
		t.Fatalf("expected AvgPaceWPM 42.5, got %v", results[0].AvgPaceWPM)
	}
	if results[0].PeakIntensityRatio != 2.1 {
		t.Fatalf("expected PeakIntensityRatio 2.1, got %v", results[0].PeakIntensityRatio)
	}
}

func TestSearchSessionsWithoutRecordedPaceReturnsZero(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	id, err := s.StartSession(now)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id, now, "hello", 1); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if _, _, err := s.FinishSession(id, now, 10, 1.0, 1, now.Format("2006-01-02")); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}

	results, _, err := s.SearchSessions("", 10, 0)
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].AvgPaceWPM != 0 || results[0].PeakIntensityRatio != 0 {
		t.Fatalf("expected zero pace fields without RecordSessionPace, got %+v", results[0])
	}
}

func TestRecentAvgPaceRequiresMinimumSessions(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	today := now.Format("2006-01-02")

	for i := 0; i < 2; i++ {
		id, err := s.StartSession(now)
		if err != nil {
			t.Fatalf("StartSession: %v", err)
		}
		if _, _, err := s.FinishSession(id, now, 10, 1.0, 1, today); err != nil {
			t.Fatalf("FinishSession: %v", err)
		}
		if err := s.RecordSessionPace(id, 30, 0); err != nil {
			t.Fatalf("RecordSessionPace: %v", err)
		}
	}

	_, ok, err := s.RecentAvgPace(10)
	if err != nil {
		t.Fatalf("RecentAvgPace: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false with fewer than 3 recorded sessions")
	}
}

func TestRecentAvgPaceAveragesLastNSessions(t *testing.T) {
	s := openTestStore(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	today := base.Format("2006-01-02")

	paces := []float64{10, 20, 30, 40, 100}
	for i, p := range paces {
		startedAt := base.Add(time.Duration(i) * time.Hour)
		id, err := s.StartSession(startedAt)
		if err != nil {
			t.Fatalf("StartSession: %v", err)
		}
		if _, _, err := s.FinishSession(id, startedAt, 10, 1.0, 1, today); err != nil {
			t.Fatalf("FinishSession: %v", err)
		}
		if err := s.RecordSessionPace(id, p, 0); err != nil {
			t.Fatalf("RecordSessionPace: %v", err)
		}
	}

	avg, ok, err := s.RecentAvgPace(3)
	if err != nil {
		t.Fatalf("RecentAvgPace: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true with 5 recorded sessions")
	}
	// Last 3 by started_at desc: 100, 40, 30.
	want := (100.0 + 40.0 + 30.0) / 3.0
	if avg != want {
		t.Fatalf("expected avg %v, got %v", want, avg)
	}
}

func TestRecentAvgPaceExcludesSessionsWithoutRecordedPace(t *testing.T) {
	s := openTestStore(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	today := base.Format("2006-01-02")

	for i, p := range []float64{10, 20, 30} {
		startedAt := base.Add(time.Duration(i) * time.Hour)
		id, err := s.StartSession(startedAt)
		if err != nil {
			t.Fatalf("StartSession: %v", err)
		}
		if _, _, err := s.FinishSession(id, startedAt, 10, 1.0, 1, today); err != nil {
			t.Fatalf("FinishSession: %v", err)
		}
		if err := s.RecordSessionPace(id, p, 0); err != nil {
			t.Fatalf("RecordSessionPace: %v", err)
		}
	}

	// A more recent session with no recorded pace (e.g. from before this
	// feature shipped) must not pull the baseline toward zero.
	later := base.Add(3 * time.Hour)
	id, err := s.StartSession(later)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if _, _, err := s.FinishSession(id, later, 10, 1.0, 1, today); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}

	avg, ok, err := s.RecentAvgPace(10)
	if err != nil {
		t.Fatalf("RecentAvgPace: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true, 3 sessions have recorded pace")
	}
	want := (10.0 + 20.0 + 30.0) / 3.0
	if avg != want {
		t.Fatalf("expected avg %v (unrecorded session excluded), got %v", want, avg)
	}
}

func TestOpenSetsOwnerOnlyFileMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != dbFileMode {
		t.Fatalf("expected journal file mode %o, got %o", dbFileMode, got)
	}
}

func TestStoredTimestampsAreUTC(t *testing.T) {
	s := openTestStore(t)
	ist := time.FixedZone("IST", 5*3600+30*60)
	local := time.Date(2026, 7, 30, 13, 50, 4, 0, ist)

	id, err := s.StartSession(local)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id, local, "hello world", 2); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if _, _, err := s.FinishSession(id, local, 10, 1.0, 1, "2026-07-30"); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}

	results, _, err := s.SearchSessions("", 10, 0)
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if want := "2026-07-30T08:20:04Z"; results[0].StartedAt != want {
		t.Fatalf("expected started_at stored as %q, got %q", want, results[0].StartedAt)
	}

	entries, err := s.GetEntries(id)
	if err != nil {
		t.Fatalf("GetEntries: %v", err)
	}
	if want := "2026-07-30T08:20:04Z"; entries[0].CreatedAt != want {
		t.Fatalf("expected created_at stored as %q, got %q", want, entries[0].CreatedAt)
	}
}

func TestDiscardSessionRemovesSessionAndEntries(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()

	id, err := s.StartSession(now)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id, now, "stray text", 2); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}

	if err := s.DiscardSession(id); err != nil {
		t.Fatalf("DiscardSession: %v", err)
	}

	entries, err := s.GetEntries(id)
	if err != nil {
		t.Fatalf("GetEntries: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected entries to be removed, got %d", len(entries))
	}

	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id = ?`, id).Scan(&count); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected the session row to be removed, got %d", count)
	}
}

func TestSearchSessionsTreatsLikeWildcardsLiterally(t *testing.T) {
	s := openTestStore(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	today := base.Format("2006-01-02")

	for i, body := range []string{"finished 100% of the plan", "nothing notable today"} {
		startedAt := base.Add(time.Duration(i) * time.Hour)
		id, err := s.StartSession(startedAt)
		if err != nil {
			t.Fatalf("StartSession: %v", err)
		}
		if err := s.SaveEntry(id, startedAt, body, 5); err != nil {
			t.Fatalf("SaveEntry: %v", err)
		}
		if _, _, err := s.FinishSession(id, startedAt, 10, 1.0, 1, today); err != nil {
			t.Fatalf("FinishSession: %v", err)
		}
	}

	// Unescaped, "100%" would end up as LIKE '%100%%' and still match only
	// the first entry — so assert on a bare "%", which unescaped matches
	// every entry.
	_, total, err := s.SearchSessions("%", 10, 0)
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected a literal %% to match only the one entry containing it, got %d", total)
	}

	_, total, err = s.SearchSessions("_", 10, 0)
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if total != 0 {
		t.Fatalf("expected a literal _ to match nothing, got %d", total)
	}
}

// makeLegacyDatabase writes a database in the shape the previous release
// left behind: user_version 0, pace values derived from wall-clock duration
// (one of them inflated by a clipboard paste), local-offset timestamps, and
// sessions abandoned without writing anything.
func makeLegacyDatabase(t *testing.T, path string) (finished, orphan, crashed int64) {
	t.Helper()
	ist := time.FixedZone("IST", 5*3600+30*60)
	local := time.Date(2026, 7, 30, 13, 50, 4, 0, ist)

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	finished, err = s.StartSession(local)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(finished, local, "a real session", 3); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if _, _, err := s.FinishSession(finished, local, 96716, 1.0, 1, "2026-07-30"); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}
	if err := s.RecordSessionPace(finished, 54.3, 662.4); err != nil {
		t.Fatalf("RecordSessionPace: %v", err)
	}

	if orphan, err = s.StartSession(local); err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	if crashed, err = s.StartSession(local); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(crashed, local, "interrupted mid-session", 3); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	s.Close()

	// Roll the file back to how the previous release left it: schema
	// unversioned, timestamps carrying the local offset.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	defer raw.Close()
	legacy := local.Format(time.RFC3339)
	for _, stmt := range []struct {
		q    string
		args []any
	}{
		{`PRAGMA user_version = 0`, nil},
		{`UPDATE sessions SET started_at = ?`, []any{legacy}},
		{`UPDATE sessions SET ended_at = ? WHERE ended_at IS NOT NULL`, []any{legacy}},
		{`UPDATE entries SET created_at = ?`, []any{legacy}},
	} {
		if _, err := raw.Exec(stmt.q, stmt.args...); err != nil {
			t.Fatalf("legacy setup %q: %v", stmt.q, err)
		}
	}
	return finished, orphan, crashed
}

func TestMigrationClearsLegacyPaceValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.db")
	makeLegacyDatabase(t, path)

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open after legacy setup: %v", err)
	}
	defer s.Close()

	results, _, err := s.SearchSessions("", 10, 0)
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 finished session, got %d", len(results))
	}
	if results[0].AvgPaceWPM != 0 || results[0].PeakIntensityRatio != 0 {
		t.Fatalf("expected legacy pace values cleared, got %+v", results[0])
	}
	if _, ok, err := s.RecentAvgPace(10); err != nil || ok {
		t.Fatalf("expected no baseline after clearing legacy pace data (ok=%v, err=%v)", ok, err)
	}
}

func TestMigrationDeletesAbandonedSessionsButKeepsWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.db")
	_, orphan, crashed := makeLegacyDatabase(t, path)

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open after legacy setup: %v", err)
	}
	defer s.Close()

	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id = ?`, orphan).Scan(&count); err != nil {
		t.Fatalf("count orphan: %v", err)
	}
	if count != 0 {
		t.Fatal("expected the abandoned, entry-less session to be deleted")
	}

	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id = ?`, crashed).Scan(&count); err != nil {
		t.Fatalf("count crashed: %v", err)
	}
	if count != 1 {
		t.Fatal("expected an unfinished session that has entries to survive")
	}
}

func TestMigrationRewritesLegacyTimestampsToUTC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.db")
	finished, _, _ := makeLegacyDatabase(t, path)

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open after legacy setup: %v", err)
	}
	defer s.Close()

	const want = "2026-07-30T08:20:04Z"
	var startedAt, endedAt string
	if err := s.db.QueryRow(`SELECT started_at, ended_at FROM sessions WHERE id = ?`, finished).Scan(&startedAt, &endedAt); err != nil {
		t.Fatalf("read session timestamps: %v", err)
	}
	if startedAt != want || endedAt != want {
		t.Fatalf("expected both session timestamps rewritten to %q, got %q and %q", want, startedAt, endedAt)
	}

	entries, err := s.GetEntries(finished)
	if err != nil {
		t.Fatalf("GetEntries: %v", err)
	}
	if entries[0].CreatedAt != want {
		t.Fatalf("expected entry timestamp rewritten to %q, got %q", want, entries[0].CreatedAt)
	}
}

func TestMigrationRunsOnlyOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.db")
	makeLegacyDatabase(t, path)

	s, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	// Pace recorded under the new definition must survive a later Open —
	// otherwise the one-time reset would wipe the baseline on every launch.
	results, _, err := s.SearchSessions("", 10, 0)
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	id := results[0].ID
	if err := s.RecordSessionPace(id, 48, 1.5); err != nil {
		t.Fatalf("RecordSessionPace: %v", err)
	}
	s.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer s2.Close()

	results, _, err = s2.SearchSessions("", 10, 0)
	if err != nil {
		t.Fatalf("SearchSessions after reopen: %v", err)
	}
	if results[0].AvgPaceWPM != 48 || results[0].PeakIntensityRatio != 1.5 {
		t.Fatalf("expected freshly recorded pace to survive reopen, got %+v", results[0])
	}
}
