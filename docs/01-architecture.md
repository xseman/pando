# Architecture

How the daemon, its sessions and its clients fit together: the socket
protocol, how a session's status is decided, and how sessions come back after
a restart. The TUI is in [02-ui-layout.md](02-ui-layout.md).

- [Processes](#processes) · [Protocol](#protocol) · [Sessions](#sessions) ·
  [Status](#status) · [Resume after a restart](#resume-after-a-restart)

## Processes

One daemon owns all state and every PTY; the TUI and the CLI are clients.
Killing a TUI never kills a session.

```text
 pando (TUI)        pando session …        any script
     │                    │                     │
     └─────── JSON lines over a unix socket ────┘
                          │  <runtime dir>/pando.sock
 ┌────────────────────────┴─────────────────────────────┐
 │ daemon: pando serve      state.json · config.toml    │
 │  500 ms ticker: status, foreground, resume, config   │
 │  session ── PTY ── shell ── claude / codex / …       │
 │     └─ vt.Emulator: screen + 10 000 lines scrollback │
 │  broadcast ──▶ every subscribe connection            │
 └──────────────────────────┬───────────────────────────┘
                            │ claude attach JOB, run in a PTY
                            ▼
          Claude Code's daemon: claude --bg jobs, outlive pando
```

- Runtime dir: `PANDO_RUNTIME_DIR`, else `$XDG_RUNTIME_DIR/pando`, else
  `/tmp/pando-<uid>`; it holds the socket, `pando.lock` (`flock`, one daemon)
  and `daemon.log`.
- Any client autostarts the daemon (`proto.EnsureDaemon`). `ping` returns the
  build id; a TUI whose build differs restarts the daemon, silently when no
  session runs, otherwise after asking (`staleModal`): a restart kills the PTYs.
- `WatchUpdates` checks GitHub at start and daily while `update_check` is on.

## Protocol

One request per connection, one response line back. `subscribe` keeps the
connection open and streams events instead.

```text
→ {"method":"session.new","params":{"workspace":"/repo","agent":"claude"}}
← {"result":{"id":"a1b2c3","agent":"claude","status":"running",…}}
← {"error":"unknown session \"zz\""}
```

Methods (`Daemon.dispatch`; `subscribe` in `Daemon.handle`):

- daemon: `ping` `shutdown` `subscribe` `focus`
- state: `state.get` `state.set` `draft.list` `draft.set`
- projects: `project.add` `project.remove` `project.move`
- worktrees: `workspace.list` `workspace.new` `workspace.remove` `workspace.move`
- sessions: `session.new` `session.get` `session.list` `session.kill`
  `session.rename` `session.move` `session.input` `session.screen`
  `session.read` `session.wait`
- updates: `update.status` `update.check` `update.install`

Events are notifications; the client re-reads what changed: `sessions` →
`session.list`, `screen` (output, at most every 16 ms) → `session.screen`,
`state` (also `config.toml` edited on disk) → `state.get`, `workspaces` →
`workspace.list`. `focus` and `update` carry their payload.

- `draft.set` broadcasts nothing: a `state` event per keystroke burst would
  make every client re-read the state.
- A subscriber that falls 256 events behind is dropped.
- `session.wait` is the one call that blocks: it returns when the status is in
  `until` (default `idle`, `blocked`, `exited`, dropped when only `match` is
  given), the screen matches `match`, or fails when `timeout_ms` passes. Every other method answers at once.

[SKILL.md](../skills/pando/SKILL.md) is the contract an agent reads before
driving the API.

## Sessions

- Each session is a `creack/pty` process and a `vt.Emulator`, one mutex each,
  with `PANDO_SESSION=<id>` and `PANDO_RUNTIME_DIR` set and `NO_COLOR` dropped.
- A shell session tries `shellCandidates` in order; one failing within 2 s
  yields to the next.
- Sessions resolve by id, by name (unique among live sessions), or an
  unambiguous prefix. A `parent` session's Terminal shells die with it.
- `xtermKey` encodes modified special keys vt drops (`ctrl+←`).
- A DECSCUSR the app sends is kept as sent, 0 included (vt reads it as 1),
  one for both screens as in xterm: `Screen.CursorStyle`.
- Attention: bell, OSC 9/777, or, unseen, turning blocked or exited or
  ending a burst of 3 s or more.
- Exit 0 closes a session; a failure stays listed with its code. A kill sends
  SIGHUP to the shell's and the foreground job's process groups, SIGKILL
  after 2 s.

`session.screen` counts as viewed: it clears attention for 2 s.

## Status

`Session.Status` is `blocked`, `running`, `idle` or `exited`; the TUI shows
`idle` with `attention` as done. Each tick, `session.tick` decides:

```text
 process gone? ────────────────────────────────yes─▶ exited
   │ no
 screenState: last 12 non-empty lines and the title
   ├─ blocked: a permission or a question ─────────▶ blocked
   ├─ running: esc to interrupt, ✻ Doing… ─────────▶ running
   └─ idle (claude at an empty ❯), or "" (no rule)
        │
 shell command under the worker on 2 ticks? ───yes─▶ running
        │ no
 the screen said idle? ────────────────────────yes─▶ idle
        │ no: unknown program, or a ◯ background row
 output in the last 1.5 s? ────────────────────yes─▶ running
        │ no
        └──────────────────────────────────────────▶ idle
```

- `agentRules` in `internal/daemon/detect.go` holds the phrases per agent
  (`claude`, `codex`, `gemini`, `opencode`). The screen is reclassified only
  after output or a change of foreground program.
- Foreground program: the PTY's process group (`TIOCGPGRP`, `/proc/<pgid>/cmdline`).
- Shell commands: `commandsUnder` keeps the worker's direct children that run a
  command line (`sh -c`, `bash -lc`). An MCP server is no shell; a hook is gone
  by the next tick. The worker is the foreground process, or, under `claude
  attach`, the background job's process. So a `run_in_background` shell keeps
  an idle claude running.
- `/proc` is Linux only; elsewhere only the screen and output timing decide.

## Resume after a restart

```text
 running daemon, every tick (Daemon.remember)
   foreground program ─▶ spec.Resume      [resume], [resume_id] or [resume_job]
   claude: ~/.claude/sessions/<pid>.json ─▶ spec.Conversation {id, job, env}
   agent is the session's own process     ─▶ spec.ResumeExec
   changed? ─▶ state.json
        │
 restart: Daemon.New, each saved spec (workspace gone: skipped)
   Daemon.resume ─▶ command, leftover pid, notice lines
   ├─ shell session ─▶ respawn the shell; 600 ms later stop the leftover,
   │                   type the command (resumeWith)
   └─ ResumeExec    ─▶ run the command in place of Cmd: sh -c 'exec env …'
```

Scrollback is lost. A claude resumes by id from its `sessions/<pid>.json`
(`procStart` guards against a reused pid); other agents use `[resume]`.
`Daemon.resume` decides per conversation:

| The conversation is…                                | The session                                                   |
| --------------------------------------------------- | ------------------------------------------------------------- |
| continued already by an earlier session in the list | stays a shell, saying which session has it                    |
| running as a background job                         | attaches to the job again (`[resume_job]`)                    |
| a background job that ended                         | resumes the conversation by id                                |
| open in a process outside pando                     | stays a shell, saying which process and the command for later |
| open in a leftover of this daemon (a crash)         | stops the leftover, then resumes                              |
| not open anywhere                                   | resumes                                                       |
| empty: no transcript yet, `--resume` would fail     | starts the agent afresh, its `[agents]` preset                |

- A leftover (`Daemon.ours`) ran under this runtime directory and belongs to a
  respawned session or lost its terminal.
- `[resume_env]` variables the agent had and its shell lacks
  (`CLAUDE_CONFIG_DIR`, …) go before the command (`agentEnv`).
- `pando claude ARGS` runs `claude --bg ARGS` and execs `claude attach ID`, so
  the conversation lives in Claude Code's daemon and a restart stops only the
  attach. With `claude_background` on, resumes by id go through it too.
- An attach sets no title: the session shows the job's name or transcript title.
