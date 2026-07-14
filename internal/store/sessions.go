package store

import "time"

type SessionRecord struct {
	ID           int64
	StartedAt    string
	SessionScore int
	WordCount    int
}

func (s *Store) StartSession(now time.Time) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO sessions (started_at, session_score, streak_bonus_applied) VALUES (?, 0, 1.0)`,
		now.Format(time.RFC3339),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) SaveEntry(sessionID int64, createdAt time.Time, body string, wordCount int) error {
	_, err := s.db.Exec(
		`INSERT INTO entries (session_id, created_at, body, word_count) VALUES (?, ?, ?, ?)`,
		sessionID, createdAt.Format(time.RFC3339), body, wordCount,
	)
	return err
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
		endedAt.Format(time.RFC3339), sessionScore, streakBonus, sessionID,
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

func (s *Store) ListSessions(limit int) ([]SessionRecord, error) {
	rows, err := s.db.Query(`
		SELECT s.id, s.started_at, s.session_score, COALESCE(SUM(e.word_count), 0)
		FROM sessions s
		LEFT JOIN entries e ON e.session_id = s.id
		WHERE s.ended_at IS NOT NULL
		GROUP BY s.id
		ORDER BY s.started_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SessionRecord
	for rows.Next() {
		var r SessionRecord
		if err := rows.Scan(&r.ID, &r.StartedAt, &r.SessionScore, &r.WordCount); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
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
