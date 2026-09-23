# Architecture

Two processes: a daemon owns the state and the agent sessions, clients (TUI, CLI)
talk to it over a unix socket. Killing a TUI never kills a session.

```
 pando (TUI)        pando session ls      any script
      │                    │                   │
      └──── JSON lines ────┴───────────────────┘
                           │  $XDG_RUNTIME_DIR/pando/pando.sock
                    ┌──────┴───────┐
                    │   daemon     │  config.toml · state.json
                    │  sessions[]  │──── PTY ──── claude / codex / shell
                    └──────────────┘      └─ vt.Emulator (screen + scrollback)
```

## Protocol

One request per connection, one response line back; `subscribe` keeps the
connection and streams events instead.

```
→ {"method":"session.new","params":{"workspace":"/repo","agent":"claude"}}
← {"result":{"id":"a1b2","agent":"claude",...}}

subscribe → {"event":"sessions"} {"event":"screen","id":"a1b2"} {"event":"state"} …
```

Methods: `ping state.get state.set draft.list draft.set project.add
project.remove project.move workspace.list workspace.new workspace.remove focus
session.new session.get session.list session.kill session.rename session.input
session.screen session.read session.wait update.status update.check
update.install subscribe shutdown`.

The API is the orchestration surface — sessions, workspaces, projects,
settings — not a mirror of the TUI. Explorer, Source Control, Search, diffs
and the editor run inside the client over `internal/git` and the filesystem,
which any script already reaches on its own.

`session.wait` is the one call that blocks: it returns when the session's
status is one of `until` (default `idle`, `blocked`, `exited`), when the
screen matches `match`, or when `timeout_ms` passes, so no client polls for a
turn to finish. Every other method answers at once.

Sessions resolve by id, by the name `session.new` or `session.rename` gave
them, or by an unambiguous prefix of either; a name is unique among live
sessions. A `parent` on `session.new` (resolved the same way, stored as the id)
makes a shell of that session's Terminal panel; `session.kill` on the parent
kills them with it. `skills/pando/SKILL.md`, which `pando skill` prints, is the guide an
agent reads before driving any of this.

`draft.list`/`draft.set` are the editors' unsaved text. They are the one pair
that does not broadcast: a draft is written on the TUI's tick, and a `state`
event per keystroke burst would have every client re-read the state.

Events are notifications, not data: the client re-reads what changed
(`state` → `state.get`, `sessions` → `session.list`, `screen` → `session.screen`).
`focus` and `update` are the exceptions and carry their payload, so a download's
progress needs no call per frame.

## Daemon

- Autostarted by any client (`proto.EnsureDaemon`), single instance via `flock`.
- `ping` returns the executable's build id; a TUI from a newer build restarts a
  stale daemon (silently when no session runs, otherwise it asks).
- Sessions: `creack/pty` + `charmbracelet/x/vt` emulator per session, each
  behind its own mutex. Output sets an "attention" flag on bell/OSC 777/long
  quiet runs. A process that exits with code 0 closes its session; a failure
  stays listed with its exit code.
- Restart respawns sessions from `state.json` (scrollback is lost).
- Updates: `pando serve` starts `WatchUpdates`, which asks GitHub for the
  latest release at startup and once a day after while `update_check` is on.
  `update.install` downloads it in the background and reports its progress as
  `update` events; see `internal/update`.

## Client

Bubble Tea model in `internal/ui`. One `Model` holds every view; messages come
from the daemon subscription, a 2 s tick (git status, preview reload, drafts,
open editors),
and the terminal (keys, mouse, resize).

```
tea.Msg ─▶ Model.Update ─▶ view state ─▶ Model.View ─▶ lines[] ─▶ terminal
   ▲                          │
   └── tea.Cmd (git, search, daemon calls in goroutines)
```

Everything expensive (git, ripgrep, daemon RPC) runs in a `tea.Cmd`, never in
`Update`, so the UI never blocks.

## Packages

| Path              | Contents                                                                                                        |
| ----------------- | --------------------------------------------------------------------------------------------------------------- |
| `main.go`         | CLI: `serve`, `stop`, `doctor`, `version`, `update`, `call`, `skill`, `project`, `ws`, `session`, `open`, `set` |
| `internal/proto`  | wire types, client calls, socket paths, daemon autostart                                                        |
| `internal/daemon` | state, config, sessions, PTY, event fan-out                                                                     |
| `internal/git`    | every git call (`git -C root …`), no libgit2                                                                    |
| `internal/lsp`    | language server client over stdio                                                                               |
| `internal/update` | GitHub release check, verified download, replacing the binary                                                   |
| `internal/ui`     | Bubble Tea model, views, preview, theme                                                                         |
