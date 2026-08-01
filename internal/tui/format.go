package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nd28/journal-tui/internal/scoring"
)

// formatSessionDate renders a stored RFC3339 timestamp as a human-readable
// date and time (e.g. "Jul 15, 2026 · 10:00 AM") in the reader's local zone.
// Timestamps are stored in UTC, so the conversion is what makes a session
// show the wall-clock time it was actually written at. Falls back to the raw
// string if it doesn't parse, so a malformed timestamp degrades gracefully
// instead of vanishing.
func formatSessionDate(raw string) string {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	return t.Local().Format("Jan 2, 2006 · 3:04 PM")
}

// truncateToWidth shortens s to at most width columns, marking the cut with
// an ellipsis. A width of zero or less means the terminal size isn't known
// yet, in which case s is returned untouched.
func truncateToWidth(s string, width int) string {
	if width <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return string(r[:width-1]) + "…"
}

// formatNumber inserts thousands separators (1234 -> "1,234") so large
// scores stay readable at a glance.
func formatNumber(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		s = "-" + s
	}
	return s
}

// pluralize picks singular or plural based on n (1 is singular, everything
// else — including 0 — is plural).
func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

// formatCount renders a count with the correct singular/plural noun and
// thousands separators (e.g. 1 -> "1 word", 1200 -> "1,200 words").
func formatCount(n int, singular, plural string) string {
	return formatNumber(n) + " " + pluralize(n, singular, plural)
}

// formatIntensityTag renders a trailing tag for a session's peak pace
// tier, e.g. "   · Intense" — or "" when the tier is empty (pace was never
// notably elevated, or no personal baseline existed yet for that session).
func formatIntensityTag(peakRatio float64) string {
	tier := scoring.IntensityTier(peakRatio)
	if tier == "" {
		return ""
	}
	return "   · " + tier
}

// formatPaceInfo renders the live pace readout for the writing header: just
// the WPM reading with no baseline yet, or WPM plus the ratio against
// personal baseline once one exists.
func formatPaceInfo(wpm, ratio float64, hasBaseline bool) string {
	if !hasBaseline {
		return fmt.Sprintf("%.0f WPM", wpm)
	}
	return fmt.Sprintf("%.0f WPM · %.1fx", wpm, ratio)
}

// formatSessionPace renders the Summary screen's pace line: the session's
// representative WPM — the same median that becomes a future baseline — plus
// the peak ratio once a baseline exists to measure against, and the tier word
// when that peak was genuinely elevated.
//
// The WPM shows unconditionally, unlike the tier word, because it is the one
// figure that means something on its own: without it a session that peaked
// just below the tier threshold reported nothing at all about how fast it was
// written.
func formatSessionPace(wpm, peakRatio float64, hasBaseline bool) string {
	line := fmt.Sprintf("Session pace:   %.0f WPM", wpm)
	if !hasBaseline {
		return line
	}
	line += fmt.Sprintf("   ·   peak %.1fx your average", peakRatio)
	if tier := scoring.IntensityTier(peakRatio); tier != "" {
		line += " (" + tier + ")"
	}
	return line
}
