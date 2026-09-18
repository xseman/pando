# pando

Terminal workspace for AI agent sessions: one Go daemon owns PTY sessions in
git worktrees, the TUI and CLI are clients over a unix socket. Read `README.md`
for what it does; read a doc below only when the task touches that area.

## Commands

```sh
make build              # go build -o pando .
make test               # go vet + go test -race ./...
make lint               # golangci-lint, config in .golangci.yml — must be clean
make fmt                # gofumpt -w .
go test -short ./...    # skip the PTY end-to-end test (main_test.go)
go test ./internal/ui -run TestName
make demo               # re-record docs/demo/*.gif with vhs (slow, needs claude, gopls)
```

`make test` and `make lint` are what CI runs (`.github/workflows/ci.yml`).

Go 1.26, stdlib + charm (bubbletea v2, lipgloss v2, x/vt), goldmark, chroma,
BurntSushi/toml, creack/pty. No cgo, no libgit2: git is shelled out.

## Layout

| Path              | Contents                                                                                                        |
| ----------------- | --------------------------------------------------------------------------------------------------------------- |
| `main.go`         | CLI: `serve`, `stop`, `doctor`, `version`, `update`, `call`, `skill`, `project`, `ws`, `session`, `open`, `set` |
| `internal/proto`  | wire types, client calls, socket paths, daemon autostart                                                        |
| `internal/daemon` | state, config.toml, sessions, PTY, event fan-out                                                                |
| `internal/git`    | every `git -C root …` call, parsed once                                                                         |
| `internal/lsp`    | language server client                                                                                          |
| `internal/update` | release check, verified download, self-update                                                                   |
| `internal/ui`     | Bubble Tea model (`app.go`), views, preview/editor, theme                                                       |
| `skills/pando`    | `SKILL.md`, the agent-facing guide; `pando skill` embeds and prints it                                          |

## Docs (read on demand)

| When working on…                                                    | Read                                     |
| ------------------------------------------------------------------- | ---------------------------------------- |
| daemon, socket protocol, sessions, resume                           | `docs/01-architecture.md`                |
| columns, rows, mouse hit-testing, modals, drag                      | `docs/02-ui-layout.md`                   |
| Explorer, Source Control, Spaces, Search, commands                  | `docs/03-views.md`                       |
| diffs, editor, Markdown, LSP, suggestions, staging lines, revisions | `docs/04-preview.md`                     |
| anything calling git                                                | `docs/05-git.md`                         |
| config.toml, state.json, settings, keys, colors                     | `docs/06-config.md`                      |
| writing tests, tmux checks, gotchas                                 | `docs/07-testing.md`                     |
| driving pando from an agent, session lifecycle over the CLI         | `skills/pando/SKILL.md`                  |
| every key, per view and in a file                                   | `docs/09-keys.md`                        |
| GIFs / tapes                                                        | README "Development", `docs/demo/*.tape` |
| releases, install script, self-update                               | `docs/08-release.md`                     |

## Conventions

- Views share one shape: build rows → render into a windowed list → handle keys
  and mouse. Follow the existing view in `internal/ui` before inventing.
- UI never blocks: long work runs in `tea.Cmd`s, results come back as messages.
- The API is the orchestration surface, not a mirror of the TUI: sessions,
  workspaces, projects and settings belong in it, and a change to any of them
  adds the proto type first. Explorer, Source Control, Search, diffs and the
  editor stay TUI-only — they are views over git and the filesystem, which a
  script already has. Do not add a method whose only caller would be the TUI.
- A session is driven by a script as much as by a human: anything that makes a
  caller poll is a missing method. `session.wait` blocks on status or a screen
  match; keep it that way rather than growing a status endpoint to poll.
- `skills/pando/SKILL.md` is the contract an agent reads before its first call.
  A change to the session API updates it, and `pando help` keeps pointing at
  it. Keep it behavioural — states, waits, safety — not a flag dump.
- Tests: drive the model directly (`testModel`, `press`, `click`, `checkWidths`),
  no terminal. Daemon tests use a real socket, git tests a temp repository.
  Setup goes through the shared `mustWrite`/`mustRead`/`mustGit`
  helpers so a failed step fails where it happened; see `docs/07-testing.md`.
  A parser that takes bytes and does no I/O gets a fuzz target with a real
  invariant, not just "did not panic"; keep the `testdata/fuzz` crashers.
- Config changes go through the daemon so `config.toml` keeps its comments and
  every attached TUI gets the `state` event.
- A change in behaviour updates `README.md` (user-facing) or `docs/` (how it
  works). Keep both terse; tables over prose.
- `make lint` must stay clean. Never silence a linter to get there: fix the
  code, or write `//nolint:<linter> // <reason>` — `nolintlint` rejects a
  directive without both. A deliberately dropped error reads `_ = f()`, with a
  short comment when the reason is not obvious from the line.
- Doc comments start with the identifier and end with a period. Exported API
  gets one; so does anything whose behaviour the name does not give away.
- `// ponytail:` marks a deliberate shortcut left for later. Keep them true —
  they are harvested as a debt ledger, so a stale one is worse than none.
- Format with `make fmt` (gofumpt). Tabs everywhere (Go, Makefile, Markdown
  tables aligned with spaces).
- Code reads in paragraphs: a step and its check sit together, a blank line
  separates it from the next step, from a `case` longer than two lines, and
  from a closing `return`. A block (`if`, `for`, `switch`) cuddles only with
  the one line it uses. `wsl_v5` and `nlreturn` enforce it; `golangci-lint
  run --fix` inserts the lines.

## Gotchas

- Unix socket paths cap at ~104 bytes: tests and manual runs need a short
  `PANDO_RUNTIME_DIR`.
- Always set `PANDO_RUNTIME_DIR`, `PANDO_CONFIG_DIR`, `PANDO_DATA_DIR` when
  running pando by hand, or you talk to the real daemon.
- `pando stop` before removing a test runtime directory. In a test the
  runtime directory must be a `t.TempDir()`: a `defer os.RemoveAll` runs
  before every `t.Cleanup`, so it takes the socket away and the shutdown
  call silently leaves the daemon running.
- Nerd Font glyphs look empty in tool output: check codepoints, not shapes.
- Tests run on a machine that signs tags: `git -c tag.gpgSign=false tag`.
