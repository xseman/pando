# UI layout

The screen is sidebar columns around one main area, VS Code's shape with
terminal cells.

```
 col 0      col 1                     main                     col 2
┌────────┬──────────────────┬──────────────────────────────┬──────────┐
│ SPACES │  Files Git  ⚙ «  │ app.ts ✕ │ notes.md          │ SEARCH   │  ← tabs / editor strip
│ ▾ repo │ EXPLORER  +f +d  │ ✕ app.ts  src — diff         │ ▏query ▕ │  ← view header
│   main │ ▾ src            │ 1  1  import …               │ 2 results│
│        │    app.ts      M │ 2    - old                   │ ▾ app.ts │
│        │                  │    2 + new                   │   line…  │
├────────┴──────────────────┴──────────────────────────────┴──────────┤
│ ⎇ main* ↑1  repo      staged 2 lines        ! 1  12 results  ^⇧p    │  ← status bar
└─────────────────────────────────────────────────────────────────────┘
   ▲ single tab: no activity bar          ▲ dividers drag to resize
```

- A **status bar** spans the bottom: branch and sync state, flash messages,
  agents waiting for you, search results, and with an editor open its cursor
  (`Ln, Col`) and indentation (`Tab Size`). The branch and the project name are
  buttons, lit under the mouse: the branch opens the branch picker, the project
  a switcher over every project with _Add Project…_ on top; the agent count
  opens the navigator.
- The bottom terminal panel's title row, outside its tabs, is a sash: dragging
  it sets `terminal_height`.
- An open Source Control drawer's header, and a GitHub section's below
  another open one, is a sash too: dragging it moves the pane's top edge.
- A sash (a column's divider, a title row or pane header) lights once the
  pointer has rested on it for `sashDelay` (300 ms, VS Code's
  `workbench.sash.hoverDelay`): `┃` in `sash_hover`, a `━` rule across the
  row's free part (`sashRule`), and the accent while it is dragged, VS Code's
  `sash.hoverBorder` and `sash.activeBorder`. `trackSash` notes the sash under
  each mouse event (`sashUnder`, the same cells the mouse code drags) and
  ticks View once the delay is out; with panel borders both frame edges of the
  gap light. A right click in the panel opens its menu (Copy
  All, Paste, Clear, Kill Terminal, Toggle Size to Content Width).
- A view moves by dragging its tab or its header title to the other side.
- The agent session is the `session` view: a column of its own beside the
  editor, on the side `session_position` names (`right` by default); a column
  nobody sized takes half the editor area. `left`/`right` list it once it has
  been docked or resized by hand, and keep its place while `session_position`
  is `editor` — the session over the whole editor area, which it takes when it
  is widened past `snapMain` cells of editor or dropped in the middle of it.
  Opening a file then docks it back on that side (`sessSide`), so the file has
  somewhere to go. Narrowed below `snapHide` cells it closes instead, as a VS
  Code sidebar dragged shut does (`hideSession`, the session runs on), at the
  width it was picked up at. Each worktree keeps its own place and width
  (`sessView`, `session_views` in `state.json`): `cols()` sizes the column
  where `left`/`right` list it, or puts it beside the editor when the worktree
  keeps it on the other side. Its column exists only while a session is on screen, and
  follows Spaces when that view is docked on the other side (`sessionFollows`).
- A **column** holds tabs (views); `left`/`right` in `config.toml` define them.
  A column with one tab draws no activity bar and puts ⚙ in its header.
- The activity bar is `actH` = 2 rows: the icons, then the row that carries
  their marks. A chip is `barPad` (two cells with icons, one with text
  labels), the icon, `barPad`: an odd width keeps a one-cell glyph centered.
  Chips that would run into ⚙ tighten to one cell of air.
- Nothing is filled behind a chip. The marks are VS Code's active border
  (`markColor`): `header_accent` for the open view, `input_border` for the
  one under the mouse, a `━` rule under the icon on a top bar and a `▎` or
  `▐` bar down the strip's outer edge on a side bar. The open view's icon
  takes the accent color too.
- Focus is a column index or `onMain` (-1); `ctrl+]` cycles left → main → right.
- A hidden side folds into a 2-cell **rail** of its tab icons; a click or `»`
  reopens it.
- The active tab of a column is the one shown most recently (`recent` counter),
  so moving views between columns needs no per-column state.
- The **terminal panel** sits under the main area when `terminal_position` is
  `bottom`: `termRows()` takes its height off `mainH()`, and everything in main
  (`pvH`, `sessH`, `peekH`) measures from that. It is focused as `onPanel`
  (-2), one more stop in `ctrl+]`. Moved to a side it becomes an ordinary
  column holding only `viewTerm` — never a tab beside another view.
- `activity_bar = "side"` moves the icons into an `actW`-wide strip down the
  sidebar's outer edge (`barW`), left-docked columns on their left,
  right-docked on their right; `barH` is then 0 and the ⚙ sits at the bottom of
  the strip. The strip holds the same chips the top bar draws, one `actH`-row
  block per view, the second row air, so both bars mark a chip the same way. A
  view waiting on its agent has no room for a count there and takes the
  attention color instead.
- Dragging a tab docks it where it is dropped (`dropAt`): over a column's
  activity bar, or its half facing main, it takes the slot between the chips
  under the mouse (`slotAt`, `placeView` — its own bar reorders); below the bar
  toward the screen edge it gets a column of its own. The drop target draws as
  a rectangle (`dropBox`): a `actH`-row frame between the icons for a slot
  (`actW` wide around the target block on a side bar), the whole labelled
  column for a new one.

## Rows

Every list row is built from segments and padded to the exact column width, so
nothing ever shears:

```
row(w, bg, left…, right…)   " ▾ Changes            ↶ + [3] "
                              │ │                  │   └ count badge (own bg)
                              │ └ label            └ hover actions
                              └ chevron
```

`checkWidths` in the tests asserts every rendered line is exactly the terminal
width, which is what keeps mouse hit-testing and the layout honest.

## Mouse

`Model.mouse` maps a click to a panel, then the panel maps it to a row:

```
x → column rect?        → sideMouse(col): the side icon strip, then
                          y < barH → tabs, y == barH → header actions,
  → divider gap?        → drag divider (width saved on release)
  → main rect           → preview / session
```

Hit tests use the same geometry helpers the renderer uses (`toggles(w)`,
`actions(row, w)`, `buttons(m, w)`), so a moved button cannot desync from its
click zone. The row width passed to a hit test excludes the scrollbar column.

The editor, a session and the Terminal panel keep their last column for VS
Code's editor scrollbar (`vbar` in `widgets.go`), whether or not anything
scrolls: the text width stays put (`pvW`, a session's `Cols` one short of its
view). The slider (`scrollbar_slider`, `scrollbar_slider_active` while held)
shows once rows are out of view, over a track drawn as the overview ruler's
border (`overview_ruler_border`); a terminal counts its scrollback as the rows
above. An app on the alternate screen (vim, less, a claude with
`"tui": "fullscreen"`) has no scrollback and scrolls itself: the daemon
reports none (`Screen.AltScreen`), the bar stays empty, and the wheel and
`pgup` `pgdn` go to the app, the wheel as mouse events when it asked for them,
else as three arrows (xterm's alternate scroll). A click on the slider grabs it, one on the track jumps it there first,
and the drag (`dragScroll`) follows the mouse. Sidebar lists keep their thin
`┃`, drawn from the same geometry and handled the same way (`listBar`): the
slider drags, lit while held, a click on the track jumps it, the wheel over it
scrolls the list, and the pointer over it lights no row. Source Control has
one per pane, beside the changes below their pinned rows and in each drawer.

With word wrap off (the default) the editor has a horizontal bar too: the
same `vbar` geometry on its side (`hbar`, the widest line against the text
width, from `left`), drawn by `hcells` on a row of its own under the text (blanks
underlined in the slider's and the track's colors: a block glyph per cell
seams in VTE at fractional scaling, a background would fill the row), the
gutter and the corner under the vertical bar left blank. The row exists only
while a line runs past the right edge: `pvH` gives it back (`hbarH`), so
`follow`, the peek and every hit test below the text move with it. A drag on
it (`editor-h`, `scrollDrag.horiz`) follows the mouse's column. Split diffs
and Markdown beside its source have none.

Tab strips (editors, a session's tabs, the Terminal's) draw each tab as a
chip (`tabChip`): the active one bold on `tab_active_bg`, a shade past the
selection, the others dim on `tab_bg`, a shade off the background (VS Code's
tab.inactiveBackground), and every tab followed by
`tab_border`'s `▏` in a column of its own (`tabGap`). The layout counts that
column, so hit tests stay on the tabs and a click on the hairline does nothing.
Every tab keeps room for the active one's `✕` (`tabClose`), so activating a
tab changes neither its width nor where the ones after it sit.

A left press on a tab shows it and picks it up (`grabTab`, `dragStrip` in
`strip.go`). As in herdr, the strip stays put while it is held: an accent `│`
in the hairline marks where it would land (`stripSlot`, herdr's
`tab_drop_index_at`: a tab's left half drops before it, its right half after),
and only the release moves it there, saved as a key move is. A release off the
strip's row drops nothing; a session's own tab never picks up.

`ctrl+shift+pgup` `ctrl+shift+pgdn` (_Move Tab Left / Right_, `moveTab` in
`strip.go`) move the focused strip's tab (`focusedStrip`), wrapping at either
end as herdr's `move_tab_previous` / `move_tab_next` do. The order is saved in
`state.json` for editors, through `session.move` for a session's tabs and the
Terminal's shells. A session's own tab stays first.

Hover works because pando requests all-motion mouse mode: the row under the
pointer paints `hoverBg`, header actions appear for 3 s after any motion over
the column (a terminal never reports "mouse left").

## Modals

One overlay at a time (`Model.modal`): a menu (items at a position), a picker
(filter + fuzzy ranking), a prompt (single input) or a dialog (`newDialog`, a
question with a row of buttons).

```
╭ Select a branch or tag to checkout ─────────╮
│›                                            │  input (picker/prompt)
│ + Create new branch…                        │  always: above the items while browsing
│                                             │  spacer before a heading
│ branches                                    │  group: a muted heading (sep + label)
│ ⎇ feat/one 42 seconds ago                   │  selected item, inline muted after label
│   Ann • 059c60c • first commit              │  its detail row, only while selected
│ ⎇ main 2 hours ago                          │
╰─────────────────────────────────────────────╯
```

Pickers keep a fixed top edge and a minimum height, so the box does not jump
while results change. Items can carry a `search` string (`@idle`, `!claude`
tokens must match verbatim, the rest fuzzily), a `group` (filed under a muted
heading; a query ranks within each group and orders the groups by their best
match, so the best match is first and selected, and the `always` items follow
the matches), an `inline` note right after the label, a right-aligned `hint`
and a `detail` row that `focus` moves under the selection, so a long list
stays one row per item. Notes, hints and headings take the `description`
color rather than Faint, which not every terminal draws.

```
╭────────────────────────────────────────╮
│ Close pando?                           │  message, wrapped
│ unsaved text is kept                   │  the focused button's hint
│                                        │
│ Save all and close    Cancel    Close  │  focused button in the button color
╰────────────────────────────────────────╯
```

A dialog's items are its buttons, `items[0]` the primary one and focused
first, `cancelItem()` last. They show in VS Code's Linux order (Cancel moved
next to the primary, then reversed): the primary rightmost, Cancel before it.
The box keeps the height of the tallest hint, and it takes every key: `←→`
`tab` walk the buttons, `⏎` runs the focused one, `esc` cancels; hover does
not move the focus.
