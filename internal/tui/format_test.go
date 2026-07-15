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
