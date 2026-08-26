# journal

A gamified terminal journaling app: a live combo meter rewards uninterrupted
writing flow, a per-session score you try to beat, and a lifetime score that
only grows.

## Install

```
go install github.com/nd28/journal-tui/cmd/journal@latest
```

Requires Go 1.25+. This installs a `journal` binary to `$(go env GOPATH)/bin`
(make sure that's on your `PATH`).

## Run

```
journal
```

Data is stored in a local SQLite file at `~/.journal/journal.db`. Set
`JOURNAL_DB` to keep it somewhere else, or to keep a second journal apart
from the daily one.

## Commands

Bare `journal` opens the app. Everything else works without a terminal, so
scripts and agents can read and write the journal directly:

```
journal add [text]         Record an entry (reads stdin if no text given)
journal list [flags]       List finished sessions, newest first
journal show <id> [flags]  Print one session's entries
journal stats [flags]      Lifetime score, high score, streak
journal version
journal help
```

`--json` switches `list`, `show`, `stats`, and `add` to machine-readable
output; `list` also takes `--limit`, `--offset`, and `--query`. Exit status
is 0 on success, 1 on failure, 2 on a malformed invocation.

```
$ echo "the fog came in on little cat feet" | journal add -
saved session 79 - 8 words (unscored)

$ journal list --json --query fog | jq '.sessions[0].id'
79
```

**Entries added this way score zero, and don't touch the streak.** The
writing screen blocks paste for a reason - the combo multiplier measures
typing rhythm over the last few seconds, and text arriving through a pipe
has no rhythm to measure. There's no honest score to award it, and awarding
one anyway would turn the lifetime score into a number any loop could
inflate. Piped entries are readable in history like any other; they just
don't count toward the game.

## Keys

While writing:

| Key | Does |
| --- | --- |
| `ctrl+n` | Finish the current entry and start a new one in the same session |
| `ctrl+t` | Toggle between full-screen and compact editor size |
| `esc` / `ctrl+d` | End the session and show the summary |
| `ctrl+c` | End the session and quit |

Reading past sessions in History (and in the Read screen):

| Key | Does |
| --- | --- |
| `enter` | Open the highlighted session |
| `ctrl+s` | Copy the session to the clipboard, ready to paste and share |
| `pgup` / `pgdn` | Page through results |
| `esc` | Go back |

A copied session carries its stat header — date, score, word count, intensity —
followed by the entry text, unstyled so it wraps to whatever you paste it into.
On a machine with no clipboard tool (a bare SSH session, say), the copy reports
an error rather than quietly copying nothing.

## Crash safety

The editor autosaves every 5 seconds, and the writing screen says `saved` or
`unsaved` so you never have to guess. If the app is killed mid-session — a
crash, a closed terminal, a battery that runs out — the next launch offers
the session back:

![The home screen offering an unfinished session back](docs/screenshots/recovery-home.png)

Resuming puts the text back in the editor and the score back on the header,
then carries on:

![A resumed session, its text and score restored](docs/screenshots/resumed-session.png) The combo multiplier restarts at 1.0x: it measures the last
few seconds of typing, and the interruption ended those. Sessions that were
opened but never written in are cleaned up silently instead of being offered.

## Develop

```
make build   # build ./journal
make test    # go build + go vet + go test ./...
make run     # build and run
make install # go install ./cmd/journal
```

`JOURNAL_DB` is what makes the CLI testable: point it at a temp file and a
test gets its own journal instead of fighting over the one in `$HOME`.
