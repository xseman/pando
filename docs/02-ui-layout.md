# UI layout

How the TUI runs as a daemon client, and how its screen is divided, sized,
hit-tested and dragged. The views themselves are in [03-views.md](03-views.md).

[Client](#client) · [Screen](#screen) · [Cursor](#cursor) · [Columns](#columns) ·
[Activity bar](#activity-bar) · [Editor groups](#editor-groups) ·
[Terminal panel](#terminal-panel) · [Short of room](#short-of-room) ·
[Dragging](#dragging) · [Rows](#rows) · [Mouse](#mouse) ·
[Scrollbars](#scrollbars) · [Tab strips](#tab-strips) · [Modals](#modals)

## Client

The TUI is one Bubble Tea `Model` (`app.go`) holding no state the daemon
owns. It never blocks: git, files, the daemon and language servers run in
`tea.Cmd`s whose results come back as messages.

```text
proto.Subscribe ─▶ waitEvent ─▶ eventMsg ─▶ onEvent ─▶ loadState,
                                                       loadSessions, …
tick (2 s) ─▶ refreshGit, reloadIfLive, saveDrafts, saveEditors, …
             every 15th: loadWorkspaces
```

- `onEvent` re-reads what an event names ([01](01-architecture.md#protocol));
  `screen` only for the shown session or Terminal shell.
- A lost connection flashes, then `reconnect` retries every second
  (`proto.EnsureDaemon`) and re-reads everything.

## Screen

```text
 left columns            main                      right columns
┌──────────────┬──────────────────────────────┬──────────────┐
│ activity bar │ editor tabs                  │ activity bar │
│ view header  │ preview / editor / session   │ view header  │
│ rows…        ├──────────────────────────────┤ rows…        │
│              │ terminal panel (bottom)      │              │
├──────────────┴──────────────────────────────┴──────────────┤
│────────────────────────────────────────────────────────────│ rule
│ ⎇ main* ⇅ 0↓ 1↑ ✖ 0 ⚠ 0 flash  Ln 3, Col 7  repo  ^⇧p  ! 1 │ status bar
└────────────────────────────────────────────────────────────┘
```

- `layout` places the columns, split by a one-cell divider (facing frame
  edges with `borders`).
- Status bar: VS Code's, on `bar_bg` in `description`'s grey, under a rule
  (`ruled`, `statusRule`: an underlined row in `panel_border`, the
  border right on the bar, a side activity bar's tint and edge running down to it; with
  `borders` the frame's edge is it). Left, the
  workspace: the branch opens the branch picker, sync (behind↓ ahead↑)
  syncs, or publishes a branch without an upstream, the problems (with
  `lsp_diagnostics`, zero too) list them. Right, the results open Search,
  Ln/Col go to line, the indentation its picker, the project a project
  switcher, `^⇧p` the palette, the update installs or restarts, and the
  bell at the end, VS Code's, the agents waiting on you (dotted, counted),
  the navigator. Each lights under the pointer and has a tip.
- Focus is a column index, `onMain` or `onPanel`; `ctrl+]` cycles left →
  main → panel → right (`cycleFocus`).

## Cursor

One terminal cursor, styled by the terminal unless an app asks otherwise:

| Where it stands  | Style                                                     |
| ---------------- | --------------------------------------------------------- |
| editor           | the terminal's; a bar in vim insert mode                  |
| session, shell   | what its app sent (`Screen.CursorStyle`, DECSCUSR)        |
| a text box       | the terminal's, at the caret the box draws (`realCaret`)  |

- Bubble Tea's plain cursor is DECSCUSR 1; `defaultCursor` (stdout) turns
  it into 0, the terminal's own, which is also what pando leaves on exit.
- Text boxes draw their caret, static, as a reverse cell: `realCaret` gives it
  the next cell's style and a zero-width APC mark, `takeCaret` finds the mark
  in the frame and puts the cursor there. Bubbles' own real cursor counts
  runes from the value's start, not cells from where the box scrolled to.

## Columns

- `left` / `right` in `config.toml` list columns of views (tabs), built by
  `cols()`. A view in neither joins the left column next to main.
- Widths come from `wants`: the column's `width`, else the side default,
  else 32; at least 20.
- A column shows its most recently shown tab (`recent`, `viewOn`).
- A one-tab column has no activity bar; ⚙ moves into its header. ⚙ opens
  the Manage menu (`manageMenu`, see `03-views.md` Commands).
- Move a view by dragging its tab or header title, or from its tab menu
  (`moveView`, `splitView`, `mergeView`, _Hide Sidebar_).

## Activity bar

`activity_bar = "side"` (the default) or `"top"`; both draw one chip per view (`barPad`,
icon, `barPad`). The mark (`markColor`) is `header_accent` for the open view,
`sash_hover`, a tint of the accent, for the icon and the mark under the mouse.
Where the chips would run into ⚙ the top bar tightens them (`tabs`): one cell
of air (word labels to their initials), no gap between chips, then the icon
alone, so every view keeps its chip down to the 20-cell column (emoji, two
cells each, need a wider one).

```text
top: activityBar, actH = 2 rows

  files    git    spaces       ⚙        icons row
──────────────────────────────────      mark row: ─ mid-row, air either side;
         └ mark: the accent         the open view's stretch in the accent

side: vertBar, actW wide on bar_bg, one vertH (2) row block per chip, its
      icon on the second row, the mark around it from the middle of the row
      over to the middle of the one under (╻ ┃ ╹); ⚙ at the bottom

 left column            right column
 ▎ icon ▕ view …        … view ▏ icon ▐
 └ mark └ border        border ┘      └ mark
```

- The border is `overview_ruler_border`. On a side bar a waiting view's icon
  takes the attention color (no room for a count).
- No side strip (`barW` 0) for a one-tab column, a railed one, or one
  narrower than `actW` + 20.
- A hidden side keeps a **rail** (`railW`): its icons down the outer edge,
  none marked, no border. Hide with `b`, `ctrl+b`, _Hide Sidebar_, or a second
  click on the open view's icon (on release, `drag.hide`, so it still drags).

## Editor groups

The editor area is a tree of groups (`groups.go`), VS Code's: a `pane` is a
group or two panes side by side (`row`) or stacked, the first taking `ratio`
of the room, a one-cell divider between them. A group is one strip of files
and sessions and the one it shows. The focused group's state lives in `Model`
(`pv`, `editors`, `edIdx`, `sess`, `term`, the session tabs); the others wait
in their `group` and are swapped in (`within`) to draw, fetch a screen, or
take a message (`previewMsg`, `onScreen`, drafts).

```text
 main.go  store.go   □ ✕ │ claude                 □ ✕
 ───────                 │ ──────
  editor                 │  session
                         ├──────────────────────────
                         │ notes.md               □ ✕
                         │  editor
```

- A file or session lives in one group: opening one another group holds
  focuses that group (`holder`), otherwise it opens in the focused group.
- Splitting: a tab dragged over a group's body (`gridDrop`) splits it on the
  edge it is near, a quarter of its width or height, and moves into a group
  of its own there; on a strip or the middle it moves into that group. A
  group's only tab offers nothing over its own group. `ctrl+\`
  moves the active tab into a new group on the right (`splitEditor`), the
  palette's _Move Editor into Left / Right Group, Group Above / Below_ into
  the neighbour or a new group (`moveEditor`). A group added or closed
  shares the room out evenly again (`even`): groups side by side as wide as
  each other, stacked as tall; a group left empty closes (`prune`). One
  showing nothing but kept by a session tab that ended closes on `ctrl+w`
  or `ctrl+d` (`idleGroup`, `closeGroup`), its neighbour taking the keyboard.
- `ctrl+1`…`ctrl+9` focus group N, `ctrl+]` walks the groups, a click focuses
  the one under it; the wheel scrolls a group without moving the focus. A
  group without the keyboard draws its active tab and cursor unlit
  (`offMain`). In a split the focused group is framed in `sash_hover`: the
  rule under its strip, the dividers beside it and the edge of a column
  beside it (`frames`, `paneLines`, `editorRule`, `View`). A group showing a
  session that waits or failed is framed in `error` instead, one that
  finished unseen in `attention` (`alert`), pulsing until the group is
  focused; `session_highlight` `steady` keeps it still, `off` drops it. The
  status bar follows the focused group.
- A divider drags (`dragSplit`), each group keeping `minGroupW` × `minGroupH`
  (20 × 5); a double click shares the room of the two beside it evenly, the
  others kept (`evenPair`). A split is refused without room; an area too
  small for every group draws the focused one alone, the tree kept.
- The strip's □ (`maxAct`, _Toggle Maximized Editor Group_) draws the focused
  group alone until pressed again.
- Each worktree keeps its tree, ratios, tabs (sessions' too), the focused
  group and maximize in `state.json` (`editors`, `gridSpec`, `restoreGrid`).
- A session's tab has no header row: the tab names the session, the frame
  title (`mainTitle`) the project and worktree, the strip carries the hint
  (exited, scrollback, where `ctrl+]` goes; `sessionHint`) and the buttons.
  Its ✕ (`closeSessTab`) closes the tab, the session running on. The
  strip's empty part or the session's tab, right-clicked, is its menu
  (`sessionTitleMenu`). Terminal panel shells stay in the panel.

## Terminal panel

`terminal_position = "bottom"` puts it under main: `termRows()` comes off
`mainH()`, and a border over its strip, the rule under it and the strip's empty
part are a sash for `terminal_height`; the border and the rule light heavy while
the mouse rests on it or drags. `panelTopH` counts the three rows.
`left` or `right` makes it a column holding only `viewTerm`, its tabs too.

The strip holds the panel's tabs (`panel.go`, `panelChips`: Terminal,
Problems with their count, Output; short titles in a strip too
narrow for them), the active one bold and marked on the rule under it, then
the buttons (`termButtons`: on the Terminal `+` and the select, then ⋮ the
tab's menu and where the panel goes,
maximize and ✕ under the editor, ✕ closing the shell in a column), always
shown, `termStripH` rows over the body; a column has no header row and
keeps the tabs. Each worktree keeps its panel maximized or not, a restart
too (`terminals`' `max`).

```text
 Terminal   Problems 3   Output              +  1: fish ▾  ⋯  □  ✕
 ────────                                          └ the select: every shell,
 ~ $ make test                                       New Terminal, Kill Terminal…
 ok   ./...
```

| Tab        | Body                                                   | Opened by                      |
| ---------- | ------------------------------------------------------ | ------------------------------ |
| Terminal   | the shell shown, the others behind the select          | ``ctrl+` ``, `ctrl+j`, `5`     |
| Problems   | every file's problems (`m.probs`, a `peek`)            | `ctrl+shift+m`, the status bar |
| Output     | servers' stderr and logs, every error flashed (`out`)  | `ctrl+shift+u`                 |

- `ptab` is the tab; `showPanel` opens the panel on one, focused. On the
  Terminal the keyboard is the shell's (`termFocused`); on the others it is
  a list's, and pando's keys work (`panelKey`).
- The shells are VS Code's select (`termSelect`: `N: name ▾`, `N ▾` in a
  strip under 60 cells; `termPick` its list, the one shown checked), the
  screen the panel's whole width. The last shell gone shuts the panel only
  on the Terminal tab.
- A row under the mouse lights in every tab (`panelHover`, `hover_bg`).
- Problems take the whole width, no source beside them. The References
  open inline in the editor instead (`04-preview.md`).

## Short of room

Nothing is saved; a wider screen undoes it.

```text
editor < minEditor (40)?
  1. columns ─────────▶ narrow to 20, outermost first
  2. side w/o Spaces ─▶ folds into its rail (folds)
  3. Spaces ──────────▶ narrows to 20
editor < 20?
  4. editor ──────────▶ gives up all but 20 cells
  5. columns ─────────▶ drop, outermost first, Spaces last
```

A side opened from its rail stays open until the next resize (`unfold`). A
dragged divider stops at `minEditor`. Groups that no longer fit draw the
focused one alone ([Editor groups](#editor-groups)).

## Dragging

| Drag                                      | Effect                                     |
| ----------------------------------------- | ------------------------------------------ |
| column divider                            | width, saved on release                    |
| editor group divider                      | the groups' ratio, saved                   |
| editor or session tab over a group        | split it or move in (`gridDrop`)           |
| Explorer file, Spaces session over one    | the same (`dragOpen`, `dragRowTo`)         |
| terminal rule and strip's empty part      | `terminal_height`                          |
| Source Control drawer, GitHub pane header | the pane's top edge                        |
| tab onto a bar, or the half facing main   | slot between chips (`slotAt`, `placeView`) |
| tab below a bar, toward the screen edge   | a new column                               |
| Spaces project, worktree or session row   | a tinted row between rows (`dropSlot`)     |

`dropAt` picks the target, `dropBox` draws it. A Spaces row moves only on
release, onto the row of its kind under the pointer (`dropOn`); a project
goes above a project's upper half, below its lower half, and into a gap
between projects (`projectDrop`). Let go anywhere else, it stays.
A file pressed in Explorer opens in the preview tab, a session of the
worktree in view in Spaces opens as a click opens it, and held on, its tab
goes over the editor area as one off its strip (`dragOpenTo`), the tab it
opens in riding under the pointer (`dragGhost`). A session that moves among
its worktree's opens on the release instead, over the editor area
(`dragRowTo`); off the list's rows the tab it would get rides along.
Another worktree's session has no group to go to: the groups are the
worktree in view's.

A sash lights after `sashDelay` (300 ms) in
`sash_hover`, the accent while dragged (`trackSash`, `sashUnder`,
`sashRule`).

## Rows

Every row is segments padded to the exact width (`row` in `widgets.go`);
`checkWidths` asserts every line is the terminal width.

- A segment is painted by `renderText`: `st.Render(s)` byte for byte
  (`FuzzRenderText`), from the escapes kept per style (`renderSides`) where
  the style only colors text and `s` is one line. lipgloss's `Render` passes
  over every property a style has, and was a third of a frame.
- Widths are grapheme widths, as the session's emulator counts them. Bubble
  Tea's renderer counts them so only where the terminal answers mode 2027
  (`graphemes`); elsewhere (tmux, Alacritty) it sums wcwidth, so `wcSafe`
  rewrites the clusters the two disagree on before the frame goes out:
  `⚠️` → `⚠ `, `👍🏽` → `👍`. Left in, the renderer would place the rest of
  the row a cell off and its next partial redraw would land there.

```text
row(w, bg, left…, right…)   " ▾ Changes            ↶ + [3] "
                              │ │                  │   └ count badge
                              │ └ label            └ hover actions
                              └ chevron
```

## Mouse

```text
Model.mouse
  modal open?      → modal.mouse
  status bar row   → statusMouse
  column rect      → sideMouse: side strip, y < barH tabs, y == barH header
  divider gap      → drag divider
  main rect        → preview / session / terminal panel
```

- Hit tests reuse the renderer's geometry (`toggles`, `actions`, `buttons`),
  minus the scrollbar column.
- All-motion mouse mode: the row under the pointer paints `hoverBg`; header
  actions linger `actionsLinger` (3 s), since terminals never report "mouse
  left".
- Motion draws a frame per `motionGap` (1/60 s, the renderer's rate): one
  sooner gets the last frame again (`motionFrame`, `still`) and a
  `redrawMsg` draws where the pointer stopped. Drawn per motion, a fast drag
  queued its motions, hundreds a second, and trailed the pointer.
- An icon button names itself and its key in a box under the pointer once
  it has rested `tipDelay`, until `actionsLinger` (`tipShown`, `hoverBox`):
  header actions, activity bar chips, strip buttons, the Terminal's `+` and `✕`, Source
  Control rows and ∨, Search's toggles, ▸ and ⋯, find widgets, status bar
  items. A session name its Spaces row or its tab cuts, at 32 cells
  (`sessionName`) or at the edge, shows whole the same way (`fullName`,
  `sessTab.tip`). Moving on from a tip shows the next at once (`tipSeen`);
  a key, a click or the wheel puts it away until the pointer moves
  (`tipOff`). The frame notes the `tip` of the button it draws under the
  mouse (`Model.tip`); `tipWakeMsg` wakes View at both moments, the sooner
  one first (`tipLater`). `tooltips = false`, or Settings' _Tooltips_, turns
  them off.

## Scrollbars

| Where                     | Bar                                                            |
| ------------------------- | -------------------------------------------------------------- |
| editor, session, Terminal | `vbar` in the last column, always reserved                     |
| editor, word wrap off     | `hbar` row under the text while a line overflows (`hbarH`)     |
| table, panel's Output     | a row under the rows while one overflows (`outputLog.hbar`)    |
| sidebar lists             | the same `vbar` cells (`listBar`); one per Source Control pane |

- Colors `scrollbar_slider`, `scrollbar_slider_hover`,
  `scrollbar_slider_active` over a solid `scrollbar_track` column (a thin
  `overview_ruler_border` rule where a theme has none, the terminal one);
  hover state via `overBar` and `barState`. A column's cells are
  backgrounds, not block glyphs, which seam at fractional scaling in VTE; a
  row's (`hcells`) are lower half blocks `▄`, track and slider as
  foregrounds, a half-height bar. Shift+wheel or a tilt pans (`wheelX`).
- Click grabs the slider or jumps to the track point; `dragScroll` follows.
  A diff's bar marks its changes (`vbar.marked`), on the slider too; a click
  on a mark off the slider goes to that change.
- An app that scrolls itself (`ownScroll`), on the alternate screen
  (`Screen.AltScreen`) or taking the mouse (`Screen.Mouse`, claude's
  fullscreen view), has no bar, its column kept so the width stays; the
  wheel and `pgup`/`pgdn` go to it. What pando kept of a mouse app's
  scrollback is what scrolled off its redraws, no history.

## Tab strips

Editors, session tabs and Terminal shells share `strip.go`. The editor
group has one strip, the files' tabs then the sessions'.

- `tabChip`: active bold, others dim, neither on a fill (`hover_bg` under
  the mouse, `overTab`), each followed by a `▏` in `tab_border`
  (`tabGap`). Room for `✕` is always kept (`tabClose`), so activating moves
  nothing. The active tab shows its `✕`, any other only under the mouse
  (unsaved, its `●` turns into it), in any group; a click on it closes that
  tab. It raises like a button while the mouse is on it (`overX`,
  `closeHot`).
- A session's tab in the editor strip shows its glyph in Spaces' color and
  sits on a much softer shade of it (`tabTint`), active or not, so it reads
  apart from the files'; a session_highlight tint or `hover_bg` takes its
  place.
- Press picks a tab up (`grabTab`, `dragStrip`), an accent `│` marks the slot
  (`stripSlot`), release moves it. While held, the tab rides a row under the
  pointer, as VS Code's drag image (`dragGhost`, `sel_bg`, an accent `▎`); off its own strip it goes to a group
  ([Editor groups](#editor-groups)). `ctrl+shift+pgup`/`pgdn` move it
  (`moveTab`). A session's own tab stays first in the Terminal's strip.
- The editor strip's session tabs follow the files' until one is moved
  among them (`stripMove`): it keeps the files that were before it
  (`sessAfter`) and sits after the last of them still open (`sessPos`), so
  the preview tab showing another file (`renameTab`), a file opened or one
  closed leaves it in place. `session_at` keeps it over a restart. The
  group's own session, one a worktree switch or a restart showed without a
  tab, keeps a tab behind the files too (`stripSessions`).

## Modals

One at a time (`Model.modal`): menu, picker (fuzzy), prompt, dialog
(`newDialog`), or a form (`modal.form`, a `formBox`: the Databases view's
connection form, the indentation, the folder picker). Centered on the
screen, or anchored (`x`, `y`) where the thing it acts on is, as rename over
a symbol, a table's prompt over its cell and an editor's menu by key (`m`,
`⇧F10`) under the cursor's row (`menuAt`), and the revert question it leads
to; the form, and a menu centred in an area, sit at the top of the area's
body. `area` keeps a box to some columns, a table's to its editor group,
and covers them whole when less than two cells a
side would show. An anchored menu that does not fit below its line opens
above it; a box never narrows while it is open (`modal.w`). A menu at the
pointer has square corners, a centered box round ones. A context menu's
groups are `menuGroups`, a rule between them; the editor's go as VS Code's:
the code (definition, references, symbol, rename, quick fix, hover),
formatting, undo, the clipboard, Select All, find, then the file's own.

A hint row too long for its width drops whole hints from its end, the
closing one kept last (`fitHints`), rather than cut one.

```text
╭ Select a branch or tag to checkout ─────────╮
│›                                            │  input
│ + Create new branch…                        │  always
│                                             │
│ branches                                    │  group heading
│ ⎇ feat/one 42 seconds ago                   │  selected, inline note
│   Ann • 059c60c • first commit              │  detail, selected only
╰─────────────────────────────────────────────╯
```

| Item field | Meaning                                                  |
| ---------- | -------------------------------------------------------- |
| `search`   | `@idle`, `!claude` tokens match verbatim, the rest fuzzy |
| `group`    | heading; a query ranks within groups                     |
| `always`   | kept after the matches                                   |
| `inline`   | note after the label                                     |
| `hint`     | right-aligned                                            |
| `detail`   | row under the selection only                             |

A picker marks the characters its query matched in each label in the
accent, VS Code's `list.highlightForeground` (`fuzzyHits`, `marked`), and
says _No matching results_ when nothing is left.

The folder picker (`dirpick.go`, `newDirPicker`) is a path to type over the
places (home, the XDG user directories, GTK's bookmarks, the folders
holding the projects) and the path's folders: what follows its last slash
filters them, `..` always first, dotted ones once a dot is typed. `tab`
walks path, places, folders and buttons; ⏎ or a second click on a folder
goes in, ⏎ on the path or the primary button picks it. Both columns scroll,
by the keyboard and the wheel (`dirForm.wheel`).

A dialog's buttons run `items[0]` (primary, focused) to `cancelItem()`,
drawn primary rightmost with Cancel before it. `←→` `tab` walk, `⏎` runs,
`esc` cancels; the button under the pointer lights without taking the focus
(`modal.hover`).
