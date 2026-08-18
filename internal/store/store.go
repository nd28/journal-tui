package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS sessions (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	started_at TEXT NOT NULL,
	ended_at TEXT,
	session_score INTEGER NOT NULL DEFAULT 0,
	streak_bonus_applied REAL NOT NULL DEFAULT 1.0
);

CREATE TABLE IF NOT EXISTS entries (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id INTEGER NOT NULL REFERENCES sessions(id),
	created_at TEXT NOT NULL,
	body TEXT NOT NULL,
	word_count INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS drafts (
	session_id INTEGER PRIMARY KEY REFERENCES sessions(id),
	body TEXT NOT NULL,
	body_words INTEGER NOT NULL,
	raw_score INTEGER NOT NULL,
	total_words INTEGER NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS stats (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	lifetime_score INTEGER NOT NULL DEFAULT 0,
	high_session_score INTEGER NOT NULL DEFAULT 0,
	current_streak INTEGER NOT NULL DEFAULT 0,
	last_entry_date TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_entries_session_id ON entries(session_id);
CREATE INDEX IF NOT EXISTS idx_sessions_started_at ON sessions(started_at);

INSERT OR IGNORE INTO stats (id, lifetime_score, high_session_score, current_streak, last_entry_date)
VALUES (1, 0, 0, 0, '');
`

// dbFileMode keeps the journal readable only by its owner. The parent
// directory is already 0700, but SQLite creates the file itself at 0644.
const dbFileMode = 0o600

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	// Only now is the file guaranteed to exist: sql.Open is lazy, the schema
	// Exec is what creates it.
	if err := os.Chmod(path, dbFileMode); err != nil && !errors.Is(err, os.ErrNotExist) {
		db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// schemaVersion is the migration level this build expects, tracked in the
// database's PRAGMA user_version. Bump it and add a matching step below
// whenever stored data needs changing.
const schemaVersion = 2

// migrate brings an existing database up to schemaVersion. Steps are
// cumulative and each is safe to run against a brand-new (empty) database,
// so a fresh file simply runs them all as no-ops and lands at the current
// version.
func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version >= schemaVersion {
		return nil
	}

	if version < 1 {
		if err := addPaceColumns(db); err != nil {
			return err
		}
	}
	if version < 2 {
		if err := resetPaceData(db); err != nil {
			return err
		}
		if err := deleteAbandonedSessions(db); err != nil {
			return err
		}
		if err := normalizeTimestampsToUTC(db); err != nil {
			return err
		}
	}

	_, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion))
	return err
}

// addPaceColumns adds the pace columns introduced after the initial schema.
// SQLite's ALTER TABLE has no "IF NOT EXISTS", and databases written by the
// release that added these columns carry them already while still reporting
// user_version 0 — so "duplicate column name" is expected and swallowed.
// Any other error is not.
func addPaceColumns(db *sql.DB) error {
	stmts := []string{
		`ALTER TABLE sessions ADD COLUMN avg_pace_wpm REAL`,
		`ALTER TABLE sessions ADD COLUMN peak_intensity_ratio REAL`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			if strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
			return err
		}
	}
	return nil
}

// resetPaceData clears every recorded pace value. Values written before this
// migration measured wall-clock pace (total words ÷ session duration), which
// idle time drags toward zero, and some were inflated by clipboard pastes
// that the paste guard didn't catch. They aren't comparable with the
// active-typing pace recorded now, so mixing them would keep the personal
// baseline wrong. Clearing them costs a few sessions of history and lets the
// baseline rebuild from trustworthy readings.
func resetPaceData(db *sql.DB) error {
	_, err := db.Exec(`UPDATE sessions SET avg_pace_wpm = NULL, peak_intensity_ratio = NULL`)
	return err
}

// deleteAbandonedSessions removes rows left behind by opening the writing
// screen and leaving without writing anything. Entry-bearing sessions are
// never touched, so a session interrupted by a crash keeps its writing.
func deleteAbandonedSessions(db *sql.DB) error {
	_, err := db.Exec(`
		DELETE FROM sessions
		WHERE ended_at IS NULL
		  AND NOT EXISTS (SELECT 1 FROM entries e WHERE e.session_id = sessions.id)`)
	return err
}

// normalizeTimestampsToUTC rewrites stored timestamps that carry a local UTC
// offset. Every ORDER BY on these columns is a lexicographic string sort, so
// mixing offsets (or mixing "+05:30" rows with "Z" rows) silently mis-orders
// history. Rows that don't parse are left alone rather than guessed at.
func normalizeTimestampsToUTC(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, c := range []struct{ table, column string }{
		{"sessions", "started_at"},
		{"sessions", "ended_at"},
		{"entries", "created_at"},
	} {
		if err := normalizeColumnToUTC(tx, c.table, c.column); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func normalizeColumnToUTC(tx *sql.Tx, table, column string) error {
	// table and column are internal constants, never user input.
	rows, err := tx.Query(fmt.Sprintf(`SELECT id, %s FROM %s WHERE %s IS NOT NULL`, column, table, column))
	if err != nil {
		return err
	}

	type row struct {
		id  int64
		raw string
	}
	var pending []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.raw); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	// The rows must be fully drained and closed before running updates on
	// the same transaction.
	rows.Close()

	for _, r := range pending {
		t, err := time.Parse(time.RFC3339, r.raw)
		if err != nil {
			continue
		}
		utc := t.UTC().Format(time.RFC3339)
		if utc == r.raw {
			continue
		}
		if _, err := tx.Exec(
			fmt.Sprintf(`UPDATE %s SET %s = ? WHERE id = ?`, table, column),
			utc, r.id,
		); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

type Stats struct {
	LifetimeScore    int
	HighSessionScore int
	CurrentStreak    int
	LastEntryDate    string
}

func (s *Store) GetStats() (Stats, error) {
	var stats Stats
	row := s.db.QueryRow(`SELECT lifetime_score, high_session_score, current_streak, last_entry_date FROM stats WHERE id = 1`)
	err := row.Scan(&stats.LifetimeScore, &stats.HighSessionScore, &stats.CurrentStreak, &stats.LastEntryDate)
	return stats, err
}
