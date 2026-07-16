# Live Pace Readout Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show live WPM and the ratio against personal baseline in the writing screen's header, alongside the existing tier tag ("Focused"/"Intense"/"Frantic").

**Architecture:** Track live WPM every combo tick regardless of whether a baseline exists (new `writingState.liveWPM` field), add a pure `formatPaceInfo` helper mirroring the existing `formatIntensityTag`, and append its output to the writing header in `viewWriting`.

**Tech Stack:** Go, Bubble Tea/Bubbles TUI framework. No changes to `internal/scoring` or `internal/store`.

## Global Constraints

- WPM displays as a whole number (`%.0f`), ratio as one decimal (`%.1fx`) — matches existing formatting conventions (combo multiplier, the peak-ratio line on the Summary screen).
- No baseline yet → show WPM only, no ratio, no `·` separator.
- Live pace reads `0 WPM` from the very start of a session — `PaceTracker.WPM` already returns 0 with no recorded events, so no special-casing is needed.
- No changes to `internal/scoring`, `internal/store`, or the Summary/History/Read screens — this is display-only, confined to the writing screen.

Reference spec: `docs/superpowers/specs/2026-07-16-journal-live-pace-readout-design.md`

---

### Task 1: `formatPaceInfo` helper

**Files:**
- Modify: `internal/tui/format.go`
- Test: `internal/tui/format_test.go`

**Interfaces:**
- Produces: `formatPaceInfo(wpm, ratio float64, hasBaseline bool) string` — used by Task 3.

- [ ] **Step 1: Write the failing tests**

Add to the end of `internal/tui/format_test.go`:

```go
func TestFormatPaceInfoWithoutBaselineShowsWPMOnly(t *testing.T) {
	if got := formatPaceInfo(42, 0, false); got != "42 WPM" {
		t.Fatalf("expected WPM-only reading, got %q", got)
	}
}

func TestFormatPaceInfoWithBaselineShowsRatio(t *testing.T) {
	if got := formatPaceInfo(42, 1.4, true); got != "42 WPM · 1.4x" {
		t.Fatalf("expected WPM and ratio, got %q", got)
	}
}

func TestFormatPaceInfoRoundsWPMToWholeNumber(t *testing.T) {
	if got := formatPaceInfo(41.6, 0, false); got != "42 WPM" {
		t.Fatalf("expected rounded WPM, got %q", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/... -run TestFormatPaceInfo -v`
Expected: FAIL — `undefined: formatPaceInfo`

- [ ] **Step 3: Implement `formatPaceInfo`**

In `internal/tui/format.go`, add `"fmt"` to the import block (currently `"strconv"`, `"strings"`, `"time"`, and the `internal/scoring` package):

```go
import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nd28/journal-tui/internal/scoring"
)
```

Then append this function at the end of the file, after `formatIntensityTag`:

```go
// formatPaceInfo renders the live pace readout for the writing header: just
// the WPM reading with no baseline yet, or WPM plus the ratio against
// personal baseline once one exists.
func formatPaceInfo(wpm, ratio float64, hasBaseline bool) string {
	if !hasBaseline {
		return fmt.Sprintf("%.0f WPM", wpm)
	}
	return fmt.Sprintf("%.0f WPM · %.1fx", wpm, ratio)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/... -run TestFormatPaceInfo -v`
Expected: PASS (all 3 subtests)

- [ ] **Step 5: Commit**

```bash
git add internal/tui/format.go internal/tui/format_test.go
git commit -m "Add formatPaceInfo helper for the live pace readout"
```

---

### Task 2: Track live WPM on every combo tick

**Files:**
- Modify: `internal/tui/writing.go:15-29` (the `writingState` struct)
- Modify: `internal/tui/writing.go:218-229` (the `comboTickMsg` branch of `updateWriting`)
- Test: `internal/tui/writing_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `writingState.liveWPM float64` — used by Task 3.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/writing_test.go`, after `TestComboTickUpdatesIntensityAndTracksPeak`:

```go
func TestComboTickUpdatesLiveWPMWithoutBaseline(t *testing.T) {
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
	if m.writing.hasBaseline {
		t.Fatal("expected no baseline for a fresh store")
	}

	now := time.Now()
	m.writing.session.CompleteWord(now)
	m.writing.session.CompleteWord(now.Add(1 * time.Second))

	// 2 words 1s apart floors to a 5s window: 2 / (5s in minutes) = 24 WPM.
	tickTime := now.Add(1 * time.Second)
	updated, _ = m.updateWriting(comboTickMsg(tickTime))
	m = updated.(Model)

	if got := m.writing.liveWPM; got != 24 {
		t.Fatalf("expected live WPM 24 without a baseline, got %v", got)
	}
	if got := m.writing.intensityRatio; got != 0 {
		t.Fatalf("expected intensity ratio to stay 0 without a baseline, got %v", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/... -run TestComboTickUpdatesLiveWPMWithoutBaseline -v`
Expected: FAIL — `m.writing.liveWPM undefined (type writingState has no field or method liveWPM)`

- [ ] **Step 3: Add the field and update the tick handler**

In `internal/tui/writing.go`, add `liveWPM` to the `writingState` struct:

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
	liveWPM            float64
	intensityRatio     float64
	peakIntensityRatio float64
}
```

Then update the `comboTickMsg` branch inside `updateWriting`:

```go
	if tickMsg, ok := msg.(comboTickMsg); ok {
		now := time.Time(tickMsg)
		m.writing.session.Combo.Tick(now)
		m.writing.liveWPM = m.writing.session.Pace.WPM(now)
		if m.writing.hasBaseline {
			m.writing.intensityRatio = m.writing.liveWPM / m.writing.baselineWPM
			if m.writing.intensityRatio > m.writing.peakIntensityRatio {
				m.writing.peakIntensityRatio = m.writing.intensityRatio
			}
		}
		return m, comboTick()
	}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/tui/... -run TestComboTickUpdatesLiveWPMWithoutBaseline -v`
Expected: PASS

- [ ] **Step 5: Run the existing intensity test to confirm no regression**

Run: `go test ./internal/tui/... -run TestComboTickUpdatesIntensityAndTracksPeak -v`
Expected: PASS (unchanged behavior for the baseline case)

- [ ] **Step 6: Commit**

```bash
git add internal/tui/writing.go internal/tui/writing_test.go
git commit -m "Track live WPM every combo tick regardless of baseline"
```

---

### Task 3: Show the pace readout in the writing header

**Files:**
- Modify: `internal/tui/writing.go:272-289` (`viewWriting`)
- Test: `internal/tui/writing_test.go`

**Interfaces:**
- Consumes: `formatPaceInfo` (Task 1), `writingState.liveWPM` (Task 2).
- Produces: nothing further downstream — this is the final display step.

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/writing_test.go`, after `TestViewWritingHidesTierTagAtNormalPace`:

```go
func TestViewWritingShowsWPMOnlyWithoutBaseline(t *testing.T) {
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
	if m.writing.hasBaseline {
		t.Fatal("expected no baseline for a fresh store")
	}

	m.writing.liveWPM = 42
	got := m.viewWriting()
	if !strings.Contains(got, "42 WPM") {
		t.Fatalf("expected WPM-only reading, got %q", got)
	}
	if strings.Contains(got, "WPM ·") {
		t.Fatalf("expected no ratio without a baseline, got %q", got)
	}
}

func TestViewWritingShowsRatioWithBaseline(t *testing.T) {
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
	m.writing.liveWPM = 42
	m.writing.intensityRatio = 1.4

	got := m.viewWriting()
	if !strings.Contains(got, "42 WPM · 1.4x") {
		t.Fatalf("expected WPM and ratio, got %q", got)
	}
}

func TestViewWritingShowsZeroWPMAtSessionStart(t *testing.T) {
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
	if !strings.Contains(got, "0 WPM") {
		t.Fatalf("expected 0 WPM before the first word, got %q", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/... -run TestViewWritingShows -v`
Expected: FAIL overall — this also matches the pre-existing `TestViewWritingShowsTierTagWhenElevated` (still PASS, unaffected), but the 3 new tests (`TestViewWritingShowsWPMOnlyWithoutBaseline`, `TestViewWritingShowsRatioWithBaseline`, `TestViewWritingShowsZeroWPMAtSessionStart`) FAIL because the header doesn't contain a WPM reading yet.

- [ ] **Step 3: Wire `formatPaceInfo` into the header**

In `internal/tui/writing.go`, update `viewWriting`:

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
	header += "   " + formatPaceInfo(m.writing.liveWPM, m.writing.intensityRatio, m.writing.hasBaseline)
	help := statStyle.Render("ctrl+n: new entry   ctrl+t: toggle size   esc: end session")
	view := titleStyle.Render(header) + "\n\n" + m.writing.textarea.View() + "\n\n" + help
	if m.writing.pasteWarning != "" {
		view += "\n" + errorStyle.Render(m.writing.pasteWarning)
	}
	return view
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/... -run TestViewWritingShows -v`
Expected: PASS (all 3 tests)

- [ ] **Step 5: Run the full `tui` package test suite to confirm no regressions**

Run: `go test ./internal/tui/...`
Expected: `ok  	github.com/nd28/journal-tui/internal/tui`

- [ ] **Step 6: Commit**

```bash
git add internal/tui/writing.go internal/tui/writing_test.go
git commit -m "Show live WPM and ratio in the writing header"
```

---

### Task 4: Full-suite verification and manual check

**Files:** none (verification only)

- [ ] **Step 1: Run the entire test suite**

Run: `go build ./... && go test ./...`
Expected: all packages `ok`, no build errors.

- [ ] **Step 2: Bump the version constant**

In `internal/tui/model.go:9`, bump the patch version for this user-visible change:

```go
const Version = "0.2.1"
```

- [ ] **Step 3: Manual check in a running session**

Build and run against a scratch DB (do not touch the real `~/.journal/journal.db`):

```bash
go build -o /tmp/journal-demo ./cmd/journal
HOME=/tmp/journal-fakehome /tmp/journal-demo
```

Start a session (Enter on "New Session") and confirm the header reads `... 0 WPM` before typing, and updates to a live WPM reading as you type (no ratio, since a fresh scratch DB has no baseline). Exit with `ctrl+c`.

- [ ] **Step 4: Commit the version bump**

```bash
git add internal/tui/model.go
git commit -m "Bump version for live pace readout"
```
