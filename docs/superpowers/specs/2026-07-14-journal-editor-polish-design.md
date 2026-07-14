# Journal — Editor Polish (v1.1 Design)

Date: 2026-07-14

## Purpose

Five small, independent improvements to the writing screen and app chrome,
gathered from using v1: the writing screen should use real terminal space
(with an escape hatch back to the old compact size), pasting should be
blocked since this app is about raw, unprocessed, in-the-moment writing, the
Summary screen's word count should be labeled for what it actually is, and
the app should show its version at all times so it's clear what build is
running.

This is a smaller, separate spec from history search/pagination/reading
(tracked separately) — none of these five items touch the store schema or
add new screens.

## 1. Terminal-responsive writing screen, with a compact toggle

**Sizing (Full mode, the default):**
- Width: `min(terminal width, 100)`, minus a 4-column margin so text never
  touches the terminal edge. Floor of 20 (degenerate-terminal safety).
- Height: terminal height minus the fixed chrome the writing screen always
  renders (header line, two blank-line separators, help line, version
  footer line — 5 lines) minus a 2-line safety margin, so nothing is ever
  clipped. Floor of 3 (bubbles/textarea's minimum sane height).

**Compact mode (opt-in):** the textarea reverts to bubbles/textarea's own
built-in default size — width 40, height 6 — i.e. today's existing v1
behavior, unchanged.

**Toggle:** `Ctrl+T` while on the writing screen switches between Full and
Compact for the rest of that run. Always starts in Full on launch; the
choice is in-memory only, not persisted across restarts. (A general TUI
settings/config file is a plausible future need if more preferences show
up, but not built now for a single boolean.)

**Resize handling:** Bubble Tea delivers `tea.WindowSizeMsg` once at
startup and again on every terminal resize. The root `Model` stores the
latest `width`/`height`; the writing screen recomputes and applies its
textarea dimensions from those whenever they change while in Full mode, and
also when a session starts or the mode is toggled.

## 2. Block paste

Bubble Tea's bracketed-paste support (on by default in the installed
version, `github.com/charmbracelet/bubbletea v1.3.10`) tags a pasted
`tea.KeyMsg` with `Paste: true`, distinguishing it from typed keystrokes.
When the writing screen receives a key message with `Paste: true`, it does
not forward it to the textarea (the paste never appears in the text) and
instead sets a short-lived warning — `"paste disabled — write it
yourself"` — rendered inline on the writing screen. The warning clears the
next time a normal (non-paste) key message arrives.

This is best-effort: a small number of terminals don't send bracketed-paste
markers, and on those a paste is indistinguishable from very fast typing
and cannot be blocked. That's an accepted limitation, not something this
spec tries to solve.

## 3. Honest word-count labeling

No logic changes. The Summary screen's `Words written:` label is renamed to
`Words typed:` to make clear it's a typing-effort count — it counts each
word once, the moment you start typing it, and never decreases even if you
later delete it. History's word count is a different, and equally correct,
number: it reflects the actual saved text for each entry, so it does
decrease if you delete before finalizing. The two are allowed to disagree
after edits; the labels should make clear why.

## 4. Version footer

A hand-maintained `const Version = "0.1.0"` (bumped by hand for future
notable changes) is rendered as the last line of every screen — Home,
Writing, Summary, History — as `journal v0.1.0`, styled the same dim gray
as other secondary stats. Lives alongside the screen-agnostic error-banner
logic already in `Model.View()`.

## Out of scope for this spec

- Persisting the compact/full choice across restarts, or any general
  settings/config file.
- History search, pagination, and reading past entries — separate spec.
- Any change to scoring/combo logic.

## Testing

- `internal/tui`: unit tests for the Full/Compact width/height math (pure
  function, testable without a real terminal), for the paste-block/warning
  behavior (constructing a `tea.KeyMsg{Paste: true}` and asserting the
  textarea value is unchanged and the warning is set, then a normal key
  clearing it), and for the `Ctrl+T` toggle transition.
- Manual smoke test via tmux (same approach as the original v1 smoke test)
  covering: resizing the terminal and confirming the textarea grows/shrinks
  and stays within bounds, toggling compact/full, attempting a paste inside
  tmux and confirming it's blocked, and reading the version footer on all
  four screens.
