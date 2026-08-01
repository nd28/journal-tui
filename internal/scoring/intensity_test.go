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
		// The empty results are the contract the retrospective tag on the
		// Summary, History, and Read screens depends on: a session with no
		// notably elevated pace — including one recorded before a baseline
		// existed, which stores a peak of 0 — must stay untagged. LiveTier
		// is the one that always returns a word.
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

func TestLiveTierRatioBands(t *testing.T) {
	cases := []struct {
		ratio float64
		want  string
	}{
		{0, "Warming"},
		{0.69, "Warming"},
		{0.7, "Cruising"},
		{1.2, "Cruising"},
		{1.3, "Focused"},
		{1.79, "Focused"},
		{1.8, "Intense"},
		{2.5, "Frantic"},
	}
	for _, c := range cases {
		// The WPM argument is ignored with a baseline present, so it's set to
		// a value whose absolute band would disagree with the expected word.
		if got := LiveTier(100, c.ratio, true); got != c.want {
			t.Fatalf("LiveTier(100, %v, true) = %q, want %q", c.ratio, got, c.want)
		}
	}
}

func TestLiveTierAbsoluteBandsWithoutBaseline(t *testing.T) {
	cases := []struct {
		wpm  float64
		want string
	}{
		{0, "Warming"},
		{10, "Warming"},
		{20, "Cruising"},
		{39, "Cruising"},
		{45, "Focused"},
		{65, "Intense"},
		{90, "Frantic"},
	}
	for _, c := range cases {
		// The ratio argument is meaningless without a baseline, so it's set
		// high enough to read as Frantic if it were wrongly consulted.
		if got := LiveTier(c.wpm, 9, false); got != c.want {
			t.Fatalf("LiveTier(%v, 9, false) = %q, want %q", c.wpm, got, c.want)
		}
	}
}

func TestLiveTierNeverEmpty(t *testing.T) {
	// At session start nothing has been typed, so both inputs are zero. The
	// header still has to carry a word, or the reading it replaced would
	// blink in and out.
	if got := LiveTier(0, 0, true); got != "Warming" {
		t.Fatalf("LiveTier(0, 0, true) = %q, want %q", got, "Warming")
	}
	if got := LiveTier(0, 0, false); got != "Warming" {
		t.Fatalf("LiveTier(0, 0, false) = %q, want %q", got, "Warming")
	}
}

func TestPaceTrackerNotActiveWithNoEvents(t *testing.T) {
	var p PaceTracker
	if p.Active(time.Now()) {
		t.Fatal("expected an empty tracker to be inactive")
	}
}

func TestPaceTrackerActiveWithinGapAndIdleBeyondIt(t *testing.T) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	var p PaceTracker
	p.CompleteWord(base)

	if !p.Active(base.Add(PaceActiveGap)) {
		t.Fatal("expected active exactly at the gap boundary")
	}
	if p.Active(base.Add(PaceActiveGap + time.Second)) {
		t.Fatal("expected inactive once the gap is exceeded")
	}
}

func TestPaceSamplerMedianEmptyReportsNoData(t *testing.T) {
	var s PaceSampler
	if _, ok := s.Median(); ok {
		t.Fatal("expected ok=false with no samples")
	}
}

func TestPaceSamplerIgnoresNonPositiveReadings(t *testing.T) {
	var s PaceSampler
	s.Sample(0)
	s.Sample(-5)
	if _, ok := s.Median(); ok {
		t.Fatal("expected non-positive readings to be ignored entirely")
	}
}

func TestPaceSamplerMedianOddCount(t *testing.T) {
	var s PaceSampler
	for _, v := range []float64{50, 10, 30} {
		s.Sample(v)
	}
	got, ok := s.Median()
	if !ok || got != 30 {
		t.Fatalf("expected median 30, got %v (ok=%v)", got, ok)
	}
}

func TestPaceSamplerMedianEvenCount(t *testing.T) {
	var s PaceSampler
	for _, v := range []float64{40, 10, 30, 20} {
		s.Sample(v)
	}
	got, ok := s.Median()
	if !ok || got != 25 {
		t.Fatalf("expected median 25, got %v (ok=%v)", got, ok)
	}
}

// A handful of fast readings must not be dragged to zero the way a
// wall-clock average is — this is the whole reason the baseline switched
// from "total words ÷ session duration" to a median of active readings.
func TestPaceSamplerMedianResistsIdlePeriods(t *testing.T) {
	var s PaceSampler
	for i := 0; i < 5; i++ {
		s.Sample(60)
	}
	// Two slow readings from the tail of a burst shouldn't move it much.
	s.Sample(12)
	s.Sample(15)

	got, _ := s.Median()
	if got != 60 {
		t.Fatalf("expected the median to stay at the sustained pace of 60, got %v", got)
	}
}
