# Journal Editor Polish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the writing screen use real terminal space (with a compact-mode escape hatch), block pasting since this app is about raw in-the-moment writing, make word-count labels honest about what they mean, and show the app version at all times.

**Architecture:** All changes live in `internal/tui` — no store schema changes, no new screens. The root `Model` gains terminal-dimension tracking (`tea.WindowSizeMsg`) and a compact-mode flag; the writing screen consumes both to size its textarea via one small pure function.

**Tech Stack:** Go 1.25, `github.com/charmbracelet/bubbletea` (already a dependency), `github.com/charmbracelet/bubbles/textarea` (already a dependency) — no new dependencies for this plan.

## Global Constraints

- No changes to `internal/scoring` or `internal/store` — this plan is `internal/tui`-only.
- No changes to combo/scoring math.
- Full mode is the default on every launch; the compact/full choice is in-memory only, never persisted.
- Width in Full mode: `min(terminal width, 100)` minus a 4-column margin, floored at 20.
- Height in Full mode: terminal height minus 6 fixed chrome lines (header, two blank separators, help line, a reserved paste-warning line, and the version footer line) minus a 2-line safety margin, floored at 3. The paste-warning line is reserved in the budget even when not currently shown, so a paste attempt never causes clipping.
- Compact mode uses `bubbles/textarea`'s own built-in default size: width 40, height 6 (today's existing v1 behavior, unchanged).
- Toggle key: `Ctrl+T`, only active on the writing screen.
- Version string: hand-maintained `const Version = "0.1.0"` in `internal/tui`, rendered as the last line of every screen (`journal v0.1.0`).

---

### Task 1: Version footer and honest word-count label

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/summary.go`
- Test: `internal/tui/model_test.go`

**Interfaces:**
- Produces: exported const `Version = "0.1.0"` in package `tui`, used by `Model.View()`.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/model_test.go` (append; the file already has `errors`, `strings`, `testing`, and the `tea` import):

```go
func TestViewIncludesVersionFooter(t *testing.T) {
	m := Model{screen: screenHome}
	view := m.View()
	if !strings.Contains(view, "journal v"+Version) {
		t.Fatalf("expected view to contain version footer %q, got %q", "journal v"+Version, view)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ~/journal && go test ./internal/tui/... -run TestViewIncludesVersionFooter -v`
Expected: FAIL — `Version` undefined.

- [ ] **Step 3: Add the Version const and footer rendering**

In `internal/tui/model.go`, add the const near the top (after the imports, before `type screen int`):

```go
const Version = "0.1.0"
```

Change `View()` from:

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
	}
	if m.err != nil {
		body += "\n" + errorStyle.Render("Error: "+m.err.Error())
	}
	return body
}
```

to:

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
	}
	if m.err != nil {
		body += "\n" + errorStyle.Render("Error: "+m.err.Error())
	}
	body += "\n" + statStyle.Render("journal v"+Version)
	return body
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd ~/journal && go test ./internal/tui/... -run TestViewIncludesVersionFooter -v`
Expected: PASS

- [ ] **Step 5: Rename the Summary word-count label**

In `internal/tui/summary.go`, change:

```go
	b.WriteString(statStyle.Render(fmt.Sprintf("Words written:  %d", m.summary.totalWords)) + "\n")
```

to:

```go
	b.WriteString(statStyle.Render(fmt.Sprintf("Words typed:    %d", m.summary.totalWords)) + "\n")
```

(Note the label column width changes from `Words written:` (14 chars) to `Words typed:` (12 chars) — added two extra spaces so the values still align with the other `Raw score:`/`Session score:`/`Lifetime score:` lines below it.)

- [ ] **Step 6: Run the full package test suite**

Run: `cd ~/journal && go build ./... && go test ./internal/tui/... -v`
Expected: all tests pass, including the pre-existing summary/writing tests (none of them assert on the literal `"Words written:"` string — confirm this by checking `grep -rn "Words written" internal/tui/*_test.go` returns nothing before this step, and nothing breaks).

- [ ] **Step 7: Commit**

```bash
cd ~/journal
git add internal/tui/model.go internal/tui/summary.go internal/tui/model_test.go
git commit -m "Add version footer and rename Summary word-count label"
```

---

### Task 2: Terminal size tracking on the root Model

**Files:**
- Modify: `internal/tui/model.go`
- Test: `internal/tui/model_test.go`

**Interfaces:**
- Produces: `Model.width int`, `Model.height int` fields, updated whenever a `tea.WindowSizeMsg` arrives, available to all per-screen `updateX`/`viewX` methods (used by Task 3's `writingDimensions`).

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/model_test.go`:

```go
func TestWindowSizeMsgUpdatesModelDimensions(t *testing.T) {
	m := Model{screen: screenHome}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	hm := updated.(Model)
	if hm.width != 120 || hm.height != 40 {
		t.Fatalf("expected width=120 height=40, got width=%d height=%d", hm.width, hm.height)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ~/journal && go test ./internal/tui/... -run TestWindowSizeMsgUpdatesModelDimensions -v`
Expected: FAIL — `hm.width`/`hm.height` undefined (no such fields yet).

- [ ] **Step 3: Add width/height fields and WindowSizeMsg handling**

In `internal/tui/model.go`, change the `Model` struct from:

```go
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
```

to:

```go
type Model struct {
	screen screen
	store  *store.Store
	stats  store.Stats

	width  int
	height int

	homeCursor int

	writing writingState
	summary summaryState
	history historyState

	err error
}
```

Change `Update()` from:

```go
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
```

to:

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
	}
	return m, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd ~/journal && go test ./internal/tui/... -run TestWindowSizeMsgUpdatesModelDimensions -v`
Expected: PASS

- [ ] **Step 5: Run the full package test suite**

Run: `cd ~/journal && go build ./... && go test ./internal/tui/... -v`
Expected: all tests pass (this change is additive and doesn't alter existing dispatch behavior for any message type other than `tea.WindowSizeMsg`, which no existing test sends).

- [ ] **Step 6: Commit**

```bash
cd ~/journal
git add internal/tui/model.go internal/tui/model_test.go
git commit -m "Track terminal dimensions on the root Model"
```

---

### Task 3: Full/Compact writing-screen sizing and Ctrl+T toggle

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/writing.go`
- Test: `internal/tui/writing_test.go`

**Interfaces:**
- Consumes: `Model.width`, `Model.height` from Task 2.
- Produces: `Model.compactMode bool` field; unexported `writingDimensions(termWidth, termHeight int, compact bool) (width, height int)` (pure, testable); applied in `startWritingSession`, on `tea.WindowSizeMsg` in `updateWriting`, and on `Ctrl+T` in `updateWriting`.

- [ ] **Step 1: Write the failing tests**

First, add the `bubbletea` import to `internal/tui/writing_test.go` — the tests below use `tea.KeyMsg`, `tea.WindowSizeMsg`, and `tea.KeyCtrlT`, which the file doesn't currently import. Change the import block from:

```go
import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"journal/internal/scoring"
	"journal/internal/store"
)
```

to:

```go
import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"journal/internal/scoring"
	"journal/internal/store"
)
```

Then add the following tests to the same file:

```go
func TestWritingDimensionsFullModeClampsToMaxWidth(t *testing.T) {
	w, h := writingDimensions(200, 50, false)
	if w != writingMaxWidth-writingWidthMargin {
		t.Fatalf("expected width %d, got %d", writingMaxWidth-writingWidthMargin, w)
	}
	if h != 50-writingChromeLines-writingHeightMargin {
		t.Fatalf("expected height %d, got %d", 50-writingChromeLines-writingHeightMargin, h)
	}
}

func TestWritingDimensionsFullModeUsesSmallerTerminal(t *testing.T) {
	w, h := writingDimensions(60, 20, false)
	if w != 60-writingWidthMargin {
		t.Fatalf("expected width %d, got %d", 60-writingWidthMargin, w)
	}
	if h != 20-writingChromeLines-writingHeightMargin {
		t.Fatalf("expected height %d, got %d", 20-writingChromeLines-writingHeightMargin, h)
	}
}

func TestWritingDimensionsFullModeFloorsOnTinyTerminal(t *testing.T) {
	w, h := writingDimensions(10, 5, false)
	if w != writingMinWidth {
		t.Fatalf("expected width floor %d, got %d", writingMinWidth, w)
	}
	if h != writingMinHeight {
		t.Fatalf("expected height floor %d, got %d", writingMinHeight, h)
	}
}

func TestWritingDimensionsCompactModeIgnoresTerminalSize(t *testing.T) {
	w, h := writingDimensions(200, 80, true)
	if w != compactWidth || h != compactHeight {
		t.Fatalf("expected compact %dx%d, got %dx%d", compactWidth, compactHeight, w, h)
	}
}

func TestCtrlTTogglesCompactMode(t *testing.T) {
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
	m.width, m.height = 120, 40

	updated, _ := m.startWritingSession()
	m = updated.(Model)
	if m.compactMode {
		t.Fatal("expected to start in Full mode")
	}
	if got := m.writing.textarea.Width(); got != writingMaxWidth-writingWidthMargin {
		t.Fatalf("expected initial full width %d, got %d", writingMaxWidth-writingWidthMargin, got)
	}

	updated, _ = m.updateWriting(tea.KeyMsg{Type: tea.KeyCtrlT})
	m = updated.(Model)
	if !m.compactMode {
		t.Fatal("expected compact mode after ctrl+t")
	}
	if got := m.writing.textarea.Width(); got != compactWidth {
		t.Fatalf("expected compact width %d after toggle, got %d", compactWidth, got)
	}

	updated, _ = m.updateWriting(tea.KeyMsg{Type: tea.KeyCtrlT})
	m = updated.(Model)
	if m.compactMode {
		t.Fatal("expected full mode after second ctrl+t")
	}
	if got := m.writing.textarea.Width(); got != writingMaxWidth-writingWidthMargin {
		t.Fatalf("expected full width %d after second toggle, got %d", writingMaxWidth-writingWidthMargin, got)
	}
}

func TestWindowSizeMsgResizesActiveTextarea(t *testing.T) {
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
	m.width, m.height = 120, 40

	updated, _ := m.startWritingSession()
	m = updated.(Model)

	updated, _ = m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m = updated.(Model)

	if got := m.writing.textarea.Width(); got != 60-writingWidthMargin {
		t.Fatalf("expected resized width %d, got %d", 60-writingWidthMargin, got)
	}
	if got := m.writing.textarea.Height(); got != 20-writingChromeLines-writingHeightMargin {
		t.Fatalf("expected resized height %d, got %d", 20-writingChromeLines-writingHeightMargin, got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/journal && go test ./internal/tui/... -v`
Expected: FAIL to compile — `writingDimensions`, `writingMaxWidth`, `writingWidthMargin`, `writingChromeLines`, `writingHeightMargin`, `writingMinWidth`, `writingMinHeight`, `compactWidth`, `compactHeight`, `m.compactMode` all undefined.

- [ ] **Step 3: Add the compactMode field to Model**

In `internal/tui/model.go`, add `compactMode bool` to the `Model` struct (after `height int`):

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

	err error
}
```

- [ ] **Step 4: Add sizing constants and the pure writingDimensions function**

In `internal/tui/writing.go`, add near the top (after the `type writingState struct` block, before `type comboTickMsg`):

```go
const (
	writingMaxWidth    = 100
	writingWidthMargin = 4
	writingMinWidth    = 20

	// writingChromeLines is the writing screen's fixed vertical overhead:
	// header, two blank separators, the help line, a line reserved for the
	// paste-block warning (even when not currently shown, so a paste
	// attempt never causes clipping), and the version footer appended by
	// Model.View().
	writingChromeLines  = 6
	writingHeightMargin = 2
	writingMinHeight    = 3

	compactWidth  = 40
	compactHeight = 6
)

// writingDimensions computes the textarea's width/height for the writing
// screen given the terminal size and whether compact mode is active.
// Compact mode ignores the terminal size entirely and returns
// bubbles/textarea's own built-in default (40x6) — today's v1 behavior.
func writingDimensions(termWidth, termHeight int, compact bool) (width, height int) {
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

	height = termHeight - writingChromeLines - writingHeightMargin
	if height < writingMinHeight {
		height = writingMinHeight
	}

	return width, height
}
```

- [ ] **Step 5: Apply sizing in startWritingSession**

In `internal/tui/writing.go`, change `startWritingSession` from:

```go
	ta := textarea.New()
	ta.Placeholder = "Start writing..."
	ta.ShowLineNumbers = false
	focusCmd := ta.Focus()
```

to:

```go
	ta := textarea.New()
	ta.Placeholder = "Start writing..."
	ta.ShowLineNumbers = false
	w, h := writingDimensions(m.width, m.height, m.compactMode)
	ta.SetWidth(w)
	ta.SetHeight(h)
	focusCmd := ta.Focus()
```

- [ ] **Step 6: Handle WindowSizeMsg and Ctrl+T in updateWriting**

In `internal/tui/writing.go`, change `updateWriting` from:

```go
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
```

to:

```go
func (m Model) updateWriting(msg tea.Msg) (tea.Model, tea.Cmd) {
	if tickMsg, ok := msg.(comboTickMsg); ok {
		m.writing.session.Combo.Tick(time.Time(tickMsg))
		return m, comboTick()
	}

	if _, ok := msg.(tea.WindowSizeMsg); ok {
		w, h := writingDimensions(m.width, m.height, m.compactMode)
		m.writing.textarea.SetWidth(w)
		m.writing.textarea.SetHeight(h)
		return m, nil
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
		case "ctrl+t":
			m.compactMode = !m.compactMode
			w, h := writingDimensions(m.width, m.height, m.compactMode)
			m.writing.textarea.SetWidth(w)
			m.writing.textarea.SetHeight(h)
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.writing.textarea, cmd = m.writing.textarea.Update(msg)
	m.writing.lastWordCount = syncWordCount(m.writing.session, m.writing.lastWordCount, m.writing.textarea.Value(), time.Now())
	return m, cmd
}
```

- [ ] **Step 7: Update the writing-screen help text**

In `internal/tui/writing.go`, change `viewWriting`'s help line from:

```go
	help := statStyle.Render("ctrl+n: new entry   esc: end session")
```

to:

```go
	help := statStyle.Render("ctrl+n: new entry   ctrl+t: toggle size   esc: end session")
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `cd ~/journal && go build ./... && go test ./internal/tui/... -v`
Expected: PASS on all tests, including the 6 new ones and every pre-existing test in the package.

- [ ] **Step 9: Commit**

```bash
cd ~/journal
git add internal/tui/model.go internal/tui/writing.go internal/tui/writing_test.go
git commit -m "Add Full/Compact writing-screen sizing with Ctrl+T toggle"
```

---

### Task 4: Block paste in the writing screen

**Files:**
- Modify: `internal/tui/writing.go`
- Test: `internal/tui/writing_test.go`

**Interfaces:**
- Consumes: the `updateWriting`/`viewWriting` structure and `writingDimensions`/`compactMode` toggle from Task 3 — this task inserts into, but does not restructure, that code.
- Produces: `writingState.pasteWarning string` field; paste-blocking behavior in `updateWriting`; warning rendering in `viewWriting`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/writing_test.go`:

```go
func TestPasteIsBlockedAndWarns(t *testing.T) {
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
	m.width, m.height = 120, 40

	updated, _ := m.startWritingSession()
	m = updated.(Model)

	updated, _ = m.updateWriting(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("pasted text"), Paste: true})
	m = updated.(Model)

	if m.writing.textarea.Value() != "" {
		t.Fatalf("expected pasted text to be blocked, got textarea value %q", m.writing.textarea.Value())
	}
	if m.writing.pasteWarning == "" {
		t.Fatal("expected a paste warning to be set")
	}
	if !strings.Contains(m.viewWriting(), m.writing.pasteWarning) {
		t.Fatalf("expected viewWriting to render the paste warning %q", m.writing.pasteWarning)
	}
}

func TestPasteWarningClearsOnNextNormalKey(t *testing.T) {
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
	m.width, m.height = 120, 40

	updated, _ := m.startWritingSession()
	m = updated.(Model)

	updated, _ = m.updateWriting(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x"), Paste: true})
	m = updated.(Model)
	if m.writing.pasteWarning == "" {
		t.Fatal("expected a paste warning to be set")
	}

	updated, _ = m.updateWriting(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	m = updated.(Model)
	if m.writing.pasteWarning != "" {
		t.Fatalf("expected paste warning to clear after a normal key, got %q", m.writing.pasteWarning)
	}
	if m.writing.textarea.Value() != "h" {
		t.Fatalf("expected normal key to reach the textarea, got %q", m.writing.textarea.Value())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ~/journal && go test ./internal/tui/... -run TestPaste -v`
Expected: FAIL — pasted text is not blocked (goes straight into the textarea, since nothing currently checks `keyMsg.Paste`), and `m.writing.pasteWarning` is undefined.

- [ ] **Step 3: Add pasteWarning field and blocking logic**

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
}
```

Change `updateWriting` (from Task 3's version) by inserting a paste check right after the `tea.WindowSizeMsg` branch and before the key-message switch:

```go
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.Paste {
			m.writing.pasteWarning = "paste disabled — write it yourself"
			return m, nil
		}
		m.writing.pasteWarning = ""

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
		case "ctrl+t":
			m.compactMode = !m.compactMode
			w, h := writingDimensions(m.width, m.height, m.compactMode)
			m.writing.textarea.SetWidth(w)
			m.writing.textarea.SetHeight(h)
			return m, nil
		}
	}
```

(This replaces the existing `if keyMsg, ok := msg.(tea.KeyMsg); ok { switch keyMsg.String() { ... } }` block — only the two new lines at the top of the block, `if keyMsg.Paste { ... }` and `m.writing.pasteWarning = ""`, are added; the four existing `case` branches are unchanged.)

- [ ] **Step 4: Render the paste warning**

In `internal/tui/writing.go`, change `viewWriting` from:

```go
func (m Model) viewWriting() string {
	combo := m.writing.session.Combo
	header := fmt.Sprintf(
		"Score: %d   Words: %d   %s",
		m.writing.session.RawScore(),
		m.writing.session.TotalWords(),
		renderComboBar(combo.Multiplier, 20),
	)
	help := statStyle.Render("ctrl+n: new entry   ctrl+t: toggle size   esc: end session")
	return titleStyle.Render(header) + "\n\n" + m.writing.textarea.View() + "\n\n" + help
}
```

to:

```go
func (m Model) viewWriting() string {
	combo := m.writing.session.Combo
	header := fmt.Sprintf(
		"Score: %d   Words: %d   %s",
		m.writing.session.RawScore(),
		m.writing.session.TotalWords(),
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

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd ~/journal && go build ./... && go vet ./... && go test ./...`
Expected: PASS on the full suite (`internal/scoring`, `internal/store`, `internal/tui`), including the 2 new tests and every pre-existing test.

- [ ] **Step 6: Manual smoke test**

Run: `cd ~/journal && go run ./cmd/journal`

1. Start a New Session. Confirm the writing screen fills most of your terminal (Full mode).
2. Press `Ctrl+T` — confirm the textarea shrinks to a small fixed box (Compact mode). Press `Ctrl+T` again — confirm it returns to filling the terminal.
3. Resize your terminal window while in Full mode — confirm the textarea reflows to the new size on the next keystroke or resize event.
4. Copy some text from outside the terminal and paste it into the writing screen (e.g. with your terminal's native paste, or `Ctrl+Shift+V`/`Cmd+V` depending on your terminal). Confirm the pasted text does NOT appear in the textarea, and a `paste disabled — write it yourself` message appears. Type a character — confirm the message disappears and normal typing works.
5. Confirm `journal v0.1.0` appears at the bottom of the Home, Writing, Summary, and History screens.
6. End a session and confirm the Summary screen shows `Words typed:` (not `Words written:`).

If any step fails, fix the underlying code (not the test) before proceeding.

- [ ] **Step 7: Commit**

```bash
cd ~/journal
git add internal/tui/writing.go internal/tui/writing_test.go
git commit -m "Block paste in the writing screen"
```

---

## After this plan

Not in scope here: History search/pagination/reading past entries (separate spec and plan, `docs/superpowers/specs/2026-07-14-journal-history-search-design.md`), persisting the compact/full choice, and any general settings/config file.
