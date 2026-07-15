# Journal History Search, Pagination, and Reading Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the History screen's static 20-session list with an always-focused fzf-style search box over entry text, fixed 10-per-page pagination, and a new read-only screen for rereading a session's full entries.

**Architecture:** A new `internal/store` query (`SearchSessions`) replaces `ListSessions` as the single data path for both browsing (empty query) and searching (non-empty query), backed by one SQL shape. `internal/tui/history.go` is rewritten to drive that query from a query/page/cursor state machine. A new `internal/tui/read.go` adds `screenRead`, built on `bubbles/viewport` (already vendored via the existing `bubbles` dependency — no `go.mod` change needed), reached via `Enter` from History and returning via `Esc` with History's state left untouched (no refetch).

**Tech Stack:** Go 1.25, `github.com/charmbracelet/bubbletea` (existing), `github.com/charmbracelet/bubbles/viewport` (existing dependency, new subpackage import), `modernc.org/sqlite` (existing).

## Global Constraints

- Page size is fixed at 10 sessions per page, everywhere (`historyPageSize = 10`).
- Filtering matches entry *text* (case-insensitive substring — SQLite's default `LIKE` behavior handles this with no extra code), not session metadata. A session with zero entries never appears in results (even with an empty query), since the match is inherently entry-based.
- Snippet: first matching entry's body (by `created_at ASC`), truncated to 60 runes with a literal `"..."` appended if longer; empty string when the query is empty.
- Known accepted limitation, not to be fixed here: a literal `%` or `_` in the query is interpreted as a SQL `LIKE` wildcard (no escaping in v1).
- Reserved keys on the History screen, never appended to the query: `Up`/`Down` (move cursor within the current page), `PgUp`/`PgDn` (change page, clamped — never below page 0 or past the last page), `Enter` (open the selected session in Read), `Backspace` (remove last query rune), `Ctrl+U` (clear query), `Esc` (back to Home), `Ctrl+C` (quit). Every other key with runes attached appends to the query and reloads at page 0.
- `Esc` from Read returns to History with the prior query, page, and cursor intact and without re-querying the store.
- Read is read-only: no editing or deleting past entries.
- `internal/store.ListSessions` is superseded and removed (not kept) — its only caller is being rewritten in this same plan.
- No changes to `internal/scoring` or to combo/scoring math.

---

### Task 1: Store layer — SearchSessions and GetEntries

**Files:**
- Modify: `internal/store/sessions.go`
- Test: `internal/store/store_test.go`

**Interfaces:**
- Produces:
  ```go
  type SessionSearchResult struct {
      SessionRecord
      Snippet string
  }

  func (s *Store) SearchSessions(query string, limit, offset int) (results []SessionSearchResult, total int, err error)

  type EntryRecord struct {
      ID        int64
      CreatedAt string
      Body      string
      WordCount int
  }

  func (s *Store) GetEntries(sessionID int64) ([]EntryRecord, error)
  ```
- `ListSessions` and its test are removed in Task 2, once `internal/tui/history.go` no longer calls it — leave both in place for this task so the package keeps building.

- [ ] **Step 1: Write the failing tests**

Add `"database/sql"` and `"strings"` to the import block, and these tests, to `internal/store/store_test.go` (current imports are `"path/filepath"`, `"testing"`, `"time"` — add the two new ones alongside):

```go
import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)
```

```go
func TestSearchSessionsEmptyQueryReturnsAllPaginated(t *testing.T) {
	s := openTestStore(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	today := base.Format("2006-01-02")

	id1, err := s.StartSession(base)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id1, base, "hello world", 2); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if _, _, err := s.FinishSession(id1, base, 10, 1.0, 1, today); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}

	later := base.Add(time.Hour)
	id2, err := s.StartSession(later)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id2, later, "a b c", 3); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if _, _, err := s.FinishSession(id2, later, 20, 1.0, 1, today); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}

	results, total, err := s.SearchSessions("", 10, 0)
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if total != 2 {
		t.Fatalf("expected total 2, got %d", total)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].ID != id2 {
		t.Fatalf("expected most recent session (%d) first, got %d", id2, results[0].ID)
	}
	if results[0].WordCount != 3 {
		t.Fatalf("expected 3 words for the latest session, got %d", results[0].WordCount)
	}
	if results[0].Snippet != "" || results[1].Snippet != "" {
		t.Fatalf("expected empty snippets for an empty query, got %q and %q", results[0].Snippet, results[1].Snippet)
	}
}

func TestSearchSessionsFiltersByEntryTextAndReturnsSnippet(t *testing.T) {
	s := openTestStore(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	today := base.Format("2006-01-02")

	id1, err := s.StartSession(base)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id1, base, "the quick brown fox", 4); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if _, _, err := s.FinishSession(id1, base, 10, 1.0, 1, today); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}

	later := base.Add(time.Hour)
	id2, err := s.StartSession(later)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id2, later, "hello world", 2); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if _, _, err := s.FinishSession(id2, later, 20, 1.0, 1, today); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}

	results, total, err := s.SearchSessions("fox", 10, 0)
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected total 1, got %d", total)
	}
	if len(results) != 1 || results[0].ID != id1 {
		t.Fatalf("expected only session %d to match, got %+v", id1, results)
	}
	if results[0].Snippet != "the quick brown fox" {
		t.Fatalf("expected snippet %q, got %q", "the quick brown fox", results[0].Snippet)
	}
}

func TestSearchSessionsIsCaseInsensitive(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	id, err := s.StartSession(now)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id, now, "The Quick Brown Fox", 4); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if _, _, err := s.FinishSession(id, now, 10, 1.0, 1, now.Format("2006-01-02")); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}

	results, total, err := s.SearchSessions("FOX", 10, 0)
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if total != 1 || len(results) != 1 {
		t.Fatalf("expected a case-insensitive match, got total=%d len=%d", total, len(results))
	}
}

func TestSearchSessionsSnippetTruncatesLongEntries(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	long := "start of entry " + strings.Repeat("padding ", 10) + "needle at the end"

	id, err := s.StartSession(now)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id, now, long, 20); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if _, _, err := s.FinishSession(id, now, 10, 1.0, 1, now.Format("2006-01-02")); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}

	results, _, err := s.SearchSessions("needle", 10, 0)
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !strings.HasSuffix(results[0].Snippet, "...") {
		t.Fatalf("expected truncated snippet to end with '...', got %q", results[0].Snippet)
	}
	if len([]rune(results[0].Snippet)) != 63 {
		t.Fatalf("expected a 60-rune snippet plus '...' (63 runes), got %d: %q", len([]rune(results[0].Snippet)), results[0].Snippet)
	}
}

func TestSearchSessionsPaginationTotalAcrossPages(t *testing.T) {
	s := openTestStore(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	today := base.Format("2006-01-02")
	for i := 0; i < 15; i++ {
		startedAt := base.Add(time.Duration(i) * time.Hour)
		id, err := s.StartSession(startedAt)
		if err != nil {
			t.Fatalf("StartSession: %v", err)
		}
		if err := s.SaveEntry(id, startedAt, "day entry", 2); err != nil {
			t.Fatalf("SaveEntry: %v", err)
		}
		if _, _, err := s.FinishSession(id, startedAt, 10, 1.0, 1, today); err != nil {
			t.Fatalf("FinishSession: %v", err)
		}
	}

	page0, total0, err := s.SearchSessions("", 10, 0)
	if err != nil {
		t.Fatalf("SearchSessions page 0: %v", err)
	}
	if total0 != 15 || len(page0) != 10 {
		t.Fatalf("expected 10 of 15 on page 0, got %d of %d", len(page0), total0)
	}

	page1, total1, err := s.SearchSessions("", 10, 10)
	if err != nil {
		t.Fatalf("SearchSessions page 1: %v", err)
	}
	if total1 != 15 || len(page1) != 5 {
		t.Fatalf("expected 5 of 15 on page 1, got %d of %d", len(page1), total1)
	}
}

func TestGetEntriesReturnsEntriesInWriteOrder(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	id, err := s.StartSession(now)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id, now, "first", 1); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if err := s.SaveEntry(id, now.Add(time.Minute), "second", 1); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if err := s.SaveEntry(id, now.Add(2*time.Minute), "third", 1); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}

	entries, err := s.GetEntries(id)
	if err != nil {
		t.Fatalf("GetEntries: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].Body != "first" || entries[1].Body != "second" || entries[2].Body != "third" {
		t.Fatalf("expected entries in write order, got %+v", entries)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/... -run 'TestSearchSessions|TestGetEntries' -v`
Expected: FAIL to compile — `SearchSessions` and `GetEntries` undefined.

- [ ] **Step 3: Add SearchSessions and GetEntries**

In `internal/store/sessions.go`, add `"database/sql"` to the import block (it currently only imports `"time"`):

```go
import (
	"database/sql"
	"time"
)
```

Add these types and methods (anywhere after `SessionRecord`, e.g. right after the existing `ListSessions` function):

```go
type SessionSearchResult struct {
	SessionRecord
	Snippet string
}

// SearchSessions returns finished sessions whose entry text matches query
// (case-insensitive substring), most recent first, paginated by limit/offset.
// An empty query matches every entry via SQL's `LIKE '%%'`, so the same
// query shape handles both plain browsing and searching. total is the
// number of matching sessions across all pages.
func (s *Store) SearchSessions(query string, limit, offset int) ([]SessionSearchResult, int, error) {
	rows, err := s.db.Query(`
		SELECT
			s.id,
			s.started_at,
			s.session_score,
			COALESCE((SELECT SUM(e.word_count) FROM entries e WHERE e.session_id = s.id), 0),
			(SELECT e.body FROM entries e WHERE e.session_id = s.id AND e.body LIKE '%' || ? || '%' ORDER BY e.created_at ASC LIMIT 1)
		FROM sessions s
		WHERE s.ended_at IS NOT NULL
		  AND EXISTS (SELECT 1 FROM entries e WHERE e.session_id = s.id AND e.body LIKE '%' || ? || '%')
		ORDER BY s.started_at DESC
		LIMIT ? OFFSET ?`, query, query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []SessionSearchResult
	for rows.Next() {
		var r SessionSearchResult
		var snippet sql.NullString
		if err := rows.Scan(&r.ID, &r.StartedAt, &r.SessionScore, &r.WordCount, &snippet); err != nil {
			return nil, 0, err
		}
		if query != "" && snippet.Valid {
			r.Snippet = truncateSnippet(snippet.String)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	var total int
	row := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM sessions s
		WHERE s.ended_at IS NOT NULL
		  AND EXISTS (SELECT 1 FROM entries e WHERE e.session_id = s.id AND e.body LIKE '%' || ? || '%')`, query)
	if err := row.Scan(&total); err != nil {
		return nil, 0, err
	}

	return out, total, nil
}

func truncateSnippet(body string) string {
	const maxLen = 60
	r := []rune(body)
	if len(r) <= maxLen {
		return body
	}
	return string(r[:maxLen]) + "..."
}

type EntryRecord struct {
	ID        int64
	CreatedAt string
	Body      string
	WordCount int
}

// GetEntries returns a session's entries ordered by created_at ASC — the
// order they were written in.
func (s *Store) GetEntries(sessionID int64) ([]EntryRecord, error) {
	rows, err := s.db.Query(
		`SELECT id, created_at, body, word_count FROM entries WHERE session_id = ? ORDER BY created_at ASC`,
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []EntryRecord
	for rows.Next() {
		var r EntryRecord
		if err := rows.Scan(&r.ID, &r.CreatedAt, &r.Body, &r.WordCount); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/store/... -v`
Expected: PASS on all tests, including the 6 new ones and every pre-existing store test (`ListSessions` and its test still exist untouched — they're removed in Task 2).

- [ ] **Step 5: Commit**

```bash
git add internal/store/sessions.go internal/store/store_test.go
git commit -m "Add SearchSessions and GetEntries to the store layer"
```

---

### Task 2: History search box and pagination

**Files:**
- Modify: `internal/tui/history.go`
- Modify: `internal/tui/history_test.go`
- Modify: `internal/store/sessions.go` (remove the now-superseded `ListSessions`)
- Modify: `internal/store/store_test.go` (remove `TestListSessionsOrdersMostRecentFirst`)

**Interfaces:**
- Consumes: `store.SearchSessions`, `store.SessionSearchResult` from Task 1.
- Produces: `historyState{query string, page, cursor, total int, results []store.SessionSearchResult}`; `historyPageSize = 10` const; unexported `(m Model) reloadHistory(page int) (tea.Model, tea.Cmd)` and `(m Model) changeHistoryPage(delta int) (tea.Model, tea.Cmd)`, used by Task 3 when wiring `Enter`/`Esc` between History and Read.
- `Enter` is intentionally unhandled in this task (falls through, no-op) — Task 3 wires it to open Read.

- [ ] **Step 1: Remove the superseded ListSessions**

In `internal/store/sessions.go`, delete the `ListSessions` method (it's fully superseded by `SearchSessions`, whose empty-query case returns the same data, paginated):

```go
func (s *Store) ListSessions(limit int) ([]SessionRecord, error) {
	rows, err := s.db.Query(`
		SELECT s.id, s.started_at, s.session_score, COALESCE(SUM(e.word_count), 0)
		FROM sessions s
		LEFT JOIN entries e ON e.session_id = s.id
		WHERE s.ended_at IS NOT NULL
		GROUP BY s.id
		ORDER BY s.started_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SessionRecord
	for rows.Next() {
		var r SessionRecord
		if err := rows.Scan(&r.ID, &r.StartedAt, &r.SessionScore, &r.WordCount); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
```

In `internal/store/store_test.go`, delete `TestListSessionsOrdersMostRecentFirst` (its coverage is superseded by Task 1's `TestSearchSessionsEmptyQueryReturnsAllPaginated` and `TestSearchSessionsPaginationTotalAcrossPages`).

- [ ] **Step 2: Run the store suite to confirm it still builds and passes**

Run: `go build ./internal/store/... && go test ./internal/store/... -v`
Expected: PASS. `internal/tui` will now fail to build (still calls `ListSessions`) — that's expected and fixed in the next step.

- [ ] **Step 3: Write the failing TUI tests**

Replace the entire contents of `internal/tui/history_test.go` with:

```go
package tui

import (
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nd28/journal-tui/internal/store"
)

func TestEnterHistoryLoadsSessions(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	id, err := s.StartSession(time.Now())
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id, time.Now(), "hello world", 2); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if _, _, err := s.FinishSession(id, time.Now(), 42, 1.0, 1, time.Now().Format("2006-01-02")); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}

	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	updated, _ := m.enterHistory()
	m = updated.(Model)

	if m.screen != screenHistory {
		t.Fatalf("expected screenHistory, got %v", m.screen)
	}
	if len(m.history.results) != 1 {
		t.Fatalf("expected 1 session, got %d", len(m.history.results))
	}
	if m.history.results[0].SessionScore != 42 {
		t.Fatalf("expected score 42, got %d", m.history.results[0].SessionScore)
	}
	if m.history.results[0].Snippet != "" {
		t.Fatalf("expected empty snippet with no query, got %q", m.history.results[0].Snippet)
	}
}

func TestHistoryQueryTypingFiltersResults(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	today := base.Format("2006-01-02")

	id1, err := s.StartSession(base)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id1, base, "apple pie recipe", 3); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if _, _, err := s.FinishSession(id1, base, 10, 1.0, 1, today); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}

	later := base.Add(time.Hour)
	id2, err := s.StartSession(later)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id2, later, "banana bread notes", 3); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if _, _, err := s.FinishSession(id2, later, 20, 1.0, 1, today); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}

	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	updated, _ := m.enterHistory()
	m = updated.(Model)
	if len(m.history.results) != 2 {
		t.Fatalf("expected 2 results before filtering, got %d", len(m.history.results))
	}

	for _, r := range []rune("banana") {
		updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}

	if m.history.query != "banana" {
		t.Fatalf("expected query %q, got %q", "banana", m.history.query)
	}
	if len(m.history.results) != 1 {
		t.Fatalf("expected 1 filtered result, got %d", len(m.history.results))
	}
	if m.history.results[0].ID != id2 {
		t.Fatalf("expected session %d, got %d", id2, m.history.results[0].ID)
	}
	if m.history.results[0].Snippet != "banana bread notes" {
		t.Fatalf("expected snippet %q, got %q", "banana bread notes", m.history.results[0].Snippet)
	}
}

func TestHistoryBackspaceRemovesLastQueryChar(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	updated, _ := m.enterHistory()
	m = updated.(Model)

	for _, r := range []rune("abc") {
		updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}
	updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updated.(Model)

	if m.history.query != "ab" {
		t.Fatalf("expected query %q after backspace, got %q", "ab", m.history.query)
	}
}

func TestHistoryCtrlUClearsQuery(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	updated, _ := m.enterHistory()
	m = updated.(Model)

	for _, r := range []rune("abc") {
		updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}
	updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = updated.(Model)

	if m.history.query != "" {
		t.Fatalf("expected empty query after ctrl+u, got %q", m.history.query)
	}
}

func TestHistoryPageNavigationClamps(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	today := base.Format("2006-01-02")
	for i := 0; i < 15; i++ {
		startedAt := base.Add(time.Duration(i) * time.Hour)
		id, err := s.StartSession(startedAt)
		if err != nil {
			t.Fatalf("StartSession: %v", err)
		}
		if err := s.SaveEntry(id, startedAt, "day entry", 2); err != nil {
			t.Fatalf("SaveEntry: %v", err)
		}
		if _, _, err := s.FinishSession(id, startedAt, 10, 1.0, 1, today); err != nil {
			t.Fatalf("FinishSession: %v", err)
		}
	}

	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	updated, _ := m.enterHistory()
	m = updated.(Model)

	if len(m.history.results) != 10 || m.history.total != 15 {
		t.Fatalf("expected 10 results of 15 total on page 0, got %d of %d", len(m.history.results), m.history.total)
	}

	updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyPgDown})
	m = updated.(Model)
	if m.history.page != 1 || len(m.history.results) != 5 {
		t.Fatalf("expected page 1 with 5 results, got page %d with %d results", m.history.page, len(m.history.results))
	}

	updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyPgDown})
	m = updated.(Model)
	if m.history.page != 1 {
		t.Fatalf("expected page to stay clamped at 1, got %d", m.history.page)
	}

	updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyPgUp})
	m = updated.(Model)
	if m.history.page != 0 || len(m.history.results) != 10 {
		t.Fatalf("expected page 0 with 10 results, got page %d with %d results", m.history.page, len(m.history.results))
	}

	updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyPgUp})
	m = updated.(Model)
	if m.history.page != 0 {
		t.Fatalf("expected page to stay clamped at 0, got %d", m.history.page)
	}
}

func TestHistoryUpDownMovesCursorWithinPageAndClamps(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	today := base.Format("2006-01-02")
	for i := 0; i < 3; i++ {
		startedAt := base.Add(time.Duration(i) * time.Hour)
		id, err := s.StartSession(startedAt)
		if err != nil {
			t.Fatalf("StartSession: %v", err)
		}
		if err := s.SaveEntry(id, startedAt, "note", 1); err != nil {
			t.Fatalf("SaveEntry: %v", err)
		}
		if _, _, err := s.FinishSession(id, startedAt, 10, 1.0, 1, today); err != nil {
			t.Fatalf("FinishSession: %v", err)
		}
	}

	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	updated, _ := m.enterHistory()
	m = updated.(Model)

	if m.history.cursor != 0 {
		t.Fatalf("expected cursor to start at 0, got %d", m.history.cursor)
	}

	updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.history.cursor != 2 {
		t.Fatalf("expected cursor 2, got %d", m.history.cursor)
	}

	updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.history.cursor != 2 {
		t.Fatalf("expected cursor clamped at 2, got %d", m.history.cursor)
	}

	updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	if m.history.cursor != 1 {
		t.Fatalf("expected cursor 1, got %d", m.history.cursor)
	}
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test ./internal/tui/... -run TestHistory -v`
Expected: FAIL to compile — `m.history.results`, `m.history.query`, `m.history.page`, `m.history.cursor`, `m.history.total` all undefined (current `historyState` only has `sessions`).

- [ ] **Step 5: Rewrite history.go**

Replace the entire contents of `internal/tui/history.go` with:

```go
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nd28/journal-tui/internal/store"
)

const historyPageSize = 10

type historyState struct {
	query   string
	page    int
	cursor  int
	total   int
	results []store.SessionSearchResult
}

func (m Model) enterHistory() (tea.Model, tea.Cmd) {
	m.screen = screenHistory
	m.history = historyState{}
	return m.reloadHistory(0)
}

// reloadHistory re-queries the store for the given page under the current
// query, resetting the cursor to the top of the new result set.
func (m Model) reloadHistory(page int) (tea.Model, tea.Cmd) {
	m.history.page = page
	m.history.cursor = 0
	results, total, err := m.store.SearchSessions(m.history.query, historyPageSize, page*historyPageSize)
	if err != nil {
		m.err = err
		return m, nil
	}
	m.history.results = results
	m.history.total = total
	return m, nil
}

// changeHistoryPage moves by delta pages, clamped to [0, last page].
func (m Model) changeHistoryPage(delta int) (tea.Model, tea.Cmd) {
	totalPages := (m.history.total + historyPageSize - 1) / historyPageSize
	if totalPages == 0 {
		totalPages = 1
	}
	newPage := m.history.page + delta
	if newPage < 0 {
		newPage = 0
	}
	if newPage > totalPages-1 {
		newPage = totalPages - 1
	}
	return m.reloadHistory(newPage)
}

func (m Model) updateHistory(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch keyMsg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		m.screen = screenHome
		m.homeCursor = 0
		return m, nil
	case tea.KeyUp:
		if m.history.cursor > 0 {
			m.history.cursor--
		}
		return m, nil
	case tea.KeyDown:
		if m.history.cursor < len(m.history.results)-1 {
			m.history.cursor++
		}
		return m, nil
	case tea.KeyPgUp:
		return m.changeHistoryPage(-1)
	case tea.KeyPgDown:
		return m.changeHistoryPage(1)
	case tea.KeyBackspace:
		if len(m.history.query) > 0 {
			r := []rune(m.history.query)
			m.history.query = string(r[:len(r)-1])
		}
		return m.reloadHistory(0)
	case tea.KeyCtrlU:
		m.history.query = ""
		return m.reloadHistory(0)
	case tea.KeySpace:
		m.history.query += " "
		return m.reloadHistory(0)
	case tea.KeyRunes:
		m.history.query += string(keyMsg.Runes)
		return m.reloadHistory(0)
	}
	return m, nil
}

func (m Model) viewHistory() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("History") + "\n\n")
	b.WriteString(statStyle.Render("search: "+m.history.query) + "\n\n")

	if len(m.history.results) == 0 {
		b.WriteString(statStyle.Render("No sessions found.") + "\n")
	}
	for i, r := range m.history.results {
		cursor := "  "
		style := statStyle
		if i == m.history.cursor {
			cursor = "> "
			style = selectedStyle
		}
		b.WriteString(cursor + style.Render(fmt.Sprintf("%s   score %d   %d words", r.StartedAt, r.SessionScore, r.WordCount)) + "\n")
		if r.Snippet != "" {
			b.WriteString("    " + statStyle.Render(r.Snippet) + "\n")
		}
	}

	from, to := 0, 0
	if m.history.total > 0 {
		from = m.history.page*historyPageSize + 1
		to = from + len(m.history.results) - 1
	}
	b.WriteString("\n" + statStyle.Render(fmt.Sprintf("showing %d-%d of %d", from, to, m.history.total)) + "\n")
	b.WriteString(statStyle.Render("enter: read   pgup/pgdn: page   esc: back to home"))
	return b.String()
}
```

- [ ] **Step 6: Run the full package test suite**

Run: `go build ./... && go test ./... -v`
Expected: PASS on all packages, including the 6 tests in `internal/tui/history_test.go` and everything in `internal/store` and `internal/tui`.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/history.go internal/tui/history_test.go internal/store/sessions.go internal/store/store_test.go
git commit -m "Add fzf-style search and pagination to the History screen"
```

---

### Task 3: Read view for full session text

**Files:**
- Create: `internal/tui/read.go`
- Create: `internal/tui/read_test.go`
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/history.go`

**Interfaces:**
- Consumes: `store.GetEntries`, `store.EntryRecord` from Task 1; `historyState.results`/`.cursor` from Task 2.
- Produces: `screenRead` screen constant; `Model.read readState`; `(m Model) enterRead(sessionID int64) (tea.Model, tea.Cmd)`; `(m Model) updateRead(msg tea.Msg) (tea.Model, tea.Cmd)`; `(m Model) viewRead() string`.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/read_test.go`:

```go
package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nd28/journal-tui/internal/store"
)

func TestReadViewportSizeUsesTerminalHeightMinusChrome(t *testing.T) {
	w, h := readViewportSize(100, 30)
	if w != 100 {
		t.Fatalf("expected width 100, got %d", w)
	}
	if h != 30-readChromeLines {
		t.Fatalf("expected height %d, got %d", 30-readChromeLines, h)
	}
}

func TestReadViewportSizeFloorsOnTinyTerminal(t *testing.T) {
	_, h := readViewportSize(20, 5)
	if h != readMinHeight {
		t.Fatalf("expected height floor %d, got %d", readMinHeight, h)
	}
}

func TestRenderReadEntriesSingleEntryNoHeader(t *testing.T) {
	entries := []store.EntryRecord{{ID: 1, CreatedAt: "2026-07-15T10:00:00Z", Body: "just one entry", WordCount: 3}}
	got := renderReadEntries(entries)
	if got != "just one entry" {
		t.Fatalf("expected raw body with no header, got %q", got)
	}
}

func TestRenderReadEntriesMultipleEntriesGetHeaders(t *testing.T) {
	entries := []store.EntryRecord{
		{ID: 1, CreatedAt: "2026-07-15T10:00:00Z", Body: "alpha", WordCount: 1},
		{ID: 2, CreatedAt: "2026-07-15T10:01:00Z", Body: "beta", WordCount: 1},
	}
	got := renderReadEntries(entries)
	if !strings.Contains(got, "— entry 1 —") || !strings.Contains(got, "— entry 2 —") {
		t.Fatalf("expected numbered headers, got %q", got)
	}
	if !strings.Contains(got, "alpha") || !strings.Contains(got, "beta") {
		t.Fatalf("expected both bodies present, got %q", got)
	}
}

func TestEnterReadLoadsEntries(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	now := time.Now()
	id, err := s.StartSession(now)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id, now, "first entry text", 3); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if err := s.SaveEntry(id, now.Add(time.Minute), "second entry text", 3); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if _, _, err := s.FinishSession(id, now, 10, 1.0, 1, now.Format("2006-01-02")); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}

	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.width, m.height = 100, 30

	updated, _ := m.enterRead(id)
	m = updated.(Model)

	if m.screen != screenRead {
		t.Fatalf("expected screenRead, got %v", m.screen)
	}
	if len(m.read.entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(m.read.entries))
	}
	view := m.read.viewport.View()
	if !strings.Contains(view, "first entry text") || !strings.Contains(view, "second entry text") {
		t.Fatalf("expected viewport to contain both entries, got %q", view)
	}
	if !strings.Contains(view, "— entry 1 —") || !strings.Contains(view, "— entry 2 —") {
		t.Fatalf("expected entry headers for multi-entry session, got %q", view)
	}
}

func TestHistoryEnterOpensReadForSelectedSessionAndEscReturnsWithStateIntact(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	today := base.Format("2006-01-02")

	id1, err := s.StartSession(base)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id1, base, "apple pie recipe", 3); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if _, _, err := s.FinishSession(id1, base, 10, 1.0, 1, today); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}

	later := base.Add(time.Hour)
	id2, err := s.StartSession(later)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id2, later, "banana bread notes", 3); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if _, _, err := s.FinishSession(id2, later, 20, 1.0, 1, today); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}

	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.width, m.height = 100, 30

	updated, _ := m.enterHistory()
	m = updated.(Model)

	for _, r := range []rune("apple") {
		updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}
	if len(m.history.results) != 1 {
		t.Fatalf("expected 1 filtered result, got %d", len(m.history.results))
	}
	wantQuery, wantPage, wantCursor := m.history.query, m.history.page, m.history.cursor

	updated, _ = m.updateHistory(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.screen != screenRead {
		t.Fatalf("expected screenRead after enter, got %v", m.screen)
	}
	if len(m.read.entries) != 1 || m.read.entries[0].Body != "apple pie recipe" {
		t.Fatalf("expected the filtered session's entry, got %+v", m.read.entries)
	}

	updated, _ = m.updateRead(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.screen != screenHistory {
		t.Fatalf("expected screenHistory after esc, got %v", m.screen)
	}
	if m.history.query != wantQuery || m.history.page != wantPage || m.history.cursor != wantCursor {
		t.Fatalf("expected history state intact, got query=%q page=%d cursor=%d", m.history.query, m.history.page, m.history.cursor)
	}
	if len(m.history.results) != 1 {
		t.Fatalf("expected results still intact without refetch, got %d", len(m.history.results))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/... -v`
Expected: FAIL to compile — `readViewportSize`, `readChromeLines`, `readMinHeight`, `renderReadEntries`, `screenRead`, `m.read`, `m.enterRead`, `m.updateRead` all undefined.

- [ ] **Step 3: Add screenRead and the read field to Model**

In `internal/tui/model.go`, change the `screen` const block from:

```go
const (
	screenHome screen = iota
	screenWriting
	screenSummary
	screenHistory
)
```

to:

```go
const (
	screenHome screen = iota
	screenWriting
	screenSummary
	screenHistory
	screenRead
)
```

Add `read readState` to the `Model` struct (after `history historyState`):

```go
type Model struct {
	screen screen
	store  *store.Store
	stats  store.Stats

	width       int
	height      int
	compactMode bool

	homeCursor int

	writing writingState
	summary summaryState
	history historyState
	read    readState

	err error
}
```

Add `screenRead` cases to `Update()` and `View()`:

```go
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if sizeMsg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = sizeMsg.Width
		m.height = sizeMsg.Height
	}

	switch m.screen {
	case screenHome:
		return m.updateHome(msg)
	case screenWriting:
		return m.updateWriting(msg)
	case screenSummary:
		return m.updateSummary(msg)
	case screenHistory:
		return m.updateHistory(msg)
	case screenRead:
		return m.updateRead(msg)
	}
	return m, nil
}
```

```go
func (m Model) View() string {
	var body string
	switch m.screen {
	case screenHome:
		body = m.viewHome()
	case screenWriting:
		body = m.viewWriting()
	case screenSummary:
		body = m.viewSummary()
	case screenHistory:
		body = m.viewHistory()
	case screenRead:
		body = m.viewRead()
	}
	if m.err != nil {
		body += "\n" + errorStyle.Render("Error: "+m.err.Error())
	}
	body += "\n" + statStyle.Render("journal v"+Version)
	return body
}
```

- [ ] **Step 4: Create read.go**

Create `internal/tui/read.go`:

```go
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/nd28/journal-tui/internal/store"
)

const (
	// readChromeLines is the Read screen's fixed vertical overhead: the
	// title line, a blank separator, the help line, and the version
	// footer appended by Model.View().
	readChromeLines = 4
	readMinHeight   = 3
)

type readState struct {
	viewport viewport.Model
	entries  []store.EntryRecord
}

// readViewportSize computes the viewport's width/height from the terminal
// size, reserving readChromeLines for the screen's fixed text and flooring
// the height so a tiny terminal never yields a non-positive viewport.
func readViewportSize(termWidth, termHeight int) (width, height int) {
	width = termWidth
	height = termHeight - readChromeLines
	if height < readMinHeight {
		height = readMinHeight
	}
	return width, height
}

// renderReadEntries renders a session's entries for the viewport. A single
// entry is shown with no extra header; multiple entries each get a dim
// "— entry N —" header.
func renderReadEntries(entries []store.EntryRecord) string {
	if len(entries) == 1 {
		return entries[0].Body
	}
	var b strings.Builder
	for i, e := range entries {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(statStyle.Render(fmt.Sprintf("— entry %d —", i+1)) + "\n")
		b.WriteString(e.Body)
	}
	return b.String()
}

func (m Model) enterRead(sessionID int64) (tea.Model, tea.Cmd) {
	entries, err := m.store.GetEntries(sessionID)
	if err != nil {
		m.err = err
		return m, nil
	}
	w, h := readViewportSize(m.width, m.height)
	vp := viewport.New(w, h)
	vp.SetContent(renderReadEntries(entries))
	m.read = readState{viewport: vp, entries: entries}
	m.screen = screenRead
	return m, nil
}

func (m Model) updateRead(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyEsc:
			m.screen = screenHistory
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.read.viewport, cmd = m.read.viewport.Update(msg)
	return m, cmd
}

func (m Model) viewRead() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Read") + "\n\n")
	b.WriteString(m.read.viewport.View() + "\n")
	b.WriteString(statStyle.Render("up/down/pgup/pgdn: scroll   esc: back to history"))
	return b.String()
}
```

- [ ] **Step 5: Wire Enter in updateHistory**

In `internal/tui/history.go`, add a `tea.KeyEnter` case to `updateHistory`'s switch (the switch currently has cases for `KeyCtrlC`, `KeyEsc`, `KeyUp`, `KeyDown`, `KeyPgUp`, `KeyPgDown`, `KeyBackspace`, `KeyCtrlU`, `KeySpace`, `KeyRunes`); add this case anywhere in the switch, e.g. right after `KeyEsc`:

```go
	case tea.KeyEnter:
		if len(m.history.results) == 0 {
			return m, nil
		}
		return m.enterRead(m.history.results[m.history.cursor].ID)
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go build ./... && go vet ./... && go test ./... -v`
Expected: PASS on the full suite (`internal/scoring`, `internal/store`, `internal/tui`), including the 5 new tests in `read_test.go` and every pre-existing test.

- [ ] **Step 7: Manual smoke test**

Run: `go run ./cmd/journal`

1. Write and finish 2-3 sessions with distinct, memorable text in each.
2. Go to History. Confirm the search box is present and focused; type a word that appears in only one session's text — confirm only that session shows, with a snippet line beneath it.
3. Press `Ctrl+U` — confirm the query clears and all sessions reappear.
4. If you have more than 10 finished sessions, press `PgDn`/`PgUp` and confirm the footer's `showing X-Y of N` updates and pagination doesn't go past the first/last page.
5. Move the cursor with `Up`/`Down`, then press `Enter` on a highlighted session — confirm the Read screen shows its full entry text (with `— entry N —` headers if it has more than one entry).
6. Press `Esc` from Read — confirm you're back on History with the same search query, page, and cursor position you left.
7. Press `Esc` from History — confirm you're back on Home.

If any step fails, fix the underlying code (not the test) before proceeding.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/read.go internal/tui/read_test.go internal/tui/model.go internal/tui/history.go
git commit -m "Add read-only screen for full session text via bubbles/viewport"
```

---

## After this plan

Not in scope here: editing/deleting past entries, full-text search ranking/relevance, escaping `%`/`_` in search queries (accepted v1 limitation), and anything from the editor-polish plan (already shipped separately).
