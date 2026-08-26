package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nd28/journal-tui/internal/store"
)

// newApp returns an App wired to a throwaway database, plus its captured
// output streams. JOURNAL_DB is what makes this possible at all — without
// it every test would fight over the real journal in the user's home.
func newApp(t *testing.T, stdin string) (App, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	t.Setenv(store.DBPathEnv, filepath.Join(t.TempDir(), "test.db"))
	var out, errOut bytes.Buffer
	return App{
		Version: "test",
		Stdin:   strings.NewReader(stdin),
		Stdout:  &out,
		Stderr:  &errOut,
	}, &out, &errOut
}

func TestBareArgsLeavesTUIToCaller(t *testing.T) {
	app, _, _ := newApp(t, "")
	code, handled := app.Run(nil)
	if handled {
		t.Fatal("no args should fall through to the TUI, not be handled here")
	}
	if code != exitOK {
		t.Fatalf("exit code = %d, want %d", code, exitOK)
	}
}

func TestAddFromStdin(t *testing.T) {
	app, out, _ := newApp(t, "the fog came in on little cat feet")

	if code, _ := app.Run([]string{"add", "--json", "-"}); code != exitOK {
		t.Fatalf("add exit code = %d", code)
	}

	var res addResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("add --json did not emit JSON: %v", err)
	}
	if res.WordCount != 8 {
		t.Errorf("word count = %d, want 8", res.WordCount)
	}
	if res.Scored {
		t.Error("piped entries must not be scored")
	}
	if res.SessionID == 0 || res.EntryID == 0 {
		t.Errorf("got zero ids: %+v", res)
	}
}

// The scoring stance is the whole reason add exists in this shape: text that
// arrives through a pipe earns nothing, so nothing can inflate the lifetime
// score by piping into it.
func TestAddLeavesScoreAndStreakAlone(t *testing.T) {
	app, out, _ := newApp(t, "")

	if code, _ := app.Run([]string{"add", "a handful of words here"}); code != exitOK {
		t.Fatalf("add exit code = %d (%s)", code, out.String())
	}
	out.Reset()

	if code, _ := app.Run([]string{"stats", "--json"}); code != exitOK {
		t.Fatalf("stats exit code = %d", code)
	}
	var st statsResult
	if err := json.Unmarshal(out.Bytes(), &st); err != nil {
		t.Fatalf("stats --json: %v", err)
	}
	if st.LifetimeScore != 0 || st.HighSessionScore != 0 || st.CurrentStreak != 0 {
		t.Errorf("add moved the game stats: %+v", st)
	}
	if st.LastEntryDate != "" {
		t.Errorf("add set last entry date to %q", st.LastEntryDate)
	}
}

func TestAddInlineTextAndRoundTrip(t *testing.T) {
	app, out, _ := newApp(t, "")

	if code, _ := app.Run([]string{"add", "hello", "there"}); code != exitOK {
		t.Fatalf("add exit code = %d", code)
	}
	out.Reset()

	if code, _ := app.Run([]string{"list", "--json"}); code != exitOK {
		t.Fatalf("list exit code = %d", code)
	}
	var lr listResult
	if err := json.Unmarshal(out.Bytes(), &lr); err != nil {
		t.Fatalf("list --json: %v", err)
	}
	if lr.Total != 1 || len(lr.Sessions) != 1 {
		t.Fatalf("list = %+v, want one session", lr)
	}
	id := lr.Sessions[0].ID
	out.Reset()

	if code, _ := app.Run([]string{"show", "--json", itoa(id)}); code != exitOK {
		t.Fatalf("show exit code = %d", code)
	}
	var sr showResult
	if err := json.Unmarshal(out.Bytes(), &sr); err != nil {
		t.Fatalf("show --json: %v", err)
	}
	if len(sr.Entries) != 1 || sr.Entries[0].Body != "hello there" {
		t.Fatalf("show = %+v, want the text back verbatim", sr.Entries)
	}
}

func TestAddRejectsBlankInput(t *testing.T) {
	app, _, errOut := newApp(t, "   \n\t\n")
	code, _ := app.Run([]string{"add", "-"})
	if code != exitUsage {
		t.Fatalf("exit code = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errOut.String(), "nothing to write") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

// An empty journal must still emit a parseable empty list rather than a
// JSON null, which is the shape that breaks callers iterating the result.
func TestListEmptyIsEmptyArray(t *testing.T) {
	app, out, _ := newApp(t, "")
	if code, _ := app.Run([]string{"list", "--json"}); code != exitOK {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(out.String(), `"sessions": []`) {
		t.Errorf("want an empty array, got %s", out.String())
	}
}

func TestListQueryFilters(t *testing.T) {
	app, out, _ := newApp(t, "")
	app.Run([]string{"add", "writing about otters"})
	app.Run([]string{"add", "writing about kestrels"})
	out.Reset()

	if code, _ := app.Run([]string{"list", "--json", "--query", "otters"}); code != exitOK {
		t.Fatalf("exit code = %d", code)
	}
	var lr listResult
	if err := json.Unmarshal(out.Bytes(), &lr); err != nil {
		t.Fatalf("list --json: %v", err)
	}
	if lr.Total != 1 {
		t.Fatalf("total = %d, want 1", lr.Total)
	}
	if !strings.Contains(lr.Sessions[0].Snippet, "otters") {
		t.Errorf("snippet = %q", lr.Sessions[0].Snippet)
	}
}

func TestShowMissingSessionFails(t *testing.T) {
	app, _, errOut := newApp(t, "")
	code, _ := app.Run([]string{"show", "404"})
	if code != exitError {
		t.Fatalf("exit code = %d, want %d", code, exitError)
	}
	if !strings.Contains(errOut.String(), "no session 404") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestUsageErrorsExitTwo(t *testing.T) {
	cases := [][]string{
		{"show"},
		{"show", "abc"},
		{"list", "--limit", "0"},
		{"list", "--offset", "-1"},
		{"list", "otters"},
		{"stats", "extra"},
		{"nonsense"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			app, _, _ := newApp(t, "")
			code, handled := app.Run(args)
			if !handled {
				t.Fatal("should have been handled")
			}
			if code != exitUsage {
				t.Errorf("exit code = %d, want %d", code, exitUsage)
			}
		})
	}
}

func TestHelpAndVersion(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}} {
		app, out, _ := newApp(t, "")
		if code, _ := app.Run(args); code != exitOK {
			t.Errorf("%v exit code = %d", args, code)
		}
		if !strings.Contains(out.String(), "journal add") {
			t.Errorf("%v printed no usage: %q", args, out.String())
		}
	}
	for _, args := range [][]string{{"version"}, {"--version"}, {"-v"}} {
		app, out, _ := newApp(t, "")
		if code, _ := app.Run(args); code != exitOK {
			t.Errorf("%v exit code = %d", args, code)
		}
		if !strings.Contains(out.String(), "journal vtest") {
			t.Errorf("%v printed %q", args, out.String())
		}
	}
}

func TestAddFromFile(t *testing.T) {
	app, out, _ := newApp(t, "")
	path := filepath.Join(t.TempDir(), "entry.txt")
	if err := writeFile(path, "from a file on disk"); err != nil {
		t.Fatal(err)
	}
	if code, _ := app.Run([]string{"add", "--json", "--file", path}); code != exitOK {
		t.Fatalf("exit code = %d", code)
	}
	var res addResult
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("add --json: %v", err)
	}
	if res.WordCount != 5 {
		t.Errorf("word count = %d, want 5", res.WordCount)
	}
}

func TestAddRejectsFileAndInlineTogether(t *testing.T) {
	app, _, _ := newApp(t, "")
	code, _ := app.Run([]string{"add", "--file", "x.txt", "inline words"})
	if code != exitUsage {
		t.Fatalf("exit code = %d, want %d", code, exitUsage)
	}
}
