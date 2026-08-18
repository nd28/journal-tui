package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nd28/journal-tui/internal/store"
)

func testSession(score, words int, peak float64) store.SessionSearchResult {
	return store.SessionSearchResult{SessionRecord: store.SessionRecord{
		ID:                 1,
		StartedAt:          "2026-07-15T10:00:00Z",
		SessionScore:       score,
		WordCount:          words,
		PeakIntensityRatio: peak,
	}}
}

func TestRenderShareTextHasStatsHeaderThenBody(t *testing.T) {
	session := testSession(1234, 2, 0)
	entries := []store.EntryRecord{{Body: "hello world"}}

	got := renderShareText(session, entries)

	header, body, found := strings.Cut(got, "\n\n")
	if !found {
		t.Fatalf("expected a header separated from the body by a blank line, got %q", got)
	}
	if !strings.Contains(header, formatSessionDate(session.StartedAt)) {
		t.Errorf("header %q missing session date", header)
	}
	if !strings.Contains(header, "1,234") {
		t.Errorf("header %q missing formatted score", header)
	}
	if !strings.Contains(header, "2 words") {
		t.Errorf("header %q missing word count", header)
	}
	if body != "hello world" {
		t.Errorf("body = %q, want %q", body, "hello world")
	}
}

func TestRenderShareTextSeparatesMultipleEntries(t *testing.T) {
	session := testSession(10, 4, 0)
	entries := []store.EntryRecord{{Body: "first thought"}, {Body: "second thought"}}

	got := renderShareText(session, entries)

	if strings.Contains(got, "first thoughtsecond thought") {
		t.Fatalf("entries ran together: %q", got)
	}
	for _, want := range []string{"— entry 1 —", "— entry 2 —", "first thought", "second thought"} {
		if !strings.Contains(got, want) {
			t.Errorf("output %q missing %q", got, want)
		}
	}
	if strings.Index(got, "first thought") > strings.Index(got, "second thought") {
		t.Error("entries are out of written order")
	}
}

func TestRenderShareTextOmitsEntryMarkerForSingleEntry(t *testing.T) {
	got := renderShareText(testSession(10, 2, 0), []store.EntryRecord{{Body: "hello world"}})

	if strings.Contains(got, "entry 1") {
		t.Errorf("single entry should not be labelled, got %q", got)
	}
}

// fakeClipboard swaps the real clipboard out for the duration of a test, so
// tests never touch the machine's actual clipboard. It returns a pointer to
// what was last copied.
func fakeClipboard(t *testing.T, err error) *string {
	t.Helper()
	var copied string
	original := copyToClipboard
	copyToClipboard = func(text string) error {
		copied = text
		return err
	}
	t.Cleanup(func() { copyToClipboard = original })
	return &copied
}

// finishedSession writes one finished session to the store and returns the
// model sitting on the History screen with it selected.
func finishedSession(t *testing.T, bodies ...string) Model {
	t.Helper()
	s, _ := openStore(t)
	id, err := s.StartSession(time.Now())
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	for _, body := range bodies {
		if err := s.SaveEntry(id, time.Now(), body, len(strings.Fields(body))); err != nil {
			t.Fatalf("SaveEntry: %v", err)
		}
	}
	if _, _, err := s.FinishSession(id, time.Now(), 42, 1.0, 1, time.Now().Format("2006-01-02")); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}
	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	updated, _ := m.enterHistory()
	return updated.(Model)
}

func TestCtrlSOnReadCopiesTheSessionToTheClipboard(t *testing.T) {
	copied := fakeClipboard(t, nil)
	m := finishedSession(t, "hello world")
	updated, _ := m.updateHistory(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	updated, _ = m.updateRead(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(Model)

	want := renderShareText(m.read.session, m.read.entries)
	if *copied != want {
		t.Errorf("copied %q, want %q", *copied, want)
	}
	if m.screen != screenRead {
		t.Errorf("copying should stay on the Read screen, got %v", m.screen)
	}
	if !strings.Contains(m.shareStatus, "copied") {
		t.Errorf("shareStatus = %q, want it to confirm the copy", m.shareStatus)
	}
}

func TestCtrlSOnHistoryCopiesTheHighlightedSession(t *testing.T) {
	copied := fakeClipboard(t, nil)
	m := finishedSession(t, "first thought", "second thought")

	updated, _ := m.updateHistory(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(Model)

	if !strings.Contains(*copied, "first thought") || !strings.Contains(*copied, "second thought") {
		t.Errorf("copied %q, want both entry bodies", *copied)
	}
	if m.screen != screenHistory {
		t.Errorf("copying should stay on the History screen, got %v", m.screen)
	}
	if !strings.Contains(m.shareStatus, "copied") {
		t.Errorf("shareStatus = %q, want it to confirm the copy", m.shareStatus)
	}
	// The History screen turns ordinary runes into a search query. Sharing
	// must not leak into it.
	if m.history.query != "" {
		t.Errorf("ctrl+s typed into the search query: %q", m.history.query)
	}
}

func TestCtrlSOnEmptyHistoryDoesNothing(t *testing.T) {
	copied := fakeClipboard(t, nil)
	s, _ := openStore(t)
	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	updated, _ := m.enterHistory()
	m = updated.(Model)

	updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(Model)

	if *copied != "" {
		t.Errorf("copied %q from an empty history, want nothing", *copied)
	}
	if m.shareStatus != "" {
		t.Errorf("shareStatus = %q, want nothing claimed", m.shareStatus)
	}
	if m.err != nil {
		t.Errorf("unexpected error: %v", m.err)
	}
}

func TestClipboardFailureIsReportedAndClaimsNothing(t *testing.T) {
	fakeClipboard(t, errors.New("no clipboard tool installed"))
	m := finishedSession(t, "hello world")

	updated, _ := m.updateHistory(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(Model)

	if m.err == nil {
		t.Fatal("expected the clipboard failure to surface")
	}
	if m.shareStatus != "" {
		t.Errorf("shareStatus = %q, want no copy claimed when the copy failed", m.shareStatus)
	}
	if !strings.Contains(m.View(), "no clipboard tool installed") {
		t.Error("the failure should be visible on screen")
	}
}

func TestShareStatusShowsOnScreenUntilTheNextKeypress(t *testing.T) {
	fakeClipboard(t, nil)
	m := finishedSession(t, "hello world")

	updated, _ := m.updateHistory(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(Model)
	if !strings.Contains(m.View(), "copied") {
		t.Fatalf("expected the copy confirmation on screen, got:\n%s", m.View())
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)

	if strings.Contains(m.View(), "copied") {
		t.Errorf("the confirmation outlived the next keypress:\n%s", m.View())
	}
}

func TestHistoryAndReadHelpAdvertiseCopying(t *testing.T) {
	fakeClipboard(t, nil)
	m := finishedSession(t, "hello world")
	if !strings.Contains(m.View(), "ctrl+s") {
		t.Errorf("History help should mention ctrl+s:\n%s", m.View())
	}

	updated, _ := m.updateHistory(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if !strings.Contains(m.View(), "ctrl+s") {
		t.Errorf("Read help should mention ctrl+s:\n%s", m.View())
	}
}
