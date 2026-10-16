# UI layout

How the TUI runs as a daemon client, and how its screen is divided, sized,
hit-tested and dragged. The views themselves are in [03-views.md](03-views.md).

[Client](#client) · [Screen](#screen) · [Cursor](#cursor) · [Columns](#columns) ·
[Activity bar](#activity-bar) · [Session column](#session-column) ·
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
│ ⎇ main* ↑1  repo   flash   ! 1 waiting   Ln 3, Col 7       │ status bar
└────────────────────────────────────────────────────────────┘
```

- `layout` places the columns, split by a one-cell divider (facing frame
  edges with `borders`).
- Status bar items: the branch opens the branch picker, the project a
  project switcher, the waiting count the navigator, the results Search,
  Ln/Col go to line, the indentation its picker, `^⇧p` the palette, the
  update installs or restarts. Each lights under the pointer and has a tip.
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

`activity_bar = "top"` or `"side"`; both draw one chip per view (`barPad`,
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

side: vertBar, actW wide, one actH-row block per chip, ⚙ at the bottom

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

## Session column

The agent session is `viewSession`. `session_position` is `editor` (default:
over the editor area, a tab in its strip) or `right` / `left` (a column;
unsized, half the editor area).

- Over the editor area each session opened from Spaces has a tab after the
  files' (`stripSessions`, `sessTabs`, not saved; the one shown stays), the
  one shown with its shell tabs and a `+` (`editorTabs`). Opening a file
  keeps it there; the file's tab or `alt+N` brings the file forward. Its ✕
  (`closeSessTab`) closes the tab, the session running on, and shows the
  session before it when no file is open; a shell tab's ✕ kills it.
- Its tab dragged to the editor area's right or left quarter docks it there
  (`sessTabClick` starts the title's `dragTab`, `sessionDrop`); docked, its
  tab leaves the strip for the column's own. Short of room the column comes
  back as its tab and docks again once there is room (`cramped`, below).

- Its column dropped mid-editor goes back to a tab. Widened past `minEditor` (40)
  cells of editor it is maximized (`toggleMax`): restoring docks its column
  again at the width it was picked up at. Narrowed below `snapHide` (10) it closes
  (`hideSession`) and keeps running.
- Each worktree keeps its own place and width (`session_views` in
  `state.json`).
- No header row, in the editor area or in its column: the tab names the
  session, the frame title (`mainTitle`, `sideTitle`) the project and
  worktree. The strip carries what a header would: maximize / restore and ✕
  (`sessionButtons`), always shown as the editor's are, and the hint (exited,
  scrollback, where `ctrl+]` goes; `sessionHint`) before them, with a rule
  under it (`sessionRule`, `sessStripH` rows over the screen). The empty part
  of the strip is the title: drag it to dock, right click for its menu.
- The button among the actions of the session's tab strip and of the file's
  (`toggleMax`, also _Toggle Session / Editor Maximized_) gives the whole editor
  area to the session or to the file, the other one waiting; pressed again it
  docks them side by side. A maximized session restores from the right end of
  its tab strip, the first row (`maxed`: `cols` drops the column, `preview`
  says who shows). Each session keeps its own, a restart too
  (`sessions_maxed`, `maxSession`): going back to one shows it as it was
  left. A maximized file is its worktree's (`session_views`' `max`: `file`);
  opening a file over a maximized session restores.

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
  1. docked session ──▶ takes the editor area (crampedBy, followCramp)
  2. other columns ───▶ narrow to 20, outermost first
  3. side w/o Spaces ─▶ folds into its rail (folds)
  4. Spaces ──────────▶ narrows to 20
editor < 20?
  5. editor ──────────▶ gives up all but 20 cells
  6. columns ─────────▶ drop, outermost first, Spaces last
```

A side opened from its rail stays open until the next resize (`unfold`). A
dragged divider stops at `minEditor`. In step 1 the session and the file share
the editor area, the one that had the keyboard last in front (`fileLast`).
While a divider is dragged the columns stay as they were picked up
(`drag.cramp`): letting it go docks the session again when there is room, or
gives it the editor area when there is not; a maximized one (`maxed`) stays
maximized. The session's column keeps its width in the settings meanwhile
(`allCols`).

## Dragging

| Drag                                      | Effect                                     |
| ----------------------------------------- | ------------------------------------------ |
| column divider                            | width, saved on release                    |
| terminal rule and strip's empty part      | `terminal_height`                          |
| Source Control drawer, GitHub pane header | the pane's top edge                        |
| tab onto a bar, or the half facing main   | slot between chips (`slotAt`, `placeView`) |
| tab below a bar, toward the screen edge   | a new column                               |
| Spaces project, worktree or session row   | a tinted row between rows (`dropSlot`)     |

`dropAt` picks the target, `dropBox` draws it. A Spaces row moves only on
release, onto the row of its kind under the pointer (`dropOn`); a project
goes above a project's upper half, below its lower half, and into a gap
between projects (`projectDrop`). Let go anywhere else, it stays. A sash lights after `sashDelay` (300 ms) in
`sash_hover`, the accent while dragged (`trackSash`, `sashUnder`,
`sashRule`).

## Rows

Every row is segments padded to the exact width (`row` in `widgets.go`);
`checkWidths` asserts every line is the terminal width.

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
- An icon button names itself and its key in a box under the pointer once
  it has rested `tipDelay`, until `actionsLinger` (`tipShown`, `hoverBox`):
  header actions, activity bar chips, strip buttons, `+` and `✕`, Source
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
- On the alternate screen (`Screen.AltScreen`) the bar is empty and the wheel
  and `pgup`/`pgdn` go to the app.

## Tab strips

Editors, session tabs and Terminal shells share `strip.go`. The editor
area has one strip, the files' tabs then the sessions' over it; a docked
session's column has its own.

- `tabChip`: active bold, others dim, neither on a fill (`hover_bg` under
  the mouse, `overTab`), each followed by a `▏` in `tab_border`
  (`tabGap`). Room for `✕` is always kept (`tabClose`), so activating moves
  nothing. The active tab's `✕` raises like a button while the mouse is on
  it (`overX`, `closeHot`).
- Press picks a tab up (`grabTab`, `dragStrip`), an accent `│` marks the slot
  (`stripSlot`), release moves it. `ctrl+shift+pgup`/`pgdn` move it
  (`moveTab`). A session's own tab stays first.

## Modals

One at a time (`Model.modal`): menu, picker (fuzzy), prompt, dialog
(`newDialog`), or a form (`modal.form`, a `formBox`: the Databases view's
connection form, the indentation, the folder picker). Centered on the
screen, or anchored (`x`, `y`) where the thing it acts on is, as rename over
a symbol, a table's prompt over its cell and an editor's menu by key (`m`,
`⇧F10`) under the cursor's row (`menuAt`), and the revert question it leads
to; the form, and a menu centred in an area, sit at the top of the area's
body. `area` keeps a box to some columns, a table's to the editor area, off
a session docked beside it, and covers them whole when less than two cells a
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
