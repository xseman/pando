# Preview and editor

How the main area shows files, diffs, commits and GitHub pages, and how a file
becomes an editor. Keys: [09-keys.md](09-keys.md); settings and editor/draft
storage: [06-config.md](06-config.md).

- [Kinds and loading](#kinds-and-loading)
- [Text model](#text-model)
- [Editors and editing](#editors-and-editing)
- [Diffs and revisions](#diffs-and-revisions)
- [Markdown](#markdown)
- [Vim mode](#vim-mode)
- [Untitled buffers and drafts](#untitled-buffers-and-drafts)
- [Merge conflicts](#merge-conflicts)
- [Suggestions and formatting](#suggestions-and-formatting)
- [Language servers](#language-servers)
- [Tables](#tables)

## Kinds and loading

One `preview` struct (`preview.go`) serves every kind. Only `pvFile` is
editable, and `editable` also refuses a truncated file and a rendering.

| kind     | Content                                        | Opened by                          |
| -------- | ---------------------------------------------- | ---------------------------------- |
| `file`   | a file, chroma-highlighted; or an untitled one | Explorer ⏎, quick open, search hit |
| `diff`   | working tree or index diff of one file         | Source Control ⏎ / `o`             |
| `show`   | a whole commit, `git show`                     | a drawer line (hash)               |
| `rev`    | what one revision changed in the open file     | header `←` `→`, `H`                |
| `cmp`    | one file of a range, `git diff RANGE -- file`  | a Compare drawer file              |
| `gh`     | a pull request or issue, buttons in the header | GitHub ⏎                           |
| `ghdiff` | a pull request's changes, `gh pr diff`         | GitHub `d`                         |
| `table`  | a table or a view of a database, as a grid     | Databases ⏎                        |

`load` runs `fetch` and `render` in a `tea.Cmd` (chroma for files,
`parseDiff`/`renderDiff` for the rest); `onLoad` drops an answer whose `id`
no longer matches. A file over 1 MiB or 5000 lines loads truncated and
read-only. Every 2 s tick, `reloadIfLive` reloads a clean `file` or `diff`;
unsaved text wins over the disk.

## Text model

```text
buffer.lines   raw runes, tabs kept       ◀── edits, save, language server
     │  docCol / displayCol  (rawPos / dispPos)
     ▼
plain[][]      tabs as tab_size spaces    ◀── cursor, selection, copy
lines[]        the same, styled           ◀── drawing only
meta[]         diff rows: old/new number, kind (c a d h p f)
     │  rows(w)
     ▼
vrow{line, from, to}   screen rows; wrapped when wrap is on
```

- `pos{line, col}` is a display position into `plain`; `anchor` is the other
  end of a selection. Styles never enter `plain`, so copy gives code.
- A tab is `tabW` spaces, not a run to the next stop; `syncTabs` redraws on a
  change. `insert_spaces` makes the tab key insert spaces.
- `render_whitespace`: `blankMarks` picks `·`/`→` per rune, `markBlanks`
  paints them over the styled line. Files only.
- The gutter's lane after the numbers marks lines against the file's staged
  text (`base`, read with each load), VS Code's quick diff (`gutter.go`):
  `lineChanges` diffs by Myers' algorithm the lines between a common prefix
  and suffix (`editScript`, at most `maxEdits`), in a command after each edit
  (`noteGutter`, keyed by buffer, edits and base); a change that only added is
  `┃` added, one that also removed `┇` modified (VS Code's hatched bar), a
  deletion `▾` on the line before it (`▴` on the first), in the scrollbar's
  change colors. A file with staged changes is diffed against its committed
  text (`head`) too, VS Code's secondary quick diff: those marks, where no
  unstaged one is, take its secondary shade (upper case in `chg`, darker on a
  dark theme, lighter on a light one: `lookDark`). The scrollbar marks them
  as VS Code's overview ruler does (`fileBarChanges`, `diffMarks`) under the
  problems, which keep their cells; a click goes to one (`changeMarkAt`).
- A click on a change's mark (`laneChange`, which `markHot` widens under the
  pointer) or `alt+F3` opens the quick diff (`quickdiff.go`, `p.qd`), a zone
  under the change as VS Code's: `rows` puts its rows after the line's own
  (`vrow{line: -1, from: k}`: header, diff, rule), so it scrolls with the
  text, and the editor keeps the keyboard. It renders the change with
  `quickCtx` lines of context as a one-hunk diff through `render`, rebuilt
  as the changes move (`followQuickDiff`). Stage writes the change into the
  index over the staged text it was found against (`git.StageChange`,
  refused when that moved); Revert is one `setText`, undoable.
- Indent guides (`indentGuides`, set as view draws): a line has as many as
  its indentation takes units, rounded up, a blank line VS Code's
  `getLinesIndentGuides` count from the lines around it; the unit is the step
  most lines indent by, else `tabW`. `activeGuide`, the Explorer's, lights
  the cursor's block; `paintGuides` draws `│` in `whitespace` and
  `indent_guide` on the cell `guideRoom` clears of render_whitespace's mark,
  a tab's arrow moving to its next cell. A wrapped line's later rows start
  with its text and have none. Files only.
- Every change goes through `preview.edit` (display columns) or `editRaw`
  (buffer columns) into `buf.apply`, which pushes an undo splice (typing
  coalesces). Undo or redo back to the text as read or saved (`saved`) is
  clean again, so the 2 s reload follows the disk.
- `refresh` re-lexes only the touched lines (a whole 5000-line file takes
  ~260 ms); undo, save and edits over 100 lines take `refreshAll`. A
  partial re-lex can miscolor the rest of a block comment until then. Both end
  in `rehit` (find hits, conflicts).

## Editors and editing

The strip above the main area lists open editors (`●` unsaved).
A `transient` editor (VS Code's preview editor, italic) is what Explorer,
Search and Source Control open: `setPreview` puts the next transient one in
its tab. `pin` keeps it: `editRaw`, or a double click on the tab or the row.
Quick open, F12 and menus open pinned; `editorsSpec` drops the flag.
`editor_limit` closes the tab with the lowest `used`, never the active or a
dirty one. Every jump is a history entry with cursor and scroll for Go Back.
`saveEditors` sends `editorsSpec` (`file` and `rev` tabs only) to the daemon;
`restoreEditors` reopens them. Go to Line takes `:120`, `:120:5`, `:120,5`.

`preview.key` offers a key to `compKey` (suggest), `findKey`, `vimKey`, then
`editKey`; the first to claim it wins.

| Command                   | How it works                                                   |
| ------------------------- | -------------------------------------------------------------- |
| Copy / Move Line Up, Down | one splice, one undo step                                      |
| Toggle Line Comment       | `commentOf` picks the marker by extension                      |
| Sort Lines Ascending      | `sortLines`: selected lines or the whole file; palette, menu   |
| Expand / Shrink Selection | `smartSel`: word, trimmed line, line, bracket pairs, file      |
| Save                      | `errChangedOnDisk` asks before overwriting a newer file        |
| Find (`ctrl+f`)           | VS Code's widget: case, word, regex; hits tinted (`rehit`)     |
| Replace (`ctrl+h`)        | `$1` in regex mode, preserve case; `replaceAll` is one undo    |

- Smart select brackets come from `plain`, no lexer; moving drops the chain.
- Occurrence highlight: `hlWord` paints the identifier under the cursor
  wherever it shows. An edit sets `typed` and hides it until the cursor
  moves; `standsElsewhere` skips a name used once.
- Read-only kinds have find but no replace.

## Diffs and revisions

```text
inline                                side by side (diff_view = "split")
 12    - foo := 1                      12 - foo := 1      │ 12 + foo := 2
    12 + foo := 2                      13   bar()         │ 13   bar()
 13 13   bar()
 old new mark code
```

- `renderDiff` tints rows (`diffAddBg`/`diffDelBg`) and changed words
  (`wordRanges`). `tokenLines` lexes an approximate old and new file, so a
  string opened on a context line colors both sides.
- `splitRows` pairs each deletion run with the additions after it; under
  `splitMinW` (90 cells) the split falls back to inline. Side by side the
  cursor is on a half (`oldHalf`, the new one first; a click picks it):
  keys move by rows on it (`splitMove`), a drag stays on it, a selection is
  its rows (`splitRange`): both halves stage, its own copies (`halfText`).
- A file's diff (`fileDiff`: kinds `diff`, `cmp`, `rev`, not untracked) and a
  commit's are fetched with full context (`--unified=1000000000`, `unfolds`)
  and folded in the view: `all` keeps every row, `shownRows` shows each change
  with `ctxRows` (3) around it and each longer unchanged run as a gap row,
  `N hidden lines`, carrying its first hidden row's numbers. ⏎ on a gap or a
  click unfolds it in place (`unfold`: the tab keeps the old lines it hid,
  `gapLines`, so a reload, even with a new change inside them, leaves them
  shown); Go to Line into a gap unfolds it, and find looks in the rows a
  gap hides (`findHit.hidden`), unfolding it for the match selected. With
  `diff_full` a file's diff shows whole; `z`, the header's unfold/fold
  button and Toggle Collapse Unchanged Regions flip it locally (`refull`)
  and fold every gap again.
- A diff over `maxPreviewLines` or a merge's combined diff (`foldable`) is
  `fixed`: fetched with git's three lines as before, its gaps git's own. A
  pull request's diff always is. `y` with no selection copies the patch as
  fetched, full context included.
- `changes` are the runs of `a` and `d` rows, a deletion and its additions
  one change. `]` `[` (`gotoChange`) and a click on a scrollbar mark
  (`markAt`; again on the same cell, its next change) put the cursor on a
  change's first line, mid view, around the ends as VS Code. `}` `{`
  (`stepFile`) go to the next file of a commit's or pull request's diff, its
  name on the top row and the cursor on its first change, or open the next
  file of Source Control (of the Compare drawer for `cmp`) and select it.
- `s` keeps the cursor's line on its screen row (`scrollTo`). `z` and the
  2 s reload of a live diff note the cursor's and the selection's line
  numbers and screen row (`keepPlace`); `onLoad` finds them again
  (`findPlace`: the same row, else the next one shown), unless the cursor
  moved meanwhile. The status bar's Ln and Go to Line count the file's lines
  (`lineNo`).
- The scrollbar marks changes (`barChanges`, `vbar.marked`): a cell is two
  halves (`▀` `▄`), row r of the wrapped or side-by-side rows lands on half
  r·2h/n, so none is too short. A change is a deletion (`diff_del_mark`), an
  addition (`diff_add_mark`) or both, a line changed (`diff_mod_mark`, blue
  as VS Code's gutter); in a shared half the rarer kind wins, deletion over
  addition over both. On the slider a mark takes the right half (`▐`), so
  the slider shows through, its color moved 3:1 apart from the slider's
  (`legible`). A row in view is marked on the slider, one out of view off
  it (`halfOf`): `thumb` scales apart.
- A file's diff opens on its first change (`onLoad`, mid view when the
  whole file hides it); a reload, a tab coming back and a commit keep theirs.
- The cursor is a whole diff line in both views. `m` (or `shift+F10`) opens the
  context menu; a diff of a tracked file (not `U` or `!`) adds Stage Selected
  Ranges and Revert Selected Ranges… (asks first, `confirmRevert`), a staged
  diff Unstage Selected Ranges. `changedLines` picks the lines (a selection
  ending at column 0 drops that line; none means the change under the
  cursor, its deletion with its additions, as VS Code's), then
  `git.ApplyLines` ([05-git.md](05-git.md)).
- `←`/`→` in the header walk the file's history, `H` picks a revision. Each
  step is a `rev` diff, entry 0 the uncommitted change.

## Markdown

`md` is 0 source, 1 rendered (`ctrl+shift+v`), 2 side by side (`alt+v`); `gh`
always renders. goldmark parses CommonMark + GFM, `markdown.go` renders in
glow's layout: `mdMargin` air (dropped under 26 cells), wrapping table cells,
chroma code blocks on `md_code_bg`, images as `[image: alt]`, HTML as source.
`markdown_width` caps the width (`mdWidth`); side by side uses its whole half
so lines stay level. The rendering is cached per width.

## Vim mode

`vim.go`, on with `vim_mode`: a key layer over the same editor, for editable
files only. Its keys: [09-keys.md](09-keys.md#vim-mode).

`vimRun` parses count and operator, `vimReg` is one register for all editors,
`vimClamp` keeps normal mode on a rune. `esc` with nothing pending closes the
editor. No ex commands, macros, marks, named registers, `.` or text objects.

## Untitled buffers and drafts

`draft.go`. `ctrl+n` opens a `file` with no path (`untitled`), built from its
buffer instead of fetched. `ctrl+s` opens Save As (`saveAsPrompt`), then re-points
the tab and reloads it so chroma lexes it.

Every dirty editor is a draft: `saveDrafts` sends it on the tick, on quit and
before a workspace switch, diffing against `savedDrafts` so two TUIs do not
rewrite each other. `onDrafts` puts drafts back on open. A draft keeps its
mtime, so `errChangedOnDisk` still fires after a restart. Closing unsaved
text asks; Close All Editors and `editor_limit` skip dirty tabs.

## Merge conflicts

`conflict.go` decorates `<<<<<<<` / `|||||||` / `=======` / `>>>>>>>` blocks
from the text alone, using VS Code's rules (`parseConflicts`, on every
`rehit`). `conflictSuffix` puts Accept Current / Incoming / Both on the header
line; an accept builds `resolved` and applies it with `setText`, one undo step.

## Suggestions and formatting

`complete.go` is not a modal: `compKey` takes arrows, `⏎`/`tab`, `esc`, and
`afterEdit` refilters. It lists `fileWords` at once and, after an 80 ms tick,
the server's completions (stale `seq` dropped). `autoSuggest` opens it while
typing in files with a language; `ctrl+space` anywhere.

`format.go` pipes the text through the `[format]` tool (stdin to stdout, 10 s
timeout, on the update loop); `setText` swaps only differing lines. A failing
tool changes nothing. `format_on_save` runs it in `save`. `commandFor` picks
by extension, language id, then defaults (`gofmt`).

## Language servers

`internal/lsp` covers definition, references, rename, documentSymbol,
completion, codeAction (with resolve and executeCommand) and diagnostics,
pushed or pulled (`diagnosticProvider`: TypeScript 7).

```text
F12, shift+F12 ─▶ lspGo ─▶ tea.Cmd ─▶ lspPool.get(root, path, lang, argv)
                                    one server per language + binary,
                                    nearest node_modules/.bin, then PATH;
                                    TypeScript 7: tsc --lsp
                                       │ Sync(editor text): didOpen, didChange
                                       ▼
                         Definition / References ─▶ lspMsg ─▶ onLSP
        one definition ─▶ openLocation        else ─▶ openPeek
```

- Servers start on first use and die with the TUI. Columns convert through
  `UTF16Col`/`RuneCol` and `docCol`/`displayCol`.
- The client keeps each document's version and text (`Sync`, `SyncFile` by
  mtime, `Forget`); every request syncs the editor's text first. Diagnostics
  arrive on the reader into the pool's `sink`, `diagBox`, whose latest list
  per file `waitDiags` hands to Update (`diag.go`).
- With `lsp_diagnostics`: `noteEdits` arms a 300 ms sync of the shown
  editor whenever its buffer's `edits` count moves (load, edit, undo, format,
  reload); each git refresh `syncDiags` syncs the editors' files and Source
  Control's changed files under the workspace (`diagFiles`, 50) and forgets
  the rest. Those syncs use `lspPool.background`, which never restarts a
  server that died or would not start (a request does) and fails for a
  workspace switched meanwhile (`reset` sets the pool's root). A server
  that would not start for a file is `diagFailed`, which the status bar
  shows while that file does. Turned off (`noteDiags`), the servers go
  (`reset("")`, so a sync still running starts none) and so does the
  Problems list; turned on again (`reset(ws)`) they start fresh and report
  every file again, a broken one nobody opened too. A pulled list for a
  version since changed is dropped.
- `probsOf` turns a file's errors and warnings into cells per row, by line
  number in a diff (its file read from disk, `disk`); one past the last line
  (a truncated file's) moves to the last line's end (`inText`), a point at a
  line's end onto its last character; `paintProbs` squiggles
  them (SGR 4:3 and 58, re-emitted after each SGR inside), `probAtCursor`
  feeds the footer, `probTip` the pointer's tip, `barProbs` the scrollbar
  (`probMarks`), `decorateDiags` Explorer and Source Control; `gotoProblem`
  is F8, `showProblems` the Problems peek built in a command, built again
  (`problems(true)`, `peek.follow`) on each change while it is open.
- Code actions carry the server's own diagnostics under the range (`near`,
  from `seen`), which its quick fixes need. They and rename write to disk:
  `saveForServer` saves first, the `WorkspaceEdit` is applied file by file,
  then preview and git reload. Git is the undo.
- Go to Symbol flattens the symbol tree; Markdown uses `mdSymbols` (ATX
  headings) without a server.
- The references peek (`peek.go`) sits under the editor: the selected hit's
  source left, hits grouped by file right.

## Tables

A `table` tab (`table.go`) is a grid over one table: `p.tbl`, which the tab's
copies share, holds the page, the two boxes and the edits. Every read and
write is one client process (`internal/db`) fed a script: `sqlite3 -safe`,
`psql -X -w`, `mariadb --batch --raw`. A row comes back as a JSON array of
text or null, so NULL, `''` and `'NULL'` stay apart; a Postgres script sets
`standard_conforming_strings` on first, as `quote` reads a `\`. A read is
read-only (`-readonly`; the script's `set session characteristics as
transaction read only`, which a URL's `options=` cannot undo; `set session
transaction read only`); a password goes in the environment (`PGPASSWORD`,
`MYSQL_PWD`), one in a URL's query (`password=`) too.

```text
 ✕ ▦ orders  app.db  1–200 of 450 · 3 pending           ←  →  ⟳  ✓
 ▏ WHERE total > 90           ▕  ▏ ORDER BY "id" desc         ▕
     # │ id ↓ │ total │ note           │        the header stays as the rows scroll
 M   1 │  448 │ 78.03 │ one↵two        │        M: a cell edited, in the modified color
 D   2 │  441 │ 26.83 │ note 441       │        D: to be deleted
 A     │ DEFAULT │ …                            A: added, unset cells take DEFAULT
```

| Part            | Behaviour                                                                                                                                                                                                                                                                |
| --------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| page            | `tablePage` (200) rows by `limit` and `offset`, one more row tells whether any follow ; `[` `]`, the header's `←` `→`; no ORDER BY sorts by the key                                                                                                                      |
| count           | `count(*)`, but a server table over `bigTable` (100 000) rows by its estimate (`reltuples`, `table_rows`) shown unfiltered: `~N`, which Count Rows counts                                                                                                                |
| stop            | a read runs under a context: `esc` interrupts its client, which stops the query on the server, the page on screen stays; a new page stops the one running, Count Rows is refused while one runs                                                                          |
| status bar      | the cursor's column: its name, type, primary key or generated                                                                                                                                                                                                            |
| WHERE, ORDER BY | your SQL put into the query as typed, as the console runs it: read-only stops a slip, not a `;` that ends the query on purpose; an error keeps the rows on screen                                                                                                        |
| sort            | `s` or a header click writes `"col"` (MariaDB: `` `col` ``), `"col" desc`, then nothing into ORDER BY                                                                                                                                                                    |
| cells           | as wide as the page needs, at most `cellMax` (40); `↵` for a line break without the indentation after it, a tab as a space, no `\r`, other control characters `�`; NULL and DEFAULT in italics                                                                           |
| sideways        | the gutter stays; `←` `→` move by column, shift+wheel and the bar under the rows by cells, as the editor's long lines                                                                                                                                                    |
| edit            | ⏎: a prompt over the cell that keeps spaces (`modal.raw`); a control character (a tab, a line break) or over 200 bytes opens an untitled editor: unchanged the value goes back as it was, changed it keeps its last line break, and `\r\n` when all its line breaks were |
| rows            | `o` add, `ctrl+d` duplicate without the primary key, `d` delete, `u` revert the cell, else the row's deletion or the added row                                                                                                                                           |
| select          | `shift+↑` `↓` (`pgup` `pgdn`), shift+click: the rows from the cursor; `d`, `u`, Set NULL and `y` (TSV under the column names) take them; `esc` or a move without shift drops them                                                                                        |
| submit          | `ctrl+s`: every edit in one transaction (`db.Table.Apply`), all or nothing; one submit at a time                                                                                                                                                                         |
| read only       | a view, a virtual table, a generated column, a blob, a MariaDB BIT or geometry (shown as bytes)                                                                                                                                                                          |

- A row is found again by its key columns (`db.Table.Keys`), read as a JSON
  array of text: SQLite's rowid, but a WITHOUT ROWID table's primary key;
  the primary key elsewhere (a MariaDB binary one as hex, `keyHex`: as text
  its bytes that are not UTF-8 turn into `?`), else Postgres's tableoid,
  ctid and xmin (a partition's ctid repeats another's, a vacuumed slot
  takes a new row, xmin tells them apart); a MariaDB table without one is
  read only.
- Each statement must touch one row: `changes()`, `row_count()` (a MariaDB
  update counts the rows it finds first, `select count(*) into @n … for
  update`: `row_count()` leaves out a row set to what it held) or a
  `returning` CTE's count goes into a temporary table whose
  `CHECK (n = 1)` fails otherwise. An update also matches the old
  values of the columns it sets, so a row changed or gone meanwhile fails the
  submit as a conflict, named by its row (`select '#i'` markers); `u` drops
  the edit, `r` then shows the row as it is now. A deletion finds its row by key alone. MariaDB's MyISAM
  and Aria tables have no transactions: a failed submit keeps what went
  before it.
- While a submit is on its way the edits stay as they are: once through it
  drops the ones it sent, and a later change to one of them would go with it.
- Open Console (the menu, `t` in the view) runs the client in a shell of the
  Terminal panel. Its password goes through a file in the runtime directory
  that `sh` reads and removes before it execs the client: a session's
  command and environment are kept in state.json. Started again by a
  restarted daemon, the client asks for it (mariadb told to by `-p`).
  Windows' client always asks.
- MariaDB sessions run in UTC (`set time_zone`): a TIMESTAMP shows, and is
  typed, in UTC, where no hour repeats; the client talks utf8mb4 whatever
  the locale, in `--binary-mode`, which keeps a value's `\r\n` and NUL and
  runs no client command; a FLOAT key reads at double precision.
- sqlite3 reads its script by lines: it drops the `\r` of a `\r\n`, in a
  string too, and ends a line at a NUL. A text holding either goes as
  `cast(x'…' as text)`, and a text key is read so (`quote()` cuts at a NUL).
  A REAL reads to the 17 digits it takes to read back as itself when 15 do
  not. A Postgres script sets `client_encoding` to UTF8 and
  `extra_float_digits` to 3, whatever `PGCLIENTENCODING`, the database or
  the URL say: below 1, 0.1 + 0.2 prints as 0.3.
- A long value's editor bases its edit on the value as read, or on the one
  it showed when a submit since wrote it: if the row holds another when it
  is put back (another hand's, or a row that took the key), the submit fails
  as a conflict.
- After a submit the page is from before it until read again (`stale`): a
  row it deleted may have given its key to one it added, so it takes no
  edit until then.
- A reload that finds the columns changed under edits made while it read
  keeps the columns the edits name, and says so.
- A long value's editor (`preview.cell`) keeps no draft and is not restored:
  `ctrl+s` puts the text back as an edit and shows the table again.
- Edits follow the row's key through pages, filters and sorts. A table
  edited in the preview tab keeps it, as an edited file does. Closing the
  tab asks to submit or discard them; closing pando (`q`, `ctrl+c`) says
  they are lost; switching workspaces keeps the tab, and a value's editor
  typed in, until the workspace shows again (`waiting`). Table tabs are not
  restored after a restart.
