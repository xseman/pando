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
  agents waiting for you, search results. The branch and the project name are
  buttons, lit under the mouse: the branch opens the branch picker, the project
  a switcher over every project with _Add Project…_ on top; the agent count
  opens the navigator.
- The bottom terminal panel's title row, outside its tabs, is a sash: dragging
  it sets `terminal_height`. A right click in the panel opens its menu (Copy
  All, Paste, Clear, Kill Terminal, Toggle Size to Content Width).
- A view moves by dragging its tab or its header title to the other side.
- The agent session is the `session` view: a column of its own beside the
  editor, on the side `session_position` names (`right` by default); a column
  nobody sized takes half the editor area. `left`/`right` list it once it has
  been docked or resized by hand, and keep its place while `session_position`
  is `editor` — the session over the whole editor area, which it takes when it
  is widened past `snapMain` cells of editor or dropped in the middle of it.
  Opening a file then docks it back on that side (`sessSide`), so the file has
  somewhere to go. Its column exists only while a session is on screen, and
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

Hover works because pando requests all-motion mouse mode: the row under the
pointer paints `hoverBg`, header actions appear for 3 s after any motion over
the column (a terminal never reports "mouse left").

## Modals

One overlay at a time (`Model.modal`): a menu (items at a position), a picker
(filter + fuzzy ranking) or a prompt (single input).

```
╭ Select a branch or tag to checkout ─────────╮
│›                                            │  input (picker/prompt)
│ + Create new branch…                        │  always: pinned above results
│                                             │  spacer before a heading
│ branches                                    │  group heading
│ ⎇ feat/one 42 seconds ago                   │  item, inline dimmed after label
│   Ann • 059c60c • first commit              │  detail row of the item above
╰─────────────────────────────────────────────╯
```

Pickers keep a fixed top edge and a minimum height, so the box does not jump
while results change. Items can carry a `search` string (`@idle`, `!claude`
tokens must match verbatim, the rest fuzzily), a `group` (browsing files the
items under a heading, a query ranks across all of them), an `inline` note
drawn dimmed right after the label and a right-aligned `hint`.
