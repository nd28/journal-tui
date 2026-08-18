package scoring

import (
	"testing"
	"time"
)

func TestSessionAggregatesAcrossEntries(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := NewSession(t0)

	now := t0
	for i := 0; i < 2; i++ {
		now = now.Add(300 * time.Millisecond)
		s.CompleteWord(now)
	}
	firstEntryScore := s.RawScore()

	s.NewEntry()
	if len(s.Entries) != 1 {
		t.Fatalf("expected 1 finalized entry, got %d", len(s.Entries))
	}
	if s.Entries[0].Words != 2 {
		t.Fatalf("expected 2 words in the first entry, got %d", s.Entries[0].Words)
	}

	now = now.Add(300 * time.Millisecond)
	s.CompleteWord(now)

	if s.TotalWords() != 3 {
		t.Fatalf("expected 3 total words, got %d", s.TotalWords())
	}
	if s.RawScore() <= firstEntryScore {
		t.Fatalf("expected raw score to grow after a 3rd word, got %d (was %d)", s.RawScore(), firstEntryScore)
	}
}

func TestNewEntryIgnoresEmptyTrailingEntry(t *testing.T) {
	s := NewSession(time.Now())
	s.NewEntry() // nothing typed yet
	if len(s.Entries) != 0 {
		t.Fatalf("expected no entries for an empty session, got %d", len(s.Entries))
	}
}

func TestStreakBonus(t *testing.T) {
	cases := []struct {
		days int
		want float64
	}{
		{0, 1.0},
		{1, 1.05},
		{3, 1.15},
		{10, 1.5},
		{20, 1.5},
	}
	for _, c := range cases {
		got := StreakBonus(c.days)
		if round2(got) != c.want {
			t.Fatalf("StreakBonus(%d) = %v, want %v", c.days, got, c.want)
		}
	}
}

func TestFinalScoreAppliesStreakBonus(t *testing.T) {
	got := FinalScore(100, 3)
	if got != 115 {
		t.Fatalf("FinalScore(100, 3) = %d, want 115", got)
	}
}

func TestCompleteWordFeedsPaceTracker(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := NewSession(t0)
	s.CompleteWord(t0)
	s.CompleteWord(t0.Add(1 * time.Second))

	// Same floored-window math as TestPaceTrackerWPMFloorsElapsedForFewEarlyWords.
	if got := s.Pace.WPM(t0.Add(1 * time.Second)); got != 24 {
		t.Fatalf("expected Session.CompleteWord to feed Pace, got %v WPM", got)
	}
}

func TestRestoreSessionSeedsPriorWorkWithoutTheCombo(t *testing.T) {
	now := time.Now()
	s := RestoreSession(now, 120, 4200)

	if got := s.TotalWords(); got != 120 {
		t.Fatalf("expected 120 restored words, got %d", got)
	}
	if got := s.RawScore(); got != 4200 {
		t.Fatalf("expected 4200 restored points, got %d", got)
	}
	if s.Combo.Multiplier != ComboFloor {
		t.Fatalf("expected the combo to restart at the floor, got %v", s.Combo.Multiplier)
	}
}

func TestRestoreSessionAddsNewWritingOnTopOfPriorWork(t *testing.T) {
	now := time.Now()
	s := RestoreSession(now, 10, 100)
	s.CompleteWord(now)

	if got := s.TotalWords(); got != 11 {
		t.Fatalf("expected the new word to add to the restored count, got %d", got)
	}
	if got := s.RawScore(); got <= 100 {
		t.Fatalf("expected the new word to add points on top of %d, got %d", 100, got)
	}
}

func TestRestoreSessionWithNothingPriorBehavesLikeANewSession(t *testing.T) {
	now := time.Now()
	s := RestoreSession(now, 0, 0)

	if s.TotalWords() != 0 || s.RawScore() != 0 {
		t.Fatalf("expected an empty restored session, got words=%d score=%d", s.TotalWords(), s.RawScore())
	}
	if len(s.Entries) != 0 {
		t.Fatalf("expected no placeholder entry when there is nothing to restore, got %d", len(s.Entries))
	}
}
