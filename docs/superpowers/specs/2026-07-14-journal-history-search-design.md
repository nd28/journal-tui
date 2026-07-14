# Journal — History Search, Pagination, and Reading Past Entries (v1.2 Design)

Date: 2026-07-14

## Purpose

Today's History screen only shows session-level stats (date, score, word
count) for the most recent 20 sessions — there's no way to see more than
that, no way to search, and critically, no way to reread what you actually
wrote. This spec adds all three together, since they share the same
underlying need: access to entry text.

Separate from, and independent of, the editor-polish spec (terminal
sizing, paste blocking, labels, version footer).

## Interaction model

History gets a search box that's always focused — like a fuzzy-finder
(fzf-style). Printable characters typed are appended to the search query
and live-filter the list on every keystroke. A fixed set of keys are
reserved for navigation and never reach the query:

- `Up` / `Down` — move the selection cursor within the current page
- `PgUp` / `PgDn` — move to the previous/next page of results
- `Enter` — open the selected session in a full read view
- `Backspace` — remove the last character of the query
- `Ctrl+U` — clear the query entirely
- `Esc` — back to Home (unconditionally, whether or not a query is active)
- `Ctrl+C` — quit, as everywhere else in the app

An empty query shows all finished sessions, most recent first (today's
behavior, now paginated). Paste is not blocked in the search box — the
paste-blocking in the editor-polish spec is specifically about journal
content, not this query field.

## What's searched, what's shown

Filtering matches against entry *text* (case-insensitive substring), not
just session metadata. A session appears in the filtered list if any of
its entries' body contains the query. Each row shows the existing
`date   score N   M words` line; when a query is active, a second line
shows a snippet (first matching entry's text, truncated to 60 characters)
so you can see why it matched without opening it.

Known accepted limitation: a literal `%` or `_` in the search query is
interpreted as a SQL `LIKE` wildcard rather than a literal character (no
escaping is implemented for v1). Fine for a personal tool; worth revisiting
if it's ever annoying in practice.

## Pagination

Fixed page size of 10 sessions per page. The footer shows `showing X–Y of
N`. Both the plain browse (empty query) and search (non-empty query) cases
share one paginated code path.

## Reading a session

`Enter` on a selected session opens a new screen (`screenRead`) showing
that session's entries in full, using `bubbles/viewport` for scrolling
(new dependency, same family as `bubbles/textarea` already in use). If a
session has more than one entry, each is preceded by a dim `— entry N —`
header; a single-entry session shows its text with no extra header. `Esc`
returns to History with the prior query/page/cursor state intact (no
refetch). Other keys are forwarded to the viewport for scrolling
(Up/Down/PgUp/PgDn — safe to reuse here since Read has no list of its own
to navigate).

## Data layer

**New store method**, replacing the current `ListSessions` call site in
`internal/tui/history.go` (kept or removed from `internal/store` at the
implementer's discretion — functionally superseded):

```go
type SessionSearchResult struct {
    SessionRecord
    Snippet string // first matching entry's body, truncated to 60 chars
                    // with "..." if longer; empty when query is empty
}

func (s *Store) SearchSessions(query string, limit, offset int) (results []SessionSearchResult, total int, err error)
```

A single SQL shape handles both browsing and searching: filtering on
`entries.body LIKE '%' || ? || '%'` naturally matches every row when
`query` is `""` (`LIKE '%%'` matches any string), so there's no branching
between a "browse" query and a "search" query. `total` is a `COUNT(DISTINCT
session id)` for the same filter, used to compute page count.

**New store method** for the read view:

```go
type EntryRecord struct {
    ID        int64
    CreatedAt string
    Body      string
    WordCount int
}

func (s *Store) GetEntries(sessionID int64) ([]EntryRecord, error)
```

Returns a session's entries ordered by `created_at ASC` (the order they
were written in).

## Out of scope for this spec

- Editing or deleting past entries — read-only.
- Full-text search ranking/relevance — plain substring match, most-recent-
  session-first ordering, no scoring of match quality.
- Escaping `%`/`_` in search queries (see accepted limitation above).
- Anything from the editor-polish spec (terminal sizing, paste blocking,
  labels, version footer) — those ship independently.

## Testing

- `internal/store`: unit tests for `SearchSessions` (empty query returns
  everything paginated; non-empty query filters correctly and returns the
  right snippet; `total` is accurate across pages) and `GetEntries`
  (returns entries in write order), all against real SQLite files via
  `t.TempDir()`, matching existing store test conventions.
- `internal/tui`: unit tests for the query-typing/backspace/clear
  transitions, page navigation clamping (can't page past the last page or
  before the first), and the `Enter` → `screenRead` → `Esc` → back-to-
  History-with-state-intact round trip.
- Manual smoke test via tmux: write a few sessions with distinct content,
  search for a word that appears in only one, confirm it's the only result
  with a correct snippet, open it and confirm the full text reads back
  correctly, page through un-filtered results if more than 10 sessions
  exist.
