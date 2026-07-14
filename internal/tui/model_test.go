package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHomeCursorMovesDownAndUp(t *testing.T) {
	m := Model{screen: screenHome}
	updated, _ := m.updateHome(tea.KeyMsg{Type: tea.KeyDown})
	hm := updated.(Model)
	if hm.homeCursor != 1 {
		t.Fatalf("expected cursor 1, got %d", hm.homeCursor)
	}
	updated, _ = hm.updateHome(tea.KeyMsg{Type: tea.KeyUp})
	hm = updated.(Model)
	if hm.homeCursor != 0 {
		t.Fatalf("expected cursor 0, got %d", hm.homeCursor)
	}
}

func TestHomeCursorDoesNotGoBelowZero(t *testing.T) {
	m := Model{screen: screenHome}
	updated, _ := m.updateHome(tea.KeyMsg{Type: tea.KeyUp})
	hm := updated.(Model)
	if hm.homeCursor != 0 {
		t.Fatalf("expected cursor to stay at 0, got %d", hm.homeCursor)
	}
}

func TestHomeCursorDoesNotExceedLastItem(t *testing.T) {
	m := Model{screen: screenHome, homeCursor: len(homeMenuItems) - 1}
	updated, _ := m.updateHome(tea.KeyMsg{Type: tea.KeyDown})
	hm := updated.(Model)
	if hm.homeCursor != len(homeMenuItems)-1 {
		t.Fatalf("expected cursor to stay at %d, got %d", len(homeMenuItems)-1, hm.homeCursor)
	}
}

func TestHomeQuitReturnsQuitMsg(t *testing.T) {
	m := Model{screen: screenHome, homeCursor: len(homeMenuItems) - 1} // "Quit" is last
	_, cmd := m.updateHome(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a command, got nil")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg, got %T", msg)
	}
}
