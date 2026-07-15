# Journal Writing Intensity Detection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Detect when a session's typing pace is significantly above the user's own recent baseline, tag it with a tier ("Focused"/"Intense"/"Frantic"), show it live while writing, and persist it so it's visible on the Summary, History, and Read screens.

**Architecture:** A new `PaceTracker` in `internal/scoring` measures words-per-minute over a 60-second sliding window, independent of the existing combo meter. `internal/store` gains two nullable columns on `sessions` (via an `ALTER TABLE` migration) and two new methods: `RecentAvgPace` (personal baseline) and `RecordSessionPace` (persist a finished session's pace data, kept separate from `FinishSession` to avoid touching ~20 unrelated call sites). `internal/tui/writing.go` fetches the baseline at session start, tracks the live ratio and its session peak on every combo tick, and shows a tier tag in the header only when elevated. The peak ratio flows into `summaryState` and is persisted at session end; Summary, History, and Read then display it via a shared formatting helper.

**Tech Stack:** Go 1.25 (existing), `modernc.org/sqlite` (existing, no new dependency).

## Global Constraints

- No change to scoring, the combo meter, or session/point calculation — `internal/scoring/combo.go` and `session.go`'s existing `RawScore`/`TotalWords` behavior are untouched.
- Live pace window: 60 seconds (`PaceWindow`), floored at 5 seconds (`PaceMinElapsed`) so the first couple of words in a session (or after a long gap) never produce a spurious spike.
- Intensity tiers, by ratio of live WPM to personal baseline WPM: `IntensityFocusedRatio = 1.3`, `IntensityIntenseRatio = 1.8`, `IntensityFranticRatio = 2.5`. Below 1.3x: empty tier, no tag shown anywhere.
- Baseline = average `avg_pace_wpm` over the last 10 finished sessions that have it recorded (`RecentAvgPace(10)`, called from `writing.go`). Requires at least 3 such sessions, or `ok` is `false` and intensity detection is inactive for the entire session (no baseline to compare against).
- `RecordSessionPace` is a separate store method, not a `FinishSession` signature change — this keeps the ~20 existing `FinishSession` call sites across the test suite unaffected; they simply leave the two new columns `NULL`, which `RecentAvgPace` and `SearchSessions` already treat as "no data."
- No manual mood tagging, no "Calm"/below-baseline tier, no retroactive backfill of pace data for sessions recorded before this feature ships.
- Emotion-neutral labels only ("Intense", not "Angry") — pace is a proxy for many possible strong emotions, not a specific one.

---

### Task 1: Scoring — PaceTracker and IntensityTier

**Files:**
- Create: `internal/scoring/intensity.go`
- Create: `internal/scoring/intensity_test.go`
- Modify: `internal/scoring/session.go`
- Modify: `internal/scoring/session_test.go`

**Interfaces:**
- Produces:
  ```go
  const (
      PaceWindow     = 60 * time.Second
      PaceMinElapsed = 5 * time.Second
  )

  type PaceTracker struct{ /* unexported */ }

  func (p *PaceTracker) CompleteWord(now time.Time)
  func (p *PaceTracker) WPM(now time.Time) float64

  const (
      IntensityFocusedRatio = 1.3
      IntensityIntenseRatio = 1.8
      IntensityFranticRatio = 2.5
  )

  func IntensityTier(ratio float64) string // "", "Focused", "Intense", or "Frantic"
  ```
  `Session` gains a `Pace PaceTracker` field, fed from the same `CompleteWord` call that already feeds `Combo`.

- [ ] **Step 1: Write the failing tests**

Create `internal/scoring/intensity_test.go`:

```go
package scoring

import (
	"testing"
	"time"
)

func TestPaceTrackerWPMZeroWithNoEvents(t *testing.T) {
	var p PaceTracker
	if got := p.WPM(time.Now()); got != 0 {
		t.Fatalf("expected 0 WPM with no events, got %v", got)
	}
}

func TestPaceTrackerWPMFloorsElapsedForFewEarlyWords(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var p PaceTracker
	p.CompleteWord(t0)
	p.CompleteWord(t0.Add(1 * time.Second))

	// Only 1s has actually elapsed, but PaceMinElapsed floors the
	// denominator to 5s: 2 words / (5s in minutes) = 24 WPM.
	if got := p.WPM(t0.Add(1 * time.Second)); got != 24 {
		t.Fatalf("expected 24 WPM (floored), got %v", got)
	}
}

func TestPaceTrackerWPMOverFullWindow(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var p PaceTracker
	now := t0
	for i := 0; i < 5; i++ {
		p.CompleteWord(now)
		now = now.Add(5 * time.Second)
	}

	// 5 words spread across 20s (t0 to t0+20s, all still well within the
	// 60s window); elapsed since the oldest event, measured at the last
	// word's timestamp, is 20s = 1/3 minute — above the 5s floor, so it's
	// used directly: 5 / (1/3) = 15 WPM.
	if got := p.WPM(now.Add(-5 * time.Second)); got != 15 {
		t.Fatalf("expected 15 WPM, got %v", got)
	}
}

func TestPaceTrackerDropsEventsOutsideWindow(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var p PaceTracker
	p.CompleteWord(t0)
	p.CompleteWord(t0.Add(90 * time.Second)) // 90s later: the first event is now outside the 60s window

	// Only the second event remains; WPM at that same instant has 0
	// elapsed, floored to 5s: 1 / (5/60) = 12.
	if got := p.WPM(t0.Add(90 * time.Second)); got != 12 {
		t.Fatalf("expected 12 WPM once the first event ages out of the window, got %v", got)
	}
}

func TestIntensityTierThresholds(t *testing.T) {
	cases := []struct {
		ratio float64
		want  string
	}{
		{0, ""},
		{1.0, ""},
		{1.29, ""},
		{1.3, "Focused"},
		{1.7, "Focused"},
		{1.8, "Intense"},
		{2.4, "Intense"},
		{2.5, "Frantic"},
		{10, "Frantic"},
	}
	for _, c := range cases {
		if got := IntensityTier(c.ratio); got != c.want {
			t.Fatalf("IntensityTier(%v) = %q, want %q", c.ratio, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/scoring/... -run 'TestPaceTracker|TestIntensityTier' -v`
Expected: FAIL to compile — `PaceTracker` and `IntensityTier` undefined.

- [ ] **Step 3: Add intensity.go**

Create `internal/scoring/intensity.go`:

```go
package scoring

import "time"

const (
	// PaceWindow is how far back CompleteWord/WPM look when computing a
	// live words-per-minute reading.
	PaceWindow = 60 * time.Second

	// PaceMinElapsed floors the denominator in WPM so the first couple of
	// words in a session (or right after a long pause) don't produce a
	// spurious spike — e.g. 2 words 1 real second apart would otherwise
	// imply 120 WPM.
	PaceMinElapsed = 5 * time.Second
)

// PaceTracker records the timestamps of recently completed words within a
// sliding window, independent of ComboState, to measure actual typing
// speed rather than the game-feel combo multiplier.
type PaceTracker struct {
	events []time.Time
}

// CompleteWord records a word completed at now, then drops events older
// than PaceWindow.
func (p *PaceTracker) CompleteWord(now time.Time) {
	p.events = append(p.events, now)
	cutoff := now.Add(-PaceWindow)
	i := 0
	for i < len(p.events) && p.events[i].Before(cutoff) {
		i++
	}
	p.events = p.events[i:]
}

// WPM returns the current words-per-minute reading: the count of events
// still in the window, divided by the elapsed time since the oldest of
// them, floored at PaceMinElapsed. Returns 0 with no recorded events.
func (p *PaceTracker) WPM(now time.Time) float64 {
	if len(p.events) == 0 {
		return 0
	}
	elapsed := now.Sub(p.events[0])
	if elapsed < PaceMinElapsed {
		elapsed = PaceMinElapsed
	}
	return float64(len(p.events)) / elapsed.Minutes()
}

const (
	IntensityFocusedRatio = 1.3
	IntensityIntenseRatio = 1.8
	IntensityFranticRatio = 2.5
)

// IntensityTier labels a pace ratio (live WPM / personal baseline WPM)
// against fixed thresholds. An empty string means pace isn't notably
// elevated.
func IntensityTier(ratio float64) string {
	switch {
	case ratio >= IntensityFranticRatio:
		return "Frantic"
	case ratio >= IntensityIntenseRatio:
		return "Intense"
	case ratio >= IntensityFocusedRatio:
		return "Focused"
	default:
		return ""
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go build ./internal/scoring/... && go test ./internal/scoring/... -v`
Expected: PASS on the new tests. `Session`-related tests still pass unchanged (Step 5 below hasn't wired `Pace` in yet).

- [ ] **Step 5: Write the failing Session-wiring test**

Add to `internal/scoring/session_test.go` (after `TestSessionAggregatesAcrossEntries`):

```go
func TestCompleteWordFeedsPaceTracker(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := NewSession(t0)
	s.CompleteWord(t0)
	s.CompleteWord(t0.Add(1 * time.Second))

	// Same floored-window math as TestPaceTrackerWPMFloorsElapsedForFewEarlyWords.
	if got := s.Pace.WPM(t0.Add(1 * time.Second)); got != 24 {
		t.Fatalf("expected Session.CompleteWord to feed Pace, got %v WPM", got)
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `go test ./internal/scoring/... -run TestCompleteWordFeedsPaceTracker -v`
Expected: FAIL to compile — `Session` has no field `Pace`.

- [ ] **Step 7: Wire Pace into Session**

In `internal/scoring/session.go`, change the `Session` struct from:

```go
type Session struct {
	Combo   ComboState
	Entries []Entry

	currentWords  int
	currentPoints int
}
```

to:

```go
type Session struct {
	Combo   ComboState
	Pace    PaceTracker
	Entries []Entry

	currentWords  int
	currentPoints int
}
```

Change `CompleteWord` from:

```go
func (s *Session) CompleteWord(now time.Time) {
	points := s.Combo.CompleteWord(now)
	s.currentPoints += points
	s.currentWords++
}
```

to:

```go
func (s *Session) CompleteWord(now time.Time) {
	points := s.Combo.CompleteWord(now)
	s.Pace.CompleteWord(now)
	s.currentPoints += points
	s.currentWords++
}
```

- [ ] **Step 8: Run the full scoring suite**

Run: `go build ./... && go test ./internal/scoring/... -v`
Expected: PASS on all tests, including every pre-existing `combo_test.go`/`session_test.go` test.

- [ ] **Step 9: Commit**

```bash
git add internal/scoring/intensity.go internal/scoring/intensity_test.go internal/scoring/session.go internal/scoring/session_test.go
git commit -m "Add PaceTracker and IntensityTier for writing-intensity detection"
```

---

### Task 2: Store — pace persistence and personal baseline

**Files:**
- Modify: `internal/store/store.go`
- Modify: `internal/store/sessions.go`
- Modify: `internal/store/store_test.go`

**Interfaces:**
- Consumes: nothing from Task 1 (this task has no dependency on `internal/scoring`).
- Produces:
  ```go
  func (s *Store) RecordSessionPace(sessionID int64, avgPaceWPM, peakIntensityRatio float64) error
  func (s *Store) RecentAvgPace(n int) (avgWPM float64, ok bool, err error)
  ```
  `SessionRecord` gains `AvgPaceWPM float64` and `PeakIntensityRatio float64` fields, populated by `SearchSessions`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/store/store_test.go` (after `TestGetEntriesReturnsEntriesInWriteOrder`):

```go
func TestOpenTwiceOnSameFileToleratesRepeatMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "journal.db")

	s1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	s1.Close()

	if _, err := Open(path); err != nil {
		t.Fatalf("second Open on the same file: %v", err)
	}
}

func TestRecordSessionPacePersistsAndSearchSessionsReturnsIt(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	today := now.Format("2006-01-02")

	id, err := s.StartSession(now)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id, now, "hello world", 2); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if _, _, err := s.FinishSession(id, now, 10, 1.0, 1, today); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}
	if err := s.RecordSessionPace(id, 42.5, 2.1); err != nil {
		t.Fatalf("RecordSessionPace: %v", err)
	}

	results, _, err := s.SearchSessions("", 10, 0)
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].AvgPaceWPM != 42.5 {
		t.Fatalf("expected AvgPaceWPM 42.5, got %v", results[0].AvgPaceWPM)
	}
	if results[0].PeakIntensityRatio != 2.1 {
		t.Fatalf("expected PeakIntensityRatio 2.1, got %v", results[0].PeakIntensityRatio)
	}
}

func TestSearchSessionsWithoutRecordedPaceReturnsZero(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	id, err := s.StartSession(now)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if err := s.SaveEntry(id, now, "hello", 1); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
	if _, _, err := s.FinishSession(id, now, 10, 1.0, 1, now.Format("2006-01-02")); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}

	results, _, err := s.SearchSessions("", 10, 0)
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].AvgPaceWPM != 0 || results[0].PeakIntensityRatio != 0 {
		t.Fatalf("expected zero pace fields without RecordSessionPace, got %+v", results[0])
	}
}

func TestRecentAvgPaceRequiresMinimumSessions(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	today := now.Format("2006-01-02")

	for i := 0; i < 2; i++ {
		id, err := s.StartSession(now)
		if err != nil {
			t.Fatalf("StartSession: %v", err)
		}
		if _, _, err := s.FinishSession(id, now, 10, 1.0, 1, today); err != nil {
			t.Fatalf("FinishSession: %v", err)
		}
		if err := s.RecordSessionPace(id, 30, 0); err != nil {
			t.Fatalf("RecordSessionPace: %v", err)
		}
	}

	_, ok, err := s.RecentAvgPace(10)
	if err != nil {
		t.Fatalf("RecentAvgPace: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false with fewer than 3 recorded sessions")
	}
}

func TestRecentAvgPaceAveragesLastNSessions(t *testing.T) {
	s := openTestStore(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	today := base.Format("2006-01-02")

	paces := []float64{10, 20, 30, 40, 100}
	for i, p := range paces {
		startedAt := base.Add(time.Duration(i) * time.Hour)
		id, err := s.StartSession(startedAt)
		if err != nil {
			t.Fatalf("StartSession: %v", err)
		}
		if _, _, err := s.FinishSession(id, startedAt, 10, 1.0, 1, today); err != nil {
			t.Fatalf("FinishSession: %v", err)
		}
		if err := s.RecordSessionPace(id, p, 0); err != nil {
			t.Fatalf("RecordSessionPace: %v", err)
		}
	}

	avg, ok, err := s.RecentAvgPace(3)
	if err != nil {
		t.Fatalf("RecentAvgPace: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true with 5 recorded sessions")
	}
	// Last 3 by started_at desc: 100, 40, 30.
	want := (100.0 + 40.0 + 30.0) / 3.0
	if avg != want {
		t.Fatalf("expected avg %v, got %v", want, avg)
	}
}

func TestRecentAvgPaceExcludesSessionsWithoutRecordedPace(t *testing.T) {
	s := openTestStore(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	today := base.Format("2006-01-02")

	for i, p := range []float64{10, 20, 30} {
		startedAt := base.Add(time.Duration(i) * time.Hour)
		id, err := s.StartSession(startedAt)
		if err != nil {
			t.Fatalf("StartSession: %v", err)
		}
		if _, _, err := s.FinishSession(id, startedAt, 10, 1.0, 1, today); err != nil {
			t.Fatalf("FinishSession: %v", err)
		}
		if err := s.RecordSessionPace(id, p, 0); err != nil {
			t.Fatalf("RecordSessionPace: %v", err)
		}
	}

	// A more recent session with no recorded pace (e.g. from before this
	// feature shipped) must not pull the baseline toward zero.
	later := base.Add(3 * time.Hour)
	id, err := s.StartSession(later)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if _, _, err := s.FinishSession(id, later, 10, 1.0, 1, today); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}

	avg, ok, err := s.RecentAvgPace(10)
	if err != nil {
		t.Fatalf("RecentAvgPace: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true, 3 sessions have recorded pace")
	}
	want := (10.0 + 20.0 + 30.0) / 3.0
	if avg != want {
		t.Fatalf("expected avg %v (unrecorded session excluded), got %v", want, avg)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/store/... -v`
Expected: FAIL to compile — `RecordSessionPace`, `RecentAvgPace`, `SessionRecord.AvgPaceWPM`, `SessionRecord.PeakIntensityRatio` all undefined.

- [ ] **Step 3: Add the schema migration**

In `internal/store/store.go`, add `"strings"` to the import block:

```go
import (
	"database/sql"
	"strings"

	_ "modernc.org/sqlite"
)
```

Change `Open` from:

```go
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}
```

to:

```go
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// migrate adds columns introduced after the initial schema. SQLite's ALTER
// TABLE has no "IF NOT EXISTS", so a repeat run is expected to fail with
// "duplicate column name" on an already-migrated file — that specific error
// is swallowed; any other error is not.
func migrate(db *sql.DB) error {
	stmts := []string{
		`ALTER TABLE sessions ADD COLUMN avg_pace_wpm REAL`,
		`ALTER TABLE sessions ADD COLUMN peak_intensity_ratio REAL`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			if strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Add SessionRecord fields, RecordSessionPace, and RecentAvgPace**

In `internal/store/sessions.go`, change `SessionRecord` from:

```go
type SessionRecord struct {
	ID           int64
	StartedAt    string
	SessionScore int
	WordCount    int
}
```

to:

```go
type SessionRecord struct {
	ID                 int64
	StartedAt          string
	SessionScore       int
	WordCount          int
	AvgPaceWPM         float64
	PeakIntensityRatio float64
}
```

Add these two methods anywhere after `FinishSession` (e.g. right before `ComputeStreak`):

```go
// RecordSessionPace persists a finished session's own average pace and the
// peak intensity ratio reached during it. Kept separate from FinishSession
// (rather than growing its parameter list) so the many existing
// FinishSession call sites across the test suite are unaffected — they
// simply leave these columns NULL, which RecentAvgPace and SearchSessions
// already treat as "no data recorded."
func (s *Store) RecordSessionPace(sessionID int64, avgPaceWPM, peakIntensityRatio float64) error {
	_, err := s.db.Exec(
		`UPDATE sessions SET avg_pace_wpm = ?, peak_intensity_ratio = ? WHERE id = ?`,
		avgPaceWPM, peakIntensityRatio, sessionID,
	)
	return err
}

const minBaselineSessions = 3

// RecentAvgPace averages avg_pace_wpm over the last n finished sessions
// that have it set (older sessions, from before pace tracking existed,
// have it NULL and are excluded). ok is false when fewer than
// minBaselineSessions such sessions exist — too little history for a
// meaningful personal baseline.
func (s *Store) RecentAvgPace(n int) (avgWPM float64, ok bool, err error) {
	rows, err := s.db.Query(`
		SELECT avg_pace_wpm FROM sessions
		WHERE ended_at IS NOT NULL AND avg_pace_wpm IS NOT NULL
		ORDER BY started_at DESC
		LIMIT ?`, n)
	if err != nil {
		return 0, false, err
	}
	defer rows.Close()

	var sum float64
	var count int
	for rows.Next() {
		var v float64
		if err := rows.Scan(&v); err != nil {
			return 0, false, err
		}
		sum += v
		count++
	}
	if err := rows.Err(); err != nil {
		return 0, false, err
	}
	if count < minBaselineSessions {
		return 0, false, nil
	}
	return sum / float64(count), true, nil
}
```

Change `SearchSessions` from:

```go
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
```

to:

```go
func (s *Store) SearchSessions(query string, limit, offset int) ([]SessionSearchResult, int, error) {
	rows, err := s.db.Query(`
		SELECT
			s.id,
			s.started_at,
			s.session_score,
			COALESCE((SELECT SUM(e.word_count) FROM entries e WHERE e.session_id = s.id), 0),
			s.avg_pace_wpm,
			s.peak_intensity_ratio,
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
		var avgPace, peakRatio sql.NullFloat64
		var snippet sql.NullString
		if err := rows.Scan(&r.ID, &r.StartedAt, &r.SessionScore, &r.WordCount, &avgPace, &peakRatio, &snippet); err != nil {
			return nil, 0, err
		}
		r.AvgPaceWPM = avgPace.Float64
		r.PeakIntensityRatio = peakRatio.Float64
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
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go build ./... && go test ./internal/store/... -v`
Expected: PASS on all tests, including the 6 new ones and every pre-existing store test.

- [ ] **Step 6: Commit**

```bash
git add internal/store/store.go internal/store/sessions.go internal/store/store_test.go
git commit -m "Add pace persistence and personal-baseline query to the store layer"
```

---

### Task 3: Writing screen — live tracking and end-of-session persistence

**Files:**
- Modify: `internal/tui/writing.go`
- Modify: `internal/tui/summary.go` (struct field only — display is added in Task 4)
- Modify: `internal/tui/writing_test.go`

**Interfaces:**
- Consumes: `scoring.PaceTracker`, `scoring.IntensityTier`, `Session.Pace` from Task 1; `store.RecentAvgPace`, `store.RecordSessionPace` from Task 2.
- Produces: `writingState` fields `baselineWPM float64`, `hasBaseline bool`, `intensityRatio float64`, `peakIntensityRatio float64`; `summaryState` field `peakIntensityRatio float64` (consumed by Task 4's `viewSummary`); const `baselinePaceSessionWindow = 10`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/writing_test.go` (after `TestRenderComboBarAtCapIsFull`):

```go
func TestStartWritingSessionFetchesBaseline(t *testing.T) {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	today := base.Format("2006-01-02")
	for i, pace := range []float64{20, 30, 40} {
		startedAt := base.Add(time.Duration(i) * time.Hour)
		id, err := s.StartSession(startedAt)
		if err != nil {
			t.Fatalf("StartSession: %v", err)
		}
		if _, _, err := s.FinishSession(id, startedAt, 10, 1.0, 1, today); err != nil {
			t.Fatalf("FinishSession: %v", err)
		}
		if err := s.RecordSessionPace(id, pace, 0); err != nil {
			t.Fatalf("RecordSessionPace: %v", err)
		}
	}

	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	updated, _ := m.startWritingSession()
	m = updated.(Model)

	if !m.writing.hasBaseline {
		t.Fatal("expected a baseline after 3 recorded sessions")
	}
	if want := (20.0 + 30.0 + 40.0) / 3.0; m.writing.baselineWPM != want {
		t.Fatalf("expected baseline %v, got %v", want, m.writing.baselineWPM)
	}
}

func TestComboTickUpdatesIntensityAndTracksPeak(t *testing.T) {
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

	updated, _ := m.startWritingSession()
	m = updated.(Model)
	m.writing.hasBaseline = true
	m.writing.baselineWPM = 10

	now := time.Now()
	m.writing.session.CompleteWord(now)
	m.writing.session.CompleteWord(now.Add(1 * time.Second))

	// 2 words 1s apart floors to a 5s window: 24 WPM / 10 baseline = 2.4x.
	tickTime := now.Add(1 * time.Second)
	updated, _ = m.updateWriting(comboTickMsg(tickTime))
	m = updated.(Model)

	if got := m.writing.intensityRatio; got != 2.4 {
		t.Fatalf("expected intensity ratio 2.4, got %v", got)
	}
	if got := m.writing.peakIntensityRatio; got != 2.4 {
		t.Fatalf("expected peak ratio 2.4, got %v", got)
	}

	// A later tick over the same (unchanged) event window yields the same
	// ratio, and the tracked peak must not have decreased.
	updated, _ = m.updateWriting(comboTickMsg(tickTime))
	m = updated.(Model)
	if got := m.writing.peakIntensityRatio; got != 2.4 {
		t.Fatalf("expected peak ratio to remain 2.4, got %v", got)
	}
}

func TestViewWritingShowsTierTagWhenElevated(t *testing.T) {
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
	updated, _ := m.startWritingSession()
	m = updated.(Model)

	m.writing.intensityRatio = scoring.IntensityIntenseRatio
	if got := m.viewWriting(); !strings.Contains(got, "Intense") {
		t.Fatalf("expected view to show the Intense tier tag, got %q", got)
	}
}

func TestViewWritingHidesTierTagAtNormalPace(t *testing.T) {
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
	updated, _ := m.startWritingSession()
	m = updated.(Model)

	got := m.viewWriting()
	if strings.Contains(got, "Focused") || strings.Contains(got, "Intense") || strings.Contains(got, "Frantic") {
		t.Fatalf("expected no tier tag at normal pace, got %q", got)
	}
}

func TestEndWritingSessionRecordsPace(t *testing.T) {
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

	updated, _ := m.startWritingSession()
	m = updated.(Model)
	m.writing.peakIntensityRatio = 2.1

	now := time.Now()
	m.writing.textarea.SetValue("hello world")
	m.writing.lastWordCount = syncWordCount(m.writing.session, m.writing.lastWordCount, m.writing.textarea.Value(), now)

	updated, _ = m.endWritingSession()
	m = updated.(Model)

	if m.summary.peakIntensityRatio != 2.1 {
		t.Fatalf("expected summary peak ratio 2.1, got %v", m.summary.peakIntensityRatio)
	}

	results, _, err := s.SearchSessions("", 10, 0)
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 finished session, got %d", len(results))
	}
	if results[0].PeakIntensityRatio != 2.1 {
		t.Fatalf("expected persisted peak ratio 2.1, got %v", results[0].PeakIntensityRatio)
	}
	if results[0].AvgPaceWPM <= 0 {
		t.Fatalf("expected a positive recorded avg pace, got %v", results[0].AvgPaceWPM)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/... -v`
Expected: FAIL to compile — `m.writing.hasBaseline`, `.baselineWPM`, `.intensityRatio`, `.peakIntensityRatio`, and `m.summary.peakIntensityRatio` all undefined.

- [ ] **Step 3: Add the writingState and summaryState fields**

In `internal/tui/writing.go`, change the `writingState` struct from:

```go
type writingState struct {
	textarea      textarea.Model
	session       *scoring.Session
	lastWordCount int
	sessionID     int64
	startedAt     time.Time
	streakDays    int
	entryDate     string
	pasteWarning  string
}
```

to:

```go
type writingState struct {
	textarea      textarea.Model
	session       *scoring.Session
	lastWordCount int
	sessionID     int64
	startedAt     time.Time
	streakDays    int
	entryDate     string
	pasteWarning  string

	baselineWPM        float64
	hasBaseline        bool
	intensityRatio     float64
	peakIntensityRatio float64
}
```

Add a new constant alongside the existing `writingMaxWidth`/etc. block:

```go
const baselinePaceSessionWindow = 10
```

In `internal/tui/summary.go`, change the `summaryState` struct from:

```go
type summaryState struct {
	rawScore   int
	finalScore int
	bonus      float64
	totalWords int
	isNewHigh  bool
}
```

to:

```go
type summaryState struct {
	rawScore           int
	finalScore         int
	bonus              float64
	totalWords         int
	isNewHigh          bool
	peakIntensityRatio float64
}
```

- [ ] **Step 4: Fetch the baseline in startWritingSession**

In `internal/tui/writing.go`, change `startWritingSession` from:

```go
func (m Model) startWritingSession() (tea.Model, tea.Cmd) {
	now := time.Now()
	today := now.Format("2006-01-02")
	newStreak := store.ComputeStreak(m.stats.LastEntryDate, today, m.stats.CurrentStreak)

	sessionID, err := m.store.StartSession(now)
	if err != nil {
		m.err = err
		return m, nil
	}

	ta := textarea.New()
	ta.Placeholder = "Start writing..."
	ta.ShowLineNumbers = false
	// Prompt defaults to a 2-column "┃ " gutter that SetWidth reserves out
	// of the content width. Clearing it removes that reservation so the
	// textarea's content width matches writingDimensions exactly, and
	// reclaims those columns for actual writing space.
	ta.Prompt = ""
	w, h := writingDimensions(m.width, m.height, m.compactMode)
	ta.SetWidth(w)
	ta.SetHeight(h)
	focusCmd := ta.Focus()

	m.writing = writingState{
		textarea:   ta,
		session:    scoring.NewSession(now),
		sessionID:  sessionID,
		startedAt:  now,
		streakDays: newStreak,
		entryDate:  today,
	}
	m.screen = screenWriting
	return m, tea.Batch(focusCmd, comboTick())
}
```

to:

```go
func (m Model) startWritingSession() (tea.Model, tea.Cmd) {
	now := time.Now()
	today := now.Format("2006-01-02")
	newStreak := store.ComputeStreak(m.stats.LastEntryDate, today, m.stats.CurrentStreak)

	sessionID, err := m.store.StartSession(now)
	if err != nil {
		m.err = err
		return m, nil
	}

	baselineWPM, hasBaseline, err := m.store.RecentAvgPace(baselinePaceSessionWindow)
	if err != nil {
		m.err = err
		return m, nil
	}

	ta := textarea.New()
	ta.Placeholder = "Start writing..."
	ta.ShowLineNumbers = false
	// Prompt defaults to a 2-column "┃ " gutter that SetWidth reserves out
	// of the content width. Clearing it removes that reservation so the
	// textarea's content width matches writingDimensions exactly, and
	// reclaims those columns for actual writing space.
	ta.Prompt = ""
	w, h := writingDimensions(m.width, m.height, m.compactMode)
	ta.SetWidth(w)
	ta.SetHeight(h)
	focusCmd := ta.Focus()

	m.writing = writingState{
		textarea:    ta,
		session:     scoring.NewSession(now),
		sessionID:   sessionID,
		startedAt:   now,
		streakDays:  newStreak,
		entryDate:   today,
		baselineWPM: baselineWPM,
		hasBaseline: hasBaseline,
	}
	m.screen = screenWriting
	return m, tea.Batch(focusCmd, comboTick())
}
```

- [ ] **Step 5: Track live intensity on each combo tick**

In `internal/tui/writing.go`, change the start of `updateWriting` from:

```go
func (m Model) updateWriting(msg tea.Msg) (tea.Model, tea.Cmd) {
	if tickMsg, ok := msg.(comboTickMsg); ok {
		m.writing.session.Combo.Tick(time.Time(tickMsg))
		return m, comboTick()
	}
```

to:

```go
func (m Model) updateWriting(msg tea.Msg) (tea.Model, tea.Cmd) {
	if tickMsg, ok := msg.(comboTickMsg); ok {
		now := time.Time(tickMsg)
		m.writing.session.Combo.Tick(now)
		if m.writing.hasBaseline {
			m.writing.intensityRatio = m.writing.session.Pace.WPM(now) / m.writing.baselineWPM
			if m.writing.intensityRatio > m.writing.peakIntensityRatio {
				m.writing.peakIntensityRatio = m.writing.intensityRatio
			}
		}
		return m, comboTick()
	}
```

- [ ] **Step 6: Show the tier tag in the writing header**

In `internal/tui/writing.go`, change `viewWriting` from:

```go
func (m Model) viewWriting() string {
	combo := m.writing.session.Combo
	header := fmt.Sprintf(
		"Score: %s   Words: %s   %s",
		formatNumber(m.writing.session.RawScore()),
		formatNumber(m.writing.session.TotalWords()),
		renderComboBar(combo.Multiplier, 20),
	)
	help := statStyle.Render("ctrl+n: new entry   ctrl+t: toggle size   esc: end session")
	view := titleStyle.Render(header) + "\n\n" + m.writing.textarea.View() + "\n\n" + help
	if m.writing.pasteWarning != "" {
		view += "\n" + errorStyle.Render(m.writing.pasteWarning)
	}
	return view
}
```

to:

```go
func (m Model) viewWriting() string {
	combo := m.writing.session.Combo
	header := fmt.Sprintf(
		"Score: %s   Words: %s   %s",
		formatNumber(m.writing.session.RawScore()),
		formatNumber(m.writing.session.TotalWords()),
		renderComboBar(combo.Multiplier, 20),
	)
	if tier := scoring.IntensityTier(m.writing.intensityRatio); tier != "" {
		header += "   " + tier
	}
	help := statStyle.Render("ctrl+n: new entry   ctrl+t: toggle size   esc: end session")
	view := titleStyle.Render(header) + "\n\n" + m.writing.textarea.View() + "\n\n" + help
	if m.writing.pasteWarning != "" {
		view += "\n" + errorStyle.Render(m.writing.pasteWarning)
	}
	return view
}
```

- [ ] **Step 7: Record pace at session end**

In `internal/tui/writing.go`, change `endWritingSession` from:

```go
func (m Model) endWritingSession() (tea.Model, tea.Cmd) {
	if body, words, ok := m.writing.finalizeCurrentEntry(); ok {
		if err := m.store.SaveEntry(m.writing.sessionID, time.Now(), body, words); err != nil {
			m.err = err
		}
	}

	totalWords := m.writing.session.TotalWords()
	if totalWords == 0 {
		m.screen = screenHome
		m.homeCursor = 0
		return m, nil
	}

	raw := m.writing.session.RawScore()
	bonus := scoring.StreakBonus(m.writing.streakDays)
	final := scoring.FinalScore(raw, m.writing.streakDays)

	stats, isNewHigh, err := m.store.FinishSession(m.writing.sessionID, time.Now(), final, bonus, m.writing.streakDays, m.writing.entryDate)
	if err != nil {
		m.err = err
	} else {
		m.stats = stats
	}

	m.summary = summaryState{
		rawScore:   raw,
		finalScore: final,
		bonus:      bonus,
		totalWords: totalWords,
		isNewHigh:  isNewHigh,
	}
	m.screen = screenSummary
	return m, nil
}
```

to:

```go
func (m Model) endWritingSession() (tea.Model, tea.Cmd) {
	if body, words, ok := m.writing.finalizeCurrentEntry(); ok {
		if err := m.store.SaveEntry(m.writing.sessionID, time.Now(), body, words); err != nil {
			m.err = err
		}
	}

	totalWords := m.writing.session.TotalWords()
	if totalWords == 0 {
		m.screen = screenHome
		m.homeCursor = 0
		return m, nil
	}

	raw := m.writing.session.RawScore()
	bonus := scoring.StreakBonus(m.writing.streakDays)
	final := scoring.FinalScore(raw, m.writing.streakDays)

	stats, isNewHigh, err := m.store.FinishSession(m.writing.sessionID, time.Now(), final, bonus, m.writing.streakDays, m.writing.entryDate)
	if err != nil {
		m.err = err
	} else {
		m.stats = stats

		avgPaceWPM := 0.0
		if duration := time.Since(m.writing.startedAt).Minutes(); duration > 0 {
			avgPaceWPM = float64(totalWords) / duration
		}
		if err := m.store.RecordSessionPace(m.writing.sessionID, avgPaceWPM, m.writing.peakIntensityRatio); err != nil {
			m.err = err
		}
	}

	m.summary = summaryState{
		rawScore:           raw,
		finalScore:         final,
		bonus:              bonus,
		totalWords:         totalWords,
		isNewHigh:          isNewHigh,
		peakIntensityRatio: m.writing.peakIntensityRatio,
	}
	m.screen = screenSummary
	return m, nil
}
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `go build ./... && go vet ./... && go test ./... -v`
Expected: PASS on the full suite, including the 5 new tests in `writing_test.go` and every pre-existing test.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/writing.go internal/tui/summary.go internal/tui/writing_test.go
git commit -m "Track live writing intensity and persist each session's peak"
```

---

### Task 4: Display the tier on Summary, History, and Read

**Files:**
- Modify: `internal/tui/format.go`
- Modify: `internal/tui/format_test.go`
- Modify: `internal/tui/summary.go`
- Create: `internal/tui/summary_test.go`
- Modify: `internal/tui/history.go`
- Modify: `internal/tui/history_test.go`
- Modify: `internal/tui/read.go`
- Modify: `internal/tui/read_test.go`

**Interfaces:**
- Consumes: `scoring.IntensityTier` from Task 1; `SessionRecord.PeakIntensityRatio` from Task 2; `summaryState.peakIntensityRatio` from Task 3.
- Produces: `formatIntensityTag(peakRatio float64) string` in `format.go`, used by both `history.go` and `read.go`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/format_test.go` (after `TestFormatCountPluralizes`):

```go
func TestFormatIntensityTagEmptyAtNormalPace(t *testing.T) {
	if got := formatIntensityTag(0); got != "" {
		t.Fatalf("expected empty tag at normal pace, got %q", got)
	}
}

func TestFormatIntensityTagShowsTier(t *testing.T) {
	if got := formatIntensityTag(2.1); got != "   · Intense" {
		t.Fatalf("expected intense tag, got %q", got)
	}
}
```

Create `internal/tui/summary_test.go`:

```go
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
```

Add to `internal/tui/history_test.go` (after `TestEnterHistoryLoadsSessions`; add `"strings"` to the import block, which currently has `"path/filepath"`, `"testing"`, `"time"`, `tea`, and `store`):

```go
func TestViewHistoryShowsIntensityTagForElevatedSessions(t *testing.T) {
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
	if err := s.RecordSessionPace(id, 50, 2.1); err != nil {
		t.Fatalf("RecordSessionPace: %v", err)
	}

	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	updated, _ := m.enterHistory()
	m = updated.(Model)

	if got := m.viewHistory(); !strings.Contains(got, "· Intense") {
		t.Fatalf("expected the history line to show the Intense tag, got %q", got)
	}
}
```

Add to `internal/tui/read_test.go` (after `TestViewReadIncludesSessionStats`):

```go
func TestViewReadShowsIntensityTagWhenElevated(t *testing.T) {
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
	if err := s.SaveEntry(id, now, "some text", 2); err != nil {
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

	session := store.SessionSearchResult{SessionRecord: store.SessionRecord{ID: id, StartedAt: "2026-07-15T10:00:00Z", SessionScore: 42, WordCount: 7, PeakIntensityRatio: 2.1}}
	updated, _ := m.enterRead(session)
	m = updated.(Model)

	if got := m.viewRead(); !strings.Contains(got, "· Intense") {
		t.Fatalf("expected view to show the Intense tag, got %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/... -v`
Expected: FAIL to compile — `formatIntensityTag` undefined; `viewSummary`, `viewHistory`, `viewRead` don't yet render any tag/peak-pace text.

- [ ] **Step 3: Add formatIntensityTag**

In `internal/tui/format.go`, add `"github.com/nd28/journal-tui/internal/scoring"` to the import block:

```go
import (
	"strconv"
	"strings"
	"time"

	"github.com/nd28/journal-tui/internal/scoring"
)
```

Add this function at the end of the file:

```go
// formatIntensityTag renders a trailing tag for a session's peak pace
// tier, e.g. "   · Intense" — or "" when the tier is empty (pace was never
// notably elevated, or no personal baseline existed yet for that session).
func formatIntensityTag(peakRatio float64) string {
	tier := scoring.IntensityTier(peakRatio)
	if tier == "" {
		return ""
	}
	return "   · " + tier
}
```

- [ ] **Step 4: Show the peak-pace line in viewSummary**

In `internal/tui/summary.go`, add `"github.com/nd28/journal-tui/internal/scoring"` to the import block:

```go
import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nd28/journal-tui/internal/scoring"
)
```

Change `viewSummary` from:

```go
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
	b.WriteString(statStyle.Render(fmt.Sprintf("Lifetime score: %s", formatNumber(m.stats.LifetimeScore))) + "\n\n")
	b.WriteString(statStyle.Render("enter: back to home") + "\n")
	return b.String()
}
```

to:

```go
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
	if tier := scoring.IntensityTier(m.summary.peakIntensityRatio); tier != "" {
		b.WriteString(statStyle.Render(fmt.Sprintf("Peak pace:      %s (%.1fx your recent average)", tier, m.summary.peakIntensityRatio)) + "\n")
	}
	b.WriteString("\n" + statStyle.Render("enter: back to home") + "\n")
	return b.String()
}
```

- [ ] **Step 5: Show the tag in viewHistory**

In `internal/tui/history.go`, change the result line in `viewHistory` from:

```go
		b.WriteString(cursor + style.Render(fmt.Sprintf(
			"%s   Score: %s   %s",
			formatSessionDate(r.StartedAt),
			formatNumber(r.SessionScore),
			formatCount(r.WordCount, "word", "words"),
		)) + "\n")
```

to:

```go
		b.WriteString(cursor + style.Render(fmt.Sprintf(
			"%s   Score: %s   %s%s",
			formatSessionDate(r.StartedAt),
			formatNumber(r.SessionScore),
			formatCount(r.WordCount, "word", "words"),
			formatIntensityTag(r.PeakIntensityRatio),
		)) + "\n")
```

- [ ] **Step 6: Show the tag in viewRead**

In `internal/tui/read.go`, change the stat line in `viewRead` from:

```go
	b.WriteString(statStyle.Render(fmt.Sprintf(
		"%s   Score: %s   %s",
		formatSessionDate(m.read.session.StartedAt),
		formatNumber(m.read.session.SessionScore),
		formatCount(m.read.session.WordCount, "word", "words"),
	)) + "\n\n")
```

to:

```go
	b.WriteString(statStyle.Render(fmt.Sprintf(
		"%s   Score: %s   %s%s",
		formatSessionDate(m.read.session.StartedAt),
		formatNumber(m.read.session.SessionScore),
		formatCount(m.read.session.WordCount, "word", "words"),
		formatIntensityTag(m.read.session.PeakIntensityRatio),
	)) + "\n\n")
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go build ./... && go vet ./... && go test ./... -v`
Expected: PASS on the full suite (`internal/scoring`, `internal/store`, `internal/tui`), including every new test from all 4 tasks and every pre-existing test.

- [ ] **Step 8: Bump the version footer**

In `internal/tui/model.go`, change `const Version = "0.1.1"` to `const Version = "0.2.0"` (minor bump — this is a new user-facing feature, not a fix).

- [ ] **Step 9: Manual smoke test**

Run: `go run ./cmd/journal`

1. Write and finish at least 3 short sessions at a relaxed pace (so `avg_pace_wpm` gets recorded each time but stays low) — check the Summary screen shows no "Peak pace" line for these.
2. Start a 4th session. Type a burst of words as fast as you can for several seconds. Confirm a tier tag (`Focused`, `Intense`, or `Frantic`) appears next to the combo bar in the header while typing fast, and disappears again once you slow back down.
3. End that session. Confirm the Summary screen shows a `Peak pace:` line naming the highest tier you reached, with a ratio like `(2.1x your recent average)`.
4. Go to History. Confirm that session's row shows a `· <Tier>` tag after the word count, and the earlier calm sessions' rows don't.
5. Press `Enter` on the fast session to open Read. Confirm its stat line also shows the `· <Tier>` tag.
6. Confirm the footer now reads `journal v0.2.0`.

If any step fails, fix the underlying code (not the test) before proceeding.

- [ ] **Step 10: Commit**

```bash
git add internal/tui/format.go internal/tui/format_test.go internal/tui/summary.go internal/tui/summary_test.go internal/tui/history.go internal/tui/history_test.go internal/tui/read.go internal/tui/read_test.go internal/tui/model.go
git commit -m "Show writing-intensity tier on Summary, History, and Read"
```

---

## After this plan

Not in scope here: manual mood tagging, a "Calm"/below-baseline tier, retroactive backfill of pace data for sessions recorded before this feature, and any change to scoring/combo/session-score math (all explicitly out of scope per the design doc).
