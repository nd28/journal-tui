package store

import (
	"testing"
	"time"
)

func TestSaveDraftRoundTrips(t *testing.T) {
	s := openTestStore(t)
	now := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)

	id, err := s.StartSession(now)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveDraft(id, now, "hello there world", 120, 3); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	d, ok, err := s.GetDraft(id)
	if err != nil {
		t.Fatalf("GetDraft: %v", err)
	}
	if !ok {
		t.Fatal("expected a draft to exist")
	}
	if d.Body != "hello there world" {
		t.Fatalf("expected the body to round-trip, got %q", d.Body)
	}
	if d.BodyWords != 3 {
		t.Fatalf("expected BodyWords to be counted from the body, got %d", d.BodyWords)
	}
	if d.RawScore != 120 || d.TotalWords != 3 {
		t.Fatalf("expected the running score to round-trip, got score=%d words=%d", d.RawScore, d.TotalWords)
	}
	if d.UpdatedAt != timestamp(now) {
		t.Fatalf("expected UpdatedAt %q, got %q", timestamp(now), d.UpdatedAt)
	}
}

// The draft mirrors the buffer rather than recording its history: repeated
// saves must overwrite, or a long session accumulates a row per autosave.
func TestSaveDraftReplacesTheExistingRow(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()

	id, err := s.StartSession(now)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveDraft(id, now, "first", 10, 1); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	if err := s.SaveDraft(id, now.Add(time.Minute), "first second", 25, 2); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	var rows int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM drafts WHERE session_id = ?`, id).Scan(&rows); err != nil {
		t.Fatalf("count drafts: %v", err)
	}
	if rows != 1 {
		t.Fatalf("expected exactly one draft row per session, got %d", rows)
	}

	d, _, err := s.GetDraft(id)
	if err != nil {
		t.Fatalf("GetDraft: %v", err)
	}
	if d.Body != "first second" || d.RawScore != 25 || d.TotalWords != 2 {
		t.Fatalf("expected the latest save to win, got %+v", d)
	}
}

func TestGetDraftReportsMissingWithoutError(t *testing.T) {
	s := openTestStore(t)
	id, err := s.StartSession(time.Now())
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	if _, ok, err := s.GetDraft(id); err != nil || ok {
		t.Fatalf("expected (ok=false, err=nil) for a session with no draft, got ok=%v err=%v", ok, err)
	}
}

func TestDeleteDraftRemovesIt(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	id, err := s.StartSession(now)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveDraft(id, now, "text", 10, 1); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	if err := s.DeleteDraft(id); err != nil {
		t.Fatalf("DeleteDraft: %v", err)
	}
	if _, ok, err := s.GetDraft(id); err != nil || ok {
		t.Fatalf("expected the draft to be gone, got ok=%v err=%v", ok, err)
	}
}

func TestDiscardSessionRemovesItsDraft(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	id, err := s.StartSession(now)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveDraft(id, now, "text", 10, 1); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	if err := s.DiscardSession(id); err != nil {
		t.Fatalf("DiscardSession: %v", err)
	}

	var rows int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM drafts`).Scan(&rows); err != nil {
		t.Fatalf("count drafts: %v", err)
	}
	if rows != 0 {
		t.Fatalf("expected the draft to go with its session, got %d rows", rows)
	}
}

func TestUnfinishedSessionsReportsWritingFromBothDraftsAndEntries(t *testing.T) {
	s := openTestStore(t)
	base := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)

	// Finished: never recoverable, however much it holds.
	finished, err := s.StartSession(base)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(finished, base, "done and dusted", 3); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if _, _, err := s.FinishSession(finished, base, 30, 1.0, 1, "2026-08-10"); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}

	// Unfinished but empty: swept, not offered.
	if _, err := s.StartSession(base.Add(time.Hour)); err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	// Unfinished with entries only — the shape a pre-autosave crash leaves.
	entriesOnly, err := s.StartSession(base.Add(2 * time.Hour))
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(entriesOnly, base.Add(2*time.Hour), "one two three four", 4); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}

	// Unfinished with both an entry and a live draft — the newest.
	both, err := s.StartSession(base.Add(3 * time.Hour))
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(both, base.Add(3*time.Hour), "saved words here", 3); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if err := s.SaveDraft(both, base.Add(3*time.Hour), "still in the editor", 40, 7); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}

	got, err := s.UnfinishedSessions()
	if err != nil {
		t.Fatalf("UnfinishedSessions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 recoverable sessions, got %d: %+v", len(got), got)
	}
	if got[0].ID != both {
		t.Fatalf("expected the newest session first, got id %d", got[0].ID)
	}
	if got[0].SavedWords != 3 || got[0].DraftWords != 4 {
		t.Fatalf("expected saved=3 draft=4, got saved=%d draft=%d", got[0].SavedWords, got[0].DraftWords)
	}
	if got[0].TotalWords() != 7 {
		t.Fatalf("expected TotalWords to sum both sources, got %d", got[0].TotalWords())
	}
	if got[1].ID != entriesOnly || got[1].TotalWords() != 4 {
		t.Fatalf("expected the entries-only session second with 4 words, got %+v", got[1])
	}
}

func TestDiscardEmptyUnfinishedSessionsKeepsAnythingWritten(t *testing.T) {
	s := openTestStore(t)
	base := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)

	empty, err := s.StartSession(base)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	// A draft of nothing but whitespace is not writing either.
	whitespace, err := s.StartSession(base.Add(time.Minute))
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveDraft(whitespace, base, "   \n  ", 0, 0); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	withDraft, err := s.StartSession(base.Add(2 * time.Minute))
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveDraft(withDraft, base, "real writing", 20, 2); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	withEntry, err := s.StartSession(base.Add(3 * time.Minute))
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(withEntry, base, "committed text", 2); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}

	n, err := s.DiscardEmptyUnfinishedSessions()
	if err != nil {
		t.Fatalf("DiscardEmptyUnfinishedSessions: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 empty sessions removed, got %d", n)
	}

	for _, tc := range []struct {
		id   int64
		want bool
		name string
	}{
		{empty, false, "empty"},
		{whitespace, false, "whitespace-only draft"},
		{withDraft, true, "draft with writing"},
		{withEntry, true, "session with an entry"},
	} {
		var exists int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id = ?`, tc.id).Scan(&exists); err != nil {
			t.Fatalf("count sessions: %v", err)
		}
		if (exists == 1) != tc.want {
			t.Fatalf("%s: expected exists=%v, got %d rows", tc.name, tc.want, exists)
		}
	}

	// The whitespace draft's session went away; its draft must not linger.
	var orphans int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM drafts WHERE session_id NOT IN (SELECT id FROM sessions)`,
	).Scan(&orphans); err != nil {
		t.Fatalf("count orphan drafts: %v", err)
	}
	if orphans != 0 {
		t.Fatalf("expected no drafts pointing at deleted sessions, got %d", orphans)
	}
}
