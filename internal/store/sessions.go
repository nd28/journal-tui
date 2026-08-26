package store

import (
	"database/sql"
	"strings"
	"time"
)

// timestamp renders a time for storage. Always UTC: every ORDER BY on these
// columns is a lexicographic string sort, which mis-orders rows as soon as
// two different UTC offsets are in play (travel, DST, or a mix of migrated
// and fresh rows). Display converts back to local.
func timestamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

type SessionRecord struct {
	ID                 int64
	StartedAt          string
	SessionScore       int
	WordCount          int
	AvgPaceWPM         float64
	PeakIntensityRatio float64
}

func (s *Store) StartSession(now time.Time) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO sessions (started_at, session_score, streak_bonus_applied) VALUES (?, 0, 1.0)`,
		timestamp(now),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) SaveEntry(sessionID int64, createdAt time.Time, body string, wordCount int) error {
	_, err := s.db.Exec(
		`INSERT INTO entries (session_id, created_at, body, word_count) VALUES (?, ?, ?, ?)`,
		sessionID, timestamp(createdAt), body, wordCount,
	)
	return err
}

// DiscardSession removes a session and anything written under it. Used when
// a session ends with nothing written, so opening the writing screen and
// changing your mind doesn't leave an unfinished row behind forever.
func (s *Store) DiscardSession(sessionID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM entries WHERE session_id = ?`, sessionID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM drafts WHERE session_id = ?`, sessionID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE id = ?`, sessionID); err != nil {
		return err
	}
	return tx.Commit()
}

// FinishSession records the session's final score, then updates the
// singleton stats row (lifetime score, high score, streak). newStreak and
// entryDate are computed by the caller (typically via ComputeStreak) at
// session start, since they reflect the day the writing happened.
// Returns the updated stats and whether sessionScore is a new all-time high.
func (s *Store) FinishSession(sessionID int64, endedAt time.Time, sessionScore int, streakBonus float64, newStreak int, entryDate string) (Stats, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Stats{}, false, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`UPDATE sessions SET ended_at = ?, session_score = ?, streak_bonus_applied = ? WHERE id = ?`,
		timestamp(endedAt), sessionScore, streakBonus, sessionID,
	); err != nil {
		return Stats{}, false, err
	}

	var stats Stats
	row := tx.QueryRow(`SELECT lifetime_score, high_session_score, current_streak, last_entry_date FROM stats WHERE id = 1`)
	if err := row.Scan(&stats.LifetimeScore, &stats.HighSessionScore, &stats.CurrentStreak, &stats.LastEntryDate); err != nil {
		return Stats{}, false, err
	}

	isNewHigh := sessionScore > stats.HighSessionScore
	stats.LifetimeScore += sessionScore
	if isNewHigh {
		stats.HighSessionScore = sessionScore
	}
	stats.CurrentStreak = newStreak
	stats.LastEntryDate = entryDate

	if _, err := tx.Exec(
		`UPDATE stats SET lifetime_score = ?, high_session_score = ?, current_streak = ?, last_entry_date = ? WHERE id = 1`,
		stats.LifetimeScore, stats.HighSessionScore, stats.CurrentStreak, stats.LastEntryDate,
	); err != nil {
		return Stats{}, false, err
	}

	if err := tx.Commit(); err != nil {
		return Stats{}, false, err
	}
	return stats, isNewHigh, nil
}

// RecordSessionPace persists a finished session's own average pace and the
// peak intensity ratio reached during it. Kept separate from FinishSession
// (rather than growing its parameter list) so the many existing
// FinishSession call sites across the test suite are unaffected — they
// simply leave these columns NULL, which RecentAvgPace and SearchSessions
// already treat as "no data recorded."
func (s *Store) RecordSessionPace(sessionID int64, avgPaceWPM, peakIntensityRatio float64) error {
	_, err := s.db.Exec(
		`UPDATE sessions SET avg_pace_wpm = ?, peak_intensity_ratio = ? WHERE id = ?`,
		avgPaceWPM, peakIntensityRatio, sessionID,
	)
	return err
}

const minBaselineSessions = 3

// RecentAvgPace averages avg_pace_wpm over the last n finished sessions
// that have it set (older sessions, from before pace tracking existed,
// have it NULL and are excluded). ok is false when fewer than
// minBaselineSessions such sessions exist — too little history for a
// meaningful personal baseline.
func (s *Store) RecentAvgPace(n int) (avgWPM float64, ok bool, err error) {
	rows, err := s.db.Query(`
		SELECT avg_pace_wpm FROM sessions
		WHERE ended_at IS NOT NULL AND avg_pace_wpm IS NOT NULL
		ORDER BY started_at DESC
		LIMIT ?`, n)
	if err != nil {
		return 0, false, err
	}
	defer rows.Close()

	var sum float64
	var count int
	for rows.Next() {
		var v float64
		if err := rows.Scan(&v); err != nil {
			return 0, false, err
		}
		sum += v
		count++
	}
	if err := rows.Err(); err != nil {
		return 0, false, err
	}
	if count < minBaselineSessions {
		return 0, false, nil
	}
	return sum / float64(count), true, nil
}

// ComputeStreak returns the consecutive-day streak for a session written on
// `today`, given the last entry date and streak recorded in stats. Pure and
// DB-free: same day returns the current streak unchanged, the day after
// increments it, any other gap (or no prior entry) restarts it at 1.
func ComputeStreak(lastEntryDate, today string, currentStreak int) int {
	if lastEntryDate == "" {
		return 1
	}
	if lastEntryDate == today {
		return currentStreak
	}
	last, err := time.Parse("2006-01-02", lastEntryDate)
	if err != nil {
		return 1
	}
	t, err := time.Parse("2006-01-02", today)
	if err != nil {
		return 1
	}
	if t.Sub(last) == 24*time.Hour {
		return currentStreak + 1
	}
	return 1
}

type SessionSearchResult struct {
	SessionRecord
	Snippet string
}

// escapeLike neutralizes SQLite's LIKE wildcards so a query the user typed
// is matched literally — without it, searching for "100%" or "foo_bar"
// silently matches far more than the writer asked for. Pairs with the
// `ESCAPE '\'` clause on every LIKE below.
func escapeLike(query string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)
}

// SearchSessions returns finished sessions whose entry text matches query
// (case-insensitive substring), most recent first, paginated by limit/offset.
// An empty query matches every entry via SQL's `LIKE '%%'`, so the same
// query shape handles both plain browsing and searching. total is the
// number of matching sessions across all pages.
func (s *Store) SearchSessions(query string, limit, offset int) ([]SessionSearchResult, int, error) {
	pattern := escapeLike(query)
	rows, err := s.db.Query(`
		SELECT
			s.id,
			s.started_at,
			s.session_score,
			COALESCE((SELECT SUM(e.word_count) FROM entries e WHERE e.session_id = s.id), 0),
			s.avg_pace_wpm,
			s.peak_intensity_ratio,
			(SELECT e.body FROM entries e WHERE e.session_id = s.id AND e.body LIKE '%' || ? || '%' ESCAPE '\' ORDER BY e.created_at ASC LIMIT 1)
		FROM sessions s
		WHERE s.ended_at IS NOT NULL
		  AND EXISTS (SELECT 1 FROM entries e WHERE e.session_id = s.id AND e.body LIKE '%' || ? || '%' ESCAPE '\')
		ORDER BY s.started_at DESC
		LIMIT ? OFFSET ?`, pattern, pattern, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []SessionSearchResult
	for rows.Next() {
		var r SessionSearchResult
		var avgPace, peakRatio sql.NullFloat64
		var snippet sql.NullString
		if err := rows.Scan(&r.ID, &r.StartedAt, &r.SessionScore, &r.WordCount, &avgPace, &peakRatio, &snippet); err != nil {
			return nil, 0, err
		}
		r.AvgPaceWPM = avgPace.Float64
		r.PeakIntensityRatio = peakRatio.Float64
		if query != "" && snippet.Valid {
			r.Snippet = truncateSnippet(snippet.String)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	var total int
	row := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM sessions s
		WHERE s.ended_at IS NOT NULL
		  AND EXISTS (SELECT 1 FROM entries e WHERE e.session_id = s.id AND e.body LIKE '%' || ? || '%' ESCAPE '\')`, pattern)
	if err := row.Scan(&total); err != nil {
		return nil, 0, err
	}

	return out, total, nil
}

func truncateSnippet(body string) string {
	const maxLen = 60
	r := []rune(body)
	if len(r) <= maxLen {
		return body
	}
	return string(r[:maxLen]) + "..."
}

type EntryRecord struct {
	ID        int64
	CreatedAt string
	Body      string
	WordCount int
}

// GetEntries returns a session's entries ordered by created_at ASC — the
// order they were written in.
func (s *Store) GetEntries(sessionID int64) ([]EntryRecord, error) {
	rows, err := s.db.Query(
		`SELECT id, created_at, body, word_count FROM entries WHERE session_id = ? ORDER BY created_at ASC`,
		sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []EntryRecord
	for rows.Next() {
		var r EntryRecord
		if err := rows.Scan(&r.ID, &r.CreatedAt, &r.Body, &r.WordCount); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetSession returns one session's stats row by ID. found is false when no
// such session exists, which callers report as "not found" rather than as a
// database error.
func (s *Store) GetSession(id int64) (rec SessionRecord, found bool, err error) {
	var avgPace, peakRatio sql.NullFloat64
	row := s.db.QueryRow(`
		SELECT
			s.id,
			s.started_at,
			s.session_score,
			COALESCE((SELECT SUM(e.word_count) FROM entries e WHERE e.session_id = s.id), 0),
			s.avg_pace_wpm,
			s.peak_intensity_ratio
		FROM sessions s
		WHERE s.id = ?`, id)
	switch err := row.Scan(&rec.ID, &rec.StartedAt, &rec.SessionScore, &rec.WordCount, &avgPace, &peakRatio); {
	case err == sql.ErrNoRows:
		return SessionRecord{}, false, nil
	case err != nil:
		return SessionRecord{}, false, err
	}
	rec.AvgPaceWPM = avgPace.Float64
	rec.PeakIntensityRatio = peakRatio.Float64
	return rec, true, nil
}

// AddUnscoredSession records a complete session in one shot: a session row,
// a single entry under it, and an ended_at — with a score of zero and no
// change to lifetime score, high score, or streak.
//
// The zero is the point. The writing screen blocks paste outright ("write it
// yourself"), and the combo multiplier that drives scoring measures typing
// rhythm over the last few seconds. Text that arrives through a pipe has no
// rhythm to measure, so there is no honest score to award it — and awarding
// one would make the lifetime score a number anything could inflate. Entries
// added this way are readable in history like any other; they just don't
// count toward the game.
func (s *Store) AddUnscoredSession(now time.Time, body string, wordCount int) (sessionID, entryID int64, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	at := timestamp(now)
	res, err := tx.Exec(
		`INSERT INTO sessions (started_at, ended_at, session_score, streak_bonus_applied) VALUES (?, ?, 0, 1.0)`,
		at, at,
	)
	if err != nil {
		return 0, 0, err
	}
	sessionID, err = res.LastInsertId()
	if err != nil {
		return 0, 0, err
	}

	res, err = tx.Exec(
		`INSERT INTO entries (session_id, created_at, body, word_count) VALUES (?, ?, ?, ?)`,
		sessionID, at, body, wordCount,
	)
	if err != nil {
		return 0, 0, err
	}
	entryID, err = res.LastInsertId()
	if err != nil {
		return 0, 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return sessionID, entryID, nil
}
