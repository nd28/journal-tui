# Gamified Journal TUI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go terminal app where journaling earns points — a live combo meter rewards uninterrupted typing flow, a per-session score chases an all-time high score, and a lifetime score grows forever — backed by SQLite.

**Architecture:** `internal/scoring` is a pure, dependency-free package holding all game-feel math (combo decay/bump, streak bonus, score aggregation), fully unit-testable with fake clocks. `internal/store` wraps a SQLite file for entries/sessions/stats. `internal/tui` is a Bubble Tea model that reads/writes through `store` and renders via `scoring`. `cmd/journal/main.go` wires it all together.

**Tech Stack:** Go 1.22, `github.com/charmbracelet/bubbletea`, `github.com/charmbracelet/bubbles` (textarea), `github.com/charmbracelet/lipgloss`, `modernc.org/sqlite` (pure-Go, no cgo).

## Global Constraints

- Module name: `journal`. All internal imports use `journal/internal/...`.
- Database file lives at `~/.journal/journal.db`, created on first run.
- Combo tuning (from spec, do not change without updating the spec): floor 1.0×, cap 5.0×, +0.1× every 3 words at <1.5s/word pace, decay starts after 2s idle at -0.08×/sec, hard reset to floor after 15s idle.
- Scoring: 10 points per completed word × current combo multiplier at word-completion time. Streak bonus: +5%/day, capped at +50% (10+ day streak).
- v1 has no levels/ranks/achievements/streak-freeze — explicitly out of scope per the spec.
- Every package-level time-based function takes `time.Time` as an explicit parameter — never call `time.Now()` inside `internal/scoring` or `internal/store` logic functions, so tests can use fixed fake clocks.

---

### Task 1: Project scaffold

**Files:**
- Create: `go.mod`
- Create: `cmd/journal/main.go`
- Create: `.gitignore`

**Interfaces:**
- Produces: module path `journal`; entry point at `cmd/journal/main.go` (rewritten fully in Task 8).

- [ ] **Step 1: Initialize the module and directory layout**

```bash
mkdir -p ~/journal/cmd/journal ~/journal/internal/scoring ~/journal/internal/store ~/journal/internal/tui
cd ~/journal
go mod init journal
```

Expected: `go.mod` created containing `module journal` and a `go 1.22` directive.

- [ ] **Step 2: Write a placeholder entry point**

`cmd/journal/main.go`:

```go
package main

import "fmt"

func main() {
	fmt.Println("journal")
}
```

- [ ] **Step 3: Verify it builds and runs**

Run: `cd ~/journal && go build ./... && go run ./cmd/journal`
Expected: prints `journal`, no errors.

- [ ] **Step 4: Add .gitignore**

`.gitignore`:

```
/journal
*.db
*.sqlite
```

- [ ] **Step 5: Commit**

```bash
cd ~/journal
git add go.mod cmd/journal/main.go .gitignore
git commit -m "Scaffold Go module and entry point"
```

---

### Task 2: Scoring — combo meter

**Files:**
- Create: `internal/scoring/combo.go`
- Test: `internal/scoring/combo_test.go`

**Interfaces:**
- Produces: `ComboState` struct with exported field `Multiplier float64`; `NewCombo(now time.Time) ComboState`; `(*ComboState) Tick(now time.Time)`; `(*ComboState) CompleteWord(now time.Time) int`; exported consts `ComboFloor`, `ComboCap`, `BasePointsPerWord`; test helper `round2(f float64) float64` (used by later scoring tests in the same package).

- [ ] **Step 1: Write the failing tests**

`internal/scoring/combo_test.go`:

```go
package scoring

import (
	"testing"
	"time"
)

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}

func TestNewComboStartsAtFloor(t *testing.T) {
	c := NewCombo(time.Now())
	if c.Multiplier != ComboFloor {
		t.Fatalf("expected multiplier %v, got %v", ComboFloor, c.Multiplier)
	}
}

func TestCompleteWordBumpsEveryThreeSteadyWords(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewCombo(t0)

	gap := 300 * time.Millisecond
	now := t0
	var lastPoints int
	for i := 0; i < 3; i++ {
		now = now.Add(gap)
		lastPoints = c.CompleteWord(now)
	}

	if lastPoints != 10 {
		t.Fatalf("expected the 3rd word to still score at 1.0x (10 points), got %d", lastPoints)
	}
	if round2(c.Multiplier) != 1.1 {
		t.Fatalf("expected multiplier 1.1 after 3 steady words, got %v", c.Multiplier)
	}

	now = now.Add(gap)
	points := c.CompleteWord(now)
	if points != 11 {
		t.Fatalf("expected the 4th word to score at 1.1x (11 points), got %d", points)
	}
}

func TestCompleteWordCapsAtFive(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewCombo(t0)

	now := t0
	for i := 0; i < 200; i++ {
		now = now.Add(100 * time.Millisecond)
		c.CompleteWord(now)
	}

	if round2(c.Multiplier) != ComboCap {
		t.Fatalf("expected multiplier capped at %v, got %v", ComboCap, c.Multiplier)
	}
}

func TestTickDecaysWithoutMutatingBaseline(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewCombo(t0)

	now := t0
	for i := 0; i < 3; i++ {
		now = now.Add(300 * time.Millisecond)
		c.CompleteWord(now)
	}
	if round2(c.Multiplier) != 1.1 {
		t.Fatalf("setup failed: expected 1.1, got %v", c.Multiplier)
	}

	// 2.5s idle past the last word = 0.5s past the 2s decay-start threshold.
	c.Tick(now.Add(2500 * time.Millisecond))
	if got := round2(c.Multiplier); got != 1.06 {
		t.Fatalf("expected 1.06 after 0.5s of decay, got %v", got)
	}

	// A second Tick at the same instant must recompute from the same baseline, not compound.
	c.Tick(now.Add(2500 * time.Millisecond))
	if got := round2(c.Multiplier); got != 1.06 {
		t.Fatalf("expected Tick to be idempotent for a repeated instant, got %v", got)
	}
}

func TestCompleteWordAfterLongPauseHardResets(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewCombo(t0)

	now := t0
	for i := 0; i < 3; i++ {
		now = now.Add(300 * time.Millisecond)
		c.CompleteWord(now)
	}

	now = now.Add(20 * time.Second)
	c.CompleteWord(now)

	if round2(c.Multiplier) != ComboFloor {
		t.Fatalf("expected hard reset to floor after 20s idle, got %v", c.Multiplier)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/journal && go test ./internal/scoring/...`
Expected: FAIL — `ComboState`, `NewCombo`, `ComboFloor`, etc. undefined.

- [ ] **Step 3: Implement the combo meter**

`internal/scoring/combo.go`:

```go
package scoring

import (
	"math"
	"time"
)

const (
	ComboFloor = 1.0
	ComboCap   = 5.0

	ComboBumpPerWords = 3
	ComboBumpAmount   = 0.1
	ComboBumpMaxGap   = 1500 * time.Millisecond

	ComboDecayStartGap      = 2 * time.Second
	ComboDecayRatePerSecond = 0.08

	ComboHardResetGap = 15 * time.Second

	BasePointsPerWord = 10
)

// ComboState tracks the live combo multiplier. All methods take an explicit
// `now` so behavior is deterministic and testable without a real clock.
type ComboState struct {
	Multiplier float64

	baseMultiplier     float64
	wordsSinceLastBump int
	lastEventTime      time.Time
}

func NewCombo(now time.Time) ComboState {
	return ComboState{
		Multiplier:     ComboFloor,
		baseMultiplier: ComboFloor,
		lastEventTime:  now,
	}
}

func decay(mult float64, gap time.Duration) float64 {
	switch {
	case gap >= ComboHardResetGap:
		return ComboFloor
	case gap > ComboDecayStartGap:
		drained := (gap - ComboDecayStartGap).Seconds() * ComboDecayRatePerSecond
		mult -= drained
		if mult < ComboFloor {
			mult = ComboFloor
		}
		return mult
	default:
		return mult
	}
}

// Tick recomputes the displayed multiplier from idle time, without
// registering a word. Call periodically (e.g. every 200ms) for live UI
// updates; it never mutates the decay baseline, so repeated calls at the
// same instant are idempotent.
func (c *ComboState) Tick(now time.Time) {
	c.Multiplier = decay(c.baseMultiplier, now.Sub(c.lastEventTime))
}

// CompleteWord registers a completed word at time `now`. It applies decay
// for the gap since the last event, awards points at the resulting
// multiplier, then bumps the combo if the pace was steady. Returns the
// points earned for this word.
func (c *ComboState) CompleteWord(now time.Time) int {
	gap := now.Sub(c.lastEventTime)
	effective := decay(c.baseMultiplier, gap)

	points := int(math.Round(BasePointsPerWord * effective))

	if gap <= ComboBumpMaxGap {
		c.wordsSinceLastBump++
		if c.wordsSinceLastBump >= ComboBumpPerWords {
			effective += ComboBumpAmount
			if effective > ComboCap {
				effective = ComboCap
			}
			c.wordsSinceLastBump = 0
		}
	} else {
		c.wordsSinceLastBump = 0
	}

	c.baseMultiplier = effective
	c.Multiplier = effective
	c.lastEventTime = now

	return points
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd ~/journal && go test ./internal/scoring/...`
Expected: PASS, all 5 tests.

- [ ] **Step 5: Commit**

```bash
cd ~/journal
git add internal/scoring/combo.go internal/scoring/combo_test.go
git commit -m "Add combo meter scoring logic"
```

---

### Task 3: Scoring — session aggregation and streak bonus

**Files:**
- Create: `internal/scoring/session.go`
- Test: `internal/scoring/session_test.go`

**Interfaces:**
- Consumes: `ComboState`, `NewCombo(now time.Time) ComboState`, `(*ComboState).CompleteWord(now time.Time) int` from Task 2. Test helper `round2` from `combo_test.go` (same package).
- Produces: `Entry{Words, Points int}`; `Session{Combo ComboState, Entries []Entry}`; `NewSession(now time.Time) *Session`; `(*Session).CompleteWord(now time.Time)`; `(*Session).NewEntry()`; `(*Session).RawScore() int`; `(*Session).TotalWords() int`; `StreakBonus(streakDays int) float64`; `FinalScore(rawScore, streakDays int) int`.

- [ ] **Step 1: Write the failing tests**

`internal/scoring/session_test.go`:

```go
package scoring

import (
	"testing"
	"time"
)

func TestSessionAggregatesAcrossEntries(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := NewSession(t0)

	now := t0
	for i := 0; i < 2; i++ {
		now = now.Add(300 * time.Millisecond)
		s.CompleteWord(now)
	}
	firstEntryScore := s.RawScore()

	s.NewEntry()
	if len(s.Entries) != 1 {
		t.Fatalf("expected 1 finalized entry, got %d", len(s.Entries))
	}
	if s.Entries[0].Words != 2 {
		t.Fatalf("expected 2 words in the first entry, got %d", s.Entries[0].Words)
	}

	now = now.Add(300 * time.Millisecond)
	s.CompleteWord(now)

	if s.TotalWords() != 3 {
		t.Fatalf("expected 3 total words, got %d", s.TotalWords())
	}
	if s.RawScore() <= firstEntryScore {
		t.Fatalf("expected raw score to grow after a 3rd word, got %d (was %d)", s.RawScore(), firstEntryScore)
	}
}

func TestNewEntryIgnoresEmptyTrailingEntry(t *testing.T) {
	s := NewSession(time.Now())
	s.NewEntry() // nothing typed yet
	if len(s.Entries) != 0 {
		t.Fatalf("expected no entries for an empty session, got %d", len(s.Entries))
	}
}

func TestStreakBonus(t *testing.T) {
	cases := []struct {
		days int
		want float64
	}{
		{0, 1.0},
		{1, 1.05},
		{3, 1.15},
		{10, 1.5},
		{20, 1.5},
	}
	for _, c := range cases {
		got := StreakBonus(c.days)
		if round2(got) != c.want {
			t.Fatalf("StreakBonus(%d) = %v, want %v", c.days, got, c.want)
		}
	}
}

func TestFinalScoreAppliesStreakBonus(t *testing.T) {
	got := FinalScore(100, 3)
	if got != 115 {
		t.Fatalf("FinalScore(100, 3) = %d, want 115", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/journal && go test ./internal/scoring/...`
Expected: FAIL — `Session`, `NewSession`, `StreakBonus`, `FinalScore` undefined.

- [ ] **Step 3: Implement session aggregation**

`internal/scoring/session.go`:

```go
package scoring

import (
	"math"
	"time"
)

const (
	StreakBonusPerDay = 0.05
	StreakBonusCap    = 0.50
)

// Entry is a finalized chunk of writing within a session.
type Entry struct {
	Words  int
	Points int
}

// Session tracks one writing "run": a live combo plus zero or more
// finalized entries and the words/points typed since the last one.
type Session struct {
	Combo   ComboState
	Entries []Entry

	currentWords  int
	currentPoints int
}

func NewSession(now time.Time) *Session {
	return &Session{Combo: NewCombo(now)}
}

// CompleteWord registers a completed word in the current (in-progress) entry.
func (s *Session) CompleteWord(now time.Time) {
	points := s.Combo.CompleteWord(now)
	s.currentPoints += points
	s.currentWords++
}

// NewEntry finalizes the current in-progress entry, if it has any words,
// and resets the counters for the next one.
func (s *Session) NewEntry() {
	if s.currentWords == 0 {
		return
	}
	s.Entries = append(s.Entries, Entry{Words: s.currentWords, Points: s.currentPoints})
	s.currentWords = 0
	s.currentPoints = 0
}

// RawScore is the sum of all points earned so far, including the
// in-progress entry, before any streak bonus.
func (s *Session) RawScore() int {
	total := s.currentPoints
	for _, e := range s.Entries {
		total += e.Points
	}
	return total
}

// TotalWords is the word count across all entries, including in-progress.
func (s *Session) TotalWords() int {
	total := s.currentWords
	for _, e := range s.Entries {
		total += e.Words
	}
	return total
}

// StreakBonus returns the multiplier applied to a session's raw score for
// a given consecutive-day streak (e.g. 1.15 for a 3-day streak), capped.
func StreakBonus(streakDays int) float64 {
	bonus := float64(streakDays) * StreakBonusPerDay
	if bonus > StreakBonusCap {
		bonus = StreakBonusCap
	}
	return 1.0 + bonus
}

// FinalScore applies the streak bonus to a raw session score, rounded to
// the nearest integer.
func FinalScore(rawScore int, streakDays int) int {
	return int(math.Round(float64(rawScore) * StreakBonus(streakDays)))
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd ~/journal && go test ./internal/scoring/...`
Expected: PASS, all 9 tests (5 from Task 2 + 4 new).

- [ ] **Step 5: Commit**

```bash
cd ~/journal
git add internal/scoring/session.go internal/scoring/session_test.go
git commit -m "Add session aggregation and streak bonus"
```

---

### Task 4: SQLite store

**Files:**
- Create: `internal/store/store.go`
- Create: `internal/store/sessions.go`
- Test: `internal/store/store_test.go`

**Interfaces:**
- Produces: `Store` type; `Open(path string) (*Store, error)`; `(*Store).Close() error`; `Stats{LifetimeScore, HighSessionScore, CurrentStreak int; LastEntryDate string}`; `(*Store).GetStats() (Stats, error)`; `(*Store).StartSession(now time.Time) (int64, error)`; `(*Store).SaveEntry(sessionID int64, createdAt time.Time, body string, wordCount int) error`; `(*Store).FinishSession(sessionID int64, endedAt time.Time, sessionScore int, streakBonus float64, newStreak int, entryDate string) (Stats, bool, error)`; `SessionRecord{ID int64; StartedAt string; SessionScore int; WordCount int}`; `(*Store).ListSessions(limit int) ([]SessionRecord, error)`; `ComputeStreak(lastEntryDate, today string, currentStreak int) int`.

- [ ] **Step 1: Add the SQLite driver dependency**

```bash
cd ~/journal
go get modernc.org/sqlite
```

Expected: `go.mod`/`go.sum` updated with `modernc.org/sqlite` and its transitive deps.

- [ ] **Step 2: Write the failing tests**

`internal/store/store_test.go`:

```go
package store

import (
	"path/filepath"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenInitializesZeroStats(t *testing.T) {
	s := openTestStore(t)
	stats, err := s.GetStats()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.LifetimeScore != 0 || stats.HighSessionScore != 0 || stats.CurrentStreak != 0 || stats.LastEntryDate != "" {
		t.Fatalf("expected zero-value stats on a fresh store, got %+v", stats)
	}
}

func TestStartSessionAndSaveEntry(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()

	id, err := s.StartSession(now)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if id == 0 {
		t.Fatal("expected a non-zero session id")
	}

	if err := s.SaveEntry(id, now, "hello world", 2); err != nil {
		t.Fatalf("SaveEntry: %v", err)
	}
}

func TestFinishSessionUpdatesStatsAndReportsNewHigh(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	today := now.Format("2006-01-02")

	id, err := s.StartSession(now)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	stats, isNewHigh, err := s.FinishSession(id, now, 100, 1.0, 1, today)
	if err != nil {
		t.Fatalf("FinishSession: %v", err)
	}
	if !isNewHigh {
		t.Fatal("expected the first session to be a new high score")
	}
	if stats.LifetimeScore != 100 || stats.HighSessionScore != 100 || stats.CurrentStreak != 1 || stats.LastEntryDate != today {
		t.Fatalf("unexpected stats after first session: %+v", stats)
	}

	id2, err := s.StartSession(now)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	stats, isNewHigh, err = s.FinishSession(id2, now, 50, 1.0, 1, today)
	if err != nil {
		t.Fatalf("FinishSession: %v", err)
	}
	if isNewHigh {
		t.Fatal("expected the second, lower-scoring session not to be a new high score")
	}
	if stats.LifetimeScore != 150 || stats.HighSessionScore != 100 {
		t.Fatalf("unexpected stats after second session: %+v", stats)
	}
}

func TestListSessionsOrdersMostRecentFirst(t *testing.T) {
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
	if _, _, err := s.FinishSession(id1, base, 20, 1.0, 1, today); err != nil {
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
	if _, _, err := s.FinishSession(id2, later, 30, 1.0, 1, today); err != nil {
		t.Fatalf("FinishSession: %v", err)
	}

	records, err := s.ListSessions(10)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(records))
	}
	if records[0].ID != id2 {
		t.Fatalf("expected the most recent session (%d) first, got %d", id2, records[0].ID)
	}
	if records[0].WordCount != 3 {
		t.Fatalf("expected 3 words for the latest session, got %d", records[0].WordCount)
	}
}

func TestComputeStreakFirstEverEntry(t *testing.T) {
	if got := ComputeStreak("", "2026-07-14", 0); got != 1 {
		t.Fatalf("ComputeStreak first entry = %d, want 1", got)
	}
}

func TestComputeStreakSameDayReturnsCurrentStreak(t *testing.T) {
	if got := ComputeStreak("2026-07-14", "2026-07-14", 3); got != 3 {
		t.Fatalf("ComputeStreak same day = %d, want 3", got)
	}
}

func TestComputeStreakConsecutiveDayIncrements(t *testing.T) {
	if got := ComputeStreak("2026-07-13", "2026-07-14", 3); got != 4 {
		t.Fatalf("ComputeStreak consecutive day = %d, want 4", got)
	}
}

func TestComputeStreakGapResetsToOne(t *testing.T) {
	if got := ComputeStreak("2026-07-10", "2026-07-14", 5); got != 1 {
		t.Fatalf("ComputeStreak after gap = %d, want 1", got)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd ~/journal && go test ./internal/store/...`
Expected: FAIL — package `store` has no `Open`/`Store`/etc.

- [ ] **Step 4: Implement the store**

`internal/store/store.go`:

```go
package store

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS sessions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	started_at TEXT NOT NULL,
	ended_at TEXT,
	session_score INTEGER NOT NULL DEFAULT 0,
	streak_bonus_applied REAL NOT NULL DEFAULT 1.0
);

CREATE TABLE IF NOT EXISTS entries (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id INTEGER NOT NULL REFERENCES sessions(id),
	created_at TEXT NOT NULL,
	body TEXT NOT NULL,
	word_count INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS stats (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	lifetime_score INTEGER NOT NULL DEFAULT 0,
	high_session_score INTEGER NOT NULL DEFAULT 0,
	current_streak INTEGER NOT NULL DEFAULT 0,
	last_entry_date TEXT NOT NULL DEFAULT ''
);

INSERT OR IGNORE INTO stats (id, lifetime_score, high_session_score, current_streak, last_entry_date)
VALUES (1, 0, 0, 0, '');
`

type Store struct {
	db *sql.DB
}

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

func (s *Store) Close() error {
	return s.db.Close()
}

type Stats struct {
	LifetimeScore    int
	HighSessionScore int
	CurrentStreak    int
	LastEntryDate    string
}

func (s *Store) GetStats() (Stats, error) {
	var stats Stats
	row := s.db.QueryRow(`SELECT lifetime_score, high_session_score, current_streak, last_entry_date FROM stats WHERE id = 1`)
	err := row.Scan(&stats.LifetimeScore, &stats.HighSessionScore, &stats.CurrentStreak, &stats.LastEntryDate)
	return stats, err
}
```

`internal/store/sessions.go`:

```go
package store

import "time"

type SessionRecord struct {
	ID           int64
	StartedAt    string
	SessionScore int
	WordCount    int
}

func (s *Store) StartSession(now time.Time) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO sessions (started_at, session_score, streak_bonus_applied) VALUES (?, 0, 1.0)`,
		now.Format(time.RFC3339),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) SaveEntry(sessionID int64, createdAt time.Time, body string, wordCount int) error {
	_, err := s.db.Exec(
		`INSERT INTO entries (session_id, created_at, body, word_count) VALUES (?, ?, ?, ?)`,
		sessionID, createdAt.Format(time.RFC3339), body, wordCount,
	)
	return err
}

// FinishSession records the session's final score, then updates the
// singleton stats row (lifetime score, high score, streak). newStreak and
// entryDate are computed by the caller (typically via ComputeStreak) at
// session start, since they reflect the day the writing happened.
// Returns the updated stats and whether sessionScore is a new all-time high.
func (s *Store) FinishSession(sessionID int64, endedAt time.Time, sessionScore int, streakBonus float64, newStreak int, entryDate string) (Stats, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Stats{}, false, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`UPDATE sessions SET ended_at = ?, session_score = ?, streak_bonus_applied = ? WHERE id = ?`,
		endedAt.Format(time.RFC3339), sessionScore, streakBonus, sessionID,
	); err != nil {
		return Stats{}, false, err
	}

	var stats Stats
	row := tx.QueryRow(`SELECT lifetime_score, high_session_score, current_streak, last_entry_date FROM stats WHERE id = 1`)
	if err := row.Scan(&stats.LifetimeScore, &stats.HighSessionScore, &stats.CurrentStreak, &stats.LastEntryDate); err != nil {
		return Stats{}, false, err
	}

	isNewHigh := sessionScore > stats.HighSessionScore
	stats.LifetimeScore += sessionScore
	if isNewHigh {
		stats.HighSessionScore = sessionScore
	}
	stats.CurrentStreak = newStreak
	stats.LastEntryDate = entryDate

	if _, err := tx.Exec(
		`UPDATE stats SET lifetime_score = ?, high_session_score = ?, current_streak = ?, last_entry_date = ? WHERE id = 1`,
		stats.LifetimeScore, stats.HighSessionScore, stats.CurrentStreak, stats.LastEntryDate,
	); err != nil {
		return Stats{}, false, err
	}

	if err := tx.Commit(); err != nil {
		return Stats{}, false, err
	}
	return stats, isNewHigh, nil
}

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

// ComputeStreak returns the consecutive-day streak for a session written on
// `today`, given the last entry date and streak recorded in stats. Pure and
// DB-free: same day returns the current streak unchanged, the day after
// increments it, any other gap (or no prior entry) restarts it at 1.
func ComputeStreak(lastEntryDate, today string, currentStreak int) int {
	if lastEntryDate == "" {
		return 1
	}
	if lastEntryDate == today {
		return currentStreak
	}
	last, err := time.Parse("2006-01-02", lastEntryDate)
	if err != nil {
		return 1
	}
	t, err := time.Parse("2006-01-02", today)
	if err != nil {
		return 1
	}
	if t.Sub(last) == 24*time.Hour {
		return currentStreak + 1
	}
	return 1
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd ~/journal && go test ./internal/store/...`
Expected: PASS, all 8 tests.

- [ ] **Step 6: Commit**

```bash
cd ~/journal
git add go.mod go.sum internal/store/store.go internal/store/sessions.go internal/store/store_test.go
git commit -m "Add SQLite store for sessions, entries, and stats"
```

---

### Task 5: TUI root model and Home screen

**Files:**
- Create: `internal/tui/model.go`
- Create: `internal/tui/home.go`
- Test: `internal/tui/model_test.go`

**Interfaces:**
- Consumes: `store.Store`, `store.Stats`, `(*store.Store).GetStats() (store.Stats, error)` from Task 4.
- Produces: `Model` type implementing `tea.Model` (`Init`, `Update`, `View`); `New(s *store.Store) (Model, error)`; unexported `screen` enum with values `screenHome, screenWriting, screenSummary, screenHistory`; `(Model).updateHome(msg tea.Msg) (tea.Model, tea.Cmd)`; `(Model).viewHome() string`. The `"New Session"` and `"History"` menu actions are no-ops in this task — Task 6 and Task 7 wire them up.

- [ ] **Step 1: Add Bubble Tea dependencies**

```bash
cd ~/journal
go get github.com/charmbracelet/bubbletea github.com/charmbracelet/lipgloss github.com/charmbracelet/bubbles
```

Expected: `go.mod`/`go.sum` updated.

- [ ] **Step 2: Write the failing tests**

`internal/tui/model_test.go`:

```go
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
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd ~/journal && go test ./internal/tui/...`
Expected: FAIL — package `tui` has no `Model`/`screenHome`/etc.

- [ ] **Step 4: Implement the root model**

`internal/tui/model.go`:

```go
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"journal/internal/store"
)

type screen int

const (
	screenHome screen = iota
	screenWriting
	screenSummary
	screenHistory
)

// Model is the root Bubble Tea model. It holds the current screen plus
// per-screen state, and dispatches Update/View to the active screen.
type Model struct {
	screen screen
	store  *store.Store
	stats  store.Stats

	homeCursor int

	writing writingState
	summary summaryState
	history historyState

	err error
}

func New(s *store.Store) (Model, error) {
	stats, err := s.GetStats()
	if err != nil {
		return Model{}, err
	}
	return Model{screen: screenHome, store: s, stats: stats}, nil
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenHome:
		return m.updateHome(msg)
	case screenWriting:
		return m.updateWriting(msg)
	case screenSummary:
		return m.updateSummary(msg)
	case screenHistory:
		return m.updateHistory(msg)
	}
	return m, nil
}

func (m Model) View() string {
	switch m.screen {
	case screenHome:
		return m.viewHome()
	case screenWriting:
		return m.viewWriting()
	case screenSummary:
		return m.viewSummary()
	case screenHistory:
		return m.viewHistory()
	}
	return ""
}
```

`internal/tui/home.go`:

```go
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	statStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
)

var homeMenuItems = []string{"New Session", "History", "Quit"}

func (m Model) updateHome(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "up", "k":
		if m.homeCursor > 0 {
			m.homeCursor--
		}
	case "down", "j":
		if m.homeCursor < len(homeMenuItems)-1 {
			m.homeCursor++
		}
	case "enter":
		switch homeMenuItems[m.homeCursor] {
		case "New Session":
			return m, nil // wired up in Task 6
		case "History":
			return m, nil // wired up in Task 7
		case "Quit":
			return m, tea.Quit
		}
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) viewHome() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Journal") + "\n\n")
	b.WriteString(statStyle.Render(fmt.Sprintf("Lifetime score: %d", m.stats.LifetimeScore)) + "\n")
	b.WriteString(statStyle.Render(fmt.Sprintf("Best session:   %d", m.stats.HighSessionScore)) + "\n")
	b.WriteString(statStyle.Render(fmt.Sprintf("Streak:         %d days", m.stats.CurrentStreak)) + "\n\n")

	for i, item := range homeMenuItems {
		cursor := "  "
		style := statStyle
		if i == m.homeCursor {
			cursor = "> "
			style = selectedStyle
		}
		b.WriteString(cursor + style.Render(item) + "\n")
	}
	return b.String()
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd ~/journal && go build ./... && go test ./internal/tui/...`
Expected: build succeeds, PASS on all 4 tests.

- [ ] **Step 6: Commit**

```bash
cd ~/journal
git add go.mod go.sum internal/tui/model.go internal/tui/home.go internal/tui/model_test.go
git commit -m "Add TUI root model and Home screen"
```

---

### Task 6: TUI — Writing screen and Summary screen

**Files:**
- Create: `internal/tui/writing.go`
- Create: `internal/tui/summary.go`
- Test: `internal/tui/writing_test.go`
- Modify: `internal/tui/home.go` (wire the `"New Session"` case)

**Interfaces:**
- Consumes: `scoring.NewSession`, `scoring.Session`, `scoring.ComboState`, `scoring.StreakBonus`, `scoring.FinalScore`, `scoring.ComboFloor`, `scoring.ComboCap` from Tasks 2–3; `store.ComputeStreak`, `(*store.Store).StartSession/SaveEntry/FinishSession` from Task 4; `Model`, `screen*` constants from Task 5.
- Produces: `writingState` type; `summaryState` type; `(Model).startWritingSession() (tea.Model, tea.Cmd)`; `(Model).endWritingSession() (tea.Model, tea.Cmd)`; `(Model).updateWriting`, `(Model).viewWriting`, `(Model).updateSummary`, `(Model).viewSummary`; unexported helpers `syncWordCount` and `renderComboBar`.

Bubble Tea's `bubbles/textarea` API surface has drifted slightly across versions. Before implementing, run `go doc github.com/charmbracelet/bubbles/textarea` and confirm the method names below (`New`, `Focus`, `Value`, `SetValue`, `Reset`, `View`, `Update`, and the `Placeholder`/`ShowLineNumbers` fields) match what's installed; adjust names if your installed version differs, keeping behavior identical.

- [ ] **Step 1: Write the failing tests**

`internal/tui/writing_test.go`:

```go
package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"journal/internal/scoring"
	"journal/internal/store"
)

func TestSyncWordCountAwardsPointsForNewWords(t *testing.T) {
	now := time.Now()
	sess := scoring.NewSession(now)
	words := syncWordCount(sess, 0, "hello world", now)
	if words != 2 {
		t.Fatalf("expected 2 words, got %d", words)
	}
	if sess.RawScore() == 0 {
		t.Fatal("expected a non-zero score after completing words")
	}
}

func TestSyncWordCountIgnoresDeletedWords(t *testing.T) {
	now := time.Now()
	sess := scoring.NewSession(now)
	words := syncWordCount(sess, 0, "hello world", now)
	scoreBefore := sess.RawScore()

	words = syncWordCount(sess, words, "hello", now)
	if words != 1 {
		t.Fatalf("expected 1 word after deletion, got %d", words)
	}
	if sess.RawScore() != scoreBefore {
		t.Fatalf("expected score to stay at %d after deletion, got %d", scoreBefore, sess.RawScore())
	}
}

func TestRenderComboBarAtFloorIsEmpty(t *testing.T) {
	bar := renderComboBar(scoring.ComboFloor, 10)
	if strings.Contains(bar, "█") {
		t.Fatalf("expected no filled blocks at floor multiplier, got %q", bar)
	}
}

func TestRenderComboBarAtCapIsFull(t *testing.T) {
	bar := renderComboBar(scoring.ComboCap, 10)
	if strings.Count(bar, "█") != 10 {
		t.Fatalf("expected 10 filled blocks at cap multiplier, got %q", bar)
	}
}

func TestEndWritingSessionPersistsAndUpdatesStats(t *testing.T) {
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
	if m.screen != screenWriting {
		t.Fatalf("expected screenWriting, got %v", m.screen)
	}

	now := time.Now()
	m.writing.textarea.SetValue("hello world this is a test")
	m.writing.lastWordCount = syncWordCount(m.writing.session, m.writing.lastWordCount, m.writing.textarea.Value(), now)

	updated, _ = m.endWritingSession()
	m = updated.(Model)

	if m.screen != screenSummary {
		t.Fatalf("expected screenSummary, got %v", m.screen)
	}
	if m.summary.finalScore == 0 {
		t.Fatal("expected a non-zero final score")
	}
	if m.summary.totalWords != 5 {
		t.Fatalf("expected 5 words, got %d", m.summary.totalWords)
	}
	if m.stats.LifetimeScore != m.summary.finalScore {
		t.Fatalf("expected lifetime score %d to equal session score %d on the first session", m.stats.LifetimeScore, m.summary.finalScore)
	}
	if !m.summary.isNewHigh {
		t.Fatal("expected the first session to be a new high score")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/journal && go test ./internal/tui/...`
Expected: FAIL — `writingState`, `syncWordCount`, `renderComboBar`, `startWritingSession`, `endWritingSession` undefined.

- [ ] **Step 3: Implement the writing screen**

`internal/tui/writing.go`:

```go
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textarea"

	"journal/internal/scoring"
	"journal/internal/store"
)

type writingState struct {
	textarea      textarea.Model
	session       *scoring.Session
	lastWordCount int
	sessionID     int64
	startedAt     time.Time
	streakDays    int
	entryDate     string
}

type comboTickMsg time.Time

func comboTick() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
		return comboTickMsg(t)
	})
}

// syncWordCount reconciles a session's word count with the current text,
// awarding points for each newly completed word. Deletions (a lower word
// count) are not clawed back — points already earned stand — but the
// returned count is still updated so the next call computes the right delta.
func syncWordCount(sess *scoring.Session, prevWords int, text string, now time.Time) int {
	newWords := len(strings.Fields(text))
	for i := 0; i < newWords-prevWords; i++ {
		sess.CompleteWord(now)
	}
	return newWords
}

func renderComboBar(multiplier float64, width int) string {
	span := scoring.ComboCap - scoring.ComboFloor
	filled := int((multiplier - scoring.ComboFloor) / span * float64(width))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	return fmt.Sprintf("%s %.1fx", bar, multiplier)
}

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

// finalizeCurrentEntry closes out the in-progress entry: it finalizes the
// scoring state and clears the textarea for the next entry, and reports the
// just-finished entry's text/word count so the caller can persist it
// immediately (rather than holding it in memory until the session ends).
// ok is false when there was nothing to save (an empty/untouched entry).
func (w *writingState) finalizeCurrentEntry() (body string, wordCount int, ok bool) {
	text := w.textarea.Value()
	words := w.lastWordCount

	w.session.NewEntry()
	w.textarea.Reset()
	w.lastWordCount = 0

	if strings.TrimSpace(text) == "" {
		return "", 0, false
	}
	return text, words, true
}

func (m Model) endWritingSession() (tea.Model, tea.Cmd) {
	if body, words, ok := m.writing.finalizeCurrentEntry(); ok {
		if err := m.store.SaveEntry(m.writing.sessionID, time.Now(), body, words); err != nil {
			m.err = err
		}
	}

	raw := m.writing.session.RawScore()
	bonus := scoring.StreakBonus(m.writing.streakDays)
	final := scoring.FinalScore(raw, m.writing.streakDays)
	totalWords := m.writing.session.TotalWords()

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

func (m Model) updateWriting(msg tea.Msg) (tea.Model, tea.Cmd) {
	if tickMsg, ok := msg.(comboTickMsg); ok {
		m.writing.session.Combo.Tick(time.Time(tickMsg))
		return m, comboTick()
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc", "ctrl+d":
			return m.endWritingSession()
		case "ctrl+n":
			if body, words, ok := m.writing.finalizeCurrentEntry(); ok {
				if err := m.store.SaveEntry(m.writing.sessionID, time.Now(), body, words); err != nil {
					m.err = err
				}
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.writing.textarea, cmd = m.writing.textarea.Update(msg)
	m.writing.lastWordCount = syncWordCount(m.writing.session, m.writing.lastWordCount, m.writing.textarea.Value(), time.Now())
	return m, cmd
}

func (m Model) viewWriting() string {
	combo := m.writing.session.Combo
	header := fmt.Sprintf(
		"Score: %d   Words: %d   %s",
		m.writing.session.RawScore(),
		m.writing.session.TotalWords(),
		renderComboBar(combo.Multiplier, 20),
	)
	help := statStyle.Render("ctrl+n: new entry   esc: end session")
	return titleStyle.Render(header) + "\n\n" + m.writing.textarea.View() + "\n\n" + help
}
```

`internal/tui/summary.go`:

```go
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type summaryState struct {
	rawScore   int
	finalScore int
	bonus      float64
	totalWords int
	isNewHigh  bool
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
	b.WriteString(statStyle.Render(fmt.Sprintf("Words written:  %d", m.summary.totalWords)) + "\n")
	b.WriteString(statStyle.Render(fmt.Sprintf("Raw score:      %d", m.summary.rawScore)) + "\n")
	b.WriteString(statStyle.Render(fmt.Sprintf("Streak bonus:   +%.0f%%", (m.summary.bonus-1)*100)) + "\n")
	b.WriteString(statStyle.Render(fmt.Sprintf("Session score:  %d", m.summary.finalScore)) + "\n")
	b.WriteString(statStyle.Render(fmt.Sprintf("Lifetime score: %d", m.stats.LifetimeScore)) + "\n\n")
	b.WriteString(statStyle.Render("enter: back to home") + "\n")
	return b.String()
}
```

- [ ] **Step 4: Wire Home's "New Session" action**

In `internal/tui/home.go`, replace:

```go
		case "New Session":
			return m, nil // wired up in Task 6
```

with:

```go
		case "New Session":
			return m.startWritingSession()
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd ~/journal && go build ./... && go test ./internal/tui/...`
Expected: build succeeds, PASS on all tests (4 from Task 5 + 5 new).

- [ ] **Step 6: Commit**

```bash
cd ~/journal
git add go.mod go.sum internal/tui/writing.go internal/tui/summary.go internal/tui/writing_test.go internal/tui/home.go
git commit -m "Add writing session and summary screens"
```

---

### Task 7: TUI — History screen

**Files:**
- Create: `internal/tui/history.go`
- Test: `internal/tui/history_test.go`
- Modify: `internal/tui/home.go` (wire the `"History"` case)

**Interfaces:**
- Consumes: `(*store.Store).ListSessions`, `store.SessionRecord` from Task 4; `Model`, `screen*` from Task 5.
- Produces: `historyState` type; `(Model).enterHistory() (tea.Model, tea.Cmd)`; `(Model).updateHistory`, `(Model).viewHistory`.

- [ ] **Step 1: Write the failing test**

`internal/tui/history_test.go`:

```go
package tui

import (
	"path/filepath"
	"testing"
	"time"

	"journal/internal/store"
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
	if len(m.history.sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(m.history.sessions))
	}
	if m.history.sessions[0].SessionScore != 42 {
		t.Fatalf("expected score 42, got %d", m.history.sessions[0].SessionScore)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ~/journal && go test ./internal/tui/...`
Expected: FAIL — `historyState`, `enterHistory` undefined.

- [ ] **Step 3: Implement the history screen**

`internal/tui/history.go`:

```go
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"journal/internal/store"
)

type historyState struct {
	sessions []store.SessionRecord
}

func (m Model) enterHistory() (tea.Model, tea.Cmd) {
	records, err := m.store.ListSessions(20)
	if err != nil {
		m.err = err
		return m, nil
	}
	m.history = historyState{sessions: records}
	m.screen = screenHistory
	return m, nil
}

func (m Model) updateHistory(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch keyMsg.String() {
	case "esc", "enter":
		m.screen = screenHome
		m.homeCursor = 0
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

func (m Model) viewHistory() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("History") + "\n\n")
	if len(m.history.sessions) == 0 {
		b.WriteString(statStyle.Render("No sessions yet.") + "\n")
	}
	for _, s := range m.history.sessions {
		b.WriteString(statStyle.Render(fmt.Sprintf("%s   score %d   %d words", s.StartedAt, s.SessionScore, s.WordCount)) + "\n")
	}
	b.WriteString("\n" + statStyle.Render("esc: back to home") + "\n")
	return b.String()
}
```

- [ ] **Step 4: Wire Home's "History" action**

In `internal/tui/home.go`, replace:

```go
		case "History":
			return m, nil // wired up in Task 7
```

with:

```go
		case "History":
			return m.enterHistory()
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd ~/journal && go build ./... && go test ./internal/tui/...`
Expected: build succeeds, PASS on all tests (9 from Task 6 + 1 new).

- [ ] **Step 6: Commit**

```bash
cd ~/journal
git add internal/tui/history.go internal/tui/history_test.go internal/tui/home.go
git commit -m "Add history screen"
```

---

### Task 8: Wire main.go and manual smoke test

**Files:**
- Modify: `cmd/journal/main.go` (full rewrite)

**Interfaces:**
- Consumes: `store.Open`, `tui.New`, `tea.NewProgram` from all prior tasks.
- Produces: the runnable `journal` binary.

- [ ] **Step 1: Rewrite main.go**

`cmd/journal/main.go`:

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"journal/internal/store"
	"journal/internal/tui"
)

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "journal: could not determine home directory:", err)
		os.Exit(1)
	}

	dbDir := filepath.Join(home, ".journal")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "journal: could not create data directory:", err)
		os.Exit(1)
	}

	s, err := store.Open(filepath.Join(dbDir, "journal.db"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "journal: could not open database:", err)
		os.Exit(1)
	}
	defer s.Close()

	m, err := tui.New(s)
	if err != nil {
		fmt.Fprintln(os.Stderr, "journal: could not initialize app:", err)
		os.Exit(1)
	}

	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "journal: fatal error:", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 2: Build and run the full test suite**

Run: `cd ~/journal && go build ./... && go vet ./... && go test ./...`
Expected: build succeeds, `go vet` reports nothing, all tests across `internal/scoring`, `internal/store`, `internal/tui` pass.

- [ ] **Step 3: Manual smoke test**

Run: `cd ~/journal && go run ./cmd/journal`

Walk through, confirming each behavior:
1. Home screen shows `Lifetime score: 0`, `Best session: 0`, `Streak: 0 days`, and a 3-item menu.
2. Arrow keys / `j`/`k` move the cursor; select "New Session" with Enter.
3. Type a sentence steadily — the combo bar fills in and the multiplier climbs above `1.0x`; the score in the header increases.
4. Pause for a few seconds without typing — the combo bar visibly drains back down.
5. Press `Ctrl+N` — the text area clears for a new entry; keep typing.
6. Press `Esc` — lands on the Summary screen showing word count, raw score, streak bonus, session score, and `*** NEW HIGH SCORE ***` (since this is the first-ever session), plus the updated lifetime score.
7. Press `Enter` — returns to Home; lifetime score, best session, and streak (`1 day`) now reflect the completed session.
8. Select "History" — see the just-completed session listed with its score and word count. Press `Esc` to return home.
9. Select "Quit" — the program exits cleanly.
10. Run `go run ./cmd/journal` again — confirm the Home screen now shows the persisted stats from step 7 (proves `~/.journal/journal.db` persisted across runs).

If any step fails, fix the underlying code (not the test) before proceeding.

- [ ] **Step 4: Commit**

```bash
cd ~/journal
git add cmd/journal/main.go
git commit -m "Wire main entry point to the TUI and SQLite store"
```

---

## After this plan

Not in scope here, per the spec: the web app phase (reusing `internal/store` and `internal/scoring` behind an HTTP/browser frontend), levels/achievements/streak-freeze, and entry editing. Revisit as a separate spec once the TUI loop feels good to use.
