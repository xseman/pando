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
- Status bar buttons: the branch opens the branch picker, the project a
  project switcher, the agent count the navigator.
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
- A one-tab column has no activity bar; ⚙ moves into its header.
- Move a view by dragging its tab or header title, or from its tab menu
  (`moveView`, `splitView`, `mergeView`, _Hide Sidebar_).

## Activity bar

`activity_bar = "top"` or `"side"`; both draw one chip per view (`barPad`,
icon, `barPad`). The mark (`markColor`) is `header_accent` for the open view,
`sash_hover`, a tint of the accent, for the icon and the mark under the mouse.

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

The agent session is `viewSession`. `session_position` is `right` (default),
`left` (a column; unsized, half the editor area) or `editor` (over the editor
area; opening a file docks it back, `sessSide`).

- Dropped mid-editor it takes the editor area. Widened past `minEditor` (40)
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
  its tab strip, the first row. Not saved (`maxed`: `cols` drops the column,
  `preview` says who shows); opening a file over a maximized session restores.

## Terminal panel

`terminal_position = "bottom"` puts it under main: `termRows()` comes off
`mainH()`, and its title row is a sash for `terminal_height`. `left` or
`right` makes it a column holding only `viewTerm`.

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
dragged divider stops at `minEditor`.

## Dragging

| Drag                                      | Effect                                     |
| ----------------------------------------- | ------------------------------------------ |
| column divider                            | width, saved on release                    |
| terminal title row                        | `terminal_height`                          |
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

## Scrollbars

| Where                     | Bar                                                        |
| ------------------------- | ---------------------------------------------------------- |
| editor, session, Terminal | `vbar` in the last column, always reserved                 |
| editor, word wrap off     | `hbar` row under the text while a line overflows (`hbarH`) |
| sidebar lists             | thin `┃` (`listBar`); one per Source Control pane          |

- Colors `scrollbar_slider`, `scrollbar_slider_hover`,
  `scrollbar_slider_active` over an `overview_ruler_border` track; hover state
  via `overBar` and `barState`.
- Click grabs the slider or jumps to the track point; `dragScroll` follows.
- On the alternate screen (`Screen.AltScreen`) the bar is empty and the wheel
  and `pgup`/`pgdn` go to the app.

## Tab strips

Editors, session tabs and Terminal shells share `strip.go`.

- `tabChip`: active bold, others dim, neither on a fill (`hover_bg` under
  the mouse, `overTab`), each followed by a `▏` in `tab_border`
  (`tabGap`). Room for `✕` is always kept (`tabClose`), so activating moves
  nothing.
- Press picks a tab up (`grabTab`, `dragStrip`), an accent `│` marks the slot
  (`stripSlot`), release moves it. `ctrl+shift+pgup`/`pgdn` move it
  (`moveTab`). A session's own tab stays first.

## Modals

One at a time (`Model.modal`): menu, picker (fuzzy), prompt, or dialog
(`newDialog`).

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

A dialog's buttons run `items[0]` (primary, focused) to `cancelItem()`,
drawn primary rightmost with Cancel before it. `←→` `tab` walk, `⏎` runs,
`esc` cancels.
