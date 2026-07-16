package scoring

import (
	"testing"
	"time"
)

func TestPaceTrackerWPMZeroWithNoEvents(t *testing.T) {
	var p PaceTracker
	if got := p.WPM(time.Now()); got != 0 {
		t.Fatalf("expected 0 WPM with no events, got %v", got)
	}
}

func TestPaceTrackerWPMFloorsElapsedForFewEarlyWords(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var p PaceTracker
	p.CompleteWord(t0)
	p.CompleteWord(t0.Add(1 * time.Second))

	// Only 1s has actually elapsed, but PaceMinElapsed floors the
	// denominator to 5s: 2 words / (5s in minutes) = 24 WPM.
	if got := p.WPM(t0.Add(1 * time.Second)); got != 24 {
		t.Fatalf("expected 24 WPM (floored), got %v", got)
	}
}

func TestPaceTrackerWPMOverFullWindow(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var p PaceTracker
	now := t0
	for i := 0; i < 5; i++ {
		p.CompleteWord(now)
		now = now.Add(5 * time.Second)
	}

	// 5 words spread across 20s (t0 to t0+20s, all still well within the
	// 60s window); elapsed since the oldest event, measured at the last
	// word's timestamp, is 20s = 1/3 minute — above the 5s floor, so it's
	// used directly: 5 / (1/3) = 15 WPM.
	if got := p.WPM(now.Add(-5 * time.Second)); got != 15 {
		t.Fatalf("expected 15 WPM, got %v", got)
	}
}

func TestPaceTrackerDropsEventsOutsideWindow(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var p PaceTracker
	p.CompleteWord(t0)
	p.CompleteWord(t0.Add(90 * time.Second)) // 90s later: the first event is now outside the 60s window

	// Only the second event remains; WPM at that same instant has 0
	// elapsed, floored to 5s: 1 / (5/60) = 12.
	if got := p.WPM(t0.Add(90 * time.Second)); got != 12 {
		t.Fatalf("expected 12 WPM once the first event ages out of the window, got %v", got)
	}
}

func TestIntensityTierThresholds(t *testing.T) {
	cases := []struct {
		ratio float64
		want  string
	}{
		{0, ""},
		{1.0, ""},
		{1.29, ""},
		{1.3, "Focused"},
		{1.7, "Focused"},
		{1.8, "Intense"},
		{2.4, "Intense"},
		{2.5, "Frantic"},
		{10, "Frantic"},
	}
	for _, c := range cases {
		if got := IntensityTier(c.ratio); got != c.want {
			t.Fatalf("IntensityTier(%v) = %q, want %q", c.ratio, got, c.want)
		}
	}
}
