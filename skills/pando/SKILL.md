---
name: pando
description: "Control pando, a terminal workspace that runs coding agents in git worktrees. Use only when the user explicitly mentions pando or asks to use pando to inspect or control sessions, workspaces, projects or another agent. Do not use merely because a task could benefit from a background terminal, a worktree, or parallel work."
---

# pando

pando keeps agent sessions alive in git worktrees. One daemon owns the PTYs;
the TUI and the `pando` CLI are clients of the same unix socket, so anything
you do from the shell shows up in every attached TUI at once.

Use it to open a worktree, start an agent in it, give that agent work, wait for
it, and read what it produced.

## Before the first call

The `pando` binary in `PATH` talks to the daemon and starts one if none
answers. Check that you are talking to the daemon you mean:

```sh
pando call ping
```

If `PANDO_RUNTIME_DIR` is set in your environment, every command follows it;
an unset variable means the user's real daemon. Do not set or change it to
work around an error.

`PANDO_SESSION` is set inside a session pando started. If it is set, you are
running **inside** one of pando's own sessions — that id is your own terminal.
Never kill, rename or send input to it.

## Learn the current CLI

The binary is the authority for syntax:

```sh
pando help
```

Every command prints JSON except `pando session read` (the screen as text) and
`pando project ls` (one path per line). Read ids and state out of those
responses; never guess them.

`pando call METHOD '{"json":"params"}'` reaches any API method directly,
including the ones with no CLI verb. `pando help` lists the methods.

## What the API covers

Sessions, workspaces, projects and settings. That is the whole orchestration
surface and it is enough to drive pando.

Explorer, Source Control, Search, the diff viewer and the editor are TUI
surfaces over git and the filesystem — they are **not** in the API by design.
For a diff, a commit, a branch or a search, use `git` and your normal tools in
the worktree directory. Do not go looking for a pando method for them.

## Identify a session

A session has a 6-character id. Commands also accept the name you gave it and
any unambiguous prefix of either, so name the sessions you create:

```sh
pando session new --agent claude --ws /path/to/worktree --name reviewer --wait
```

Names are unique among live sessions; a second `reviewer` is refused rather
than shadowed. A name is not reused after the session is gone.

Read the live list, or one session, with:

```sh
pando session ls
pando session get reviewer
```

## Lifecycle states

`status` is one of:

| Status    | Meaning                                           |
| --------- | ------------------------------------------------- |
| `running` | a turn or a command is working                    |
| `blocked` | the agent is waiting at an approval or a question |
| `idle`    | at its prompt, ready for input                    |
| `exited`  | the process is gone; `exit_code` says how         |

pando reads the screen for the agents it recognizes (claude, codex, gemini,
opencode) and falls back to output timing for everything else. A `blocked`
screen is an approval or a question **that you must not answer on the user's
behalf** — read it and ask, unless the user already told you how to answer.

`attention` is a UI flag for the human, not a state. Ignore it.

## Wait instead of polling

`pando session wait` blocks until the session settles. Never poll `session
read` in a loop.

```sh
pando session wait reviewer                          # idle, blocked or exited
pando session wait reviewer --until blocked --timeout 120000
pando session wait reviewer --match 'All tests passed' --timeout 60000
```

- `--until` takes a comma-separated list of the statuses above; the default is
  `idle,blocked,exited`.
- `--match` is a Go regular expression tested against the screen text; with
  `--until` either condition returns.
- `--timeout` is milliseconds. Without one the wait is indefinite — always
  give one for an agent turn.
- A timeout exits 1 with `timeout waiting for session …`. It does **not**
  prove the work failed; read the session before deciding anything.

## Give an agent work

```sh
pando session send reviewer 'Review the current diff and report only actionable findings.' --wait --timeout 180000
```

`--wait` submits the text (it implies Enter), waits for the session to pick the
work up, then waits for it to settle, and prints the session it settled in.
Check `.status` on what comes back: `idle` means the turn finished, `blocked`
means the agent is asking for something.

Multi-line text is delivered as a paste, so it arrives as one block wherever
the app brackets pastes instead of submitting at the first newline.

Without `--wait`, `send` only types: add `--enter` to submit, and nothing
tells you when the turn ends.

## Answer a prompt or interrupt

Named keys, not escape codes:

```sh
pando session keys reviewer enter
pando session keys reviewer esc
pando session keys reviewer ctrl+c
pando session keys reviewer shift+tab
```

Every key is parsed before any is delivered, so a typo late in the list sends
nothing. `pando session keys` with no keys prints the names it accepts.

## Read the result

```sh
pando session read reviewer --lines 120
pando session read reviewer --scrollback --lines 400
```

Without `--scrollback` you get the current screen. `--lines N` keeps the last
N lines of whatever that selects; pass it, or a long transcript arrives whole.

If an agent draws on the alternate screen, its finished output never reaches
scrollback and more lines will not recover it. Then, and only then, ask the
agent to write its answer to a file and read the file yourself.

## A worktree of its own

```sh
pando ws new feat-x --project /path/to/repo   # a git worktree, prints its path
pando ws new --project /path/to/repo          # no branch: a random worktree/… one
pando ws ls
pando session new --agent claude --ws <that path> --name feat-x --wait
```

Create a worktree only when the user asked for isolated work. Otherwise start
the session in a workspace that already exists.

## Safety

- Do not kill, rename, move or send input to a session you did not create,
  and never to `$PANDO_SESSION`. Killing a session also kills its own tabs
  (`--agent tab --parent ID`) and the shells of its Terminal panel
  (`--agent terminal --parent ID`), so a kill takes more than one terminal.
- Do not run `pando stop`: it takes down the daemon and every session in it,
  including the user's.
- Do not answer an approval or a permission prompt for the user. Report what
  the screen asks and let them decide.
- Do not change settings with `pando set` unless the user asked; they are the
  user's UI, and every attached TUI changes with them.
- `pando ws rm` removes a git worktree. Only on an explicit request.
- Errors go to stderr as `pando: <message>` with exit status 1.
