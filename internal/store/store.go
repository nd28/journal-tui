package store

import (
	"database/sql"
	"strings"

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

CREATE TABLE IF NOT EXISTS stats (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	lifetime_score INTEGER NOT NULL DEFAULT 0,
	high_session_score INTEGER NOT NULL DEFAULT 0,
	current_streak INTEGER NOT NULL DEFAULT 0,
	last_entry_date TEXT NOT NULL DEFAULT ''
);

INSERT OR IGNORE INTO stats (id, lifetime_score, high_session_score, current_streak, last_entry_date)
VALUES (1, 0, 0, 0, '');
`

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
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// migrate adds columns introduced after the initial schema. SQLite's ALTER
// TABLE has no "IF NOT EXISTS", so a repeat run is expected to fail with
// "duplicate column name" on an already-migrated file — that specific error
// is swallowed; any other error is not.
func migrate(db *sql.DB) error {
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
