package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Draft is the live state of a session that hasn't ended yet: the text
// sitting in the editor which no entry row holds, plus the running score
// needed to pick the session back up. Exactly one row per session — it
// mirrors what's on screen rather than recording a history, so it stays the
// size of the current buffer no matter how long the session runs.
type Draft struct {
	SessionID  int64
	Body       string
	BodyWords  int
	RawScore   int
	TotalWords int
	UpdatedAt  string
}

// SaveDraft writes (or replaces) a session's draft. rawScore and totalWords
// are the session's running scoring totals, including work already committed
// as entries — they're what a resumed session starts from, since scoring
// state is otherwise reconstructible only from the combo history that the
// interruption destroyed.
func (s *Store) SaveDraft(sessionID int64, updatedAt time.Time, body string, rawScore, totalWords int) error {
	_, err := s.db.Exec(`
		INSERT INTO drafts (session_id, body, body_words, raw_score, total_words, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			body = excluded.body,
			body_words = excluded.body_words,
			raw_score = excluded.raw_score,
			total_words = excluded.total_words,
			updated_at = excluded.updated_at`,
		sessionID, body, len(strings.Fields(body)), rawScore, totalWords, timestamp(updatedAt),
	)
	return err
}

// GetDraft returns a session's draft. ok is false when none was ever
// written — a session interrupted before the first autosave, or one that
// predates drafts entirely.
func (s *Store) GetDraft(sessionID int64) (Draft, bool, error) {
	var d Draft
	err := s.db.QueryRow(
		`SELECT session_id, body, body_words, raw_score, total_words, updated_at FROM drafts WHERE session_id = ?`,
		sessionID,
	).Scan(&d.SessionID, &d.Body, &d.BodyWords, &d.RawScore, &d.TotalWords, &d.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Draft{}, false, nil
	}
	if err != nil {
		return Draft{}, false, err
	}
	return d, true, nil
}

// DeleteDraft drops a session's draft. Called once the session ends and its
// text lives in an entry row instead, so a finished session never shows up
// as recoverable.
func (s *Store) DeleteDraft(sessionID int64) error {
	_, err := s.db.Exec(`DELETE FROM drafts WHERE session_id = ?`, sessionID)
	return err
}

// UnfinishedSession is a session with no ended_at that still holds writing:
// the app was killed mid-session. Its writing is unreachable through History
// (every read query filters on ended_at IS NOT NULL) until the session is
// resumed and ended properly.
type UnfinishedSession struct {
	ID        int64
	StartedAt string
	// SavedWords is writing already committed as entries by a new-entry
	// keypress; DraftWords is what was still in the editor at the last
	// autosave.
	SavedWords int
	DraftWords int
}

func (u UnfinishedSession) TotalWords() int {
	return u.SavedWords + u.DraftWords
}

// UnfinishedSessions returns interrupted sessions that still hold writing,
// most recent first. Ones holding nothing are excluded rather than offered
// as recoverable — DiscardEmptyUnfinishedSessions sweeps those instead.
func (s *Store) UnfinishedSessions() ([]UnfinishedSession, error) {
	rows, err := s.db.Query(`
		SELECT
			s.id,
			s.started_at,
			COALESCE((SELECT SUM(e.word_count) FROM entries e WHERE e.session_id = s.id), 0),
			COALESCE((SELECT d.body_words FROM drafts d WHERE d.session_id = s.id), 0)
		FROM sessions s
		WHERE s.ended_at IS NULL
		ORDER BY s.started_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []UnfinishedSession
	for rows.Next() {
		var u UnfinishedSession
		if err := rows.Scan(&u.ID, &u.StartedAt, &u.SavedWords, &u.DraftWords); err != nil {
			return nil, err
		}
		if u.TotalWords() == 0 {
			continue
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// DiscardEmptyUnfinishedSessions removes interrupted sessions that hold no
// writing at all — opening the writing screen and walking away, or quitting
// before typing a word. Without this they accumulate forever, since nothing
// at runtime ever revisits an unfinished row. Returns how many were removed.
func (s *Store) DiscardEmptyUnfinishedSessions() (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// A draft of pure whitespace counts as empty: body_words is what the
	// writer would recognize as "something I wrote".
	res, err := tx.Exec(`
		DELETE FROM sessions
		WHERE ended_at IS NULL
		  AND NOT EXISTS (SELECT 1 FROM entries e WHERE e.session_id = sessions.id)
		  AND NOT EXISTS (SELECT 1 FROM drafts d WHERE d.session_id = sessions.id AND d.body_words > 0)`)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}

	// Drafts belonging to sessions that just went away, or to any session
	// removed before drafts existed as a concept.
	if _, err := tx.Exec(`DELETE FROM drafts WHERE session_id NOT IN (SELECT id FROM sessions)`); err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int(n), nil
}
