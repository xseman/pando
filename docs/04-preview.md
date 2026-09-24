# Preview

The main area shows either the active session's screen or the preview
(`preview.go`). One struct serves four kinds:

| kind   | Content                                    | Opened by                          |
| ------ | ------------------------------------------ | ---------------------------------- |
| `file` | a file, chroma-highlighted                 | Explorer ⏎, quick open, search hit |
| `diff` | working tree or index diff of one file     | Source Control ⏎ / `o`             |
| `show` | a whole commit                             | a drawer line (hash)               |
| `rev`  | what one revision changed in the open file | `alt+←`                            |

## Editors

Open editors stack in a strip above the main area, drawn from two upwards.
A revision of the open file stays in its tab; a diff gets its own.

```
 app.ts ✕ │ README.md │ notes.md      ctrl+tab / ctrl+shift+tab cycle
     ▲ active, ✕ closes it            ctrl+w closes, ctrl+alt+- / ctrl+shift+- go back
```

Every jump (search hit, quick open, `go to line`, a revision step) is a history
entry that remembers the cursor and the scroll, so going back lands where you
left. The strip itself outlives the TUI: `saveEditors` sends the files, cursors
and scroll to the daemon (`editorsSpec`, per workspace in `state.json`) and
`restoreEditors` reopens them when the workspace opens, the active one on
screen unless a session of the worktree is showing. An untitled buffer is a tab
there too, with a name in place of a path. `ctrl+g` opens Go to Line, a picker
whose query starts with `:` (`:120`, `:120:5` or `:120,5`); the editor follows
the number as it is typed and `esc` puts the cursor back. `:` typed first in
quick open is the same picker, `@` goes to a symbol instead, and quick open
still takes `app.ts:120:5`.

## Text model

```
raw ──render()──▶ lines[]   styled, one per source line
                  plain[][] the same without styles  ← cursor, selection, copy
                  meta[]    diff rows: old/new line number + kind (c a d h p f)
lines ──rows(w)──▶ vrow{line, from, to}  screen rows (wrapping when `wrap` is on)
```

Word wrap is the `word_wrap` setting, off by default: Settings, `alt+z`, `w`
in a read-only view and the header's toggle (lit while on) all flip it through
the daemon, as `s` flips `diff_view`, and `syncWrap` puts it on the open
editor when the settings change. Off, a
row is cut at `left` and a horizontal scrollbar under the text pans it
(`docs/02-ui-layout.md`); the cursor still pulls `left` along.

While nothing is selected, `hlWord` takes the identifier under the cursor and
`renderRow` paints every place it stands on a visible line (`wordSpans`, whole
words only, two characters or more) — VS Code's occurrence highlight without a
language server. It lights up only when the cursor *moves* onto a name the file
already uses: an edit sets `typed` and takes the highlight away until the
cursor moves on its own (VS Code cancels its word highlighter on every change),
and `standsElsewhere` drops a name that stands nowhere else, so a word is never
painted as it is being typed. That scan stops at the first other occurrence, so
only a name used once pays for the whole file. `ctrl+↑`/`ctrl+↓` scroll the
view by a line and leave the cursor alone, as the wheel does.

The cursor is a `pos{line, col}` into `plain`; `shift`+arrows or a mouse drag
set an anchor, so selection, `y` copy and "Ln x, Col y" all read the same
model. Styles never enter the text model, so a selection copies code, not ANSI.
`alt+shift+→`/`←` (or `ctrl+shift`) grow and shrink the selection through the
chain VS Code's smart select uses — word, trimmed line, line, inside each
bracket pair and with it, the file — computed once per chain by a bracket pass
over `plain` (no lexer, so brackets in strings count) and dropped as soon as
the cursor moves on its own.

## Diff rendering

```
 2    - console.log("scm up");     old · new · mark · code
    3 + console.log("scm oh yeah");
```

Rows are tinted (`diffAddBg`/`diffDelBg`), the changed words darker
(word ranges from the common prefix/suffix of a paired del/add run), and the
code is highlighted per side: two chroma token streams approximate the old and
the new file so a string opened on a context line colors both.

`s` switches inline ↔ side by side; the split pairs each deletion run with the
additions after it and needs a 90-cell main area, else it falls back to inline.

Side by side selects the same lines inline does: the cursor is a whole row
(there are two columns of text, so no text caret), `⇧↑↓` and a mouse drag
extend it, and `m` stages, unstages or reverts exactly the selected changes.
The cursor is a line of the diff, so it walks a deletion run before the
additions paired with it — the order the changes are staged in.

## Markdown

`.md` opens as source. `p` renders it in place, `s` puts source and rendering
side by side. goldmark parses (CommonMark + GFM), the renderer is pando's own:
headings, lists, quotes, tables, task lists and fenced code through chroma,
wrapped to the panel width. Images become `[image: alt]`, HTML stays source.

## Find and replace

`ctrl+f` finds in the open file with VS Code's find widget, floating over the
editor's top right corner: the query, match case / whole word / regular
expression (`alt+c` `alt+w` `alt+r`, or a click), `n of m`, and `↑` `↓` `✕`.
Every match is tinted, the current one stronger, and selected in turn (`⏎`/`F3`
next, `⇧F3` back, `esc` closes the widget but keeps the matches). An edit or a
reload recollects them.

`ctrl+h`, or the `▸` before the query, opens the replace box under it: the
replacement, preserve case (`alt+p`, VS Code's `AB`), and the replace and
replace-all buttons. `tab` moves the caret between the two boxes. `⏎` in the
replacement (or `ctrl+shift+1`) replaces the current match and selects the
next; `ctrl+alt+⏎` replaces every match as one undo step. `$1` expands in regex
mode. `replacement` reads the buffer's own line so a tab inside a match
survives; `replaceAll` joins the rewritten lines into one `editRaw` splice.
Read-only previews (diffs, revisions, rendered Markdown) have no chevron and no
`ctrl+h`. While Go to Line's query holds a number, that line is highlighted
across the editor until the picker closes. Revisions have no key of their own;
the `←`/`→` header buttons step through them.

## Editing

A file preview is an editor: `buffer` (`buffer.go`) holds the file's raw lines —
tabs kept, unlike the view's expanded ones — plus an undo stack of splices and
the mtime the file was read at. Every change goes through `preview.edit`
(display coordinates) or `editRaw` (buffer coordinates, where a tab is one
rune); `docCol`/`displayCol` translate between them, the same pair the language
server uses. `alt+shift+↑`/`↓` (and `ctrl+d`) copy the line or the selected
lines as one splice, so one undo step.

```
key → editKey → buf.apply(a, z, text) ─▶ undo stack (typing coalesces)
                     │
                     ▼
              refresh(from, oldTo, newTo)  only the changed lines re-lex
```

Re-lexing the whole file costs 260 ms at 5 000 lines, so an edit re-renders
only the lines it touched (~2 ms whatever the file size); undo, save and reload
take the full pass. A dirty buffer wins over the file: the one-second reload
skips it, reopening the tab keeps it, and the language server is handed the
unsaved text through `Client.Overlay` for definitions and references. Code
actions and rename write to disk, so `saveForServer` saves the editor first and
only stops when that save cannot happen.

## Vim mode

`vim.go`, off until `vim_mode = true`. It is one key layer over the editor
that is already there, not a second editor: the modes decide who gets the key.

```
preview.key ─▶ compKey ─▶ findKey ─▶ vimKey ─▶ editKey
                                       │ normal, visual: letters are commands
                                       └ insert: only esc, so typing,
                                         suggestions and every ⌃ key are
                                         exactly what they are with it off
```

An editor opens in normal mode (`vimState`'s zero value, so a reopened tab
comes back there) and the header names the mode beside `Ln, Col`. Only an
editable file takes vim keys — a diff, a revision and a rendering keep their
own single letters.

| Mode   | Keys                                                                                  |
| ------ | ------------------------------------------------------------------------------------- |
| normal | `h j k l 0 $ w b e gg G`, a count before any of them; `i a I A o O` insert            |
|        | `x D C J p P u`, `⌃r` redo, `v` `V` visual, `/` find, `n` `N` matches, `:` go to line |
|        | operators `d c y` over a motion (`dw`, `d$`, `dgg`) or doubled (`dd cc yy`)           |
| visual | the motions grow the selection, `o` swaps its ends, `d x y c s` run over it           |
| insert | the editor itself; `esc` goes back to normal                                          |

The pieces come from what the editor already had: `moved()` walks `hjkl 0 $ G`,
`p.edit` splices and undoes, `anchor` is the visual selection, `/` opens the
find widget and `:` the Go to Line picker. What is new is `vimWord` (vim's
three rune classes behind `w b e`), the operator/count parser in `vimRun`, and
one register on the model (`vimReg`, shared by every editor, line-wise or not).

Normal mode stands on a rune rather than past the line end (`vimClamp`), so
`$`, `x` and leaving insert mode land where vim lands. `esc` with nothing
selected and no half-typed command still closes the editor, pando's own key.

ponytail: no ex commands (`:w` is `⌃s`, `:q` is `⌃w`), no macros, no marks,
no named registers, no `.`, no text objects (`ciw`). Add one when it is
actually missed.

## Untitled buffers and drafts

`draft.go`. `⌃n`, a double click on the empty main area, or *New Untitled File*
opens an editor with no path — `untitled()`, `kind` still `file`, so it types,
undoes and finds like any other. It has no file to read, so `load` builds the
view from the buffer instead of fetching; and no name to lex by, so it is plain
text until it is saved. `⌃s` opens *Save as* (`saveAsPrompt`, the prompt with
fish path completion `addProjectPrompt` uses, `completePath` rather than
`completeDir` so an existing file can be picked), asks before clobbering
anything, then re-points the tab and reads the file back so chroma lexes it as
what it now is.

```
⌃n ─▶ Untitled-1 ●  ──⌃s──▶ Save as ─▶ main.go   the tab follows the file
  │                                        │
  └── tick ──▶ draft.set ──▶ ~/.local/share/pando/drafts/…   draft.set "" ──┘
```

Nothing unsaved is lost to a restart. Every dirty editor — an untitled buffer
or an edit to a file that exists — is a draft the daemon keeps as a file
(`docs/06-config.md`), sent by `saveDrafts` on the tick, on the way out and
before a workspace switch, diffed against `m.savedDrafts` rather than the
daemon's copy so two TUIs do not rewrite each other. `loadDrafts`/`onDrafts`
put the text back when the workspace opens: into the tabs `restoreEditors`
rebuilt, or a tab of their own. A draft carries the mtime its editor read the
file at, so `errChangedOnDisk` still catches a foreign write across the restart,
and a tab restored but never opened still counts as unsaved — its buffer is
built at once, so the `●` shows and the next tick does not mistake it for clean.

A draft goes when its editor is saved, or when a tab is closed without saving.
Closing unsaved text always asks (`closeEditor`, `⌃w`, `esc`, a middle click);
for an untitled buffer *Save and close* opens *Save as* first and drops the tab
only once the file is written. *Close All Editors* and the strip's 20-tab LRU
step over what is unsaved rather than take its draft with the tab, the rule
*Close Other Editors* already followed.

## Merge conflicts

`conflict.go` is VS Code's merge-conflict extension over the editor's text,
independent of git: `parseConflicts` scans `plain` on every `rehit` (load,
edit, undo) with the extension's rules — `<<<<<<<` and `>>>>>>>` by prefix,
`=======` as the whole line, `|||||||` ancestors before the splitter, a
footer without a splitter drops its block, a nested header stops the scan.

```
<<<<<<< HEAD (Current Change)  Accept Current Change | Accept Incoming Change | Accept Both Changes
ours                             merge_current_bg (header: merge_current_head_bg)
||||||| base / old               merge_common_*
=======                          no tint
theirs                           merge_incoming_bg
>>>>>>> feat (Incoming Change)   merge_incoming_head_bg
```

The header row carries the CodeLens: `conflictSuffix` appends the note and
the three actions to the marker line (a TUI has no row between lines), and
`conflictClick` hits them before `posAt` does. `renderRow` lays the block's
tint under the text with `underBg`, as Go to Line does. An accept rebuilds
the text (`resolved`: markers and the other side go, the kept side stays
verbatim, a lone empty line chosen from one side goes too, Both is current
then incoming) and hands it to `setText`, so a whole Accept All is one undo
step and the view keeps its scroll. Next / Previous wrap around like VS
Code's. Compare Changes has no counterpart.

## Suggestions, formatting and language servers

`complete.go` is the suggest widget. It is not a modal: `preview.key` gives the
open list first pick of the keys (`compKey`: arrows, `⏎`/`tab`, `esc`), lets
everything else reach `editKey`, and `afterEdit` refilters or closes it.

```
typed letter ─▶ suggest ─▶ refilter: server items, else fileWords (nearest line first)
                   │
                   └─ 80 ms tick (seq) ─▶ Overlay + textDocument/completion ─▶ compMsg
                                                      stale seq dropped ◀─┘
```

Typing opens it in files with a language (`autoSuggest`), not in prose;
`ctrl+space` forces it. A `.` asks the server for members with an empty word.
An accepted item replaces the typed word, or from the column the server's
`textEdit` names, in one `editRaw`; snippet tab stops are stripped to their
placeholders. The box is drawn by `View` over the editor, under the cursor's
line or above it when it would run off, like the code action menu.

`ctrl+shift+i` formats: the editor's text goes into the `[format]` tool on stdin
and what comes back replaces the buffer in one undoable edit, the cursor kept
where it was (`format.go`). A tool that exits non-zero changes nothing and
flashes its first stderr line. `format_on_save` runs the same path from
`preview.save` before the write, so a file that stops parsing still saves.
Lookup is `commandFor`, shared with `[lsp]`: the file's extension, then its
language id, then pando's defaults (`gofmt` for Go).

`F12` jumps to a definition, `⇧F12` lists references, `ctrl+.` (`alt+⏎`, or `.`
in a read-only file) lists the code actions for the selection, `F2` renames the
symbol under the cursor (`textDocument/rename`; the box opens over the symbol
at `lightbulb(back)`, the same placement the actions menu uses, and starts with
the identifier around the cursor — no `prepareRename`). pando speaks the small
part of LSP it needs (`internal/lsp`): initialize, didOpen, definition,
references, codeAction with its resolve and executeCommand — no diagnostics, no
completion.

The menu opens under the cursor's line, its actions grouped by kind. A picked
action is applied by the client: the server's `WorkspaceEdit` (its own,
one from `codeAction/resolve`, or one the command sends back as
`workspace/applyEdit`) is spliced into the files on disk, last edit first, and
the preview and the git status reload. Create, rename and delete operations are
ignored, and there is no undo: git is the undo.

`ctrl+shift+o` is Go to Symbol: `textDocument/documentSymbol` in a picker whose
query starts with `@` (typing `@` in quick open lands there too). The tree a
server answers is flattened in document order, a child naming its parent on the
right, and the symbol around the cursor is selected. `@:` groups the rows under
VS Code's kind headings (`methods (3)`), groups by name. Moving through the
list moves the editor to the symbol's name, esc puts it back, and deleting the
`@` goes back to Go to File. A Markdown file needs no server: its ATX headings
are the symbols, fenced code skipped.

```
ctrl+shift+o ─▶ Symbols(path) ─▶ picker "@"  ─▶ ↑/↓ showSymbol   esc ─▶ cursor back
                   (overlay)          │ ":"
                                      ▼
                         fields (1) · functions (2) · methods (3) …
```

```
F12 ─▶ lspPool.get(workspace, language, binary) ─▶ gopls / typescript-language-server
   │        nearest node_modules/.bin first, then PATH (TypeScript 7: its own tsc --lsp);
   │        started on the first jump, stdio JSON-RPC
   ▼
 one location ─▶ open it          many ─▶ peek under the editor
```

```
 withRouter.tsx  src/util — References (27)                              ✕
 16 export function withRouter…  │ ▾ app.tsx  src                     2
 17     function Component…      │ ▾ withRouter.tsx  src/util         1
 18         let location = …     │     function withRouter<Params…   16
```

↑↓ walks the locations and the source follows, ←→ folds a file, ⏎ opens and
closes, `o` opens and keeps the list, `esc` closes. A file's columns and the
preview's differ (a tab shows as four spaces), so pando translates both ways.

## Staging selected lines

Right click or `m` in a diff: *Stage / Unstage / Revert Selected Ranges*, the
VS Code behaviour without patching hunks.

```
selection ─▶ dels{old line numbers}, adds{new line numbers}
git diff -U1000000000 ─▶ whole file as one hunk
  stage : start from the old side, apply the selected changes → git hash-object → update-index
  revert: start from the new side, undo them              → write the file
  unstage: same, against the index diff                   → update-index
```

Rebuilding the file from a fresh full-context diff means there are no hunk
offsets to get wrong; a file that changed underneath simply fails the diff.

## Revisions

`alt+←` / `alt+→` walk the open file's history like GitLens; `H` picks one.

```
file ──alt+←──▶ [0] uncommitted changes ──▶ [1] abc1234 · 2 days ago · fix …
     ◀──alt+→──                          ◀──
```

The list comes from `git log --follow` (plus an entry for uncommitted changes
when there are any); each step shows that revision's diff, so the split view,
selection and copy work unchanged. A repository with nothing committed yet
shows the uncommitted entry alone, diffed against an empty file.
