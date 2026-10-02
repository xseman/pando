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

## Kinds and loading

One `preview` struct (`preview.go`) serves every kind. Only `pvFile` is
editable, and `editable` also refuses a truncated file and a rendering.

| kind     | Content                                        | Opened by                          |
| -------- | ---------------------------------------------- | ---------------------------------- |
| `file`   | a file, chroma-highlighted; or an untitled one | Explorer ⏎, quick open, search hit |
| `diff`   | working tree or index diff of one file         | Source Control ⏎ / `o`             |
| `show`   | a whole commit, `git show`                     | a drawer line (hash)               |
| `rev`    | what one revision changed in the open file     | header `←` `→`, `H`                |
| `gh`     | a pull request or issue, rendered Markdown     | GitHub ⏎                           |
| `ghdiff` | a pull request's changes, `gh pr diff`         | GitHub `d`                         |

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
- Every change goes through `preview.edit` (display columns) or `editRaw`
  (buffer columns) into `buf.apply`, which pushes an undo splice (typing
  coalesces).
- `refresh` re-lexes only the touched lines (a whole 5000-line file takes
  ~260 ms); undo, save and edits over 100 lines take `refreshAll`. A
  partial re-lex can miscolor the rest of a block comment until then. Both end
  in `rehit` (find hits, conflicts).

## Editors and editing

The strip above the main area lists open editors (`●` unsaved).
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
  `splitMinW` (90 cells) the split falls back to inline.
- The cursor is a whole diff line in both views. `m` (or `shift+F10`) opens the
  context menu; a diff of a tracked file (not `U` or `!`) adds Stage Selected
  Ranges and Revert Selected Ranges… (asks first, `confirmRevert`), a staged
  diff Unstage Selected Ranges. `changedLines` picks the lines (a selection
  ending at column 0 drops that line; none means the cursor line), then
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
completion and codeAction (with resolve and executeCommand); no diagnostics.

```text
F12, shift+F12 ─▶ lspGo ─▶ tea.Cmd ─▶ lspPool.get(root, path, lang, argv)
                                    one server per language + binary,
                                    nearest node_modules/.bin, then PATH;
                                    TypeScript 7: tsc --lsp
                                       │ Overlay(unsaved text), didOpen
                                       ▼
                         Definition / References ─▶ lspMsg ─▶ onLSP
        one definition ─▶ openLocation        else ─▶ openPeek
```

- Servers start on first use and die with the TUI. Columns convert through
  `UTF16Col`/`RuneCol` and `docCol`/`displayCol`.
- Code actions and rename write to disk: `saveForServer` saves first, the
  `WorkspaceEdit` is applied file by file, then preview and git reload. Git is
  the undo.
- Go to Symbol flattens the symbol tree; Markdown uses `mdSymbols` (ATX
  headings) without a server.
- The references peek (`peek.go`) sits under the editor: the selected hit's
  source left, hits grouped by file right.
