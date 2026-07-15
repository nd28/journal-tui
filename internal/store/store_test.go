package store

import (
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
