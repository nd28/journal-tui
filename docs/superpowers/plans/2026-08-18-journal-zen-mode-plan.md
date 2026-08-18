# Zen Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a persisted, app-wide zen mode that hides every score in the app while the scoring machinery keeps running underneath.

**Architecture:** A singleton `settings` table holds the preference; `Model` carries it as a `zen bool` loaded at startup; `ctrl+z` toggles it centrally in `Model.Update` before the per-screen dispatch. Every screen's `view*` function branches on `m.zen` to omit its scores. Nothing in `internal/scoring` or in how sessions, entries, drafts and stats are stored changes at all.

**Tech Stack:** Go 1.25, bubbletea v1.3.10, bubbles v1.0.0, lipgloss v1.1.0, modernc.org/sqlite.

**Spec:** `docs/superpowers/specs/2026-08-18-journal-zen-mode-design.md`

## Global Constraints

- Zen is display-only. No task may change what is written to the database for a session, entry, draft or stats row.
- `schemaVersion` stays at `2`. The settings table is added to the `schema` const, which `Open` execs on every launch; it needs no migration step.
- Run the full suite with `make test` (that is `go build ./... && go vet ./... && go test ./...`). It must be green before every commit.
- Follow the repo's comment style: comments explain *why*, not *what*. Match the density of the file you are editing.
- The `saved` / `unsaved` marker on the writing help line stays visible in zen mode. It is not gamification.
- Commit messages: a short imperative subject line, a body explaining the reasoning, and the trailer `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`.

---

### Task 1: Store the preference

**Files:**
- Modify: `internal/store/store.go` (the `schema` const at line 14; new methods beside `GetStats` at line 249)
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `store.Settings{ZenMode bool}`, `(*Store).GetSettings() (Settings, error)`, `(*Store).SetZenMode(on bool) error`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/store/store_test.go`:

```go
func TestZenModeDefaultsToOff(t *testing.T) {
	s := openTestStore(t)
	settings, err := s.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if settings.ZenMode {
		t.Fatal("expected zen mode off in a fresh store")
	}
}

func TestSetZenModeRoundTrips(t *testing.T) {
	s := openTestStore(t)
	if err := s.SetZenMode(true); err != nil {
		t.Fatalf("SetZenMode(true): %v", err)
	}
	settings, err := s.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if !settings.ZenMode {
		t.Fatal("expected zen mode on after SetZenMode(true)")
	}

	if err := s.SetZenMode(false); err != nil {
		t.Fatalf("SetZenMode(false): %v", err)
	}
	settings, err = s.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if settings.ZenMode {
		t.Fatal("expected zen mode off after SetZenMode(false)")
	}
}

// The preference is worthless if it does not outlive the process.
func TestZenModeSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.SetZenMode(true); err != nil {
		t.Fatalf("SetZenMode: %v", err)
	}
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()

	settings, err := s.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if !settings.ZenMode {
		t.Fatal("zen mode did not survive reopening the store")
	}
}

// A database written by an older build has no settings table. Open must add
// it rather than failing, which is what makes this change migration-free.
func TestOpenAddsSettingsTableToAnExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE sessions (id INTEGER PRIMARY KEY AUTOINCREMENT, started_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("seed old schema: %v", err)
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open on an existing database: %v", err)
	}
	defer s.Close()

	if _, err := s.GetSettings(); err != nil {
		t.Fatalf("GetSettings on an upgraded database: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/store/ -run "ZenMode|SettingsTable" -v`
Expected: FAIL to build with `undefined: Settings`, `s.GetSettings undefined`, `s.SetZenMode undefined`.

- [ ] **Step 3: Add the table to the schema const**

In `internal/store/store.go`, inside the `schema` const, after the `stats` table and before the `CREATE INDEX` lines:

```sql
CREATE TABLE IF NOT EXISTS settings (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	zen_mode INTEGER NOT NULL DEFAULT 0
);
```

And after the existing `INSERT OR IGNORE INTO stats ...` statement:

```sql
INSERT OR IGNORE INTO settings (id, zen_mode) VALUES (1, 0);
```

- [ ] **Step 4: Add the accessors**

In `internal/store/store.go`, beside `GetStats`:

```go
// Settings holds display preferences, as opposed to the scores in Stats.
// Zen mode hides every score in the app; it never changes what is recorded,
// so turning it off restores a complete history.
type Settings struct {
	ZenMode bool
}

func (s *Store) GetSettings() (Settings, error) {
	var settings Settings
	row := s.db.QueryRow(`SELECT zen_mode FROM settings WHERE id = 1`)
	err := row.Scan(&settings.ZenMode)
	return settings, err
}

func (s *Store) SetZenMode(on bool) error {
	_, err := s.db.Exec(`UPDATE settings SET zen_mode = ? WHERE id = 1`, on)
	return err
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/store/ -run "ZenMode|SettingsTable" -v`
Expected: PASS, all four.

- [ ] **Step 6: Run the full suite**

Run: `make test`
Expected: all packages ok.

- [ ] **Step 7: Commit**

```bash
git add internal/store/store.go internal/store/store_test.go
git commit -m "Store a zen-mode preference beside the scores"
```

---

### Task 2: Toggle it with ctrl+z

**Files:**
- Modify: `internal/tui/model.go` (the `Model` struct, `New`, and `Update`)
- Create: `internal/tui/zen_test.go`

**Interfaces:**
- Consumes: `store.Settings`, `(*Store).GetSettings`, `(*Store).SetZenMode` from Task 1.
- Produces: `Model.zen bool`, set at startup and toggled by `tea.KeyCtrlZ` in `Model.Update`.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/zen_test.go`:

```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestZenStartsOffAndCtrlZTurnsItOn(t *testing.T) {
	s, _ := openStore(t)
	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if m.zen {
		t.Fatal("expected zen off on a fresh store")
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlZ})
	m = updated.(Model)

	if !m.zen {
		t.Fatal("ctrl+z did not turn zen on")
	}
	settings, err := s.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if !settings.ZenMode {
		t.Fatal("ctrl+z did not write the preference through to the store")
	}
}

func TestZenIsLoadedFromTheStoreAtStartup(t *testing.T) {
	s, _ := openStore(t)
	if err := s.SetZenMode(true); err != nil {
		t.Fatalf("SetZenMode: %v", err)
	}

	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !m.zen {
		t.Fatal("expected a stored zen preference to be picked up at startup")
	}
}

// The History screen turns every ordinary rune into a search query. The
// toggle must not land in it.
func TestCtrlZOnHistoryTogglesWithoutTypingIntoTheSearch(t *testing.T) {
	s, _ := openStore(t)
	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	updated, _ := m.enterHistory()
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlZ})
	m = updated.(Model)

	if !m.zen {
		t.Fatal("ctrl+z did not toggle zen from the History screen")
	}
	if m.history.query != "" {
		t.Fatalf("ctrl+z was typed into the search query: %q", m.history.query)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run "Zen" -v`
Expected: FAIL to build with `m.zen undefined`.

- [ ] **Step 3: Add the field and load it**

In `internal/tui/model.go`, add to the `Model` struct beside `compactMode`:

```go
	// zen hides every score in the app. It is a stored preference rather
	// than session state: sharing happens from History, and "share follows
	// zen" only holds together if zen is still on tomorrow.
	zen bool
```

In `New`, after `GetStats` succeeds and before the empty-session sweep:

```go
	settings, err := s.GetSettings()
	if err != nil {
		return Model{}, err
	}
	m := Model{screen: screenHome, store: s, stats: stats, zen: settings.ZenMode}
```

(replacing the existing `m := Model{screen: screenHome, store: s, stats: stats}` line).

- [ ] **Step 4: Handle the key centrally**

In `internal/tui/model.go`, in `Update`, after the error-clearing block and before the `switch m.screen`:

```go
	// Handled here rather than per screen so one code path covers all five,
	// and so it can never reach the textarea or the History search query.
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.Type == tea.KeyCtrlZ {
		m.zen = !m.zen
		if err := m.store.SetZenMode(m.zen); err != nil {
			m.err = err
		}
		return m, nil
	}
```

Task 3 adds an editor resize inside this block. Leave it out for now — this task must compile and pass on its own.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run "Zen" -v`
Expected: PASS, all three.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go internal/tui/zen_test.go
git commit -m "Toggle zen mode with ctrl+z from any screen"
```

---

### Task 3: Quiet the writing screen

**Files:**
- Modify: `internal/tui/writing.go` (`writingChromeLines` comment at line 57, `writingDimensions` at line 74, `newWritingTextarea` at line 215, call sites at lines 195, 274, 470, 529, and `viewWriting` at line 552)
- Test: `internal/tui/zen_test.go`

**Interfaces:**
- Consumes: `Model.zen` from Task 2.
- Produces: `writingDimensions(termWidth, termHeight int, compact, zen bool) (width, height int)` and `newWritingTextarea(termWidth, termHeight int, compact, zen bool) textarea.Model` — both gain a trailing `zen` parameter.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/zen_test.go`:

```go
func TestWritingViewHidesTheScoreHeaderInZen(t *testing.T) {
	s, _ := openStore(t)
	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.width, m.height = 100, 40
	m = startWriting(t, m, "hello world ")

	loud := m.viewWriting()
	if !strings.Contains(loud, "Score:") {
		t.Fatalf("expected the score header outside zen, got:\n%s", loud)
	}

	m.zen = true
	quiet := m.viewWriting()
	for _, banned := range []string{"Score:", "Words:", "WPM"} {
		if strings.Contains(quiet, banned) {
			t.Errorf("zen writing view still shows %q:\n%s", banned, quiet)
		}
	}
	// Crash-safety honesty is not gamification.
	if !strings.Contains(quiet, "saved") {
		t.Errorf("zen writing view dropped the save status:\n%s", quiet)
	}
}

func TestZenGivesTheEditorTheHeaderRows(t *testing.T) {
	_, loudHeight := writingDimensions(100, 40, false, false)
	_, zenHeight := writingDimensions(100, 40, false, true)

	if zenHeight != loudHeight+2 {
		t.Fatalf("zen height %d, want %d (the header line and its blank separator)", zenHeight, loudHeight+2)
	}
}

// Compact mode ignores the terminal size entirely, so zen must not change it.
func TestZenDoesNotChangeCompactDimensions(t *testing.T) {
	w, h := writingDimensions(100, 40, true, true)
	if w != compactWidth || h != compactHeight {
		t.Fatalf("compact zen dimensions = %dx%d, want %dx%d", w, h, compactWidth, compactHeight)
	}
}
```

```go
// Zen changes the writing screen's height budget, so the editor has to be
// resized the moment it is toggled rather than at the next window resize.
func TestTogglingZenWhileWritingResizesTheEditor(t *testing.T) {
	s, _ := openStore(t)
	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.width, m.height = 100, 40
	m = startWriting(t, m, "hello world ")
	before := m.writing.textarea.Height()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlZ})
	m = updated.(Model)

	after := m.writing.textarea.Height()
	if after != before+2 {
		t.Fatalf("editor height went from %d to %d, want %d", before, after, before+2)
	}
}
```

Add `"strings"` to the import block of `zen_test.go`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run "Zen" -v`
Expected: FAIL to build — `not enough arguments in call to writingDimensions`.

- [ ] **Step 3: Thread zen through the dimensions**

In `internal/tui/writing.go`, update the `writingChromeLines` comment to record that zen drops two of the six, then:

```go
func writingDimensions(termWidth, termHeight int, compact, zen bool) (width, height int) {
	if compact {
		return compactWidth, compactHeight
	}

	width = termWidth
	if width > writingMaxWidth {
		width = writingMaxWidth
	}
	width -= writingWidthMargin
	if width < writingMinWidth {
		width = writingMinWidth
	}

	chrome := writingChromeLines
	if zen {
		// The header line and the blank separator beneath it both go: a
		// blank line where the score used to be is its own reminder.
		chrome -= 2
	}

	height = termHeight - chrome - writingHeightMargin
	if height < writingMinHeight {
		height = writingMinHeight
	}

	return width, height
}
```

Give `newWritingTextarea` the same trailing `zen bool` parameter and pass it through to `writingDimensions` at line 235. Update the four call sites to pass `m.zen`: lines 195 and 274 (`newWritingTextarea(m.width, m.height, m.compactMode, m.zen)`) and lines 470 and 529 (`writingDimensions(m.width, m.height, m.compactMode, m.zen)`).

- [ ] **Step 4: Resize the editor when zen is toggled**

In `internal/tui/model.go`, inside the `tea.KeyCtrlZ` block added in Task 2, before `return m, nil`:

```go
		// Zen frees two lines of chrome on the writing screen, so the editor
		// has to be resized now rather than at the next window resize.
		if m.screen == screenWriting {
			w, h := writingDimensions(m.width, m.height, m.compactMode, m.zen)
			m.writing.textarea.SetWidth(w)
			m.writing.textarea.SetHeight(h)
		}
```

- [ ] **Step 5: Drop the header from the view**

In `viewWriting`, wrap the header construction and prepend only when not in zen. The final assembly becomes:

```go
	view := m.writing.textarea.View() + "\n\n" + help
	if !m.zen {
		view = titleStyle.Render(header) + "\n\n" + view
	}
```

Build `header` only in the non-zen branch — computing a header nobody renders would leave a reader wondering what it was for.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run "Zen" -v`
Expected: PASS, including `TestTogglingZenWhileWritingResizesTheEditor`.

- [ ] **Step 7: Run the full suite**

Run: `make test`
Expected: all ok. The existing writing tests call `writingDimensions` — fix their call sites to pass `false` for zen if the compiler flags them.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/model.go internal/tui/writing.go internal/tui/zen_test.go
git commit -m "Drop the score header from the writing screen in zen"
```

---

### Task 4: Quiet Home and Summary

**Files:**
- Modify: `internal/tui/home.go` (`viewHome` at line 69), `internal/tui/summary.go` (`viewSummary` at line 42)
- Test: `internal/tui/zen_test.go`

**Interfaces:**
- Consumes: `Model.zen`.
- Produces: nothing new.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/zen_test.go`:

```go
func TestHomeHidesTheStatsBlockInZen(t *testing.T) {
	s, _ := openStore(t)
	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if !strings.Contains(m.viewHome(), "Lifetime score") {
		t.Fatal("expected the stats block outside zen")
	}

	m.zen = true
	quiet := m.viewHome()
	for _, banned := range []string{"Lifetime score", "Best session", "Streak"} {
		if strings.Contains(quiet, banned) {
			t.Errorf("zen home still shows %q:\n%s", banned, quiet)
		}
	}
	if !strings.Contains(quiet, "New Session") {
		t.Errorf("zen home lost its menu:\n%s", quiet)
	}
}

// Home has no help line, so it is the only place the preference can be
// advertised — and the only way back out of zen if you forget the key.
func TestHomeAdvertisesZenInBothStates(t *testing.T) {
	s, _ := openStore(t)
	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if !strings.Contains(m.viewHome(), "ctrl+z") {
		t.Errorf("home should mention ctrl+z when zen is off:\n%s", m.viewHome())
	}

	m.zen = true
	if !strings.Contains(m.viewHome(), "ctrl+z") {
		t.Errorf("home should mention ctrl+z when zen is on:\n%s", m.viewHome())
	}
	if !strings.Contains(m.viewHome(), "zen mode on") {
		t.Errorf("home should show that zen is on:\n%s", m.viewHome())
	}
}

func TestSummaryIsACloseOutInZen(t *testing.T) {
	s, _ := openStore(t)
	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m.zen = true
	m.summary = summaryState{
		rawScore:   500,
		finalScore: 600,
		bonus:      1.2,
		totalWords: 412,
		isNewHigh:  true,
	}

	view := m.viewSummary()
	for _, banned := range []string{"Raw score", "Session score", "Lifetime score", "Streak bonus", "NEW HIGH SCORE"} {
		if strings.Contains(view, banned) {
			t.Errorf("zen summary still shows %q:\n%s", banned, view)
		}
	}
	if !strings.Contains(view, "412 words") {
		t.Errorf("zen summary should still say what was written:\n%s", view)
	}
	if !strings.Contains(view, "enter") {
		t.Errorf("zen summary lost its way back home:\n%s", view)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run "Zen|Home|Summary" -v`
Expected: FAIL — `zen home still shows "Lifetime score"`, `home should mention ctrl+z`, `zen summary still shows "Raw score"`.

- [ ] **Step 3: Implement the Home view**

In `viewHome`, replace the three unconditional stat lines with:

```go
	if m.zen {
		b.WriteString(statStyle.Render("zen mode on · ctrl+z to show scores") + "\n\n")
	} else {
		b.WriteString(statStyle.Render(fmt.Sprintf("Lifetime score: %s", formatNumber(m.stats.LifetimeScore))) + "\n")
		b.WriteString(statStyle.Render(fmt.Sprintf("Best session:   %s", formatNumber(m.stats.HighSessionScore))) + "\n")
		b.WriteString(statStyle.Render(fmt.Sprintf("Streak:         %s", formatCount(m.stats.CurrentStreak, "day", "days"))) + "\n")
		b.WriteString(statStyle.Render("ctrl+z: zen mode") + "\n\n")
	}
```

- [ ] **Step 4: Implement the Summary view**

At the top of `viewSummary`, before the existing body:

```go
	if m.zen {
		var b strings.Builder
		b.WriteString(titleStyle.Render("Session complete") + "\n\n")
		b.WriteString(statStyle.Render(fmt.Sprintf("You wrote %s.", formatCount(m.summary.totalWords, "word", "words"))) + "\n")
		b.WriteString("\n" + statStyle.Render("enter: back to home") + "\n")
		return b.String()
	}
```

The new-high banner is inside the branch that is skipped, so it cannot appear in zen — congratulating someone on a score they cannot see would be incoherent.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run "Zen|Home|Summary" -v`
Expected: PASS.

- [ ] **Step 6: Run the full suite**

Run: `make test`
Expected: all ok.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/home.go internal/tui/summary.go internal/tui/zen_test.go
git commit -m "Quiet the Home stats block and the Summary scorecard in zen"
```

---

### Task 5: Quiet History, Read and the share text

**Files:**
- Modify: `internal/tui/history.go` (`viewHistory` at line 122), `internal/tui/read.go` (`viewRead` at line 111), `internal/tui/share.go` (`renderShareText`, and `shareSession` which calls it)
- Test: `internal/tui/zen_test.go`

**Interfaces:**
- Consumes: `Model.zen`.
- Produces: `renderShareText(session store.SessionSearchResult, entries []store.EntryRecord, zen bool) string` — the existing function gains a trailing `zen` parameter.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/zen_test.go` (`fakeClipboard`, `finishedSession` and `testSession` already exist in `share_test.go` in the same package):

```go
func TestHistoryRowsHideScoresInZen(t *testing.T) {
	m := finishedSession(t, "hello world")
	if !strings.Contains(m.viewHistory(), "Score:") {
		t.Fatal("expected scores on History outside zen")
	}

	m.zen = true
	quiet := m.viewHistory()
	for _, banned := range []string{"Score:", "words"} {
		if strings.Contains(quiet, banned) {
			t.Errorf("zen History still shows %q:\n%s", banned, quiet)
		}
	}
	if !strings.Contains(quiet, formatSessionDate(m.history.results[0].StartedAt)) {
		t.Errorf("zen History lost the date:\n%s", quiet)
	}
}

func TestReadHidesScoresInZen(t *testing.T) {
	fakeClipboard(t, nil)
	m := finishedSession(t, "hello world")
	updated, _ := m.updateHistory(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	m.zen = true
	quiet := m.viewRead()
	if strings.Contains(quiet, "Score:") {
		t.Errorf("zen Read still shows a score:\n%s", quiet)
	}
	if !strings.Contains(quiet, formatSessionDate(m.read.session.StartedAt)) {
		t.Errorf("zen Read lost the date:\n%s", quiet)
	}
	if !strings.Contains(quiet, "hello world") {
		t.Errorf("zen Read lost the entry text:\n%s", quiet)
	}
}

func TestShareTextIsMinimalInZen(t *testing.T) {
	session := testSession(1234, 2, 0)
	entries := []store.EntryRecord{{Body: "hello world"}}

	got := renderShareText(session, entries, true)

	for _, banned := range []string{"Score:", "1,234", "2 words"} {
		if strings.Contains(got, banned) {
			t.Errorf("zen share text still carries %q: %q", banned, got)
		}
	}
	if !strings.Contains(got, formatSessionDate(session.StartedAt)) {
		t.Errorf("zen share text dropped the date, which places the excerpt: %q", got)
	}
	if !strings.Contains(got, "hello world") {
		t.Errorf("zen share text dropped the entry: %q", got)
	}
}

func TestZenShareKeepsEntryMarkers(t *testing.T) {
	got := renderShareText(testSession(10, 4, 0), []store.EntryRecord{{Body: "first"}, {Body: "second"}}, true)

	for _, want := range []string{"— entry 1 —", "— entry 2 —"} {
		if !strings.Contains(got, want) {
			t.Errorf("zen share text %q missing %q", got, want)
		}
	}
}

func TestCtrlSInZenCopiesTheMinimalForm(t *testing.T) {
	copied := fakeClipboard(t, nil)
	m := finishedSession(t, "hello world")
	m.zen = true

	updated, _ := m.updateHistory(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = updated.(Model)

	if strings.Contains(*copied, "Score:") {
		t.Errorf("ctrl+s in zen copied the stat header: %q", *copied)
	}
	if !strings.Contains(*copied, "hello world") {
		t.Errorf("ctrl+s in zen copied no text: %q", *copied)
	}
}
```

Add `"github.com/nd28/journal-tui/internal/store"` to the import block of `zen_test.go`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run "Zen|History|Read|Share" -v`
Expected: FAIL to build — `too many arguments in call to renderShareText`.

- [ ] **Step 3: Implement the History and Read views**

In `viewHistory`, replace the per-row stat line with a zen branch:

```go
		line := formatSessionDate(r.StartedAt)
		if !m.zen {
			line = fmt.Sprintf(
				"%s   Score: %s   %s%s",
				formatSessionDate(r.StartedAt),
				formatNumber(r.SessionScore),
				formatCount(r.WordCount, "word", "words"),
				formatIntensityTag(r.PeakIntensityRatio),
			)
		}
		b.WriteString(cursor + style.Render(line) + "\n")
```

In `viewRead`, the same shape for the single stat line:

```go
	stats := formatSessionDate(m.read.session.StartedAt)
	if !m.zen {
		stats = fmt.Sprintf(
			"%s   Score: %s   %s%s",
			formatSessionDate(m.read.session.StartedAt),
			formatNumber(m.read.session.SessionScore),
			formatCount(m.read.session.WordCount, "word", "words"),
			formatIntensityTag(m.read.session.PeakIntensityRatio),
		)
	}
	b.WriteString(statStyle.Render(stats) + "\n\n")
```

- [ ] **Step 4: Implement the share text**

Give `renderShareText` a trailing `zen bool` and build the header accordingly:

```go
func renderShareText(session store.SessionSearchResult, entries []store.EntryRecord, zen bool) string {
	var b strings.Builder
	if zen {
		// The date stays even in zen: a pasted excerpt with no date is hard
		// for the reader — and for its writer — to place.
		b.WriteString(formatSessionDate(session.StartedAt))
	} else {
		b.WriteString(fmt.Sprintf(
			"%s   Score: %s   %s%s",
			formatSessionDate(session.StartedAt),
			formatNumber(session.SessionScore),
			formatCount(session.WordCount, "word", "words"),
			formatIntensityTag(session.PeakIntensityRatio),
		))
	}
	b.WriteString("\n\n")
	// ... the entry-body loop below is unchanged
```

Update `shareSession` to pass `m.zen`, and update the three existing calls in `share_test.go` to pass `false`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run "Zen|History|Read|Share" -v`
Expected: PASS.

- [ ] **Step 6: Run the full suite**

Run: `make test`
Expected: all ok.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/history.go internal/tui/read.go internal/tui/share.go internal/tui/share_test.go internal/tui/zen_test.go
git commit -m "Hide scores on History and Read, and in the shared text, in zen"
```

---

### Task 6: Guard the decision, and document it

**Files:**
- Modify: `internal/tui/model.go` (the `Version` const at line 9), `README.md`
- Test: `internal/tui/zen_test.go`

**Interfaces:**
- Consumes: everything above.
- Produces: nothing new.

- [ ] **Step 1: Write the failing test**

This is the test that would catch zen quietly becoming a "doesn't count" mode. Append to `internal/tui/zen_test.go`:

```go
// Zen hides the score; it does not stop it. A writer who turns zen off must
// find a complete history, not a hole where their quiet week was.
func TestASessionWrittenInZenStillScores(t *testing.T) {
	s, _ := openStore(t)
	if err := s.SetZenMode(true); err != nil {
		t.Fatalf("SetZenMode: %v", err)
	}
	m, err := New(s)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	m = startWriting(t, m, "a whole quiet paragraph written without a score in sight ")

	updated, _ := m.updateWriting(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	stats, err := s.GetStats()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.LifetimeScore <= 0 {
		t.Errorf("a zen session earned no lifetime score (%d)", stats.LifetimeScore)
	}
	if stats.CurrentStreak != 1 {
		t.Errorf("a zen session did not advance the streak (streak %d)", stats.CurrentStreak)
	}
	if m.summary.totalWords == 0 {
		t.Error("a zen session recorded no words")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails, or explain why it passes**

Run: `go test ./internal/tui/ -run "TestASessionWrittenInZenStillScores" -v`

Expected: PASS, and that is correct here. This test guards a decision rather than driving new code — nothing in Tasks 1-5 touches scoring, and this is the test that proves it. If it *fails*, a previous task changed what gets stored, which violates the plan's first global constraint. Find and revert that change rather than adapting this test.

- [ ] **Step 3: Bump the version**

In `internal/tui/model.go`, `const Version = "0.8.0"`.

- [ ] **Step 4: Document it in the README**

Add after the Keys section:

```markdown
## Zen mode

`ctrl+z` from any screen hides every score in the app — the home stats, the
writing header, the summary, the score lines in history, and the stat header on
a copied session. The writing screen keeps its `saved` / `unsaved` marker, and
the editor grows into the two lines the header gave up.

The scoring never stops. A session written in zen earns its score and holds your
streak together exactly as it would otherwise; you just don't watch it happen.
Turn zen off and the whole history is there, intact. The preference is stored, so
it survives quitting the app.
```

- [ ] **Step 5: Run the full suite**

Run: `make test`
Expected: all ok.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go internal/tui/zen_test.go README.md
git commit -m "Guard that zen sessions still count, and document zen mode"
```
