package scoring

import (
	"math"
	"time"
)

const (
	StreakBonusPerDay = 0.05
	StreakBonusCap    = 0.50
)

// Entry is a finalized chunk of writing within a session.
type Entry struct {
	Words  int
	Points int
}

// Session tracks one writing "run": a live combo plus zero or more
// finalized entries and the words/points typed since the last one.
type Session struct {
	Combo   ComboState
	Pace    PaceTracker
	Entries []Entry

	currentWords  int
	currentPoints int
}

func NewSession(now time.Time) *Session {
	return &Session{Combo: NewCombo(now)}
}

// RestoreSession rebuilds a session that was interrupted mid-write, seeding
// it with the words and points it had already earned so the writer picks up
// from their running score rather than from zero. The prior work is folded
// into one finalized entry: only the totals were ever persisted, and only
// the totals are ever read back out.
//
// The combo deliberately restarts at the floor. It measures the rhythm of
// the last few seconds of typing, and the interruption ended that rhythm —
// restoring a 4x multiplier would hand out points for momentum that no
// longer exists.
func RestoreSession(now time.Time, priorWords, priorPoints int) *Session {
	s := NewSession(now)
	if priorWords > 0 || priorPoints > 0 {
		s.Entries = append(s.Entries, Entry{Words: priorWords, Points: priorPoints})
	}
	return s
}

// CompleteWord registers a completed word in the current (in-progress) entry.
func (s *Session) CompleteWord(now time.Time) {
	points := s.Combo.CompleteWord(now)
	s.Pace.CompleteWord(now)
	s.currentPoints += points
	s.currentWords++
}

// NewEntry finalizes the current in-progress entry, if it has any words,
// and resets the counters for the next one.
func (s *Session) NewEntry() {
	if s.currentWords == 0 {
		return
	}
	s.Entries = append(s.Entries, Entry{Words: s.currentWords, Points: s.currentPoints})
	s.currentWords = 0
	s.currentPoints = 0
}

// RawScore is the sum of all points earned so far, including the
// in-progress entry, before any streak bonus.
func (s *Session) RawScore() int {
	total := s.currentPoints
	for _, e := range s.Entries {
		total += e.Points
	}
	return total
}

// TotalWords is the word count across all entries, including in-progress.
func (s *Session) TotalWords() int {
	total := s.currentWords
	for _, e := range s.Entries {
		total += e.Words
	}
	return total
}

// StreakBonus returns the multiplier applied to a session's raw score for
// a given consecutive-day streak (e.g. 1.15 for a 3-day streak), capped.
func StreakBonus(streakDays int) float64 {
	bonus := float64(streakDays) * StreakBonusPerDay
	if bonus > StreakBonusCap {
		bonus = StreakBonusCap
	}
	return 1.0 + bonus
}

// FinalScore applies the streak bonus to a raw session score, rounded to
// the nearest integer.
func FinalScore(rawScore int, streakDays int) int {
	return int(math.Round(float64(rawScore) * StreakBonus(streakDays)))
}
