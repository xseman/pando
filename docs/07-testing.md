# Testing

How pando is tested, checked by hand in tmux, and how the demo GIFs are
recorded.

```sh
make test                              # go vet + go test -race ./... (CI)
make lint                              # golangci-lint (CI)
go test -short ./...                   # skip the PTY end-to-end test
go test ./internal/ui -run TestName    # one test
```

## Layers

```text
 cheap ────────────────────────────────────────────────────────▶ whole binary
 ┌──────────────┐ ┌───────────┐ ┌──────────────┐ ┌────────────┐ ┌───────────┐
 │ unit + model │ │ fuzz      │ │ daemon over  │ │ git on a   │ │ PTY e2e   │
 │ testModel,   │ │ byte      │ │ a real unix  │ │ temp repo, │ │ TestE2E,  │
 │ press, click │ │ parsers   │ │ socket       │ │ fake gh    │ │ main_test │
 └──────────────┘ └───────────┘ └──────────────┘ └────────────┘ └───────────┘
```

| Layer             | Where                   | Catches                                     |
| ----------------- | ----------------------- | ------------------------------------------- |
| unit + model      | `internal/{git,lsp,ui}` | parsing, geometry, rendering, LSP framing   |
| daemon            | `internal/daemon`       | protocol, settings, config reload, sessions |
| git, fake `gh`    | `internal/{git,ui}`     | real git commands; gh calls, offline        |
| PTY               | `main_test.go`          | autostart, keys, mouse, redraw              |

`TestE2E` re-execs the test binary as `pando` (`PANDO_TEST_MAIN=1`) in a PTY,
renders it with x/vt and asserts on the screen.

## UI tests

```go
m := testModel(t)             // 100x20, a temp workspace
press(m, "2", "down")
click(m, x, y, tea.MouseLeft)
out := checkWidths(t, m)      // every line exactly m.w wide
```

Assert on stripped text, or on SGR parameters (`bgParams(pal.selBg)`) when
color is the point.

## Helpers

| Helper                         | Does                                        |
| ------------------------------ | ------------------------------------------- |
| `mustWrite(t, path, content)`  | writes a file and its parent directories    |
| `mustRead(t, path) string`     | reads a file back                           |
| `mustGit(t, root, args…)`      | runs one git command in root                |
| `touch(t, path)`               | dates a file an hour ahead (foreign edit)   |
| `fire(m, cmd)`                 | runs a `tea.Cmd`, feeds its message back    |
| `start(t)` (daemon)            | scratch dirs; returns a boot func           |
| `call(t, method, params, out)` | one request to the test daemon              |
| `waitFor(t, what, cond)`       | polls cond until it holds                   |
| `repo(t)` (git)                | an empty repository with an identity        |
| `fakeGH(t, script)` (git)      | a gh on `PATH` running script, logging args |

- They fail with `t.Fatalf("<op> %s: %v", what, err)`, naming the step.
- Define one only where it is used; an unused helper fails `make lint`.
- `t.Fatal` is illegal off the test goroutine: `TestConcurrentState` reports
  with `t.Errorf`.

## Fuzzing

Targets: `parseStatus`, `pickLines`, `ConflictText`, `parseSuggestion`, the
merge message (`internal/git`); `Columns` JSON/TOML (`internal/proto`);
transcript titles (`internal/daemon`); `checksum` (`internal/update`);
`backgroundID` (`main`).

```sh
go test -run xxx -fuzz FuzzParseStatus -fuzztime 30s ./internal/git
```

Each asserts an invariant, e.g. every `parseStatus` path is a substring of
the input. Crashers in `testdata/fuzz/` replay in `go test`: commit them.

## Interactive checks

```sh
tmux new-session -d -s gv -x 150 -y 40 "env -u NO_COLOR \
	PANDO_RUNTIME_DIR=/tmp/gvt/run PANDO_CONFIG_DIR=/tmp/gvt/cfg \
	PANDO_DATA_DIR=/tmp/gvt/data pando"
tmux send-keys -t gv -l $'\e[<0;10;5M\e[<0;10;5m'   # click col 10, row 5
tmux capture-pane -p -t gv                          # -e keeps colors
```

- Socket paths cap at ~104 bytes: keep `PANDO_RUNTIME_DIR` short.
- Click first, then send keys: focus decides where they go.
- `capture-pane -e` maps truecolor to 256 colors.
- Nerd Font glyphs print blank: check codepoints.
- The dev machine signs tags: `git -c tag.gpgSign=false tag`.
- `pando stop` before removing a runtime directory. In a test use
  `t.TempDir()`: `defer os.RemoveAll` runs before `t.Cleanup`, removes the
  socket, and the shutdown call never reaches the daemon.

## Demo recordings

Each GIF in `docs/demo` is its `*.tape` run by
[VHS](https://github.com/charmbracelet/vhs) v0.10 or v0.11 (v0.12 writes no
GIF, [vhs#787](https://github.com/charmbracelet/vhs/issues/787)). Also needs
`ttyd`, `ffmpeg`, Chrome, `jq`, `gopls`, `claude`.

```text
make demo ─▶ make install          overwrites $(PREFIX)/bin/pando
   ├─ PAR_TAPES, JOBS=4 at once    markdown lsp vim
   │    own /tmp/pando-<tape>/ repo and daemon
   └─ SEQ_TAPES, one at a time     cli diff edit github tui resume
        share /tmp/pando-demo (claude trusts it; its path is on screen)
   after each tape: pando stop
```

- `make demo-lsp` records one tape.
- Private build: `env PATH=$P/bin:$PATH make demo PREFIX=$P`.

| File            | Role                                                        |
| --------------- | ----------------------------------------------------------- |
| `settings.tape` | 98×27 cells, VS Code Light theme, `bg.png` (made by `make`) |
| `setup.tape`    | hidden start: clean env, fresh repo, `source cfg.sh`        |
| `cfg.sh`        | `cfg <width> [preset…] [key=command…]`; `mode=light`        |
| `gh/gh`         | fake gh for `github.tape`                                   |

Tape rules:

- `Set Theme` and `mode` paint terminal and pando: change both together.
- `lsp.tape` writes `lsp.png` with `Screenshot`, then `Sleep`: vhs grabs the
  next frame, which already shows the next key.
- Agents start from the CLI (`session new`, `session wait --until`), hidden
  except in `cli.tape`; `Hide`/`Show` cuts the awaited turn.
- vhs cannot send `F12`, `ctrl+.`, `ctrl+s`, `ctrl+space`, `alt+shift`: bind
  to a ctrl letter (`cfg 26 ctrl+k=editor.saveFile`).
- Inside a session every key goes to the agent, `[keys]` bindings included.
- `cfg … claude-ask` for a permission prompt.
- Wait on CLI output or claude's answer, never the welcome screen.
  `Wait+Screen` matches the whole screen: `^` is its start.
- Page (`PageDown`); line scrolling costs a frame per line.
