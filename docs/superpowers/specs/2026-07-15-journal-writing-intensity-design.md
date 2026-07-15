# Journal — Writing Intensity Detection (Design)

Date: 2026-07-15

## Purpose

Typing pace carries signal beyond the score combo: a burst of fast, sustained typing
often reflects a strong emotional state (anger, excitement, urgency) rather than just
"good flow." The existing combo multiplier (`internal/scoring/combo.go`) caps at a
fixed 5.0x for everyone, so it can't tell "this is fast for me" from "this is my normal
pace" — a naturally fast typist hits the cap without anything emotionally notable
happening. This feature adds a second, independent signal: your typing pace compared
against *your own* recent baseline, surfaced as a tiered label ("Focused" / "Intense" /
"Frantic") captured alongside each session so unusually charged writing sessions are
identifiable later, not just felt in the moment.

This is purely a new observational signal. It does not change scoring, the combo
meter, or session score in any way.

## Architecture

New/changed pieces, following the existing package boundaries:

```
internal/scoring/intensity.go   — new: PaceTracker (pure, testable), tier thresholds
internal/store/sessions.go      — changed: 2 new columns, RecentAvgPace() method
internal/tui/writing.go         — changed: live tier tracking + display
internal/tui/summary.go         — changed: peak tier line
internal/tui/history.go         — changed: tier tag on stat line
internal/tui/read.go            — changed: tier tag on stat line
internal/tui/format.go          — changed: shared tier-tag formatting helper
```

## Pace tracking (`internal/scoring/intensity.go`)

A `PaceTracker` records timestamps of completed words in a sliding window, independent
of `ComboState`:

```go
const (
	PaceWindow     = 60 * time.Second
	PaceMinElapsed = 5 * time.Second
)

type PaceTracker struct {
	events []time.Time
}

func (p *PaceTracker) CompleteWord(now time.Time)
func (p *PaceTracker) WPM(now time.Time) float64
```

`CompleteWord` appends `now` and drops events older than `PaceWindow`. `WPM` returns
`len(events) / elapsed-in-minutes`, where `elapsed` is time since the oldest retained
event, floored at `PaceMinElapsed` so the first couple of words in a session don't
produce a spurious spike (e.g. 2 words in 1 real second would otherwise imply 120 WPM).

`Session` (in `session.go`) gains a `Pace PaceTracker` field, fed from the same
`CompleteWord` call that already feeds `Combo` — mirrors how `Combo` is embedded today.

### Tiers

```go
const (
	IntensityFocusedRatio = 1.3
	IntensityIntenseRatio = 1.8
	IntensityFranticRatio = 2.5
)

func IntensityTier(ratio float64) string // "", "Focused", "Intense", or "Frantic"
```

`ratio` is `live WPM / personal baseline WPM`. These threshold values are a starting
point, not derived from measured data — they're plain constants and can be retuned
later without touching callers.

## Baseline (`internal/store`)

Two new nullable columns on `sessions`, added via `ALTER TABLE ... ADD COLUMN`
executed after the existing `CREATE TABLE IF NOT EXISTS` schema (idempotent: a
"duplicate column name" error from a repeat run is ignored, any other error is not):

- `avg_pace_wpm REAL` — the session's own overall pace (total words ÷ wall-clock
  session duration in minutes), written once at `FinishSession`.
- `peak_intensity_ratio REAL` — the highest `live WPM / baseline` ratio observed
  during the session, written once at `FinishSession`.

```go
// RecentAvgPace averages avg_pace_wpm over the last n finished sessions that have
// it set (oldest sessions, from before this feature shipped, have it NULL and are
// excluded). ok is false if fewer than 3 such sessions exist yet — too little
// history for a meaningful baseline, so callers should skip intensity detection
// entirely rather than compare against a noisy average.
func (s *Store) RecentAvgPace(n int) (avgWPM float64, ok bool, err error)

// RecordSessionPace persists the two pace columns for an already-finished
// session. Kept separate from FinishSession (rather than growing its parameter
// list) so the ~20 existing FinishSession call sites across the test suite are
// unaffected — they simply leave these columns NULL, which RecentAvgPace already
// treats as "no data yet."
func (s *Store) RecordSessionPace(sessionID int64, avgPaceWPM, peakIntensityRatio float64) error
```

Called with `n = 10` (last 10 finished sessions). `endWritingSession` calls
`RecordSessionPace` once, immediately after `FinishSession` succeeds.

`SessionRecord` gains `AvgPaceWPM float64` and `PeakIntensityRatio float64` fields;
`SearchSessions`'s query selects the two new columns so History/Read can display them.

## Live behavior while writing (`internal/tui/writing.go`)

- **Session start:** `startWritingSession` calls `m.store.RecentAvgPace(10)`. If `ok`,
  `writingState` stores the baseline; if not, intensity detection is inactive for the
  whole session (no baseline to compare against).
- **Each combo tick (every 200ms):** alongside the existing `Combo.Tick`, feed
  `Session.Pace` and — when a baseline exists — recompute
  `ratio = Pace.WPM(now) / baseline`, tracking the running peak ratio in
  `writingState`.
- **Header display:** the writing header shows a tier tag *only when the current
  tier is non-empty*, e.g.:

  ```
  Score: 1,240   Words: 210   [████████████████░░░░] 4.2x   Intense
  ```

  When pace is below the "Focused" threshold (the common case), the header looks
  exactly as it does today — no added noise.
- **Session end:** `endWritingSession` computes this session's own average pace
  (`total words / wall-clock duration in minutes`) and, after `FinishSession`
  succeeds, calls `RecordSessionPace` with that value and the tracked peak ratio
  (0 if no baseline was available).

## Display on other screens

- **Summary (`summary.go`):** if the session's peak tier is non-empty, add a line
  after the existing stats:
  ```
  Peak pace:      Intense (2.1x your recent average)
  ```
  Omitted entirely when the tier is empty (no baseline, or pace never crossed the
  first threshold).
- **History and Read (`history.go`, `read.go`):** append the tier to the existing
  stat line when non-empty, via a shared helper in `format.go`:
  ```
  Jul 15, 2026 · 10:00 AM   Score: 1,240   210 words   · Intense
  ```

## Out of scope

- No "Calm"/below-baseline tier — this feature only flags unusually *fast* pace.
- No manual mood tagging or emotion naming — labels are auto-inferred and
  emotion-neutral ("Intense", not "Angry").
- No retroactive backfill of `avg_pace_wpm`/`peak_intensity_ratio` for sessions
  recorded before this feature ships — they simply have `NULL` and are excluded from
  baseline calculations.
- No change to scoring, combo meter, or session score.

## Testing

`internal/scoring`: unit tests for `PaceTracker` (window trimming, the
`PaceMinElapsed` floor, WPM calculation with fixed `now` values) and `IntensityTier`
(boundary values at each threshold).

`internal/store`: tests for `RecentAvgPace` covering fewer-than-3-sessions (`ok`
false), exactly-3, more-than-10 (only last 10 counted), and sessions with NULL
`avg_pace_wpm` correctly excluded.

`internal/tui`: existing `writing.go`/`summary.go`/`history.go`/`read.go` tests
extended to cover the tier tag appearing when a baseline + elevated pace are present,
and staying absent otherwise (no baseline, or normal pace).
