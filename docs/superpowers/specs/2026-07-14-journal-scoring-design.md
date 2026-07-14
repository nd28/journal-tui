# Journal — Gamified Writing App (v1 Design)

Date: 2026-07-14

## Purpose

A journaling tool that makes writing addictive the way arcade games are addictive: a
live combo meter that rewards uninterrupted flow, a per-session score you try to beat
(like a Tetris/runner high score), and a lifetime score that only grows. Primary
interface is a terminal TUI; a web app sharing the same backend/storage is a later
phase, out of scope for this spec.

## Architecture

Go module `journal`, laid out as:

```
~/journal/
  cmd/journal/main.go        — entry point, wires store + scoring + TUI, launches Bubble Tea program
  internal/store/            — SQLite persistence (modernc.org/sqlite, pure-Go, no cgo)
  internal/scoring/          — pure scoring engine (no UI/IO deps, unit-testable in isolation)
  internal/tui/              — Bubble Tea models/views
```

TUI stack: Bubble Tea + Lip Gloss (standard, well-supported Go TUI ecosystem — handles
animation/re-render loops needed for the live combo meter).

Storage: SQLite file at `~/.journal/journal.db`. Single file, easy to query for
streaks/history, and reusable by a future web app without changing the data model.

## Data model (SQLite)

- `sessions`: id, started_at, ended_at, session_score, streak_bonus_applied
- `entries`: id, session_id, created_at, body (text), word_count
- `stats`: single row — lifetime_score, high_session_score, current_streak, last_entry_date

Streak and high score are derived facts but cached in `stats` for cheap reads on the
Home screen; recomputed/updated at session end.

## Scoring engine (`internal/scoring`)

Pure structs and functions — `ComboState`, `Session` — with no I/O. The TUI calls into
this package on every keystroke/tick and renders whatever it returns. Fully
unit-testable independent of Bubble Tea.

**Combo meter (live, per-keystroke):**
- Starts at 1.0×, cap 5.0×.
- +0.1× every 3 words typed at a steady pace (gap between words < 1.5s).
- If the gap since the last keystroke exceeds 2s, the combo decays at -0.08×/sec
  (gradual, not a hard reset).
- Full idle for 15s+ resets the combo to the 1.0× floor.
- Decay is computed from real elapsed time (not tick count), so it's correct
  regardless of UI render rate. The UI re-renders the combo bar on a 200ms tick for
  smooth animation.

**Points:** 10 points per completed word (word boundary = whitespace), awarded live,
multiplied by the current combo multiplier at the moment the word completes.

**Streak:** consecutive calendar days with at least one entry. Adds a bonus to that
session's total: +5% per streak day, capped at +50% (10+ day streak).

**Session score** = sum of all combo-weighted word points earned during the session,
times the streak bonus multiplier. This is the "run" — compared against the all-time
best session score (`stats.high_session_score`); a new record triggers a
"NEW HIGH SCORE" banner on the summary screen.

**Lifetime score** = running total of all session scores ever (`stats.lifetime_score`),
shown as a single ever-growing number. No levels/ranks/achievements in v1 — just the
raw number.

## Entry model

A session can contain multiple entries (freeform: `Ctrl+N` starts a new entry within
the same session, like starting a new "level"). Session score is the sum across all
entries in that session. Each entry is stored as its own row with its own timestamp
and word count; the session aggregates them.

## TUI screens

1. **Home** — lifetime score, all-time high session score, current streak, menu:
   "New Session" / "History" / "Quit".
2. **Writing session** — full-screen text area. Top bar shows: live combo meter
   (bar + multiplier), running session score (ticks up live), word count.
   `Ctrl+N` = new entry within this session. `Esc` / `Ctrl+D` = end session.
3. **Session summary** — final session score, high-score banner if beaten, streak
   bonus applied, updated lifetime score. Returns to Home.
4. **History** — simple list of past sessions: date, score, word count.

Entries are persisted to SQLite when a session ends, plus periodically during writing
(e.g. every N seconds or on each new-entry boundary) so a crash mid-session doesn't
lose text.

## Out of scope for v1

- Levels/ranks, achievements/badges, streak freeze — explicitly deferred.
- Web app — later phase, same storage layer, separate spec.
- Any editing/deletion of past entries — v1 is append-only.

## Testing

`internal/scoring` gets unit tests covering: combo buildup, decay-after-pause,
hard-reset-after-15s, streak bonus calculation, and session score aggregation across
multiple entries — since this package is pure and holds all the game-feel logic.
