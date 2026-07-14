package store

import (
	"path/filepath"
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

func TestListSessionsOrdersMostRecentFirst(t *testing.T) {
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
	if _, _, err := s.FinishSession(id1, base, 20, 1.0, 1, today); err != nil {
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
	if _, _, err := s.FinishSession(id2, later, 30, 1.0, 1, today); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}

	records, err := s.ListSessions(10)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(records))
	}
	if records[0].ID != id2 {
		t.Fatalf("expected the most recent session (%d) first, got %d", id2, records[0].ID)
	}
	if records[0].WordCount != 3 {
		t.Fatalf("expected 3 words for the latest session, got %d", records[0].WordCount)
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
