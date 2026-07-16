package scoring

import "time"

const (
	// PaceWindow is how far back CompleteWord/WPM look when computing a
	// live words-per-minute reading.
	PaceWindow = 60 * time.Second

	// PaceMinElapsed floors the denominator in WPM so the first couple of
	// words in a session (or right after a long pause) don't produce a
	// spurious spike — e.g. 2 words 1 real second apart would otherwise
	// imply 120 WPM.
	PaceMinElapsed = 5 * time.Second
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
