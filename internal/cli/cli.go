// Package cli is journal's non-interactive surface: the subcommands that
// read and write the journal without a terminal. Running `journal` bare
// still opens the TUI — everything here is for the cases the TUI can't
// serve, such as a script appending an entry or a program reading history
// back out.
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nd28/journal-tui/internal/store"
)

// Exit codes. Anything scripted against journal can tell "you asked wrong"
// (2) from "it went wrong" (1).
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// App holds the process context a command needs, so every command is
// testable without touching os.Stdin or the real database.
type App struct {
	Version string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
}

// Run dispatches args (os.Args[1:]) to a subcommand. handled is false when
// args name no subcommand at all, which means the caller should launch the
// TUI — that's the bare `journal` case, and it stays the default.
func (a App) Run(args []string) (exitCode int, handled bool) {
	if len(args) == 0 {
		return exitOK, false
	}

	switch args[0] {
	case "help", "--help", "-h":
		a.usage(a.Stdout)
		return exitOK, true
	case "version", "--version", "-v":
		fmt.Fprintf(a.Stdout, "journal v%s\n", a.Version)
		return exitOK, true
	case "add":
		return a.wrap(a.add(args[1:])), true
	case "list":
		return a.wrap(a.list(args[1:])), true
	case "show":
		return a.wrap(a.show(args[1:])), true
	case "stats":
		return a.wrap(a.stats(args[1:])), true
	}

	fmt.Fprintf(a.Stderr, "journal: unknown command %q\n\n", args[0])
	a.usage(a.Stderr)
	return exitUsage, true
}

// usageError marks a mistake in how the command was invoked, which exits 2
// rather than 1.
type usageError struct{ error }

func badUsage(format string, args ...any) error {
	return usageError{fmt.Errorf(format, args...)}
}

func (a App) wrap(err error) int {
	if err == nil {
		return exitOK
	}
	fmt.Fprintln(a.Stderr, "journal:", err)
	var ue usageError
	if errors.As(err, &ue) {
		return exitUsage
	}
	return exitError
}

func (a App) usage(w io.Writer) {
	fmt.Fprint(w, `journal — a gamified terminal journal

Usage:
  journal                       Open the interactive app
  journal add [text]            Record an entry (reads stdin if no text given)
  journal list [flags]          List finished sessions, newest first
  journal show <id> [flags]     Print one session's entries
  journal stats [flags]         Print lifetime score, high score, streak
  journal version
  journal help

Flags:
  --json                        Emit JSON instead of text (list, show, stats, add)
  --limit N                     Sessions to list (default 20)
  --offset N                    Sessions to skip (default 0)
  --query TEXT                  List only sessions whose text contains TEXT

Environment:
  JOURNAL_DB                    Database path (default ~/.journal/journal.db)

Entries added with `+"`journal add`"+` score zero and don't affect your streak:
the score measures typing rhythm, which piped text doesn't have.
`)
}

// flags builds a flag set that reports errors through the usage-error path
// instead of printing its own message and calling os.Exit.
func (a App) flags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	return fs
}

func (a App) open() (*store.Store, error) {
	s, err := store.OpenDefault()
	if err != nil {
		return nil, fmt.Errorf("could not open database: %w", err)
	}
	return s, nil
}

func (a App) emit(asJSON bool, v any, text func(io.Writer)) error {
	if !asJSON {
		text(a.Stdout)
		return nil
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// --- add ---------------------------------------------------------------

type addResult struct {
	SessionID int64  `json:"session_id"`
	EntryID   int64  `json:"entry_id"`
	WordCount int    `json:"word_count"`
	CreatedAt string `json:"created_at"`
	Scored    bool   `json:"scored"`
}

func (a App) add(args []string) error {
	fs := a.flags("add")
	asJSON := fs.Bool("json", false, "")
	file := fs.String("file", "", "")
	if err := fs.Parse(args); err != nil {
		return badUsage("add: %v", err)
	}

	body, err := a.addBody(fs.Args(), *file)
	if err != nil {
		return err
	}
	if strings.TrimSpace(body) == "" {
		return badUsage("add: nothing to write")
	}
	words := len(strings.Fields(body))

	s, err := a.open()
	if err != nil {
		return err
	}
	defer s.Close()

	now := time.Now()
	sessionID, entryID, err := s.AddUnscoredSession(now, body, words)
	if err != nil {
		return fmt.Errorf("could not save entry: %w", err)
	}

	res := addResult{
		SessionID: sessionID,
		EntryID:   entryID,
		WordCount: words,
		CreatedAt: now.UTC().Format(time.RFC3339),
		Scored:    false,
	}
	return a.emit(*asJSON, res, func(w io.Writer) {
		fmt.Fprintf(w, "saved session %d — %s (unscored)\n", res.SessionID, plural(words, "word", "words"))
	})
}

// addBody resolves the entry text from the three ways of supplying it:
// --file, trailing arguments, or standard input. "-" means stdin wherever a
// path or text would go, so `journal add -` reads a pipe explicitly.
func (a App) addBody(rest []string, file string) (string, error) {
	switch {
	case file != "" && len(rest) > 0:
		return "", badUsage("add: give --file or inline text, not both")

	case file == "-":
		return a.readStdin()

	case file != "":
		b, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("add: %w", err)
		}
		return string(b), nil

	case len(rest) == 1 && rest[0] == "-":
		return a.readStdin()

	case len(rest) > 0:
		return strings.Join(rest, " "), nil

	default:
		return a.readStdin()
	}
}

func (a App) readStdin() (string, error) {
	b, err := io.ReadAll(a.Stdin)
	if err != nil {
		return "", fmt.Errorf("add: could not read stdin: %w", err)
	}
	return string(b), nil
}

// --- list --------------------------------------------------------------

type sessionJSON struct {
	ID                 int64   `json:"id"`
	StartedAt          string  `json:"started_at"`
	Score              int     `json:"score"`
	WordCount          int     `json:"word_count"`
	AvgPaceWPM         float64 `json:"avg_pace_wpm"`
	PeakIntensityRatio float64 `json:"peak_intensity_ratio"`
	Snippet            string  `json:"snippet,omitempty"`
}

type listResult struct {
	Total    int           `json:"total"`
	Count    int           `json:"count"`
	Offset   int           `json:"offset"`
	Query    string        `json:"query"`
	Sessions []sessionJSON `json:"sessions"`
}

func (a App) list(args []string) error {
	fs := a.flags("list")
	asJSON := fs.Bool("json", false, "")
	limit := fs.Int("limit", 20, "")
	offset := fs.Int("offset", 0, "")
	query := fs.String("query", "", "")
	if err := fs.Parse(args); err != nil {
		return badUsage("list: %v", err)
	}
	if fs.NArg() > 0 {
		return badUsage("list: unexpected argument %q (did you mean --query?)", fs.Arg(0))
	}
	if *limit < 1 {
		return badUsage("list: --limit must be at least 1")
	}
	if *offset < 0 {
		return badUsage("list: --offset cannot be negative")
	}

	s, err := a.open()
	if err != nil {
		return err
	}
	defer s.Close()

	found, total, err := s.SearchSessions(*query, *limit, *offset)
	if err != nil {
		return fmt.Errorf("could not read sessions: %w", err)
	}

	res := listResult{Total: total, Count: len(found), Offset: *offset, Query: *query, Sessions: []sessionJSON{}}
	for _, r := range found {
		res.Sessions = append(res.Sessions, sessionJSON{
			ID:                 r.ID,
			StartedAt:          r.StartedAt,
			Score:              r.SessionScore,
			WordCount:          r.WordCount,
			AvgPaceWPM:         r.AvgPaceWPM,
			PeakIntensityRatio: r.PeakIntensityRatio,
			Snippet:            r.Snippet,
		})
	}

	return a.emit(*asJSON, res, func(w io.Writer) {
		if len(res.Sessions) == 0 {
			fmt.Fprintln(w, "no sessions")
			return
		}
		for _, r := range res.Sessions {
			fmt.Fprintf(w, "%-6d %-17s %6d pts  %s\n",
				r.ID, localTime(r.StartedAt), r.Score, plural(r.WordCount, "word", "words"))
			if r.Snippet != "" {
				fmt.Fprintf(w, "       %s\n", r.Snippet)
			}
		}
		fmt.Fprintf(w, "\n%d of %d\n", len(res.Sessions), res.Total)
	})
}

// --- show --------------------------------------------------------------

type entryJSON struct {
	ID        int64  `json:"id"`
	CreatedAt string `json:"created_at"`
	WordCount int    `json:"word_count"`
	Body      string `json:"body"`
}

type showResult struct {
	sessionJSON
	Entries []entryJSON `json:"entries"`
}

func (a App) show(args []string) error {
	fs := a.flags("show")
	asJSON := fs.Bool("json", false, "")
	if err := fs.Parse(args); err != nil {
		return badUsage("show: %v", err)
	}
	if fs.NArg() != 1 {
		return badUsage("show: need exactly one session id")
	}
	id, err := strconv.ParseInt(fs.Arg(0), 10, 64)
	if err != nil {
		return badUsage("show: %q is not a session id", fs.Arg(0))
	}

	s, err := a.open()
	if err != nil {
		return err
	}
	defer s.Close()

	rec, found, err := s.GetSession(id)
	if err != nil {
		return fmt.Errorf("could not read session: %w", err)
	}
	if !found {
		return fmt.Errorf("no session %d", id)
	}
	entries, err := s.GetEntries(id)
	if err != nil {
		return fmt.Errorf("could not read entries: %w", err)
	}

	res := showResult{
		sessionJSON: sessionJSON{
			ID:                 rec.ID,
			StartedAt:          rec.StartedAt,
			Score:              rec.SessionScore,
			WordCount:          rec.WordCount,
			AvgPaceWPM:         rec.AvgPaceWPM,
			PeakIntensityRatio: rec.PeakIntensityRatio,
		},
		Entries: []entryJSON{},
	}
	for _, e := range entries {
		res.Entries = append(res.Entries, entryJSON{
			ID:        e.ID,
			CreatedAt: e.CreatedAt,
			WordCount: e.WordCount,
			Body:      e.Body,
		})
	}

	return a.emit(*asJSON, res, func(w io.Writer) {
		fmt.Fprintf(w, "session %d — %s — %d pts — %s\n\n",
			res.ID, localTime(res.StartedAt), res.Score, plural(res.WordCount, "word", "words"))
		for i, e := range res.Entries {
			if i > 0 {
				fmt.Fprintln(w)
				fmt.Fprintln(w, "---")
				fmt.Fprintln(w)
			}
			fmt.Fprintln(w, strings.TrimRight(e.Body, "\n"))
		}
	})
}

// --- stats -------------------------------------------------------------

type statsResult struct {
	LifetimeScore    int    `json:"lifetime_score"`
	HighSessionScore int    `json:"high_session_score"`
	CurrentStreak    int    `json:"current_streak"`
	LastEntryDate    string `json:"last_entry_date"`
}

func (a App) stats(args []string) error {
	fs := a.flags("stats")
	asJSON := fs.Bool("json", false, "")
	if err := fs.Parse(args); err != nil {
		return badUsage("stats: %v", err)
	}
	if fs.NArg() > 0 {
		return badUsage("stats: unexpected argument %q", fs.Arg(0))
	}

	s, err := a.open()
	if err != nil {
		return err
	}
	defer s.Close()

	st, err := s.GetStats()
	if err != nil {
		return fmt.Errorf("could not read stats: %w", err)
	}
	res := statsResult{
		LifetimeScore:    st.LifetimeScore,
		HighSessionScore: st.HighSessionScore,
		CurrentStreak:    st.CurrentStreak,
		LastEntryDate:    st.LastEntryDate,
	}

	return a.emit(*asJSON, res, func(w io.Writer) {
		fmt.Fprintf(w, "lifetime score  %d\n", res.LifetimeScore)
		fmt.Fprintf(w, "high session    %d\n", res.HighSessionScore)
		fmt.Fprintf(w, "current streak  %s\n", plural(res.CurrentStreak, "day", "days"))
		if res.LastEntryDate != "" {
			fmt.Fprintf(w, "last entry      %s\n", res.LastEntryDate)
		}
	})
}

// --- shared ------------------------------------------------------------

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// localTime renders a stored UTC timestamp in the reader's own timezone.
// Unparseable values are shown as stored rather than guessed at.
func localTime(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.Local().Format("2006-01-02 15:04")
}
