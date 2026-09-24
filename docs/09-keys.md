# Keys

Every command is also in the command palette (`ctrl+shift+p`), where its id
is what `[keys]` in `config.toml` rebinds; `pando doctor` prints the ids.
`?` opens the same list in the TUI.

⌃` reaches pando only where the terminal can tell it from ⌃space: both are NUL
without the kitty keyboard protocol, and in an editor that byte belongs to the
suggestions, so the panel stays on `ctrl+j` there. Inside tmux the protocol
needs `set -s extended-keys on` and `set -as terminal-features ",*:extkeys"`.

A key means what the focused part makes of it, as VS Code's `when` clauses
say (`keyContext`, `keybindings` in `internal/ui/keys.go`):

| Focus                                   | Keys                                                                                              |
| --------------------------------------- | ------------------------------------------------------------------------------------------------- |
| a session or the Terminal panel         | every key goes to the app, but for the chords a terminal gives up (VS Code's commandsToSkipShell) |
| an editable file                        | its keys are text: `5`, `[` `]` type, `ctrl+←` `ctrl+→` move by words                             |
| a text box (commit message, find, …)    | the box keeps its keys; `[keys]` and pando's panel chords wait                                    |
| a sidebar list, a diff, a rendering     | the single letters and every chord below                                                          |

In the commit message `⏎` commits and `shift+⏎` starts a new line; `alt+⏎`
does too, for the terminals that send `shift+⏎` as a plain `⏎`. Selecting is
the editor's: `ctrl+a` all, `shift+←→↑↓` or a mouse drag, then `ctrl+c` copy,
`ctrl+x` cut, or type over it. `home` is the line start.

A terminal gives up `ctrl+]`, `ctrl+shift+p`, `ctrl+shift+f` `ctrl+shift+e`
`ctrl+shift+g` `ctrl+shift+h`, `ctrl+j` ``ctrl+` `` `ctrl+space`,
``ctrl+shift+` ``, `ctrl+shift+↑↓`, `ctrl+b`, `ctrl+,`, `ctrl+0` `ctrl+1`,
`alt+t`, `ctrl+pgup` `ctrl+pgdn` and `alt+1`…`alt+9`; everything else, `ctrl+←`
`ctrl+p` `ctrl+s` `ctrl+enter` `F1` included, is the shell's.

| Key                                                         | Action                                                                                       |
| ----------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `ctrl+]`                                                    | cycle focus: left sidebar, main, right sidebar. In a session every other key goes to the app |
| `1`–`4`                                                     | Files, Git, Spaces, Search                                                                   |
| `ctrl+shift+e` `ctrl+shift+g` `ctrl+shift+f` `ctrl+shift+h` | Explorer, Source Control, Search, Search with replace                                        |
| `ctrl+j`, `5`, ``ctrl+` ``                                  | toggle the terminal panel (a terminal sends the same byte for ``ctrl+` `` and `ctrl+space`)  |
| ``ctrl+shift+` ``                                           | another shell in the terminal panel                                                          |
| `ctrl+shift+↑` `ctrl+shift+↓`                               | maximize the terminal panel, and restore it                                                  |
| `ctrl+shift+p`, `F1`                                        | command palette: every command available now, each one bindable in `[keys]`                  |
| `ctrl+p`                                                    | quick open (`ctrl+t` or the title button switches list and tree)                             |
| `alt+t`                                                     | go to an agent session or worktree (`@idle`, `@running`, `!claude` narrow it)                |
| `[` `]`                                                     | previous / next session                                                                      |
| `ctrl+n`                                                    | new untitled file (a double click on the empty editor area does the same)                    |
| `ctrl+tab`, `ctrl+w`                                        | next editor, close editor (middle click closes a tab too); a workspace reopens its editors   |
| `ctrl+pgup` `ctrl+pgdn`, `alt+1`…`alt+9`                    | previous / next editor, editor N                                                             |
| `ctrl+alt+-` `ctrl+shift+-`, `alt+,` `alt+.`                | back and forward through visited editors (also `ctrl+-`, `super+←` `super+→`)                |
| `ctrl+0` `ctrl+1`                                           | focus sidebar / editor                                                                       |
| `ctrl+f`                                                    | in a panel: filter the list (`enter` keeps it, `esc` clears); in a file: find                |
| `ctrl+b`, `b`, `<` `>`                                      | fold the sidebars to a rail of view icons (click one or `»` to reopen), resize a column      |
| `ctrl+,`, `?`                                               | settings, help (`,` in a sidebar too)                                                        |
| `m`, right click                                            | context menu                                                                                 |
| `q`, `esc`                                                  | close the TUI once there is nothing left to close; it asks first (sessions keep running)     |

| View   | Keys                                                                                                                                                                                                                                |
| ------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Files  | `⏎` open, `h`/`l` fold, `n`/`N` new file/folder, `R` rename, `D` delete, `s` stage, `e` edit, `y`/`Y` copy path, `.` hidden files, `C` collapse all                                                                                 |
| Git    | `⏎` stage/unstage, `o` diff, `t` tree or list, `a`/`u` stage/unstage all, `U` stage untracked, `d` discard, `c` message, `C` commit, `A` suggest, `S` sync or publish, `O` open file, `B` switch branch or tag, `{` `}` switch repo |
| Spaces | `⏎` switch, `M-↑↓` move a project, `n` new shell session, `w` new worktree, `a` add project, `o` view options (filter, sort, group), `x` kill a session, delete a worktree, close a project                                         |

| In a file                         | Action                                                                    |
| --------------------------------- | ------------------------------------------------------------------------- |
| arrows, `shift`+arrows, drag      | move the cursor, select                                                   |
| `ctrl+←` `ctrl+→`                 | a word back, a word on (`shift` selects), past blanks and punctuation     |
| `ctrl+↑` `ctrl+↓`                 | scroll the view, the cursor stays where it is (the wheel does the same)   |
| `ctrl+a`, `ctrl+c`, `ctrl+v`      | select all, copy the selection (or the whole file), paste                 |
| `ctrl+z` `ctrl+y`, `ctrl+s`       | undo, redo, save (`●` until you do; an untitled file asks where)          |
| `⏎`, `tab`, `⌫`, `del`            | typing; `⏎` keeps the indent                                              |
| `ctrl+x`, `ctrl+shift+k`          | cut, delete the line or selection                                         |
| `alt+shift+↑↓`, `ctrl+d`          | copy the line or the selected lines up, down                              |
| `alt+↑` `alt+↓`                   | move the line                                                             |
| `alt+shift+→←`                    | expand, shrink the selection: word, line, brackets, file                  |
| `ctrl+/`                          | toggle a comment                                                          |
| `ctrl+space`                      | suggestions                                                               |
| `ctrl+shift+i`                    | format document                                                           |
| `ctrl+f`, `⏎`/`F3`, `shift+F3`    | find widget, next, previous (`alt+c` `alt+w` `alt+r` case/word/regex)     |
| `ctrl+h`, `⏎`, `ctrl+alt+⏎`       | replace box (`tab` switches fields), replace and go on, replace all       |
| `alt+p`                           | preserve case while replacing                                             |
| `ctrl+g`                          | go to line: the `:` picker, also `:` typed first in `ctrl+p`              |
| `ctrl+shift+o`                    | go to symbol; `@:` groups them by kind, `@` in `ctrl+p` does the same     |
| `alt+z`, header wrap toggle       | word wrap (`word_wrap`, off by default); off, a scrollbar pans long lines |
| `s`                               | diff inline or side by side                                               |
| `shift+F10`, right click          | Stage / Unstage / Revert Selected Ranges in a diff (`m` outside a file)   |
| `ctrl+⏎`                          | commit                                                                    |
| `O`, header button                | open the file a diff shows, at the line under the cursor                  |
| `←` `→` header buttons, `H`       | step through the file's revisions, pick one                               |
| `ctrl+shift+v`, `alt+v`           | rendered Markdown, beside the source                                      |
| `e`                               | open in `$EDITOR`                                                         |
| `esc`, `q`                        | clear the selection, close                                                |
| vim mode                          | `vim_mode = true`: the editor opens in normal mode, `i` types, `esc` back |

Every copy (a selection, a terminal's text, a path) goes to the desktop's
clipboard through `wl-copy`, `xclip`, `xsel` or `pbcopy`, whichever the session
has, and as OSC 52 too for a terminal that takes it, which is what reaches the
desktop over ssh; VTE terminals (GNOME Terminal, Ptyxis) ignore OSC 52. `ctrl+v`
in a file and the Terminal's _Paste_ read it back with the matching tool; the
terminal's own paste (`ctrl+shift+v`) arrives as a bracketed paste anywhere.

A tilt wheel or `shift`+wheel scrolls sideways; the plain wheel scrolls a
session's scrollback when the app does not use the mouse, and on the alternate
screen (a fullscreen claude, less, vim) it is the app's: mouse events, or
arrows for one without the mouse. A left drag over a
session or the Terminal selects text and the release copies it (`term.sel`);
it is drawn in reverse video until a key or the next click.
