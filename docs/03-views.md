# Views

Four tabs, each a struct in `internal/ui` with the same shape: build rows →
render into a windowed list → handle keys and mouse.

| View           | File          | Rows                                                                  |
| -------------- | ------------- | --------------------------------------------------------------------- |
| Explorer       | `explorer.go` | directory tree, git decorations, `ctrl+f` filters the whole workspace |
| Source Control | `scm.go`      | message box, Commit, sections, history drawers                        |
| Spaces         | `agents.go`   | project → worktree → session tree                                     |
| Search         | `search.go`   | query box, summary, matches grouped by file                           |
| Terminal       | `agents.go`   | shell panel under the editor (or a sidebar), opened by ⌃`             |

## Source Control

```
 SOURCE CONTROL                repo · main
                                            ← blank row
 ▏Message (⏎ to commit on "main")      ✧ ▕   widget rows: keyboard skips them,
                                            clicks act on them
            ✓ Commit                │ ∨      ✓ Continue during a merge, rebase, cherry-pick;
                                            ☁ Publish Branch / ⇅ Sync Changes 1↓ 2↑ with nothing to commit
 ▾ Merge Changes                       [1]   only while conflicts exist
   main.go · both modified       src +   !
 ▾ Staged Changes                      [1]
   api.go src                    src ↶ − A   hover actions
 ▾ Changes                             [2]
 ▾ Untracked Changes                   [1]
 ─────────────────────────────────────────  rule
 ▸ Graph ▸ Commits ▸ File History …         drawers, drag a header to resize
```

Sections are the VS Code split (merge / staged / tracked / untracked). Merge
Changes takes the conflicted entries out of Changes: no discard button, a
click opens the file itself (VS Code without the merge editor), and staging
is "mark resolved". Staging a `UU`/`AA` file that still matches VS Code's
marker test (`^<<<<<<< `, `^=======$`, `^>>>>>>> `) asks first; a `UD`/`DU`
file asks _Keep Our/Their Version_ or _Delete File_ (`git rm`). Stage All and
the Changes `+` leave the merge group alone, as VS Code's `git.stageAll`
does. While `Status.Op` is set the Commit button reads _Continue_, refuses
until Merge Changes is empty, and then commits (merge, cherry-pick) or runs
`rebase --continue`. During a merge or cherry-pick the message box shows the
first line of git's `MERGE_MSG` (`Merge branch 'develop' of … into feat`) as
its placeholder, and Continue with the box left empty commits that message,
as VS Code's prefilled box does. The button is VS Code's SCM action button
(`scmView.action`): Commit while anything is staged, changed or conflicted or
an operation waits; else _Publish Branch_ (`push -u`) on a branch without an
upstream; else _Sync Changes_ when ahead or behind; else a muted Commit that
only flashes. Publish and Sync have no `∨` menu; `S` publishes too when there
is no upstream. Publish follows VS Code's `git.publish`: one remote is pushed
to at once, several open a picker of them (push URL underneath) with _Add a
new remote…_ that asks for the URL, then the name; none offers _Publish to
GitHub_ when `gh` is installed, a picker whose text is the repository name
(prefilled with the folder's, sanitized like VS Code) and whose two items pick
private or public, or flashes a warning otherwise. Scrolled, the
changes pane draws its sticky rows over the top (`scmView.pinned`): the
repository's head (message box, button and the gaps) and the header of the
section the first shown row is in, VS Code's tree sticky scroll. They are
computed from the scroll offset alone, so scrolling by one hides exactly one
row beneath them, the next section header takes over the sticky line when it
gets there, and a click or ↑ on them acts on the real row. A pane shorter than
twice the sticky rows scrolls as a plain list. Drawers run a
git command each (`Graph`, `Commits`, `File History`, `Branches`, `Worktrees`,
`Remotes`, `Stashes`, `Tags`); a right click on one hides it or brings another
back. Which drawers show, their open state and height live in the config.

## Spaces

Sessions belong to worktrees, worktrees to projects — switching a session
re-roots Explorer and Source Control to its worktree (the Zed threads model).

```
▾ ◐ repo               project, with its most demanding session's symbol (· for none)
   ⌂ main          2   the project's own checkout (accent), session count
   ├─ ◐ claude · fix   working
   └─ ○ shell          idle
   ⑂ feat/x        1   a linked worktree (dimmed)
   └─ ○ shell          idle
                       blank row: herdr's gap between two spaces
▾ ○ other
   ⌂ main          1
   └─ ○ shell          idle
▸ · folded             a folded project has nothing to separate: no gap
▸ · folded too
```

`x` on a row does what the row can take, as herdr's workspace menu does: a
session is killed; a linked worktree is deleted (`workspace.remove`, the
checkout folder goes, the branch stays); a project or its own checkout is
only closed (`project.remove`), leaving Spaces with nothing on disk touched.
Sessions in the way are killed first (`killThen`), after a confirmation that
counts them; a Terminal panel's shells go with their session.

_New Worktree…_ (`w`) asks for a branch prefilled with a random name,
`worktree/rapid-meadow-a12e` as herdr makes them; ⏎ takes it, and an emptied
prompt still gets one from `workspace.new`.

Dragging a project row moves it up and down the list: the tree reorders under
the pointer and the release sends `project.move`, so the order is saved with
the projects. `alt+↑↓` and the menu's _Move Project Up / Down_ do the same
without the mouse, and a press that never leaves its row is still the click
that folds the project.

A blank row sits before a project whenever the project above it is unfolded,
so a run of folded ones stays a tight list. It is a row like any other, and
the scrollbar and the wheel count it, but `↑↓` steps over it and a click on
it is ignored: nothing ever selects it.

The symbols are herdr's: `×` blocked on a permission or a question, `◐`
working, `✓` done (finished while nobody was looking), `○` idle, `✕` exited
with a code. The daemon reads each agent's screen once a tick and matches the
phrases it prints while it waits (`Do you want to proceed?`, `Allow command?`)
or works (`esc to interrupt`), the way herdr's detection manifests do
(`internal/daemon/detect.go`); output timing decides for anything else.

`alt+t` opens the same data as a fuzzy picker (agent navigator) with
`@blocked`, `@running`, `@done`, `@idle`, `@exited`, `@worktree` and `!agent`
filters.

With `sounds = true` a session out of view plays the `sound_request` file when
it starts waiting and `sound_done` when it finishes unseen (the desktop's own
event sounds by default, through the first of `paplay`, `pw-play`, `afplay`,
`ffplay`, `mpv`; without one, or with `""`, the terminal bell). One cue a
second at most.

A new session is a shell in the workspace, started without asking: agents run
inside it the way they do in any terminal (*New Agent Session…* still starts an
`[agents]` preset directly). A session has tabs of its own, as a herdr
workspace does: the strip over the terminal shows the one in view and its
tabs (`tabsOf`), `+` opens one more shell in it (agent `tab`, the session as
its parent, so it dies with it), `[` and `]` cycle them, and middle click or
the active tab's `✕` kills one after a confirmation. The Spaces tree lists the
sessions alone (`agentSessions`), each carrying its most demanding tab's
symbol; going back to a session shows the tab it was on (`lastTabOf`), and
its Terminal panel is shared by its tabs.

The daemon remembers which program holds each session's terminal: once a tick
it reads the pty's foreground process group (`TIOCGPGRP`, then
`/proc/<pgid>/cmdline`, with `node cli.js` counting as `cli`) and stores the
matching `[resume]` command in the session's spec. A daemon that starts again
respawns the shell and types that command into it, so the agent comes back with
its conversation instead of an empty prompt. Leaving the agent clears it.

`--continue` means the latest conversation in the directory, which is another
session's when two share a worktree. So for an agent that says which one a
process has open, the spec keeps that conversation and `[resume_id]` continues
it by id. Claude Code writes `~/.claude/sessions/<pid>.json` (`sessionId`,
`procStart` against `/proc/<pid>/stat` so a reused pid does not count); other
agents fall back to `[resume]`. `claude attach JOB` writes no file of its own:
its conversation is the background session (`kind: "bg"`) whose `jobId`
begins with JOB, run by Claude's own daemon, so it outlives pando and
`[resume_job]` (`claude attach {id}`) goes back to it. A leftover is a process
of this runtime directory whose session the daemon respawns, or one that lost
its controlling terminal: only a daemon that died held that pty, whatever
became of the session since (`Daemon.ours`). A claude started with
its own `CLAUDE_CONFIG_DIR` keeps its sessions and transcripts there: the spec
records the variable when the session's shell does not set it the same way
(`Conversation.Env`), every check looks in that directory, and the command
typed back sets it first (`CLAUDE_CONFIG_DIR='…' claude --resume …`, a form
bash, zsh and fish all read). On respawn (`Daemon.resume`):

| The conversation is…                                  | The session                                                   |
| ----------------------------------------------------- | ------------------------------------------------------------- |
| continued already by an earlier session in the list   | stays a shell, saying which session has it                    |
| running as a background job                           | attaches to the job again; its process is never a leftover    |
| a background job that ended                           | resumes the conversation by id                                |
| open in a process outside pando                       | stays a shell, saying which process and the command for later |
| open in a leftover of this daemon (a crash)           | stops the leftover, then resumes                              |
| not open anywhere                                     | resumes                                                       |
| empty: no transcript yet, `--resume` would fail       | starts the agent afresh, its `[agents]` preset                |

Stopping a session signals the foreground job's process group as well as the
shell's and waits for both, and the ticker leaves specs alone while the daemon
closes, so what a restart finds is what was running.

## Terminal

⌃` (or `5`) opens the Terminal under the editor and focuses it; pressing it
again closes it, wherever the focus is. `terminal_position` moves it to the
left or right sidebar, where it is a column of its own — `moveView` and
`mergeView` refuse to put another view beside it, so it never becomes a tab.
⌃⇧↑ gives the bottom panel the whole editor area and ⌃⇧↓ hands it back.

It holds sessions of the `terminal` agent, which the Spaces tree and the main
area's session strip leave out. A shell's `parent` is the agent session it was
opened under, so every session has tabs of its own (`termOwned`); one opened
with no session in view has no parent and belongs to its workspace. Switching
sessions re-attaches the panel (`attachTerm`), and an open panel with no shell
to show starts one (`ensureTerm`) — at startup too, so a panel saved open never
draws an empty strip. The daemon kills a session's shells with it.
The panel has the same tab strip: click to
switch, `+` for one more, middle click or the active tab's `✕` to kill after a
confirmation. A killed tab hands focus to the tab left of it, else the one
right of it; only the last one closing empties the strip — and, for the panel,
closes it. Its screen is fetched sized to wherever it sits, so the shell
reflows with it.

## Search

Workspace-wide text search, run 250 ms after the last keystroke, capped at 2000
matches. Engine, first available: `rg --json` → `git grep -z --untracked` →
`grep -rnIZ`; the last two get their match ranges from a Go regexp built from
the same options.

```
 ▾▏ query                       Aa ab .* ▕   ▾ opens replace (ctrl+h)
  ▏ replace                        AB ⇄ ▕   AB preserve case, ⇄ replace all
    files to include                        ⋯ opens include and exclude
  ▏ e.g. *.ts, src/**                   ▕
 2 results in 1 file              tree  ⋯   tree groups results by folder
 ▾ src                                 [2]
   ▾ main.go                           [2]
       println("hello world")              matches highlighted, ⏎ opens the
       // todo hello                       file with the match selected
```

With a replacement typed, each match previews as ~~old~~ new; `r` rewrites the
selected line or file, `R` everything after a confirmation. A file that changed
since the search is refused rather than guessed at, and there is no undo — git
is the safety net.

## Commands

Every view exposes `items()` — one list used by its context menu (`m`, right
click) and by the command palette (`ctrl+shift+p`, `F1`), so a command exists
once:

```
items() ──┬── newMenu(…)        context menu at the cursor
          └── commandPalette()  "Git: Switch Branch…"   B
```

The palette only lists what applies now (editor commands need an open preview)
and puts the last command used on top.
