# Journal — Zen Mode (Design)

Date: 2026-08-18

## Purpose

The app is gamified in six places: the Home stats block, the writing header, the
Summary screen, the score line on each History row, the Read stat line, and the
share text added in v0.7.0. Sometimes that is the point, and sometimes a writer
wants to open the app and write without a combo meter moving in their peripheral
vision.

Zen mode is a persisted, app-wide preference that silences all six. The scoring
machinery keeps running underneath: score, streak and lifetime total accrue
exactly as they do now. Zen changes what is displayed and nothing else.

This is deliberately not a per-session mode. Sharing happens from History, and
"share follows zen" only makes sense if zen is a standing preference rather than
a toggle flipped during a session hours ago.

## Architecture

```
internal/store/store.go   — changed: settings table in the schema const; GetSettings/SetZenMode
internal/tui/model.go     — changed: zen field; central ctrl+z handling; loaded in New
internal/tui/home.go      — changed: hide the stats block; add the zen hint line
internal/tui/writing.go   — changed: drop the header in zen; chrome lines follow
internal/tui/summary.go   — changed: minimal close-out in zen
internal/tui/history.go   — changed: date + snippet only in zen
internal/tui/read.go      — changed: date only in zen
internal/tui/share.go     — changed: minimal share text in zen
```

No changes to `internal/scoring`. Nothing in the store's session, entry, draft or
stats handling changes either — the only store addition is the preference itself.

## Persistence (`internal/store/store.go`)

A singleton settings table joins the `schema` const:

```sql
CREATE TABLE IF NOT EXISTS settings (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	zen_mode INTEGER NOT NULL DEFAULT 0
);

INSERT OR IGNORE INTO settings (id, zen_mode) VALUES (1, 0);
```

**`schemaVersion` stays at 2 — this needs no migration step.** `Open` execs the
whole `schema` const on every launch, and every statement in it is
`CREATE TABLE IF NOT EXISTS` or `INSERT OR IGNORE`, so an existing database picks
the table up the next time it opens. The migration path exists for altering data
that is already stored; there is none to alter here.

The preference is stored as its own table rather than a column on `stats` for two
reasons: `stats` holds score data and a display preference is not that, and
adding a column to an existing table *would* require a migration step.

Two methods, mirroring the shape of `GetStats`:

```go
type Settings struct {
	ZenMode bool
}

func (s *Store) GetSettings() (Settings, error)
func (s *Store) SetZenMode(on bool) error
```

## The toggle (`internal/tui/model.go`)

`Model` gains a `zen bool`, loaded in `New` alongside `GetStats`.

`ctrl+z` is handled centrally in `Model.Update`, before the per-screen dispatch:

```go
if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.Type == tea.KeyCtrlZ {
	m.zen = !m.zen
	if err := m.store.SetZenMode(m.zen); err != nil {
		m.err = err
	}
	return m, nil
}
```

One code path covers every screen. It cannot be swallowed by the History
screen's search-query typing, which only catches `KeyRunes`, and it never reaches
the textarea on the writing screen. Writing through immediately means the
preference survives a crash, consistent with how the rest of the app treats
in-progress state.

`ctrl+z` is free to bind. Bubble Tea puts the terminal in raw mode, and raw mode
clears `ISIG` (`charmbracelet/x/term`'s `term_unix.go`), so the terminal never
turns ctrl+z into SIGTSTP — it arrives as an ordinary key. The same is true of
ctrl+c, which is why every screen in this app already handles it explicitly.

## What each screen shows in zen

**Home** drops the lifetime score, best session and streak lines. The title, the
recovery notice and the menu stay. One dim line shows the state and the way back,
because Home has no help line and is otherwise the only place the preference
would be invisible:

```
zen mode on · ctrl+z to show scores      (when on)
ctrl+z: zen mode                         (when off)
```

The line is present in both states. When zen is off it is the only advertisement
the preference gets on this screen; when zen is on it doubles as the way back.

**Writing** drops the header line entirely — score, words, combo bar, tier word
and pace all go — along with the blank separator beneath the header, since a
blank line where the header used to be is just as much a reminder of it. That is
two of the six lines `writingChromeLines` budgets, so the editor gains two rows
and `writingDimensions` takes the zen flag. The help line stays, including the `saved` / `unsaved` marker: knowing
whether your words reached disk is honesty, not gamification, and the crash-safety
work exists precisely so the writer never has to wonder.

**Summary** becomes a close-out rather than a scorecard:

```
Session complete

You wrote 412 words.

enter: back to home
```

The `*** NEW HIGH SCORE ***` banner does not appear in zen — it is the single
most gamified element in the app, and a close-out that congratulates you on a
score you cannot see would be incoherent.

Word count is a plain fact about what happened, and it gives the session an
ending instead of dropping the writer back at the menu. It is shown here but not
in the writing header on purpose: a total at the end is closure, a number moving
while you type is feedback of the kind zen exists to remove.

**History** rows become the date and the snippet — no score, no word count, no
intensity tag. The pagination line and search behaviour are unchanged.

**Read** shows the date alone in place of the stat line.

**Share** (`renderShareText`) emits the date, a blank line, then the entry text,
keeping the `— entry N —` markers for multi-entry sessions. Score, word count and
intensity tag are omitted. The date stays because a pasted journal excerpt with
no date is hard to place.

## Testing

**Store** (`internal/store/store_test.go`): zen defaults to off in a fresh
database; `SetZenMode` round-trips through `GetSettings`; the setting survives
closing and reopening the store; an existing database created before the settings
table gains it on open.

**TUI** (`internal/tui/zen_test.go`): `ctrl+z` toggles from each screen and
writes through to the store; the toggle is not typed into the History search
query; each of the five views omits its scores when zen is on and shows them when
it is off; `renderShareText` emits the minimal form in zen.

**The decision guard:** a session written start to finish in zen mode still
increases the lifetime score and advances the streak. This is the test that would
catch zen quietly becoming a "doesn't count" mode.

## Not in scope

Per-session zen, a zen flag stored on individual sessions, and any change to what
is recorded in the database. Zen is display-only, and the moment it starts
changing what is stored it stops being reversible — a writer who turns it off
should find their full history intact.
