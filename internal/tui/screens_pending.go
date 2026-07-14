package tui

import tea "github.com/charmbracelet/bubbletea"

// This file is NOT part of the Task 5 brief. It exists only to make
// internal/tui compile after Task 5, because model.go (as specified in the
// plan) already references writingState/summaryState/historyState and the
// updateWriting/viewWriting/updateSummary/viewSummary/updateHistory/viewHistory
// methods before those screens exist. Task 6 and Task 7 own the real
// definitions.
//
// DELETE THIS FILE as part of Task 6 (when adding writing.go/summary.go,
// which define the real writingState/summaryState types and
// updateWriting/viewWriting/updateSummary/viewSummary methods) and Task 7
// (when adding history.go, which defines the real historyState type and
// updateHistory/viewHistory methods). Leaving this file in place after
// those types/methods are defined elsewhere will cause "redeclared"
// compile errors.

type writingState struct{}

type summaryState struct{}

type historyState struct{}

func (m Model) updateWriting(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m Model) viewWriting() string {
	return ""
}

func (m Model) updateSummary(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m Model) viewSummary() string {
	return ""
}

func (m Model) updateHistory(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m Model) viewHistory() string {
	return ""
}
