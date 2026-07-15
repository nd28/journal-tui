package tui

import (
	"strconv"
	"strings"
	"time"
)

// formatSessionDate renders a stored RFC3339 timestamp as a human-readable
// date and time (e.g. "Jul 15, 2026 · 10:00 AM"). Falls back to the raw
// string if it doesn't parse, so a malformed timestamp degrades gracefully
// instead of vanishing.
func formatSessionDate(raw string) string {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	return t.Format("Jan 2, 2006 · 3:04 PM")
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
