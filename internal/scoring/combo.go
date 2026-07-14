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
