# Keys

Every key, by where the focus is. Each command is in the palette
(`ctrl+shift+p`) under an id that `[keys]` rebinds
([06-config.md](06-config.md#keys)); `?` shows a summary in the TUI.

[Focus](#focus) · [Everywhere](#everywhere) · [Lists](#sidebar-lists) ·
[Views](#views) · [Terminals](#terminals) · [Files](#in-a-file) ·
[Vim](#vim-mode) · [Pickers](#pickers-menus-questions) ·
[Mouse](#mouse-and-clipboard)

## Focus

A key means what the focused part makes of it (`keyContext`, `keybindings`):

- a session or the Terminal panel: every key but the chords marked T below
- an editable file: printable keys type; ctrl and F-keys stay commands
- a text box (commit message, filter, find): its own keys; `[keys]` waits
- a sidebar list, a diff, a rendering: single letters and every chord

Without the kitty keyboard protocol ``ctrl+` `` and `ctrl+space` are one byte:
an editable file takes it as suggestions (the panel is on `ctrl+j` there), a
terminal as the panel (its `ctrl+j` is the app's line feed). Inside tmux the
protocol needs `set -s extended-keys on` and
`set -as terminal-features ",*:extkeys"`.

## Everywhere

T: also in a terminal; the rest are the shell's there.

| Key                                                    | Action                                                  |
| ------------------------------------------------------ | ------------------------------------------------------- |
| `ctrl+]` T                                             | cycle focus: left sidebar, main, panel, right sidebar   |
| `ctrl+shift+p` T, `F1`                                 | command palette                                         |
| `ctrl+shift+e` `ctrl+shift+g` `ctrl+shift+f` T         | Explorer, Source Control, Search                        |
| `ctrl+shift+h` T                                       | Search with replace                                     |
| ``ctrl+` `` T, `ctrl+j`, `5` (list, diff)              | toggle the Terminal panel                               |
| ``ctrl+shift+` `` T                                    | new shell in the panel                                  |
| `ctrl+shift+↑` `ctrl+shift+↓` T                        | maximize, restore the panel                             |
| `ctrl+b` T, `ctrl+,` T                                 | hide the sidebars, settings                             |
| `ctrl+0` `ctrl+1` T                                    | focus sidebar, editor                                   |
| `alt+t` T                                              | go to a session or worktree (`@idle`, `!claude` narrow) |
| `ctrl+pgup` `ctrl+pgdn` T, `ctrl+shift+tab` `ctrl+tab` | previous, next editor                                   |
| `alt+1`…`alt+9` T                                      | editor N                                                |
| `ctrl+shift+pgup` `ctrl+shift+pgdn` T                  | move the tab: editor, session tab, shell                |
| `ctrl+p`                                               | quick open; `:` line, `@` symbols, `ctrl+t` tree        |
| `ctrl+n`, `ctrl+s`                                     | new untitled file, save                                 |
| `ctrl+enter`                                           | commit                                                  |
| `ctrl+g`, `ctrl+shift+o`                               | go to line, symbol                                      |
| `ctrl+shift+v`, `alt+v` (in a file)                    | rendered Markdown, side by side                         |
| `ctrl+alt+-` `ctrl+-` `alt+,` `super+←`                | back through visited editors                            |
| `ctrl+shift+-` `alt+.` `super+→`                       | forward                                                 |

## Sidebar lists

- `↑` `↓` `j` `k`, `pgup` `pgdn`: move, a page
- `g` `home`, `G` `end`: first, last row
- `⏎` `space`, `→` `l`, `←` `h`: activate, unfold, fold or go to the parent
- `m`, right click: context menu
- `ctrl+f`: filter (`⏎` keeps it, `esc` clears)
- `1` `2` `3` `4` `6`: Explorer, Source Control, Spaces, Search, GitHub
- `tab`: focus main
- `[` `]`: previous, next tab of the session
- `b`, `<` `>`: hide this column, narrow or widen it
- `,`, `?`: settings, help
- `q`, `esc`: close pando, asking first (sessions keep running)
- `ctrl+c`: close pando at once (in Explorer: copy)

## Views

| View           | Keys                                                                                                                                                                                                                                                                                                        |
| -------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Explorer       | `⏎` open, `n` `N` new file, folder, `R` `F2` rename, `D` `del` delete, `d` duplicate, `ctrl+x` `ctrl+c` `ctrl+v` cut, copy, paste, `s` stage, `e` `$EDITOR`, `o` system app, `O` containing folder, `F` find in folder, `c` `y` `Y` copy name, path, relative path, `.` dotfiles, `C` collapse, `r` refresh |
| Source Control | `⏎` stage, unstage, `o` diff, `O` open, `d` discard, `a` `u` stage, unstage all, `U` stage untracked, `c` message, `C` commit, `A` suggest, `S` sync, publish, `B` branch or tag, `t` tree, `{` `}` repository, `r` refresh                                                                                 |
| Spaces         | `⏎` `l` switch, `alt+↑` `alt+↓` reorder, `n` session, `w` worktree, `a` add project, `R` rename, `x` `d` `del` kill, delete, close, `o` view options, `r` refresh                                                                                                                                           |
| Search         | `ctrl+f` `/` `i` query, `ctrl+h` replace, `ctrl+i` include, `⏎` `o` open, `r` `R` replace in file, all, `alt+c` `alt+w` `alt+r` `alt+p` case, word, regex, preserve case, `t` tree, `ctrl+r` rerun, `x` clear, `C` collapse                                                                                 |
| GitHub         | `⏎` open, `d` pull request diff, `w` worktree (issue: start one), `o` github.com, `y` copy link, `x` mark done, `ctrl+r` refresh, `C` collapse                                                                                                                                                              |

Commit message: `⏎` commits, `shift+⏎` `alt+⏎` new line, `esc` `tab` leave,
`ctrl+a` all, `home` line start, `ctrl+c` `ctrl+x` copy, cut, `↑` on the top
line an older commit message, `↓` on the bottom line a newer one. Search
boxes: `⏎` `↓` to the results, `tab` next box.

## Terminals

- `ctrl+f`: find in the scrollback
- `shift+F3`, `F3` (in the box also `⏎` `↑`, `shift+⏎` `↓`): previous (older), next match
- `alt+c` `alt+w` `alt+r`: find: case, word, regex
- `esc` (`ctrl+f` in the box): close find; a click on the terminal keeps the matches
- `pgup` `pgdn` (`shift` too): page the scrollback; on the alternate screen, the app's
- `alt+z`: Terminal panel: fit to the content width, scroll sideways

## In a file

Editable files and read-only views (diff, revision, rendering):

| Key                                  | Action                                                   |
| ------------------------------------ | -------------------------------------------------------- |
| arrows, `shift`+arrows               | move, select                                             |
| `ctrl+←` `ctrl+→`                    | a word back, on (`shift` selects)                        |
| `home` `end`, `ctrl+home` `ctrl+end` | line start, end; file start, end                         |
| `ctrl+↑` `ctrl+↓`                    | scroll the view, the cursor stays                        |
| `alt+shift+→` `alt+shift+←`          | expand, shrink the selection: word, line, brackets, file |
| `ctrl+a`, `ctrl+c`                   | select all, copy the selection (or the whole file)       |
| `ctrl+f`, `ctrl+h`                   | find, replace                                            |
| `F3` `shift+F3`                      | next, previous match with the find box closed            |
| `F12`, `shift+F12`, `F2`             | go to definition, references, rename symbol              |
| `ctrl+.` `alt+⏎`                     | quick fix                                                |
| `ctrl+shift+i`                       | format document                                          |
| `alt+z`                              | word wrap                                                |
| `shift+F10`, right click             | menu: Stage / Unstage / Revert Selected Ranges in a diff |
| `esc`, `ctrl+w`                      | clear the selection, else close; close                   |

Editable only:

- `⏎`, `tab`, `⌫`, `del`: type; `⏎` keeps the indent, `tab` follows `insert_spaces`
- `ctrl+z`, `ctrl+y` `ctrl+shift+z`: undo, redo
- `ctrl+x`, `ctrl+v`: cut (the line without a selection), paste
- `ctrl+shift+k`: delete the line or selection
- `alt+↑` `alt+↓`: move the line
- `alt+shift+↑`, `alt+shift+↓` `ctrl+d`: copy the line up, down
- `ctrl+/`: toggle a comment
- `ctrl+space`: suggestions (`⏎` `tab` accept, `esc` close)

Read-only views add letters: `hjkl` `0` `$` move, `b` `f` `space` page, `g`
`G` top, bottom, `n` `N` matches, `y` copy, `w` wrap, `.` quick fix, `s` inline
or split diff (Markdown: side by side), `z` whole file or only the changes in a
file's diff, `p` rendered Markdown, `O` open the
diffed file at the cursor, `H` pick a revision (header `←` `→` step), `e`
`$EDITOR`, `o` system app, `m` menu, `q` close.

Find box: `⏎` `↓` next, `shift+⏎` `↑` previous, `tab` to replace, where `⏎`
replaces and goes on; `ctrl+shift+1` one, `ctrl+alt+⏎` all; `alt+c` `alt+w`
`alt+r` `alt+p` case, word, regex, preserve case; `esc` closes.

## Vim mode

`vim_mode = true`: editable files open in normal mode; other `ctrl` keys work
in every mode.

- normal: `h j k l 0 $ w b e gg G` with a count; `i a I A o O` insert; `x D C
  J p P u`, `ctrl+r` redo, `v` `V` visual, `/` find, `n` `N`, `:` go to line;
  `d c y` over a motion (`dw`, `d$`, `dgg`) or doubled (`dd cc yy`); `esc`
  with nothing pending closes the editor
- visual: motions grow the selection, `o` swaps ends, `d x y c s` act on it
- insert: the editor itself; `esc` back to normal

## Pickers, menus, questions

- Palette, quick open, menus: `↑` `↓` (`ctrl+p` `ctrl+n`, `tab` `shift+tab`)
  move, `⏎` runs, `esc` `ctrl+c` close; without a query box `j` `k` move and
  `q` closes. In a path prompt `tab` or `→` takes the suggestion.
- A question: `←` `→` `h` `l` `tab` pick a button, `⏎` `space` run it, `esc`
  `q` cancel.
- References (`shift+F12`): `j` `k` `b` `f` `g` `G` move, `h` `l` fold, `⏎` go
  and close, `o` `space` go and keep the list, `esc` `q` `tab` close.

## Mouse and clipboard

- Right click: menu. Middle click: close a tab. Double click on the empty
  editor area: untitled file.
- In a file: drag selects, double click a word, triple click a line.
- Tilt or `shift`+wheel pans. The wheel scrolls a session's scrollback unless
  the app takes the mouse or the alternate screen.
- A drag over a terminal selects scrollback lines (`lineAt`), following its
  text; the release copies.
- Copies go through `wl-copy`, `xclip`, `xsel` or `pbcopy`, plus OSC 52 (ssh;
  VTE ignores it); `ctrl+v` reads back. The terminal's own paste arrives as a
  bracketed paste.
