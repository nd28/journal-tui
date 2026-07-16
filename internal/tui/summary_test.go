package tui

import (
	"strings"
	"testing"
)

func TestViewSummaryShowsPeakPaceWhenElevated(t *testing.T) {
	m := Model{summary: summaryState{totalWords: 10, finalScore: 100, peakIntensityRatio: 2.1}}
	got := m.viewSummary()
	if !strings.Contains(got, "Peak pace:      Intense (2.1x your recent average)") {
		t.Fatalf("expected peak pace line, got %q", got)
	}
}

func TestViewSummaryOmitsPeakPaceAtNormalPace(t *testing.T) {
	m := Model{summary: summaryState{totalWords: 10, finalScore: 100, peakIntensityRatio: 0}}
	got := m.viewSummary()
	if strings.Contains(got, "Peak pace:") {
		t.Fatalf("expected no peak pace line at normal pace, got %q", got)
	}
}
