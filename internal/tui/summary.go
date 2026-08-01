package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type summaryState struct {
	rawScore           int
	finalScore         int
	bonus              float64
	totalWords         int
	isNewHigh          bool
	peakIntensityRatio float64

	// sessionPaceWPM is the median of the readings taken while actually
	// typing — the same figure recorded as this session's pace and folded
	// into future baselines. hasSessionPace is false when the session was too
	// short to sample any reading at all, in which case there is no pace to
	// report rather than a pace of zero.
	sessionPaceWPM float64
	hasSessionPace bool
	hasBaseline    bool
}

func (m Model) updateSummary(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "enter", "esc":
		m.screen = screenHome
		m.homeCursor = 0
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) viewSummary() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Session complete") + "\n\n")
	if m.summary.isNewHigh {
		b.WriteString(selectedStyle.Render("*** NEW HIGH SCORE ***") + "\n\n")
	}
	b.WriteString(statStyle.Render(fmt.Sprintf("Words typed:    %s", formatNumber(m.summary.totalWords))) + "\n")
	b.WriteString(statStyle.Render(fmt.Sprintf("Raw score:      %s", formatNumber(m.summary.rawScore))) + "\n")
	b.WriteString(statStyle.Render(fmt.Sprintf("Streak bonus:   +%.0f%%", (m.summary.bonus-1)*100)) + "\n")
	b.WriteString(statStyle.Render(fmt.Sprintf("Session score:  %s", formatNumber(m.summary.finalScore))) + "\n")
	b.WriteString(statStyle.Render(fmt.Sprintf("Lifetime score: %s", formatNumber(m.stats.LifetimeScore))) + "\n")
	if m.summary.hasSessionPace {
		b.WriteString(statStyle.Render(formatSessionPace(m.summary.sessionPaceWPM, m.summary.peakIntensityRatio, m.summary.hasBaseline)) + "\n")
	}
	b.WriteString("\n" + statStyle.Render("enter: back to home") + "\n")
	return b.String()
}
