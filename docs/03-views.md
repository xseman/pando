# Views

What each sidebar view shows and does, and where its code lives. Screen
geometry is in [02-ui-layout.md](02-ui-layout.md).

Contents: [Shape](#shape) · [Explorer](#explorer) ·
[Source Control](#source-control) · [Sessions](#sessions) ·
[Terminal](#terminal) · [Search](#search) · [Quick open](#quick-open) ·
[GitHub](#github) · [Databases](#databases) ·
[Customizations](#customizations) · [Usage](#usage) ·
[Commands](#commands) · [Text effects](#text-effects)

## Shape

Every view is a struct in `internal/ui` with the same steps; copy an existing
one before inventing.

```text
lines(m, w, h) ──▶ rows(m) ──▶ l.render(w, h, n, rowFn) ──▶ screen
key(m, k), mouse(m, msg, y) ──▶ the same rows (l.at(y) for a click)
items(m) ──▶ context menu + command palette
```

| View           | Key | File           | Rows                                                                         |
| -------------- | --- | -------------- | ---------------------------------------------------------------------------- |
| Explorer       | `1` | `explorer.go`  | directory tree, git decorations, problems (`probDecoFor`)                    |
| Source Control | `2` | `scm.go`       | message box, action button, sections, drawers                                |
| Sessions       | `3` | `agents.go`    | project → worktree → session tree                                            |
| Search         | `4` | `search.go`    | query boxes, matches by file                                                 |
| Terminal       | `5` | `agents.go`    | shells under the editor or in a sidebar                                      |
| GitHub         | `6` | `github.go`    | pull requests, notifications, issues; needs `gh`                             |
| Databases      | `7` | `databases.go` | SQLite files, `.env` and `[databases]` servers, their tables; needs a client |
| Customizations | `8` | `custom.go`    | a tab of Sessions: what one agent loads in the worktree; needs a known agent |
| Usage          | `9` | `usage.go`     | a tab of Sessions: the agents' plan limits by sign-in; needs `usage` on      |

`ctrl+f` filters a list (`startFilter`) from a framed box under the header;
Sessions' header has a search button for it.

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
  indent guides (`renderIndentGuides: always`), in `whitespace`'s color; the
  active one, the selected open folder's or else the selection's parent's
  (`activeGuide`), in `indent_guide`'s. Every tree draws them so (`guides.at`):
  Search, Source Control (a section's files, Compare's tree), GitHub (a
  query's results), Databases (a source's tables), the references peek.
- A row without children leaves its chevron's two cells blank, VS Code's
  empty twistie, so a file sits a level right of its folder's name.

## Source Control

```text
 SOURCE CONTROL                repo · main
 ▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁   input boxes framed in input_border
 ▏Message (⏎ to commit on "main")       ▏∨▕   message box, ∨ suggest menu
 ▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔▔   accent while focused
               ✓ Commit                 ▏∨    action button
 ▾ Merge Changes                       [1]   only while conflicts exist
     main.go · both modified     src +   !
 ▾ Staged Changes                      [1]
     api.go src                  src ↶ − A   hover actions
 ▾ Changes                             [2]
 ▾ Untracked Changes                   [1]
 ───────────────────────────────────────
 ▾ Tools                              5      strip; on hover ⊟, close all drawers, for the count
 ───────────────────────────────────────
 ▸ Graph ▸ Commits ▸ File History …          drawers, drag a header to resize
```

| Section           | Notes                                                                                    |
| ----------------- | ---------------------------------------------------------------------------------------- |
| Merge Changes     | no discard; staging marks resolved, asks if markers remain; `UD`/`DU` ask keep or delete |
| Staged, Changes   | Stage All and `+` leave merge entries alone                                              |
| Changes           | a folder's `↶` runs `git checkout -- <folder>` after a dialog (`confirmDiscard`)         |
| Untracked Changes | `↶` deletes after a dialog (`confirmDeleteUntracked`, `git.DeleteUntracked`)             |

`d` runs the row's `↶`. Scrolled, the repository head and current section
header stick to the top (`scmView.pinned`).

The action button (`scmView.action`):

| State                                    | Button                                |
| ---------------------------------------- | ------------------------------------- |
| merge, rebase or cherry-pick in progress | _Continue_                            |
| anything staged, changed or conflicted   | Commit, with a `∨` menu               |
| no upstream                              | _Publish Branch_ (`push -u`); `S` too |
| ahead or behind                          | _Sync Changes_                        |
| otherwise                                | muted Commit                          |

A Commit with nothing staged is muted too, and a click on it does nothing.

- Publish (`git.Publish`): one remote pushes, several open a picker, none
  offers _Publish to GitHub_ via `gh`.
- During a merge or cherry-pick an empty box commits `MERGE_MSG`.
- The box grows to `maxMsgLines` (10). The `∨` opens `suggestMenu`
  (`git.SuggestOpts`): Generate Commit Message (`A`) and variants, then a row
  naming `commit_agent` and `commit_style` that opens them in Settings; while
  the agent writes, the box draws `scramble`.
- `↑` on the box's top line walks back through `git.Messages`, the draft
  first in `scmView.past`; `↓` on the bottom line walks forward to it.
- Drawers (`Graph`, `Commits`, `File History`, `Compare`, `Branches`,
  `Worktrees`, `Remotes`, `Stashes`, `Tags`) run one git command each
  (`git.Drawers`), File History the selected file's log, Compare its range;
  which show and their order live in `git_drawers`, open and height in
  `git_panes`.
- Drawers stack like Sessions' projects (`flipDrawer`): open ones above closed
  ones, an opened drawer on top, a closed one on top of the closed, in
  `git_drawers` order. _Collapse All Drawers_ (the strip's ⊟ or a header's
  menu; `collapseDrawers`) closes the open ones where they show; the view's
  Collapse All (`collapseAll`) closes them with the tree.
- The drawers sit under a `▾ Tools  N` strip (`toolsRow`), as Sessions' No
  Sessions: `toolsActions` take the place of N while the mouse is over the
  drawers, lingering as the header's do. Folded (`git_drawers_folded`), the
  strip is all that shows, the drawers not built nor loaded, each keeping
  whether it is open; a comparison opened unfolds them:

| On the strip or the rule over it (`hover_bg`) | Does                                                   |
| --------------------------------------------- | ------------------------------------------------------ |
| ⊟ on the strip, while one is open             | _Collapse All Drawers_ (`collapseDrawers`)             |
| a click off it                                | folds or unfolds them, as the view menu's _Fold Tools_ |
| right click                                   | the drawers' menu (`drawerMenu`)                       |

- A commit line (Graph, Commits, File History) shows its whole message in a
  box by the mouse after `tipDelay` (`tipBox`); its right-click menu has
  _Copy Commit Message_.
- `Compare` lists, read-only, the files one git diff range changes
  (`scmView.cmp`, `git.Compare`), as a tree or a list like the changes; ⏎ on a
  file opens preview kind `cmp`. One comparison per project (`scmView.cmps`
  keeps the others while another project shows), not saved; the ✕ on the
  header (`cmpCloseX`) or its right-click menu drops it. The empty drawer's
  line and the header's menu open `comparePicker`: the refs but HEAD, or a
  revision typed into the filter, `#` dropped as GitLens takes it:

| Source                                    | Range                        |
| ----------------------------------------- | ---------------------------- |
| ⏎ or click on a commit line               | `git.CommitRange`            |
| Compare with HEAD                         | `h..HEAD`                    |
| Compare Working Tree to Here              | `h`                          |
| Select for Compare, then Compare with `s` | `s..h`                       |
| Compare `b` with… (`comparePicker`)       | `r..HEAD`                    |
| Open Changed Files on a PR or its notice  | `git.FetchPR`: `base...head` |

## Sessions

Sessions belong to worktrees, worktrees to projects. Switching a session
re-roots Explorer and Source Control to its worktree.

```text
▾ ◐ Pinned            1   a pane over the tree (pinPane): its strip, folds, stays on top
▾ ◐ repo  ⌂ main       1   a pinned session's project heading (pinnedGroup), folds as pin:…, holds the checkout as the tree's
 ⊙ └─ ◐ claude · fix   running   pinned sessions in the order pinned, copies: each stays in the tree too
━━━━━━━━━━━━━━━━━━━━━━━━━━  its border, heavy in tab_border
▾ ◐ repo +⎇ New Worktree  project: most demanding session's glyph (· none), button (hover)
   ⌂ main + New Session  ↺ Resume ▥ 2  the project's own checkout, buttons (hover; ▥ Split Sessions; one that does not fit before the count is left out), session count
 ⊙ ├─ ◐ claude · fix   running ✕       ⊙ pinned, ✕ kills it (hover)
   │  ↳ Explore · Trace tabs        a subagent it runs (agSub): ↑↓ walk past, a click is its session's
   └─ ○ shell            3h   idle since (shortAgo); a shell says shell, other states their word
   └─ ○ pr-737   work · 2m   the profile it runs in off its agent's default (profileOf), its first letter where it would cut the name
   ⑂ feat/x       1 ✕   linked worktree (dimmed), ✕ deletes it (hover)
   └─ ○ shell          idle   no Updated yet
──────────────────────────  a rule over every open heading but the first (withGaps)
▾ ○ other  ⌂ main + ↺ ⎇  its checkout alone: on the heading (merged), buttons as glyphs with tips
   └─ ○ shell           12m
                          a blank, no rule; blanks sink what follows to the bottom edge (pinFolded)
▾ recent              3   a dim caption over the three not on top used last, newest first (recentHead, usedLast)
▸ ◌ verba             1
                          a blank, no rule
▾ other projects     24   a dim caption over the rest (bareHead), folds as a heading
▸ ◌ beratung          1   A–Z, a tight list: every session asleep, or folded: not bold, no button, a session count
     dotfiles  + ⎇       no session (bare): dimmed, no glyph, no fold, buttons as a heading holding its checkout
▾ ◌ super.dental  ⌂ main  asleep and opened in place (opened): its sessions under it, still A–Z
   └─ ◌ wails-v3     asleep
▸ ◐ templates         1   folded by hand, here though a session works
```

- A project heading holds its own checkout when that is its only worktree
  (`merged`): its row goes, `n` and `+` start in it, a click on its name or
  `⌂ main` or `⏎` switches to it. A linked worktree splits them again.
- A project heading folds on a click on its chevron or `space` (`click`); a
  click elsewhere switches to its first worktree, opening it if folded. The
  project in view only selects, staying on its worktree. The chevron under
  the pointer is raised as a button, as the pin is. Chevrons are the codicon
  twisties (`icTwistyR`, `icTwistyD`) with Nerd Font icons, `▸` `▾` without.
- On top go the projects with a session awake (`awake`: not asleep, or the
  one on screen) and not folded, in the project Sort's order
  (`topProjects`). A pin holds a project there unfolded, asleep or not
  (`pinHeld`): a project pinned whole, or one with a session pinned in it.
  Its heading does not fold, and it never goes under the captions. Every other project goes under _other projects_
  (`bareHead`), A–Z, a row each: folded, every session asleep, or no session
  at all (`bare`). A switch into one of those with no session lifts it on
  top for five minutes (`pick`, `pickFor`, `lift`): a session started there
  keeps it, else it sinks back. The three of them used last (`usedLast`:
  their newest session's Updated) go above it under _recent_ (`recentHead`),
  newest first, when that leaves some under the caption; not while
  filtering. Each caption counts its own. Nothing moves under them.
- One asleep opens in place on its chevron (`openInPlace`, `opened`) and goes
  on top once a session in it wakes, at its place in the order; asleep
  again, it is shut. A switch into one opens it so (`reveal`). The project
  of the session on screen stays on top, asleep or not.
- A project with no session: a click or `⏎` switches to its checkout, `n`
  starts a session as on a heading.
- `/` or `ctrl+f` filters (`matching`): a row holding every word typed, in
  any case and without accents (`hits`), in a session's name and title but
  not its agent, a worktree's branch, a project's name, its folder's and the
  branch of the checkout it holds (`searchText`). A project found brings all it holds; one under the caption
  shows there, under it. The first session found is selected (`firstHit`),
  `⏎` opens it, as a click on its name, and clears the filter (`openHit`);
  a session opened from the list, a project or worktree clicked or switched
  to, clears it too (`unfilter`); a fold keeps it.
- A session row says its name, then its title where the name does not say
  it (`titleAfter`). A title starting with the issue key a set name ends in
  (`DEV-43739 …` for `jira-dev-43739`) takes the name's place (`keyedBy`).
- Scrolled down, the heading of the tree's first row covers that line
  (`sticky`), VS Code's sticky scroll: a click there is the heading's, and a
  selection under it scrolls into view. Off while a row drags.

### Status

The daemon decides each status
([01-architecture.md](01-architecture.md#status)); Sessions
draws herdr's glyphs, one per state (`sessionGlyph`, `sessionState`; the
table is in [usage.md](usage.md#sessions)). `done` is `idle` with
`attention`; an `exited` row is a failure, shown as `exit N`.

- Rows roll up the most demanding glyph of what they hold (`groupGlyph`).
- A working `◐` turns a quarter every `spinStep` (250 ms), in Sessions and on
  tabs, every one in step (`spinGlyph`); `spinMsg` ticks only while one
  works, and `animations = false` stops it. A plain shell, with no screen to
  read, turns only with a program other than itself in front (`works`): its
  prompt's echo reads as running for a moment. A server printing in one
  keeps it turning, a redraw every 250 ms.
- A tab strip draws the glyph on an agent's tab only, one started by hand
  in a Terminal panel tab too (`runsAgent`, its `Program`):
  a shell's screen reads like an agent's, so a server's logs would show as
  working.
- `session_highlight` tints blocked, exited and done tabs (`sessionTint`);
  the tint pulses until the session is clicked or shown in the focused
  group (`pulses`); shown in another group it pulses on that group's frame. A
  Sessions row is never tinted, so none reads as selected: its status says
  `input`, `done` or `exit N` in its symbol's color, bold on the pulse's
  beat. `"steady"` drops the pulse, `"off"` the tint and
  the color.
- The session on screen has the accent's `▎` in its row's first cell and
  its name in the accent (`header_accent`).
- `sounds = true` plays `sound_request` / `sound_done` for sessions out of
  view (`sound.go`).
- A restart brings each session's agent back
  ([01-architecture.md](01-architecture.md#resume-after-a-restart)).

### Keys

| Action         | Session                    | Worktree                                            | Project                  |
| -------------- | -------------------------- | --------------------------------------------------- | ------------------------ |
| click          | open; on screen, focus     | switch                                              | fold                     |
| `x`            | kill                       | linked: delete folder, keep branch; checkout: close | close (`project.remove`) |
| drag, `alt+↑↓` | reorder (`session.move`)   | reorder (`workspace.move`)                          | reorder (`project.move`) |
| `n`, hover btn | its worktree's harness     | the harness (`harnessPicker`)                       | a worktree or a new one  |
| Resume         | menu, header: its worktree | hover btn, menu, header (`resumePicker`)            | header: which worktree   |

- The right-click menu groups by action, rules from `menuGroups`: create
  (New Session, Resume Session, New Worktree, Add / Open Project), the row's
  own (Rename, Pin, Move), go to / view options, then remove. A session's title
  menu: Rename and Pin, then Close, Sleep and Kill.
- `x` kills the sessions in the way first (`killThen`), after asking; a
  session's or a linked worktree's `✕` (on hover, at its row's right end)
  is its `x`.
- _New Worktree…_ (`w`) offers a random branch (`workspace.new`).
- A worktree with two sessions or more has _Split Sessions_ (`▥`, hover,
  `splitSessions`): a group each, side by side and as wide as each other,
  the files of the groups it had in the first.
- Session reordering applies under Sort by Created + Group by Workspace only;
  a project on top's under Sort Projects Manually only.
- `p`, the gutter's ⊙ (on hover) or the menu pins a session, or unpins it
  (`togglePin`, `spaces_pinned`). Pinned lists it again on top, an `agTime`
  heading (`pinnedRows`), under its project's heading (`pinnedGroup`, folded
  as `pin:…`); a worktree row sits between, or the heading holds the
  checkout and its branch when every pinned session runs there, as the
  tree's does. Filter and search apply, reordering does not.
- `p` or the menu on a project pins it whole (its path in `spaces_pinned`):
  under Pinned it lists as the tree lists it open, every worktree and
  session, asleep too, its fold its own (`pinKey`, `pin:…`). A session it
  holds pinned on its own shows under it once. The Filter takes each part
  of Pinned apart.
- Pinned is a pane as tall as its rows, at most a third of the view; past
  that it scrolls on its own (`ptop`), the tree under it too (`l.top`). One
  selection walks both (`snap`, `rowAt`).
- A folded project goes under _other projects_ (`spaces_folded` keeps the
  folds); unfolded, it goes back to its place on top. A pinned one does not
  fold, so it stays on top; unpinned, its fold counts again. _Collapse All Groups_
  leaves the caption open.
- A session going to work (to `running`) opens its folded project
  (`unfoldRunning`); folded again while it works, it stays shut. A session
  the list did not hold yet, every one on start, opens nothing; nor does one
  in the first 30 s after a reconnect, a restarted daemon's agents coming back
  (`resumeGrace`).
- `alt+t` opens quick open on its Sessions tab (`sessionTree`), with
  `@blocked`, `@running`, `@shell`, `@done`, `@idle`, `@exited`, `@asleep`
  and `!agent` filters.
- `alt+a` opens the next session waiting on you (`alarms`): blocked, then
  ended with an error, then finished unseen, the one waiting longest first in
  each. Each press walks on from the session on screen.

### View options

`o` or the header's sliders (`agents.viewMenu`). Filter, Sort and Group are
settings (`spaces_hide`, `spaces_sort`, `spaces_group`) read by
`listedSessions`; the projects' Sort (`spaces_project_sort`) by `projects`.

| Option              | Effect                                               |
| ------------------- | ---------------------------------------------------- |
| Filter ›            | the states shown                                     |
| Sort by Created     | creation order (`SessionSpec.Created`)               |
| Sort by Updated     | newest input, question or real work (`Updated`)      |
| Sort Projects …     | saved order, or newest session change first          |
| Group by Workspace  | the tree above                                       |
| Group by Time       | `agTime` headings Today … Older; rows add the branch |
| Group by State      | Blocked … Asleep, counted; rows add project, age     |
| Collapse All Groups | folds every project or heading                       |

### Sessions and tabs

A new session runs a harness: an `[agents]` preset whose program is on the
`PATH`, `shell` and `terminal` aside (`harnesses`), then _terminal_, a `shell`
session that opens the `shell` setting. The harnesses of the three newest
sessions come first, under _recently opened_. An agent with profiles asks which
(`profilePicker`): _default_, its `[profiles.<agent>]`, then the configs the
daemon saw it run in or found (`seen_env`) that no profile sets (`profiles`); the choice
goes in `session.new`'s `env`. A row says how spent its account is (`room`),
and the picker opens on the active one (`State.Active`, `item.lead`). A project's `+` asks for its worktree first
(`worktreePicker`), _New Worktree…_ included (`sessionWorktreeMsg`). The `+`
follows the name of the row under the pointer, or of the selected one while
Sessions has the keyboard (`agents.actions`).

A worktree's _Resume_ (`resumePicker`) starts one that continues a
conversation: an installed agent's `[resume_id]` without the id, which for
claude lists the worktree's conversations (`claude --resume`), else its
`[resume]`. It asks for the agent only when more than one can, then the
profile, whose config holds the conversations. The header's `↺`, after `+`,
resumes in the selected row's worktree, a project asking which
(`agents.resume`).

A session is one tab: its strip has no `+`, shells live in the worktree's
Terminal panel.

_Move to Profile…_ (`moveProfile`) shows where the daemon knows the session's
conversation, its agent has a `[resume_id]` and another profile to go to
(`canMove`): the other profiles, then `session.profile`.

## Terminal

- ``ctrl+` `` or `5` toggles the panel ([keys](09-keys.md#terminals));
  `terminal_position` makes it a tab of a sidebar, its tabs with it; drag any
  of them there, and back under the editor, where nothing else goes.
- It holds the worktree's `terminal` agent sessions (`termOwned`), hidden from
  Sessions, whichever session is shown. One a script gave a `parent` shows there
  too and dies with that session; the panel's own have none.
- Switching sessions keeps the shell in view, switching worktrees re-attaches
  the panel (`attachTerm`); an open panel with no shell starts one
  (`ensureTerm`).
- `ctrl+f` is VS Code's terminal find (`termfind.go`): it searches
  `session.read` scrollback, scrolls a match to the middle, and searches
  again on every screen update.
- Links (`termlink.go`), in a session and in the panel: `termLinks` takes a
  screen line's OSC 8 runs, then the URLs and paths of its text
  (`textLinks`: a dot, a slash or a `:12` after it; `lineRe` the positions
  VS Code reads, `file:///a.mjs:12:5` too; a quoted path with spaces when
  a position follows it, as Python prints one). Rows whose text reaches the
  last column (`Screen.Cols`; trailing blanks do not count) run on into the
  next (`runOn`), so a wrapped path is one
  link. The disk decides on the pointer: `linkAt` resolves a path in the
  directory of the program in front (`Screen.Cwd`, from `/proc`), then the
  session's worktree (also without a diff's `a/` `b/`); one not
  there but with a line, or with a directory and an extension, is a search
  (`openQuery`: the one file ending in it, else quick open). Hover underlines it with VS Code's tip; ctrl+click
  (`followLink`) goes ahead of a mouse app.

## Search

Workspace search 250 ms after the last key, capped at `maxSearchMatches`.
Engine: `rg --json --max-columns=1000 --max-columns-preview`, else `git
grep`, else `grep -rnIZ`. The results show while it runs: `runSearch`
hands over the matches so far, sorted, at once and then every
`searchBatch` (100 ms), the latest only (`waitSearch`); the selection stays
on its line.

```text
 ▾ ❱ query                    Aa ab .*   ▾ opens replace (ctrl+h); ❱ in the accent with the keyboard
   ❱ replace                      AB ⇄   AB preserve case, ⇄ replace all
 ──────────────────────────────────── ▾   ▾ opens include / exclude (ctrl+i)
   include ❱ e.g. *.ts, src/**
   exclude ❱ e.g. **/dist
  2 results in 1 file               tree  tree groups by folder; Searching… while it runs
 ─────────────────────────────────────
 ▾ main.go                             2
   12: println("hello world")             the line number; ⏎ opens with the match selected
```

Matches are marked in `match_bg` (yellow on the light theme), their text
in `match_fg` where a theme gives one (the terminal theme), as in the
references peek.

`r` replaces the selected line or file, `R` all after asking. A file changed
since the search is refused; there is no undo.

## Quick open

`ctrl+p` (`quickopen.go`): VS Code's quick open with JetBrains' Search
Everywhere tabs in its title row; `tab` `shift+tab` or a click go round them.
The tab's keys sit in the bottom edge (`qoHints`).

| Tab      | Lists                                                                    |
| -------- | ------------------------------------------------------------------------ |
| All      | files, commands, sessions, text; a query caps them at 8, 3, 3 and 5 rows |
| Files    | the worktree's files, the 5 opened last first                            |
| Sessions | the Sessions tree, what `alt+t` opens                                    |
| Text     | the lines holding the query under their files, counted if the tabs fit   |
| Actions  | the Command Palette, the command run last first; `ctrl+shift+p` opens it |

- Empty, All lists the recent files, up to 5 files git status lists
  (`changed`, its letter at the right, the recent and deleted ones left
  out), the sessions waiting on you (`alarms`) and the first 8 other files,
  `+ N more` going to Files for the rest (`browseAll`). A query
  lists files first, so a name and `⏎` open the file as before; a capped
  group ends in `+ N more`, which goes to its tab. All's commands hold every
  word of the query (`actionsFor`): the letters alone would match most.
- `:` line, `@` symbol and `path:12:5` work in All and Files only; `>` goes
  to Actions from any tab but Sessions and Text, and leaves the query.
- `ctrl+t` or a click on its switch at the right of the title row cycles
  the views of a file (`quick_open_view`): `path`, `name` (its icon and name,
  its folder dim after) and `tree`, Files' only; the tree shows as `name`
  elsewhere. All, Files and Text offer it.
- Sessions is the Sessions tree (`treeRows` with every project open), with
  no chevrons, rules, captions, Pinned pane or subagents: the projects with a
  session awake in their order, the 3 used last, the rest A–Z, one with no
  session dimmed. A query finds as the tree's filter does (`hits`, `matching`)
  and lights the words found (`wordRuns`); `@state` and `!agent` narrow the
  sessions, a project staying with those left. A session awake says its
  state in its glyph's color, an asleep one since when. `⏎` on a project
  goes to its checkout (on top, `lift`), on a worktree to it. Selected: the
  session on screen, under a query the first session found. All shows the
  first 3 sessions found the same way, under their projects, selected when
  no file is found; a project with no session counts as one.
- Text is `runSearch` as Search runs it: literal, any case, from 2
  characters, 150 ms after the last key (`quickTextWait`), stopped at 200
  matches (`quickTextLimit`): which 200 is up to rg's threads, `--sort=path`
  costs 7× on a miss. It lands after the selection and leaves it
  where it is. A line drops its indentation and starts `…` a little before a
  match that would not fit (`excerpt`). Text lists each file's row, its
  matches counted, and its lines under it, numbered (`byFile`); All the
  lines alone, `path:line` at the right. `alt+⏎` in All and Text hands the
  query to Search.
- The box keeps one width whatever a tab lists: 2/3 of the screen, 60 to
  120 columns.

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

## Databases

Databases and their tables through their clients (`internal/db`): sqlite3,
psql, mariadb or mysql. `hasDB` (one of them on the `PATH`) is read once at
start; without any the view does not exist.

```text
 DATABASES                   +  ⟳  -
 ▾ app.db
     ◫ adults                 view    a view: read only
     ▦ orders                         ⏎ opens it in an editor tab
 ▸ cache.sqlite data                  its tables are read when it unfolds
 ▸ .env                    mariadb    the worktree's .env DATABASE_URL
 ▾ local                  postgres    a [databases] entry, by name
     ▦ audit.events                   a schema other than the connection's
```

- Files: `git ls-files -co` for `*.db`, `*.sqlite`, `*.sqlite3`, ignored ones
  too (`git.FilesNamed`), kept when the header says SQLite (`db.IsSQLite`: no
  Thumbs.db). Listed again while shown once `dbTTL` (10 s) passed, and on `r`.
- Explorer and quick open hand a SQLite file over (`openFile` → `revealDB`):
  the view shows it unfolded, kept while its workspace shows (`extra`) when
  the listing does not find it.
- Servers (`sources`): `DATABASE_URL` in the worktree's `.env` (`envURL`, read
  with the listing), then `[databases]` from config.toml by name. A URL that
  does not parse says so under its row.
- A server that wants a password (`db.ErrPassword`) asks for it once a run
  (`askPassword`, hidden input, the server named; with another box open it
  only says so, and asks when the source is read again, `r` or unfolded);
  it stays in memory (`dbView.passwords`) under the source's name, kept if
  the source still names that server when it is typed, and the `.env`'s
  dropped when the `.env` names another; it goes to the client in its
  environment, never on its command line. A table tab is its server's:
  reopened after its connection names another, the table opens in a tab of
  its own. A `[databases]` entry named `.env` is skipped: the worktree's
  takes that name.
- `n` or the header's `+` opens the connection form (`connform.go`), `e` and
  `D` edit or remove a `[databases]` entry; a modal (`modal.form`) in the
  editor area. Type cycles PostgreSQL, MariaDB, MySQL, SQLite (a file:
  `sqlite:///path`); a password written into a URL by hand (`user:pw@`, or
  libpq's `password=`, shown in the masked box) stays there while the box
  holds it, the form adds none; the name defaults to database@host;
  Options is the URL's query (`sslmode=require`; for MariaDB only TLS and
  transport options pass, `mariadbOptions`, as a `.env` is not trusted);
  `ctrl+t` reads the tables to test it; `⏎` saves through `state.set`
  `databases`.
- `t` opens the source's client in the Terminal panel, for SQL the grid
  does not run ([04-preview.md](04-preview.md#tables)).
- Save: Forever keeps the password in the system keyring (`db.SavePassword`:
  secret-tool on Linux, security on macOS), under service `pando` and the
  connection's name; Until restart keeps it in memory. A client finds it by
  `db.DB.Keyring` when no password is given, one keyring call per name a run.
- A table opens in an editor tab, transient on a click, pinned on a double
  click or Open Table; opening one whose tab is open shows that tab, edits
  and all: [04-preview.md](04-preview.md#tables).

## Customizations

`custom.go`. VS Code's Customizations editor as a list: what one agent loads in
this worktree, read from its files and never written. It is a tab beside
Sessions in that view's header row (`headTabs`), not a column's view: `allCols`
leaves it out wherever config lists it, so it costs Sessions no activity bar
and no width, and moves with Sessions. `viewOn` stays the column's tab, for
chips, menus and drags; `bodyOn` is what the column draws, `headTab` choosing.
The tabs sit under the frame title Agents, a rule row below them, and keep
their cells whichever shows; the one under the mouse lights, its label and
the rule under it (`hotTab`), as a strip's tab does. The header's hover
actions that do not fit beside them stay on their keys.
A section names its scope when it has entries in one, scope rows appear
when it has both, and details line up in a column set by every entry, folded or
not (`nameW`, at most 40%). `⏎` opens the file, on
the entry's line where it shares a JSON file with others. `ensure` scans off
the UI's goroutine (`scanCustom`) when the target changes and every `custTTL`
while on screen. A scan reads files only: nothing starts the agent or its MCP
servers, so connectors and live status are out of reach, and a skill a plugin
or the agent bundles is not listed.

| Agent    | Skills, User and Workspace                                   | Instructions, User and Workspace                                                                  |
| -------- | ------------------------------------------------------------ | ------------------------------------------------------------------------------------------------- |
| `claude` | `$CONF/skills`, `.claude/skills`                             | `$CONF/CLAUDE.md`, `$CONF/rules/*.md`; `CLAUDE.md`, `.claude/CLAUDE.md`, `CLAUDE.local.md`, rules |
| `codex`  | `~/.agents/skills`, `$CONF/skills`; `.agents/skills`         | `$CONF/AGENTS.override.md`, `$CONF/AGENTS.md`; the same two in the worktree                       |
| `grok`   | its own, `.agents` and `.claude` skills, at both levels      | `$CONF/AGENTS.md`; `AGENTS.md`, `CLAUDE.md`                                                       |
| `agy`    | `~/.gemini/config/skills`; `.agents/skills`, `.agent/skills` | `~/.gemini/GEMINI.md`; `GEMINI.md`, `AGENTS.md`, `.agents/rules/*.md`                             |

`$CONF` is the agent's config directory: the session's own
`CLAUDE_CONFIG_DIR` or `CODEX_HOME` first, a profile's, then pando's
environment, then the default. Two skills of one name in a scope show the
first one's. Only `claude` has the other sections:

| Section | User                                                 | Workspace                                                                |
| ------- | ---------------------------------------------------- | ------------------------------------------------------------------------ |
| Agents  | `$CONF/agents/*.md`                                  | `.claude/agents/*.md`                                                    |
| Hooks   | `$CONF/settings.json`                                | `.claude/settings.json`, `.claude/settings.local.json`                   |
| MCP     | `.claude.json`'s `mcpServers`                        | its `projects[worktree].mcpServers`, `.mcp.json`                         |
| Plugins | `$CONF/plugins/installed_plugins.json`, `user` scope | the same file's entries of this worktree, `enabledPlugins` marking `off` |

The agent shown (`target`) is the session's, by `Program` then `Agent`; a shell
keeps the last one, and `a` or the top row picks another until the session
changes. A row never holds a secret: MCP entries decode to their transport
only, so `env` and `headers` stay unread, and a hook shows the file its command
names, or its first word, never a word with `=`. The file itself opens whole.
`printable` cleans what comes from a skill before it reaches a row. A file
that does not parse, or a skill linking to nothing, is a row with a warning,
as an empty section would say it holds nothing.

`jsonKeys` walks to an object by `json.Decoder` tokens for the keys' lines, and
`skillMeta` reads the two frontmatter fields without a YAML dependency; both
take bytes and have a fuzz target.

## Usage

`usage.go`. The agents' plan limits as the daemon measured them
(`proto.Usage`, [01-architecture.md](01-architecture.md#usage)): the view asks
for nothing itself. `Model.usage` is read once (`loadUsage`), then follows the
`usage` event; showing the tab and `r` call `usage.refresh`. It is the tab
after Sessions in its header, before Customizations (`headed`, `headTab`),
there while `hasUsage`: `usage` on and an account known.

```text
 ✳ Claude Code                    auto    usAgent: its mark, profile_switch when "auto"
   default · Max 5x             active    usAccount: State.Active's, else "use"
     5h  ━━━━━━━━─────────  35%   3h 4m   usLimit: meter, share used, time to reset
     7d  ━━━━━┃━━━━━━━━━──  92%  9h 44m     ┃ a model's own allowance on the window
                                          usGap: before each account and agent but the first
   work · Team 5x                  use
     paused: its sign-in lapsed, …        usNote: Account.Note, its limits dim

 ◎ Codex · Free                           an only account rides its agent's row
   30d ───────────────────   0% 16d 11h
```

- An account's rows sit under its name, two cells in. `usGap` and the heading
  of an agent with accounts to pick are `inert`: `↑↓`, a page and a click
  pass over them, and the cursor starts on the first account.
- A model's limit (`Limit.Model`) marks the row of the window it shares
  (`ticks`); with no such row it gets one of its own, named. A mark on the
  first cell is left out: it would read as the fill.
- `limitMeter` draws in half cells (`╾`); `levelStyle` colors from 75% and
  90%, the status bar's chip too (`usageChip`).
- `⏎`, or a click on `use`, is `useProfile`: `state.set`'s `active_profile`,
  set here at once; `use` under the mouse lights and its tip says what a
  click does (`overUse`). An agent's only account has nothing to pick (`solo`).
- The chip names the active account of the agent in view, else the first
  known. With an offer (`Usage.Offer`) it adds `use NAME`, and a click takes
  it; `onUsage` flashes a new offer once, `movedProfile` a profile the daemon
  made active (`profile_switch = "auto"`).

## Commands

A view's `items()` feeds its context menu. The palette (`ctrl+shift+p`, `F1`)
lists `commands()`: each view's `items()` plus `viewItems`, `settingsItems`,
`updateItems` and the editor's `keyItems`, labels prefixed "Category: ", the
last used first. Both come from `categories()`, which also builds ⚙'s Manage
menu (`manageMenu`, `F10`): Command Palette, Settings, Keyboard Shortcuts,
then one row per category (`→` too) opening its items as a menu in the
same place (`manageSub`): its first row `‹ Category`, `←` too (`modal.back`),
goes back, on the category it came from. A setting that is on is checked
(✓); Preferences change in place, the menu staying open (`modal.keep`,
rebuilt). No menu bar: it would cost a row on every screen, and `alt+`
mnemonics are the shell's.

`commandID` turns a label into its `[keys]` id: "Git: Checkout to…" →
`git.checkoutTo`. Renaming a label changes the id and silently breaks user
bindings ([06-config.md](06-config.md#keys), [09-keys.md](09-keys.md)):
`renamedCommands` maps the old id to the new one (`git.switchBranch`).

## Prompts

`alt+i` (the palette's _Prompts: Insert…_) reads
`ConfigDir()/prompts/*.md` in a `tea.Cmd` on every open (`readPrompts`):
no daemon, no watcher, the files are the state. Rows are `always` items the
picker's `change` rebuilds with `hits`, words in the name or the text, as
quick open's Sessions tab does, a name match before a text-only one;
`lastPrompt` (memory only) goes first, the rest A–Z by `unaccented` name.
The target (`promptTarget`) is the panel's shell when it has the keyboard
(`termFocused`: not Problems or Output over it), else the session the active
group shows. `⏎` sends one `Paste` with `Wake`, the
file's trailing whitespace cut so it cannot submit itself; `alt+⏎` adds a
`"\r"` 50 ms later, as `pando session send --enter` does. `ctrl+e` and
`shift+del` are `modal.keys`, acting on the selection.

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
