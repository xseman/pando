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
 │     └─ vt.Emulator: screen; scrollback: 10 000 rows  │
 │  broadcast ──▶ every subscribe connection            │
 └──────────────────────────┬───────────────────────────┘
                            │ claude attach JOB, run in a PTY
                            ▼
          Claude Code's daemon: claude --bg jobs, outlive pando
```

- Runtime dir: `PANDO_RUNTIME_DIR`, else `$XDG_RUNTIME_DIR/pando`, else
  `/tmp/pando-<uid>`; it holds the socket, `pando.lock` (`flock`, one daemon)
  and `daemon.log`.
- An instance is another daemon (`proto.Instance`, `PANDO_INSTANCE`): its
  runtime dir is `instances/<name>/` in the default's (`InstanceDir`), its
  `state.json` `instances/<name>/` in the config dir (`daemon.StateDir`);
  `config.toml` and the data dir are shared, and `State.Instance` names it to
  the TUI's status bar. A leading `--instance` sets it and drops an inherited
  `PANDO_RUNTIME_DIR`, which otherwise wins: a session's commands reach their
  own daemon. `pando instance ls` dials each one's socket.
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

`session.new` takes `cmd` over the agent's preset and `env`, `KEY=VALUE`
over the daemon's environment, kept with the session for its restarts.

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

- Each session is a process on a `term` and a `vt.Emulator`, one mutex each,
  with `PANDO_SESSION=<id>` and `PANDO_RUNTIME_DIR` set and `NO_COLOR` dropped.
  The `term` is a `creack/pty` pty (`os_unix.go`), on Windows a pseudo console
  (ConPTY, `os_windows.go`).
- A pseudo console outlives its process: the daemon waits for the process and
  closes the console then, which ends the console processes still on it
  (`CTRL_CLOSE_EVENT`) and the output it reads. Windows has no process groups:
  a kill closes the console, as closing a Windows Terminal tab does, then ends
  the session's process after 2 s. A process on a console of its own (started
  hidden or detached) outlives both, as a detached one outlives a pty.
- `pando serve` on Windows runs on a hidden console of its own, out of its
  starter's job where the job lets it (`CREATE_BREAKAWAY_FROM_JOB`).
- On Unix it runs in a session of its own (`Setsid`); under a systemd user
  manager also in a scope of its own (`systemd-run --user --scope -p
  OOMPolicy=continue`). The terminal's scope it would share otherwise has
  `OOMPolicy=stop`: one OOM kill in a session ends the scope, daemon and every
  session with it. Without systemd-run or a user bus it starts as before.
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

`session.screen` counts as viewed: the next tick clears attention for 2 s
and sends `sessions`. Its `scroll` counts back from the bottom;
`Screen.Pushed` counts the lines scrolled into scrollback, and a call that
sends it back as `pushed` gets the page moved up by those pushed since, so a
scrolled page stays on its text.

The scrollback is pando's, not vt's: after each write or resize `drain`
moves the rows vt scrolled off into `session.back`, rendered (~150 B a row
against ~11 KB of vt's 112-byte cells), at most 10 000; ED 3 clears it. vt's
alternate screen keeps a scrollback of its own that nothing shows;
`noAltScrollback` drops it by reflection, as vt has no setter
(`TestScrollbackMemory` fails if that stops working).

## Status

`Session.Status` is `blocked`, `running`, `idle`, `asleep` or `exited`; the TUI shows
`idle` with `attention` as done, and `running` with `shell` (a shell command
under the worker) as shell, its ◐ blue. Each tick, `session.tick` decides:

```text
 process gone? ────────────────────────────────yes─▶ exited
   │ no
 screenState: last 12 non-empty lines and the title
   ├─ blocked: a permission or a question ─────────▶ blocked
   │
 the user scrolled the app's own view (held)? ─yes─▶ as it was
   │ no
 the screen says running (esc to interrupt, ✻ Doing…),
 a shell command under the worker on 2 ticks (shell),
 no output yet, or output of its own in the last 1.5 s? ──yes─▶ running
   │ no
 an input in the last 1.5 s? ──────────────────yes─▶ as it was
   │ no
   └───────────────────────────────────────────────▶ idle
```

- Output of its own is the app's: not within 500 ms (`redrawWindow`) of a
  resize, its redraw, or of an input, its echo. It says running over any
  screen, claude's idle ❯ too: claude takes its spinner down while it streams
  the answer, the prompt still under it. Idle shows once it stops, 1.5 s
  (`busyWindow`) after its last output or the user's last input.
- `held`: a wheel up sent to an app that takes the mouse scrolls its own view.
  Scrolled, claude shows history over its prompt and draws nothing while it
  works, so neither its screen nor its output says anything; the status stays
  as it was until a key, text or a paste (typing takes the app back to its
  live view), or output of its own. A turn that ends while the view is
  scrolled shows `running` until then.

- `agentRules` in `internal/daemon/detect.go` holds the phrases per agent
  (`claude`, `codex`, `gemini`, `opencode`). The screen is reclassified only
  after output or a change of foreground program.
- Foreground program: the PTY's process group (`TIOCGPGRP`, `/proc/<pgid>/cmdline`).
- Shell commands: `commandsUnder` keeps the worker's direct children that run a
  command line (`sh -c`, `bash -lc`). An MCP server is no shell; a hook is gone
  by the next tick. The worker is the foreground process, or, under `claude
  attach`, the background job's process. So a `run_in_background` shell keeps
  an idle claude running.
- `/proc` is Linux only; elsewhere (macOS, Windows) only the screen and
  output timing decide, and an agent's session is taken to run the agent
  itself (`ownAgent`): a restart runs its `[resume]` in its place.

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
  (`CLAUDE_CONFIG_DIR`, …) go before the command (`agentEnv`). An agent that
  is the session's own process has no shell: those it has beyond the daemon's
  environment count, such as `session.new`'s `env` (`ownEnv`).
- `pando claude ARGS` runs `claude --bg ARGS` and execs `claude attach ID`, so
  the conversation lives in Claude Code's daemon and a restart stops only the
  attach. With `claude_background` on, resumes by id go through it too.
- `session.kill` stops the job too (`[stop_job]`, `claude stop ID`), and the
  jobs its Terminal shells attached to, and waits for each to let go of its
  conversation; `claude --resume` still opens it.
- An attach sets no title: the session shows the job's name or transcript title.

## Sleep

A session put to sleep (`session.sleep`, `sleep_after`) is a restart of one
session, put off until `session.wake` (`sleep.go`).

```text
 sleep: under d.mu  canSleep (remember first, as a tick) ─▶ asleep = true
        kill (SIGHUP, as Close) ─▶ stopJob ─▶ spec.Asleep, screen kept ─▶ save
        then its Terminal panel's shells
 wake:  respawn (the restart's path: resume, ResumeExec, shells)
        add replaces it under its id, in its place ─▶ save, then its shells
        waits up to 20 s for the agent in front, idle or blocked (no /proc: none)
 restart: spec.Asleep ─▶ dormant: a screen, no process, until woken
```

| A session in front runs…                  | By hand | On its own | Wakes to                |
| ----------------------------------------- | ------- | ---------- | ----------------------- |
| an agent with a resume (spec.Resume)      | yes     | when idle  | the agent, resumed      |
| its shell, at the prompt                  | yes     | when idle  | a new shell             |
| something else (a server, an editor)      | yes     | no         | a new shell             |
| a known agent with no `[resume]` (gemini) | refused | no         | -                       |

- On its own: `sleep_after` minutes since `used`: the last input, a client's
  `session.screen` (a TUI asks for the session it shows every 30 s while
  `sleep_after` is on), or a status change past `startGrace` (30 s) after a
  start, so an agent coming back does not count. `Active` keeps it over a
  restart. Not with `attention` (done unseen), not a Terminal shell (they go
  with their session), nor a session one of whose shells is not unused the
  same way (a quiet server, an editor); where nothing says what runs in
  front (no /proc), only an agent with a resume.
- `asleep` is settled: `session.wait` stops at it; `session.input` is
  refused. A wait begun asleep follows the woken session (`d.session` each
  poll). The exit of the process it stops neither closes it nor falls back.
- `d.moving` keeps a sleep and a wake of one session apart.
