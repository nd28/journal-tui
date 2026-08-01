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
//
// The empty string is load-bearing for the retrospective tag shown on the
// Summary, History, and Read screens: sessions written before a baseline
// existed store a peak ratio of 0, and those must stay untagged rather than
// carrying a word on every row. The live writing header wants the opposite —
// a word at every speed — so it uses LiveTier instead.
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

// IntensityCruisingRatio separates ordinary writing pace from a warming-up or
// winding-down one. It has no counterpart in IntensityTier, which only ever
// labels elevated pace.
const IntensityCruisingRatio = 0.7

// Absolute words-per-minute thresholds used by LiveTier before a personal
// baseline exists, so the live readout still says something in a writer's
// first few sessions.
const (
	LiveWPMCruising = 20
	LiveWPMFocused  = 40
	LiveWPMIntense  = 60
	LiveWPMFrantic  = 80
)

// LiveTier labels the current writing pace for the live header, and unlike
// IntensityTier it always returns a word — a constant, glanceable signal is
// the point, so there is no silent band. With a personal baseline it reads
// the ratio; without one (the first few sessions, before enough pace history
// exists) it falls back to absolute WPM using the same vocabulary. At session
// start, with no words yet and a WPM of 0, that means "Warming".
func LiveTier(wpm, ratio float64, hasBaseline bool) string {
	if !hasBaseline {
		switch {
		case wpm >= LiveWPMFrantic:
			return "Frantic"
		case wpm >= LiveWPMIntense:
			return "Intense"
		case wpm >= LiveWPMFocused:
			return "Focused"
		case wpm >= LiveWPMCruising:
			return "Cruising"
		default:
			return "Warming"
		}
	}

	switch {
	case ratio >= IntensityFranticRatio:
		return "Frantic"
	case ratio >= IntensityIntenseRatio:
		return "Intense"
	case ratio >= IntensityFocusedRatio:
		return "Focused"
	case ratio >= IntensityCruisingRatio:
		return "Cruising"
	default:
		return "Warming"
	}
}
