package scoring

import (
	"testing"
	"time"
)

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}

func TestNewComboStartsAtFloor(t *testing.T) {
	c := NewCombo(time.Now())
	if c.Multiplier != ComboFloor {
		t.Fatalf("expected multiplier %v, got %v", ComboFloor, c.Multiplier)
	}
}

func TestCompleteWordBumpsEveryThreeSteadyWords(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewCombo(t0)

	gap := 300 * time.Millisecond
	now := t0
	var lastPoints int
	for i := 0; i < 3; i++ {
		now = now.Add(gap)
		lastPoints = c.CompleteWord(now)
	}

	if lastPoints != 10 {
		t.Fatalf("expected the 3rd word to still score at 1.0x (10 points), got %d", lastPoints)
	}
	if round2(c.Multiplier) != 1.1 {
		t.Fatalf("expected multiplier 1.1 after 3 steady words, got %v", c.Multiplier)
	}

	now = now.Add(gap)
	points := c.CompleteWord(now)
	if points != 11 {
		t.Fatalf("expected the 4th word to score at 1.1x (11 points), got %d", points)
	}
}

func TestCompleteWordCapsAtFive(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewCombo(t0)

	now := t0
	for i := 0; i < 200; i++ {
		now = now.Add(100 * time.Millisecond)
		c.CompleteWord(now)
	}

	if round2(c.Multiplier) != ComboCap {
		t.Fatalf("expected multiplier capped at %v, got %v", ComboCap, c.Multiplier)
	}
}

func TestTickDecaysWithoutMutatingBaseline(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewCombo(t0)

	now := t0
	for i := 0; i < 3; i++ {
		now = now.Add(300 * time.Millisecond)
		c.CompleteWord(now)
	}
	if round2(c.Multiplier) != 1.1 {
		t.Fatalf("setup failed: expected 1.1, got %v", c.Multiplier)
	}

	// 2.5s idle past the last word = 0.5s past the 2s decay-start threshold.
	c.Tick(now.Add(2500 * time.Millisecond))
	if got := round2(c.Multiplier); got != 1.06 {
		t.Fatalf("expected 1.06 after 0.5s of decay, got %v", got)
	}

	// A second Tick at the same instant must recompute from the same baseline, not compound.
	c.Tick(now.Add(2500 * time.Millisecond))
	if got := round2(c.Multiplier); got != 1.06 {
		t.Fatalf("expected Tick to be idempotent for a repeated instant, got %v", got)
	}
}

func TestCompleteWordAfterLongPauseHardResets(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewCombo(t0)

	now := t0
	for i := 0; i < 3; i++ {
		now = now.Add(300 * time.Millisecond)
		c.CompleteWord(now)
	}

	now = now.Add(20 * time.Second)
	c.CompleteWord(now)

	if round2(c.Multiplier) != ComboFloor {
		t.Fatalf("expected hard reset to floor after 20s idle, got %v", c.Multiplier)
	}
}
