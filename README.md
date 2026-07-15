# journal

A gamified terminal journaling app: a live combo meter rewards uninterrupted
writing flow, a per-session score you try to beat, and a lifetime score that
only grows.

## Install

```
go install github.com/nd28/journal-tui/cmd/journal@latest
```

Requires Go 1.25+. This installs a `journal` binary to `$(go env GOPATH)/bin`
(make sure that's on your `PATH`).

## Run

```
journal
```

Data is stored in a local SQLite file at `~/.journal/journal.db`.

## Develop

```
make build   # build ./journal
make test    # go build + go vet + go test ./...
make run     # build and run
make install # go install ./cmd/journal
```
