package tui

import (
	"testing"
	"time"
)

func TestFormatSessionDateRendersHumanReadableInLocalTime(t *testing.T) {
	const raw = "2026-07-15T10:00:00Z"
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := parsed.Local().Format("Jan 2, 2006 · 3:04 PM")
	if got := formatSessionDate(raw); got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

// Timestamps written before the switch to UTC storage carry a local offset.
// The same instant must render identically however it was stored, or history
// would appear to shift when old and new rows sit side by side.
func TestFormatSessionDateRendersEqualInstantsIdentically(t *testing.T) {
	utc := formatSessionDate("2026-07-30T08:20:04Z")
	offset := formatSessionDate("2026-07-30T13:50:04+05:30")
	if utc != offset {
		t.Fatalf("expected the same instant to render identically, got %q and %q", utc, offset)
	}
}

func TestTruncateToWidthLeavesShortStringsAlone(t *testing.T) {
	if got := truncateToWidth("Score: 10", 40); got != "Score: 10" {
		t.Fatalf("expected the string unchanged, got %q", got)
	}
}

func TestTruncateToWidthUnknownTerminalSizeLeavesStringAlone(t *testing.T) {
	long := "Score: 1,240   Words: 210   Frantic   240 WPM · 6.2x"
	if got := truncateToWidth(long, 0); got != long {
		t.Fatalf("expected no truncation at unknown width, got %q", got)
	}
}

func TestTruncateToWidthCutsToExactlyWidth(t *testing.T) {
	got := truncateToWidth("abcdefghij", 5)
	if want := "abcd…"; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if n := len([]rune(got)); n != 5 {
		t.Fatalf("expected exactly 5 columns, got %d", n)
	}
}

func TestTruncateToWidthCountsRunesNotBytes(t *testing.T) {
	// The combo bar and separator are multi-byte; truncating by byte would
	// cut mid-rune and produce garbage.
	got := truncateToWidth("████████ · Frantic", 10)
	if n := len([]rune(got)); n != 10 {
		t.Fatalf("expected exactly 10 runes, got %d in %q", n, got)
	}
}

func TestFormatSessionDateFallsBackOnParseError(t *testing.T) {
	got := formatSessionDate("not-a-date")
	if got != "not-a-date" {
		t.Fatalf("expected raw string fallback, got %q", got)
	}
}

func TestFormatNumberInsertsThousandsSeparators(t *testing.T) {
	cases := map[int]string{
		0:       "0",
		42:      "42",
		999:     "999",
		1000:    "1,000",
		12345:   "12,345",
		1234567: "1,234,567",
		-1234:   "-1,234",
	}
	for n, want := range cases {
		if got := formatNumber(n); got != want {
			t.Fatalf("formatNumber(%d): expected %q, got %q", n, want, got)
		}
	}
}

func TestFormatCountPluralizes(t *testing.T) {
	if got := formatCount(1, "word", "words"); got != "1 word" {
		t.Fatalf("expected singular, got %q", got)
	}
	if got := formatCount(0, "word", "words"); got != "0 words" {
		t.Fatalf("expected plural for zero, got %q", got)
	}
	if got := formatCount(1200, "word", "words"); got != "1,200 words" {
		t.Fatalf("expected comma-separated plural, got %q", got)
	}
}

func TestFormatIntensityTagEmptyAtNormalPace(t *testing.T) {
	if got := formatIntensityTag(0); got != "" {
		t.Fatalf("expected empty tag at normal pace, got %q", got)
	}
}

func TestFormatIntensityTagShowsTier(t *testing.T) {
	if got := formatIntensityTag(2.1); got != "   · Intense" {
		t.Fatalf("expected intense tag, got %q", got)
	}
}

func TestFormatPaceInfoWithoutBaselineShowsWPMOnly(t *testing.T) {
	if got := formatPaceInfo(42, 0, false); got != "42 WPM" {
		t.Fatalf("expected WPM-only reading, got %q", got)
	}
}

func TestFormatPaceInfoWithBaselineShowsRatio(t *testing.T) {
	if got := formatPaceInfo(42, 1.4, true); got != "42 WPM · 1.4x" {
		t.Fatalf("expected WPM and ratio, got %q", got)
	}
}

func TestFormatPaceInfoRoundsWPMToWholeNumber(t *testing.T) {
	if got := formatPaceInfo(41.6, 0, false); got != "42 WPM" {
		t.Fatalf("expected rounded WPM, got %q", got)
	}
}
