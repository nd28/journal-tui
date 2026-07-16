package tui

import "testing"

func TestFormatSessionDateRendersHumanReadable(t *testing.T) {
	got := formatSessionDate("2026-07-15T10:00:00Z")
	want := "Jul 15, 2026 · 10:00 AM"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
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
