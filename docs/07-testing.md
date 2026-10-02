# Testing

```
make test               go vet + go test -race ./... (what CI runs)
make lint               golangci-lint, config in .golangci.yml (also CI)
go test -short ./...    skips the PTY end-to-end test
```

Six layers, each catching a different class of bug:

| Layer                         | Where                   | Catches                                                    |
| ----------------------------- | ----------------------- | ---------------------------------------------------------- |
| unit                          | `internal/{git,lsp,ui}` | parsing, geometry, rendering, fuzzy matching, LSP framing  |
| fuzz                          | every byte parser       | parsers fed bytes no test would think to write             |
| daemon over a real socket     | `internal/daemon`       | protocol, settings merge, config reload, session lifecycle |
| git against temp repositories | `internal/git`          | every command pando runs, on real git                      |
| gh through a fake gh on PATH  | `internal/{git,ui}`     | every gh call's arguments, answers and failures, offline   |
| TUI in a PTY                  | `main_test.go`          | the whole binary: daemon autostart, keys, mouse, redraw    |

## Fuzzing

The pure parsers are fuzzed:

| Package           | Targets                                                                          |
| ----------------- | -------------------------------------------------------------------------------- |
| `internal/git`    | `parseStatus`, `pickLines`, `ConflictText`, `parseSuggestion`, the merge message |
| `internal/proto`  | the `Columns` JSON and TOML decoders                                             |
| `internal/daemon` | transcript titles                                                                |
| `internal/update` | `CHECKSUMS.txt`                                                                  |
| `main`            | `backgroundID`, the id `claude --bg` prints                                      |

```
go test -run xxx -fuzz FuzzParseStatus -fuzztime 30s ./internal/git
```

A target that only checks "did not panic" is weak, so each asserts a real
invariant — every `parseStatus` path is a non-empty substring of the input;
selecting every line of a diff with `fromOld=true` equals selecting none with
`fromOld=false`. That is what caught the porcelain record too short to hold a
path, which parsed to an entry naming the repository root — `Discard` would
then have removed the whole worktree.

Crashers live in `testdata/fuzz/` and are replayed by a plain `go test`, so
they are regression tests: commit them.

## Rendering tests

The model is driven directly — no terminal needed:

```go
m := testModel(t)          // 100x20, a temp workspace
press(m, "2", "down")      // keys
click(m, x, y, tea.MouseLeft)
out := checkWidths(t, m)   // renders and asserts every line is exactly m.w wide
```

`checkWidths` is the cheap invariant that catches most layout regressions; the
rest assert on the stripped text or on SGR parameters (`bgParams(pal.selBg)`)
when a color is the point.

## Test helpers

Every package that needs one defines the same filesystem and git helpers,
with the same names and the same failure style:

| Helper                        | Does                                              |
| ----------------------------- | ------------------------------------------------- |
| `mustWrite(t, path, content)` | writes a file and the directories above it        |
| `mustRead(t, path) string`    | reads a file the test is about to assert on       |
| `mustGit(t, root, args…)`     | runs one git command in root                      |
| `touch(t, path)`              | dates a file an hour ahead, faking a foreign edit |
| `fire(m, cmd)`                | runs a `tea.Cmd` and feeds its message back (ui)  |

They call `t.Helper()` and fail with `t.Fatalf("<op> %s: %v", <what>, err)` —
never a bare `t.Fatal(err)`, which leaves you guessing which of six writes
blew up. Add a helper only to the package that uses it; an unused one fails
`make lint`. `t.Fatal` is illegal off the test goroutine, so concurrency tests
such as `TestConcurrentState` call `proto.Call` directly and report with
`t.Errorf`.

## End-to-end

`TestE2E` runs the test binary as `pando` (it re-execs itself through
`PANDO_TEST_MAIN`), gives it a PTY and renders the output with pando's own vt
emulator, then asserts on the screen text and sends SGR mouse sequences.

## Interactive checks

Real terminals still surface things tests cannot (fonts, colors, tmux):

```bash
tmux new-session -d -s gv -x 150 -y 40 \
  "env -u NO_COLOR PANDO_RUNTIME_DIR=/tmp/gvt/run PANDO_CONFIG_DIR=/tmp/gvt/cfg \
   PANDO_DATA_DIR=/tmp/gvt/data pando"
tmux send-keys -t gv -l $'\e[<0;10;5M\e[<0;10;5m'   # left click at col 10, row 5
tmux capture-pane -p -t gv                          # text, -e keeps the colors
```

Gotchas learned the hard way:

- Unix socket paths are capped at ~104 bytes: use a short `PANDO_RUNTIME_DIR`.
- Focus decides where keys go — click first, then send keys.
- Terminals map truecolor to 256 colors in `capture-pane -e`; grep the
  approximation, not the `#rrggbb`.
- Nerd Font glyphs look empty in tool output; check codepoints, not shapes.
- This machine signs tags, so tests need `git -c tag.gpgSign=false tag`.
- Kill the test daemon (`pando stop`) before removing its runtime directory.
  In a test the runtime directory has to be a `t.TempDir()`: a `defer
  os.RemoveAll` runs before every `t.Cleanup`, so it takes the socket away
  before the shutdown call can reach the daemon, and the daemon is orphaned.

## Demo recordings

Every GIF in `docs/demo` comes from its `*.tape`, replayed by
[VHS](https://github.com/charmbracelet/vhs) against a throwaway repository in
`/tmp/pando-demo`. Needs `vhs` v0.10 or v0.11 (v0.12 records but writes no
GIF, [vhs#787](https://github.com/charmbracelet/vhs/issues/787)), `ttyd`,
`ffmpeg`, a Chrome, `jq`, `gopls` and `claude`. Every tape sources
`settings.tape` and `setup.tape`, so a tape names only what makes it different.

```
make demo          # PAR_TAPES in parallel (JOBS, 4), then SEQ_TAPES one at a time
make demo-lsp      # one tape
```

A parallel tape gets a repository and a daemon of its own (`PANDO_DEMO_REPO`,
`PANDO_DEMO_STATE`). A serial one shares `/tmp/pando-demo`: it runs claude,
which must be trusted by hand in every new directory, or its path is on
screen. Each recipe stops its tape's daemon afterwards. A claude tape starts
its agents from the CLI (`session new`, `session wait --until`, `send`),
hidden except in `cli.tape`, and cuts the turn it waits for with
`Hide`/`Show`, so no GIF idles on a spinner.
`github.tape` puts `gh/gh`, a fake answering from the files beside it, on the
PATH. What bites:

- A GIF is 98×27 cells (`settings.tape`), so `cfg` keeps every panel on the
  left and the session over the editor area (`session_position = "editor"`):
  docked beside the editor, a session and a file are both too narrow to
  read. A file opened shows in front of the session, its Spaces row brings
  the session back.
- VHS cannot send `F12`, `ctrl+.`, `ctrl+s`, `ctrl+space` or `alt+shift`
  chords: a tape binds those commands to `ctrl` letters in its own
  `config.toml` (`cfg 26 ctrl+k=editor.saveFile`).
- A `[keys]` binding never fires inside a session, so a tape that leaves the
  focus there types every following key into the agent. `1`–`4` need a
  focused sidebar for the same reason.
- An editable file types `5` and `[` rather than toggling the terminal or
  cycling tabs: reach them from a sidebar (`ctrl+]` from the Terminal).
- Switching a project already shows its session, so `⏎` on its Spaces row
  hides it again. `⏎` on another session's row opens it and hands it the
  keys; `ctrl+]` leads back to the tree. An open file keeps `ctrl+]` (it
  indents).
- `b` hides the focused column and the focus falls to main; `ctrl+b` brings
  it back. `ctrl+0` takes the first open column, the session's when the
  sidebar is hidden.
- A tape reaches claude's permission prompt only with `cfg … claude-ask`,
  agent `ask` (permission mode `default`), whatever the recording machine's
  settings.json allows.
- claude records in its classic renderer (`cfg.sh` passes `"tui": "default"`):
  a fullscreen claude draws on the alternate screen and matches no wait.
- A tape waits on the CLI's output or on what claude answers (a word of it,
  its `●` reply marker), never on the welcome screen, whose hints change
  between claude versions. `Wait+Screen` matches the whole screen as one
  string, so `^` is its start, not a line's.
- A resume command is typed into a shell unquoted, so `cfg` hands claude its
  settings as a file (`$PANDO_CONFIG_DIR/claude.json`), not inline JSON.
- A screen that scrolls line by line costs a full GIF frame per line: page
  instead (`PageDown`), and hold the result.
