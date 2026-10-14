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
- An open folder draws a `│` under its chevron down its children, VS Code's
  indent guides (`renderIndentGuides: always`), in `whitespace`'s color.

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
- Drawers (`Graph`, `Commits`, `File History`, `Compare`, `Branches`,
  `Worktrees`, `Remotes`, `Stashes`, `Tags`) run one git command each; shown,
  open and height live in `git_panes`.
- Drawers stack like Spaces' projects (`flipDrawer`): open ones above closed
  ones, an opened drawer on top, a closed one on top of the closed, in
  `git_drawers` order.
- A commit line (Graph, Commits, File History) shows its whole message in a
  box by the mouse after `tipDelay` (`tipBox`); its right-click menu has
  _Copy Commit Message_.
- `Compare` lists, read-only, the files one git diff range changes
  (`scmView.cmp`, `git.Compare`), as a tree or a list like the changes; ⏎ on a
  file opens preview kind `cmp`. One comparison per project (`scmView.cmps`
  keeps the others while another project shows), not saved; the ✕ on the
  header (`cmpCloseX`) or its right-click menu drops it:

| Source                                    | Range                        |
| ----------------------------------------- | ---------------------------- |
| ⏎ or click on a commit line               | `git.CommitRange`            |
| Compare with HEAD                         | `h..HEAD`                    |
| Compare Working Tree to Here              | `h`                          |
| Select for Compare, then Compare with `s` | `s..h`                       |
| Open Changed Files on a PR or its notice  | `git.FetchPR`: `base...head` |

## Spaces

Sessions belong to worktrees, worktrees to projects. Switching a session
re-roots Explorer and Source Control to its worktree.

```text
▾ ◐ Pinned                 pinned sessions, in the order pinned; folds, stays on top
 ⊙ └─ ◐ claude · fix  main · running   a copy: the session stays in the tree too

▾ ◐ repo +⎇ New Worktree  project: most demanding session's glyph (· none), button (hover)
   ⌂ main + New Session  ↺ Resume 2  the project's own checkout, buttons (hover), session count
 ⊙ ├─ ◐ claude · fix   ⧉ 2 · running   tab count, with more than its own; ⊙ pinned
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
- A working `◐` turns a quarter every `spinStep` (250 ms), in Spaces and on
  tabs, every one in step (`spinGlyph`); `spinMsg` ticks only while one
  works, and `animations = false` stops it. A plain shell, with no screen to
  read, turns only with a program other than itself in front (`works`): its
  prompt's echo reads as running for a moment. A server printing in one
  keeps it turning, a redraw every 250 ms.
- A tab strip draws the glyph on an agent's tab only, one started by hand
  in a shell tab or a Terminal panel tab too (`runsAgent`, its `Program`):
  a shell's screen reads like an agent's, so a server's logs would show as
  working.
- `session_highlight` tints blocked, exited and done rows (`sessionTint`);
  the tint pulses until the session is clicked or shown (`pulses`). A
  session row carries its tabs' tint; selected, it still pulses for one.
  `"steady"` drops the pulse, `"off"` the tint.
- `sounds = true` plays `sound_request` / `sound_done` for sessions out of
  view (`sound.go`).
- A restart brings each session's agent back
  ([01-architecture.md](01-architecture.md#resume-after-a-restart)).

### Keys

| Action         | Session                    | Worktree                                            | Project                  |
| -------------- | -------------------------- | --------------------------------------------------- | ------------------------ |
| click          | open; again puts it away   | switch                                              | fold                     |
| `x`            | kill                       | linked: delete folder, keep branch; checkout: close | close (`project.remove`) |
| drag, `alt+↑↓` | reorder (`session.move`)   | reorder (`workspace.move`)                          | reorder (`project.move`) |
| `n`, hover btn | its worktree's harness     | the harness (`harnessPicker`)                       | a worktree or a new one  |
| Resume         | menu, header: its worktree | hover btn, menu, header (`resumePicker`)            | header: which worktree   |

- A click on a session opens its one tab with a new state (`newsTab`);
  with several, the tab shown last, and again it puts the session away.
- The right-click menu groups by action, rules from `menuGroups`: create
  (New Session, Resume Session, New Worktree, Add / Open Project), the row's
  own (Rename, Pin, Move), go to / view options, then remove. A session's title
  menu: where it sits (Move to Editor Area, Dock), Rename and Pin, then Close and Kill.
- `x` kills the sessions in the way first (`killThen`), after asking.
- _New Worktree…_ (`w`) offers a random branch (`workspace.new`).
- Session reordering applies under Sort by Created + Group by Workspace only;
  an open project's under Sort Projects Manually only.
- `p`, the gutter's ⊙ (on hover) or the menu pins a session, or unpins it
  (`togglePin`, `spaces_pinned`). Pinned lists it again on top, an `agTime`
  heading (`pinnedRows`): Filter and search apply, reordering does not.
- A folded project tops the folded list (`spaces_folded`); unfolded, it goes
  last among the open ones, saved there (Sort Projects Manually only).
- `alt+t` opens the agent navigator, a picker with `@blocked`, `@running`,
  `@shell`, `@done`, `@idle`, `@exited`, `@worktree` and `!agent` filters.

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

A new session runs a harness: an `[agents]` preset whose program is on the
`PATH`, `shell` and `terminal` aside (`harnesses`), then _terminal_, a `shell`
session that opens the `shell` setting. An agent with profiles asks which
(`profilePicker`): _default_, its `[profiles.<agent>]`, then the configs the
daemon saw it run in (`seen_env`) that no profile sets; the choice goes in
`session.new`'s `env`. A project's `+` asks for its worktree first
(`worktreePicker`), _New Worktree…_ included (`sessionWorktreeMsg`). The `+`
follows the name of the row under the pointer, or of the selected one while
Spaces has the keyboard (`agents.actions`).

A worktree's _Resume_ (`resumePicker`) starts one that continues a
conversation: an installed agent's `[resume_id]` without the id, which for
claude lists the worktree's conversations (`claude --resume`), else its
`[resume]`. It asks for the agent only when more than one can, then the
profile, whose config holds the conversations. The header's `↺`, after `+`,
resumes in the selected row's worktree, a project asking which
(`agents.resume`).

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
| Changed Files              | Source Control's Compare drawer (`comparePR`)            |
| Checkout in worktree (`w`) | the branch's worktree, else a new one (`git.CheckoutPR`) |
| Start working (`w`, issue) | New Worktree prompt with `issue/<n>-<slug>`              |
| Merge…                     | `gh pr merge --match-head-commit`                        |
| Page header (`ghpage.go`)  | buttons and keys of the open `gh` page, see below        |
| Create PR, Sign in         | typed into a new Terminal tab once it is idle            |

### The conversation

`ghTimeline` (`ghtimeline.go`) fetches it with one `gh api graphql` call beside
`gh pr view`: commits, comments, reviews with their threads on the code, and
events (review requested, assigned, labeled, closed, merged, …), in order,
into `ghDoc.Timeline`. A reply joins the thread it answers. ponytail: the
first 100 items; page through `pageInfo` when a conversation is longer.

`ghLayout` (`ghlayout.go`) lays the page out from the document, replacing
the Markdown rendering for kind `gh` (`preview.markdown`): a box for a
comment, rows on the rail for the rest, each body rendered by
`renderMarkdown` at the box's width. From `ghWideMin` cells the sidebar
(`ghSide`) stands beside it; below, `ghMeta` puts its content in rows above.
`preview.ghHits` says what a click, or ⏎ on the cursor's row, does on each
row; `ghClick` takes the narrowest hit. `ghHot` is the `key` of the hit under
the pointer (`preview.hot`): every hit with it tints its `lit` cells in
`hover_bg` (a commit's row), and a key the layout draws (`ghFramed`: a box's
top, its `⋯`, the comment box) lays the page out again, the frame in
`header_accent` and the `⋯` a keycap. Frames are `sash_hover`, one that tags
the reader `warn`. The page's cursor is its row in `hover_bg`, not the
terminal's. Folds are `preview.folded`; a page from memory says how old it is
beside Refresh.

Answers stay in memory: `Model.ghPages` shows a page or a pull request's
diff seen before at once, and `load` fetches it again behind it only when it
is older than `ghTTL` (`ghCached.at`); `^r` fetches anyway, and an action
(`ghView.onDone`) makes every page old. A list older than `ghTTL` is fetched
again behind its rows (`ghView.ensure`); the lists of a project left behind
are kept (`ghView.kept`) and shown while they are fetched again. A commit's
row shows the local commit, `git fetch`ing the pull request only when it is
missing.

### The page's header

A loaded `gh` page (`preview.gh`, its `ghDoc`) draws its state and buttons
at the right end of the header row (`ghHeader`), no rows of its own; the title
is the header's context and the body starts with who opened it and the branch.
`ghActs` is the one list behind the buttons, the page's keys and its context
menu, filtered by state (a closed pull request has Reopen, no Merge or
Worktree) and grouped between rules. The `primary` one leads, with its name;
when it is of group 0 its `▾` is that group's menu. A button's tip names it
and its key by the pointer, as every other button's does. The state gives
way when the row is short, the buttons never do.

| Command                | gh                                                   |
| ---------------------- | ---------------------------------------------------- |
| Comment                | the box at the end of the page, `^s` sends it        |
| Close, Reopen, Ready   | `gh pr close`, `reopen`, `ready [--undo]`            |
| Edit Title, Reviewers… | `gh pr edit --title` or `--add-*` `--remove-*`       |

An action answers with `ghDoneMsg`, which fetches the lists and the open page
again. Each asks first (`ghConfirm`, a dialog); the merge dialog is the
method's choice and names failing checks.

The comment box is a `textarea` (`preview.compose`) drawn by `ghComposeRows` as
the last rows of the page, one a page in `Model.ghDrafts`, so closing the page
keeps what was typed. While `preview.composing` the page's `key` is
`composeKey` and `typing()` is true, so the editor's chords stay text. `^s`
(through `preview.save`) and `ctrl+enter` send; `ghSentMsg` empties the box
only when gh took the text.

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

A morph or decode is as wide as the new text from its first frame, so a
shorter name never waits to shrink, and is over in about half a second
(`fxNoise`, `fxSpread`, `fxJitter`, `morphW`); a dissolve keeps the old width.
