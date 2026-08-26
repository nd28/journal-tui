package store

import (
	"os"
	"path/filepath"
)

// DBPathEnv overrides where the journal database lives. Without it the
// database sits under the user's home directory, which is fine for a person
// with one journal but awkward for anything else — a test run, a scripted
// import, a second journal kept apart from the daily one.
const DBPathEnv = "JOURNAL_DB"

// DefaultPath returns the database path to use, honouring DBPathEnv. It does
// not create anything; see EnsureDir.
func DefaultPath() (string, error) {
	if p := os.Getenv(DBPathEnv); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".journal", "journal.db"), nil
}

// EnsureDir creates the directory holding path, owner-only. The database
// itself is chmodded by Open; this covers the directory it lands in.
func EnsureDir(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o700)
}

// OpenDefault resolves DefaultPath, creates its directory, and opens the
// store — the three steps every entry point needs, in the one order that
// works.
func OpenDefault() (*Store, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	if err := EnsureDir(path); err != nil {
		return nil, err
	}
	return Open(path)
}
