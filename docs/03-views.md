# Views

What each sidebar view shows and does, and where its code lives. Screen
geometry is in [02-ui-layout.md](02-ui-layout.md).

Contents: [Shape](#shape) · [Explorer](#explorer) ·
[Source Control](#source-control) · [Spaces](#spaces) ·
[Terminal](#terminal) · [Search](#search) · [GitHub](#github) ·
[Commands](#commands) · [Text effects](#text-effects)

## Shape

Every view is a struct in `internal/ui` with the same steps; copy an existing
one before inventing.

```text
lines(m, w, h) ──▶ rows(m) ──▶ l.render(w, h, n, rowFn) ──▶ screen
key(m, k), mouse(m, msg, y) ──▶ the same rows (l.at(y) for a click)
items(m) ──▶ context menu + command palette
```

| View           | Key | File          | Rows                                             |
| -------------- | --- | ------------- | ------------------------------------------------ |
| Explorer       | `1` | `explorer.go` | directory tree, git decorations                  |
| Source Control | `2` | `scm.go`      | message box, action button, sections, drawers    |
| Spaces         | `3` | `agents.go`   | project → worktree → session tree                |
| Search         | `4` | `search.go`   | query boxes, matches by file                     |
| Terminal       | `5` | `agents.go`   | shells under the editor or in a sidebar          |
| GitHub         | `6` | `github.go`   | pull requests, notifications, issues; needs `gh` |

`ctrl+f` filters a list (`startFilter`) from a framed box under the header.

## Explorer

The context menu (`m`, right click) follows VS Code's; groups are split by
`separator()` rows. Right click below the tree acts on the root, with Refresh,
Show Hidden Files and Sort by Type / Name (`explorer_sort`) added.

| Group     | File                                                           | Folder                                  |
| --------- | -------------------------------------------------------------- | --------------------------------------- |
| create    | New File…, New Folder… beside it                               | New File…, New Folder… inside           |
| open      | Open Containing Folder, Open with Default App, Edit in $EDITOR | Open Containing Folder, Find in Folder… |
| clipboard | Cut, Copy, Paste                                               | the same                                |
| copy      | Copy Name, Copy Path, Copy Relative Path                       | the same                                |
| change    | Duplicate…, Rename…, Delete…                                   | the same                                |
| git, view | Stage Changes, Collapse All                                    | the same                                |

- Cut `ctrl+x` / Copy `ctrl+c` keep one entry in `explorer.clip`, pando's
  own clipboard. Paste `ctrl+v` copies onto a taken name as `name copy.ext`; a move
  never replaces and falls back to copy + delete across file systems.
- Duplicate offers `main copy.go`, then `main copy 2.go` (`os.CopyFS` for a
  folder).
- Find in Folder opens Search with `dir/**`, globs escaped.

## Source Control

```text
 SOURCE CONTROL                repo · main
 ▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁   input boxes framed in input_border
 ▏Message (⏎ to commit on "main")       ▏∨▕   message box, ∨ suggest menu
 ▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔   accent while focused
               ✓ Commit                 ▏∨    action button
 ▾ Merge Changes                       [1]   only while conflicts exist
   main.go · both modified       src +   !
 ▾ Staged Changes                      [1]
   api.go src                    src ↶ − A   hover actions
 ▾ Changes                             [2]
 ▾ Untracked Changes                   [1]
 ─────────────────────────────────────────
 ▸ Graph ▸ Commits ▸ File History …          drawers, drag a header to resize
```

| Section           | Notes                                                                                    |
| ----------------- | ---------------------------------------------------------------------------------------- |
| Merge Changes     | no discard; staging marks resolved, asks if markers remain; `UD`/`DU` ask keep or delete |
| Staged, Changes   | Stage All and `+` leave merge entries alone                                              |
| Untracked Changes | `↶` deletes after a dialog (`confirmDeleteUntracked`, `git.DeleteUntracked`)             |

`d` runs the row's `↶`. Scrolled, the repository head and current section
header stick to the top (`scmView.pinned`).

The action button (`scmView.action`):

| State                                    | Button                                  |
| ---------------------------------------- | --------------------------------------- |
| merge, rebase or cherry-pick in progress | _Continue_, once Merge Changes is empty |
| anything staged, changed or conflicted   | Commit, with a `∨` menu                 |
| no upstream                              | _Publish Branch_ (`push -u`); `S` too   |
| ahead or behind                          | _Sync Changes_                          |
| otherwise                                | muted Commit                            |

A Commit with nothing staged is muted too, and a click on it does nothing.

- Publish (`git.publish`): one remote pushes, several open a picker, none
  offers _Publish to GitHub_ via `gh`.
- During a merge or cherry-pick an empty box commits `MERGE_MSG`.
- The box grows to `maxMsgLines` (10). The `∨` opens `suggestMenu`
  (`git.SuggestOpts`): Generate Commit Message (`A`) and variants; while
  `claude` writes, the box draws `scramble`.
- `↑` on the box's top line walks back through `git.Messages`, the draft
  first in `scmView.past`; `↓` on the bottom line walks forward to it.
- Drawers (`Graph`, `Commits`, `File History`, `Branches`, `Worktrees`,
  `Remotes`, `Stashes`, `Tags`) run one git command each; shown, open and
  height live in `git_panes`.

## Spaces

Sessions belong to worktrees, worktrees to projects. Switching a session
re-roots Explorer and Source Control to its worktree.

```text
▾ ◐ repo                project: most demanding session's glyph (· none)
   ⌂ main +         2   the project's own checkout, + (hover), session count
   ├─ ◐ claude · fix   ⧉ 2 · running   tab count, with more than its own
   └─ ○ shell          idle
   ⑂ feat/x         1   linked worktree (dimmed)
   └─ ○ shell          idle
                        gap: only after an unfolded project
▾ ○ other
   ⌂ main           1
   └─ ○ shell          idle

▸ · folded              folded projects sink to the bottom (pinFolded)
```

### Status

The daemon decides each status
([01-architecture.md](01-architecture.md#status)); Spaces
draws herdr's glyphs, one per state (`sessionGlyph`, `sessionState`; the
table is in [usage.md](usage.md#sessions)). `done` is `idle` with
`attention`; an `exited` row is a failure, shown as `exit N`.

- Rows roll up the most demanding glyph of what they hold (`groupGlyph`).
- `session_highlight` tints blocked, exited and done rows (`sessionTint`);
  the tint pulses until the session is clicked or shown (`pulses`). A
  session row carries its tabs' tint; selected, it still pulses for one.
  `"steady"` drops the pulse, `"off"` the tint.
- `sounds = true` plays `sound_request` / `sound_done` for sessions out of
  view (`sound.go`).
- A restart brings each session's agent back
  ([01-architecture.md](01-architecture.md#resume-after-a-restart)).

### Keys

| Action         | Session                  | Worktree                                            | Project                  |
| -------------- | ------------------------ | --------------------------------------------------- | ------------------------ |
| click          | open; again puts it away | switch                                              | fold                     |
| `x`            | kill                     | linked: delete folder, keep branch; checkout: close | close (`project.remove`) |
| drag, `alt+↑↓` | reorder (`session.move`) | reorder (`workspace.move`)                          | reorder (`project.move`) |
| `n`, hover `+` | its worktree's harness   | the harness (`harnessPicker`)                       | a worktree or a new one  |

- A click on a session opens its one tab with a new state (`newsTab`);
  with several, the tab shown last, and again it puts the session away.
- `x` kills the sessions in the way first (`killThen`), after asking.
- _New Worktree…_ (`w`) offers a random branch (`workspace.new`).
- Session reordering applies under Sort by Created + Group by Workspace only;
  an open project's under Sort Projects Manually only.
- A folded project tops the folded list (`spaces_folded`); unfolded, it goes
  last among the open ones, saved there (Sort Projects Manually only).
- `alt+t` opens the agent navigator, a picker with `@blocked`, `@running`,
  `@done`, `@idle`, `@exited`, `@worktree` and `!agent` filters.

### View options

`o` or the header's sliders (`agents.viewMenu`). Filter, Sort and Group are
settings (`spaces_hide`, `spaces_sort`, `spaces_group`) read by
`listedSessions`; the projects' Sort (`spaces_project_sort`) by `projects`.

| Option              | Effect                                               |
| ------------------- | ---------------------------------------------------- |
| Filter ›            | the states shown                                     |
| Sort by Created     | creation order (`SessionSpec.Created`)               |
| Sort by Updated     | newest output first (`Session.Updated`)              |
| Sort Projects …     | saved order, or newest session output first          |
| Group by Workspace  | the tree above                                       |
| Group by Time       | `agTime` headings Today … Older; rows add the branch |
| Collapse All Groups | folds every project or heading                       |

### Sessions and tabs

A new session runs a harness, never a bare shell: an `[agents]` preset whose
program is on the `PATH`, `shell` and `terminal` aside (`harnesses`); none
installed, the picker is empty. A project's `+` asks for its worktree first
(`worktreePicker`), _New Worktree…_ included (`sessionWorktreeMsg`). The `+`
follows the name of the row under the pointer, or of the selected one while
Spaces has the keyboard (`agents.actions`).

A session has tabs (`tabsOf`): `+` adds a shell that dies with it, `[` `]`
cycle, middle click or `✕` kills after asking. A session with more than its
own tab shows their count in its row (`icTabs`).

## Terminal

- ``ctrl+` `` or `5` toggles the panel ([keys](09-keys.md#terminals));
  `terminal_position` docks it in a sidebar.
- It holds `terminal` agent sessions, hidden from Spaces. A shell's `parent`
  is the session it was opened under (`termOwned`) and dies with it.
- Switching sessions re-attaches the panel (`attachTerm`); an open panel with
  no shell starts one (`ensureTerm`).
- `ctrl+f` is VS Code's terminal find (`termfind.go`): it searches
  `session.read` scrollback, scrolls a match to the middle, and searches
  again on every screen update.

## Search

Workspace search 250 ms after the last key, capped at `maxSearchMatches`.
Engine: `rg --json`, else `git grep`, else `grep -rnIZ`.

```text
  ▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁
 ▾▏ query                       Aa ab .* ▕   ▾ opens replace (ctrl+h)
  ▏ replace                        AB ⇄ ▕   AB preserve case, ⇄ replace all
  ▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔   one frame round both
    files to include                        ⋯ opens include / exclude
  ▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁
  ▏ e.g. *.ts, src/**                   ▕
  ▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔
 2 results in 1 file              tree  ⋯   tree groups by folder
 ▾ main.go                             [2]
     println("hello world")                ⏎ opens with the match selected
```

`r` replaces the selected line or file, `R` all after asking. A file changed
since the search is refused; there is no undo.

## GitHub

VS Code's GitHub Pull Requests view over `gh`. `hasGH` is read once at start;
without gh the view does not exist.

```text
 ▾ PULL REQUESTS
   ▸ Local Pull Request Branches     All Open with a local branch
   ▸ Waiting For My Review           --search review-requested:@me
   ▸ Assigned To Me                  --assignee @me
   ▸ Created By Me                   --author @me
   ▾ All Open                    3   gh pr list --limit 50
      PR release v0.4.0 #14 @ann ⑂ ✓ worktree, checks
 ▾ NOTIFICATIONS               1 ⇕   unread
 ▸ ISSUES                            folded: pinned at the bottom
```

- Open panes share the height (`panes`, `ghView.h`); drag a header to resize.
- A folder is fetched once when shown (`ensure`); `ctrl+r` refetches. No timer.
- `ghErrKind` maps gh's stderr to: not a GitHub repository, no remote
  (offers Publish), sign in (`gh auth login`), or the first error line.

| Action                     | How                                                      |
| -------------------------- | -------------------------------------------------------- |
| Description (⏎)            | preview kind `gh` (`ghMarkdown`)                         |
| Changes (`d`)              | preview kind `ghdiff`, `gh pr diff`                      |
| Checkout in worktree (`w`) | the branch's worktree, else a new one (`git.CheckoutPR`) |
| Start working (`w`, issue) | New Worktree prompt with `issue/<n>-<slug>`              |
| Merge…                     | `gh pr merge --match-head-commit`                        |
| Create PR, Sign in         | typed into a new Terminal tab once it is idle            |

## Commands

A view's `items()` feeds its context menu. The palette (`ctrl+shift+p`, `F1`)
lists `commands()`: each view's `items()` plus `viewItems`, `settingsItems`,
`updateItems` and the editor's `keyItems`, labels prefixed "Category: ", the
last used first.

`commandID` turns a label into its `[keys]` id: "Git: Switch Branch…" →
`git.switchBranch`. Renaming a label changes the id and silently breaks user
bindings ([06-config.md](06-config.md#keys), [09-keys.md](09-keys.md)).

## Text effects

`fx.go`: `noteFx` compares shown text after every `Update` and starts an
effect keyed by its subject (`sess:ID`, `ws:PATH`); drawing asks `fx.text` or
`fx.segs`. One `fxTickMsg` ticker (`fxFrame`) runs while anything animates;
`animations = false` turns it off.

| Effect   | On                                                    |
| -------- | ----------------------------------------------------- |
| morph    | session renamed, branch switched, update version      |
| decode   | new session or worktree, a flash, a commit suggestion |
| dissolve | session killed, worktree deleted, message committed   |
| sweep    | session starts waiting or finishes                    |
| roll     | counts: changes, ahead/behind, results, sessions      |
| shimmer  | Syncing…, Committing…, Searching…, Loading…           |
