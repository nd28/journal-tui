package tui

import (
	"strings"
	"testing"
)

func TestViewSummaryShowsPeakTierWhenElevated(t *testing.T) {
	m := Model{summary: summaryState{
		totalWords: 10, finalScore: 100,
		sessionPaceWPM: 46, hasSessionPace: true, hasBaseline: true,
		peakIntensityRatio: 2.1,
	}}
	got := m.viewSummary()
	if !strings.Contains(got, "Session pace:   46 WPM   ·   peak 2.1x your average (Intense)") {
		t.Fatalf("expected pace line with the Intense tier, got %q", got)
	}
}

func TestViewSummaryShowsPaceWithoutTierAtNormalPace(t *testing.T) {
	// 1.24x is the case that motivated this line: a real session peaked just
	// under the 1.3x tier threshold and so reported nothing about its pace.
	m := Model{summary: summaryState{
		totalWords: 10, finalScore: 100,
		sessionPaceWPM: 46, hasSessionPace: true, hasBaseline: true,
		peakIntensityRatio: 1.24,
	}}
	got := m.viewSummary()
	if !strings.Contains(got, "Session pace:   46 WPM   ·   peak 1.2x your average") {
		t.Fatalf("expected pace line at normal pace, got %q", got)
	}
	if strings.Contains(got, "Focused") || strings.Contains(got, "Intense") || strings.Contains(got, "Frantic") {
		t.Fatalf("expected no tier word below the threshold, got %q", got)
	}
}

func TestViewSummaryShowsPaceWithoutPeakWhenNoBaseline(t *testing.T) {
	m := Model{summary: summaryState{
		totalWords: 10, finalScore: 100,
		sessionPaceWPM: 46, hasSessionPace: true, hasBaseline: false,
		peakIntensityRatio: 0,
	}}
	got := m.viewSummary()
	if !strings.Contains(got, "Session pace:   46 WPM") {
		t.Fatalf("expected the WPM reading without a baseline, got %q", got)
	}
	if strings.Contains(got, "peak") {
		t.Fatalf("expected no peak ratio without a baseline, got %q", got)
	}
}

func TestViewSummaryOmitsPaceWhenSessionTooShortToMeasure(t *testing.T) {
	m := Model{summary: summaryState{totalWords: 10, finalScore: 100, hasSessionPace: false}}
	got := m.viewSummary()
	if strings.Contains(got, "Session pace:") {
		t.Fatalf("expected no pace line when nothing was sampled, got %q", got)
	}
}
