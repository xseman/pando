# Views

Each view is a struct in `internal/ui` with the same shape: build rows →
render into a windowed list → handle keys and mouse.

| View           | File          | Rows                                                                  |
| -------------- | ------------- | --------------------------------------------------------------------- |
| Explorer       | `explorer.go` | directory tree, git decorations, `ctrl+f` filters the whole workspace |
| Source Control | `scm.go`      | message box, Commit, sections, history drawers                        |
| Spaces         | `agents.go`   | project → worktree → session tree                                     |
| Search         | `search.go`   | query box, summary, matches grouped by file                           |
| Terminal       | `agents.go`   | shell panel under the editor (or a sidebar), opened by ⌃`             |
| GitHub         | `github.go`   | pull request and issue queries, notifications; only where `gh` is     |

## Explorer

The context menu (`m`, right click) follows VS Code's, in groups split by a
rule (`separator()`, which the keys and the mouse skip). A right click below
the tree clears the selection, so the menu acts on the workspace root.

| Group     | File                                                           | Folder                                  | Below the tree (the root)                       |
| --------- | -------------------------------------------------------------- | --------------------------------------- | ----------------------------------------------- |
| create    | New File…, New Folder… beside it                               | New File…, New Folder… inside           | … in the root                                   |
| open      | Open Containing Folder, Open with Default App, Edit in $EDITOR | Open Containing Folder, Find in Folder… | the same, on the root                           |
| clipboard | Cut, Copy, Paste                                               | the same                                | Paste                                           |
| copy      | Copy Name, Copy Path, Copy Relative Path                       | the same                                | Copy Name, Copy Path                            |
| change    | Duplicate…, Rename…, Delete…                                   | the same                                |                                                 |
| git, view | Stage Changes, Collapse All                                    | the same                                | Refresh, Show / Hide Hidden Files, Collapse All |

Open Containing Folder is VS Code's Linux label: `xdg-open` on the parent,
which cannot select the entry in it; on the root it opens the root itself.
Cut (`^x`) and Copy (`^c`) keep one entry in `explorer.clip`, pando's own
clipboard rather than the system's, kept across workspaces; a cut entry is
drawn faded and Paste stays greyed out while it is empty. Paste (`^v`, or a
terminal's paste) goes into the selected folder, a file's folder, or the
root, in a `tea.Cmd`: a copy onto a taken name becomes `name copy.ext`; a
move refuses to replace anything, falls back to copy and delete across file
systems, and empties the clipboard once it lands. Neither goes into itself.
In Explorer `^c` copies rather than quits; a terminal still gets its `^c`.
Find in Folder opens Search with files to include set to `dir/**`, glob
characters escaped (`app/\[id\]/**`). Duplicate asks for the name, offering
VS Code's `main copy.go`, then `main copy 2.go`; it copies in a `tea.Cmd`
(`os.CopyFS` for a folder, a symlink as a link, a file with its mode), never
over an existing path or into itself, and selects the copy.

## Source Control

```
 SOURCE CONTROL                repo · main
                                            ← blank row
 ▏Message (⏎ to commit on "main")       ▏∨▕   widget rows: keyboard skips them,
                                            clicks act on them
               ✓ Commit                 ▏∨  ✓ Continue during a merge, rebase, cherry-pick;
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
as VS Code's prefilled box does. The box is a `textarea` that grows a row
per visual line up to `maxMsgLines` (10, VS Code's `scm.inputMaxLineCount`)
and scrolls past it: `build` adds one `rowMsg` per line (`scmRow.line`), and
`fit` rebuilds when a key, a paste or a rewrap changes the count. Only the
active repository's box grows; another shows its draft's first line. Its
selection is the textarea's: `ctrl+a` is remapped from line start to select
all, copy and cut go through `setClipboard`, and a press on the box starts a
`dragMsgSel` that `BeginSelection`/`ExtendSelection` follow. The ∨ at its
top right, split off by a ▏ like the Commit button's (on the cell's left edge, so the hover starts at the line), opens `suggestMenu`: Generate Commit Message (`A`), Generate with
Description, Match Repository Style, Rewrite Current Message (text in the
box) and Regenerate (after a suggestion, until a commit or a repository
switch), each a `git.SuggestOpts`; under the mouse
it is raised (`keycapHot`) up to the border, whose ▕ cell takes the same
background so no gap shows. While a suggestion waits on `claude`, the box
draws `scramble` instead of the input: the effects ticker (below) settles
ASCII noise into "Generating commit message" (shorter when narrow), holds it,
dissolves it, and stops when `busy` clears; typing into the box is dropped
until the suggestion replaces it, which then decodes into the box a wrapped
line after another. A commit dissolves the message out of it. The button is VS Code's SCM action button
(`scmView.action`): Commit while anything is staged, changed or conflicted or
an operation waits; else _Publish Branch_ (`push -u`) on a branch without an
upstream; else _Sync Changes_ when ahead or behind; else a muted Commit that
only flashes. The hovered half (label or `∨`) darkens; the blank rows around
the button draw ▁ and ▔ in its colors (`scmView.buttonOf`), so it stands a few
pixels taller than a cell, and they hover and click as the button. Publish and
Sync have no `∨` menu; `S` publishes too when there
is no upstream. Publish follows VS Code's `git.publish`: one remote is pushed
to at once, several open a picker of them (push URL underneath) with _Add a
new remote…_ that asks for the URL, then the name; none offers _Publish to
GitHub_ when `gh` is installed, a picker whose text is the repository name
(prefilled with the folder's, sanitized like VS Code) and whose two items pick
private or public, or flashes a warning otherwise. A file row keeps the
text colour; only its status letter is tinted, and in tree mode a folder
shows a ● in the colour of its first file in path order (`treeRows`), as
VS Code's bubbled decoration does. Scrolled, the
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

A folded project sinks below the open ones, the selection with it, to the top
of the folded: they list the one folded last first (`agents.folded`, saved in
that order as `spaces_folded`). Unfolded, it goes back to its place among the
open, which folding never touches: `state.json` keeps the project order.
Blank rows before the first folded one pin the folded to the panel's bottom
edge, as VS Code stacks collapsed views (`pinFolded`); a tree taller than the
panel has none and scrolls as it is. They are gaps like the one between two
spaces, and a walk over them that meets the end of the list turns back.

Dragging a project row moves it up and down the list: the tree reorders under
the pointer and the release sends `project.move`, so the order is saved with
the projects. `alt+↑↓` and the menu's _Move Project Up / Down_ do the same
without the mouse, and a press that never leaves its row is still the click
that folds the project. Neither moves an open project among the folded, or a
folded one among the open; among the folded they reorder the folded list,
saved with the folds rather than as `project.move`.

A session row drags the same way among its worktree's sessions and sends
`session.move`, which takes its tabs along; `alt+↑↓` and _Move Session Up /
Down_ do it from the keyboard. Only Sort by Created and Group by Workspace keep
that order, so under the others a press opens the session at once. A
`sessions` event mid-drag replaces the list, and `drag.to` puts the held
session back under the pointer.

A worktree row drags among its project's worktrees the same way, its sessions
under it, and sends `workspace.move`; `alt+↑↓` and _Move Worktree Up / Down_
do it from the keyboard, and a press that never leaves the row still switches
to it. The checkout keeps its `⌂` wherever it goes.

A blank row sits before a project whenever the project above it is unfolded,
so a run of folded ones stays a tight list. It is a row like any other, and
the scrollbar and the wheel count it, but `↑↓` steps over it and a click on
it is ignored: nothing ever selects it.

The symbols are herdr's: `×` blocked on a permission or a question, `◐`
working, `✓` done (finished while nobody was looking), `○` idle, `✕` exited
with a code. The daemon reads each agent's screen once a tick and matches the
phrases it prints while it waits (`Do you want to proceed?`, `Allow command?`)
or works (`esc to interrupt`), the way herdr's detection manifests do
(`internal/daemon/detect.go`); output timing decides for anything else. A
claude at its prompt with a subagent or a workflow still listed under it
(`◯ deep-task … 8m 33s`) is left to output timing too: the row's clock ticks
while the work runs, so the session reads working until it stops.

A shell command under the agent reads working short of a prompt that blocks,
whatever the screen says: claude leaves a `run_in_background` shell at its idle
`❯` (`· 1 shell`). Once a tick the daemon lists the foreground program's own
children (`/proc/<pid>/task/*/children`) and counts a shell running a command
line (`sh -c`, `bash -lc`), as claude, codex, gemini and opencode start their
tools (`commandsUnder`). An MCP server is a child too but not a shell, and its
`npm exec`'s `sh -c` is a grandchild; a status line or hook command is gone by
the next tick, so only one seen on two ticks counts. A background dev server
keeps its session working until it stops.

A second click on the session in view puts it away and leaves no row selected
(`agents.click`), so nothing reads as open; `↵` keeps the selection.

View options (`o`, the header's sliders, or _View Options…_) are VS Code's
agent sessions menu (`agents.viewMenu`). Filter, Sort and Group are settings
(`spaces_hide`, `spaces_sort`, `spaces_group`), so every window lists alike;
the TUI reads them in `listedSessions`, which every Spaces row comes from.

| Option              | Effect                                                                                                        |
| ------------------- | ------------------------------------------------------------------------------------------------------------- |
| Filter ›            | a second menu that stays open: ✓ the states shown (blocked, working, done, idle, exited), by the row's rollup |
| Sort by Created     | the order sessions were made in (`SessionSpec.Created`, stamped by `session.new`); newest first when by time  |
| Sort by Updated     | newest output first (`Session.Updated`, the pty's last output, across the session's tabs)                     |
| Group by Workspace  | the project → worktree → session tree above                                                                   |
| Group by Time       | `agTime` headings Today, Yesterday, Last 7 Days, Last 30 Days, Older by calendar day; rows add the branch     |
| Collapse All Groups | folds every project, or every time heading; folds are kept across restarts (`spaces_folded`)                  |

A session from before `created` existed files under Older. `Updated` is not
saved: a respawned session starts over at its first output. The selection
is a row, not a row number: when a `sessions` event re-sorts the list, it
moves with its session (`agents.follow`). VS Code's _Show
Recent / All Sessions_ has no counterpart: every listed session is a live
terminal, not a history entry.

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
its conversation instead of an empty prompt. Leaving the agent clears it. A
session started as the agent itself (an `[agents]` preset, `--agent`) has no
shell to type into: its foreground is the session's own process, the spec
marks it (`ResumeExec`), and a restart runs the command in place of the
preset (`sh -c 'exec env …'`) rather than typing it into a fresh agent.

`--continue` means the latest conversation in the directory, which is another
session's when two share a worktree. So for an agent that says which one a
process has open, the spec keeps that conversation and `[resume_id]` continues
it by id. Claude Code writes `~/.claude/sessions/<pid>.json` (`sessionId`,
`procStart` against `/proc/<pid>/stat` so a reused pid does not count); other
agents fall back to `[resume]`. `claude attach JOB` writes no file of its own:
its conversation is the background session (`kind: "bg"`) whose `jobId`
begins with JOB, run by Claude's own daemon, so it outlives pando and
`[resume_job]` (`claude attach {id}`) goes back to it. An attach sets no
terminal title, so the shell's (the command line and the directory) would
name the session: while the foreground attaches to a job, the session's title
is the job's `name` from its file instead, the name claude itself shows. A
job that goes by its id (one `claude --bg --resume` started) takes the
transcript's title: the last `custom-title`, else the last `ai-title`, read
only as far as claude has appended since (`titleScan`). Claude rewrites its
`sessions/<pid>.json` in place, so a read that catches it half written is read
again (`readClaude`) rather than taken for a job that ended, and a job holding
the conversation wins over any other process that shows it (`holder`). A leftover is a process
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

`[resume_env]` generalises the config directory to any program: the variables
it names (`CLAUDE_CONFIG_DIR`, `CODEX_HOME`, `OPENCODE_CONFIG_DIR`,
`OPENCODE_DATA_DIR`), as the agent's process has them and its shell does not,
go before whichever command brings it back (`agentEnv`), by id or not.

A restart stops the processes in its ptys, and a claude takes its subagents
with it. `pando claude ARGS` runs `claude --bg ARGS`, reads the short id it
prints (`backgroundID`) and execs `claude attach ID`, so Claude Code's daemon
holds the conversation and pando only the attach. With `claude_background` on,
`byID` continues a claude conversation that way (`pando claude --resume ID`):
one killed by a restart comes back as a background session, which the next
restart attaches to. A claude without `--bg` runs as it is.

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

`⌃f` over a session or a Terminal shell is VS Code's terminal find
(`termfind.go`): the editor's widget (`findRow`, `findLayout`) at the top
right of the screen, searching everything the emulator holds.

```
query ─▶ session.read {scrollback} ─▶ lines, oldest first ─▶ termHit{line, cells}
                                                                 │
row y of a screen scrolled back s  =  line Scrollback − s + y  ◀─┘  paintFind, reveal
```

A match out of view scrolls the screen to put it in the middle, as xterm's
search addon does. A new query selects the newest match at or above the one
selected, `⏎` walks up toward older output and `⇧⏎` down, both wrapping. Every
screen update searches again (one read at a time, `busy`), keeping the
selection, so a streaming agent's new matches are counted. The alternate
screen has no scrollback: its screen is searched as it is. No API method was
needed: `session.read` is what a script uses for the same text.

ponytail: the line count goes stale when the emulator drops its oldest
scrollback lines or an app redraws above the prompt, until the next search.

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

## GitHub

VS Code's GitHub Pull Requests view (the extension, not the built-in
`extensions/github`) over `gh`, for the repository gh picks in the workspace.
`hasGH` is read once at start: without gh the view is in no column, `6` does
nothing and the palette leaves it out. `TestMain` turns it off, since CI's
runners have gh.

```
 ▾ PULL REQUESTS                     open panes share the height
   ▸ Local Pull Request Branches     All Open's, where a local branch has them
   ▸ Waiting For My Review           --search review-requested:@me
   ▸ Assigned To Me                  --assignee @me
   ▸ Created By Me                   --author @me
   ▾ All Open                    3   gh pr list --limit 50
      PR release v0.4.0 #14 @ann ⑂ ✓ its worktree, its checks (✓ ⇅ ✕)
                                     a pane scrolls on its own
 ▾ NOTIFICATIONS               1 ⇕   drag its header: the edge with the pane above moves
      feat: streets  review requested   api repos/{owner}/{repo}/notifications: unread
 ▸ ISSUES                            folded: pinned at the bottom
```

The three sections are VS Code's panes (`panes`): every header keeps a row,
the open ones share what is left, and folded ones are pinned under them at
the bottom, in their order. An open pane is as tall as it was dragged (`ghView.h`), an even
share until then, and the last one takes the rest. Dragging an open pane's
header moves its edge with the open pane above, as Source Control's drawer
headers do, and a press that does not move folds or unfolds it. Each pane
scrolls and keeps its scrollbar on its own (`tops`); the selection is one row
of them all, and a pane scrolls to it. Issues: `--assignee @me` for My,
`--author` for Created, `sort:updated-desc` for Recent. ponytail: the
heights and folds live as long as the TUI does; `github_panes` in the
settings, as `git_panes` does for the drawers, if they should outlast it.

A folder asks gh when it is unfolded and on screen (`ensure`), once; `^r`
(`refresh`) asks again and keeps the old lists up until the answers land. No
timer. Answers belong to the project (its worktrees share a repository): a
switch to another one drops them, and `gen` drops answers still on the way.
Local Pull Request Branches filters All Open's answer by `prBranch` against
`for-each-ref refs/heads`: the head's name, `pr/<n>` for a fork, whose head
(often `main`) would match the local `main`. The selection follows its row
through a reload, as `agents.follow` does.

gh gives one exit status to most failures, so `ghErrKind` reads stderr, first
match wins:

| stderr says                             | the view shows                     | ⏎                            |
| --------------------------------------- | ---------------------------------- | ---------------------------- |
| `point to a known GitHub host`          | Not a GitHub repository            | nothing                      |
| `no git remotes`                        | No remote · Publish to GitHub…     | Source Control's Publish     |
| `To get started with…`, `gh auth login` | Sign in to GitHub…                 | `gh auth login` in a new tab |
| anything else                           | `gh:` and its first line, in place | refresh                      |

The first three stand alone, as VS Code's welcome view: nothing else would
load. The host message says `gh auth login` too, hence the order. With several
remotes and no default, gh picks one (`upstream` before `origin`) without
asking; `gh repo set-default` changes it.

| Action                     | How                                                                                                                     |
| -------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| Description (⏎)            | preview kind `gh`: `gh pr view --json` laid out as GitHub's page (`ghMarkdown`), rendered                               |
| Changes (`d`)              | preview kind `ghdiff`: `gh pr diff`, the diff view's own parsing and split                                              |
| Checkout in worktree (`w`) | a worktree on the branch: switch there; else `workspace.new` and `gh pr checkout n --branch b` in it (`git.CheckoutPR`) |
| Start working (`w`, issue) | the New Worktree prompt, offering `issue/<n>-<slug>`                                                                    |
| Merge…                     | a dialog of the three methods, `--match-head-commit`: a head pushed since the list came is refused                      |
| Create PR, Sign in         | `gh pr create`, `gh auth login` typed into a new Terminal tab once `session.wait` sees it idle                          |
| Notification ⏎, `x`        | opens it and marks it read (faint until the next fetch), Mark as Done                                                   |

The checkout never passes `--force`, which resets a branch that exists. A
local branch is checked out as it is and only fast-forwards; gh refuses one
that has diverged. A missing one starts the worktree on the daemon's
throwaway branch, gh creates the real one tracking the pull request's head,
and the throwaway goes: gh sets the upstream only for a branch it creates.
ponytail: the worktree's folder keeps the throwaway's name.

Create PR and Sign in are typed rather than run as the tab's command: a
restarted daemon respawns a session's command, which would run gh again.

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

## Text effects

`fx.go`. `noteFx` runs after every `Update` and compares what the model shows
(session and worktree names, branches, counts, the flash, the update state)
with what it showed; a difference starts an effect keyed by what it is on
(`sess:ID`, `ws:PATH`, `branch:ROOT`, …), and every place that draws that
text asks `fx.text` or `fx.segs` for its frame. One 60 ms `fxTickMsg` ticker
drives them all while an effect runs or work shimmers, and stops itself.
`animations = false` turns every one off.

| Effect   | On                                                                                                |
| -------- | ------------------------------------------------------------------------------------------------- |
| morph    | a session renamed (or retitled by its program), a branch switched, the update chip's version      |
| decode   | a new session or worktree, a flash, a suggestion landing in the message box                       |
| dissolve | a session killed or worktree deleted from Spaces (the call waits for it), a committed message     |
| sweep    | a session that starts waiting or finishes: a band of light crosses its name once                  |
| roll     | Source Control section counts, ahead/behind, search results, `!` attention, sessions per worktree |
| shimmer  | Syncing…, Publishing…, Committing…, Searching…, downloading…, GitHub's Loading…                   |

The band of a sweep or shimmer is seven cells, its color blended in CIELAB
(`lipgloss.Blend1D`) from the text's up to the accent in the middle and back;
text styled without a color blends from the terminal's own foreground, faint
text from halfway to its background. The `terminal` theme's ANSI colors do
not blend, so there only the middle three cells light.

A morph keeps the characters both texts share, so `claude` → `claude · fix`
only scrambles the tail; keys first seen at startup or on a workspace switch
are taken as they are.
