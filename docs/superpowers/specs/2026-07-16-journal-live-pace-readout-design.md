# Journal — Live Pace Readout (Design)

Date: 2026-07-16

## Purpose

The writing-intensity feature (`2026-07-15-journal-writing-intensity-design.md`) added
a discrete tier label ("Focused" / "Intense" / "Frantic") to the writing header, but
only the label — the numbers driving it (live words-per-minute, and the ratio against
your personal baseline) aren't visible anywhere during writing. This follow-on surfaces
those numbers live, next to the existing tier tag, so the header reads as information
rather than just a mood label.

This is a display-only change: no new scoring, storage, or peak-tracking behavior.

## Architecture

```
internal/tui/writing.go   — changed: track live WPM regardless of baseline; append pace numbers to header
internal/tui/format.go    — changed: new formatPaceInfo helper
```

No changes to `internal/scoring`, `internal/store`, or the Summary/History/Read
screens — those already show what they need (Summary shows the peak ratio; History/Read
show the peak tier word), and this ask is specifically about the *live* writing view.

## Live WPM tracking (`internal/tui/writing.go`)

`writingState` gains a `liveWPM float64` field. Today the `comboTickMsg` handler only
calls `session.Pace.WPM(now)` inside the `if m.writing.hasBaseline` branch, since that's
currently the only consumer. This changes to:

```go
now := time.Time(tickMsg)
m.writing.session.Combo.Tick(now)
m.writing.liveWPM = m.writing.session.Pace.WPM(now)
if m.writing.hasBaseline {
    m.writing.intensityRatio = m.writing.liveWPM / m.writing.baselineWPM
    if m.writing.intensityRatio > m.writing.peakIntensityRatio {
        m.writing.peakIntensityRatio = m.writing.intensityRatio
    }
}
```

`liveWPM` updates every tick (200ms) regardless of whether a baseline exists —
`PaceTracker.WPM` already returns 0 with no recorded events, so a fresh session reads
"0 WPM" until the first word completes; no special-casing needed.

## Display (`internal/tui/format.go`, `internal/tui/writing.go`)

New helper, parallel to the existing `formatIntensityTag`:

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

`viewWriting`'s header construction appends this after the existing tier-tag logic,
using the same `"   "` (3-space) separator used elsewhere in the header:

```go
if tier := scoring.IntensityTier(m.writing.intensityRatio); tier != "" {
    header += "   " + tier
}
header += "   " + formatPaceInfo(m.writing.liveWPM, m.writing.intensityRatio, m.writing.hasBaseline)
```

Example headers:

```
Score: 130   Words: 13   [████████████████░░░░] 1.0x   0 WPM                    (session start, no baseline)
Score: 130   Words: 13   [████████████████░░░░] 1.0x   28 WPM · 0.9x            (baseline exists, pace normal)
Score: 130   Words: 13   [████████████████░░░░] 1.0x   Focused   42 WPM · 1.4x  (baseline exists, pace elevated)
```

## Out of scope

- No changes to Summary, History, or Read — those already show peak numbers/tier and
  aren't "live."
- No change to how the tier tag itself is computed or when it's shown — unchanged from
  the existing intensity feature.
- No decimal precision on the WPM reading — whole numbers only, matching the terse
  style of the rest of the header.

## Testing

`internal/tui`: extend `writing_test.go` to assert `liveWPM` updates on `comboTickMsg`
even when `hasBaseline` is false, and that `viewWriting`'s header contains the right
pace string in three cases — no baseline, baseline with normal pace, baseline with
elevated pace (tier present). Add `TestFormatPaceInfo` in `format_test.go` covering both
branches and rounding (e.g. 41.6 → "42 WPM").
