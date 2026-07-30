package scoring

import (
	"sort"
	"time"
)

const (
	// PaceWindow is how far back CompleteWord/WPM look when computing a
	// live words-per-minute reading.
	PaceWindow = 60 * time.Second

	// PaceMinElapsed floors the denominator in WPM so the first couple of
	// words in a session (or right after a long pause) don't produce a
	// spurious spike — e.g. 2 words 1 real second apart would otherwise
	// imply 120 WPM.
	PaceMinElapsed = 5 * time.Second

	// PaceActiveGap is how recently a word must have been completed for the
	// writer to count as actively typing.
	PaceActiveGap = 5 * time.Second
)

// PaceTracker records the timestamps of recently completed words within a
// sliding window, independent of ComboState, to measure actual typing
// speed rather than the game-feel combo multiplier.
type PaceTracker struct {
	events []time.Time
}

// CompleteWord records a word completed at now, then drops events older
// than PaceWindow.
func (p *PaceTracker) CompleteWord(now time.Time) {
	p.events = append(p.events, now)
	cutoff := now.Add(-PaceWindow)
	i := 0
	for i < len(p.events) && p.events[i].Before(cutoff) {
		i++
	}
	p.events = p.events[i:]
}

// WPM returns the current words-per-minute reading: the count of events
// still in the window, divided by the elapsed time since the oldest of
// them, floored at PaceMinElapsed. Returns 0 with no recorded events.
func (p *PaceTracker) WPM(now time.Time) float64 {
	if len(p.events) == 0 {
		return 0
	}
	elapsed := now.Sub(p.events[0])
	if elapsed < PaceMinElapsed {
		elapsed = PaceMinElapsed
	}
	return float64(len(p.events)) / elapsed.Minutes()
}

// Active reports whether a word was completed within PaceActiveGap of now —
// i.e. whether the writer is typing right now rather than sitting idle.
func (p *PaceTracker) Active(now time.Time) bool {
	if len(p.events) == 0 {
		return false
	}
	return now.Sub(p.events[len(p.events)-1]) <= PaceActiveGap
}

// PaceSampler collects live WPM readings taken while the writer is actively
// typing. Its median is the session's representative pace, and unlike a
// wall-clock average (total words ÷ session duration) it isn't dragged
// toward zero by thinking time — which matters because the same readings
// are what the intensity ratio divides by. Comparing a burst-measured live
// reading against a pause-diluted baseline inflates every ratio.
type PaceSampler struct {
	samples []float64
}

// Sample records one WPM reading. Non-positive readings are ignored: they
// carry no pace information and would only drag the median down.
func (s *PaceSampler) Sample(wpm float64) {
	if wpm <= 0 {
		return
	}
	s.samples = append(s.samples, wpm)
}

// Median returns the middle reading (the mean of the middle two when the
// count is even). ok is false when nothing was sampled — a session too
// short to have a measurable pace, which callers should record as "no data"
// rather than as a pace of zero.
func (s *PaceSampler) Median() (wpm float64, ok bool) {
	if len(s.samples) == 0 {
		return 0, false
	}
	sorted := append([]float64(nil), s.samples...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid], true
	}
	return (sorted[mid-1] + sorted[mid]) / 2, true
}

const (
	IntensityFocusedRatio = 1.3
	IntensityIntenseRatio = 1.8
	IntensityFranticRatio = 2.5
)

// IntensityTier labels a pace ratio (live WPM / personal baseline WPM)
// against fixed thresholds. An empty string means pace isn't notably
// elevated.
func IntensityTier(ratio float64) string {
	switch {
	case ratio >= IntensityFranticRatio:
		return "Frantic"
	case ratio >= IntensityIntenseRatio:
		return "Intense"
	case ratio >= IntensityFocusedRatio:
		return "Focused"
	default:
		return ""
	}
}
